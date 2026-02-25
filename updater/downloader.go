//go:build windows

package updater

import (
	"crypto/hmac"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/blake2b"
	"golang.org/x/sys/windows"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/config"
	"github.com/fosrl/windows/elevate"
	"github.com/fosrl/windows/updater/winhttp"
	"github.com/fosrl/windows/version"
)

type DownloadProgress struct {
	Activity        string
	BytesDownloaded uint64
	BytesTotal      uint64
	Error           error
	Complete        bool
}

type progressHashWatcher struct {
	dp        *DownloadProgress
	c         chan DownloadProgress
	hashState hash.Hash
}

func (pm *progressHashWatcher) Write(p []byte) (int, error) {
	bytes := len(p)
	pm.dp.BytesDownloaded += uint64(bytes)
	pm.c <- *pm.dp
	pm.hashState.Write(p)
	return bytes, nil
}

type UpdateFound struct {
	name             string
	hash             [blake2b.Size256]byte
	downloadLocation string // Can be empty (use default), a relative path, or a full URL
}

// Name returns the filename of the update MSI
func (u *UpdateFound) Name() string {
	return u.name
}

func CheckForUpdate() (updateFound *UpdateFound, err error) {
	logger.Info("Updater: CheckForUpdate() called")
	updateFound, _, _, err = checkForUpdate(false)
	if err != nil {
		logger.Error("Updater: CheckForUpdate failed: %v", err)
	} else if updateFound == nil {
		logger.Info("Updater: CheckForUpdate completed - no update found")
	} else {
		logger.Info("Updater: CheckForUpdate completed - update found: %s", updateFound.name)
	}
	return
}

func checkForUpdate(keepSession bool) (*UpdateFound, *winhttp.Session, *winhttp.Connection, error) {
	logger.Info("Updater: checkForUpdate() started (keepSession=%v)", keepSession)
	logger.Info("Updater: Current version: %s, Architecture: %s", version.Number, version.Arch())

	// // Allow bypassing official version check for development/testing
	// // Set PANGOLIN_ALLOW_DEV_UPDATES=1 to enable updates on unsigned builds
	// isOfficial := version.IsRunningOfficialVersion()
	// logger.Info("Updater: IsRunningOfficialVersion: %v", isOfficial)
	// if !isOfficial {
	// 	devMode := os.Getenv("PANGOLIN_ALLOW_DEV_UPDATES") == "1"
	// 	logger.Info("Updater: PANGOLIN_ALLOW_DEV_UPDATES: %v", devMode)
	// 	if !devMode {
	// 		err := errors.New("Build is not official, so updates are disabled")
	// 		logger.Error("Updater: %v", err)
	// 		return nil, nil, nil, err
	// 	}
	// 	logger.Info("Updater: Development mode enabled - allowing updates on unsigned build")
	// }

	logger.Info("Updater: Creating WinHTTP session with User-Agent: %s", version.UserAgent())
	session, err := winhttp.NewSession(version.UserAgent())
	if err != nil {
		logger.Error("Updater: Failed to create WinHTTP session: %v", err)
		return nil, nil, nil, err
	}
	logger.Info("Updater: WinHTTP session created successfully")
	defer func() {
		if err != nil || !keepSession {
			logger.Info("Updater: Closing WinHTTP session")
			session.Close()
		}
	}()

	logger.Info("Updater: Connecting to update server: %s:%d (HTTPS=%v)", updateServerHost, updateServerPort, updateServerUseHttps)
	connection, err := session.Connect(updateServerHost, updateServerPort, updateServerUseHttps)
	if err != nil {
		logger.Error("Updater: Failed to connect to update server: %v", err)
		return nil, nil, nil, err
	}
	logger.Info("Updater: Connected to update server successfully")
	defer func() {
		if err != nil || !keepSession {
			logger.Info("Updater: Closing connection")
			connection.Close()
		}
	}()

	logger.Info("Updater: Fetching manifest from: %s", latestVersionPath)
	response, err := connection.Get(latestVersionPath, true)
	if err != nil {
		logger.Error("Updater: Failed to fetch manifest: %v", err)
		return nil, nil, nil, err
	}
	defer response.Close()
	logger.Info("Updater: Manifest response received")

	var fileList [1024 * 512] /* 512 KiB */ byte
	bytesRead, err := response.Read(fileList[:])
	if err != nil && (err != io.EOF || bytesRead == 0) {
		logger.Error("Updater: Failed to read manifest data: %v (bytesRead=%d)", err, bytesRead)
		return nil, nil, nil, err
	}
	logger.Info("Updater: Read %d bytes from manifest", bytesRead)

	logger.Info("Updater: Parsing manifest file list")
	files, err := readFileList(fileList[:bytesRead])
	if err != nil {
		logger.Error("Updater: Failed to parse manifest: %v", err)
		return nil, nil, nil, err
	}
	logger.Info("Updater: Manifest parsed successfully, found %d files", len(files))

	logger.Info("Updater: Searching for update candidate")
	updateFound, err := findCandidate(files)
	if err != nil {
		logger.Error("Updater: Error finding candidate: %v", err)
		return nil, nil, nil, err
	}
	if updateFound == nil {
		logger.Info("Updater: No update candidate found")
	} else {
		logger.Info("Updater: Update candidate found: %s", updateFound.name)
	}

	if keepSession {
		logger.Info("Updater: Keeping session and connection open")
		return updateFound, session, connection, nil
	}
	return updateFound, nil, nil, nil
}

var updateInProgress = uint32(0)

func DownloadVerifyAndExecute(userToken uintptr) (progress chan DownloadProgress) {
	progress = make(chan DownloadProgress, 128)
	progress <- DownloadProgress{Activity: "Initializing"}

	if !atomic.CompareAndSwapUint32(&updateInProgress, 0, 1) {
		progress <- DownloadProgress{Error: errors.New("An update is already in progress")}
		return
	}

	doIt := func() {
		defer atomic.StoreUint32(&updateInProgress, 0)
		logger.Info("Updater: DownloadVerifyAndExecute started (userToken=%v)", userToken != 0)

		progress <- DownloadProgress{Activity: "Checking for update"}
		logger.Info("Updater: Checking for update...")
		update, session, connection, err := checkForUpdate(true)
		if err != nil {
			logger.Error("Updater: Update check failed: %v", err)
			progress <- DownloadProgress{Error: err}
			return
		}
		defer connection.Close()
		defer session.Close()
		if update == nil {
			logger.Error("Updater: No update was found")
			progress <- DownloadProgress{Error: errors.New("No update was found")}
			return
		}
		logger.Info("Updater: Update found: %s", update.name)

		progress <- DownloadProgress{Activity: "Creating temporary file"}
		logger.Info("Updater: Creating temporary file for MSI")
		file, err := msiTempFile()
		if err != nil {
			logger.Error("Updater: Failed to create temporary file: %v", err)
			progress <- DownloadProgress{Error: err}
			return
		}
		logger.Info("Updater: Temporary file created: %s", file.Name())
		progress <- DownloadProgress{Activity: fmt.Sprintf("Msi destination is %#q", file.Name())}
		defer func() {
			if file != nil {
				logger.Info("Updater: Cleaning up temporary file: %s", file.Name())
				file.Delete()
			}
		}()

		dp := DownloadProgress{Activity: "Downloading update"}
		progress <- dp

		var response *winhttp.Response
		var downloadConnection *winhttp.Connection
		var downloadSession *winhttp.Session

		// Get download location from manifest (required)
		downloadLocation := update.downloadLocation
		if downloadLocation == "" {
			logger.Error("Updater: Download location not specified in manifest for file: %s", update.name)
			progress <- DownloadProgress{Error: errors.New("download location not specified in manifest")}
			return
		}

		// Check if downloadLocation is a full URL (http:// or https://)
		if strings.HasPrefix(downloadLocation, "http://") || strings.HasPrefix(downloadLocation, "https://") {
			// Full URL - download from external source
			logger.Info("Updater: Downloading MSI from external URL: %s", downloadLocation)

			// Parse the URL to extract host, port, and path
			parsedURL, err := url.Parse(downloadLocation)
			if err != nil {
				logger.Error("Updater: Failed to parse download URL: %v", err)
				progress <- DownloadProgress{Error: fmt.Errorf("invalid download URL: %w", err)}
				return
			}

			// Determine if HTTPS
			isHTTPS := parsedURL.Scheme == "https"
			if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
				logger.Error("Updater: Unsupported URL scheme: %s", parsedURL.Scheme)
				progress <- DownloadProgress{Error: fmt.Errorf("unsupported URL scheme: %s", parsedURL.Scheme)}
				return
			}

			// Extract host and port
			host := parsedURL.Hostname()
			if host == "" {
				logger.Error("Updater: Missing host in download URL")
				progress <- DownloadProgress{Error: errors.New("missing host in download URL")}
				return
			}

			portStr := parsedURL.Port()
			var port uint16
			if portStr == "" {
				// Use default port based on scheme
				if isHTTPS {
					port = 443
				} else {
					port = 80
				}
			} else {
				portNum, err := strconv.ParseUint(portStr, 10, 16)
				if err != nil {
					logger.Error("Updater: Invalid port in download URL: %v", err)
					progress <- DownloadProgress{Error: fmt.Errorf("invalid port in download URL: %w", err)}
					return
				}
				port = uint16(portNum)
			}

			// Build the path (include query and fragment if present)
			downloadPath := parsedURL.Path
			if parsedURL.RawQuery != "" {
				downloadPath += "?" + parsedURL.RawQuery
			}
			if parsedURL.Fragment != "" {
				downloadPath += "#" + parsedURL.Fragment
			}
			if downloadPath == "" {
				downloadPath = "/"
			}

			logger.Info("Updater: Connecting to external download server: %s:%d (HTTPS=%v)", host, port, isHTTPS)

			// Create a new session for the external download
			downloadSession, err = winhttp.NewSession(version.UserAgent())
			if err != nil {
				logger.Error("Updater: Failed to create WinHTTP session for external download: %v", err)
				progress <- DownloadProgress{Error: err}
				return
			}
			defer downloadSession.Close()

			// Connect to the external host
			downloadConnection, err = downloadSession.Connect(host, port, isHTTPS)
			if err != nil {
				logger.Error("Updater: Failed to connect to external download server: %v", err)
				progress <- DownloadProgress{Error: err}
				return
			}
			defer downloadConnection.Close()

			logger.Info("Updater: Downloading MSI from path: %s", downloadPath)
			response, err = downloadConnection.Get(downloadPath, false)
			if err != nil {
				logger.Error("Updater: Failed to download MSI from external URL: %v", err)
				progress <- DownloadProgress{Error: err}
				return
			}
		} else {
			// Relative path - download from update server
			logger.Info("Updater: Downloading MSI from update server: %s", downloadLocation)
			response, err = connection.Get(downloadLocation, false)
			if err != nil {
				logger.Error("Updater: Failed to download MSI: %v", err)
				progress <- DownloadProgress{Error: err}
				return
			}
		}
		defer response.Close()
		logger.Info("Updater: MSI download response received")

		length, err := response.Length()
		if err == nil {
			logger.Info("Updater: MSI file size: %d bytes", length)
			dp.BytesTotal = length
			progress <- dp
		} else {
			logger.Warn("Updater: Could not determine MSI file size: %v", err)
		}

		logger.Info("Updater: Initializing BLAKE2b-256 hasher for verification")
		hasher, err := blake2b.New256(nil)
		if err != nil {
			logger.Error("Updater: Failed to create hasher: %v", err)
			progress <- DownloadProgress{Error: err}
			return
		}
		pm := &progressHashWatcher{&dp, progress, hasher}
		logger.Info("Updater: Starting download (max 100 MiB)")
		bytesWritten, err := io.Copy(file, io.TeeReader(io.LimitReader(response, 1024*1024*100 /* 100 MiB */), pm))
		if err != nil {
			logger.Error("Updater: Download failed: %v (bytes written: %d)", err, bytesWritten)
			progress <- DownloadProgress{Error: err}
			return
		}
		logger.Info("Updater: Download completed: %d bytes written", bytesWritten)

		calculatedHash := hasher.Sum(nil)
		logger.Info("Updater: Verifying hash - calculated: %x, expected: %x", calculatedHash, update.hash)
		if !hmac.Equal(calculatedHash, update.hash[:]) {
			logger.Error("Updater: Hash verification failed!")
			progress <- DownloadProgress{Error: errors.New("The downloaded update has the wrong hash")}
			return
		}
		logger.Info("Updater: Hash verification passed")

		// // Skip authenticode verification in development mode
		// devMode := os.Getenv("PANGOLIN_ALLOW_DEV_UPDATES") == "1"
		// if !devMode {
		// 	logger.Info("Updater: Verifying Authenticode signature")
		// 	progress <- DownloadProgress{Activity: "Verifying authenticode signature"}
		// 	if !verifyAuthenticode(file.ExclusivePath()) {
		// 		logger.Error("Updater: Authenticode verification failed")
		// 		progress <- DownloadProgress{Error: errors.New("The downloaded update does not have an authentic authenticode signature")}
		// 		return
		// 	}
		// 	logger.Info("Updater: Authenticode verification passed")
		// } else {
		// 	logger.Info("Updater: Skipping Authenticode verification (dev mode)")
		// }

		logger.Info("Updater: Starting MSI installation")
		progress <- DownloadProgress{Activity: "Installing update"}

		restartUIFlagPath := filepath.Join(config.GetProgramDataDir(), "restart-ui-after-update.flag")
		if err := os.MkdirAll(config.GetProgramDataDir(), 0o755); err != nil {
			logger.Error("Updater: Failed to create ProgramData dir for restart flag: %v", err)
		} else if err := os.WriteFile(restartUIFlagPath, nil, 0o644); err != nil {
			logger.Error("Updater: Failed to write restart-ui flag file: %v", err)
		} else {
			logger.Info("Updater: Wrote restart-ui flag at %s", restartUIFlagPath)
		}

		err = runMsi(file, userToken)
		if err != nil {
			logger.Error("Updater: MSI installation failed: %v", err)
			if removeErr := os.Remove(restartUIFlagPath); removeErr != nil && !os.IsNotExist(removeErr) {
				logger.Error("Updater: Failed to remove restart-ui flag after MSI failure: %v", removeErr)
			}
			progress <- DownloadProgress{Error: err}
			return
		}
		// Flag file left in place so next start can start the UI automatically, then delete the file
		logger.Info("Updater: MSI installation completed successfully")

		logger.Info("Updater: Update process complete")
		progress <- DownloadProgress{Complete: true}
	}
	if userToken == 0 {
		logger.Info("Updater: No user token provided, attempting to run as SYSTEM")

		// Check if we have admin privileges before attempting elevation
		var processToken windows.Token
		err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &processToken)
		if err == nil {
			isElevated := processToken.IsElevated()
			processToken.Close()
			if !isElevated {
				logger.Error("Updater: Process is not running with admin privileges")
				progress <- DownloadProgress{Error: errors.New("update requires administrator privileges. Please run the application as administrator")}
				return progress
			}
			logger.Info("Updater: Process is running with admin privileges")
		}

		go func() {
			err := elevate.DoAsSystem(func() error {
				logger.Info("Updater: Successfully elevated to SYSTEM, starting update process")
				doIt()
				return nil
			})
			if err != nil {
				logger.Error("Updater: Failed to elevate to SYSTEM: %v", err)
				progress <- DownloadProgress{Error: fmt.Errorf("failed to elevate privileges: %w. Make sure the application is running as administrator", err)}
			}
		}()
	} else {
		logger.Info("Updater: Using provided user token: %v", userToken)
		go doIt()
	}

	return progress
}

// UpdateFoundCallback is a function type that gets called when an update is found
type UpdateFoundCallback func(update *UpdateFound)

// StartBackgroundUpdateChecker starts a background goroutine that checks for updates
// at the specified interval. When an update is found, it calls the provided callback.
// The function performs an initial check after a 30 second delay, then checks at
// the specified interval thereafter.
func StartBackgroundUpdateChecker(interval time.Duration, callback UpdateFoundCallback) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		// Perform initial check after a short delay
		time.Sleep(30 * time.Second)

		for {
			// Check for updates
			logger.Info("Background update check: checking for updates...")
			update, err := CheckForUpdate()
			if err != nil {
				logger.Error("Background update check failed: %v", err)
				// Don't call callback for errors, just log
			} else if update != nil {
				logger.Info("Background update check: update found: %s", update.Name())
				// Call the callback to notify about the update
				if callback != nil {
					callback(update)
				}
			} else {
				logger.Info("Background update check: no update available")
			}

			// Wait for next tick
			<-ticker.C
		}
	}()
}

package updater

import (
	"crypto/hmac"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/blake2b"
	"golang.org/x/sys/windows"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows-linux/elevate"
	"github.com/fosrl/windows-linux/updater/winhttp"
	"github.com/fosrl/windows-linux/version"
)

func CheckForUpdate() (updateFound *UpdateFound, err error) {
	logger.Debug("Updater: CheckForUpdate() called")
	updateFound, _, _, err = checkForUpdate(false)
	if err != nil {
		logger.Debug("Updater: CheckForUpdate failed: %v", err)
	} else if updateFound == nil {
		logger.Debug("Updater: CheckForUpdate completed - no update found")
	} else {
		logger.Debug("Updater: CheckForUpdate completed - update found: %s", updateFound.name)
	}
	return
}

func checkForUpdate(keepSession bool) (*UpdateFound, *winhttp.Session, *winhttp.Connection, error) {
	logger.Debug("Updater: checkForUpdate() started (keepSession=%v)", keepSession)
	logger.Debug("Updater: Current version: %s, Architecture: %s", version.Number, version.Arch())

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

	logger.Debug("Updater: Creating WinHTTP session with User-Agent: %s", version.UserAgent())
	session, err := winhttp.NewSession(version.UserAgent())
	if err != nil {
		logger.Debug("Updater: Failed to create WinHTTP session: %v", err)
		return nil, nil, nil, err
	}
	logger.Debug("Updater: WinHTTP session created successfully")
	defer func() {
		if err != nil || !keepSession {
			logger.Debug("Updater: Closing WinHTTP session")
			session.Close()
		}
	}()

	logger.Debug("Updater: Connecting to update server: %s:%d (HTTPS=%v)", updateServerHost, updateServerPort, updateServerUseHttps)
	connection, err := session.Connect(updateServerHost, updateServerPort, updateServerUseHttps)
	if err != nil {
		logger.Debug("Updater: Failed to connect to update server: %v", err)
		return nil, nil, nil, err
	}
	logger.Debug("Updater: Connected to update server successfully")
	defer func() {
		if err != nil || !keepSession {
			logger.Debug("Updater: Closing connection")
			connection.Close()
		}
	}()

	logger.Debug("Updater: Fetching manifest from: %s", latestVersionPath)
	response, err := connection.Get(latestVersionPath, true)
	if err != nil {
		logger.Debug("Updater: Failed to fetch manifest: %v", err)
		return nil, nil, nil, err
	}
	defer response.Close()
	logger.Debug("Updater: Manifest response received")

	var fileList [1024 * 512] /* 512 KiB */ byte
	bytesRead, err := response.Read(fileList[:])
	if err != nil && (err != io.EOF || bytesRead == 0) {
		logger.Debug("Updater: Failed to read manifest data: %v (bytesRead=%d)", err, bytesRead)
		return nil, nil, nil, err
	}
	logger.Debug("Updater: Read %d bytes from manifest", bytesRead)

	logger.Debug("Updater: Parsing manifest file list")
	files, err := readFileList(fileList[:bytesRead])
	if err != nil {
		logger.Debug("Updater: Failed to parse manifest: %v", err)
		return nil, nil, nil, err
	}
	logger.Debug("Updater: Manifest parsed successfully, found %d files", len(files))

	logger.Debug("Updater: Searching for update candidate")
	updateFound, err := findCandidate(files)
	if err != nil {
		logger.Debug("Updater: Error finding candidate: %v", err)
		return nil, nil, nil, err
	}
	if updateFound == nil {
		logger.Debug("Updater: No update candidate found")
	} else {
		logger.Debug("Updater: Update candidate found: %s", updateFound.name)
		updateFound.manifest = append([]byte(nil), fileList[:bytesRead]...)
	}

	if keepSession {
		logger.Debug("Updater: Keeping session and connection open")
		return updateFound, session, connection, nil
	}
	return updateFound, nil, nil, nil
}

var updateInProgress = uint32(0)

// downloadAndVerify checks for an update, downloads its MSI, verifies it against the
// signed manifest and stages it on disk. progress may be nil when nobody is watching.
func downloadAndVerify(progress chan DownloadProgress) (*StagedUpdate, error) {
	send := func(dp DownloadProgress) {
		if progress != nil {
			progress <- dp
		}
	}

	send(DownloadProgress{Activity: "Checking for update"})
	logger.Info("Updater: Checking for update...")
	update, session, connection, err := checkForUpdate(true)
	if err != nil {
		logger.Info("Updater: Update check failed: %v", err)
		return nil, err
	}
	defer connection.Close()
	defer session.Close()
	if update == nil {
		logger.Info("Updater: No update was found")
		return nil, errors.New("No update was found")
	}
	logger.Info("Updater: Update found: %s", update.name)

	send(DownloadProgress{Activity: "Creating temporary file"})
	logger.Debug("Updater: Creating staging file for MSI")
	file, err := createStagingFile()
	if err != nil {
		logger.Debug("Updater: Failed to create staging file: %v", err)
		return nil, err
	}
	logger.Debug("Updater: Staging file created: %s", file.Name())
	defer func() {
		file.Close()
		os.Remove(file.Name()) // no-op once committed
	}()

	dp := DownloadProgress{Activity: "Downloading update"}
	send(dp)

	var response *winhttp.Response
	var downloadConnection *winhttp.Connection
	var downloadSession *winhttp.Session

	// Get download location from manifest (required)
	downloadLocation := update.downloadLocation
	if downloadLocation == "" {
		logger.Debug("Updater: Download location not specified in manifest for file: %s", update.name)
		return nil, errors.New("download location not specified in manifest")
	}

	// Check if downloadLocation is a full URL (http:// or https://)
	if strings.HasPrefix(downloadLocation, "http://") || strings.HasPrefix(downloadLocation, "https://") {
		// Full URL - download from external source
		logger.Debug("Updater: Downloading MSI from external URL: %s", downloadLocation)

		// Parse the URL to extract host, port, and path
		parsedURL, err := url.Parse(downloadLocation)
		if err != nil {
			logger.Debug("Updater: Failed to parse download URL: %v", err)
			return nil, fmt.Errorf("invalid download URL: %w", err)
		}

		// Determine if HTTPS
		isHTTPS := parsedURL.Scheme == "https"
		if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
			logger.Debug("Updater: Unsupported URL scheme: %s", parsedURL.Scheme)
			return nil, fmt.Errorf("unsupported URL scheme: %s", parsedURL.Scheme)
		}

		// Extract host and port
		host := parsedURL.Hostname()
		if host == "" {
			logger.Debug("Updater: Missing host in download URL")
			return nil, errors.New("missing host in download URL")
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
				logger.Debug("Updater: Invalid port in download URL: %v", err)
				return nil, fmt.Errorf("invalid port in download URL: %w", err)
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

		logger.Debug("Updater: Connecting to external download server: %s:%d (HTTPS=%v)", host, port, isHTTPS)

		// Create a new session for the external download
		downloadSession, err = winhttp.NewSession(version.UserAgent())
		if err != nil {
			logger.Debug("Updater: Failed to create WinHTTP session for external download: %v", err)
			return nil, err
		}
		defer downloadSession.Close()

		// Connect to the external host
		downloadConnection, err = downloadSession.Connect(host, port, isHTTPS)
		if err != nil {
			logger.Debug("Updater: Failed to connect to external download server: %v", err)
			return nil, err
		}
		defer downloadConnection.Close()

		logger.Debug("Updater: Downloading MSI from path: %s", downloadPath)
		response, err = downloadConnection.Get(downloadPath, false)
		if err != nil {
			logger.Debug("Updater: Failed to download MSI from external URL: %v", err)
			return nil, err
		}
	} else {
		// Relative path - download from update server
		logger.Debug("Updater: Downloading MSI from update server: %s", downloadLocation)
		response, err = connection.Get(downloadLocation, false)
		if err != nil {
			logger.Debug("Updater: Failed to download MSI: %v", err)
			return nil, err
		}
	}
	defer response.Close()
	defer response.Close()
	logger.Debug("Updater: MSI download response received")

	length, err := response.Length()
	if err == nil {
		logger.Debug("Updater: MSI file size: %d bytes", length)
		dp.BytesTotal = length
		send(dp)
	} else {
		logger.Debug("Updater: Could not determine MSI file size: %v", err)
	}

	logger.Debug("Updater: Initializing BLAKE2b-256 hasher for verification")
	hasher, err := blake2b.New256(nil)
	if err != nil {
		logger.Debug("Updater: Failed to create hasher: %v", err)
		return nil, err
	}
	pm := &progressHashWatcher{&dp, progress, hasher}
	logger.Debug("Updater: Starting download (max 100 MiB)")
	bytesWritten, err := io.Copy(file, io.TeeReader(io.LimitReader(response, maxMsiSize), pm))
	if err != nil {
		logger.Debug("Updater: Download failed: %v (bytes written: %d)", err, bytesWritten)
		return nil, err
	}
	logger.Debug("Updater: Download completed: %d bytes written", bytesWritten)

	calculatedHash := hasher.Sum(nil)
	logger.Debug("Updater: Verifying hash - calculated: %x, expected: %x", calculatedHash, update.hash)
	if !hmac.Equal(calculatedHash, update.hash[:]) {
		logger.Debug("Updater: Hash verification failed!")
		return nil, errors.New("The downloaded update has the wrong hash")
	}
	logger.Debug("Updater: Hash verification passed")

	// // Skip authenticode verification in development mode
	// devMode := os.Getenv("PANGOLIN_ALLOW_DEV_UPDATES") == "1"
	// if !devMode {
	// 	logger.Info("Updater: Verifying Authenticode signature")
	// 	send(DownloadProgress{Activity: "Verifying authenticode signature"})
	// 	if !verifyAuthenticode(file.Name()) {
	// 		logger.Error("Updater: Authenticode verification failed")
	// 		return nil, errors.New("The downloaded update does not have an authentic authenticode signature")
	// 	}
	// 	logger.Info("Updater: Authenticode verification passed")
	// } else {
	// 	logger.Info("Updater: Skipping Authenticode verification (dev mode)")
	// }

	if err := file.Close(); err != nil {
		return nil, err
	}
	if err := commitStagedFiles(update.manifest); err != nil {
		logger.Debug("Updater: Failed to stage update: %v", err)
		return nil, err
	}
	logger.Info("Updater: Update %s staged", update.version)
	return &StagedUpdate{version: update.version, StagedAt: time.Now()}, nil
}

// installStaged verifies the staged MSI and installs it with run. When restartUI is
// set, the new manager relaunches the UI in the console session after the upgrade.
// A staged update that fails to install is discarded so it is not retried on every
// launch; the next check downloads it again.
func installStaged(restartUI bool, run func(msi *tempFile) error) error {
	msi, err := prepareStagedMsi()
	if err != nil {
		logger.Error("Updater: Unable to use staged update: %v", err)
		discardStagedFiles()
		return err
	}
	defer msi.Delete()

	logger.Info("Updater: Starting MSI installation")
	if restartUI {
		if err := RelaunchUIAfterUpdate(0); err != nil {
			logger.Debug("Updater: Failed to write restart-ui flag file: %v", err)
		}
	}

	err = run(msi)
	if err != nil {
		logger.Info("Updater: MSI installation failed: %v", err)
		if restartUI {
			CancelUIRelaunchAfterUpdate()
		}
		discardStagedFiles()
		return err
	}
	// Flag file left in place so next start can start the UI automatically, then delete the file
	logger.Info("Updater: MSI installation completed successfully")
	discardStagedFiles()
	return nil
}

// DownloadInBackground downloads, verifies and stages the latest update without installing it.
func DownloadInBackground() (*StagedUpdate, error) {
	if !atomic.CompareAndSwapUint32(&updateInProgress, 0, 1) {
		return nil, errors.New("An update is already in progress")
	}
	defer atomic.StoreUint32(&updateInProgress, 0)
	logger.Info("Updater: Downloading update in background")
	return downloadAndVerify(nil)
}

// InstallStagedInBackground installs the staged update using the calling process's
// token (the manager service runs as SYSTEM). With session 0 it shows no UI at all;
// otherwise the user in session is waiting for Pangolin to launch, so msiexec's
// progress bar is shown on their desktop. Any UI relaunch is requested separately
// with RelaunchUIAfterUpdate.
func InstallStagedInBackground(session uint32) error {
	if !atomic.CompareAndSwapUint32(&updateInProgress, 0, 1) {
		return errors.New("An update is already in progress")
	}
	defer atomic.StoreUint32(&updateInProgress, 0)
	return installStaged(false, func(msi *tempFile) error {
		if session == 0 {
			return runMsi(msi, 0, true)
		}
		return runMsiInSession(msi, session)
	})
}

// DownloadVerifyAndExecute installs the latest update, reporting progress on the
// returned channel. With useStaged set, the staged update is installed instead of
// downloading again.
func DownloadVerifyAndExecute(userToken uintptr, useStaged bool) (progress chan DownloadProgress) {
	progress = make(chan DownloadProgress, 128)
	progress <- DownloadProgress{Activity: "Initializing"}

	if !atomic.CompareAndSwapUint32(&updateInProgress, 0, 1) {
		progress <- DownloadProgress{Error: errors.New("An update is already in progress")}
		return
	}

	doIt := func() {
		defer atomic.StoreUint32(&updateInProgress, 0)
		logger.Info("Updater: DownloadVerifyAndExecute started (userToken=%v, useStaged=%v)", userToken != 0, useStaged)

		if !useStaged {
			if _, err := downloadAndVerify(progress); err != nil {
				progress <- DownloadProgress{Error: err}
				return
			}
		} else {
			logger.Info("Updater: Using previously downloaded update")
		}

		progress <- DownloadProgress{Activity: "Installing update"}
		if err := installStaged(true, func(msi *tempFile) error { return runMsi(msi, userToken, false) }); err != nil {
			progress <- DownloadProgress{Error: err}
			return
		}

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
				logger.Info("Updater: Process is not running with admin privileges")
				atomic.StoreUint32(&updateInProgress, 0)
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
				logger.Info("Updater: Failed to elevate to SYSTEM: %v", err)
				progress <- DownloadProgress{Error: fmt.Errorf("failed to elevate privileges: %w. Make sure the application is running as administrator", err)}
			}
		}()
	} else {
		logger.Info("Updater: Using provided user token: %v", userToken)
		go doIt()
	}

	return progress
}

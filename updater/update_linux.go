package updater

import (
	"context"
	"crypto/hmac"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows-linux/version"
	"golang.org/x/crypto/blake2b"
)

// Linux updates use the same signed manifest scheme as Windows: a signify
// signed list of BLAKE2b-256 hashes at latestVersionPath. deb, rpm and Arch
// installs download the package, check its hash and install it through the
// system package manager. The install runs in its own transient systemd unit,
// because the package restarts this daemon partway through. AUR and
// hand-installed builds are told how to update instead; see ManualUpdate.
//
// Every download is staged first (see staged_linux.go), as on Windows: with
// automatic downloads on, the daemon stages updates in the background and
// installs them when Pangolin quits or launches, or when the daemon starts.

const (
	maxManifestSize = 512 * 1024
	maxPackageSize  = 100 * 1024 * 1024
	httpTimeout     = 30 * time.Second
)

var httpClient = &http.Client{Timeout: 10 * time.Minute}

func get(ctx context.Context, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", version.UserAgent())
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: %s", rawURL, resp.Status)
	}
	return resp, nil
}

// updateBaseURL replaces the update server's scheme and host when set, for
// test builds (-ldflags -X); release builds leave it empty.
var updateBaseURL string

func updateServerURL(path string) string {
	if updateBaseURL != "" {
		return strings.TrimSuffix(updateBaseURL, "/") + path
	}
	scheme := "https"
	if !updateServerUseHttps {
		scheme = "http"
	}
	host := updateServerHost
	if updateServerPort != 443 && updateServerPort != 80 {
		host += ":" + strconv.Itoa(updateServerPort)
	}
	return scheme + "://" + host + path
}

// CheckForUpdate fetches the signed manifest and returns a newer version for
// this machine, or nil if there is none.
func CheckForUpdate() (*UpdateFound, error) {
	ctx, cancel := context.WithTimeout(context.Background(), httpTimeout)
	defer cancel()
	resp, err := get(ctx, updateServerURL(latestVersionPath))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxManifestSize))
	if err != nil {
		return nil, err
	}
	files, err := readFileList(data)
	if err != nil {
		return nil, err
	}
	update, err := findCandidate(files)
	if update != nil {
		update.manifest = data
	}
	return update, err
}

// ManualUpdate returns instructions, and a command where there is one, for
// installs that can't update in the app: the AUR package, which its AUR
// helper keeps up to date, and builds installed by hand. Both are empty when
// the app can install updates.
func ManualUpdate() (instructions, command string) {
	switch packageFormat() {
	case formatDeb, formatRPM, formatArch:
		return "", ""
	case formatAUR:
		return "Pangolin was installed from the AUR. Update the pangolin-client-bin package with your AUR helper, for example:",
			"yay -Syu pangolin-client-bin"
	default:
		return "This copy of Pangolin wasn't installed from a package. Download the new version from:",
			"https://github.com/fosrl/windows-linux/releases/latest"
	}
}

// manualUpdateError is the error for installs that can't update in the app.
func manualUpdateError() error {
	if instructions, command := ManualUpdate(); instructions != "" {
		return fmt.Errorf("%s %s", instructions, command)
	}
	return nil
}

var updateInProgress atomic.Bool

// DownloadVerifyAndExecute downloads the update, checks it against the
// signed manifest and starts installing it. With useStaged set, the update
// downloaded in the background is installed instead of downloading again.
// userToken is non-zero when the requesting user may install updates; a
// staged update may be installed by anyone, as it would be when they quit
// Pangolin. Progress, and any error, arrive on the returned channel.
func DownloadVerifyAndExecute(userToken uintptr, useStaged bool) chan DownloadProgress {
	progress := make(chan DownloadProgress, 128)
	progress <- DownloadProgress{Activity: "Initializing"}
	if userToken == 0 && !useStaged {
		progress <- DownloadProgress{Error: errors.New("installing updates needs an administrator: a member of the sudo or wheel group")}
		return progress
	}
	if err := manualUpdateError(); err != nil {
		progress <- DownloadProgress{Error: err}
		return progress
	}
	if !updateInProgress.CompareAndSwap(false, true) {
		progress <- DownloadProgress{Error: errors.New("an update is already in progress")}
		return progress
	}
	go func() {
		defer updateInProgress.Store(false)
		if !useStaged {
			if _, err := downloadAndVerify(progress); err != nil {
				logger.Error("Updater: %v", err)
				progress <- DownloadProgress{Error: err}
				return
			}
		} else {
			logger.Info("Updater: using the update downloaded in the background")
		}
		progress <- DownloadProgress{Activity: "Installing update"}
		if err := installStaged(); err != nil {
			os.Remove(RestartUIFile)
			logger.Error("Updater: %v", err)
			progress <- DownloadProgress{Error: err}
			return
		}
		// Usually the package restarts the daemon before this point.
		progress <- DownloadProgress{Complete: true}
	}()
	return progress
}

// DownloadInBackground downloads, verifies and stages the latest update
// without installing it.
func DownloadInBackground() (*StagedUpdate, error) {
	if err := manualUpdateError(); err != nil {
		return nil, err
	}
	if !updateInProgress.CompareAndSwap(false, true) {
		return nil, errors.New("an update is already in progress")
	}
	defer updateInProgress.Store(false)
	logger.Info("Updater: downloading update in the background")
	return downloadAndVerify(nil)
}

// InstallStagedInBackground installs the staged update without showing
// anything. The session argument matches Windows, where it picks the desktop
// that shows msiexec's progress; on Linux a UI waiting for the install shows
// its own progress window. Any UI relaunch is requested separately with
// RelaunchUIAfterUpdate.
func InstallStagedInBackground(uint32) error {
	if !updateInProgress.CompareAndSwap(false, true) {
		return errors.New("an update is already in progress")
	}
	defer updateInProgress.Store(false)
	return installStaged()
}

// downloadAndVerify checks for an update, downloads its package, verifies it
// against the signed manifest and stages it. progress may be nil.
func downloadAndVerify(progress chan DownloadProgress) (*StagedUpdate, error) {
	send := func(dp DownloadProgress) {
		if progress != nil {
			progress <- dp
		}
	}
	send(DownloadProgress{Activity: "Checking for update"})
	update, err := CheckForUpdate()
	if err != nil {
		return nil, err
	}
	if update == nil {
		return nil, errors.New("no update was found")
	}
	logger.Info("Updater: downloading %s", update.name)

	location := update.downloadLocation
	if location == "" {
		return nil, errors.New("download location not specified in manifest")
	}
	if !strings.HasPrefix(location, "https://") && !strings.HasPrefix(location, "http://") {
		location = updateServerURL(location)
	}
	if u, err := url.Parse(location); err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("invalid download URL %q", location)
	}

	f, err := createStagingFile()
	if err != nil {
		return nil, err
	}
	defer func() {
		f.Close()
		os.Remove(f.Name()) // no-op once committed
	}()
	if err := download(location, f, update, progress); err != nil {
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	if err := commitStagedFiles(update.manifest); err != nil {
		return nil, err
	}
	logger.Info("Updater: update %s staged", update.version)
	return &StagedUpdate{version: update.version, StagedAt: time.Now()}, nil
}

func download(location string, f *os.File, update *UpdateFound, progress chan DownloadProgress) error {
	dp := DownloadProgress{Activity: "Downloading update"}
	if progress != nil {
		progress <- dp
	}
	resp, err := get(context.Background(), location)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.ContentLength > 0 {
		dp.BytesTotal = uint64(resp.ContentLength)
		if progress != nil {
			progress <- dp
		}
	}
	hasher, _ := blake2b.New256(nil)
	watcher := &progressHashWatcher{dp: &dp, c: progress, hashState: hasher}
	if _, err := io.Copy(f, io.TeeReader(io.LimitReader(resp.Body, maxPackageSize), watcher)); err != nil {
		return err
	}
	if !hmac.Equal(hasher.Sum(nil), update.hash[:]) {
		return errors.New("the downloaded update has the wrong hash")
	}
	return nil
}

// installStaged verifies the staged package and installs it. A staged update
// that fails to install is discarded so it isn't retried on every quit; the
// next check downloads it again. One that can't start because the system is
// shutting down is kept for the next boot.
func installStaged() error {
	if systemStopping() {
		return errors.New("not installing the update while the system shuts down")
	}
	path, err := verifyStagedPackage()
	if err != nil {
		logger.Error("Updater: unable to use staged update: %v", err)
		discardStagedFiles()
		return err
	}
	if err := install(path); err != nil {
		discardStagedFiles()
		return err
	}
	discardStagedFiles()
	return nil
}

// systemStopping reports whether systemd is shutting down or rebooting, when
// a package install could be killed partway through.
func systemStopping() bool {
	out, _ := exec.Command("systemctl", "is-system-running").Output()
	return strings.TrimSpace(string(out)) == "stopping"
}

// install installs a verified package with the system package manager, in a
// transient systemd unit so it keeps running when the package restarts this
// daemon. It waits for the install while the daemon lives.
func install(path string) error {
	cmd, err := installCommand(path)
	if err != nil {
		return err
	}
	if packageFormat() == formatArch {
		waitForPacmanLock()
	}
	unit := "pangolin-client-update-" + strconv.FormatInt(time.Now().Unix(), 10)
	args := append([]string{
		"--unit=" + unit, "--collect", "--wait", "--quiet",
		"--description=Pangolin client update",
		"--setenv=DEBIAN_FRONTEND=noninteractive",
		// zypper otherwise gives up at once if another zypper holds its lock.
		"--setenv=ZYPP_LOCK_TIMEOUT=" + strconv.Itoa(int(packageLockTimeout/time.Second)),
		"--",
	}, cmd...)
	logger.Info("Updater: running %s in %s", strings.Join(cmd, " "), unit)
	out, err := exec.Command("systemd-run", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("the package manager couldn't install the update (see journalctl -u %s): %v %s",
			unit, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// packageLockTimeout is how long an install waits for another package
// manager, such as unattended-upgrades or PackageKit, to finish.
const packageLockTimeout = 5 * time.Minute

const pacmanLock = "/var/lib/pacman/db.lck"

// waitForPacmanLock waits for another pacman to finish, since pacman fails at
// once if its database is locked.
func waitForPacmanLock() {
	deadline := time.Now().Add(packageLockTimeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(pacmanLock); os.IsNotExist(err) {
			return
		}
		time.Sleep(2 * time.Second)
	}
}

func installCommand(path string) ([]string, error) {
	have := func(name string) bool { _, err := exec.LookPath(name); return err == nil }
	switch packageFormat() {
	case formatDeb:
		if have("apt-get") {
			// apt resolves new dependencies; keep changed config files.
			return []string{"apt-get", "install", "-y",
				"-o", "Dpkg::Options::=--force-confold",
				"-o", "DPkg::Lock::Timeout=" + strconv.Itoa(int(packageLockTimeout/time.Second)),
				path}, nil
		}
		return []string{"dpkg", "-i", "--force-confold", path}, nil
	case formatRPM:
		// The package isn't signed for the package manager; its hash was
		// checked against the signed manifest instead.
		switch {
		case have("dnf"):
			return []string{"dnf", "install", "-y", "--nogpgcheck", path}, nil
		case have("zypper"):
			return []string{"zypper", "--non-interactive", "install", "--allow-unsigned-rpm", path}, nil
		case have("yum"):
			return []string{"yum", "install", "-y", "--nogpgcheck", path}, nil
		}
		return []string{"rpm", "-U", "--replacepkgs", path}, nil
	case formatArch:
		// pacman accepts the unsigned package under its default
		// LocalFileSigLevel; its hash was checked against the signed manifest.
		return []string{"pacman", "-U", "--noconfirm", path}, nil
	}
	return nil, errors.New("this copy of Pangolin can't install updates itself")
}

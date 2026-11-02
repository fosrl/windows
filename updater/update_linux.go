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
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/config"
	"github.com/fosrl/windows/version"
	"golang.org/x/crypto/blake2b"
)

// Linux updates use the same signed manifest scheme as Windows: a signify
// signed list of BLAKE2b-256 hashes at latestVersionPath. deb and rpm
// installs download the package, check its hash and install it through the
// system package manager. The install runs in its own transient systemd unit,
// because the package restarts this daemon partway through. Arch (AUR) and
// hand-installed builds are told how to update instead; see ManualUpdate.

const (
	maxManifestSize = 512 * 1024
	maxPackageSize  = 100 * 1024 * 1024
	httpTimeout     = 30 * time.Second
)

// RestartUIFile lists the UIDs whose UI was open when an update started, so
// the daemon can reopen them once the new version is running.
var RestartUIFile = filepath.Join(config.GetProgramDataDir(), "restart-ui-after-update")

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
	return findCandidate(files)
}

// ManualUpdate returns instructions, and a command where there is one, for
// installs that can't update in the app: the AUR package and builds
// installed by hand. Both are empty when the app can install updates.
func ManualUpdate() (instructions, command string) {
	switch packageFormat() {
	case formatDeb, formatRPM:
		return "", ""
	case formatArch:
		return "Pangolin was installed from the AUR. Update the pangolin-client-bin package with your AUR helper, for example:",
			"yay -Syu pangolin-client-bin"
	default:
		return "This copy of Pangolin wasn't installed from a package. Download the new version from:",
			"https://github.com/fosrl/windows/releases/latest"
	}
}

var updateInProgress atomic.Bool

// DownloadVerifyAndExecute downloads the update, checks it against the
// signed manifest and starts installing it. userToken is non-zero when the
// requesting user may install updates. Progress, and any error, arrive on
// the returned channel.
func DownloadVerifyAndExecute(userToken uintptr) chan DownloadProgress {
	progress := make(chan DownloadProgress, 128)
	progress <- DownloadProgress{Activity: "Initializing"}
	if userToken == 0 {
		progress <- DownloadProgress{Error: errors.New("installing updates needs an administrator: a member of the sudo or wheel group")}
		return progress
	}
	if instructions, command := ManualUpdate(); instructions != "" {
		progress <- DownloadProgress{Error: fmt.Errorf("%s %s", instructions, command)}
		return progress
	}
	if !updateInProgress.CompareAndSwap(false, true) {
		progress <- DownloadProgress{Error: errors.New("an update is already in progress")}
		return progress
	}
	go func() {
		defer updateInProgress.Store(false)
		if err := downloadAndInstall(progress); err != nil {
			logger.Error("Updater: %v", err)
			progress <- DownloadProgress{Error: err}
		}
	}()
	return progress
}

func downloadAndInstall(progress chan DownloadProgress) error {
	progress <- DownloadProgress{Activity: "Checking for update"}
	update, err := CheckForUpdate()
	if err != nil {
		return err
	}
	if update == nil {
		return errors.New("no update was found")
	}
	logger.Info("Updater: installing %s", update.name)

	location := update.downloadLocation
	if location == "" {
		return errors.New("download location not specified in manifest")
	}
	if !strings.HasPrefix(location, "https://") && !strings.HasPrefix(location, "http://") {
		location = updateServerURL(location)
	}
	if u, err := url.Parse(location); err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return fmt.Errorf("invalid download URL %q", location)
	}

	dir := downloadDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	RemoveDownloads()
	path := filepath.Join(dir, filepath.Base(update.name))
	if err := download(location, path, update, progress); err != nil {
		os.Remove(path)
		return err
	}

	progress <- DownloadProgress{Activity: "Installing update"}
	if err := install(path); err != nil {
		os.Remove(RestartUIFile)
		return err
	}
	// Usually the package restarts the daemon before this point.
	os.Remove(path)
	progress <- DownloadProgress{Complete: true}
	return nil
}

func download(location, path string, update *UpdateFound, progress chan DownloadProgress) error {
	dp := DownloadProgress{Activity: "Downloading update"}
	progress <- dp
	resp, err := get(context.Background(), location)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.ContentLength > 0 {
		dp.BytesTotal = uint64(resp.ContentLength)
		progress <- dp
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	hasher, _ := blake2b.New256(nil)
	watcher := &progressHashWatcher{dp: &dp, c: progress, hashState: hasher}
	_, copyErr := io.Copy(f, io.TeeReader(io.LimitReader(resp.Body, maxPackageSize), watcher))
	closeErr := f.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return err
	}
	if !hmac.Equal(hasher.Sum(nil), update.hash[:]) {
		return errors.New("the downloaded update has the wrong hash")
	}
	return nil
}

func downloadDir() string { return filepath.Join(config.GetProgramDataDir(), "updates") }

// RemoveDownloads deletes downloaded update packages. The daemon calls it at
// start, since the package usually restarts it before it can clean up.
func RemoveDownloads() {
	old, _ := filepath.Glob(filepath.Join(downloadDir(), "*"))
	for _, f := range old {
		os.Remove(f)
	}
}

// install installs a verified package with the system package manager, in a
// transient systemd unit so it keeps running when the package restarts this
// daemon. It waits for the install while the daemon lives.
func install(path string) error {
	cmd, err := installCommand(path)
	if err != nil {
		return err
	}
	unit := "pangolin-client-update-" + strconv.FormatInt(time.Now().Unix(), 10)
	args := append([]string{
		"--unit=" + unit, "--collect", "--wait", "--quiet",
		"--description=Pangolin client update",
		"--setenv=DEBIAN_FRONTEND=noninteractive",
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

func installCommand(path string) ([]string, error) {
	have := func(name string) bool { _, err := exec.LookPath(name); return err == nil }
	switch packageFormat() {
	case formatDeb:
		if have("apt-get") {
			// apt resolves new dependencies; keep changed config files.
			return []string{"apt-get", "install", "-y", "-o", "Dpkg::Options::=--force-confold", path}, nil
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
	}
	return nil, errors.New("this copy of Pangolin can't install updates itself")
}

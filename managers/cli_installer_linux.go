package managers

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/fosrl/newt/logger"
)

// The daemon installs the CLI binary from the CLI's GitHub release into the
// same place its install script (get-cli.sh) does.
var cliInstallDir = "/usr/local/bin"

const cliBinaryName = "pangolin"

// cliInstallAction is the polkit action in
// packaging/linux/net.pangolin.client.policy.
const cliInstallAction = "net.pangolin.client.install-cli"

// IsCLIInstalled reports whether the pangolin CLI is on PATH.
func IsCLIInstalled() bool {
	_, err := exec.LookPath(cliBinaryName)
	return err == nil
}

// authorizeCLIInstall lets administrators (see tokenForUID) install the CLI
// straight away. Anyone else is asked for an administrator's password by
// polkit, as Windows asks through UAC.
func authorizeCLIInstall(s *ManagerService) (userToken, error) {
	if s.elevatedToken != 0 {
		return s.elevatedToken, nil
	}
	uid, err := strconv.ParseUint(s.clientUserKey, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("unknown user %q", s.clientUserKey)
	}
	attachedUIsLock.Lock()
	ui := attachedUIs[uint32(uid)]
	attachedUIsLock.Unlock()
	if ui == nil || ui.subject == "" {
		return 0, errors.New("installing the CLI needs an administrator: a member of the sudo or wheel group")
	}
	if err := polkitAuthorize(cliInstallAction, ui.subject); err != nil {
		return 0, err
	}
	logger.Info("CLI install authorized by polkit for uid %d", uid)
	return 1, nil
}

// cliAssetName is the CLI release asset for this machine's architecture.
func cliAssetName() (string, error) {
	arch := runtime.GOARCH
	switch arch {
	case "amd64", "arm64", "riscv64":
	case "arm":
		arch = "arm32"
	default:
		return "", fmt.Errorf("the Pangolin CLI isn't available for %s", arch)
	}
	return "pangolin-cli_linux_" + arch, nil
}

// InstallCLI downloads the latest CLI release and installs it as
// /usr/local/bin/pangolin. userToken must be non-zero: see authorizeCLIInstall.
func InstallCLI(userToken uintptr) error {
	if userToken == 0 {
		return errors.New("installing the CLI needs an administrator")
	}
	asset, err := cliAssetName()
	if err != nil {
		return err
	}
	release, err := getLatestCLIRelease(cliInstallerRepo)
	if err != nil {
		return err
	}
	url, err := release.assetURL(asset)
	if err != nil {
		return err
	}
	logger.Info("Installing Pangolin CLI %s from %s", release.TagName, url)

	if err := os.MkdirAll(cliInstallDir, 0o755); err != nil {
		return err
	}
	// Download beside the target so the rename below replaces it in one step,
	// and the file gets the directory's SELinux label.
	tmp, err := os.CreateTemp(cliInstallDir, "."+cliBinaryName+"-*")
	if err != nil {
		return fmt.Errorf("failed to create download file: %w", err)
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)
	if err := os.Remove(tmpPath); err != nil {
		return err
	}
	if err := downloadCLIAsset(url, tmpPath); err != nil {
		return err
	}
	if err := checkELF(tmpPath); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return err
	}
	target := filepath.Join(cliInstallDir, cliBinaryName)
	if err := os.Rename(tmpPath, target); err != nil {
		return fmt.Errorf("failed to install %s: %w", target, err)
	}
	logger.Info("Installed Pangolin CLI %s at %s", release.TagName, target)
	return nil
}

// checkELF rejects a download that isn't a Linux executable, such as an
// error page.
func checkELF(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	magic := make([]byte, 4)
	if _, err := io.ReadFull(f, magic); err != nil || !bytes.Equal(magic, []byte("\x7fELF")) {
		return errors.New("the downloaded CLI isn't a Linux executable")
	}
	return nil
}

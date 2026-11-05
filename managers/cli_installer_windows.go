package managers

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

const cliInstallerAssetName = "pangolin-cli_windows_installer.msi"

func IsCLIInstalled() bool {
	programFiles := os.Getenv("ProgramFiles")
	if programFiles != "" {
		defaultInstallPath := filepath.Join(programFiles, "pangolin-cli", "pangolin.exe")
		if _, err := os.Stat(defaultInstallPath); err == nil {
			return true
		}
	}

	_, err := exec.LookPath("pangolin.exe")
	return err == nil
}

// authorizeCLIInstall returns the token the installer runs with: the
// connected user's elevated token, so only administrators can install.
func authorizeCLIInstall(s *ManagerService) (userToken, error) {
	if s.elevatedToken == 0 {
		return 0, errors.New("manager is not running with elevated privileges")
	}
	return s.elevatedToken, nil
}

func InstallCLI(userToken uintptr) error {
	release, err := getLatestCLIRelease(cliInstallerRepo)
	if err != nil {
		return err
	}

	installerURL, err := release.assetURL(cliInstallerAssetName)
	if err != nil {
		return err
	}

	tempDir, err := os.MkdirTemp("", "pangolin-cli-install-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}

	installerPath := filepath.Join(tempDir, cliInstallerAssetName)
	if err := downloadCLIAsset(installerURL, installerPath); err != nil {
		return err
	}

	if err := runCLIInstaller(installerPath, userToken); err != nil {
		return err
	}

	return nil
}

func runCLIInstaller(installerPath string, userToken uintptr) error {
	msiExecPath := filepath.Join(os.Getenv("WINDIR"), "System32", "msiexec.exe")
	if _, statErr := os.Stat(msiExecPath); statErr != nil {
		msiExecPath = "msiexec.exe"
	}

	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("failed to open null device: %w", err)
	}
	defer devNull.Close()

	attr := &os.ProcAttr{
		Sys: &syscall.SysProcAttr{
			Token: syscall.Token(userToken),
		},
		Files: []*os.File{devNull, devNull, devNull},
		Dir:   filepath.Dir(installerPath),
	}

	proc, err := os.StartProcess(msiExecPath, []string{msiExecPath, "/i", filepath.Base(installerPath)}, attr)
	if err != nil {
		return fmt.Errorf("failed to start msiexec: %w", err)
	}

	state, err := proc.Wait()
	if err != nil {
		return fmt.Errorf("failed waiting for msiexec: %w", err)
	}
	if !state.Success() {
		return &exec.ExitError{ProcessState: state}
	}

	return nil
}

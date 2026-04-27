//go:build windows

package managers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	cliInstallerRepo      = "fosrl/cli"
	cliInstallerAssetName = "pangolin-cli_windows_installer.msi"
)

type githubReleaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

type githubReleaseResponse struct {
	TagName string               `json:"tag_name"`
	HTMLURL string               `json:"html_url"`
	Assets  []githubReleaseAsset `json:"assets"`
}

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

func InstallCLI(userToken uintptr) error {
	release, err := getLatestCLIRelease(cliInstallerRepo)
	if err != nil {
		return err
	}

	installerURL, err := getCLIInstallerURL(release.Assets)
	if err != nil {
		return fmt.Errorf("%w (release: %s)", err, release.HTMLURL)
	}

	tempDir, err := os.MkdirTemp("", "pangolin-cli-install-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}

	installerPath := filepath.Join(tempDir, cliInstallerAssetName)
	if err := downloadCLIInstaller(installerURL, installerPath); err != nil {
		return err
	}

	if err := runCLIInstaller(installerPath, userToken); err != nil {
		return err
	}

	return nil
}

func getLatestCLIRelease(repo string) (*githubReleaseResponse, error) {
	repoParts := strings.Split(repo, "/")
	if len(repoParts) != 2 || repoParts[0] == "" || repoParts[1] == "" {
		return nil, fmt.Errorf("invalid repo %q, expected owner/name", repo)
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", repoParts[0], repoParts[1])
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create release request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "pangolin-windows-cli-installer")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to query GitHub releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub releases API returned %d: %s", resp.StatusCode, string(body))
	}

	var release githubReleaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to decode release response: %w", err)
	}
	return &release, nil
}

func getCLIInstallerURL(assets []githubReleaseAsset) (string, error) {
	for _, asset := range assets {
		if asset.Name == cliInstallerAssetName && asset.URL != "" {
			return asset.URL, nil
		}
	}
	return "", fmt.Errorf("latest release does not include %s", cliInstallerAssetName)
}

func downloadCLIInstaller(url, destPath string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create download request: %w", err)
	}
	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Set("User-Agent", "pangolin-windows-cli-installer")

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download installer: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("installer download failed with %d: %s", resp.StatusCode, string(body))
	}

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create installer file: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("failed to write installer file: %w", err)
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

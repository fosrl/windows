package managers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// The CLI install on each platform downloads an asset from the CLI's latest
// GitHub release.
const cliInstallerRepo = "fosrl/cli"

type githubReleaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

type githubReleaseResponse struct {
	TagName string               `json:"tag_name"`
	HTMLURL string               `json:"html_url"`
	Assets  []githubReleaseAsset `json:"assets"`
}

const cliInstallerUserAgent = "pangolin-client-cli-installer"

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
	req.Header.Set("User-Agent", cliInstallerUserAgent)

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

// assetURL returns the download URL of the release asset called name.
func (r *githubReleaseResponse) assetURL(name string) (string, error) {
	for _, asset := range r.Assets {
		if asset.Name == name && asset.URL != "" {
			return asset.URL, nil
		}
	}
	return "", fmt.Errorf("latest release does not include %s (release: %s)", name, r.HTMLURL)
}

// downloadCLIAsset saves url to destPath, which it creates.
func downloadCLIAsset(url, destPath string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create download request: %w", err)
	}
	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Set("User-Agent", cliInstallerUserAgent)

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download the CLI: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("CLI download failed with %d: %s", resp.StatusCode, string(body))
	}

	out, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("failed to create download file: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("failed to write download file: %w", err)
	}
	return out.Close()
}

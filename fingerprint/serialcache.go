//go:build windows

package fingerprint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/config"
)

// serialCacheFileName holds the last successfully-resolved hardware serial
// number under %LOCALAPPDATA%\Pangolin, so that a transient failure to read
// it from WMI/registry on a given run can fall back to a known-good value
// instead of destabilizing the platform fingerprint hash.
const serialCacheFileName = "fingerprint_cache.json"

type serialCacheFile struct {
	SerialNumber string `json:"serialNumber"`
}

var serialCacheMu sync.Mutex

func serialCachePath() string {
	appData := os.Getenv("LOCALAPPDATA")
	if appData == "" {
		appData = os.Getenv("APPDATA")
	}
	return filepath.Join(appData, config.AppName, serialCacheFileName)
}

func loadCachedSerial() string {
	serialCacheMu.Lock()
	defer serialCacheMu.Unlock()

	data, err := os.ReadFile(serialCachePath())
	if err != nil {
		return ""
	}

	var cached serialCacheFile
	if err := json.Unmarshal(data, &cached); err != nil {
		return ""
	}

	return strings.TrimSpace(cached.SerialNumber)
}

func saveCachedSerial(serial string) {
	serial = strings.TrimSpace(serial)
	if serial == "" {
		return
	}

	serialCacheMu.Lock()
	defer serialCacheMu.Unlock()

	path := serialCachePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		logger.Debug("Fingerprint: failed to create serial cache directory: %v", err)
		return
	}

	data, err := json.Marshal(serialCacheFile{SerialNumber: serial})
	if err != nil {
		return
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		logger.Debug("Fingerprint: failed to write serial cache: %v", err)
	}
}

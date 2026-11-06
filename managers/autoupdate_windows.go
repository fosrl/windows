package managers

import "github.com/fosrl/windows-linux/config"

// sessionAutoDownloadUpdates reports whether the user logged into session has
// automatic update downloads on. If their LOCALAPPDATA can't be found, only the
// machine config counts.
func sessionAutoDownloadUpdates(session uint32) (bool, error) {
	localAppData, err := localAppDataForSession(session)
	return config.AutoDownloadUpdatesEnabled(localAppData), err
}

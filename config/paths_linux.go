package config

import (
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

// linuxDirName names this app's directories under XDG, /etc and /var. The
// Pangolin CLI already uses "pangolin" for ~/.config, /etc and /var/log, with
// files of the same names (accounts.json), so the client keeps apart.
const linuxDirName = "pangolin-client"

// UserDataDir is the per-user directory for config, accounts and caches:
// $XDG_CONFIG_HOME/pangolin-client, normally ~/.config/pangolin-client.
func UserDataDir() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, linuxDirName)
	}
	return filepath.Join(homeDir(), ".config", linuxDirName)
}

// GetProgramDataDir returns the daemon's state directory, /var/lib/pangolin-client.
func GetProgramDataDir() string {
	return filepath.Join("/var/lib", linuxDirName)
}

// GetLogDir returns the log directory: /var/log/pangolin-client for the root
// daemon, and $XDG_STATE_HOME/pangolin-client/logs (normally
// ~/.local/state/pangolin-client/logs) for the UI, which runs as the user and
// cannot write under /var/log.
func GetLogDir() string {
	if os.Geteuid() == 0 {
		return filepath.Join("/var/log", linuxDirName)
	}
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" || !filepath.IsAbs(state) {
		state = filepath.Join(homeDir(), ".local", "state")
	}
	return filepath.Join(state, linuxDirName, "logs")
}

// systemConfigPath is the machine-wide config file, /etc/pangolin-client/pangolin.json.
func systemConfigPath() string {
	return filepath.Join("/etc", linuxDirName, ConfigFileName)
}

// isLaptop reports whether the kernel exposes a system battery.
func isLaptop() bool {
	entries, err := os.ReadDir("/sys/class/power_supply")
	if err != nil {
		return false
	}
	for _, e := range entries {
		base := filepath.Join("/sys/class/power_supply", e.Name())
		typ, err := os.ReadFile(filepath.Join(base, "type"))
		if err != nil || strings.TrimSpace(string(typ)) != "Battery" {
			continue
		}
		// Peripheral batteries (mice, headsets) report scope "Device".
		if scope, err := os.ReadFile(filepath.Join(base, "scope")); err == nil &&
			strings.TrimSpace(string(scope)) == "Device" {
			continue
		}
		return true
	}
	return false
}

// homeDir returns $HOME, or the home directory from the user database when
// $HOME is unset (as it can be for services).
func homeDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	if u, err := user.Current(); err == nil && u.HomeDir != "" {
		return u.HomeDir
	}
	return "/"
}

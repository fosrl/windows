//go:build windows

package ui

import (
	"net"
	"strconv"
	"strings"

	"github.com/fosrl/windows/config"
)

const (
	minMTU = 576
	maxMTU = 9000
)

// Settings is the Preferences tab form.
type Settings struct {
	OpenAtLogin  bool   `json:"openAtLogin"`
	AutoConnect  bool   `json:"autoConnect"`
	DNSOverride  bool   `json:"dnsOverride"`
	DNSTunnel    bool   `json:"dnsTunnel"`
	PrimaryDNS   string `json:"primaryDns"`
	SecondaryDNS string `json:"secondaryDns"`
	MTU          string `json:"mtu"`
	// Disabled is set when an administrator has turned off user settings.
	Disabled bool `json:"disabled"`
}

// PrefsOpened is sent to the preferences window each time it is shown.
type PrefsOpened struct {
	Tab      int      `json:"tab"`
	Settings Settings `json:"settings"`
}

func currentSettings() Settings {
	if configManager == nil {
		return Settings{MTU: "1280"}
	}
	cm := configManager
	return Settings{
		OpenAtLogin:  cm.GetOpenUIAtLogin() || cm.GetAutoConnectAtLogin(),
		AutoConnect:  cm.GetAutoConnectAtLogin(),
		DNSOverride:  cm.GetDNSOverride(),
		DNSTunnel:    cm.GetDNSTunnel(),
		PrimaryDNS:   cm.GetPrimaryDNS(),
		SecondaryDNS: cm.GetSecondaryDNS(),
		MTU:          strconv.Itoa(cm.GetMTU()),
		Disabled:     cm.GetUserSettingsDisabled(),
	}
}

func isValidIPAddress(ip string) bool {
	return net.ParseIP(ip) != nil
}

// saveSettings validates and saves the form, reporting problems in native
// dialogs. It returns the values the form should show afterwards: an invalid
// field is reset to its saved value.
func saveSettings(form Settings) Settings {
	owner := preferencesWindowOrNil()
	if configManager == nil || configManager.GetUserSettingsDisabled() {
		return currentSettings()
	}

	openAtLogin := form.OpenAtLogin || form.AutoConnect
	primaryDNS := strings.TrimSpace(form.PrimaryDNS)
	secondaryDNS := strings.TrimSpace(form.SecondaryDNS)
	mtuText := strings.TrimSpace(form.MTU)

	mtu, err := strconv.Atoi(mtuText)
	if mtuText == "" || err != nil || mtu < minMTU || mtu > maxMTU {
		form.MTU = strconv.Itoa(configManager.GetMTU())
		showWarning(owner, "Invalid Input", "MTU must be a whole number between 576 and 9000.")
		return form
	}
	if primaryDNS != "" && !isValidIPAddress(primaryDNS) {
		form.PrimaryDNS = configManager.GetPrimaryDNS()
		showWarning(owner, "Invalid Input", "Primary DNS Server must be a valid IP address.")
		return form
	}
	if secondaryDNS != "" && !isValidIPAddress(secondaryDNS) {
		form.SecondaryDNS = configManager.GetSecondaryDNS()
		showWarning(owner, "Invalid Input", "Secondary DNS Server must be a valid IP address.")
		return form
	}

	// Start from the current config so fields not on this form (e.g.
	// defaultServerURL, userSettingsDisabled) are preserved.
	cfg := configManager.GetConfigCopy()
	if cfg == nil {
		cfg = &config.Config{}
	}
	dnsOverride := form.DNSOverride
	dnsTunnel := form.DNSTunnel
	autoConnect := form.AutoConnect
	cfg.DNSOverride = &dnsOverride
	cfg.DNSTunnel = &dnsTunnel
	cfg.AutoConnectAtLogin = &autoConnect
	cfg.OpenUIAtLogin = &openAtLogin
	cfg.MTU = &mtu
	if primaryDNS != "" {
		cfg.PrimaryDNS = &primaryDNS
	} else {
		cfg.PrimaryDNS = nil
	}
	if secondaryDNS != "" {
		cfg.SecondaryDNS = &secondaryDNS
	} else {
		cfg.SecondaryDNS = nil
	}

	if !configManager.Save(cfg) {
		showError(owner, "Save Failed", "Failed to save settings. Please try again.")
		return form
	}
	notify("Settings Saved", "Settings have been saved successfully.")
	return currentSettings()
}

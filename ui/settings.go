//go:build windows

package ui

import (
	"fmt"
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
	OpenAtLogin             bool   `json:"openAtLogin"`
	AutoConnect             bool   `json:"autoConnect"`
	DNSOverride             bool   `json:"dnsOverride"`
	DNSTunnel               bool   `json:"dnsTunnel"`
	PrimaryDNS              string `json:"primaryDns"`
	SecondaryDNS            string `json:"secondaryDns"`
	MTU                     string `json:"mtu"`
	ExitNodeTakesPrecedence bool   `json:"exitNodeTakesPrecedence"`
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
		OpenAtLogin:             cm.GetOpenUIAtLogin() || cm.GetAutoConnectAtLogin(),
		AutoConnect:             cm.GetAutoConnectAtLogin(),
		DNSOverride:             cm.GetDNSOverride(),
		DNSTunnel:               cm.GetDNSTunnel(),
		PrimaryDNS:              cm.GetPrimaryDNS(),
		SecondaryDNS:            cm.GetSecondaryDNS(),
		MTU:                     strconv.Itoa(cm.GetMTU()),
		ExitNodeTakesPrecedence: cm.GetExitNodeTakesPrecedence(),
		Disabled:                cm.GetUserSettingsDisabled(),
	}
}

func isValidIPAddress(ip string) bool {
	return net.ParseIP(ip) != nil
}

// SettingsResult is the outcome of applying a settings change. On a
// validation error nothing is saved and Field names the invalid field
// ("mtu", "primaryDns" or "secondaryDns") so the sheet can show Error inline.
type SettingsResult struct {
	Settings Settings `json:"settings"`
	Field    string   `json:"field"`
	Error    string   `json:"error"`
}

// applySettings validates and saves the form. Settings apply as soon as they
// change, like the macOS app, so there is no Save button or confirmation.
func applySettings(form Settings) SettingsResult {
	if configManager == nil || configManager.GetUserSettingsDisabled() {
		return SettingsResult{Settings: currentSettings()}
	}

	openAtLogin := form.OpenAtLogin || form.AutoConnect
	primaryDNS := strings.TrimSpace(form.PrimaryDNS)
	secondaryDNS := strings.TrimSpace(form.SecondaryDNS)
	mtuText := strings.TrimSpace(form.MTU)

	invalid := func(field, message string) SettingsResult {
		return SettingsResult{Settings: currentSettings(), Field: field, Error: message}
	}
	mtu, err := strconv.Atoi(mtuText)
	if mtuText == "" || err != nil || mtu < minMTU || mtu > maxMTU {
		return invalid("mtu", fmt.Sprintf("Enter an integer between %d and %d (e.g., 1280)", minMTU, maxMTU))
	}
	if primaryDNS != "" && !isValidIPAddress(primaryDNS) {
		return invalid("primaryDns", "Enter an IP address for the DNS server (e.g., 1.1.1.1)")
	}
	if secondaryDNS != "" && !isValidIPAddress(secondaryDNS) {
		return invalid("secondaryDns", "Enter an IP address for the DNS server (e.g., 1.1.1.1)")
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
	exitNodeTakesPrecedence := form.ExitNodeTakesPrecedence
	cfg.DNSOverride = &dnsOverride
	cfg.DNSTunnel = &dnsTunnel
	cfg.AutoConnectAtLogin = &autoConnect
	cfg.OpenUIAtLogin = &openAtLogin
	cfg.MTU = &mtu
	cfg.ExitNodeTakesPrecedence = &exitNodeTakesPrecedence
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
		showError(preferencesWindowOrNil(), "Save Failed", "Failed to save settings. Please try again.")
	}
	return SettingsResult{Settings: currentSettings()}
}

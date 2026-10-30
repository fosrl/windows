//go:build windows

package ui

import (
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/config"
)

const (
	minMTU = 576
	maxMTU = 9000
)

// settingsScopeOrg is the Update scope that saves a change to the current org's
// section of pangolin.json; anything else saves to the global settings.
const settingsScopeOrg = "org"

// Settings is the Preferences tab form. The tunnel settings are what the
// current org actually uses, so they aren't always the global values.
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
	// OrgName is the current org, empty if there is none.
	OrgName string `json:"orgName"`
	// OrgMode is set when the current org has its own settings.
	OrgMode bool `json:"orgMode"`
	// Managed lists the fields above that the org's server sets, by JSON name.
	Managed []string `json:"managed"`
}

// PrefsOpened is sent to the preferences window each time it is shown.
type PrefsOpened struct {
	Tab      int      `json:"tab"`
	Settings Settings `json:"settings"`
	// Login, when set, opens the add-account sheet on the Accounts tab.
	Login *LoginRequest `json:"login"`
}

// currentOrgInfo returns the current org's ID and name, or "" for both.
func currentOrgInfo() (string, string) {
	if authManager == nil {
		return "", ""
	}
	if org := authManager.CurrentOrg(); org != nil {
		return org.Id, org.Name
	}
	return "", ""
}

// cachedServerSettings returns the server settings last fetched for orgID.
func cachedServerSettings(orgID string) *config.ClientSettings {
	if accountManager == nil || orgID == "" {
		return nil
	}
	active, err := accountManager.ActiveAccount()
	if err != nil {
		return nil
	}
	return accountManager.GetServerSettings(active.UserID, orgID)
}

// fetchServerSettings fetches the settings the org's server manages and records
// them on the account. If the server can't be reached the last fetched settings
// keep applying.
func fetchServerSettings(orgID string) *config.ClientSettings {
	if apiClient == nil || accountManager == nil {
		return nil
	}
	settings, err := apiClient.GetMyClientSettings(orgID)
	if err != nil {
		logger.Warn("Could not fetch client settings from the server: %v", err)
		return cachedServerSettings(orgID)
	}
	active, err := accountManager.ActiveAccount()
	if err != nil {
		return settings
	}
	if err := accountManager.SetServerSettings(active.UserID, orgID, settings); err != nil {
		logger.Warn("Could not save client settings: %v", err)
	}
	return settings
}

// refreshPreferencesServerSettings re-fetches the server's settings when
// Preferences opens and shows the result.
func refreshPreferencesServerSettings() {
	orgID, _ := currentOrgInfo()
	if orgID == "" {
		return
	}
	fetchServerSettings(orgID)
	prefsMu.Lock()
	defer prefsMu.Unlock()
	if prefsWindow != nil {
		prefsWindow.EmitEvent("prefs:settings", currentSettings())
	}
	publish()
}

func managedFields(server *config.ClientSettings) []string {
	managed := []string{}
	if server == nil {
		return managed
	}
	if server.OverrideDNS != nil {
		managed = append(managed, "dnsOverride")
	}
	if server.TunnelDNS != nil {
		managed = append(managed, "dnsTunnel")
	}
	if server.ManagesUpstreamDNS() {
		managed = append(managed, "primaryDns", "secondaryDns")
	}
	if server.MTU != nil {
		managed = append(managed, "mtu")
	}
	if server.ExitNodeTakesPrecedence != nil {
		managed = append(managed, "exitNodeTakesPrecedence")
	}
	return managed
}

func currentSettings() Settings {
	if configManager == nil {
		return Settings{MTU: "1280", Managed: []string{}}
	}
	cm := configManager
	orgID, orgName := currentOrgInfo()
	server := cachedServerSettings(orgID)
	effective := cm.EffectiveSettings(orgID, server)
	return Settings{
		OpenAtLogin:             cm.GetOpenUIAtLogin() || cm.GetAutoConnectAtLogin(),
		AutoConnect:             cm.GetAutoConnectAtLogin(),
		DNSOverride:             effective.DNSOverride,
		DNSTunnel:               effective.DNSTunnel,
		PrimaryDNS:              effective.PrimaryDNS,
		SecondaryDNS:            effective.SecondaryDNS,
		MTU:                     strconv.Itoa(effective.MTU),
		ExitNodeTakesPrecedence: effective.ExitNodeTakesPrecedence,
		Disabled:                cm.GetUserSettingsDisabled(),
		OrgName:                 orgName,
		OrgMode:                 !cm.GetOrgConfig(orgID).IsEmpty(),
		Managed:                 managedFields(server),
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

// tunnelChanges returns what changed between the shown and submitted forms. Only
// real changes are saved: the form shows the org's effective values, which must
// not be copied into the global settings. Managed settings can't be changed.
func tunnelChanges(prev, form Settings, mtu int, primaryDNS, secondaryDNS string) config.OrgConfig {
	managed := func(field string) bool { return slices.Contains(prev.Managed, field) }

	var changes config.OrgConfig
	if form.DNSOverride != prev.DNSOverride && !managed("dnsOverride") {
		changes.DNSOverride = &form.DNSOverride
	}
	if form.DNSTunnel != prev.DNSTunnel && !managed("dnsTunnel") {
		changes.DNSTunnel = &form.DNSTunnel
	}
	if primaryDNS != prev.PrimaryDNS && !managed("primaryDns") {
		changes.PrimaryDNS = &primaryDNS
	}
	if secondaryDNS != prev.SecondaryDNS && !managed("secondaryDns") {
		changes.SecondaryDNS = &secondaryDNS
	}
	if strconv.Itoa(mtu) != prev.MTU && !managed("mtu") {
		changes.MTU = &mtu
	}
	if form.ExitNodeTakesPrecedence != prev.ExitNodeTakesPrecedence && !managed("exitNodeTakesPrecedence") {
		changes.ExitNodeTakesPrecedence = &form.ExitNodeTakesPrecedence
	}
	return changes
}

// applyGlobalChanges writes tunnel changes to the global settings.
func applyGlobalChanges(cfg *config.Config, changes config.OrgConfig) {
	emptyToNil := func(value *string) *string {
		if *value == "" {
			return nil
		}
		return value
	}

	if changes.DNSOverride != nil {
		cfg.DNSOverride = changes.DNSOverride
	}
	if changes.DNSTunnel != nil {
		cfg.DNSTunnel = changes.DNSTunnel
	}
	if changes.PrimaryDNS != nil {
		cfg.PrimaryDNS = emptyToNil(changes.PrimaryDNS)
	}
	if changes.SecondaryDNS != nil {
		cfg.SecondaryDNS = emptyToNil(changes.SecondaryDNS)
	}
	if changes.MTU != nil {
		cfg.MTU = changes.MTU
	}
	if changes.ExitNodeTakesPrecedence != nil {
		cfg.ExitNodeTakesPrecedence = changes.ExitNodeTakesPrecedence
	}
}

// applySettings validates and saves the form. Settings apply as soon as they
// change, like the macOS app; the frontend asks for the scope of a change. Start
// at login and connect at start are application settings, so they're always
// global.
func applySettings(form Settings, scope string) SettingsResult {
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

	changes := tunnelChanges(currentSettings(), form, mtu, primaryDNS, secondaryDNS)
	orgID, _ := currentOrgInfo()
	saveToOrg := scope == settingsScopeOrg && orgID != ""

	// Start from the current config so fields not on this form (e.g.
	// defaultServerURL, userSettingsDisabled) are preserved.
	cfg := configManager.GetConfigCopy()
	if cfg == nil {
		cfg = &config.Config{}
	}
	autoConnect := form.AutoConnect
	cfg.AutoConnectAtLogin = &autoConnect
	cfg.OpenUIAtLogin = &openAtLogin
	if !saveToOrg {
		applyGlobalChanges(cfg, changes)
	}

	saved := configManager.Save(cfg)
	if saved && saveToOrg && !changes.IsEmpty() {
		saved = configManager.UpdateOrgConfig(orgID, changes)
	}
	if !saved {
		showError(preferencesWindowOrNil(), "Save Failed", "Failed to save settings. Please try again.")
	}
	return SettingsResult{Settings: currentSettings()}
}

// resetOrgSettings makes the current org use the global settings again.
func resetOrgSettings() Settings {
	if configManager == nil || configManager.GetUserSettingsDisabled() {
		return currentSettings()
	}
	if orgID, _ := currentOrgInfo(); orgID != "" && !configManager.ResetOrgConfig(orgID) {
		showError(preferencesWindowOrNil(), "Save Failed", "Failed to save settings. Please try again.")
	}
	return currentSettings()
}

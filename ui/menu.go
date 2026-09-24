package ui

import (
	"fmt"
	"strings"
)

// Menu item kinds rendered by the tray popup.
const (
	MenuKindItem      = "item"
	MenuKindSeparator = "separator"
	MenuKindHeader    = "header"
	MenuKindSubmenu   = "submenu"
)

// Menu item IDs dispatched back from the tray popup to invokeMenuItem.
const (
	menuIDUpdate       = "update"
	menuIDReAuth       = "reauth"
	menuIDConnect      = "connect"
	menuIDLogin        = "login"
	menuIDPreferences  = "preferences"
	menuIDQuit         = "quit"
	menuIDHowItWorks   = "more.howItWorks"
	menuIDDocs         = "more.docs"
	menuIDTerms        = "more.terms"
	menuIDPrivacy      = "more.privacy"
	menuIDCheckUpdates = "more.checkUpdates"
	menuIDInstallCLI   = "more.installCLI"
	menuIDAddAccount   = "account.add"
	menuIDLogout       = "account.logout"
	menuPrefixAccount  = "account:"
	menuPrefixOrg      = "org:"
)

// MenuItem is one row of the tray popup. Only visible items are included.
type MenuItem struct {
	ID      string     `json:"id"`
	Kind    string     `json:"kind"`
	Label   string     `json:"label"`
	Enabled bool       `json:"enabled"`
	Checked bool       `json:"checked"`
	Items   []MenuItem `json:"items,omitempty"`
}

// MenuState is the full tray popup contents.
type MenuState struct {
	Items []MenuItem `json:"items"`
}

// tunnelPhase groups tunnel states the way the tray menu cares about them.
type tunnelPhase int

const (
	phaseStopped tunnelPhase = iota
	phaseStarting
	phaseRunning
	phaseStopping
	phaseOther
)

type menuAccount struct {
	UserID   string
	Display  string
	Email    string
	Hostname string
}

type menuOrg struct {
	ID   string
	Name string
}

type menuServerInfo struct {
	Build                  string
	EnterpriseLicenseValid bool
	EnterpriseLicenseType  string
	SupporterStatusValid   bool
}

// menuInputs is a plain snapshot of everything the tray menu depends on.
type menuInputs struct {
	Initializing         bool
	Authenticated        bool
	LoggedOut            bool
	ServerDown           bool
	SessionExpired       bool
	DeviceAuthInProgress bool
	ErrorMessage         string
	ServerInfo           *menuServerInfo

	TunnelPhase     tunnelPhase
	TunnelStateText string
	Connected       bool

	Accounts        []menuAccount
	ActiveAccountID string
	// LoginLabel is only used for the "login" item, which shows when there are no accounts.
	LoginLabel string

	Orgs         []menuOrg
	CurrentOrgID string
	// CurrentOrgName is empty when no organization is selected.
	CurrentOrgName string

	HasUpdate          bool
	CLIInstalled       bool
	CLIInstalling      bool
	CheckUpdateVisible bool

	Version string
	Year    int
}

func item(id, label string, enabled bool) MenuItem {
	return MenuItem{ID: id, Kind: MenuKindItem, Label: label, Enabled: enabled}
}

func separator() MenuItem {
	return MenuItem{Kind: MenuKindSeparator}
}

func header(label string) MenuItem {
	return MenuItem{Kind: MenuKindHeader, Label: label}
}

func watermarkText(in menuInputs) string {
	si := in.ServerInfo
	if si == nil || !in.Authenticated || in.Initializing || in.SessionExpired {
		return ""
	}
	if si.Build == "enterprise" && strings.ToLower(si.EnterpriseLicenseType) == "personal" {
		return "Licensed for personal use only."
	}
	if si.Build == "enterprise" && !si.EnterpriseLicenseValid {
		return "This server is unlicensed."
	}
	if si.Build == "oss" && !si.SupporterStatusValid {
		return "Community Edition. Consider supporting."
	}
	return ""
}

// buildMenuState mirrors the visibility and text rules of the old walk tray menu.
func buildMenuState(in menuInputs) MenuState {
	var items []MenuItem

	if in.HasUpdate {
		items = append(items, item(menuIDUpdate, "Pangolin Update Available", true))
	}
	if in.Initializing {
		items = append(items, item("", "Loading...", false))
	}
	if in.Authenticated && in.ServerDown && !in.Initializing {
		items = append(items, item("", "The server appears to be down.", false))
	}
	if in.ErrorMessage != "" && !in.ServerDown && !in.SessionExpired && in.Authenticated && !in.Initializing {
		items = append(items, item("", in.ErrorMessage, false))
	}

	// Show the auth section even when the server is down so accounts can still be switched.
	showAuthSection := in.Authenticated && (!in.LoggedOut || in.SessionExpired) && !in.Initializing
	if showAuthSection {
		if in.SessionExpired {
			items = append(items, item("", "Status: Account Locked", false))
			items = append(items, item(menuIDReAuth, "Log In", !in.DeviceAuthInProgress))
		} else {
			items = append(items, item("", "Status: "+in.TunnelStateText, false))
			connect := item(menuIDConnect, "Connect", true)
			switch in.TunnelPhase {
			case phaseStopping:
				connect.Label = "Disconnecting..."
				connect.Enabled = false
			case phaseStopped:
			default:
				connect.Label = "Disconnect"
			}
			connect.Checked = in.TunnelPhase == phaseRunning || in.Connected
			items = append(items, connect)
		}
	}

	items = append(items, separator())

	if len(in.Accounts) > 0 {
		items = append(items, header("Account"))
		items = append(items, accountSubmenu(in))
	}
	if showAuthSection {
		items = append(items, header("Organization"))
		items = append(items, orgSubmenu(in))
	}

	items = append(items, separator())

	if len(in.Accounts) == 0 {
		items = append(items, item(menuIDLogin, in.LoginLabel, true))
	}

	items = append(items, separator())
	items = append(items, item(menuIDPreferences, "Preferences", true))
	items = append(items, moreSubmenu(in))
	items = append(items, separator())

	if wm := watermarkText(in); wm != "" {
		items = append(items, item("", wm, false))
	}

	items = append(items, separator())
	items = append(items, item(menuIDQuit, "Quit", true))

	return MenuState{Items: collapseSeparators(items)}
}

// transitional reports whether account and org switching must be blocked.
func (p tunnelPhase) transitional() bool {
	return p == phaseStarting || p == phaseStopping
}

func accountSubmenu(in menuInputs) MenuItem {
	sub := []MenuItem{header("Available Accounts"), separator()}

	// Show the hostname only when more than one account shares an email.
	emailCounts := map[string]int{}
	for _, a := range in.Accounts {
		emailCounts[a.Email]++
	}

	label := "Select Account"
	for _, a := range in.Accounts {
		text := a.Display
		if emailCounts[a.Email] > 1 {
			text = fmt.Sprintf("%s (%s)", a.Display, a.Hostname)
		}
		it := item(menuPrefixAccount+a.UserID, text, !in.TunnelPhase.transitional())
		it.Checked = a.UserID == in.ActiveAccountID
		sub = append(sub, it)
		if it.Checked {
			label = a.Display
		}
	}

	sub = append(sub, separator(), item(menuIDAddAccount, "Add Account", true))
	if in.ActiveAccountID != "" {
		sub = append(sub, item(menuIDLogout, "Logout", true))
	}

	return MenuItem{ID: "accounts", Kind: MenuKindSubmenu, Label: label, Enabled: true, Items: sub}
}

func orgSubmenu(in menuInputs) MenuItem {
	count := fmt.Sprintf("%d Organization", len(in.Orgs))
	if len(in.Orgs) != 1 {
		count += "s"
	}
	sub := []MenuItem{header(count), separator()}
	if len(in.Orgs) == 0 {
		sub = append(sub, item("", "No organizations", false))
	}
	for _, o := range in.Orgs {
		it := item(menuPrefixOrg+o.ID, o.Name, !in.TunnelPhase.transitional())
		it.Checked = in.CurrentOrgID != "" && o.ID == in.CurrentOrgID
		sub = append(sub, it)
	}

	label := "Organizations"
	if in.CurrentOrgName != "" {
		label = in.CurrentOrgName
	}
	return MenuItem{ID: "orgs", Kind: MenuKindSubmenu, Label: label, Enabled: true, Items: sub}
}

func moreSubmenu(in menuInputs) MenuItem {
	sub := []MenuItem{
		header("Support"),
		item(menuIDHowItWorks, "How Pangolin Works", true),
		item(menuIDDocs, "Documentation", true),
		separator(),
		item("", fmt.Sprintf("© %d Fossorial, Inc.", in.Year), false),
		item(menuIDTerms, "Terms of Service", true),
		item(menuIDPrivacy, "Privacy Policy", true),
		separator(),
		item("", "Version: "+in.Version, false),
	}
	if in.CheckUpdateVisible {
		sub = append(sub, item(menuIDCheckUpdates, "Check for Updates", true))
	}
	if !in.CLIInstalled {
		label := "Install Pangolin CLI"
		if in.CLIInstalling {
			label = "Installing CLI…"
		}
		sub = append(sub, item(menuIDInstallCLI, label, !in.CLIInstalling))
	}
	return MenuItem{ID: "more", Kind: MenuKindSubmenu, Label: "More", Enabled: true, Items: sub}
}

// collapseSeparators drops leading, trailing and repeated separators, which the
// native menu used to show when the items between them were hidden.
func collapseSeparators(items []MenuItem) []MenuItem {
	out := make([]MenuItem, 0, len(items))
	for _, it := range items {
		if it.Kind == MenuKindSeparator {
			if len(out) == 0 || out[len(out)-1].Kind == MenuKindSeparator {
				continue
			}
		}
		out = append(out, it)
	}
	for len(out) > 0 && out[len(out)-1].Kind == MenuKindSeparator {
		out = out[:len(out)-1]
	}
	return out
}

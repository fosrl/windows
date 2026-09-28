package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Menu item kinds rendered by the tray popup. They mirror the row types of the
// macOS menu bar panel (MenuBarComponents.swift).
const (
	MenuKindItem      = "item"
	MenuKindSeparator = "separator"
	MenuKindHeader    = "header"
	MenuKindSubmenu   = "submenu"
	// MenuKindToggle is a bold title with a trailing switch; Checked is the switch value.
	MenuKindToggle = "toggle"
	// MenuKindLabel is non-interactive text that wraps, with an optional Icon.
	MenuKindLabel = "label"
	// MenuKindStatus is the tunnel status line: a Dot, secondary text and a
	// spinner while Loading. It opens a submenu when it has Items.
	MenuKindStatus = "status"
	// MenuKindDetail is a read-only label and Value on one row, with an optional Dot.
	MenuKindDetail = "detail"
)

// Status dot colors used by the tray popup.
const (
	menuDotGreen  = "green"
	menuDotOrange = "orange"
	menuDotYellow = "yellow"
	menuDotGray   = "gray"
)

// Label icons.
const (
	menuIconWarning = "warning"
	menuIconLock    = "lock"
)

// Menu item IDs dispatched back from the tray popup to invokeMenuItem.
const (
	menuIDUpdate         = "update"
	menuIDReAuth         = "reauth"
	menuIDConnect        = "connect"
	menuIDLogin          = "login"
	menuIDPreferences    = "preferences"
	menuIDQuit           = "quit"
	menuIDOpenStatus     = "sites.openStatus"
	menuIDHowItWorks     = "more.howItWorks"
	menuIDDocs           = "more.docs"
	menuIDTerms          = "more.terms"
	menuIDPrivacy        = "more.privacy"
	menuIDCheckUpdates   = "more.checkUpdates"
	menuIDInstallCLI     = "more.installCLI"
	menuIDAddAccount     = "account.add"
	menuIDLogout         = "account.logout"
	menuIDManageAccounts = "account.manage"
	menuIDOpenSetup      = "setup"
	menuPrefixAccount    = "account:"
	menuPrefixOrg        = "org:"
	menuIDExitNodeNone   = "exitnode.none"
	// menuPrefixExitNode is followed by the gateway site resource's numeric ID.
	menuPrefixExitNode = "exitnode:"
	// menuIDSites is the tunnel status row, which opens the sites submenu while connected.
	menuIDSites = "sites"
	// menuPrefixSite is followed by the site ID; these rows only open a detail submenu.
	menuPrefixSite = "site:"
)

// MenuItem is one row of the tray popup. Only visible items are included.
type MenuItem struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	Enabled bool   `json:"enabled"`
	Checked bool   `json:"checked"`
	// Checkable reserves the leading check column, so checked and unchecked rows line up.
	Checkable bool `json:"checkable"`
	// Loading shows a small spinner: in the check column of a checkable row,
	// otherwise trailing the label.
	Loading bool `json:"loading"`
	// Dot is a status dot color shown before the label.
	Dot string `json:"dot"`
	// Icon is shown before a label's text.
	Icon string `json:"icon"`
	// Inset indents a header to line up with the text of checkable rows.
	Inset bool `json:"inset"`
	// Value is the right-hand text of a detail row.
	Value string     `json:"value"`
	Items []MenuItem `json:"items,omitempty"`
}

// MenuState is the full tray popup contents.
type MenuState struct {
	Items []MenuItem `json:"items"`
	// Loading replaces the whole panel with a spinner, as the macOS menu does
	// while starting up and while switching accounts or organizations.
	Loading bool `json:"loading"`
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

// menuExitNode is a gateway site resource that can be selected as the exit node.
type menuExitNode struct {
	ID   int
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
	// Onboarding is true until setup (welcome and privacy) is finished.
	Onboarding           bool
	Initializing         bool
	Authenticated        bool
	LoggedOut            bool
	ServerDown           bool
	SessionExpired       bool
	DeviceAuthInProgress bool
	ErrorMessage         string
	ServerInfo           *menuServerInfo

	// TunnelPhase is the phase to show; drops are debounced before they reach it.
	TunnelPhase tunnelPhase
	Connected   bool
	// PendingTunnelOn is set from a click on the connect switch until the
	// tunnel reaches the requested state.
	PendingTunnelOn *bool
	// ConnectionError is the last connect failure, shown under the switch.
	ConnectionError string

	// Sites are the rows of the sites submenu. SitesLoaded is false until the
	// first OLM status arrives.
	Sites       []StatusSite
	SitesLoaded bool
	Now         time.Time

	Accounts        []menuAccount
	ActiveAccountID string
	// CurrentUserDisplay is the signed-in user's display name, when known.
	CurrentUserDisplay string

	Orgs         []menuOrg
	CurrentOrgID string
	// CurrentOrgName is empty when no organization is selected.
	CurrentOrgName string

	// ExitNodes are the gateway resources available in the current org. The
	// exit node section is hidden when there are none.
	ExitNodes []menuExitNode
	// ActiveExitNodeID is the ID of the selected exit node, or 0 for none.
	ActiveExitNodeID int

	// Account and organization switches and logouts in progress.
	SwitchingAccountID string
	SwitchingOrgID     string
	LoggingOut         bool

	HasUpdate          bool
	CLIInstalled       bool
	CLIInstalling      bool
	CheckUpdateVisible bool

	Version string
	Year    int
}

// switching reports whether an account switch, org switch or logout is running.
func (in menuInputs) switching() bool {
	return in.SwitchingAccountID != "" || in.SwitchingOrgID != "" || in.LoggingOut
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

func insetHeader(label string) MenuItem {
	return MenuItem{Kind: MenuKindHeader, Label: label, Inset: true}
}

func label(text, icon string) MenuItem {
	return MenuItem{Kind: MenuKindLabel, Label: text, Icon: icon}
}

func detail(name, value string) MenuItem {
	if value == "" {
		value = "—"
	}
	return MenuItem{Kind: MenuKindDetail, Label: name, Value: value}
}

func checkItem(id, text string, enabled, checked bool) MenuItem {
	return MenuItem{ID: id, Kind: MenuKindItem, Label: text, Enabled: enabled, Checked: checked, Checkable: true}
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

// buildMenuState lays the menu out like the macOS menu bar panel
// (MenuBarView.swift): tunnel, notices, account, organization, then
// Preferences, More and Quit.
func buildMenuState(in menuInputs) MenuState {
	var items []MenuItem

	if in.HasUpdate {
		items = append(items, item(menuIDUpdate, "Pangolin Update Available…", true), separator())
	}

	if in.Onboarding {
		// Until setup is finished it is the only thing offered, as on macOS.
		items = append(items, item(menuIDOpenSetup, "Open Pangolin Setup…", true))
	} else {
		showAuth := in.Authenticated && (!in.LoggedOut || in.SessionExpired) && !in.Initializing
		if showAuth && in.ActiveAccountID != "" {
			items = append(items, tunnelSection(in)...)
			items = append(items, separator())
		}

		if in.Authenticated && in.ServerDown && !in.Initializing {
			items = append(items, label("The server appears to be down.", menuIconWarning), separator())
		} else if in.ErrorMessage != "" && in.Authenticated && !in.SessionExpired && !in.Initializing {
			items = append(items, label(in.ErrorMessage, menuIconWarning), separator())
		}

		if len(in.Accounts) == 0 {
			items = append(items, item(menuIDLogin, "Log In…", true))
		} else {
			items = append(items, header("Account"), accountSubmenu(in))
		}
		if showAuth {
			items = append(items, header("Organization"), orgSubmenu(in))
		}
		if showAuth && !in.SessionExpired && len(in.ExitNodes) > 0 {
			items = append(items, header("Exit Node"), exitNodeSubmenu(in))
		}
	}

	items = append(items, separator())
	items = append(items, item(menuIDPreferences, "Preferences…", true))
	items = append(items, moreSubmenu(in))
	items = append(items, separator())
	items = append(items, item(menuIDQuit, "Quit Pangolin", true))

	if wm := watermarkText(in); wm != "" {
		items = append(items, separator(), label(wm, ""))
	}

	return MenuState{
		Items:   collapseSeparators(items),
		Loading: !in.Onboarding && (in.Initializing || in.switching()),
	}
}

// tunnelOn is the connect switch's resting value: on unless fully stopped.
func (in menuInputs) tunnelOn() bool {
	return in.TunnelPhase != phaseStopped || in.Connected
}

// tunnelStatus is the status line text and dot. A pending switch click shows
// the state it is heading to right away.
func tunnelStatus(in menuInputs) (text, dot string, transitioning bool) {
	if p := in.PendingTunnelOn; p != nil {
		if !*p {
			return "Disconnecting…", menuDotGray, true
		}
		if in.TunnelPhase != phaseRunning {
			return "Registering…", menuDotOrange, true
		}
	}
	switch in.TunnelPhase {
	case phaseRunning:
		return "Connected", menuDotGreen, false
	case phaseStarting:
		return "Registering…", menuDotOrange, true
	case phaseStopping:
		return "Disconnecting…", menuDotGray, true
	}
	if in.SessionExpired {
		return "Account Locked", menuDotGray, false
	}
	return "Disconnected", menuDotGray, false
}

func tunnelSection(in menuInputs) []MenuItem {
	text, dot, transitioning := tunnelStatus(in)
	status := MenuItem{ID: menuIDSites, Kind: MenuKindStatus, Label: text, Dot: dot, Loading: transitioning, Enabled: true}
	if in.TunnelPhase == phaseRunning && in.PendingTunnelOn == nil {
		status.Items = sitesSubmenu(in)
	}

	on := in.tunnelOn()
	toggle := MenuItem{ID: menuIDConnect, Kind: MenuKindToggle, Label: "Connect", Checked: on}
	if on {
		toggle.Label = "Disconnect"
	}
	if in.PendingTunnelOn != nil {
		toggle.Checked = *in.PendingTunnelOn
	}
	// A locked account can still turn the tunnel off, but not on.
	toggle.Enabled = in.PendingTunnelOn == nil && !in.switching() &&
		in.TunnelPhase != phaseStopping && !(in.SessionExpired && !on)

	items := []MenuItem{status, toggle}
	if in.SessionExpired {
		items = append(items,
			label("Your session expired. Log in again to connect.", menuIconLock),
			item(menuIDReAuth, "Log In…", !in.DeviceAuthInProgress))
	} else if in.ConnectionError != "" {
		items = append(items, label(in.ConnectionError, menuIconWarning))
	}
	return items
}

func sitesSubmenu(in menuInputs) []MenuItem {
	sub := []MenuItem{item(menuIDOpenStatus, "Open Status…", true), separator()}
	switch {
	case len(in.Sites) > 0:
		count := "1 Site"
		if len(in.Sites) != 1 {
			count = fmt.Sprintf("%d Sites", len(in.Sites))
		}
		sub = append(sub, header(count))
		for _, s := range in.Sites {
			sub = append(sub, MenuItem{
				ID:      menuPrefixSite + strconv.Itoa(s.ID),
				Kind:    MenuKindSubmenu,
				Label:   s.Name,
				Dot:     siteDot(s.Color),
				Enabled: true,
				Items:   siteDetail(s, in.Now),
			})
		}
	case !in.SitesLoaded && !in.Connected:
		sub = append(sub, label("Connect to see sites.", ""))
	case !in.SitesLoaded:
		sub = append(sub, label("Loading…", ""))
	default:
		sub = append(sub, label("No sites", ""))
	}
	return sub
}

func siteDot(color string) string {
	switch color {
	case statusColorGreen:
		return menuDotGreen
	case statusColorYellow:
		return menuDotYellow
	}
	return menuDotGray
}

func siteDetail(s StatusSite, now time.Time) []MenuItem {
	status := detail("Status", s.Status)
	status.Dot = siteDot(s.Color)
	return []MenuItem{
		header(s.Name),
		status,
		detail("Connection", s.Connection),
		detail("Endpoint", s.Endpoint),
		detail("Last Seen", relativeTime(s.LastSeen, now)),
	}
}

// relativeTime renders "12s ago", "5m ago", "3h ago" or "2d ago", matching
// the macOS app and the Status tab.
func relativeTime(rfc3339 string, now time.Time) string {
	if rfc3339 == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		return rfc3339
	}
	seconds := int(now.Sub(t) / time.Second)
	if seconds < 0 {
		seconds = 0
	}
	switch {
	case seconds < 60:
		return fmt.Sprintf("%ds ago", seconds)
	case seconds < 3600:
		return fmt.Sprintf("%dm ago", seconds/60)
	case seconds < 86400:
		return fmt.Sprintf("%dh ago", seconds/3600)
	}
	return fmt.Sprintf("%dd ago", seconds/86400)
}

// transitional reports whether account and org switching must be blocked.
func (p tunnelPhase) transitional() bool {
	return p == phaseStarting || p == phaseStopping
}

func accountSubmenu(in menuInputs) MenuItem {
	sub := []MenuItem{insetHeader("Available Accounts")}

	// Show the hostname only when more than one account shares an email.
	emailCounts := map[string]int{}
	for _, a := range in.Accounts {
		emailCounts[a.Email]++
	}

	title := in.CurrentUserDisplay
	for _, a := range in.Accounts {
		text := a.Display
		if emailCounts[a.Email] > 1 {
			text = fmt.Sprintf("%s (%s)", a.Display, a.Hostname)
		}
		it := checkItem(menuPrefixAccount+a.UserID, text,
			!in.TunnelPhase.transitional() && !in.switching(), a.UserID == in.ActiveAccountID)
		it.Loading = a.UserID == in.SwitchingAccountID
		sub = append(sub, it)
		if it.Checked && title == "" {
			title = a.Display
		}
	}
	if title == "" {
		title = "Select Account"
	}

	sub = append(sub, separator(),
		item(menuIDAddAccount, "Add Account…", !in.switching()),
		item(menuIDManageAccounts, "Manage Accounts…", true))
	if in.ActiveAccountID != "" {
		logoutItem := item(menuIDLogout, "Log Out", !in.switching())
		logoutItem.Loading = in.LoggingOut
		sub = append(sub, logoutItem)
	}

	return MenuItem{
		ID:      "accounts",
		Kind:    MenuKindSubmenu,
		Label:   title,
		Enabled: true,
		Loading: in.SwitchingAccountID != "" || in.LoggingOut,
		Items:   sub,
	}
}

func orgSubmenu(in menuInputs) MenuItem {
	var sub []MenuItem
	if len(in.Orgs) == 0 {
		empty := "No Organizations"
		if in.SwitchingAccountID != "" {
			empty = "Loading…"
		}
		sub = append(sub, label(empty, ""))
	} else {
		count := "1 Organization"
		if len(in.Orgs) != 1 {
			count = fmt.Sprintf("%d Organizations", len(in.Orgs))
		}
		sub = append(sub, insetHeader(count))
	}
	for _, o := range in.Orgs {
		it := checkItem(menuPrefixOrg+o.ID, o.Name,
			!in.TunnelPhase.transitional() && !in.switching(),
			in.CurrentOrgID != "" && o.ID == in.CurrentOrgID)
		it.Loading = o.ID == in.SwitchingOrgID
		sub = append(sub, it)
	}

	title := in.CurrentOrgName
	switch {
	case title != "":
	case in.SwitchingAccountID != "":
		title = "Loading…"
	default:
		title = "Select Organization"
	}
	return MenuItem{
		ID:      "orgs",
		Kind:    MenuKindSubmenu,
		Label:   title,
		Enabled: true,
		Loading: in.SwitchingOrgID != "",
		Items:   sub,
	}
}

func exitNodeSubmenu(in menuInputs) MenuItem {
	enabled := !in.TunnelPhase.transitional() && !in.switching()
	sub := []MenuItem{
		insetHeader("Route All Traffic Through"),
		checkItem(menuIDExitNodeNone, "None", enabled, in.ActiveExitNodeID == 0),
	}

	title := "None"
	for _, n := range in.ExitNodes {
		it := checkItem(menuPrefixExitNode+strconv.Itoa(n.ID), n.Name, enabled, n.ID == in.ActiveExitNodeID)
		sub = append(sub, it)
		if it.Checked {
			title = n.Name
		}
	}

	return MenuItem{ID: "exitnodes", Kind: MenuKindSubmenu, Label: title, Enabled: true, Items: sub}
}

func moreSubmenu(in menuInputs) MenuItem {
	sub := []MenuItem{
		header("Support"),
		item(menuIDHowItWorks, "How Pangolin Works", true),
		item(menuIDDocs, "Documentation", true),
		separator(),
		header(fmt.Sprintf("© %d Fossorial, Inc.", in.Year)),
		item(menuIDTerms, "Terms of Service", true),
		item(menuIDPrivacy, "Privacy Policy", true),
		separator(),
		header("Version " + in.Version),
	}
	if in.CheckUpdateVisible {
		sub = append(sub, item(menuIDCheckUpdates, "Check for Updates…", true))
	}
	if !in.CLIInstalled {
		text := "Install Pangolin CLI…"
		if in.CLIInstalling {
			text = "Installing CLI…"
		}
		sub = append(sub, item(menuIDInstallCLI, text, !in.CLIInstalling))
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

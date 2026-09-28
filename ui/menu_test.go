package ui

import (
	"strings"
	"testing"
	"time"
)

func labels(items []MenuItem) []string {
	var out []string
	for _, it := range items {
		switch it.Kind {
		case MenuKindSeparator:
			out = append(out, "---")
		default:
			out = append(out, it.Label)
		}
	}
	return out
}

func find(items []MenuItem, label string) *MenuItem {
	for i := range items {
		if items[i].Label == label {
			return &items[i]
		}
	}
	return nil
}

func findID(items []MenuItem, id string) *MenuItem {
	for i := range items {
		if items[i].ID == id {
			return &items[i]
		}
	}
	return nil
}

func signedIn() menuInputs {
	return menuInputs{
		Authenticated: true,
		TunnelPhase:   phaseStopped,
		Accounts: []menuAccount{
			{UserID: "u1", Display: "a@example.com", Email: "a@example.com", Hostname: "https://one"},
		},
		ActiveAccountID: "u1",
		Orgs:            []menuOrg{{ID: "o1", Name: "Org One"}, {ID: "o2", Name: "Org Two"}},
		CurrentOrgID:    "o1",
		CurrentOrgName:  "Org One",
		CLIInstalled:    true,
		Version:         "1.2.3",
		Year:            2026,
		Now:             time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC),
	}
}

func boolPtr(v bool) *bool { return &v }

func TestMenuSignedOut(t *testing.T) {
	got := strings.Join(labels(buildMenuState(menuInputs{CLIInstalled: true}).Items), "|")
	want := "Log In…|---|Preferences…|More|---|Quit Pangolin"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestMenuLoadingStates(t *testing.T) {
	in := signedIn()
	if buildMenuState(in).Loading {
		t.Fatal("menu should not be loading")
	}
	for name, mutate := range map[string]func(*menuInputs){
		"initializing":     func(in *menuInputs) { in.Initializing = true },
		"switchingAccount": func(in *menuInputs) { in.SwitchingAccountID = "u1" },
		"switchingOrg":     func(in *menuInputs) { in.SwitchingOrgID = "o2" },
		"loggingOut":       func(in *menuInputs) { in.LoggingOut = true },
	} {
		in := signedIn()
		mutate(&in)
		if !buildMenuState(in).Loading {
			t.Errorf("%s: expected the loading spinner", name)
		}
	}
}

func TestMenuInitializingHidesTunnel(t *testing.T) {
	in := signedIn()
	in.Initializing = true
	items := buildMenuState(in).Items
	if findID(items, menuIDConnect) != nil || findID(items, "orgs") != nil {
		t.Fatal("tunnel and org sections should be hidden while initializing")
	}
}

func TestMenuSignedInStopped(t *testing.T) {
	got := strings.Join(labels(buildMenuState(signedIn()).Items), "|")
	want := "Disconnected|Connect|---|Account|a@example.com|Organization|Org One|---|Preferences…|More|---|Quit Pangolin"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestMenuTunnelStatus(t *testing.T) {
	cases := []struct {
		phase   tunnelPhase
		pending *bool
		text    string
		dot     string
		loading bool
	}{
		{phaseStopped, nil, "Disconnected", menuDotGray, false},
		{phaseStarting, nil, "Registering…", menuDotOrange, true},
		{phaseRunning, nil, "Connected", menuDotGreen, false},
		{phaseStopping, nil, "Disconnecting…", menuDotGray, true},
		{phaseOther, nil, "Disconnected", menuDotGray, false},
		{phaseStopped, boolPtr(true), "Registering…", menuDotOrange, true},
		{phaseRunning, boolPtr(false), "Disconnecting…", menuDotGray, true},
	}
	for _, c := range cases {
		in := signedIn()
		in.TunnelPhase = c.phase
		in.PendingTunnelOn = c.pending
		it := findID(buildMenuState(in).Items, menuIDSites)
		if it == nil || it.Kind != MenuKindStatus || it.Label != c.text || it.Dot != c.dot || it.Loading != c.loading {
			t.Errorf("phase %v pending %v: got %+v", c.phase, c.pending, it)
		}
	}
}

func TestMenuConnectToggle(t *testing.T) {
	cases := []struct {
		phase   tunnelPhase
		pending *bool
		label   string
		enabled bool
		checked bool
	}{
		{phaseStopped, nil, "Connect", true, false},
		{phaseStarting, nil, "Disconnect", true, true},
		{phaseRunning, nil, "Disconnect", true, true},
		{phaseOther, nil, "Disconnect", true, true},
		{phaseStopping, nil, "Disconnect", false, true},
		// A pending click flips the switch at once and blocks another click.
		{phaseStopped, boolPtr(true), "Connect", false, true},
		{phaseRunning, boolPtr(false), "Disconnect", false, false},
	}
	for _, c := range cases {
		in := signedIn()
		in.TunnelPhase = c.phase
		in.PendingTunnelOn = c.pending
		it := findID(buildMenuState(in).Items, menuIDConnect)
		if it == nil || it.Kind != MenuKindToggle || it.Label != c.label || it.Enabled != c.enabled || it.Checked != c.checked {
			t.Errorf("phase %v pending %v: got %+v", c.phase, c.pending, it)
		}
	}
}

func TestMenuSwitchingDisabledWhileTransitional(t *testing.T) {
	for _, phase := range []tunnelPhase{phaseStarting, phaseStopping} {
		in := signedIn()
		in.TunnelPhase = phase
		items := buildMenuState(in).Items
		if org := findID(findID(items, "orgs").Items, menuPrefixOrg+"o2"); org.Enabled {
			t.Errorf("phase %v: org switching should be disabled", phase)
		}
		if acc := findID(findID(items, "accounts").Items, menuPrefixAccount+"u1"); acc.Enabled {
			t.Errorf("phase %v: account switching should be disabled", phase)
		}
	}
	in := signedIn()
	in.TunnelPhase = phaseRunning
	if org := findID(findID(buildMenuState(in).Items, "orgs").Items, menuPrefixOrg+"o2"); !org.Enabled {
		t.Error("org switching should be enabled while connected")
	}
}

func TestMenuSessionExpired(t *testing.T) {
	in := signedIn()
	in.SessionExpired = true
	in.LoggedOut = true
	in.ErrorMessage = "should not show"
	in.ConnectionError = "should not show either"
	in.ServerInfo = &menuServerInfo{Build: "oss"}
	items := buildMenuState(in).Items
	got := strings.Join(labels(items), "|")
	want := "Account Locked|Connect|Your session expired. Log in again to connect.|Log In…|---|Account|a@example.com|Organization|Org One|---|Preferences…|More|---|Quit Pangolin"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if it := findID(items, menuIDConnect); it.Enabled {
		t.Fatal("a locked account can't connect")
	}
	if it := find(items, "Your session expired. Log in again to connect."); it.Kind != MenuKindLabel || it.Icon != menuIconLock {
		t.Fatalf("unexpected notice %+v", it)
	}
	if it := findID(items, menuIDReAuth); it == nil || !it.Enabled {
		t.Fatal("expected enabled Log In…")
	}

	// It can still turn a running tunnel off.
	in.TunnelPhase = phaseRunning
	if it := findID(buildMenuState(in).Items, menuIDConnect); !it.Enabled || it.Label != "Disconnect" {
		t.Fatalf("got %+v", it)
	}

	in.DeviceAuthInProgress = true
	if it := findID(buildMenuState(in).Items, menuIDReAuth); it.Enabled {
		t.Fatal("Log In… should be disabled while device auth runs")
	}
}

func TestMenuConnectionError(t *testing.T) {
	in := signedIn()
	in.ConnectionError = "Could not connect"
	items := buildMenuState(in).Items
	it := find(items, "Could not connect")
	if it == nil || it.Kind != MenuKindLabel || it.Icon != menuIconWarning {
		t.Fatalf("got %+v", it)
	}
	if items[2].Label != "Could not connect" {
		t.Fatalf("error should follow the switch, got %q", labels(items))
	}
}

func TestMenuLoggedOutHidesAuthSection(t *testing.T) {
	in := signedIn()
	in.LoggedOut = true
	items := buildMenuState(in).Items
	if findID(items, menuIDConnect) != nil || findID(items, "orgs") != nil {
		t.Fatal("tunnel and org sections should be hidden when logged out")
	}
	if findID(items, "accounts") == nil {
		t.Fatal("accounts should stay visible")
	}
}

func TestMenuNoActiveAccountHidesTunnel(t *testing.T) {
	in := signedIn()
	in.ActiveAccountID = ""
	items := buildMenuState(in).Items
	if findID(items, menuIDConnect) != nil {
		t.Fatal("tunnel section needs an active account")
	}
	if got := findID(items, "accounts").Label; got != "Select Account" {
		t.Fatalf("account title %q", got)
	}
}

func TestMenuServerDownAndError(t *testing.T) {
	in := signedIn()
	in.ServerDown = true
	in.ErrorMessage = "boom"
	items := buildMenuState(in).Items
	got := strings.Join(labels(items), "|")
	want := "Disconnected|Connect|---|The server appears to be down.|---|Account|a@example.com|Organization|Org One|---|Preferences…|More|---|Quit Pangolin"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if find(items, "boom") != nil {
		t.Fatal("error message should be hidden while the server is down")
	}

	in.ServerDown = false
	if it := find(buildMenuState(in).Items, "boom"); it == nil || it.Icon != menuIconWarning {
		t.Fatal("expected error message")
	}
}

func TestMenuLicenseNoticeAfterQuit(t *testing.T) {
	in := signedIn()
	in.ServerInfo = &menuServerInfo{Build: "oss"}
	items := buildMenuState(in).Items
	last := items[len(items)-1]
	if last.Kind != MenuKindLabel || last.Label != "Community Edition. Consider supporting." {
		t.Fatalf("got %+v", last)
	}
	if items[len(items)-3].ID != menuIDQuit || items[len(items)-2].Kind != MenuKindSeparator {
		t.Fatalf("notice should follow Quit: %q", labels(items))
	}
}

func TestMenuWatermark(t *testing.T) {
	personal := "Personal"
	cases := []struct {
		si   menuServerInfo
		want string
	}{
		{menuServerInfo{Build: "enterprise", EnterpriseLicenseValid: true, EnterpriseLicenseType: personal}, "Licensed for personal use only."},
		{menuServerInfo{Build: "enterprise"}, "This server is unlicensed."},
		{menuServerInfo{Build: "enterprise", EnterpriseLicenseValid: true}, ""},
		{menuServerInfo{Build: "oss"}, "Community Edition. Consider supporting."},
		{menuServerInfo{Build: "oss", SupporterStatusValid: true}, ""},
	}
	for _, c := range cases {
		in := signedIn()
		si := c.si
		in.ServerInfo = &si
		if got := watermarkText(in); got != c.want {
			t.Errorf("%+v: got %q want %q", c.si, got, c.want)
		}
	}
}

func TestMenuUpdateAndCLI(t *testing.T) {
	in := signedIn()
	in.HasUpdate = true
	in.CLIInstalled = false
	in.CLIInstalling = true
	in.CheckUpdateVisible = true
	items := buildMenuState(in).Items
	if items[0].ID != menuIDUpdate || items[1].Kind != MenuKindSeparator {
		t.Fatalf("update item should be first, got %+v", items[0])
	}
	more := findID(items, "more")
	got := strings.Join(labels(more.Items), "|")
	want := "Support|How Pangolin Works|Documentation|---|© 2026 Fossorial, Inc.|Terms of Service|Privacy Policy|---|Version 1.2.3|Check for Updates…|Installing CLI…"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if more.Items[4].Kind != MenuKindHeader || more.Items[8].Kind != MenuKindHeader {
		t.Fatal("copyright and version should be headers")
	}
	if cli := findID(more.Items, menuIDInstallCLI); cli.Enabled {
		t.Fatalf("unexpected CLI item %+v", cli)
	}
	in.CLIInstalling = false
	if cli := findID(findID(buildMenuState(in).Items, "more").Items, menuIDInstallCLI); cli.Label != "Install Pangolin CLI…" || !cli.Enabled {
		t.Fatalf("unexpected CLI item %+v", cli)
	}
	in.CLIInstalled = true
	if findID(findID(buildMenuState(in).Items, "more").Items, menuIDInstallCLI) != nil {
		t.Fatal("CLI item should be hidden once installed")
	}
}

func TestMenuAccounts(t *testing.T) {
	in := signedIn()
	in.Accounts = []menuAccount{
		{UserID: "u1", Display: "a@example.com", Email: "a@example.com", Hostname: "https://one"},
		{UserID: "u2", Display: "a@example.com", Email: "a@example.com", Hostname: "https://two"},
		{UserID: "u3", Display: "b@example.com", Email: "b@example.com", Hostname: "https://one"},
	}
	accounts := findID(buildMenuState(in).Items, "accounts")
	if accounts.Label != "a@example.com" {
		t.Fatalf("submenu label %q", accounts.Label)
	}
	got := strings.Join(labels(accounts.Items), "|")
	want := "Available Accounts|a@example.com (https://one)|a@example.com (https://two)|b@example.com|---|Add Account…|Manage Accounts…|Log Out"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if !accounts.Items[0].Inset {
		t.Fatal("header should be inset")
	}
	u1, u2 := findID(accounts.Items, menuPrefixAccount+"u1"), findID(accounts.Items, menuPrefixAccount+"u2")
	if !u1.Checked || u2.Checked || !u1.Checkable || !u2.Checkable {
		t.Fatal("only the active account should be checked")
	}

	in.CurrentUserDisplay = "Alice"
	if got := findID(buildMenuState(in).Items, "accounts").Label; got != "Alice" {
		t.Fatalf("title should prefer the signed-in user, got %q", got)
	}

	in.SwitchingAccountID = "u2"
	accounts = findID(buildMenuState(in).Items, "accounts")
	if !accounts.Loading || !findID(accounts.Items, menuPrefixAccount+"u2").Loading {
		t.Fatal("expected spinners while switching")
	}
	if findID(accounts.Items, menuPrefixAccount+"u3").Enabled || findID(accounts.Items, menuIDAddAccount).Enabled {
		t.Fatal("rows should be disabled while switching")
	}
}

func TestMenuOrgs(t *testing.T) {
	in := signedIn()
	orgs := findID(buildMenuState(in).Items, "orgs")
	if orgs.Label != "Org One" || orgs.Items[0].Label != "2 Organizations" || !orgs.Items[0].Inset {
		t.Fatalf("unexpected org submenu %+v", orgs)
	}
	if got := strings.Join(labels(orgs.Items), "|"); got != "2 Organizations|Org One|Org Two" {
		t.Fatalf("got %q", got)
	}

	in.Orgs = []menuOrg{{ID: "o1", Name: "Solo"}}
	if got := findID(buildMenuState(in).Items, "orgs").Items[0].Label; got != "1 Organization" {
		t.Fatalf("got %q", got)
	}

	in.Orgs = nil
	in.CurrentOrgID, in.CurrentOrgName = "", ""
	orgs = findID(buildMenuState(in).Items, "orgs")
	if orgs.Label != "Select Organization" || len(orgs.Items) != 1 || orgs.Items[0].Label != "No Organizations" || orgs.Items[0].Kind != MenuKindLabel {
		t.Fatalf("unexpected empty org submenu %+v", orgs)
	}

	in.SwitchingAccountID = "u1"
	orgs = findID(buildMenuState(in).Items, "orgs")
	if orgs.Label != "Loading…" || orgs.Items[0].Label != "Loading…" {
		t.Fatalf("unexpected org submenu while switching accounts %+v", orgs)
	}
}

func TestMenuSitesSubmenu(t *testing.T) {
	in := signedIn()
	if it := findID(buildMenuState(in).Items, menuIDSites); len(it.Items) != 0 {
		t.Fatal("sites only open while connected")
	}

	in.TunnelPhase = phaseRunning
	in.Connected = true
	sites := findID(buildMenuState(in).Items, menuIDSites)
	if got := strings.Join(labels(sites.Items), "|"); got != "Open Status…|---|Loading…" {
		t.Fatalf("got %q", got)
	}

	in.SitesLoaded = true
	sites = findID(buildMenuState(in).Items, menuIDSites)
	if got := strings.Join(labels(sites.Items), "|"); got != "Open Status…|---|No sites" {
		t.Fatalf("got %q", got)
	}

	in.SitesLoaded, in.Connected = false, false
	sites = findID(buildMenuState(in).Items, menuIDSites)
	if got := strings.Join(labels(sites.Items), "|"); got != "Open Status…|---|Connect to see sites." {
		t.Fatalf("got %q", got)
	}

	in.SitesLoaded = true
	in.Sites = []StatusSite{
		{ID: exitNodeSiteID, Name: "Pangolin Server", Endpoint: "203.0.113.1:51820", Status: "Connected", Color: statusColorGreen,
			LastSeen: in.Now.Add(-12 * time.Second).Format(time.RFC3339)},
		{ID: 4, Name: "Office", Status: "Connecting", Color: statusColorYellow, Connection: "Relay"},
	}
	sites = findID(buildMenuState(in).Items, menuIDSites)
	if got := strings.Join(labels(sites.Items), "|"); got != "Open Status…|---|2 Sites|Pangolin Server|Office" {
		t.Fatalf("got %q", got)
	}
	server := findID(sites.Items, "site:-1")
	if server.Kind != MenuKindSubmenu || server.Dot != menuDotGreen {
		t.Fatalf("got %+v", server)
	}
	var details []string
	for _, d := range server.Items[1:] {
		details = append(details, d.Label+"="+d.Value)
	}
	if got := strings.Join(details, "|"); got != "Status=Connected|Connection=—|Endpoint=203.0.113.1:51820|Last Seen=12s ago" {
		t.Fatalf("got %q", got)
	}
	if office := findID(sites.Items, "site:4"); office.Dot != menuDotYellow || office.Items[4].Value != "—" {
		t.Fatalf("got %+v", office)
	}

	// A pending disconnect closes the sites submenu.
	in.PendingTunnelOn = boolPtr(false)
	if it := findID(buildMenuState(in).Items, menuIDSites); len(it.Items) != 0 {
		t.Fatal("sites should close while disconnecting")
	}
}

func TestRelativeTime(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	cases := map[time.Duration]string{
		5 * time.Second:  "5s ago",
		90 * time.Second: "1m ago",
		3 * time.Hour:    "3h ago",
		50 * time.Hour:   "2d ago",
		-time.Minute:     "0s ago",
	}
	for ago, want := range cases {
		if got := relativeTime(now.Add(-ago).Format(time.RFC3339), now); got != want {
			t.Errorf("%v: got %q want %q", ago, got, want)
		}
	}
	if relativeTime("", now) != "" {
		t.Error("empty time should stay empty")
	}
}

func TestPhaseDebouncer(t *testing.T) {
	var d phaseDebouncer
	start := time.Now()
	if got, _ := d.update(phaseRunning, start, false); got != phaseRunning {
		t.Fatalf("got %v", got)
	}
	// A drop is held back...
	got, wait := d.update(phaseStarting, start.Add(100*time.Millisecond), false)
	if got != phaseRunning || wait != tunnelDropDelay {
		t.Fatalf("got %v wait %v", got, wait)
	}
	// ...and recovering before the delay never shows it.
	if got, _ := d.update(phaseRunning, start.Add(time.Second), false); got != phaseRunning {
		t.Fatalf("got %v", got)
	}
	d.update(phaseStopped, start.Add(2*time.Second), false)
	if got, _ := d.update(phaseStopped, start.Add(2*time.Second+tunnelDropDelay), false); got != phaseStopped {
		t.Fatalf("drop should show after the delay, got %v", got)
	}
	// Drops the user asked for show at once, as do non-drops.
	d.update(phaseRunning, start.Add(5*time.Second), false)
	if got, _ := d.update(phaseStopped, start.Add(5*time.Second), true); got != phaseStopped {
		t.Fatalf("got %v", got)
	}
	d.update(phaseRunning, start.Add(6*time.Second), false)
	if got, _ := d.update(phaseStopping, start.Add(6*time.Second), false); got != phaseStopping {
		t.Fatalf("got %v", got)
	}
}

func TestCollapseSeparators(t *testing.T) {
	in := []MenuItem{separator(), item("a", "A", true), separator(), separator(), item("b", "B", true), separator()}
	if got := strings.Join(labels(collapseSeparators(in)), "|"); got != "A|---|B" {
		t.Fatalf("got %q", got)
	}
}

func TestMenuExitNodeHiddenWithoutGateways(t *testing.T) {
	in := signedIn()
	items := buildMenuState(in).Items
	if findID(items, "exitnodes") != nil || find(items, "Exit Node") != nil {
		t.Fatal("exit node section must be hidden when no exit nodes are available")
	}
}

func TestMenuExitNodeHiddenWhenSessionExpired(t *testing.T) {
	in := signedIn()
	in.ExitNodes = []menuExitNode{{ID: 7, Name: "Office"}}
	in.SessionExpired = true
	if findID(buildMenuState(in).Items, "exitnodes") != nil {
		t.Fatal("exit node section must be hidden when the session expired")
	}
}

func TestMenuExitNodeSubmenu(t *testing.T) {
	in := signedIn()
	in.ExitNodes = []menuExitNode{{ID: 7, Name: "Office"}, {ID: 9, Name: "Home"}}

	// nothing selected: submenu is labelled None and "None" is checked
	sub := findID(buildMenuState(in).Items, "exitnodes")
	if sub == nil || sub.Kind != MenuKindSubmenu || sub.Label != "None" {
		t.Fatalf("got %+v", sub)
	}
	if got := strings.Join(labels(sub.Items), "|"); got != "Route All Traffic Through|None|Office|Home" {
		t.Fatalf("got %q", got)
	}
	if none := findID(sub.Items, menuIDExitNodeNone); none == nil || !none.Checked {
		t.Fatalf("None should be checked: %+v", none)
	}
	for _, id := range []string{"exitnode:7", "exitnode:9"} {
		if it := findID(sub.Items, id); it == nil || it.Checked || !it.Enabled {
			t.Fatalf("%s: %+v", id, it)
		}
	}

	// one selected: it is checked, labels the submenu, and None is not checked
	in.ActiveExitNodeID = 9
	sub = findID(buildMenuState(in).Items, "exitnodes")
	if sub.Label != "Home" {
		t.Fatalf("label = %q", sub.Label)
	}
	if it := findID(sub.Items, "exitnode:9"); !it.Checked {
		t.Fatalf("selected exit node should be checked: %+v", it)
	}
	if none := findID(sub.Items, menuIDExitNodeNone); none.Checked {
		t.Fatal("None must not be checked when an exit node is selected")
	}

	// switching is blocked while the tunnel is connecting/disconnecting
	in.TunnelPhase = phaseStarting
	sub = findID(buildMenuState(in).Items, "exitnodes")
	if it := findID(sub.Items, "exitnode:7"); it.Enabled {
		t.Fatal("exit node switching should be disabled while transitional")
	}
}

func TestMenuOnboarding(t *testing.T) {
	in := signedIn()
	in.Onboarding = true
	in.SwitchingAccountID = "u1"
	in.ServerDown = true
	in.ServerInfo = &menuServerInfo{Build: "oss"}
	state := buildMenuState(in)
	got := strings.Join(labels(state.Items), "|")
	want := "Open Pangolin Setup…|---|Preferences…|More|---|Quit Pangolin|---|Community Edition. Consider supporting."
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if state.Loading {
		t.Fatal("setup never shows the loading spinner")
	}
	if findID(state.Items, menuIDOpenSetup) == nil {
		t.Fatal("expected Open Pangolin Setup…")
	}
}

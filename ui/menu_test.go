package ui

import (
	"strings"
	"testing"
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
		Authenticated:   true,
		TunnelPhase:     phaseStopped,
		TunnelStateText: "Disconnected",
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
	}
}

func TestMenuSignedOut(t *testing.T) {
	got := strings.Join(labels(buildMenuState(menuInputs{LoginLabel: "Login to Account", CLIInstalled: true}).Items), "|")
	want := "Login to Account|---|Preferences|More|---|Quit"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestMenuInitializing(t *testing.T) {
	in := signedIn()
	in.Initializing = true
	items := buildMenuState(in).Items
	if find(items, "Loading...") == nil {
		t.Fatal("expected Loading...")
	}
	if findID(items, menuIDConnect) != nil || findID(items, "orgs") != nil {
		t.Fatal("auth section should be hidden while initializing")
	}
	// Accounts stay visible, as before.
	if findID(items, "accounts") == nil {
		t.Fatal("expected account submenu")
	}
}

func TestMenuSignedInStopped(t *testing.T) {
	got := strings.Join(labels(buildMenuState(signedIn()).Items), "|")
	want := "Status: Disconnected|Connect|---|Account|a@example.com|Organization|Org One|---|Preferences|More|---|Quit"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestMenuConnectLabels(t *testing.T) {
	cases := []struct {
		phase   tunnelPhase
		label   string
		enabled bool
		checked bool
	}{
		{phaseStopped, "Connect", true, false},
		{phaseStarting, "Disconnect", true, false},
		{phaseRunning, "Disconnect", true, true},
		{phaseOther, "Disconnect", true, false},
		{phaseStopping, "Disconnecting...", false, false},
	}
	for _, c := range cases {
		in := signedIn()
		in.TunnelPhase = c.phase
		it := findID(buildMenuState(in).Items, menuIDConnect)
		if it == nil || it.Label != c.label || it.Enabled != c.enabled || it.Checked != c.checked {
			t.Errorf("phase %v: got %+v", c.phase, it)
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
	in.ServerInfo = &menuServerInfo{Build: "oss"}
	items := buildMenuState(in).Items
	if find(items, "Status: Account Locked") == nil {
		t.Fatal("expected Account Locked status")
	}
	if findID(items, menuIDConnect) != nil {
		t.Fatal("connect should be hidden when the session expired")
	}
	if it := findID(items, menuIDReAuth); it == nil || !it.Enabled {
		t.Fatal("expected enabled Log In")
	}
	if findID(items, "orgs") == nil {
		t.Fatal("cached org should stay visible")
	}
	if find(items, "should not show") != nil || find(items, "Community Edition. Consider supporting.") != nil {
		t.Fatal("error and watermark should be hidden when the session expired")
	}

	in.DeviceAuthInProgress = true
	if it := findID(buildMenuState(in).Items, menuIDReAuth); it.Enabled {
		t.Fatal("Log In should be disabled while device auth runs")
	}
}

func TestMenuLoggedOutHidesAuthSection(t *testing.T) {
	in := signedIn()
	in.LoggedOut = true
	items := buildMenuState(in).Items
	if findID(items, menuIDConnect) != nil || findID(items, "orgs") != nil {
		t.Fatal("auth section should be hidden when logged out")
	}
}

func TestMenuServerDownAndError(t *testing.T) {
	in := signedIn()
	in.ServerDown = true
	in.ErrorMessage = "boom"
	items := buildMenuState(in).Items
	if find(items, "The server appears to be down.") == nil {
		t.Fatal("expected server down message")
	}
	if find(items, "boom") != nil {
		t.Fatal("error message should be hidden while the server is down")
	}
	if findID(items, menuIDConnect) == nil {
		t.Fatal("auth section should stay visible while the server is down")
	}

	in.ServerDown = false
	if find(buildMenuState(in).Items, "boom") == nil {
		t.Fatal("expected error message")
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
	if items[0].ID != menuIDUpdate {
		t.Fatalf("update item should be first, got %+v", items[0])
	}
	more := findID(items, "more")
	if findID(more.Items, menuIDCheckUpdates) == nil {
		t.Fatal("expected Check for Updates")
	}
	cli := findID(more.Items, menuIDInstallCLI)
	if cli == nil || cli.Enabled || cli.Label != "Installing CLI…" {
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
	want := "Available Accounts|---|a@example.com (https://one)|a@example.com (https://two)|b@example.com|---|Add Account|Logout"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if !findID(accounts.Items, menuPrefixAccount+"u1").Checked || findID(accounts.Items, menuPrefixAccount+"u2").Checked {
		t.Fatal("only the active account should be checked")
	}
}

func TestMenuOrgs(t *testing.T) {
	in := signedIn()
	orgs := findID(buildMenuState(in).Items, "orgs")
	if orgs.Label != "Org One" || orgs.Items[0].Label != "2 Organizations" {
		t.Fatalf("unexpected org submenu %+v", orgs)
	}

	in.Orgs = []menuOrg{{ID: "o1", Name: "Solo"}}
	if got := findID(buildMenuState(in).Items, "orgs").Items[0].Label; got != "1 Organization" {
		t.Fatalf("got %q", got)
	}

	in.Orgs = nil
	in.CurrentOrgID, in.CurrentOrgName = "", ""
	orgs = findID(buildMenuState(in).Items, "orgs")
	if orgs.Label != "Organizations" || orgs.Items[0].Label != "0 Organizations" || orgs.Items[2].Label != "No organizations" {
		t.Fatalf("unexpected empty org submenu %+v", orgs)
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

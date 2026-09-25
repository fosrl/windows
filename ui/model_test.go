package ui

import (
	"testing"
	"time"
)

func TestParseLogLine(t *testing.T) {
	cases := []struct {
		in    string
		level string
		line  string
		stamp string
	}{
		{"ERROR: 2025/11/26 11:37:43 Failed to poll OLM status", "ERROR", "Failed to poll OLM status", "2025-11-26 11:37:43.000"},
		{"[2025-01-02 03:04:05.678] [WARN] careful", "WARN", "careful", "2025-01-02 03:04:05.678"},
		{"2025-01-02T03:04:05Z debug iso line", "debug", "iso line", "2025-01-02 03:04:05.000"},
		{"2025-01-02 03:04:05 info spaced line", "info", "spaced line", "2025-01-02 03:04:05.000"},
	}
	for _, c := range cases {
		got := parseLogLine(c.in)
		if got == nil || got.Level != c.level || got.Line != c.line || got.Stamp.Format(logStampFormat) != c.stamp {
			t.Errorf("%q: got %+v", c.in, got)
		}
	}
	if parseLogLine("   ") != nil {
		t.Error("blank line should be skipped")
	}
	if got := parseLogLine("no timestamp here"); got == nil || got.Level != "UNKNOWN" || got.Line != "no timestamp here" {
		t.Errorf("fallback: got %+v", got)
	}
}

func TestLogLineString(t *testing.T) {
	l := LogLine{Stamp: time.Date(2025, 1, 2, 3, 4, 5, 6e6, time.UTC), Level: "INFO", Line: "hello"}
	if got := l.String(); got != "2025-01-02 03:04:05.006 [INFO] hello\r\n" {
		t.Errorf("got %q", got)
	}
}

func TestNormalizeURL(t *testing.T) {
	cases := map[string]string{
		"":                         "",
		"  pangolin.example.com/ ": "https://pangolin.example.com",
		"http://local:3000//":      "http://local:3000",
		"https://x.y":              "https://x.y",
	}
	for in, want := range cases {
		if got := normalizeURL(in); got != want {
			t.Errorf("normalizeURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAppendAuthPath(t *testing.T) {
	if got := appendAuthPathToURL("https://h/auth", ""); got != "https://h/auth" {
		t.Errorf("got %q", got)
	}
	if got := appendAuthPathToURL("https://h/auth?code=1", "/sso x"); got != "https://h/auth?code=1&authPath=%2Fsso+x" {
		t.Errorf("got %q", got)
	}
	if got := appendAuthPathToURL("https://h/auth", "p"); got != "https://h/auth?authPath=p" {
		t.Errorf("got %q", got)
	}
}

func TestSiteTracker(t *testing.T) {
	tr := newSiteTracker()
	t0 := time.Now()
	exit := &peerInput{Endpoint: "1.2.3.4:51820", Connected: true}
	peers := []peerInput{{ID: 5, Name: "B", Connected: false}, {ID: 2, Name: "", Connected: true}}

	sites := tr.update(exit, peers, t0)
	if len(sites) != 3 || sites[0].Name != "Pangolin Server" || sites[0].ID != exitNodeSiteID {
		t.Fatalf("exit node should be first: %+v", sites)
	}
	if sites[1].ID != 2 || sites[1].Name != "Unknown" || sites[1].Status != "Connected" {
		t.Fatalf("unexpected site %+v", sites[1])
	}
	if sites[2].Status != "Connecting" || sites[2].Color != statusColorYellow {
		t.Fatalf("new unconnected site should be connecting: %+v", sites[2])
	}

	sites = tr.update(exit, peers, t0.Add(peerConnectingTimeout))
	if sites[2].Status != "Disconnected" || sites[2].Color != statusColorGray {
		t.Fatalf("site should time out: %+v", sites[2])
	}

	// A site that disappears and comes back gets a fresh connecting window.
	tr.update(exit, peers[1:], t0.Add(11*time.Second))
	sites = tr.update(exit, peers, t0.Add(12*time.Second))
	if len(sites) != 3 || sites[2].ID != 5 || sites[2].Status != "Connecting" {
		t.Fatalf("reappearing site should be connecting again: %+v", sites)
	}

	if got := tr.update(nil, nil, t0); len(got) != 0 {
		t.Fatalf("no status should mean no sites: %+v", got)
	}
}

func TestSiteTrackerDetails(t *testing.T) {
	seen := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	tr := newSiteTracker()
	sites := tr.update(
		&peerInput{Connected: true, LastSeen: seen},
		[]peerInput{
			{ID: 1, Name: "a", IsLocal: true, IsRelay: true},
			{ID: 2, Name: "b", IsRelay: true},
			{ID: 3, Name: "c"},
		},
		time.Now(),
	)
	if sites[0].Connection != "" || sites[0].LastSeen != "2026-09-24T10:00:00Z" {
		t.Fatalf("exit node: %+v", sites[0])
	}
	want := []string{"Local", "Relay", "Direct"}
	for i, w := range want {
		if sites[i+1].Connection != w || sites[i+1].LastSeen != "" {
			t.Errorf("site %d: %+v", i+1, sites[i+1])
		}
	}
	if got := tr.update(nil, nil, time.Now()); len(got) != 0 {
		t.Fatalf("no status should mean no sites: %+v", got)
	}
}

package ui

import (
	"sort"
	"time"
)

// Status dot colors, matching the old walk status tab.
const (
	statusColorGreen  = "green"
	statusColorGray   = "gray"
	statusColorYellow = "yellow"
)

const peerConnectingTimeout = 10 * time.Second

// exitNodeSiteID keys the synthetic "Pangolin Server" row alongside real (non-negative) site IDs.
const exitNodeSiteID = -1

// StatusSite is one row of the Sites list in the Status tab.
type StatusSite struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Endpoint string `json:"endpoint"`
	Status   string `json:"status"`
	Color    string `json:"color"`
	// Connection is "Local", "Relay" or "Direct"; empty for the Pangolin Server row.
	Connection string `json:"connection"`
	// LastSeen is an RFC 3339 time, or empty when unknown.
	LastSeen string `json:"lastSeen"`
}

// StatusView is everything the Status tab renders.
type StatusView struct {
	StateText string       `json:"stateText"`
	Color     string       `json:"color"`
	Version   string       `json:"version"`
	Agent     string       `json:"agent"`
	OrgID     string       `json:"orgId"`
	Sites     []StatusSite `json:"sites"`
	JSON      string       `json:"json"`
}

type peerInput struct {
	ID        int
	Name      string
	Endpoint  string
	Connected bool
	LastSeen  time.Time
	IsLocal   bool
	IsRelay   bool
}

// connectionLabel matches the macOS app's SiteStatusItem.connectionLabel.
func connectionLabel(isLocal, isRelay bool) string {
	switch {
	case isLocal:
		return "Local"
	case isRelay:
		return "Relay"
	}
	return "Direct"
}

// siteTracker keeps the per-site "first seen" times used for the connecting
// timeout, and the order sites first appeared in.
type siteTracker struct {
	order     []int
	firstSeen map[int]time.Time
}

func newSiteTracker() *siteTracker {
	return &siteTracker{firstSeen: map[int]time.Time{}}
}

func (t *siteTracker) reset() {
	t.order = nil
	t.firstSeen = map[int]time.Time{}
}

// update returns the rows to show for the given peers. The exit node goes
// first. A site that is not connected shows "Connecting" for 10 seconds after
// it (re)appears, then "Disconnected".
func (t *siteTracker) update(exitNode *peerInput, peers []peerInput, now time.Time) []StatusSite {
	present := map[int]peerInput{}
	var incoming []peerInput
	if exitNode != nil {
		p := *exitNode
		p.ID = exitNodeSiteID
		p.Name = "Pangolin Server"
		incoming = append(incoming, p)
	}
	sorted := append([]peerInput(nil), peers...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	incoming = append(incoming, sorted...)

	known := map[int]bool{}
	for _, id := range t.order {
		known[id] = true
	}
	for _, p := range incoming {
		present[p.ID] = p
		if !known[p.ID] {
			t.order = append(t.order, p.ID)
			known[p.ID] = true
		}
		if _, seen := t.firstSeen[p.ID]; !seen {
			t.firstSeen[p.ID] = now
		}
	}
	// Sites that disappeared restart their connecting window when they come back.
	for id := range t.firstSeen {
		if _, ok := present[id]; !ok {
			delete(t.firstSeen, id)
		}
	}

	sites := make([]StatusSite, 0, len(present))
	for _, id := range t.order {
		p, ok := present[id]
		if !ok {
			continue
		}
		name := p.Name
		if name == "" {
			name = "Unknown"
		}
		site := StatusSite{ID: id, Name: name, Endpoint: p.Endpoint}
		if id != exitNodeSiteID {
			site.Connection = connectionLabel(p.IsLocal, p.IsRelay)
		}
		if !p.LastSeen.IsZero() {
			site.LastSeen = p.LastSeen.UTC().Format(time.RFC3339)
		}
		switch {
		case p.Connected:
			site.Status, site.Color = "Connected", statusColorGreen
		case now.Sub(t.firstSeen[id]) >= peerConnectingTimeout:
			site.Status, site.Color = "Disconnected", statusColorGray
		default:
			site.Status, site.Color = "Connecting", statusColorYellow
		}
		sites = append(sites, site)
	}
	return sites
}

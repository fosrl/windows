//go:build windows

package ui

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/fosrl/windows/tunnel"
)

var (
	statusMu      sync.Mutex
	statusQuit    chan struct{}
	statusSites   = newSiteTracker()
	statusCurrent StatusView
	// statusLoaded is true once an OLM status has been read since polling started.
	statusLoaded bool
	// statusRefs counts the views polling: the preferences window and the
	// tray's sites submenu.
	statusRefs int
	// statusMenuVisible is true while the tray's sites submenu is open.
	statusMenuVisible bool
)

// startStatusPolling starts polling for one more view; pair each call with
// stopStatusPolling.
func startStatusPolling() {
	statusMu.Lock()
	defer statusMu.Unlock()
	statusRefs++
	if statusQuit != nil {
		return
	}
	statusLoaded = false
	statusSites.reset()
	statusQuit = make(chan struct{})
	quit := statusQuit
	statusCurrent = buildStatusView(currentTunnelState(), nil, time.Now())
	go pollStatus(quit)
}

func stopStatusPolling() {
	statusMu.Lock()
	defer statusMu.Unlock()
	if statusRefs > 0 {
		statusRefs--
	}
	if statusRefs == 0 && statusQuit != nil {
		close(statusQuit)
		statusQuit = nil
	}
}

// setMenuSitesVisible starts or stops polling for the tray's sites submenu.
func setMenuSitesVisible(visible bool) {
	statusMu.Lock()
	changed := statusMenuVisible != visible
	statusMenuVisible = visible
	statusMu.Unlock()
	if !changed {
		return
	}
	if visible {
		startStatusPolling()
	} else {
		stopStatusPolling()
	}
	publish()
}

func menuSitesVisible() bool {
	statusMu.Lock()
	defer statusMu.Unlock()
	return statusMenuVisible
}

// currentStatusSites returns the sites for the tray menu, and whether an OLM
// status has been read yet.
func currentStatusSites() ([]StatusSite, bool) {
	statusMu.Lock()
	defer statusMu.Unlock()
	return statusCurrent.Sites, statusLoaded
}

func currentStatusView() StatusView {
	statusMu.Lock()
	defer statusMu.Unlock()
	return statusCurrent
}

// pollStatus reads the tunnel state and OLM status once a second while the
// preferences window is open.
func pollStatus(quit chan struct{}) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-quit:
			return
		case <-ticker.C:
		}

		state := currentTunnelState()
		var status *tunnel.OLMStatusResponse
		if tunnelManager != nil {
			if s, err := tunnelManager.GetOLMStatus(); err == nil {
				status = s
			}
		}

		statusMu.Lock()
		if statusQuit != quit {
			statusMu.Unlock()
			return
		}
		statusCurrent = buildStatusView(state, status, time.Now())
		if status != nil {
			statusLoaded = true
		}
		view := statusCurrent
		menuVisible := statusMenuVisible
		statusMu.Unlock()

		app.Event.Emit(eventStatusUpdate, view)
		if menuVisible {
			publish()
		}
	}
}

// buildStatusView must be called with statusMu held.
func buildStatusView(state tunnel.State, status *tunnel.OLMStatusResponse, now time.Time) StatusView {
	view := StatusView{StateText: state.DisplayText(), Gateway: gatewayLabel(false, 0)}
	switch state {
	case tunnel.StateRunning:
		view.Color = statusColorGreen
	case tunnel.StateStopped:
		view.Color = statusColorGray
	default:
		view.Color = statusColorYellow
	}

	if status == nil {
		statusSites.update(nil, nil, now)
		view.Sites = []StatusSite{}
		view.JSON = "{\n  \"connected\": false\n}"
		return view
	}

	view.Version = status.Version
	view.Agent = status.Agent
	view.OrgID = status.OrgID
	view.Gateway = gatewayLabel(status.GatewayActive, status.GatewaySiteResourceID)

	gatewaySites := map[int]bool{}
	if status.GatewayActive {
		for _, id := range status.GatewaySiteIDs {
			gatewaySites[id] = true
		}
	}

	var exitNode *peerInput
	if status.ExitNode != nil {
		exitNode = &peerInput{
			Endpoint:  status.ExitNode.Endpoint,
			Connected: status.ExitNode.Connected,
			LastSeen:  status.ExitNode.LastSeen,
		}
	}
	peers := make([]peerInput, 0, len(status.PeerStatuses))
	for siteID, p := range status.PeerStatuses {
		if p == nil {
			continue
		}
		peers = append(peers, peerInput{
			ID:        siteID,
			Name:      p.SiteName,
			Endpoint:  p.Endpoint,
			Connected: p.Connected,
			LastSeen:  p.LastSeen,
			IsLocal:   p.IsLocal,
			IsRelay:   p.IsRelay,
			IsGateway: gatewaySites[siteID],
		})
	}
	view.Sites = statusSites.update(exitNode, peers, now)

	if data, err := json.MarshalIndent(status, "", "  "); err != nil {
		view.JSON = fmt.Sprintf("Error formatting JSON: %v", err)
	} else {
		view.JSON = string(data)
	}
	return view
}

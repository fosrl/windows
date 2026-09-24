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
)

func startStatusPolling() {
	statusMu.Lock()
	defer statusMu.Unlock()
	if statusQuit != nil {
		return
	}
	statusSites.reset()
	statusQuit = make(chan struct{})
	quit := statusQuit
	statusCurrent = buildStatusView(currentTunnelState(), nil, time.Now())
	go pollStatus(quit)
}

func stopStatusPolling() {
	statusMu.Lock()
	defer statusMu.Unlock()
	if statusQuit != nil {
		close(statusQuit)
		statusQuit = nil
	}
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
		view := statusCurrent
		statusMu.Unlock()

		app.Event.Emit(eventStatusUpdate, view)
	}
}

// buildStatusView must be called with statusMu held.
func buildStatusView(state tunnel.State, status *tunnel.OLMStatusResponse, now time.Time) StatusView {
	view := StatusView{StateText: state.DisplayText()}
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

	var exitNode *peerInput
	if status.ExitNode != nil {
		exitNode = &peerInput{Endpoint: status.ExitNode.Endpoint, Connected: status.ExitNode.Connected}
	}
	peers := make([]peerInput, 0, len(status.PeerStatuses))
	for siteID, p := range status.PeerStatuses {
		if p == nil {
			continue
		}
		peers = append(peers, peerInput{ID: siteID, Name: p.SiteName, Endpoint: p.Endpoint, Connected: p.Connected})
	}
	view.Sites = statusSites.update(exitNode, peers, now)

	if data, err := json.MarshalIndent(status, "", "  "); err != nil {
		view.JSON = fmt.Sprintf("Error formatting JSON: %v", err)
	} else {
		view.JSON = string(data)
	}
	return view
}

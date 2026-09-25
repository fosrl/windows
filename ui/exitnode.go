//go:build windows

package ui

import (
	"fmt"
	"strconv"
	"sync"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/api"
	"github.com/fosrl/windows/tunnel"
)

// Exit nodes are the org's gateway-mode site resources. Selecting one routes
// all tunnel traffic through its sites. The list comes from the server and is
// cached for the tray menu; the selection itself is saved in the config (by
// niceId) so it is re-applied on the next connect, and applied live through
// olm when the tunnel is already up.

var (
	exitNodeMu sync.Mutex
	// exitNodeList is the usable gateway resources of exitNodeListOrg.
	exitNodeList    []api.SiteResource
	exitNodeListOrg string
	// olmGatewayResourceID is the gateway resource olm reports it is routing
	// through while the tunnel is up (0 for none).
	olmGatewayResourceID int
)

// usableGateways drops gateway resources that can't be used as an exit node.
func usableGateways(all []api.SiteResource) []api.SiteResource {
	var usable []api.SiteResource
	for _, g := range all {
		if g.Enabled && len(g.SiteIDs) > 0 {
			usable = append(usable, g)
		}
	}
	return usable
}

// currentExitNodes returns the cached exit nodes for orgID (empty if the cache
// belongs to another org) and the ID of the active one, or 0 for none. While the
// tunnel is up the active one is what olm reports; otherwise it is the saved one.
func currentExitNodes(orgID string, running bool) (nodes []menuExitNode, activeID int) {
	exitNodeMu.Lock()
	defer exitNodeMu.Unlock()

	if orgID == "" || exitNodeListOrg != orgID {
		return nil, 0
	}

	savedOrg, savedNiceID := "", ""
	if configManager != nil {
		savedOrg, savedNiceID = configManager.GetExitNode()
	}
	for _, g := range exitNodeList {
		nodes = append(nodes, menuExitNode{ID: g.SiteResourceID, Name: g.Name})
		if !running && savedNiceID != "" && g.NiceID == savedNiceID && (savedOrg == "" || savedOrg == orgID) {
			activeID = g.SiteResourceID
		}
	}
	if running {
		activeID = olmGatewayResourceID
	}
	return nodes, activeID
}

func setOLMGatewayResourceID(id int) {
	exitNodeMu.Lock()
	olmGatewayResourceID = id
	exitNodeMu.Unlock()
}

// refreshOLMGateway reads the gateway selection olm is currently using.
func refreshOLMGateway() {
	if tunnelManager == nil || tunnelManager.State() != tunnel.StateRunning {
		setOLMGatewayResourceID(0)
		return
	}
	status, err := tunnelManager.GetOLMStatus()
	if err != nil {
		logger.Error("Failed to read exit node status from OLM: %v", err)
		return
	}
	if status.GatewayActive {
		setOLMGatewayResourceID(status.GatewaySiteResourceID)
	} else {
		setOLMGatewayResourceID(0)
	}
}

// refreshExitNodes reloads the org's exit nodes and olm's gateway state, then
// republishes the menu. It runs in the background.
func refreshExitNodes() {
	if authManager == nil || apiClient == nil || !authManager.IsAuthenticated() {
		return
	}
	org := authManager.CurrentOrg()
	if org == nil {
		return
	}
	orgID := org.Id

	go func() {
		gateways, err := apiClient.ListGatewayResources(orgID)
		if err != nil {
			// Keep whatever we had; the server may just be unreachable.
			logger.Error("Failed to list exit nodes: %v", err)
		} else {
			exitNodeMu.Lock()
			exitNodeList = usableGateways(gateways)
			exitNodeListOrg = orgID
			exitNodeMu.Unlock()
		}
		refreshOLMGateway()
		publish()
	}()
}

// onTunnelStateForExitNodes keeps exit node state in step with the tunnel.
func onTunnelStateForExitNodes(state tunnel.State) {
	switch state {
	case tunnel.StateRunning:
		// Connecting applies the saved exit node, so pick up the result.
		refreshExitNodes()
	case tunnel.StateStopped:
		setOLMGatewayResourceID(0)
	}
}

func findExitNode(orgID string, id int) (api.SiteResource, bool) {
	exitNodeMu.Lock()
	defer exitNodeMu.Unlock()
	if exitNodeListOrg != orgID {
		return api.SiteResource{}, false
	}
	for _, g := range exitNodeList {
		if g.SiteResourceID == id {
			return g, true
		}
	}
	return api.SiteResource{}, false
}

// selectExitNode routes all traffic through the exit node with the given
// gateway resource ID (as a string, from the menu item ID). With the tunnel up
// it takes effect immediately; otherwise it is applied on the next connect.
func selectExitNode(idStr string) {
	if authManager == nil || configManager == nil || tunnelManager == nil {
		return
	}
	org := authManager.CurrentOrg()
	id, err := strconv.Atoi(idStr)
	if org == nil || err != nil {
		logger.Error("Invalid exit node selection %q", idStr)
		return
	}

	gateway, ok := findExitNode(org.Id, id)
	if !ok {
		logger.Error("Exit node %d no longer exists", id)
		refreshExitNodes()
		return
	}

	if tunnelManager.State() == tunnel.StateRunning {
		if err := tunnelManager.SelectGateway(gateway.SiteResourceID, gateway.SiteIDs); err != nil {
			logger.Error("Failed to select exit node: %v", err)
			showError(nil, "Exit Node Selection Failed", fmt.Sprintf("Failed to route traffic through %s: %v", gateway.Name, err))
			refreshExitNodes()
			return
		}
		setOLMGatewayResourceID(gateway.SiteResourceID)
	}

	if !configManager.SetExitNode(org.Id, gateway.NiceID) {
		logger.Warn("Exit node applied but could not be saved for the next connect")
	}
	logger.Info("Exit node set to %s", gateway.Name)
	publish()
}

// disableExitNode stops routing traffic through an exit node and forgets the
// saved selection.
func disableExitNode() {
	if configManager == nil || tunnelManager == nil {
		return
	}

	if tunnelManager.State() == tunnel.StateRunning {
		if err := tunnelManager.DisableGateway(); err != nil {
			logger.Error("Failed to disable exit node: %v", err)
			showError(nil, "Exit Node Failed", fmt.Sprintf("Failed to disable the exit node: %v", err))
			refreshExitNodes()
			return
		}
		setOLMGatewayResourceID(0)
	}

	configManager.SetExitNode("", "")
	logger.Info("Exit node disabled")
	publish()
}

// resolveSavedExitNode turns the saved exit node into the resource and site IDs
// to establish when connecting, or 0/nil to connect without one. Only the
// niceId is saved, so a deleted, disabled or site-less resource is skipped.
func resolveSavedExitNode(orgID string) (int, []int) {
	if configManager == nil || apiClient == nil {
		return 0, nil
	}
	savedOrg, niceID := configManager.GetExitNode()
	if niceID == "" {
		return 0, nil
	}
	if savedOrg != "" && savedOrg != orgID {
		logger.Info("Saved exit node '%s' belongs to a different organization; not using it", niceID)
		return 0, nil
	}

	gateways, err := apiClient.ListGatewayResources(orgID)
	if err != nil {
		logger.Warn("Could not look up saved exit node '%s' (%v); connecting without it", niceID, err)
		return 0, nil
	}
	for _, g := range gateways {
		if g.NiceID != niceID {
			continue
		}
		if !g.Enabled || len(g.SiteIDs) == 0 {
			logger.Warn("Saved exit node '%s' is disabled or has no sites; not using it", niceID)
			return 0, nil
		}
		return g.SiteResourceID, g.SiteIDs
	}
	logger.Warn("Saved exit node '%s' no longer exists; not using it", niceID)
	return 0, nil
}

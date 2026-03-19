//go:build windows

package preferences

import (
	"encoding/json"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/tunnel"

	"github.com/tailscale/walk"
	"github.com/tailscale/win"
)

// DisplayMode represents the display mode for OLM status
type DisplayMode int

const (
	DisplayModeFormatted DisplayMode = iota
	DisplayModeJSON
)

// statusWidgets holds references to status display widgets
type statusWidgets struct {
	statusIndicator *walk.Label
	statusText      *walk.Label
	versionLabel    *walk.Label
	versionRow      *walk.Composite
	agentLabel      *walk.Label
	agentRow        *walk.Composite
	orgLabel        *walk.Label
	orgRow          *walk.Composite
}

// peerWidgets holds references to a peer's display widgets
type peerWidgets struct {
	row           *walk.Composite
	nameLabel     *walk.Label
	endpointLabel *walk.Label
	indicator     *walk.Label
	statusLabel   *walk.Label
	firstSeen     time.Time
	rowVisible    bool
}

const peerConnectingTimeout = 10 * time.Second

// OLMStatusTab handles the OLM status viewing tab
type OLMStatusTab struct {
	tabPage       *walk.TabPage
	tunnelManager *tunnel.Manager
	quit          chan bool
	mu            sync.Mutex

	// Inner tab widget for Formatted/JSON views
	innerTabWidget *walk.TabWidget
	formattedTab   *walk.TabPage
	jsonTab        *walk.TabPage

	// JSON view
	jsonEdit *walk.TextEdit

	// Formatted view
	formattedContainer *walk.Composite
	statusContainer    *walk.Composite
	peersContainer     *walk.Composite
	noSitesLabel       *walk.Label

	// Widget references for updating (protected by mu)
	statusWidgets *statusWidgets
	peerWidgets   map[int]*peerWidgets // keyed by siteID

	// Current status (protected by mu)
	currentStatus *tunnel.OLMStatusResponse
	// Current tunnel state (protected by mu)
	currentTunnelState tunnel.State
	displayMode   DisplayMode
}

// NewOLMStatusTab creates a new OLM status tab
func NewOLMStatusTab(tm *tunnel.Manager) *OLMStatusTab {
	var state tunnel.State
	if tm != nil {
		state = tm.State()
	} else {
		state = tunnel.StateStopped
	}
	return &OLMStatusTab{
		tunnelManager: tm,
		quit:          make(chan bool),
		peerWidgets:   make(map[int]*peerWidgets),
		currentTunnelState: state,
		displayMode:   DisplayModeFormatted, // Default to formatted view
	}
}

// Create creates the OLM status tab UI
func (ost *OLMStatusTab) Create(parent *walk.TabWidget) (*walk.TabPage, error) {
	var err error
	if ost.tabPage, err = walk.NewTabPage(); err != nil {
		return nil, err
	}

	ost.tabPage.SetTitle("Status")
	ost.tabPage.SetLayout(walk.NewVBoxLayout())

	// Create inner tab widget for Formatted/JSON views
	if ost.innerTabWidget, err = walk.NewTabWidget(ost.tabPage); err != nil {
		return nil, err
	}

	// Create Formatted tab
	if ost.formattedTab, err = walk.NewTabPage(); err != nil {
		return nil, err
	}
	ost.formattedTab.SetTitle("Formatted")
	ost.formattedTab.SetLayout(walk.NewVBoxLayout())

	// Formatted view container
	if ost.formattedContainer, err = walk.NewComposite(ost.formattedTab); err != nil {
		return nil, err
	}
	formattedLayout := walk.NewVBoxLayout()
	formattedLayout.SetMargins(walk.Margins{})
	formattedLayout.SetSpacing(16)
	ost.formattedContainer.SetLayout(formattedLayout)

	// Connection Status section
	statusSectionLabel, err := walk.NewLabel(ost.formattedContainer)
	if err != nil {
		return nil, err
	}
	statusSectionLabel.SetText("Connection Status")
	font, err := walk.NewFont("Segoe UI", 10, walk.FontBold)
	if err == nil {
		statusSectionLabel.SetFont(font)
	}

	// Status container
	if ost.statusContainer, err = walk.NewComposite(ost.formattedContainer); err != nil {
		return nil, err
	}
	statusLayout := walk.NewVBoxLayout()
	statusLayout.SetMargins(walk.Margins{})
	statusLayout.SetSpacing(8)
	ost.statusContainer.SetLayout(statusLayout)

	// Create status widgets once (will be updated, not recreated)
	if err := ost.createStatusWidgets(); err != nil {
		return nil, err
	}

	// Seed the UI with the current tunnel state immediately so transitional phases
	// (e.g. Registering...) show up even before OLM status returns a response.
	if ost.tunnelManager != nil {
		ost.mu.Lock()
		ost.currentTunnelState = ost.tunnelManager.State()
		ost.mu.Unlock()
	}

	// Peers section
	peersSectionLabel, err := walk.NewLabel(ost.formattedContainer)
	if err != nil {
		return nil, err
	}
	peersSectionLabel.SetText("Sites")
	if font, err := walk.NewFont("Segoe UI", 10, walk.FontBold); err == nil {
		peersSectionLabel.SetFont(font)
	}

	// No sites connected label (initially visible)
	// Place directly in formattedContainer to align with "Sites" header
	if ost.noSitesLabel, err = walk.NewLabel(ost.formattedContainer); err != nil {
		return nil, err
	}
	ost.noSitesLabel.SetText("No sites connected")
	ost.noSitesLabel.SetTextColor(walk.RGB(100, 100, 100))

	// Peers container
	if ost.peersContainer, err = walk.NewComposite(ost.formattedContainer); err != nil {
		return nil, err
	}
	peersLayout := walk.NewVBoxLayout()
	peersLayout.SetMargins(walk.Margins{})
	peersLayout.SetSpacing(8)
	ost.peersContainer.SetLayout(peersLayout)

	// Add spacer to fill remaining space
	walk.NewVSpacer(ost.formattedContainer)

	// Add formatted tab to inner tab widget
	ost.innerTabWidget.Pages().Add(ost.formattedTab)

	// Create JSON tab
	if ost.jsonTab, err = walk.NewTabPage(); err != nil {
		return nil, err
	}
	ost.jsonTab.SetTitle("JSON")
	ost.jsonTab.SetLayout(walk.NewVBoxLayout())

	// JSON view
	if ost.jsonEdit, err = walk.NewTextEdit(ost.jsonTab); err != nil {
		return nil, err
	}
	ost.jsonEdit.SetReadOnly(true)

	// Enable multiline and scrolling for large JSON content
	hwnd := ost.jsonEdit.Handle()
	style := win.GetWindowLong(hwnd, win.GWL_STYLE)
	style |= win.ES_MULTILINE | win.ES_AUTOVSCROLL | win.ES_AUTOHSCROLL | win.WS_VSCROLL | win.WS_HSCROLL
	win.SetWindowLong(hwnd, win.GWL_STYLE, style)

	// Set monospace font for better JSON readability
	if font, err := walk.NewFont("Consolas", 10, 0); err == nil {
		ost.jsonEdit.SetFont(font)
	} else if font, err := walk.NewFont("Courier New", 10, 0); err == nil {
		ost.jsonEdit.SetFont(font)
	}

	// Add JSON tab to inner tab widget
	ost.innerTabWidget.Pages().Add(ost.jsonTab)

	// Track which tab is active and only update that tab's content
	ost.innerTabWidget.CurrentIndexChanged().Attach(func() {
		ost.mu.Lock()
		currentIndex := ost.innerTabWidget.CurrentIndex()
		if currentIndex == 0 {
			ost.displayMode = DisplayModeFormatted
		} else {
			ost.displayMode = DisplayModeJSON
		}
		ost.mu.Unlock()
		// Update the newly visible tab
		ost.updateUI()
	})

	// Start OLM status polling
	go ost.pollOLMStatus()

	return ost.tabPage, nil
}

// createStatusWidgets creates the status widgets once (they will be updated, not recreated)
func (ost *OLMStatusTab) createStatusWidgets() error {
	ost.statusWidgets = &statusWidgets{}

	// Status row
	statusRow, err := walk.NewComposite(ost.statusContainer)
	if err != nil {
		return err
	}
	statusRowLayout := walk.NewHBoxLayout()
	statusRowLayout.SetMargins(walk.Margins{})
	statusRowLayout.SetSpacing(12)
	statusRow.SetLayout(statusRowLayout)

	statusLabel, err := walk.NewLabel(statusRow)
	if err != nil {
		return err
	}
	statusLabel.SetText("Status")
	statusLabel.SetMinMaxSize(walk.Size{Width: 200, Height: 0}, walk.Size{Width: 200, Height: 0})

	valueContainer, err := walk.NewComposite(statusRow)
	if err != nil {
		return err
	}
	valueLayout := walk.NewHBoxLayout()
	valueLayout.SetMargins(walk.Margins{})
	valueLayout.SetSpacing(6)
	valueContainer.SetLayout(valueLayout)

	// Status indicator
	ost.statusWidgets.statusIndicator, err = walk.NewLabel(valueContainer)
	if err != nil {
		return err
	}
	ost.statusWidgets.statusIndicator.SetText("●")
	ost.statusWidgets.statusIndicator.SetMinMaxSize(walk.Size{Width: 15, Height: 15}, walk.Size{Width: 15, Height: 15})

	// Status text
	ost.statusWidgets.statusText, err = walk.NewLabel(valueContainer)
	if err != nil {
		return err
	}
	ost.statusWidgets.statusText.SetTextColor(walk.RGB(100, 100, 100))
	// Initialize to disconnected state
	ost.statusWidgets.statusIndicator.SetTextColor(walk.RGB(150, 150, 150))
	ost.statusWidgets.statusText.SetText("Disconnected")

	walk.NewHSpacer(statusRow)

	// Version row (initially hidden)
	ost.statusWidgets.versionRow, err = walk.NewComposite(ost.statusContainer)
	if err != nil {
		return err
	}
	versionRowLayout := walk.NewHBoxLayout()
	versionRowLayout.SetMargins(walk.Margins{})
	versionRowLayout.SetSpacing(12)
	ost.statusWidgets.versionRow.SetLayout(versionRowLayout)

	versionLabel, err := walk.NewLabel(ost.statusWidgets.versionRow)
	if err != nil {
		return err
	}
	versionLabel.SetText("Version")
	versionLabel.SetMinMaxSize(walk.Size{Width: 200, Height: 0}, walk.Size{Width: 200, Height: 0})

	ost.statusWidgets.versionLabel, err = walk.NewLabel(ost.statusWidgets.versionRow)
	if err != nil {
		return err
	}
	ost.statusWidgets.versionLabel.SetTextColor(walk.RGB(100, 100, 100))

	walk.NewHSpacer(ost.statusWidgets.versionRow)
	ost.statusWidgets.versionRow.SetVisible(false)

	// Agent row (initially hidden)
	ost.statusWidgets.agentRow, err = walk.NewComposite(ost.statusContainer)
	if err != nil {
		return err
	}
	agentRowLayout := walk.NewHBoxLayout()
	agentRowLayout.SetMargins(walk.Margins{})
	agentRowLayout.SetSpacing(12)
	ost.statusWidgets.agentRow.SetLayout(agentRowLayout)

	agentLabel, err := walk.NewLabel(ost.statusWidgets.agentRow)
	if err != nil {
		return err
	}
	agentLabel.SetText("Agent")
	agentLabel.SetMinMaxSize(walk.Size{Width: 200, Height: 0}, walk.Size{Width: 200, Height: 0})

	ost.statusWidgets.agentLabel, err = walk.NewLabel(ost.statusWidgets.agentRow)
	if err != nil {
		return err
	}
	ost.statusWidgets.agentLabel.SetTextColor(walk.RGB(100, 100, 100))

	walk.NewHSpacer(ost.statusWidgets.agentRow)
	ost.statusWidgets.agentRow.SetVisible(false)

	// Organization row (initially hidden)
	ost.statusWidgets.orgRow, err = walk.NewComposite(ost.statusContainer)
	if err != nil {
		return err
	}
	orgRowLayout := walk.NewHBoxLayout()
	orgRowLayout.SetMargins(walk.Margins{})
	orgRowLayout.SetSpacing(12)
	ost.statusWidgets.orgRow.SetLayout(orgRowLayout)

	orgLabel, err := walk.NewLabel(ost.statusWidgets.orgRow)
	if err != nil {
		return err
	}
	orgLabel.SetText("Organization")
	orgLabel.SetMinMaxSize(walk.Size{Width: 200, Height: 0}, walk.Size{Width: 200, Height: 0})

	ost.statusWidgets.orgLabel, err = walk.NewLabel(ost.statusWidgets.orgRow)
	if err != nil {
		return err
	}
	ost.statusWidgets.orgLabel.SetTextColor(walk.RGB(100, 100, 100))

	walk.NewHSpacer(ost.statusWidgets.orgRow)
	ost.statusWidgets.orgRow.SetVisible(false)

	return nil
}

// AfterAdd is called after the tab page is added to the tab widget
func (ost *OLMStatusTab) AfterAdd() {
	// Nothing to do for OLM status tab
}

// Cleanup cleans up resources when the tab is closed
func (ost *OLMStatusTab) Cleanup() {
	ost.mu.Lock()
	defer ost.mu.Unlock()

	if ost.quit != nil {
		select {
		case <-ost.quit:
			// Already closed
		default:
			close(ost.quit)
		}
	}
}

func (ost *OLMStatusTab) pollOLMStatus() {
	if ost.tunnelManager == nil {
		// Just set status to nil and show disconnected state
		ost.mu.Lock()
		ost.currentStatus = nil
		ost.currentTunnelState = tunnel.StateStopped
		ost.mu.Unlock()
		walk.App().Synchronize(func() {
			ost.updateUI()
		})
		return
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	lastState := ost.tunnelManager.State()
	for {
		select {
		case <-ost.quit:
			return
		case <-ticker.C:
			// Always read tunnel state so the UI can move out of "Disconnected"
			// even when OLM status isn't available yet.
			state := ost.tunnelManager.State()
			stateChanged := state != lastState
			if stateChanged {
				ost.mu.Lock()
				ost.currentTunnelState = state
				ost.mu.Unlock()
			}

			status, err := ost.tunnelManager.GetOLMStatus()
			if err != nil {
				// Show disconnected state instead of error message
				ost.mu.Lock()
				// Only update if status changed from non-nil to nil
				if ost.currentStatus != nil {
					ost.currentStatus = nil
					ost.mu.Unlock()
					walk.App().Synchronize(func() {
						ost.updateUI()
					})
				} else if stateChanged {
					ost.mu.Unlock()
					walk.App().Synchronize(func() {
						ost.updateUI()
					})
				} else {
					ost.mu.Unlock()
				}
				lastState = state
				continue
			}

			// Update current status
			ost.mu.Lock()
			ost.currentStatus = status
			ost.mu.Unlock()

			// Update UI (state or OLM details changed)
			walk.App().Synchronize(func() {
				ost.updateUI()
			})

			lastState = state
		}
	}
}

// updateUI updates the UI based on current status and display mode
func (ost *OLMStatusTab) updateUI() {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("OLM status tab panic in updateUI: %v\n%s", r, debug.Stack())
		}
	}()
	ost.mu.Lock()
	status := ost.currentStatus
	tstate := ost.currentTunnelState
	ost.mu.Unlock()

	// Check which tab is actually visible (outside lock)
	currentIndex := ost.innerTabWidget.CurrentIndex()

	// Only update the active tab
	if currentIndex == 0 {
		// Formatted tab is active
		ost.updateFormattedView(status, tstate)
	} else {
		// JSON tab is active
		ost.updateJSONView(status)
	}
}

// updateJSONView updates the JSON view
func (ost *OLMStatusTab) updateJSONView(status *tunnel.OLMStatusResponse) {
	// No need to set visibility - tabs handle that automatically

	if status == nil {
		ost.jsonEdit.SetText("Disconnected")
		return
	}

	// Format JSON with indentation
	jsonData, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		ost.jsonEdit.SetText(fmt.Sprintf("Error formatting JSON: %v", err))
		return
	}

	// Convert Unix newlines to Windows line breaks for proper display
	jsonText := strings.ReplaceAll(string(jsonData), "\n", "\r\n")
	ost.jsonEdit.SetText(jsonText)
}

// updateFormattedView updates the formatted view by updating existing widgets
func (ost *OLMStatusTab) updateFormattedView(status *tunnel.OLMStatusResponse, state tunnel.State) {
	// No need to set visibility - tabs handle that automatically

	if ost.statusWidgets == nil {
		return
	}

	// Update overall connection status from tunnel state (matches tray behavior).
	switch state {
	case tunnel.StateRunning:
		ost.statusWidgets.statusIndicator.SetTextColor(walk.RGB(0, 200, 0))
	case tunnel.StateStopped:
		ost.statusWidgets.statusIndicator.SetTextColor(walk.RGB(150, 150, 150))
	default:
		ost.statusWidgets.statusIndicator.SetTextColor(walk.RGB(255, 200, 0))
	}
	ost.statusWidgets.statusText.SetText(state.DisplayText())

	if status == nil {
		ost.statusWidgets.versionRow.SetVisible(false)
		ost.statusWidgets.agentRow.SetVisible(false)
		ost.statusWidgets.orgRow.SetVisible(false)
		ost.updatePeersList(status)
		return
	}

	// Update version
	if status.Version != "" {
		ost.statusWidgets.versionLabel.SetText(status.Version)
		ost.statusWidgets.versionRow.SetVisible(true)
	} else {
		ost.statusWidgets.versionRow.SetVisible(false)
	}

	// Update agent
	if status.Agent != "" {
		ost.statusWidgets.agentLabel.SetText(status.Agent)
		ost.statusWidgets.agentRow.SetVisible(true)
	} else {
		ost.statusWidgets.agentRow.SetVisible(false)
	}

	// Update organization
	if status.OrgID != "" {
		ost.statusWidgets.orgLabel.SetText(status.OrgID)
		ost.statusWidgets.orgRow.SetVisible(true)
	} else {
		ost.statusWidgets.orgRow.SetVisible(false)
	}

	// Update peers list
	ost.updatePeersList(status)
}

// formatStatus formats the connection status text
func (ost *OLMStatusTab) formatStatus(connected, registered bool) string {
	if connected {
		return "Connected"
	}
	if registered {
		return "Connecting..."
	}
	return "Registering..."
}

// updatePeersList updates the peers container, reusing existing widgets when possible
func (ost *OLMStatusTab) updatePeersList(status *tunnel.OLMStatusResponse) {
	if status == nil || status.PeerStatuses == nil || len(status.PeerStatuses) == 0 {
		ost.mu.Lock()
		// Hide all peer widgets
		for _, pw := range ost.peerWidgets {
			if pw.row != nil {
				pw.row.SetVisible(false)
				pw.rowVisible = false
				pw.firstSeen = time.Time{}
			}
		}
		ost.mu.Unlock()
		// Show "No sites connected" message
		if ost.noSitesLabel != nil {
			ost.noSitesLabel.SetVisible(true)
		}
		return
	}

	// Hide "No sites connected" message when there are peers
	if ost.noSitesLabel != nil {
		ost.noSitesLabel.SetVisible(false)
	}

	// Track which peers we've seen and which need to be created
	seenPeers := make(map[int]bool)
	peersToCreate := make([]struct {
		siteID    int
		name      string
		endpoint  string
		connected bool
	}, 0)

	ost.mu.Lock()
	// First pass: update existing widgets and identify new ones
	for siteID, peer := range status.PeerStatuses {
		seenPeers[siteID] = true

		pw, exists := ost.peerWidgets[siteID]
		if !exists {
			// Mark for creation (outside lock)
			peersToCreate = append(peersToCreate, struct {
				siteID    int
				name      string
				endpoint  string
				connected bool
			}{siteID, peer.SiteName, peer.Endpoint, peer.Connected})
		} else {
			// Update existing peer widget
			if pw.nameLabel != nil {
				name := peer.SiteName
				if name == "" {
					name = "Unknown"
				}
				pw.nameLabel.SetText(name)
			}
			if pw.endpointLabel != nil {
				if peer.Endpoint != "" {
					pw.endpointLabel.SetText(peer.Endpoint)
					pw.endpointLabel.SetVisible(true)
				} else {
					pw.endpointLabel.SetVisible(false)
				}
			}

			// Mark as visible and (re)start the "first seen" timer when it becomes
			// visible again (e.g. reappears after being hidden).
			if pw.row != nil {
				pw.row.SetVisible(true)
			}
			if !pw.rowVisible || pw.firstSeen.IsZero() {
				pw.firstSeen = time.Now()
			}
			pw.rowVisible = true

			// Update per-site status with a 10-second connecting window.
			if peer.Connected {
				if pw.indicator != nil {
					pw.indicator.SetTextColor(walk.RGB(0, 200, 0))
				}
				if pw.statusLabel != nil {
					pw.statusLabel.SetText("Connected")
				}
			} else {
				// Not connected yet: show "Connecting" until timeout, then "Disconnected".
				if pw.firstSeen.IsZero() {
					pw.firstSeen = time.Now()
				}
				elapsed := time.Since(pw.firstSeen)
				if elapsed >= peerConnectingTimeout {
					if pw.indicator != nil {
						pw.indicator.SetTextColor(walk.RGB(150, 150, 150))
					}
					if pw.statusLabel != nil {
						pw.statusLabel.SetText("Disconnected")
					}
				} else {
					if pw.indicator != nil {
						pw.indicator.SetTextColor(walk.RGB(255, 200, 0))
					}
					if pw.statusLabel != nil {
						pw.statusLabel.SetText("Connecting")
					}
				}
			}
		}
	}

	// Hide peers that are no longer present
	for siteID, pw := range ost.peerWidgets {
		if !seenPeers[siteID] && pw.row != nil {
			pw.row.SetVisible(false)
			pw.rowVisible = false
			pw.firstSeen = time.Time{}
		}
	}
	ost.mu.Unlock()

	// Create new peer widgets (outside lock, as it creates UI widgets)
	for _, peerInfo := range peersToCreate {
		if err := ost.createPeerWidget(peerInfo.siteID, peerInfo.name, peerInfo.endpoint, peerInfo.connected); err != nil {
			continue
		}
	}
}

// createPeerWidget creates a new peer widget row
func (ost *OLMStatusTab) createPeerWidget(siteID int, name, endpoint string, connected bool) error {
	pw := &peerWidgets{}

	ost.mu.Lock()
	// Check if it was already created by another goroutine
	if _, exists := ost.peerWidgets[siteID]; exists {
		ost.mu.Unlock()
		return nil
	}
	ost.mu.Unlock()

	// Record first time this peer widget was created (used for the connecting timeout).
	pw.firstSeen = time.Now()
	pw.rowVisible = true

	row, err := walk.NewComposite(ost.peersContainer)
	if err != nil {
		return err
	}
	pw.row = row
	rowLayout := walk.NewHBoxLayout()
	rowLayout.SetMargins(walk.Margins{})
	rowLayout.SetSpacing(12)
	row.SetLayout(rowLayout)

	// Peer name and endpoint container - aligned with label column (200px width)
	nameContainer, err := walk.NewComposite(row)
	if err != nil {
		return err
	}
	nameLayout := walk.NewVBoxLayout()
	nameLayout.SetMargins(walk.Margins{})
	nameLayout.SetSpacing(2)
	nameContainer.SetLayout(nameLayout)
	nameContainer.SetMinMaxSize(walk.Size{Width: 200, Height: 0}, walk.Size{Width: 200, Height: 0})

	// Peer name
	pw.nameLabel, err = walk.NewLabel(nameContainer)
	if err != nil {
		return err
	}
	if name == "" {
		name = "Unknown"
	}
	pw.nameLabel.SetText(name)

	// Endpoint (if available)
	if endpoint != "" {
		pw.endpointLabel, err = walk.NewLabel(nameContainer)
		if err == nil {
			pw.endpointLabel.SetText(endpoint)
			pw.endpointLabel.SetTextColor(walk.RGB(100, 100, 100))
		}
	}

	// Status indicator and text - aligned with value column (after 200px label + 12px spacing)
	statusContainer, err := walk.NewComposite(row)
	if err != nil {
		return err
	}
	statusLayout := walk.NewHBoxLayout()
	statusLayout.SetMargins(walk.Margins{})
	statusLayout.SetSpacing(6)
	statusContainer.SetLayout(statusLayout)

	// Status indicator circle
	pw.indicator, err = walk.NewLabel(statusContainer)
	if err != nil {
		return err
	}
	pw.indicator.SetText("●")
	if connected {
		pw.indicator.SetTextColor(walk.RGB(0, 200, 0))
	} else {
		// When a site first shows up but isn't connected yet, show it as "Connecting".
		pw.indicator.SetTextColor(walk.RGB(255, 200, 0))
	}
	pw.indicator.SetMinMaxSize(walk.Size{Width: 12, Height: 12}, walk.Size{Width: 12, Height: 12})

	// Status text
	statusText := "Connected"
	if !connected {
		statusText = "Connecting"
	}
	pw.statusLabel, err = walk.NewLabel(statusContainer)
	if err != nil {
		return err
	}
	pw.statusLabel.SetText(statusText)
	pw.statusLabel.SetTextColor(walk.RGB(100, 100, 100))

	// Add spacer to match status row structure
	walk.NewHSpacer(row)

	ost.mu.Lock()
	ost.peerWidgets[siteID] = pw
	ost.mu.Unlock()
	return nil
}

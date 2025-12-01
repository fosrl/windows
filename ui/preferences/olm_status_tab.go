//go:build windows

package preferences

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fosrl/windows/tunnel"

	"github.com/tailscale/walk"
	"github.com/tailscale/win"
)

// OLMStatusTab handles the OLM status viewing tab
type OLMStatusTab struct {
	tabPage       *walk.TabPage
	statusEdit    *walk.TextEdit
	tunnelManager *tunnel.Manager
	quit          chan bool
	mu            sync.Mutex
}

// NewOLMStatusTab creates a new OLM status tab
func NewOLMStatusTab(tm *tunnel.Manager) *OLMStatusTab {
	return &OLMStatusTab{
		tunnelManager: tm,
		quit:          make(chan bool),
	}
}

// Create creates the OLM status tab UI
func (ost *OLMStatusTab) Create(parent *walk.TabWidget) (*walk.TabPage, error) {
	var err error
	if ost.tabPage, err = walk.NewTabPage(); err != nil {
		return nil, err
	}

	ost.tabPage.SetTitle("OLM Status")
	ost.tabPage.SetLayout(walk.NewVBoxLayout())

	if ost.statusEdit, err = walk.NewTextEdit(ost.tabPage); err != nil {
		return nil, err
	}
	ost.statusEdit.SetReadOnly(true)
	ost.statusEdit.SetText("Loading OLM status...")

	// Enable multiline and scrolling for large JSON content
	// Get the window handle and set multiline/scroll styles
	hwnd := ost.statusEdit.Handle()
	style := win.GetWindowLong(hwnd, win.GWL_STYLE)
	style |= win.ES_MULTILINE | win.ES_AUTOVSCROLL | win.ES_AUTOHSCROLL | win.WS_VSCROLL | win.WS_HSCROLL
	win.SetWindowLong(hwnd, win.GWL_STYLE, style)

	// Set monospace font for better JSON readability
	if font, err := walk.NewFont("Consolas", 10, 0); err == nil {
		ost.statusEdit.SetFont(font)
	} else if font, err := walk.NewFont("Courier New", 10, 0); err == nil {
		// Fallback to Courier New if Consolas is not available
		ost.statusEdit.SetFont(font)
	}

	// Start OLM status polling
	go ost.pollOLMStatus()

	return ost.tabPage, nil
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
		walk.App().Synchronize(func() {
			ost.statusEdit.SetText("Tunnel manager not available")
		})
		return
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ost.quit:
			return
		case <-ticker.C:
			status, err := ost.tunnelManager.GetOLMStatus()
			if err != nil {
				walk.App().Synchronize(func() {
					ost.statusEdit.SetText("Unable to get status via pipe. Is the tunnel service running?")
				})
				continue
			}

			// Format JSON with indentation
			jsonData, err := json.MarshalIndent(status, "", "  ")
			if err != nil {
				walk.App().Synchronize(func() {
					ost.statusEdit.SetText(fmt.Sprintf("Error formatting JSON: %v", err))
				})
				continue
			}

			// Convert Unix newlines to Windows line breaks for proper display
			jsonText := strings.ReplaceAll(string(jsonData), "\n", "\r\n")

			// Update the text edit with formatted JSON
			walk.App().Synchronize(func() {
				ost.statusEdit.SetText(jsonText)
			})
		}
	}
}

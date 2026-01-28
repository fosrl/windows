//go:build windows

package preferences

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/fosrl/windows/config"
	"github.com/fosrl/windows/tunnel"

	"github.com/fosrl/newt/logger"
	"github.com/tailscale/walk"
	"github.com/tailscale/win"
)

// PreferencesWindow manages the preferences window with multiple tabs
type PreferencesWindow struct {
	*walk.Dialog
	tabWidget     *walk.TabWidget
	tunnelManager *tunnel.Manager
	configManager *config.ConfigManager
	trayIcon      *walk.NotifyIcon
	tabs          []Tab
}

// Tab represents a tab in the preferences window
type Tab interface {
	// Create creates the tab UI and returns the tab page
	Create(parent *walk.TabWidget) (*walk.TabPage, error)
	// AfterAdd is called after the tab page is added to the tab widget
	// This allows tabs to perform any initialization that requires the tab to be in the widget tree
	AfterAdd()
	// Cleanup is called when the window is closing to clean up resources
	Cleanup()
}

var (
	preferencesWindowInstance *PreferencesWindow
	preferencesWindowMutex    sync.Mutex
)

// ShowPreferencesWindow shows the preferences window (creates if needed, or brings to front).
// It accepts a tunnel manager to enable OLM status polling, a config manager for settings, and a tray icon for notifications.
func ShowPreferencesWindow(owner walk.Form, tm *tunnel.Manager, cm *config.ConfigManager, trayIcon *walk.NotifyIcon) error {
	preferencesWindowMutex.Lock()
	defer preferencesWindowMutex.Unlock()

	if preferencesWindowInstance != nil {
		// Check if the window is still valid (not closed)
		if preferencesWindowInstance.Handle() != 0 {
			// Focus the existing window using Windows API
			hwnd := preferencesWindowInstance.Handle()
			win.ShowWindow(hwnd, win.SW_RESTORE)
			win.SetForegroundWindow(hwnd)
			return nil
		}
		// Window was closed, clear the reference
		preferencesWindowInstance = nil
	}

	// Create new window
	pw, err := NewPreferencesWindow(owner, tm, cm, trayIcon)
	if err != nil {
		return err
	}

	preferencesWindowInstance = pw

	// Clean up when window closes
	pw.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
		preferencesWindowMutex.Lock()
		if preferencesWindowInstance == pw {
			preferencesWindowInstance = nil
		}
		preferencesWindowMutex.Unlock()

		// Cleanup all tabs
		for _, tab := range pw.tabs {
			tab.Cleanup()
		}
	})

	// Show the dialog (non-modal, doesn't block)
	pw.SetVisible(true)
	return nil
}

// NewPreferencesWindow creates a new preferences window with tabs
func NewPreferencesWindow(owner walk.Form, tm *tunnel.Manager, cm *config.ConfigManager, trayIcon *walk.NotifyIcon) (*PreferencesWindow, error) {
	pw := &PreferencesWindow{
		tunnelManager: tm,
		configManager: cm,
		trayIcon:      trayIcon,
		tabs:          make([]Tab, 0),
	}

	var err error
	var disposables walk.Disposables
	defer disposables.Treat()

	if pw.Dialog, err = walk.NewDialog(owner); err != nil {
		return nil, err
	}
	disposables.Add(pw)

	pw.SetTitle("Pangolin Preferences")
	pw.SetLayout(walk.NewVBoxLayout())

	// Create tab widget
	if pw.tabWidget, err = walk.NewTabWidget(pw); err != nil {
		return nil, err
	}

	// Create and add tabs
	// Order: Preferences, Status, Logs, About
	prefsTab := NewPreferencesTab(cm)
	if tabPage, err := prefsTab.Create(pw.tabWidget); err != nil {
		return nil, fmt.Errorf("failed to create preferences tab: %w", err)
	} else {
		prefsTab.SetWindow(pw)
		pw.tabWidget.Pages().Add(tabPage)
		prefsTab.AfterAdd()
		pw.tabs = append(pw.tabs, prefsTab)
	}

	olmTab := NewOLMStatusTab(tm)
	if tabPage, err := olmTab.Create(pw.tabWidget); err != nil {
		return nil, fmt.Errorf("failed to create OLM status tab: %w", err)
	} else {
		pw.tabWidget.Pages().Add(tabPage)
		olmTab.AfterAdd()
		pw.tabs = append(pw.tabs, olmTab)
	}

	logsTab := NewLogsTab()
	if tabPage, err := logsTab.Create(pw.tabWidget); err != nil {
		return nil, fmt.Errorf("failed to create logs tab: %w", err)
	} else {
		logsTab.SetWindow(pw)
		pw.tabWidget.Pages().Add(tabPage)
		logsTab.AfterAdd()
		pw.tabs = append(pw.tabs, logsTab)
	}

	aboutTab := NewAboutTab()
	if tabPage, err := aboutTab.Create(pw.tabWidget); err != nil {
		return nil, fmt.Errorf("failed to create about tab: %w", err)
	} else {
		pw.tabWidget.Pages().Add(tabPage)
		aboutTab.AfterAdd()
		pw.tabs = append(pw.tabs, aboutTab)
	}

	disposables.Spare()

	// Set window icon
	iconsPath := config.GetIconsPath()
	iconPath := filepath.Join(iconsPath, "icon-orange.ico")
	icon, err := walk.NewIconFromFile(iconPath)
	if err != nil {
		logger.Error("Failed to load window icon from %s: %v", iconPath, err)
	} else {
		if err := pw.SetIcon(icon); err != nil {
			logger.Error("Failed to set window icon: %v", err)
		}
	}

	// Set window size after all components are added
	pw.SetSize(walk.Size{Width: 450, Height: 600})

	// Make dialog appear in taskbar by setting WS_EX_APPWINDOW extended style
	const GWL_EXSTYLE = -20
	const WS_EX_APPWINDOW = 0x00040000
	exStyle := win.GetWindowLong(pw.Handle(), GWL_EXSTYLE)
	exStyle |= WS_EX_APPWINDOW
	win.SetWindowLong(pw.Handle(), GWL_EXSTYLE, exStyle)

	return pw, nil
}

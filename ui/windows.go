package ui

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

const (
	// A click on the tray icon that also made the popup lose focus should not reopen it.
	trayReopenGuard = 300 * time.Millisecond
	// trayHeadroom is spare window height kept beside the menu while it is open,
	// so size changes animate inside the window instead of resizing it. Moving
	// or resizing the window makes WebView2 draw one misplaced frame.
	trayHeadroom = 240
	// trayHideWait bounds how long hiding waits for the popup to paint itself
	// transparent, so the next show doesn't flash the menu as it was.
	trayHideWait = 100 * time.Millisecond
	// trayOpenWait bounds how long opening waits, with the window off screen,
	// for the popup to draw the current menu before moving it into place.
	trayOpenWait = 250 * time.Millisecond
	// trayOffscreen is where the window waits while the popup draws.
	trayOffscreen = -32000
	// trayPrimeDelay lets the page mount after loading before the popup is
	// primed, and trayPrimeWait bounds how long priming waits for it to draw.
	trayPrimeDelay = 300 * time.Millisecond
	trayPrimeWait  = time.Second
)

var (
	systemTray *application.SystemTray

	trayWindow  *application.WebviewWindow
	trayMu      sync.Mutex
	trayHeight  = 400 // window content height
	trayNeeded  = 400 // height the menu needs, as last reported by the popup
	trayBounds  application.Rect
	trayHiding  bool
	trayHideAck chan struct{}
	// trayOpening is true from Show until the window is moved into place.
	trayOpening   bool
	trayOpenGen   int
	trayOpenAck   chan struct{}
	trayNoAnimate sync.Once
	// trayPriming is true while the popup is shown off screen once at startup.
	trayPrimed     bool
	trayPriming    bool
	trayPrimeHide  bool
	trayCursor     application.Point
	trayLayout     = TrayLayout{SubmenuSide: "left", Anchor: "bottom", PanelWidth: trayPanelWidth, Padding: trayShadowPadding}
	trayLastHidden time.Time

	prefsWindow *application.WebviewWindow
	prefsMu     sync.Mutex
	prefsTab    int
	prefsOpen   bool
	// prefsLogin is the login request waiting for the Accounts tab.
	prefsLogin *LoginRequest
	// prefsVisible mirrors prefsOpen for readers that must not take prefsMu.
	prefsVisible atomic.Bool
	prefsLoginID int
)

func setupSystemTray() {
	systemTray = app.SystemTray.New()
	updateTrayForState(currentTunnelState())
	systemTray.OnClick(toggleTrayPopup)
	// Right-click shows only Quit, like the macOS status item's context menu.
	contextMenu := application.NewMenu()
	contextMenu.Add("Quit Pangolin").OnClick(func(*application.Context) { go quit() })
	systemTray.SetMenu(contextMenu)
	systemTray.OnRightClick(func() {
		hideTrayPopup()
		systemTray.OpenMenu()
	})
	createTrayWindow()
}

func createTrayWindow() {
	opts := application.WebviewWindowOptions{
		Name:                       "tray",
		Title:                      "Pangolin",
		URL:                        "/#/tray",
		Width:                      trayWindowWidth,
		Height:                     trayHeight,
		Frameless:                  true,
		AlwaysOnTop:                true,
		Hidden:                     true,
		HideOnEscape:               true,
		DefaultContextMenuDisabled: true,
		BackgroundType:             application.BackgroundTypeTransparent,
	}
	setTrayWindowOptions(&opts)
	trayWindow = app.Window.NewWithOptions(opts)
	trayWindow.OnWindowEvent(events.Common.WindowLostFocus, func(*application.WindowEvent) {
		hideTrayPopup()
	})
	trayWindow.OnWindowEvent(events.Common.WindowHide, func(*application.WindowEvent) {
		trayMu.Lock()
		if trayPrimeHide {
			// Hiding after priming shouldn't block the first click.
			trayPrimeHide = false
		} else {
			trayLastHidden = time.Now()
		}
		trayMu.Unlock()
	})
	onTrayWindowCreated()
}

func toggleTrayPopup() {
	if trayWindow == nil {
		return
	}
	trayMu.Lock()
	priming := trayPriming
	trayMu.Unlock()
	if priming {
		// Take over the off-screen window instead of closing it.
		showTrayPopup()
		return
	}
	if trayWindow.IsVisible() {
		hideTrayPopup()
		return
	}
	trayMu.Lock()
	recentlyHidden := time.Since(trayLastHidden) < trayReopenGuard
	trayMu.Unlock()
	if recentlyHidden {
		return
	}
	showTrayPopup()
}

// showTrayPopup opens the popup. WebView2 stops rendering while its window
// is hidden, so a freshly shown window would first draw a stale frame at the
// wrong size. Instead the window is shown off screen at its final size, the
// popup draws the current menu (still transparent) and reports back, and only
// then is the window moved into place and the menu faded in.
func showTrayPopup() {
	handleMenuOpen()
	disableTrayTransitions()

	cursor, ok := trayClickPoint()
	trayMu.Lock()
	if ok {
		trayCursor = cursor
	}
	trayHeight = trayNeeded + trayHeadroom
	trayMu.Unlock()

	bounds, layout := trayPlacement()
	trayMu.Lock()
	trayLayout = layout
	trayPriming = false
	trayOpening = true
	trayOpenGen++
	gen := trayOpenGen
	ack := make(chan struct{}, 1)
	trayOpenAck = ack
	trayMu.Unlock()

	offscreen := bounds
	offscreen.X, offscreen.Y = trayOffscreen, trayOffscreen
	setTrayBounds(offscreen)

	app.Event.Emit(eventMenuState, currentMenuState())
	trayWindow.EmitEvent("tray:open", layout)
	trayWindow.Show()
	trayWindow.Focus()

	// This may run on the main thread, which the popup needs to report back.
	go func() {
		select {
		case <-ack:
		case <-time.After(trayOpenWait):
		}
		trayMu.Lock()
		if gen != trayOpenGen || !trayOpening {
			// Hidden, or opened again, in the meantime.
			trayMu.Unlock()
			return
		}
		trayOpening = false
		trayOpenAck = nil
		trayMu.Unlock()
		positionTrayWindow()
		trayWindow.EmitEvent("tray:shown")
	}()
}

// trayOpenReady is called by the popup once it has drawn the menu while
// opening, with the window height the menu needs.
func trayOpenReady(needed int) {
	trayMu.Lock()
	defer trayMu.Unlock()
	if needed > 0 {
		trayNeeded = needed
		if needed > trayHeight {
			trayHeight = needed + trayHeadroom
		}
	}
	if trayOpenAck != nil {
		select {
		case trayOpenAck <- struct{}{}:
		default:
		}
	}
}

// hideTrayPopup asks the popup to paint itself transparent, then hides the
// window. It returns at once; this may run on the main thread.
func hideTrayPopup() {
	// Stop polling site status.
	go setMenuSitesVisible(false)
	if trayWindow == nil || !trayWindow.IsVisible() {
		return
	}
	trayMu.Lock()
	if trayPriming {
		// Focus leaving the off-screen window while priming; it hides itself.
		trayMu.Unlock()
		return
	}
	// Cancel an open still waiting off screen.
	trayOpening = false
	trayOpenAck = nil
	trayOpenGen++
	if trayHiding {
		trayMu.Unlock()
		return
	}
	trayHiding = true
	ack := make(chan struct{}, 1)
	trayHideAck = ack
	trayMu.Unlock()

	go func() {
		trayWindow.EmitEvent("tray:hide")
		select {
		case <-ack:
		case <-time.After(trayHideWait):
		}
		trayWindow.Hide()
		trayMu.Lock()
		trayHiding = false
		trayHideAck = nil
		trayMu.Unlock()
	}()
}

// trayHideReady is called by the popup once it has painted itself transparent.
func trayHideReady() {
	trayMu.Lock()
	defer trayMu.Unlock()
	if trayHideAck != nil {
		select {
		case trayHideAck <- struct{}{}:
		default:
		}
	}
}

func currentTrayLayout() TrayLayout {
	trayMu.Lock()
	defer trayMu.Unlock()
	return trayLayout
}

// resizeTrayWindow is called by the popup whenever the height its menu needs
// changes. While the popup is open the window only grows, with headroom, so
// most changes animate inside it; it is sized to fit again on the next show.
func resizeTrayWindow(needed int) {
	if needed <= 0 || trayWindow == nil {
		return
	}
	visible := trayWindow.IsVisible()
	trayMu.Lock()
	trayNeeded = needed
	if visible && needed > trayHeight {
		trayHeight = needed + trayHeadroom
	}
	trayMu.Unlock()
	// Reposition while showing even when the height is unchanged, in case the
	// window was sized before its frame insets were known. Unchanged bounds
	// are skipped.
	if visible {
		positionTrayWindow()
	}
}

// positionTrayWindow places the popup at the last tray click; see
// placeTrayWindow. While opening or priming, the window stays off screen
// until the popup has drawn, so this does nothing then.
func positionTrayWindow() {
	trayMu.Lock()
	offscreen := trayOpening || trayPriming
	trayMu.Unlock()
	if offscreen {
		return
	}
	bounds, layout := trayPlacement()
	trayMu.Lock()
	layoutChanged := layout != trayLayout
	trayLayout = layout
	trayMu.Unlock()
	setTrayBounds(bounds)
	if layoutChanged {
		trayWindow.EmitEvent("tray:layout", layout)
	}
}

// trayPlacement returns the window bounds and popup layout for the last tray
// click and the current height.
func trayPlacement() (application.Rect, TrayLayout) {
	trayMu.Lock()
	cursor := trayCursor
	height := trayHeight
	trayMu.Unlock()

	work := trayRect{X: 0, Y: 0, Width: 1920, Height: 1080}
	scale := float32(1)
	screen := app.Screen.ScreenNearestDipPoint(cursor)
	if screen == nil {
		screen = app.Screen.GetPrimary()
	}
	if screen != nil {
		work = trayRect{X: screen.WorkArea.X, Y: screen.WorkArea.Y, Width: screen.WorkArea.Width, Height: screen.WorkArea.Height}
		if screen.ScaleFactor > 0 {
			scale = screen.ScaleFactor
		}
	}

	rect, layout := placeTrayWindow(cursor.X, cursor.Y, height, work)

	// The rect above is where the web content must go. The webview only fills
	// the client area, so grow the window by whatever frame Windows keeps
	// around it; otherwise the bottom-anchored menu is clipped at the top.
	in := trayFrameInsets(scale)
	return application.Rect{
		X:      rect.X - in.left,
		Y:      rect.Y - in.top,
		Width:  trayWindowWidth + in.left + in.right,
		Height: rect.Height + in.top + in.bottom,
	}, layout
}

// setTrayBounds moves the window, skipping bounds it already has.
func setTrayBounds(bounds application.Rect) {
	trayMu.Lock()
	changed := bounds != trayBounds
	trayBounds = bounds
	trayMu.Unlock()
	if changed {
		trayWindow.SetBounds(bounds)
	}
}

// windowBackground matches the page background so windows don't flash white
// in dark mode before the page paints.
func windowBackground() application.RGBA {
	if systemDarkMode() {
		return application.NewRGB(0x1e, 0x1e, 0x1e)
	}
	return application.NewRGB(0xec, 0xec, 0xec)
}

// windowTitleBarTheme paints the title bar in the window background colour so
// it blends into the page instead of showing as a separate strip. Wails picks
// the active or inactive colours once, when the window is created, so both use
// the same ones. Windows only honours caption colours on Windows 11.
func windowTitleBarTheme() application.ThemeSettings {
	light := &application.WindowTheme{
		TitleBarColour:  application.NewRGBPtr(0xec, 0xec, 0xec),
		TitleTextColour: application.NewRGBPtr(0x1d, 0x1d, 0x1f),
	}
	dark := &application.WindowTheme{
		TitleBarColour:  application.NewRGBPtr(0x1e, 0x1e, 0x1e),
		TitleTextColour: application.NewRGBPtr(0xe5, 0xe5, 0xe7),
	}
	return application.ThemeSettings{
		LightModeActive:   light,
		LightModeInactive: light,
		DarkModeActive:    dark,
		DarkModeInactive:  dark,
	}
}

type frameInsets struct{ left, top, right, bottom int }

// Preferences sidebar sections, in order. The frontend uses the same indexes.
const (
	prefsTabPreferences = iota
	prefsTabAccounts
	prefsTabStatus
	prefsTabLogs
	prefsTabAbout
)

// LoginRequest asks Preferences > Accounts to show the add-account sheet.
type LoginRequest struct {
	// ID tells requests apart, so each one opens a fresh sheet.
	ID int `json:"id"`
	// Hostname is the server of an account to log in to again, or "" to add one.
	Hostname string `json:"hostname"`
}

// currentPrefsOpened must be called with prefsMu held.
func currentPrefsOpened() PrefsOpened {
	return PrefsOpened{Tab: prefsTab, Settings: currentSettings(), Login: prefsLogin}
}

// showPreferencesWindow shows the preferences window on the given tab,
// creating it on first use.
func showPreferencesWindow(tab int, login *LoginRequest) {
	prefsMu.Lock()
	defer prefsMu.Unlock()

	prefsTab = tab
	prefsLogin = nil
	if login != nil {
		// Each request opens a fresh sheet, even for the same server.
		prefsLoginID++
		login.ID = prefsLoginID
		prefsLogin = login
	}
	if prefsWindow == nil {
		prefsWindow = app.Window.NewWithOptions(application.WebviewWindowOptions{
			Name:             "preferences",
			Title:            "Pangolin Preferences",
			URL:              "/#/preferences",
			Width:            720,
			Height:           560,
			MinWidth:         600,
			MinHeight:        400,
			Hidden:           true,
			BackgroundColour: windowBackground(),
			Windows: application.WindowsWindow{
				CustomTheme: windowTitleBarTheme(),
			},
		})
		// Closing only hides the window. Hooks run on the main thread, so the
		// work that takes locks happens on a goroutine.
		prefsWindow.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
			e.Cancel()
			go hidePreferencesWindow()
		})
	}

	if !prefsOpen {
		prefsOpen = true
		prefsVisible.Store(true)
		startStatusPolling()
		startLogTail()
	}
	prefsWindow.EmitEvent("prefs:opened", currentPrefsOpened())
	go refreshPreferencesServerSettings()
	prefsWindow.Show()
	if prefsWindow.IsMinimised() {
		prefsWindow.Restore()
	}
	prefsWindow.Focus()
}

func hidePreferencesWindow() {
	prefsMu.Lock()
	defer prefsMu.Unlock()
	if prefsWindow == nil {
		return
	}
	prefsWindow.Hide()
	if prefsOpen {
		prefsOpen = false
		prefsVisible.Store(false)
		stopStatusPolling()
		stopLogTail()
	}
}

func preferencesWindowOrNil() application.Window {
	prefsMu.Lock()
	defer prefsMu.Unlock()
	if prefsWindow == nil {
		return nil
	}
	return prefsWindow
}

// newDialogWindow creates a small fixed-size window for a single task, such as
// installing an update, centered and hidden until shown. Closing it calls
// onClose instead, so the task can keep running and the window be shown again.
func newDialogWindow(name, title, route string, height int, onClose func()) *application.WebviewWindow {
	w := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:                name,
		Title:               title,
		URL:                 "/#/" + route,
		Width:               440,
		Height:              height,
		DisableResize:       true,
		MinimiseButtonState: application.ButtonHidden,
		MaximiseButtonState: application.ButtonHidden,
		Hidden:              true,
		BackgroundColour:    windowBackground(),
		Windows: application.WindowsWindow{
			CustomTheme: windowTitleBarTheme(),
		},
	})
	// Hooks run on the main thread, so the work that takes locks happens on a goroutine.
	w.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		e.Cancel()
		go onClose()
	})
	w.Center()
	return w
}

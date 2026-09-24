//go:build windows

package ui

import (
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/w32"
)

// A click on the tray icon that also made the popup lose focus should not reopen it.
const trayReopenGuard = 300 * time.Millisecond

var (
	systemTray *application.SystemTray

	trayWindow     *application.WebviewWindow
	trayMu         sync.Mutex
	trayHeight     = 400
	trayCursor     application.Point
	trayLayout     = TrayLayout{SubmenuSide: "left", Anchor: "bottom", PanelWidth: trayPanelWidth, Padding: trayShadowPadding}
	trayLastHidden time.Time

	prefsWindow *application.WebviewWindow
	prefsMu     sync.Mutex
	prefsTab    int
	prefsOpen   bool

	loginWindow *application.WebviewWindow
	loginMu     sync.Mutex

	progressMu      sync.Mutex
	progressWindows = map[string]*application.WebviewWindow{}
	progressTexts   = map[string]string{}
)

func setupSystemTray() {
	systemTray = app.SystemTray.New()
	updateTrayForState(currentTunnelState())
	systemTray.OnClick(toggleTrayPopup)
	systemTray.OnRightClick(toggleTrayPopup)
	createTrayWindow()
}

func createTrayWindow() {
	trayWindow = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:                       "tray",
		Title:                      "Pangolin",
		URL:                        "/#/tray",
		Width:                      trayWindowWidth,
		Height:                     trayHeight,
		Frameless:                  true,
		AlwaysOnTop:                true,
		Hidden:                     true,
		DisableResize:              true,
		HideOnEscape:               true,
		DefaultContextMenuDisabled: true,
		BackgroundType:             application.BackgroundTypeTransparent,
		Windows: application.WindowsWindow{
			HiddenOnTaskbar:                   true,
			DisableFramelessWindowDecorations: true,
			BackdropType:                      application.None,
		},
	})
	trayWindow.OnWindowEvent(events.Common.WindowLostFocus, func(*application.WindowEvent) {
		hideTrayPopup()
	})
	trayWindow.OnWindowEvent(events.Common.WindowHide, func(*application.WindowEvent) {
		trayMu.Lock()
		trayLastHidden = time.Now()
		trayMu.Unlock()
	})
}

func toggleTrayPopup() {
	if trayWindow == nil {
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

func showTrayPopup() {
	handleMenuOpen()

	x, y, ok := w32.GetCursorPos()
	trayMu.Lock()
	if ok {
		trayCursor = application.PhysicalToDipPoint(application.Point{X: x, Y: y})
	}
	trayMu.Unlock()
	positionTrayWindow()

	app.Event.Emit(eventMenuState, currentMenuState())
	trayWindow.EmitEvent("tray:open", currentTrayLayout())
	trayWindow.Show()
	trayWindow.Focus()
}

func hideTrayPopup() {
	if trayWindow != nil && trayWindow.IsVisible() {
		trayWindow.Hide()
	}
}

func currentTrayLayout() TrayLayout {
	trayMu.Lock()
	defer trayMu.Unlock()
	return trayLayout
}

// resizeTrayWindow is called by the popup whenever its content height changes.
func resizeTrayWindow(height int) {
	if height <= 0 {
		return
	}
	trayMu.Lock()
	changed := trayHeight != height
	trayHeight = height
	trayMu.Unlock()
	// Reposition even when the height is unchanged but the popup is showing,
	// in case the window was sized before its frame insets were known.
	if changed || trayWindow.IsVisible() {
		positionTrayWindow()
	}
}

// positionTrayWindow places the popup at the last tray click; see placeTrayWindow.
func positionTrayWindow() {
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
	windowX, windowY, height := rect.X, rect.Y, rect.Height

	trayMu.Lock()
	layoutChanged := layout != trayLayout
	trayLayout = layout
	trayMu.Unlock()

	// The rect above is where the web content must go. The webview only fills
	// the client area, so grow the window by whatever frame Windows keeps
	// around it; otherwise the bottom-anchored menu is clipped at the top.
	in := trayFrameInsets(scale)
	trayWindow.SetBounds(application.Rect{
		X:      windowX - in.left,
		Y:      windowY - in.top,
		Width:  trayWindowWidth + in.left + in.right,
		Height: height + in.top + in.bottom,
	})
	if layoutChanged {
		trayWindow.EmitEvent("tray:layout", layout)
	}
}

type frameInsets struct{ left, top, right, bottom int }

// trayFrameInsets measures, in DIPs, how far the tray window's client area is
// inset from its outer bounds. It is zero before the native window exists.
func trayFrameInsets(scale float32) frameInsets {
	ptr := trayWindow.NativeWindow()
	if ptr == nil {
		return frameInsets{}
	}
	hwnd := w32.HWND(uintptr(ptr))
	wr := w32.GetWindowRect(hwnd)
	cr := w32.GetClientRect(hwnd)
	if wr == nil || cr == nil {
		return frameInsets{}
	}
	cx, cy := w32.ClientToScreen(hwnd, 0, 0)
	toDip := func(v int32) int {
		if v <= 0 {
			return 0
		}
		return int(float32(v)/scale + 0.999)
	}
	left := int32(cx) - wr.Left
	top := int32(cy) - wr.Top
	return frameInsets{
		left:   toDip(left),
		top:    toDip(top),
		right:  toDip((wr.Right - wr.Left) - (cr.Right - cr.Left) - left),
		bottom: toDip((wr.Bottom - wr.Top) - (cr.Bottom - cr.Top) - top),
	}
}

// showPreferencesWindow shows the preferences window on the given tab,
// creating it on first use.
func showPreferencesWindow(tab int) {
	prefsMu.Lock()
	defer prefsMu.Unlock()

	prefsTab = tab
	if prefsWindow == nil {
		prefsWindow = app.Window.NewWithOptions(application.WebviewWindowOptions{
			Name:     "preferences",
			Title:    "Pangolin Preferences",
			URL:      "/#/preferences",
			Width:    450,
			Height:   600,
			MinWidth: 320,
			Hidden:   true,
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
		startStatusPolling()
		startLogTail()
	}
	prefsWindow.EmitEvent("prefs:opened", PrefsOpened{Tab: tab, Settings: currentSettings()})
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

// showLoginWindow opens the login window, or focuses it when it is already open.
func showLoginWindow() {
	loginMu.Lock()
	defer loginMu.Unlock()

	if loginWindow != nil && loginWindow.IsVisible() {
		// Auto-start only applies when the window is first opened.
		if authManager != nil {
			authManager.ClearStartDeviceAuthImmediately()
		}
		if loginWindow.IsMinimised() {
			loginWindow.Restore()
		}
		loginWindow.Focus()
		return
	}

	if loginWindow == nil {
		loginWindow = app.Window.NewWithOptions(application.WebviewWindowOptions{
			Name:                "login",
			Title:               "Login to Pangolin",
			URL:                 "/#/login",
			Width:               450,
			Height:              330,
			DisableResize:       true,
			Hidden:              true,
			MinimiseButtonState: application.ButtonHidden,
			MaximiseButtonState: application.ButtonHidden,
			BackgroundColour:    application.NewRGB(0xFC, 0xFC, 0xFC),
			Windows: application.WindowsWindow{
				Theme: application.Light,
			},
		})
		loginWindow.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
			e.Cancel()
			go closeLoginWindow()
		})
	}

	startLoginFlow()
	loginWindow.Center()
	loginWindow.Show()
	loginWindow.Focus()
}

// closeLoginWindow hides the login window and ends the login flow.
func closeLoginWindow() {
	loginMu.Lock()
	w := loginWindow
	loginMu.Unlock()
	if w != nil {
		w.Hide()
	}
	endLoginFlow()
	publish()
}

func loginWindowOrNil() application.Window {
	loginMu.Lock()
	defer loginMu.Unlock()
	if loginWindow == nil {
		return nil
	}
	return loginWindow
}

// openProgressWindow shows a small marquee progress window and returns a
// function that closes it.
func openProgressWindow(kind, title, text string) func() {
	progressMu.Lock()
	defer progressMu.Unlock()

	if w, ok := progressWindows[kind]; ok {
		w.Close()
	}
	progressTexts[kind] = text
	w := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:                "progress-" + kind,
		Title:               title,
		URL:                 "/#/progress?kind=" + kind,
		Width:               420,
		Height:              140,
		DisableResize:       true,
		MinimiseButtonState: application.ButtonHidden,
		MaximiseButtonState: application.ButtonHidden,
	})
	progressWindows[kind] = w
	w.Center()

	var once sync.Once
	return func() {
		once.Do(func() {
			progressMu.Lock()
			defer progressMu.Unlock()
			if progressWindows[kind] == w {
				delete(progressWindows, kind)
			}
			w.Close()
		})
	}
}

func setProgressText(kind, text string) {
	progressMu.Lock()
	progressTexts[kind] = text
	w := progressWindows[kind]
	progressMu.Unlock()
	if w != nil {
		w.EmitEvent(eventProgressText, text)
	}
}

func progressText(kind string) string {
	progressMu.Lock()
	defer progressMu.Unlock()
	return progressTexts[kind]
}

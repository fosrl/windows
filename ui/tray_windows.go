package ui

import (
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/w32"
)

var systemTray *application.SystemTray

// trayOpensOffscreen: WebView2 windows are opened off screen; see showTrayPopup.
const trayOpensOffscreen = true

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

// setTrayIcon sets the tray icon (an .ico) and tooltip.
func setTrayIcon(ico []byte, tooltip string) {
	if systemTray == nil {
		return
	}
	systemTray.SetIcon(ico)
	systemTray.SetTooltip(tooltip)
}

func beforeTrayShow() {}

// afterTrayHide is a no-op: the WindowHide event does the bookkeeping.
func afterTrayHide() {}

func focusTrayWindow() { trayWindow.Focus() }

// trayWorkArea returns the work area, in DIPs, of the screen nearest cursor
// and that screen's scale.
func trayWorkArea(cursor application.Point) (trayRect, float32) {
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
	return work, scale
}

// trayNativeBounds converts popup bounds to window bounds; Wails takes DIPs.
func trayNativeBounds(r application.Rect) application.Rect { return r }

// scaleWindowOptions is a no-op on Windows, where Wails handles DPI scaling.
func scaleWindowOptions(opts *application.WebviewWindowOptions) {}

func setTrayWindowOptions(opts *application.WebviewWindowOptions) {
	opts.DisableResize = true
	opts.Windows = application.WindowsWindow{
		HiddenOnTaskbar:                   true,
		DisableFramelessWindowDecorations: true,
		BackdropType:                      application.None,
	}
}

// onTrayWindowCreated primes the popup once its page has loaded.
func onTrayWindowCreated() {
	trayWindow.OnWindowEvent(events.Windows.WebViewNavigationCompleted, func(*application.WindowEvent) {
		go func() {
			time.Sleep(trayPrimeDelay)
			primeTrayWindow()
		}()
	})
}

// trayClickPoint returns the cursor position, in DIPs, at the tray click.
func trayClickPoint() (application.Point, bool) {
	x, y, ok := w32.GetCursorPos()
	if !ok {
		return application.Point{}, false
	}
	return application.PhysicalToDipPoint(application.Point{X: x, Y: y}), true
}

// primeTrayWindow shows the popup off screen once and hides it again as soon
// as it has drawn. WebView2 sets up rendering the first time a window is
// shown, which otherwise makes the first click on the tray icon slow. Focus
// goes straight back to whatever had it.
func primeTrayWindow() {
	if trayWindow == nil || trayWindow.IsVisible() {
		return
	}
	trayMu.Lock()
	if trayPrimed || trayOpening {
		trayMu.Unlock()
		return
	}
	trayPrimed = true
	trayPriming = true
	trayOpenGen++
	gen := trayOpenGen
	ack := make(chan struct{}, 1)
	trayOpenAck = ack
	trayMu.Unlock()

	disableTrayTransitions()
	bounds, layout := trayPlacement()
	bounds.X, bounds.Y = trayOffscreen, trayOffscreen
	setTrayBounds(bounds)

	previous := w32.GetForegroundWindow()
	trayWindow.EmitEvent("tray:open", layout)
	trayWindow.Show()
	if previous != 0 {
		w32.SetForegroundWindow(previous)
	}

	select {
	case <-ack:
	case <-time.After(trayPrimeWait):
	}
	trayMu.Lock()
	if gen != trayOpenGen || !trayPriming {
		// A click opened the popup for real in the meantime.
		trayMu.Unlock()
		return
	}
	trayPriming = false
	trayOpenAck = nil
	trayPrimeHide = true
	trayMu.Unlock()
	trayWindow.Hide()
}

// disableTrayTransitions turns off the fade Windows plays when the popup
// window is shown or hidden; the popup animates itself.
func disableTrayTransitions() {
	ptr := trayWindow.NativeWindow()
	if ptr == nil {
		return
	}
	trayNoAnimate.Do(func() {
		disabled := int32(1)
		w32.DwmSetWindowAttribute(w32.HWND(uintptr(ptr)), w32.DWMWA_TRANSITIONS_FORCEDISABLED,
			unsafe.Pointer(&disabled), unsafe.Sizeof(disabled))
	})
}

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

// focusWindow brings a shown window to the front with keyboard focus.
func focusWindow(w *application.WebviewWindow) { w.Focus() }

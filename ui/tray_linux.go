package ui

import (
	"image"
	"math"
	"sync"
	"time"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows-linux/icons"
	"github.com/fosrl/windows-linux/config"
	"github.com/fosrl/windows-linux/ui/sni"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// On Linux the tray icon is a StatusNotifierItem (see package sni), or the
// Pangolin GNOME Shell extension's panel button when that is running (see
// shell_linux.go). Both report where they were clicked, and the popup opens
// there.
//
// Units: the popup's layout is worked out in CSS pixels, as on Windows in
// DIPs. GTK windows are positioned in GTK units, which are X pixels divided
// by gtkScale(). Web content is zoomed by uiScale(), so one CSS pixel is
// uiScale GTK units.

// trayOpensOffscreen: X window managers may keep windows on screen, so the
// popup opens in place, still transparent, and fades in once drawn.
const trayOpensOffscreen = false

var (
	trayItem *sni.Item

	trayGeomMu sync.Mutex
	// trayClick is the last click on the tray icon in GTK units.
	trayClick    application.Point
	trayClickSet bool
	// trayScale is uiScale() as of the last open.
	trayScale = 1.0
	trayZoom  float64
)

// trayHostWait is how long after start a missing tray counts as missing.
const trayHostWait = 5 * time.Second

func setupSystemTray() {
	createTrayWindow()
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		go startLinuxTray()
	})
}

func startLinuxTray() {
	watchColorScheme()
	startShellBridge()

	item, err := sni.New(sni.Options{
		ID:    "pangolin-client",
		Title: config.AppName,
		Activate: func(x, y int) {
			logger.Debug("Tray: activate at %d,%d", x, y)
			// SNI hosts report X pixels.
			s := gtkScale()
			setTrayClick(x/s, y/s, x != 0 || y != 0)
			toggleTrayPopup()
		},
		Menu: []sni.MenuItem{{Label: "Quit Pangolin", OnClick: func() {
			hideTrayPopup()
			quit()
		}}},
	})
	if err != nil {
		logger.Error("Failed to create the tray icon: %v", err)
	} else {
		trayGeomMu.Lock()
		trayItem = item
		trayGeomMu.Unlock()
		item.SetPassive(shellExtensionActive())
		updateTrayForState(currentTunnelState())
	}

	// Without a tray there is no way to open Pangolin, so open Preferences.
	time.Sleep(trayHostWait)
	if !shellExtensionActive() && (item == nil || !item.HostAvailable()) {
		logger.Error("No system tray found; opening Preferences instead. On GNOME, enable the Pangolin extension.")
		showPreferencesWindow(prefsTabPreferences, nil)
	}
}

func setTrayClick(x, y int, known bool) {
	trayGeomMu.Lock()
	trayClick = application.Point{X: x, Y: y}
	trayClickSet = known
	trayGeomMu.Unlock()
}

// setTrayIcon sets the tray icon (an .ico) and tooltip.
func setTrayIcon(ico []byte, tooltip string) {
	setShellIcon(ico, tooltip)
	trayGeomMu.Lock()
	item := trayItem
	trayGeomMu.Unlock()
	if item == nil {
		return
	}
	item.SetIcon(icoFrames(ico, 64))
	item.SetTooltip(tooltip)
}

// icoFrames decodes the frames of an .ico up to maxSize pixels wide, so the
// tray can pick the closest size.
func icoFrames(ico []byte, maxSize int) []image.Image {
	var frames []image.Image
	forEachICOFrame(ico, func(img *image.RGBA) {
		if img.Bounds().Dx() <= maxSize {
			frames = append(frames, img)
		}
	})
	return frames
}

func setTrayWindowOptions(opts *application.WebviewWindowOptions) {
	// DisableResize breaks the window height under GTK, so it stays off.
	opts.Linux = application.LinuxWindow{
		WindowIsTranslucent: true,
	}
}

func onTrayWindowCreated() {
	// Escape hides the window inside Wails, which loses focus as it goes.
	trayWindow.OnWindowEvent(events.Common.WindowLostFocus, func(*application.WindowEvent) {
		logger.Debug("Tray: popup lost focus")
		go func() {
			if !trayWindow.IsVisible() {
				afterTrayHide()
			}
		}()
	})
}

// afterTrayHide records that the popup closed. Wails has no WindowHide event
// on Linux, so this stands in for the handler createTrayWindow registers.
func afterTrayHide() {
	trayMu.Lock()
	trayLastHidden = time.Now()
	trayMu.Unlock()
	logger.Debug("Tray: popup hidden")
	setShellPopupVisible(false)
}

// beforeTrayShow sets the popup's window hints and zoom before it maps.
func beforeTrayShow() {
	setPopupHints(trayWindow)
	trayGeomMu.Lock()
	scale := trayScale
	changed := scale != trayZoom
	trayZoom = scale
	trayGeomMu.Unlock()
	if changed {
		trayWindow.SetZoom(scale)
	}
}

func focusTrayWindow() {
	presentNow(trayWindow)
	logger.Debug("Tray: popup shown")
	go setShellPopupVisible(true)
}

// trayClickPoint returns the last tray click in CSS pixels. Without one it
// falls back to the pointer, then to the bottom-right of the primary screen.
func trayClickPoint() (application.Point, bool) {
	scale := uiScale()
	trayGeomMu.Lock()
	trayScale = scale
	click, ok := trayClick, trayClickSet
	trayGeomMu.Unlock()

	if !ok {
		if x, y, pok := pointerPosition(); pok && (x != 0 || y != 0) {
			s := gtkScale()
			click, ok = application.Point{X: x / s, Y: y / s}, true
		}
	}
	if !ok {
		screen := app.Screen.GetPrimary()
		if screen == nil {
			return application.Point{}, false
		}
		w := screen.WorkArea
		click = application.Point{X: w.X + w.Width - 1, Y: w.Y + w.Height - 1}
	}
	return application.Point{X: toCSS(click.X, scale), Y: toCSS(click.Y, scale)}, true
}

// trayWorkArea returns the work area, in CSS pixels, of the screen holding
// cursor (in CSS pixels).
func trayWorkArea(cursor application.Point) (trayRect, float32) {
	trayGeomMu.Lock()
	scale := trayScale
	trayGeomMu.Unlock()
	nx, ny := toGTK(cursor.X, scale), toGTK(cursor.Y, scale)

	var screen *application.Screen
	for _, s := range app.Screen.GetAll() {
		b := s.Bounds
		if nx >= b.X && nx < b.X+b.Width && ny >= b.Y && ny < b.Y+b.Height {
			screen = s
			break
		}
	}
	if screen == nil {
		screen = app.Screen.GetPrimary()
	}
	if screen == nil {
		return trayRect{X: 0, Y: 0, Width: 1920, Height: 1080}, 1
	}
	w := screen.WorkArea
	x, y := toCSS(w.X, scale), toCSS(w.Y, scale)
	return trayRect{
		X: x, Y: y,
		Width:  toCSS(w.X+w.Width, scale) - x,
		Height: toCSS(w.Y+w.Height, scale) - y,
	}, 1
}

// trayNativeBounds converts popup bounds in CSS pixels to GTK units.
func trayNativeBounds(r application.Rect) application.Rect {
	trayGeomMu.Lock()
	scale := trayScale
	trayGeomMu.Unlock()
	x, y := toGTK(r.X, scale), toGTK(r.Y, scale)
	return application.Rect{
		X: x, Y: y,
		Width:  toGTK(r.X+r.Width, scale) - x,
		Height: toGTK(r.Y+r.Height, scale) - y,
	}
}

func toCSS(v int, scale float64) int { return int(math.Floor(float64(v) / scale)) }
func toGTK(v int, scale float64) int { return int(math.Round(float64(v) * scale)) }

// scaleWindowOptions sizes a window for uiScale and zooms its content to match.
func scaleWindowOptions(opts *application.WebviewWindowOptions) {
	scale := uiScale()
	if scale == 1 {
		return
	}
	opts.Width = toGTK(opts.Width, scale)
	opts.Height = toGTK(opts.Height, scale)
	opts.MinWidth = toGTK(opts.MinWidth, scale)
	opts.MinHeight = toGTK(opts.MinHeight, scale)
	opts.Zoom = scale
}

// disableTrayTransitions is a no-op: GTK windows have no show/hide animation
// of their own to turn off.
func disableTrayTransitions() {}

// trayFrameInsets is zero: the frameless GTK window has no frame.
func trayFrameInsets(scale float32) frameInsets { return frameInsets{} }

// appIconPNG is the application icon as PNG, for window icons.
func appIconPNG() []byte {
	data, err := icoToPNG(icons.Orange, 256)
	if err != nil {
		logger.Error("Failed to convert app icon: %v", err)
		return nil
	}
	return data
}

// focusWindow brings a shown window to the front with keyboard focus. It is
// presented with a fresh X timestamp: window managers keep windows that open
// without one, such as at startup, behind the active window.
func focusWindow(w *application.WebviewWindow) { presentNow(w) }

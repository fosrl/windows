package ui

import (
	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/icons"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// The Linux popup here is a first pass: it opens from the tray and works, but
// placement, focus and HiDPI handling (XWayland, X server timestamps, webview
// zoom) come with the Linux UI step.

func setTrayWindowOptions(opts *application.WebviewWindowOptions) {
	// DisableResize breaks the window height under GTK, so it stays off.
	opts.Linux = application.LinuxWindow{
		WindowIsTranslucent: true,
	}
}

func onTrayWindowCreated() {}

// trayClickPoint returns where the tray was clicked. Wails keeps the
// StatusNotifierItem click position private, so for now this is the
// bottom-right corner of the primary work area, where KDE and XFCE put the
// tray by default.
func trayClickPoint() (application.Point, bool) {
	screen := app.Screen.GetPrimary()
	if screen == nil {
		return application.Point{}, false
	}
	w := screen.WorkArea
	return application.Point{X: w.X + w.Width - 1, Y: w.Y + w.Height - 1}, true
}

// disableTrayTransitions is a no-op: GTK windows have no show/hide animation
// of their own to turn off.
func disableTrayTransitions() {}

// trayFrameInsets is zero: the frameless GTK window has no frame.
func trayFrameInsets(scale float32) frameInsets { return frameInsets{} }

// trayIconData converts a tray icon to PNG, the only format Linux trays take.
func trayIconData(ico []byte) []byte {
	data, err := icoToPNG(ico, 64)
	if err != nil {
		logger.Error("Failed to convert tray icon: %v", err)
		return ico
	}
	return data
}

// appIconPNG is the application icon as PNG, for window icons.
func appIconPNG() []byte {
	data, err := icoToPNG(icons.Orange, 256)
	if err != nil {
		logger.Error("Failed to convert app icon: %v", err)
		return nil
	}
	return data
}

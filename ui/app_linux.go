package ui

import (
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// checkWebView always succeeds on Linux: WebKitGTK is a package dependency,
// so the binary would not have started without it.
func checkWebView() error { return nil }

// beforeAppStart runs just before the Wails application is created.
func beforeAppStart() {
	// Run under XWayland on Wayland sessions: Wayland compositors decide
	// where windows go, so the tray popup can only be placed through X11.
	// Under native Wayland KWin also gives the frameless popup a title bar.
	// Setting GDK_BACKEND overrides this.
	if os.Getenv("GDK_BACKEND") == "" {
		os.Setenv("GDK_BACKEND", "x11")
	}
}

func setPlatformOptions(opts *application.Options) {
	if icon := appIconPNG(); icon != nil {
		opts.Icon = icon
	}
	opts.Linux = application.LinuxOptions{
		DisableQuitOnLastWindowClosed: true,
		ProgramName:                   "pangolin-client",
	}
}

// webviewUserDataPath is empty on Linux. On Windows it locates WebView2 data
// left by releases from before setup existed; no such Linux releases exist.
func webviewUserDataPath() string { return "" }

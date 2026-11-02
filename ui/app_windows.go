package ui

import (
	"os"
	"path/filepath"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/config"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/w32"
)

var procChangeWindowMessageFilter = user32.NewProc("ChangeWindowMessageFilter")

// checkWebView returns ErrWebView2Missing when the WebView2 Runtime is not installed.
func checkWebView() error {
	if !webView2Installed() {
		return ErrWebView2Missing
	}
	return nil
}

// beforeAppStart runs just before the Wails application is created.
func beforeAppStart() {
	allowTaskbarCreated()
}

func setPlatformOptions(opts *application.Options) {
	opts.Windows = application.WindowsOptions{
		DisableQuitOnLastWindowClosed: true,
		WebviewUserDataPath:           webviewUserDataPath(),
	}
}

func allowTaskbarCreated() {
	const msgfltAdd = 1
	msg := w32.RegisterWindowMessage(w32.MustStringToUTF16Ptr("TaskbarCreated"))
	if r, _, err := procChangeWindowMessageFilter.Call(uintptr(msg), msgfltAdd); r == 0 {
		logger.Error("Failed to allow TaskbarCreated message: %v", err)
	}
}

// webviewUserDataPath keeps WebView2's profile in the user's local app data;
// the default next to the exe is not writable under Program Files.
func webviewUserDataPath() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base = os.Getenv("APPDATA")
	}
	if base == "" {
		return ""
	}
	return filepath.Join(base, config.AppName, "WebView2")
}

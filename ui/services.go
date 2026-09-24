//go:build windows

package ui

import (
	"strings"
	"time"

	"github.com/fosrl/windows/version"
)

// Services bound to the frontend. Each method runs on its own goroutine, so
// methods may block on dialogs or IPC.

// MenuService backs the tray popup.
type MenuService struct{}

func (MenuService) State() MenuState   { return currentMenuState() }
func (MenuService) Layout() TrayLayout { return currentTrayLayout() }
func (MenuService) Invoke(id string)   { invokeMenuItem(id) }
func (MenuService) Resize(height int)  { resizeTrayWindow(height) }
func (MenuService) Hide()              { hideTrayPopup() }

// PreferencesService backs the Preferences tab.
type PreferencesService struct{}

func (PreferencesService) Opened() PrefsOpened {
	prefsMu.Lock()
	tab := prefsTab
	prefsMu.Unlock()
	return PrefsOpened{Tab: tab, Settings: currentSettings()}
}
func (PreferencesService) Save(form Settings) Settings { return saveSettings(form) }

// StatusService backs the Status tab.
type StatusService struct{}

func (StatusService) Current() StatusView { return currentStatusView() }

// LogsService backs the Logs tab.
type LogsService struct{}

func (LogsService) Snapshot() LogsSnapshot { return currentLogsSnapshot() }
func (LogsService) Clear()                 { clearLogs() }
func (LogsService) Copy(seqs []uint64)     { copyLogLines(seqs) }
func (LogsService) Export()                { exportLogs() }

// LoginService backs the login window.
type LoginService struct{}

func (LoginService) State() LoginView  { return currentLoginView() }
func (LoginService) ChooseCloud()      { loginChooseCloud() }
func (LoginService) ChooseSelfHosted() { loginChooseSelfHosted() }
func (LoginService) SetURL(u string)   { loginSetURL(u) }
func (LoginService) Login()            { loginSubmit() }
func (LoginService) Back()             { loginBack() }
func (LoginService) Cancel()           { closeLoginWindow() }
func (LoginService) CopyCode()         { loginCopyCode() }
func (LoginService) OpenBrowser()      { loginOpenBrowser() }

// AppInfo is static information shown in the About tab.
type AppInfo struct {
	Version string `json:"version"`
	Year    int    `json:"year"`
}

// AppService holds small helpers shared by all windows.
type AppService struct{}

func (AppService) Info() AppInfo { return AppInfo{Version: version.Number, Year: time.Now().Year()} }
func (AppService) OpenURL(url string) {
	// Only the fixed docs and legal links are opened from the frontend.
	if strings.HasPrefix(url, "https://docs.pangolin.net/") || strings.HasPrefix(url, "https://pangolin.net/") {
		openURL(url)
	}
}
func (AppService) ProgressText(kind string) string { return progressText(kind) }

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

// HideReady is called once the popup has painted itself transparent for hiding.
func (MenuService) HideReady() { trayHideReady() }

// OpenReady is called once the popup has drawn the menu while opening, with
// the window height it needs.
func (MenuService) OpenReady(height int) { trayOpenReady(height) }

// SetSitesVisible is called when the sites submenu opens or closes, so site
// status is only polled while it is on screen.
func (MenuService) SetSitesVisible(visible bool) { setMenuSitesVisible(visible) }

// PreferencesService backs the Preferences tab.
type PreferencesService struct{}

func (PreferencesService) Opened() PrefsOpened {
	prefsMu.Lock()
	defer prefsMu.Unlock()
	return currentPrefsOpened()
}
func (PreferencesService) Update(form Settings) SettingsResult { return applySettings(form) }

// StatusService backs the Status tab.
type StatusService struct{}

func (StatusService) Current() StatusView { return currentStatusView() }

// LogsService backs the Logs tab.
type LogsService struct{}

func (LogsService) Snapshot() LogsSnapshot { return currentLogsSnapshot() }
func (LogsService) Clear()                 { clearLogs() }
func (LogsService) Copy(seqs []uint64)     { copyLogLines(seqs) }
func (LogsService) Export()                { exportLogs() }

// LoginService backs the add-account sheet in Preferences > Accounts.
type LoginService struct{}

func (LoginService) State() LoginView { return currentLoginView() }

// Open starts a session for a newly shown sheet; renewHostname is the server
// of an account to log in to again, or "" to add an account.
func (LoginService) Open(renewHostname string) LoginView { return loginOpen(renewHostname) }
func (LoginService) Start(hostname string)               { loginStart(hostname) }
func (LoginService) Back()                               { loginBack() }

// Close ends the sheet's session, cancelling a login that hasn't finished.
func (LoginService) Close(session int) { loginClose(session) }
func (LoginService) CopyCode()         { loginCopyCode() }
func (LoginService) OpenBrowser()      { loginOpenBrowser() }

// OnboardingService backs the setup window.
type OnboardingService struct{}

func (OnboardingService) State() OnboardingState   { return currentOnboardingState() }
func (OnboardingService) MarkWelcomeSeen()         { markOnboardingWelcomeSeen() }
func (OnboardingService) MarkPrivacyAcknowledged() { markOnboardingPrivacyAcknowledged() }
func (OnboardingService) Close()                   { closeOnboardingWindow() }

// AccountsService backs Preferences > Accounts.
type AccountsService struct{}

func (AccountsService) State() AccountsView  { return currentAccountsView() }
func (AccountsService) Switch(userID string) { switchAccount(userID) }
func (AccountsService) Remove(userID string) { deleteAccount(userID) }

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

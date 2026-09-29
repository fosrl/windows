//go:build windows

package ui

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/api"
	"github.com/fosrl/windows/auth"
	"github.com/fosrl/windows/config"
	"github.com/fosrl/windows/managers"
	"github.com/fosrl/windows/tunnel"
	"github.com/fosrl/windows/version"
)

// Event names emitted to the frontend.
const (
	eventMenuState     = "menu:state"
	eventStatusUpdate  = "status:update"
	eventLogsReset     = "logs:reset"
	eventLogsAppend    = "logs:append"
	eventLoginState    = "login:state"
	eventAccountsState = "accounts:state"
	eventCLIState      = "cli:state"
	eventUpdateState   = "update:state"
)

var (
	authManager    *auth.AuthManager
	configManager  *config.ConfigManager
	accountManager *config.AccountManager
	apiClient      *api.APIClient
	tunnelManager  *tunnel.Manager

	stateMu              sync.RWMutex
	hasUpdate            bool
	isLoggedOut          bool
	cliInstalled         bool
	cliInstallInProgress bool

	// Progress shown in the menu while it stays open, as on macOS.
	pendingTunnelOn    *bool
	pendingTunnelTimer *time.Timer
	connectionError    string
	switchingAccountID string
	switchingOrgID     string
	loggingOut         bool
	removingAccountID  string

	tunnelDisplay      phaseDebouncer
	tunnelDisplayTimer *time.Timer

	alwaysOn      bool
	alwaysOnMutex sync.Mutex
)

func setHasUpdate(v bool) {
	stateMu.Lock()
	hasUpdate = v
	stateMu.Unlock()
	publish()
}

func setLoggedOut(v bool) {
	stateMu.Lock()
	isLoggedOut = v
	stateMu.Unlock()
}

func setCLIInstallInProgress(v bool) {
	stateMu.Lock()
	cliInstallInProgress = v
	stateMu.Unlock()
	publish()
}

// pendingTunnelTimeout clears a pending switch click if the tunnel never
// reports the requested state.
const pendingTunnelTimeout = 30 * time.Second

func setPendingTunnel(on *bool) {
	stateMu.Lock()
	pendingTunnelOn = on
	if pendingTunnelTimer != nil {
		pendingTunnelTimer.Stop()
		pendingTunnelTimer = nil
	}
	if on != nil {
		pendingTunnelTimer = time.AfterFunc(pendingTunnelTimeout, func() {
			setPendingTunnel(nil)
			publish()
		})
	}
	stateMu.Unlock()
}

// onTunnelStateForMenu clears a pending switch click once the tunnel has
// moved toward the requested state.
func onTunnelStateForMenu(state tunnel.State) {
	stateMu.RLock()
	p := pendingTunnelOn
	stateMu.RUnlock()
	if p != nil && *p == (state != tunnel.StateStopped) {
		setPendingTunnel(nil)
	}
	if state == tunnel.StateRunning {
		setConnectionError("")
	}
}

func setConnectionError(message string) {
	stateMu.Lock()
	connectionError = message
	stateMu.Unlock()
}

func setSwitchingAccount(userID string) {
	stateMu.Lock()
	switchingAccountID = userID
	stateMu.Unlock()
	publish()
}

func setSwitchingOrg(orgID string) {
	stateMu.Lock()
	switchingOrgID = orgID
	stateMu.Unlock()
	publish()
}

func setLoggingOut(v bool) {
	stateMu.Lock()
	loggingOut = v
	stateMu.Unlock()
	publish()
}

// displayedTunnelPhase debounces drops in the tunnel state. While one is held
// back it schedules a publish for when it should show.
func displayedTunnelPhase(raw tunnelPhase, immediate bool) tunnelPhase {
	stateMu.Lock()
	defer stateMu.Unlock()
	shown, wait := tunnelDisplay.update(raw, time.Now(), immediate)
	if wait > 0 && tunnelDisplayTimer == nil {
		tunnelDisplayTimer = time.AfterFunc(wait, func() {
			stateMu.Lock()
			tunnelDisplayTimer = nil
			stateMu.Unlock()
			publish()
		})
	}
	return shown
}

func currentTunnelState() tunnel.State {
	if tunnelManager == nil {
		return tunnel.StateStopped
	}
	return tunnelManager.State()
}

func phaseForState(s tunnel.State) tunnelPhase {
	switch s {
	case tunnel.StateStopped:
		return phaseStopped
	case tunnel.StateStarting, tunnel.StateRegistering, tunnel.StateRegistered, tunnel.StateReconnecting:
		return phaseStarting
	case tunnel.StateRunning:
		return phaseRunning
	case tunnel.StateStopping:
		return phaseStopping
	default:
		return phaseOther
	}
}

// collectMenuInputs snapshots the managers for buildMenuState.
func collectMenuInputs() menuInputs {
	in := menuInputs{
		Onboarding:         onboardingNeeded(),
		Now:                time.Now(),
		Version:            version.Number,
		Year:               time.Now().Year(),
		CheckUpdateVisible: config.CheckForUpdatesButtonEnabled(),
	}

	stateMu.RLock()
	in.LoggedOut = isLoggedOut
	in.HasUpdate = hasUpdate
	in.CLIInstalled = cliInstalled
	in.CLIInstalling = cliInstallInProgress
	if pendingTunnelOn != nil {
		v := *pendingTunnelOn
		in.PendingTunnelOn = &v
	}
	in.ConnectionError = connectionError
	in.SwitchingAccountID = switchingAccountID
	in.SwitchingOrgID = switchingOrgID
	in.LoggingOut = loggingOut
	stateMu.RUnlock()

	rawPhase := phaseForState(currentTunnelState())
	// Drops the user asked for show right away.
	in.TunnelPhase = displayedTunnelPhase(rawPhase, in.PendingTunnelOn != nil || in.switching())
	if tunnelManager != nil {
		in.Connected = tunnelManager.IsConnected()
	}
	if menuSitesVisible() {
		view, loaded := currentStatusSites()
		in.Sites, in.SitesLoaded = view, loaded
	}

	if authManager != nil {
		in.Initializing = authManager.IsInitializing()
		in.Authenticated = authManager.IsAuthenticated()
		in.ServerDown = authManager.IsServerDown()
		in.SessionExpired = authManager.SessionExpired()
		in.DeviceAuthInProgress = authManager.IsDeviceAuthInProgress()
		if msg := authManager.ErrorMessage(); msg != nil {
			in.ErrorMessage = *msg
		}
		if si := authManager.ServerInfo(); si != nil {
			msi := &menuServerInfo{
				Build:                  si.Build,
				EnterpriseLicenseValid: si.EnterpriseLicenseValid,
				SupporterStatusValid:   si.SupporterStatusValid,
			}
			if si.EnterpriseLicenseType != nil {
				msi.EnterpriseLicenseType = *si.EnterpriseLicenseType
			}
			in.ServerInfo = msi
		}
		for _, o := range authManager.Organizations() {
			in.Orgs = append(in.Orgs, menuOrg{ID: o.Id, Name: o.Name})
		}
		if org := authManager.CurrentOrg(); org != nil {
			in.CurrentOrgID = org.Id
			in.CurrentOrgName = org.Name
		}
		in.ExitNodes, in.ActiveExitNodeID = currentExitNodes(in.CurrentOrgID, rawPhase == phaseRunning)
		if user := authManager.CurrentUser(); user != nil {
			in.CurrentUserDisplay = auth.UserDisplayName(user)
		}
	}

	if accountManager != nil {
		for _, a := range accountManager.Accounts {
			a := a
			in.Accounts = append(in.Accounts, menuAccount{
				UserID:   a.UserID,
				Display:  auth.AccountDisplayName(&a),
				Email:    a.Email,
				Hostname: a.Hostname,
			})
		}
		sort.Slice(in.Accounts, func(i, j int) bool {
			// Case-insensitive, like the macOS accounts submenu.
			di, dj := strings.ToLower(in.Accounts[i].Display), strings.ToLower(in.Accounts[j].Display)
			if di != dj {
				return di < dj
			}
			return in.Accounts[i].Hostname < in.Accounts[j].Hostname
		})
		active, _ := accountManager.ActiveAccount()
		if active != nil {
			in.ActiveAccountID = active.UserID
		}
	}

	return in
}

func currentMenuState() MenuState {
	return buildMenuState(collectMenuInputs())
}

// publish pushes a fresh menu snapshot to the tray popup.
func publish() {
	if app == nil {
		return
	}
	app.Event.Emit(eventMenuState, currentMenuState())
	// Keep Preferences > Accounts in step with the tray.
	if prefsVisible.Load() {
		app.Event.Emit(eventAccountsState, currentAccountsView())
	}
}

// handleMenuOpen verifies the session and refreshes organizations when the tray popup opens.
func handleMenuOpen() {
	if authManager == nil || apiClient == nil || !authManager.IsAuthenticated() {
		return
	}

	refreshExitNodes()

	go func() {
		_ = authManager.CheckHealthAndSetState()

		if authManager.IsServerDown() {
			// Keep the UI visible while the server is down.
			setLoggedOut(false)
			publish()
			return
		}

		user, err := apiClient.GetUser()
		if err != nil {
			// 401/403: the API callback already set sessionExpired. Leave isLoggedOut
			// alone so the menu shows "Account Locked" and "Log In".
			var apiErr *api.APIError
			if !(errors.As(err, &apiErr) && (apiErr.Status == 401 || apiErr.Status == 403)) {
				setLoggedOut(true)
			}
			publish()
			return
		}

		authManager.UpdateCurrentUser(user)

		if accountManager != nil {
			activeAccount, _ := accountManager.ActiveAccount()
			if activeAccount != nil && activeAccount.UserID == user.UserId {
				var username, name string
				if user.Username != nil {
					username = *user.Username
				}
				if user.Name != nil {
					name = *user.Name
				}
				_ = accountManager.UpdateAccountUserInfo(activeAccount.UserID, username, name)
			}
		}

		setLoggedOut(false)
		publish()

		if authManager.IsAuthenticated() {
			if err := authManager.RefreshOrganizations(); err != nil {
				logger.Error("Failed to refresh organizations: %v", err)
			} else {
				publish()
			}
		}
	}()
}

func refreshCLIInstallState() {
	go func() {
		installed, err := managers.IPCClientIsCLIInstalled()
		if err != nil {
			logger.Error("Failed to check CLI install state: %v", err)
			return
		}
		stateMu.Lock()
		cliInstalled = installed
		stateMu.Unlock()
		publish()
	}()
}

// watchAuthState republishes the menu when authentication or initialization changes.
func watchAuthState() {
	lastAuth := authManager != nil && authManager.IsAuthenticated()
	lastInit := authManager != nil && authManager.IsInitializing()
	for {
		time.Sleep(500 * time.Millisecond)
		if authManager == nil {
			continue
		}
		curAuth := authManager.IsAuthenticated()
		curInit := authManager.IsInitializing()
		if curAuth != lastAuth || curInit != lastInit {
			lastAuth, lastInit = curAuth, curInit
			publish()
		}
	}
}

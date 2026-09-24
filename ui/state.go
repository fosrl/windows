//go:build windows

package ui

import (
	"errors"
	"sort"
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
	eventProgressText  = "progress:text"
	eventPrefsTab      = "prefs:tab"
	eventPrefsSettings = "prefs:settings"
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

	alwaysOn      bool
	alwaysOnMutex sync.Mutex

	startupDialogOnce sync.Once
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
	case tunnel.StateStarting, tunnel.StateRegistering, tunnel.StateRegistered:
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
		Version:            version.Number,
		Year:               time.Now().Year(),
		CheckUpdateVisible: config.CheckForUpdatesButtonEnabled(),
	}

	stateMu.RLock()
	in.LoggedOut = isLoggedOut
	in.HasUpdate = hasUpdate
	in.CLIInstalled = cliInstalled
	in.CLIInstalling = cliInstallInProgress
	stateMu.RUnlock()

	state := currentTunnelState()
	in.TunnelPhase = phaseForState(state)
	in.TunnelStateText = state.DisplayText()
	if tunnelManager != nil {
		in.Connected = tunnelManager.IsConnected()
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
			if in.Accounts[i].Display != in.Accounts[j].Display {
				return in.Accounts[i].Display < in.Accounts[j].Display
			}
			return in.Accounts[i].Hostname < in.Accounts[j].Hostname
		})
		active, _ := accountManager.ActiveAccount()
		if active != nil {
			in.ActiveAccountID = active.UserID
		}

		in.LoginLabel = "Login to Account"
		if in.Authenticated {
			switch {
			case active == nil:
				in.LoginLabel = "Select Account"
			case authManager.CurrentUser() != nil:
				in.LoginLabel = auth.UserDisplayName(authManager.CurrentUser())
			default:
				in.LoginLabel = auth.AccountDisplayName(active)
			}
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
}

// handleMenuOpen verifies the session and refreshes organizations when the tray popup opens.
func handleMenuOpen() {
	if authManager == nil || apiClient == nil || !authManager.IsAuthenticated() {
		return
	}

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

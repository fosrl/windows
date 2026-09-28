//go:build windows

package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/auth"
	"github.com/fosrl/windows/managers"
)

// AccountRow is one account in Preferences > Accounts.
type AccountRow struct {
	UserID      string `json:"userId"`
	DisplayName string `json:"displayName"`
	// Host is the server's host name.
	Host     string `json:"host"`
	Hostname string `json:"hostname"`
	Subtitle string `json:"subtitle"`
	Active   bool   `json:"active"`
	// Locked accounts must log in again: the session expired or no token is stored.
	Locked bool `json:"locked"`
}

// AccountsView is everything the Accounts tab renders.
type AccountsView struct {
	Accounts []AccountRow `json:"accounts"`
	// BusyUserID is the account being switched to or removed.
	BusyUserID     string `json:"busyUserId"`
	TunnelStarting bool   `json:"tunnelStarting"`
}

func currentAccountsView() AccountsView {
	view := AccountsView{Accounts: []AccountRow{}}
	if accountManager == nil || authManager == nil {
		return view
	}

	stateMu.RLock()
	view.BusyUserID = switchingAccountID
	if view.BusyUserID == "" {
		view.BusyUserID = removingAccountID
	}
	stateMu.RUnlock()
	view.TunnelStarting = phaseForState(currentTunnelState()) == phaseStarting

	activeID := accountManager.ActiveUserID
	var orgName string
	if org := authManager.CurrentOrg(); org != nil {
		orgName = org.Name
	}
	for _, a := range accountManager.Accounts {
		a := a
		row := AccountRow{
			UserID:      a.UserID,
			DisplayName: auth.AccountDisplayName(&a),
			Host:        displayHost(a.Hostname),
			Hostname:    a.Hostname,
			Active:      a.UserID == activeID,
		}
		if row.Host == "the server" {
			row.Host = a.Hostname
		}
		row.Locked = (row.Active && authManager.SessionExpired()) || !authManager.HasSession(a.UserID)
		switch {
		case row.Locked:
			row.Subtitle = row.Host + " · Login required"
		case row.Active && orgName != "":
			row.Subtitle = row.Host + " · " + orgName
		default:
			row.Subtitle = row.Host
		}
		view.Accounts = append(view.Accounts, row)
	}
	sort.Slice(view.Accounts, func(i, j int) bool {
		return strings.ToLower(view.Accounts[i].DisplayName) < strings.ToLower(view.Accounts[j].DisplayName)
	})
	return view
}

// deleteAccount signs out of an account and removes it, as Remove… in
// Preferences and Log Out in the tray both do. Removing the active account
// stops the tunnel and switches to the next account.
func deleteAccount(userID string) {
	if authManager == nil || accountManager == nil {
		return
	}
	if _, ok := accountManager.Accounts[userID]; !ok {
		publish()
		return
	}
	stateMu.Lock()
	busy := switchingAccountID != "" || switchingOrgID != "" || loggingOut || removingAccountID != ""
	if !busy {
		removingAccountID = userID
	}
	stateMu.Unlock()
	if busy {
		return
	}
	defer func() {
		stateMu.Lock()
		removingAccountID = ""
		stateMu.Unlock()
		publish()
	}()

	active := userID == accountManager.ActiveUserID
	if active {
		setConnectionError("")
		setLoggingOut(true)
		defer setLoggingOut(false)
		logger.Info("Stopping tunnel before removing the active account")
		if err := managers.IPCClientStopTunnel(); err != nil {
			logger.Error("Failed to stop tunnel before removing account: %v", err)
		}
	} else {
		publish()
	}
	if err := authManager.DeleteAccount(userID); err != nil {
		logger.Error("Failed to remove account: %v", err)
		showError(preferencesWindowOrNil(), "Remove Account Failed", fmt.Sprintf("Failed to remove the account: %v", err))
	}
}

package main

import (
	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/api"
	"github.com/fosrl/windows/auth"
	"github.com/fosrl/windows/config"
	"github.com/fosrl/windows/secrets"
	"github.com/fosrl/windows/ui"
)

// runUI creates the managers the UI works with and runs the tray UI until it
// quits. startupWarning, if set, is shown once the UI is up.
func runUI(startupWarning string) error {
	// Initialize managers
	accountManager := config.NewAccountManager()
	configManager := config.NewConfigManager()
	secretManager := secrets.NewSecretManager()

	var hostname string
	if activeAccount, _ := accountManager.ActiveAccount(); activeAccount != nil {
		hostname = activeAccount.Hostname
	} else {
		hostname = config.DefaultHostname
	}

	apiClient := api.NewAPIClient(hostname, "")
	apiClient.SetSessionCookieName(configManager.GetSessionCookieName())
	authManager := auth.NewAuthManager(apiClient, configManager, accountManager, secretManager)

	// When any authenticated request gets 401/403, mark the session expired.
	// This runs asynchronously, as walk's Synchronize did, because the request
	// may have been made while the auth manager holds its lock.
	apiClient.SetOnUnauthorized(func() {
		go authManager.MarkSessionExpired()
	})

	// Initialize auth manager (loads saved session token if available)
	if err := authManager.Initialize(); err != nil {
		logger.Error("Failed to initialize auth manager: %v", err)
	}

	return ui.Run(ui.Deps{
		Auth:     authManager,
		Config:   configManager,
		Accounts: accountManager,
		API:      apiClient,
		Secrets:  secretManager,

		StartupWarning: startupWarning,
	})
}

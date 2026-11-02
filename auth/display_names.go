package auth

import (
	"github.com/fosrl/windows/api"
	"github.com/fosrl/windows/config"
)

// UserDisplayName returns a display name for a user with precedence:
// email > name > username > "User"
func UserDisplayName(user *api.User) string {
	if user == nil {
		return "User"
	}

	if user.Email != "" {
		return user.Email
	}

	if user.Name != nil && *user.Name != "" {
		return *user.Name
	}

	if user.Username != nil && *user.Username != "" {
		return *user.Username
	}

	return "User"
}

// AccountDisplayName returns a display name for an account with precedence:
// email > name > username > "Account"
func AccountDisplayName(account *config.Account) string {
	if account == nil {
		return "Account"
	}

	if account.Email != "" {
		return account.Email
	}

	if account.Name != "" {
		return account.Name
	}

	if account.Username != "" {
		return account.Username
	}

	return "Account"
}

//go:build windows

package secrets

import "github.com/fosrl/windows/managers/secretstore"

// IPCAPI is implemented by the manager IPC client and registered at UI startup.
type IPCAPI interface {
	Ready() bool
	GetUserSecrets(userID string) (secretstore.UserSecrets, error)
	SaveUserSecrets(userID string, update secretstore.SecretsUpdate) error
	DeleteUserSecrets(userID string, flags secretstore.DeleteSecretsFlags) error
}

var ipc IPCAPI

// SetIPCAPI registers the manager IPC implementation (call after InitializeIPCClient).
func SetIPCAPI(client IPCAPI) {
	ipc = client
}

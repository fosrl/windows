//go:build windows

package managers

import (
	"github.com/fosrl/windows/managers/secretstore"
	"github.com/fosrl/windows/secrets"
)

type secretsIPCBridge struct{}

func (secretsIPCBridge) Ready() bool {
	return IPCClientReady()
}

func (secretsIPCBridge) GetUserSecrets(userID string) (secretstore.UserSecrets, error) {
	return IPCClientGetUserSecrets(userID)
}

func (secretsIPCBridge) SaveUserSecrets(userID string, update secretstore.SecretsUpdate) error {
	return IPCClientSaveUserSecrets(userID, update)
}

func (secretsIPCBridge) DeleteUserSecrets(userID string, flags secretstore.DeleteSecretsFlags) error {
	return IPCClientDeleteUserSecrets(userID, flags)
}

func registerSecretsIPC() {
	secrets.SetIPCAPI(secretsIPCBridge{})
}

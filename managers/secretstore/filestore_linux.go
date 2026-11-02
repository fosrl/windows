package secretstore

import "errors"

// errNotImplemented marks the Linux secret store, which arrives with the
// Linux daemon (root-owned files under /var/lib/pangolin-client, keyed by UID).
var errNotImplemented = errors.New("secret store is not implemented on Linux yet")

func userSecretsPath(userKey, userID string) (string, error) { return "", errNotImplemented }

func readSecretsFile(path string) ([]byte, error) { return nil, errNotImplemented }

func removeSecretsFile(path string) error { return errNotImplemented }

func writeLockedDownFile(path string, data []byte) error { return errNotImplemented }

func encrypt(plaintext []byte, userKey, userID string) ([]byte, error) {
	return nil, errNotImplemented
}

func decrypt(ciphertext []byte, userKey, userID string) ([]byte, error) {
	return nil, errNotImplemented
}

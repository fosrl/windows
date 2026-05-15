//go:build windows

package secretstore

// UserSecrets holds per-Pangolin-account credentials for one Windows user profile.
type UserSecrets struct {
	SessionToken string `json:"sessionToken,omitempty"`
	OlmId        string `json:"olmId,omitempty"`
	OlmSecret    string `json:"olmSecret,omitempty"`
}

// SecretsUpdate describes fields to merge when saving secrets.
type SecretsUpdate struct {
	Secrets UserSecrets

	SetSessionToken bool
	SetOlmId        bool
	SetOlmSecret    bool
}

// DeleteSecretsFlags selects which credential fields to remove.
type DeleteSecretsFlags struct {
	SessionToken   bool
	OlmCredentials bool
}

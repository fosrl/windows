package secrets

// migrateFromCredentialManager is a no-op on Linux: there was never a
// Credential Manager store to migrate from.
func migrateFromCredentialManager() error { return nil }

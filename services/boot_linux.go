package services

// StartedAtBoot reports whether the daemon was started at boot. The Linux
// daemon arrives in a later step; until then this is always false.
func StartedAtBoot() bool { return false }

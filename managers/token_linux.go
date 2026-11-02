package managers

// userToken is non-zero when the connected user may run admin actions
// (updates, CLI install). The Linux daemon will set it for sudo/wheel members.
type userToken = uintptr

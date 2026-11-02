package managers

import "golang.org/x/sys/windows"

// userToken is the connected user's elevated token, or 0 if they are not an admin.
type userToken = windows.Token

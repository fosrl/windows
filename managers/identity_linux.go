package managers

import (
	"maps"
	"os/user"
	"strconv"

	"github.com/fosrl/windows/fingerprint"
)

// userToken is non-zero when the connected user may run admin actions such as
// installing updates: root, or a member of the sudo, wheel or admin group.
type userToken = uintptr

// adminGroups are the groups that grant sudo on the major distributions.
var adminGroups = map[string]bool{"sudo": true, "wheel": true, "admin": true}

// tokenForUID returns the userToken for uid.
func tokenForUID(uid uint32) userToken {
	if uid == 0 {
		return 1
	}
	u, err := user.LookupId(strconv.FormatUint(uint64(uid), 10))
	if err != nil {
		return 0
	}
	gids, err := u.GroupIds()
	if err != nil {
		return 0
	}
	for _, gid := range gids {
		if g, err := user.LookupGroupId(gid); err == nil && adminGroups[g.Name] {
			return 1
		}
	}
	return 0
}

// postureForClient reports the user who started the tunnel instead of root,
// which gathered the cached posture.
func postureForClient(snapshot fingerprint.DevicePostureSnapshot, clientUserKey string) fingerprint.DevicePostureSnapshot {
	u, err := user.LookupId(clientUserKey)
	if err != nil || snapshot.Fingerprint == nil {
		return snapshot
	}
	fp := maps.Clone(snapshot.Fingerprint)
	fp["username"] = u.Username
	snapshot.Fingerprint = fp
	return snapshot
}

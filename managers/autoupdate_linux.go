package managers

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/fosrl/windows-linux/config"
	"golang.org/x/sys/unix"
)

// maxUserConfigSize bounds how much of a user's config file the daemon reads.
const maxUserConfigSize = 1024 * 1024

// sessionAutoDownloadUpdates reports whether the user uid has automatic
// update downloads on, in the machine config or their own. If their config
// can't be read, only the machine config counts.
func sessionAutoDownloadUpdates(uid uint32) (bool, error) {
	data, err := readUserConfig(uid)
	return config.AutoDownloadUpdatesEnabledWithUserConfig(data), err
}

// readUserConfig reads uid's config file, ~/.config/pangolin-client/pangolin.json.
// The daemon runs as root and the user controls the file, so it must be a
// regular file the user owns: a symlink, FIFO or device is refused.
func readUserConfig(uid uint32) ([]byte, error) {
	u, err := user.LookupId(strconv.FormatUint(uint64(uid), 10))
	if err != nil {
		return nil, err
	}
	path := filepath.Join(config.UserDataDirFor(u.HomeDir), config.ConfigFileName)
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil, nil
		}
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return nil, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != uid {
		return nil, fmt.Errorf("%s is not a regular file owned by uid %d", path, uid)
	}
	return io.ReadAll(io.LimitReader(f, maxUserConfigSize))
}

// signedInSessions returns the UIDs of users with a login session. logind
// keeps /run/user/<uid> while a user is signed in.
func signedInSessions() []uint32 {
	entries, _ := os.ReadDir("/run/user")
	var uids []uint32
	for _, e := range entries {
		if uid, err := strconv.ParseUint(e.Name(), 10, 32); err == nil && uid != 0 && e.IsDir() {
			uids = append(uids, uint32(uid))
		}
	}
	return uids
}

// userSignedIn reports whether logind still has an open session for uid, so
// a UI that went away was quit (or crashed) rather than closed by sign-out.
func userSignedIn(uid uint32) bool {
	out, err := exec.Command("loginctl", "show-user", strconv.FormatUint(uint64(uid), 10),
		"--property=State", "--value").Output()
	if err != nil {
		return false
	}
	switch strings.TrimSpace(string(out)) {
	case "active", "online":
		return true
	}
	return false
}

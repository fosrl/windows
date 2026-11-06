package secretstore

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/fosrl/windows-linux/config"
	"golang.org/x/sys/unix"
)

// On Linux the daemon keeps each user's secrets in root-only files:
// /var/lib/pangolin-client/credentials/<uid>/<user id>.secrets.json. There is
// no DPAPI equivalent for a system service, so as with NetworkManager's
// system connections, the protection is that only root can read them.

const (
	secretsFileSuffix  = ".secrets.json"
	credentialsDirName = "credentials"
)

func credentialsRoot() string {
	return filepath.Join(config.GetProgramDataDir(), credentialsDirName)
}

// ValidateUID ensures userKey is a numeric UID, safe as a directory name.
func ValidateUID(userKey string) error {
	if userKey == "" {
		return errors.New("uid is empty")
	}
	if _, err := strconv.ParseUint(userKey, 10, 32); err != nil {
		return fmt.Errorf("invalid uid %q", userKey)
	}
	return nil
}

func userSecretsPath(userKey, userID string) (string, error) {
	if err := ValidateUID(userKey); err != nil {
		return "", err
	}
	if err := ValidateUserID(userID); err != nil {
		return "", err
	}
	root := credentialsRoot()
	if err := ensurePrivateDir(filepath.Dir(root)); err != nil {
		return "", err
	}
	if err := ensurePrivateDir(root); err != nil {
		return "", err
	}
	userDir := filepath.Join(root, userKey)
	if err := ensurePrivateDir(userDir); err != nil {
		return "", err
	}
	path := filepath.Join(userDir, userID+secretsFileSuffix)
	if filepath.Dir(path) != userDir {
		return "", errors.New("invalid secrets path")
	}
	return path, nil
}

// ensurePrivateDir creates dir as root-only, or checks an existing one is a
// real directory owned by root and not writable by anyone else.
func ensurePrivateDir(dir string) error {
	if err := os.Mkdir(dir, 0o700); err != nil && !os.IsExist(err) {
		return err
	}
	var st unix.Stat_t
	if err := unix.Lstat(dir, &st); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR {
		return fmt.Errorf("not a directory: %s", dir)
	}
	if st.Uid != 0 || st.Mode&0o022 != 0 {
		return fmt.Errorf("unsafe ownership or permissions on %s", dir)
	}
	return nil
}

func readSecretsFile(path string) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file: %s", path)
	}
	buf := make([]byte, fi.Size())
	if _, err := f.ReadAt(buf, 0); err != nil && fi.Size() > 0 {
		return nil, err
	}
	return buf, nil
}

func removeSecretsFile(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// writeLockedDownFile writes data to a 0600 temp file in the same directory,
// syncs it and renames it over path.
func writeLockedDownFile(path string, data []byte) error {
	var name [16]byte
	if _, err := rand.Read(name[:]); err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(path), "."+hex.EncodeToString(name[:])+".tmp")
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	serr := f.Sync()
	cerr := f.Close()
	if err := errors.Join(werr, serr, cerr); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// encrypt and decrypt pass data through: the files are protected by
// permissions, as above.
func encrypt(plaintext []byte, userKey, userID string) ([]byte, error) {
	if len(plaintext) == 0 {
		return nil, errors.New("cannot store empty data")
	}
	return plaintext, nil
}

func decrypt(ciphertext []byte, userKey, userID string) ([]byte, error) {
	return ciphertext, nil
}

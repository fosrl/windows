package updater

import (
	"crypto/hmac"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows-linux/config"
	"golang.org/x/crypto/blake2b"
)

// A downloaded update is kept in /var/lib/pangolin-client/updates so it
// survives reboots and can be installed the next time Pangolin quits or
// launches, as on Windows. The signed manifest it came from is kept beside it,
// and both are checked again before installing. The directory is root-only.
const (
	stagedDirName      = "updates"
	stagedPackageName  = "pending" // plus the package's suffix: .deb, .rpm or .pkg.tar.zst
	stagedManifestName = "pending.sig"
	partialSuffix      = ".part"
)

// StagedUpdate is a downloaded and verified update waiting to be installed.
type StagedUpdate struct {
	version  string
	StagedAt time.Time
}

// Version returns the version of the staged update.
func (s *StagedUpdate) Version() string { return s.version }

// Discard deletes the staged update from disk.
func (s *StagedUpdate) Discard() { discardStagedFiles() }

func stagedDir() string { return filepath.Join(config.GetProgramDataDir(), stagedDirName) }

// stagedPackagePath keeps the package's suffix, which apt and pacman need to
// treat the argument as a file.
func stagedPackagePath() string {
	_, suffix := candidatePattern()
	return filepath.Join(stagedDir(), stagedPackageName+suffix)
}

func stagedManifestPath() string { return filepath.Join(stagedDir(), stagedManifestName) }

// discardStagedFiles empties the staged update directory, including leftovers
// from an interrupted download or an older version of the updater.
func discardStagedFiles() {
	if !stagedDirTrusted() {
		return
	}
	entries, _ := os.ReadDir(stagedDir())
	for _, e := range entries {
		path := filepath.Join(stagedDir(), e.Name())
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			logger.Debug("Updater: failed to remove %s: %v", path, err)
		}
	}
}

// stagedDirTrusted reports whether the staged update directory is a real
// directory owned by root that only root can write to.
func stagedDirTrusted() bool {
	fi, err := os.Lstat(stagedDir())
	if err != nil || !fi.IsDir() || fi.Mode().Perm()&0o022 != 0 {
		return false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && st.Uid == 0
}

// ensureStagedDir creates the staged update directory, replacing anything
// untrusted in its place.
func ensureStagedDir() error {
	if err := os.MkdirAll(config.GetProgramDataDir(), 0o700); err != nil {
		return err
	}
	if stagedDirTrusted() {
		return nil
	}
	if _, err := os.Lstat(stagedDir()); err == nil {
		logger.Error("Updater: replacing untrusted update directory %s", stagedDir())
		if err := os.RemoveAll(stagedDir()); err != nil {
			return fmt.Errorf("untrusted update directory: %w", err)
		}
	}
	if err := os.Mkdir(stagedDir(), 0o700); err != nil {
		return err
	}
	return os.Chmod(stagedDir(), 0o700)
}

// createStagingFile creates the file a download is written to before it is verified.
func createStagingFile() (*os.File, error) {
	if err := ensureStagedDir(); err != nil {
		return nil, fmt.Errorf("failed to create update directory: %w", err)
	}
	return os.OpenFile(stagedPackagePath()+partialSuffix, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
}

// commitStagedFiles replaces any staged update with the verified download and its manifest.
func commitStagedFiles(manifest []byte) error {
	if err := os.WriteFile(stagedManifestPath()+partialSuffix, manifest, 0o600); err != nil {
		return err
	}
	// Remove the old manifest first so a half-replaced pair is never treated as staged.
	if err := os.Remove(stagedManifestPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(stagedPackagePath()+partialSuffix, stagedPackagePath()); err != nil {
		return err
	}
	return os.Rename(stagedManifestPath()+partialSuffix, stagedManifestPath())
}

// stagedCandidate verifies the staged manifest and returns the update it
// describes, or nil when nothing is staged or it isn't newer than this version.
func stagedCandidate() (*UpdateFound, error) {
	manifest, err := os.ReadFile(stagedManifestPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	files, err := readFileList(manifest)
	if err != nil {
		return nil, err
	}
	return findCandidate(files)
}

// LoadStagedUpdate returns the update staged by an earlier download, if it is
// still newer than the running version. Anything else in the staged directory
// is removed, such as the update that has just been installed.
func LoadStagedUpdate() *StagedUpdate {
	if !stagedDirTrusted() {
		return nil
	}
	update, err := stagedCandidate()
	if err != nil {
		logger.Error("Updater: discarding invalid staged update: %v", err)
		discardStagedFiles()
		return nil
	}
	if update == nil {
		discardStagedFiles()
		return nil
	}
	info, err := os.Stat(stagedPackagePath())
	if err != nil {
		logger.Error("Updater: discarding staged update %s: %v", update.version, err)
		discardStagedFiles()
		return nil
	}
	logger.Info("Updater: found staged update %s", update.version)
	return &StagedUpdate{version: update.version, StagedAt: info.ModTime()}
}

// verifyStagedPackage checks the staged package against its signed manifest
// and returns its path.
func verifyStagedPackage() (string, error) {
	if !stagedDirTrusted() {
		return "", errors.New("the update directory is not trusted")
	}
	update, err := stagedCandidate()
	if err != nil {
		return "", err
	}
	if update == nil {
		return "", errors.New("the staged update is missing or not newer than the installed version")
	}
	path := stagedPackagePath()
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open staged update: %w", err)
	}
	defer f.Close()
	hasher, _ := blake2b.New256(nil)
	if _, err := io.Copy(hasher, io.LimitReader(f, maxPackageSize)); err != nil {
		return "", err
	}
	if !hmac.Equal(hasher.Sum(nil), update.hash[:]) {
		return "", errors.New("the staged update has the wrong hash")
	}
	logger.Debug("Updater: staged update %s verified", update.version)
	return path, nil
}

// RestartUIFile lists the UIDs whose UI should be opened once the updated
// daemon is running: the UIs open when an update started.
var RestartUIFile = filepath.Join(config.GetProgramDataDir(), "restart-ui-after-update")

// RelaunchUIAfterUpdate asks the daemon that starts after the next update to
// open the UI for uid. Zero is ignored: there is no console session to fall
// back to on Linux.
func RelaunchUIAfterUpdate(uid uint32) error {
	if uid == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(RestartUIFile), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(RestartUIFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "%d\n", uid)
	return err
}

// CancelUIRelaunchAfterUpdate removes the relaunch request after a failed update.
func CancelUIRelaunchAfterUpdate() {
	if err := os.Remove(RestartUIFile); err != nil && !os.IsNotExist(err) {
		logger.Debug("Updater: failed to remove %s: %v", RestartUIFile, err)
	}
}

// TakeUIRelaunchAfterUpdate consumes the relaunch request left by an update,
// returning the UIDs to open the UI for and whether there was a request.
func TakeUIRelaunchAfterUpdate() (uids []uint32, requested bool) {
	data, err := os.ReadFile(RestartUIFile)
	if err != nil {
		return nil, false
	}
	CancelUIRelaunchAfterUpdate()
	seen := map[uint32]bool{}
	for _, field := range strings.Fields(string(data)) {
		if uid, err := strconv.ParseUint(field, 10, 32); err == nil && uid != 0 && !seen[uint32(uid)] {
			seen[uint32(uid)] = true
			uids = append(uids, uint32(uid))
		}
	}
	return uids, true
}

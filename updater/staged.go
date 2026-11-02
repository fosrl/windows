//go:build windows

package updater

import (
	"crypto/hmac"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/crypto/blake2b"
	"golang.org/x/sys/windows"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/config"
)

// A downloaded update is kept under %ProgramData%\Pangolin\updates so it survives
// reboots and can be installed the next time Pangolin launches. The signed manifest
// it came from is kept beside it, and both are verified again before installing, so
// a file planted or swapped in that directory is never installed.
const (
	stagedDirName      = "updates"
	stagedMsiName      = "pending.msi"
	stagedManifestName = "pending.sig"
	partialSuffix      = ".part"
	maxMsiSize         = 1024 * 1024 * 100 // 100 MiB
	restartUIFlagName  = "restart-ui-after-update.flag"
)

// StagedUpdate is a downloaded and verified update waiting to be installed.
type StagedUpdate struct {
	version  string
	StagedAt time.Time
}

// Version returns the version of the staged update
func (s *StagedUpdate) Version() string {
	return s.version
}

// Discard deletes the staged update from disk
func (s *StagedUpdate) Discard() {
	discardStagedFiles()
}

func stagedDir() string {
	return filepath.Join(config.GetProgramDataDir(), stagedDirName)
}

func stagedMsiPath() string {
	return filepath.Join(stagedDir(), stagedMsiName)
}

func stagedManifestPath() string {
	return filepath.Join(stagedDir(), stagedManifestName)
}

func discardStagedFiles() {
	if !stagedDirTrusted() {
		return
	}
	for _, path := range []string{stagedManifestPath(), stagedMsiPath(), stagedMsiPath() + partialSuffix, stagedManifestPath() + partialSuffix} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			logger.Debug("Updater: Failed to remove %s: %v", path, err)
		}
	}
}

// stagedDirTrusted reports whether the staged update directory is a real directory
// owned by SYSTEM or administrators. Standard users can create directories under
// %ProgramData%, so one planted there (or a junction pointing elsewhere) must not be
// written to or deleted from by the service.
func stagedDirTrusted() bool {
	dir16, err := windows.UTF16PtrFromString(stagedDir())
	if err != nil {
		return false
	}
	attrs, err := windows.GetFileAttributes(dir16)
	if err != nil || attrs&windows.FILE_ATTRIBUTE_DIRECTORY == 0 || attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return false
	}
	sd, err := windows.GetNamedSecurityInfo(stagedDir(), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return false
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil {
		return false
	}
	return owner.IsWellKnown(windows.WinLocalSystemSid) || owner.IsWellKnown(windows.WinBuiltinAdministratorsSid)
}

// ensureStagedDir creates the staged update directory, writable only by SYSTEM and
// administrators, replacing anything untrusted in its place.
func ensureStagedDir() error {
	if err := os.MkdirAll(config.GetProgramDataDir(), 0o755); err != nil {
		return err
	}
	if stagedDirTrusted() {
		return nil
	}
	if _, err := os.Lstat(stagedDir()); err == nil {
		logger.Error("Updater: Replacing untrusted update directory %s", stagedDir())
		// Removes a junction or symlink itself, or an empty directory.
		if err := os.Remove(stagedDir()); err != nil {
			return fmt.Errorf("untrusted update directory: %w", err)
		}
	}
	sd, err := windows.SecurityDescriptorFromString("O:SYD:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)")
	if err != nil {
		return err
	}
	sa := &windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: sd,
	}
	dir16, err := windows.UTF16PtrFromString(stagedDir())
	if err != nil {
		return err
	}
	err = windows.CreateDirectory(dir16, sa)
	runtime.KeepAlive(sd)
	if err != nil {
		return err
	}
	return nil
}

// createStagingFile creates the file a download is written to before it is verified.
func createStagingFile() (*os.File, error) {
	if err := ensureStagedDir(); err != nil {
		return nil, fmt.Errorf("failed to create update directory: %w", err)
	}
	return os.Create(stagedMsiPath() + partialSuffix)
}

// commitStagedFiles replaces any staged update with the verified download and its manifest.
func commitStagedFiles(manifest []byte) error {
	if err := os.WriteFile(stagedManifestPath()+partialSuffix, manifest, 0o644); err != nil {
		return err
	}
	// Remove the old manifest first so a half-replaced pair is never treated as staged.
	if err := os.Remove(stagedManifestPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(stagedMsiPath()+partialSuffix, stagedMsiPath()); err != nil {
		return err
	}
	return os.Rename(stagedManifestPath()+partialSuffix, stagedManifestPath())
}

// stagedCandidate verifies the staged manifest and returns the update it describes,
// or nil when nothing is staged or the staged update is not newer than this version.
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

// LoadStagedUpdate returns the update staged by an earlier download, if it is still
// newer than the running version. Anything else in the staged directory is removed.
func LoadStagedUpdate() *StagedUpdate {
	if !stagedDirTrusted() {
		return nil
	}
	update, err := stagedCandidate()
	if err != nil {
		logger.Error("Updater: Discarding invalid staged update: %v", err)
		discardStagedFiles()
		return nil
	}
	if update == nil {
		discardStagedFiles()
		return nil
	}
	info, err := os.Stat(stagedMsiPath())
	if err != nil {
		logger.Error("Updater: Discarding staged update %s: %v", update.version, err)
		discardStagedFiles()
		return nil
	}
	logger.Info("Updater: Found staged update %s", update.version)
	return &StagedUpdate{version: update.version, StagedAt: info.ModTime()}
}

// prepareStagedMsi copies the staged MSI into a SYSTEM-only temp file for msiexec,
// verifying its hash against the signed manifest as it is copied.
func prepareStagedMsi() (*tempFile, error) {
	if !stagedDirTrusted() {
		return nil, errors.New("The update directory is not trusted")
	}
	update, err := stagedCandidate()
	if err != nil {
		return nil, err
	}
	if update == nil {
		return nil, errors.New("The staged update is missing or not newer than the installed version")
	}

	path16, err := windows.UTF16PtrFromString(stagedMsiPath())
	if err != nil {
		return nil, err
	}
	// Deny writers while the file is hashed and copied.
	handle, err := windows.CreateFile(path16, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to open staged update: %w", err)
	}
	src := os.NewFile(uintptr(handle), stagedMsiPath())
	defer src.Close()

	dst, err := msiTempFile()
	if err != nil {
		return nil, err
	}
	hasher, err := blake2b.New256(nil)
	if err != nil {
		dst.Delete()
		return nil, err
	}
	if _, err := io.Copy(dst, io.TeeReader(io.LimitReader(src, maxMsiSize), hasher)); err != nil {
		dst.Delete()
		return nil, err
	}
	if !hmac.Equal(hasher.Sum(nil), update.hash[:]) {
		dst.Delete()
		return nil, errors.New("The staged update has the wrong hash")
	}
	logger.Debug("Updater: Staged update %s verified", update.version)
	return dst, nil
}

func restartUIFlagPath() string {
	return filepath.Join(config.GetProgramDataDir(), restartUIFlagName)
}

// RelaunchUIAfterUpdate asks the manager that starts after the next update to launch
// the UI in session. An empty flag means the active console session.
func RelaunchUIAfterUpdate(session uint32) error {
	if err := os.MkdirAll(config.GetProgramDataDir(), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(restartUIFlagPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if session != 0 {
		_, err = fmt.Fprintf(f, "%d\n", session)
	}
	return err
}

// CancelUIRelaunchAfterUpdate removes the relaunch request after a failed update.
func CancelUIRelaunchAfterUpdate() {
	if err := os.Remove(restartUIFlagPath()); err != nil && !os.IsNotExist(err) {
		logger.Debug("Updater: Failed to remove restart-ui flag: %v", err)
	}
}

// TakeUIRelaunchAfterUpdate consumes the relaunch request left by an update. It returns
// the sessions to launch the UI in (empty means the active console session) and
// whether there was a request at all.
func TakeUIRelaunchAfterUpdate() (sessions []uint32, requested bool) {
	data, err := os.ReadFile(restartUIFlagPath())
	if err != nil {
		return nil, false
	}
	CancelUIRelaunchAfterUpdate()
	for _, line := range strings.Fields(string(data)) {
		if session, err := strconv.ParseUint(line, 10, 32); err == nil && session != 0 {
			sessions = append(sessions, uint32(session))
		}
	}
	return sessions, true
}

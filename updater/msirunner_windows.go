package updater

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/fosrl/newt/logger"
	"golang.org/x/sys/windows"
)

type tempFile struct {
	*os.File
	originalHandle windows.Handle
}

func (t *tempFile) ExclusivePath() string {
	if t.originalHandle != 0 {
		t.Close() // TODO: sort of a toctou, but msi requires unshared file
		t.originalHandle = 0
	}
	return t.Name()
}

func (t *tempFile) Delete() error {
	if t.originalHandle == 0 {
		name16, err := windows.UTF16PtrFromString(t.Name())
		if err != nil {
			return err
		}
		return windows.DeleteFile(name16) // TODO: how does this deal with reparse points?
	}
	disposition := byte(1)
	err := windows.SetFileInformationByHandle(t.originalHandle, windows.FileDispositionInfo, &disposition, 1)
	t.originalHandle = 0
	t.Close()
	return err
}

// runMsi installs the MSI with msiexec. With silent set, msiexec shows no UI and
// never reboots; otherwise it shows basic progress without a cancel button.
func runMsi(msi *tempFile, userToken uintptr, silent bool) error {
	logger.Debug("Updater: runMsi() called with userToken: %v, silent: %v", userToken, silent)

	logger.Debug("Updater: Getting system directory")
	system32, err := windows.GetSystemDirectory()
	if err != nil {
		logger.Error("Updater: Failed to get system directory: %v", err)
		return err
	}
	logger.Debug("Updater: System directory: %s", system32)

	logger.Debug("Updater: Opening /dev/null for process I/O")
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		logger.Error("Updater: Failed to open /dev/null: %v", err)
		return err
	}
	defer devNull.Close()

	msiPath := msi.ExclusivePath()
	logger.Debug("Updater: MSI path: %s", msiPath)
	logger.Debug("Updater: MSI directory: %s", filepath.Dir(msiPath))
	logger.Debug("Updater: MSI filename: %s", filepath.Base(msiPath))

	attr := &os.ProcAttr{
		Sys: &syscall.SysProcAttr{
			Token: syscall.Token(userToken),
		},
		Files: []*os.File{devNull, devNull, devNull},
		Dir:   filepath.Dir(msiPath),
	}
	msiexec := filepath.Join(system32, "msiexec.exe")
	logger.Debug("Updater: msiexec path: %s", msiexec)
	args := []string{msiexec, "/qb!-", "/i", filepath.Base(msiPath)}
	if silent {
		args = []string{msiexec, "/qn", "/norestart", "/i", filepath.Base(msiPath)}
	}
	logger.Debug("Updater: Starting msiexec with args: %v", args[1:])

	proc, err := os.StartProcess(msiexec, args, attr)
	if err != nil {
		logger.Error("Updater: Failed to start msiexec process: %v", err)
		return fmt.Errorf("failed to start msiexec: %w", err)
	}
	logger.Debug("Updater: msiexec process started (PID: %d)", proc.Pid)

	logger.Debug("Updater: Waiting for msiexec to complete")
	state, err := proc.Wait()
	if err != nil {
		logger.Error("Updater: Error waiting for msiexec: %v", err)
		return err
	}
	logger.Debug("Updater: msiexec completed with exit code: %d", state.ExitCode())

	if !state.Success() {
		logger.Error("Updater: msiexec failed with exit code: %d", state.ExitCode())
		return &exec.ExitError{ProcessState: state}
	}
	logger.Debug("Updater: MSI installation completed successfully")
	return nil
}

// runMsiInSession installs the MSI as SYSTEM, showing basic progress without a cancel
// button on session's desktop, for an update installed while the user waits for
// Pangolin to launch. Like the silent install, it never reboots.
func runMsiInSession(msi *tempFile, session uint32) error {
	logger.Debug("Updater: runMsiInSession() called for session %d", session)

	system32, err := windows.GetSystemDirectory()
	if err != nil {
		return err
	}

	// A copy of the service's SYSTEM token moved into the user's session, so msiexec's
	// progress window shows on their desktop. Needs SeTcbPrivilege, which the manager enables.
	var processToken windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_DUPLICATE, &processToken); err != nil {
		return fmt.Errorf("failed to open process token: %w", err)
	}
	defer processToken.Close()
	var token windows.Token
	if err := windows.DuplicateTokenEx(processToken, windows.MAXIMUM_ALLOWED, nil, windows.SecurityIdentification, windows.TokenPrimary, &token); err != nil {
		return fmt.Errorf("failed to duplicate process token: %w", err)
	}
	defer token.Close()
	if err := windows.SetTokenInformation(token, windows.TokenSessionId, (*byte)(unsafe.Pointer(&session)), uint32(unsafe.Sizeof(session))); err != nil {
		return fmt.Errorf("failed to set token session: %w", err)
	}

	msiPath := msi.ExclusivePath()
	msiexec := filepath.Join(system32, "msiexec.exe")
	args := []string{msiexec, "/qb!-", "/norestart", "/i", filepath.Base(msiPath)}
	logger.Debug("Updater: Starting msiexec in session %d with args: %v", session, args[1:])
	msiexec16, err := windows.UTF16PtrFromString(msiexec)
	if err != nil {
		return err
	}
	args16, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(args))
	if err != nil {
		return err
	}
	dir16, err := windows.UTF16PtrFromString(filepath.Dir(msiPath))
	if err != nil {
		return err
	}
	desktop16, err := windows.UTF16PtrFromString(`winsta0\default`)
	if err != nil {
		return err
	}
	si := &windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfo{})), Desktop: desktop16}
	pi := new(windows.ProcessInformation)
	if err := windows.CreateProcessAsUser(token, msiexec16, args16, nil, nil, false, windows.CREATE_DEFAULT_ERROR_MODE, nil, dir16, si, pi); err != nil {
		return fmt.Errorf("failed to start msiexec: %w", err)
	}
	windows.CloseHandle(pi.Thread)
	defer windows.CloseHandle(pi.Process)
	logger.Debug("Updater: msiexec process started (PID: %d)", pi.ProcessId)

	if _, err := windows.WaitForSingleObject(pi.Process, windows.INFINITE); err != nil {
		return fmt.Errorf("failed to wait for msiexec: %w", err)
	}
	var exitCode uint32
	if err := windows.GetExitCodeProcess(pi.Process, &exitCode); err != nil {
		return err
	}
	logger.Debug("Updater: msiexec completed with exit code: %d", exitCode)
	if exitCode != 0 && exitCode != uint32(windows.ERROR_SUCCESS_REBOOT_REQUIRED) {
		return fmt.Errorf("msiexec failed with exit code %d", exitCode)
	}
	logger.Debug("Updater: MSI installation completed successfully")
	return nil
}

func msiTempFile() (*tempFile, error) {
	logger.Debug("Updater: Creating temporary MSI file")
	var randBytes [32]byte
	n, err := rand.Read(randBytes[:])
	if err != nil {
		logger.Error("Updater: Failed to generate random bytes: %v", err)
		return nil, err
	}
	if n != int(len(randBytes)) {
		logger.Error("Updater: Insufficient random bytes generated: %d", n)
		return nil, errors.New("Unable to generate random bytes")
	}
	logger.Debug("Updater: Generated random filename")

	logger.Debug("Updater: Creating security descriptor")
	sd, err := windows.SecurityDescriptorFromString("O:SYD:PAI(A;;FA;;;SY)(A;;FR;;;BA)")
	if err != nil {
		logger.Error("Updater: Failed to create security descriptor: %v", err)
		return nil, err
	}
	sa := &windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: sd,
	}

	logger.Debug("Updater: Getting Windows directory")
	windir, err := windows.GetWindowsDirectory()
	if err != nil {
		logger.Error("Updater: Failed to get Windows directory: %v", err)
		return nil, err
	}
	name := filepath.Join(windir, "Temp", hex.EncodeToString(randBytes[:]))
	logger.Debug("Updater: Temporary file path: %s", name)

	name16 := windows.StringToUTF16Ptr(name)
	logger.Debug("Updater: Creating file with SYSTEM-only access")
	fileHandle, err := windows.CreateFile(name16, windows.GENERIC_WRITE|windows.DELETE, 0, sa, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_TEMPORARY, 0)
	runtime.KeepAlive(sd)
	if err != nil {
		logger.Error("Updater: Failed to create temporary file: %v (path: %s)", err, name)
		return nil, fmt.Errorf("failed to create temporary file: %w", err)
	}
	logger.Debug("Updater: Temporary file created successfully")

	logger.Debug("Updater: Scheduling file for deletion on reboot")
	windows.MoveFileEx(name16, nil, windows.MOVEFILE_DELAY_UNTIL_REBOOT)
	return &tempFile{
		File:           os.NewFile(uintptr(fileHandle), name),
		originalHandle: fileHandle,
	}, nil
}

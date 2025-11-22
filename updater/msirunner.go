//go:build windows

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

func runMsi(msi *tempFile, userToken uintptr) error {
	logger.Info("Updater: runMsi() called with userToken: %v", userToken)

	logger.Info("Updater: Getting system directory")
	system32, err := windows.GetSystemDirectory()
	if err != nil {
		logger.Error("Updater: Failed to get system directory: %v", err)
		return err
	}
	logger.Info("Updater: System directory: %s", system32)

	logger.Info("Updater: Opening /dev/null for process I/O")
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		logger.Error("Updater: Failed to open /dev/null: %v", err)
		return err
	}
	defer devNull.Close()

	msiPath := msi.ExclusivePath()
	logger.Info("Updater: MSI path: %s", msiPath)
	logger.Info("Updater: MSI directory: %s", filepath.Dir(msiPath))
	logger.Info("Updater: MSI filename: %s", filepath.Base(msiPath))

	attr := &os.ProcAttr{
		Sys: &syscall.SysProcAttr{
			Token: syscall.Token(userToken),
		},
		Files: []*os.File{devNull, devNull, devNull},
		Dir:   filepath.Dir(msiPath),
	}
	msiexec := filepath.Join(system32, "msiexec.exe")
	logger.Info("Updater: msiexec path: %s", msiexec)
	logger.Info("Updater: Starting msiexec with args: /qb!- /i %s", filepath.Base(msiPath))

	proc, err := os.StartProcess(msiexec, []string{msiexec, "/qb!-", "/i", filepath.Base(msiPath)}, attr)
	if err != nil {
		logger.Error("Updater: Failed to start msiexec process: %v", err)
		return fmt.Errorf("failed to start msiexec: %w", err)
	}
	logger.Info("Updater: msiexec process started (PID: %d)", proc.Pid)

	logger.Info("Updater: Waiting for msiexec to complete")
	state, err := proc.Wait()
	if err != nil {
		logger.Error("Updater: Error waiting for msiexec: %v", err)
		return err
	}
	logger.Info("Updater: msiexec completed with exit code: %d", state.ExitCode())

	if !state.Success() {
		logger.Error("Updater: msiexec failed with exit code: %d", state.ExitCode())
		return &exec.ExitError{ProcessState: state}
	}
	logger.Info("Updater: MSI installation completed successfully")
	return nil
}

func msiTempFile() (*tempFile, error) {
	logger.Info("Updater: Creating temporary MSI file")
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
	logger.Info("Updater: Generated random filename")

	logger.Info("Updater: Creating security descriptor")
	sd, err := windows.SecurityDescriptorFromString("O:SYD:PAI(A;;FA;;;SY)(A;;FR;;;BA)")
	if err != nil {
		logger.Error("Updater: Failed to create security descriptor: %v", err)
		return nil, err
	}
	sa := &windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: sd,
	}

	logger.Info("Updater: Getting Windows directory")
	windir, err := windows.GetWindowsDirectory()
	if err != nil {
		logger.Error("Updater: Failed to get Windows directory: %v", err)
		return nil, err
	}
	name := filepath.Join(windir, "Temp", hex.EncodeToString(randBytes[:]))
	logger.Info("Updater: Temporary file path: %s", name)

	name16 := windows.StringToUTF16Ptr(name)
	logger.Info("Updater: Creating file with SYSTEM-only access")
	fileHandle, err := windows.CreateFile(name16, windows.GENERIC_WRITE|windows.DELETE, 0, sa, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_TEMPORARY, 0)
	runtime.KeepAlive(sd)
	if err != nil {
		logger.Error("Updater: Failed to create temporary file: %v (path: %s)", err, name)
		return nil, fmt.Errorf("failed to create temporary file: %w", err)
	}
	logger.Info("Updater: Temporary file created successfully")

	logger.Info("Updater: Scheduling file for deletion on reboot")
	windows.MoveFileEx(name16, nil, windows.MOVEFILE_DELAY_UNTIL_REBOOT)
	return &tempFile{
		File:           os.NewFile(uintptr(fileHandle), name),
		originalHandle: fileHandle,
	}, nil
}

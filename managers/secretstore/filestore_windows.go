//go:build windows

package secretstore

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"github.com/fosrl/windows/config"
	"golang.org/x/sys/windows"
)

var (
	advapi32                                                  = windows.NewLazySystemDLL("advapi32.dll")
	procConvertStringSecurityDescriptorToSecurityDescriptorW = advapi32.NewProc("ConvertStringSecurityDescriptorToSecurityDescriptorW")
	procSetFileSecurityW                                      = advapi32.NewProc("SetFileSecurityW")
)

const (
	secretsFileSuffix   = ".secrets.dpapi"
	credentialsDirName  = "Credentials"
	credentialsRootSDDL = "O:SYG:SYD:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"
	// Administrators get full control on secret files so elevated admins can back up, delete, or fix
	// the store in Explorer without takeown/icacls. Plaintext remains DPAPI-protected for LOCAL SYSTEM.
	secretsFileSDDL = "O:SYG:SYD:PAI(A;;FA;;;SY)(A;;FA;;;BA)"
)

func credentialsRoot() string {
	return filepath.Join(config.GetProgramDataDir(), credentialsDirName)
}

func userSecretsPath(windowsSID, userID string) (string, error) {
	if err := ValidateWindowsSID(windowsSID); err != nil {
		return "", err
	}
	if err := ValidateUserID(userID); err != nil {
		return "", err
	}
	root, err := ensureCredentialsRoot()
	if err != nil {
		return "", err
	}
	userDir := filepath.Join(root, windowsSID)
	if err := ensureUserDir(userDir); err != nil {
		return "", err
	}
	path := filepath.Join(userDir, userID+secretsFileSuffix)
	if filepath.Dir(path) != userDir {
		return "", errors.New("invalid secrets path")
	}
	return path, nil
}

func ensureCredentialsRoot() (string, error) {
	root := credentialsRoot()
	if err := verifyNotReparsePoint(root, true); err == nil {
		return root, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}

	if err := os.MkdirAll(root, 0); err != nil {
		return "", err
	}
	if err := applySecurityDescriptor(root, credentialsRootSDDL); err != nil {
		return "", err
	}
	return root, verifyNotReparsePoint(root, true)
}

func ensureUserDir(userDir string) error {
	if err := verifyNotReparsePoint(userDir, true); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Mkdir(userDir, 0); err != nil {
		return err
	}
	return applySecurityDescriptor(userDir, credentialsRootSDDL)
}

func verifyNotReparsePoint(path string, allowMissing bool) error {
	fi, err := os.Lstat(path)
	if err != nil {
		if allowMissing && os.IsNotExist(err) {
			return err
		}
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("path is a symlink: %s", path)
	}
	// Detect reparse points (junctions, etc.)
	p := windows.StringToUTF16Ptr(path)
	attrs, err := windows.GetFileAttributes(p)
	if err != nil {
		return err
	}
	if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return fmt.Errorf("path is a reparse point: %s", path)
	}
	return nil
}

func applySecurityDescriptor(path, sddl string) error {
	sddlPtr, err := windows.UTF16PtrFromString(sddl)
	if err != nil {
		return err
	}
	var sd *windows.SECURITY_DESCRIPTOR
	var sdSize uint32
	r0, _, e1 := procConvertStringSecurityDescriptorToSecurityDescriptorW.Call(
		uintptr(unsafe.Pointer(sddlPtr)),
		1,
		uintptr(unsafe.Pointer(&sd)),
		uintptr(unsafe.Pointer(&sdSize)),
	)
	if r0 == 0 {
		if e1 != nil && e1 != syscall.Errno(0) {
			return e1
		}
		return syscall.EINVAL
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(sd)))

	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	securityInfo := uintptr(windows.OWNER_SECURITY_INFORMATION | windows.GROUP_SECURITY_INFORMATION | windows.DACL_SECURITY_INFORMATION)
	r0, _, e1 = procSetFileSecurityW.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		securityInfo,
		uintptr(unsafe.Pointer(sd)),
		0,
	)
	if r0 == 0 {
		if e1 != nil && e1 != syscall.Errno(0) {
			return e1
		}
		return syscall.EINVAL
	}
	return nil
}

func writeLockedDownFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := verifyNotReparsePoint(dir, false); err != nil {
		return err
	}

	var tempName [16]byte
	if _, err := rand.Read(tempName[:]); err != nil {
		return err
	}
	tempPath := filepath.Join(dir, "."+hex.EncodeToString(tempName[:])+".tmp")

	if err := os.WriteFile(tempPath, data, 0); err != nil {
		return err
	}
	if err := applySecurityDescriptor(tempPath, secretsFileSDDL); err != nil {
		os.Remove(tempPath)
		return err
	}
	// Windows: Renaming over an existing file often fails with ERROR_ACCESS_DENIED when the
	// destination has restrictive DACLs (WireGuard-style SY-only + BA delete). Remove the
	// destination first, then rename — LocalSystem owns the path and has full access.
	if err := removePathForReplace(path); err != nil {
		os.Remove(tempPath)
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		os.Remove(tempPath)
		return err
	}
	return nil
}

// removePathForReplace deletes an existing file so a temp file can be renamed into place.
func removePathForReplace(path string) error {
	_, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	attrs, e0 := windows.GetFileAttributes(pathPtr)
	if e0 == nil && attrs != windows.INVALID_FILE_ATTRIBUTES && attrs&windows.FILE_ATTRIBUTE_READONLY != 0 {
		_ = windows.SetFileAttributes(pathPtr, attrs&^windows.FILE_ATTRIBUTE_READONLY)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func readSecretsFile(path string) ([]byte, error) {
	if err := verifyNotReparsePoint(path, false); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func removeSecretsFile(path string) error {
	if err := verifyNotReparsePoint(path, true); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

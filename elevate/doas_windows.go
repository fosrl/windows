package elevate

import (
	"errors"
	"os"
	"runtime"
	"strings"
	"unsafe"

	"github.com/fosrl/newt/logger"
	"golang.org/x/sys/windows"
)

func setAllEnv(env []string) {
	windows.Clearenv()
	for _, e := range env {
		k, v, ok := strings.Cut(e, "=")
		if !ok {
			continue
		}
		windows.Setenv(k, v)
	}
}

func DoAsSystem(f func() error) error {
	logger.Info("Elevate: DoAsSystem() called - attempting to elevate to SYSTEM")
	runtime.LockOSThread()
	defer func() {
		logger.Info("Elevate: Reverting to self and unlocking thread")
		windows.RevertToSelf()
		runtime.UnlockOSThread()
	}()

	logger.Info("Elevate: Looking up SeDebugPrivilege")
	privileges := windows.Tokenprivileges{
		PrivilegeCount: 1,
		Privileges: [1]windows.LUIDAndAttributes{
			{
				Attributes: windows.SE_PRIVILEGE_ENABLED,
			},
		},
	}
	err := windows.LookupPrivilegeValue(nil, windows.StringToUTF16Ptr("SeDebugPrivilege"), &privileges.Privileges[0].Luid)
	if err != nil {
		logger.Error("Elevate: Failed to lookup SeDebugPrivilege: %v", err)
		return err
	}
	logger.Info("Elevate: SeDebugPrivilege found")

	logger.Info("Elevate: Impersonating self")
	err = windows.ImpersonateSelf(windows.SecurityImpersonation)
	if err != nil {
		logger.Error("Elevate: Failed to impersonate self: %v", err)
		return err
	}

	logger.Info("Elevate: Opening thread token")
	var threadToken windows.Token
	err = windows.OpenThreadToken(windows.CurrentThread(), windows.TOKEN_QUERY|windows.TOKEN_ADJUST_PRIVILEGES, false, &threadToken)
	if err != nil {
		logger.Error("Elevate: Failed to open thread token: %v", err)
		return err
	}

	logger.Info("Elevate: Checking if already running as SYSTEM")
	tokenUser, err := threadToken.GetTokenUser()
	if err == nil && tokenUser.User.Sid.IsWellKnown(windows.WinLocalSystemSid) {
		logger.Info("Elevate: Already running as SYSTEM, executing function")
		threadToken.Close()
		return f()
	}
	logger.Info("Elevate: Not running as SYSTEM, attempting to adjust privileges")

	err = windows.AdjustTokenPrivileges(threadToken, false, &privileges, uint32(unsafe.Sizeof(privileges)), nil, nil)
	threadToken.Close()
	if err != nil {
		logger.Error("Elevate: Failed to adjust token privileges: %v", err)
		return err
	}
	logger.Info("Elevate: Privileges adjusted, searching for winlogon.exe")

	logger.Info("Elevate: Creating process snapshot")
	processes, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		logger.Error("Elevate: Failed to create process snapshot: %v", err)
		return err
	}
	defer windows.CloseHandle(processes)

	processEntry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	var impersonationError error
	winlogonFound := false

	logger.Info("Elevate: Enumerating processes to find winlogon.exe")
	for err = windows.Process32First(processes, &processEntry); err == nil; err = windows.Process32Next(processes, &processEntry) {
		exeName := strings.ToLower(windows.UTF16ToString(processEntry.ExeFile[:]))
		if exeName != "winlogon.exe" {
			continue
		}
		winlogonFound = true
		logger.Info("Elevate: Found winlogon.exe (PID: %d)", processEntry.ProcessID)

		winlogonProcess, err := windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION, false, processEntry.ProcessID)
		if err != nil {
			logger.Error("Elevate: Failed to open winlogon process: %v", err)
			impersonationError = err
			continue
		}

		logger.Info("Elevate: Opening winlogon process token")
		var winlogonToken windows.Token
		err = windows.OpenProcessToken(winlogonProcess, windows.TOKEN_QUERY|windows.TOKEN_IMPERSONATE|windows.TOKEN_DUPLICATE, &winlogonToken)
		windows.CloseHandle(winlogonProcess)
		if err != nil {
			logger.Error("Elevate: Failed to open winlogon token: %v", err)
			continue
		}

		logger.Info("Elevate: Verifying winlogon token is SYSTEM")
		tokenUser, err := winlogonToken.GetTokenUser()
		if err != nil || !tokenUser.User.Sid.IsWellKnown(windows.WinLocalSystemSid) {
			logger.Error("Elevate: Winlogon token is not SYSTEM")
			winlogonToken.Close()
			continue
		}
		logger.Info("Elevate: Winlogon token verified as SYSTEM")

		logger.Info("Elevate: Duplicating token for impersonation")
		var duplicatedToken windows.Token
		err = windows.DuplicateTokenEx(winlogonToken, 0, nil, windows.SecurityImpersonation, windows.TokenImpersonation, &duplicatedToken)
		winlogonToken.Close()
		if err != nil {
			logger.Error("Elevate: Failed to duplicate token: %v", err)
			return err
		}
		logger.Info("Elevate: Token duplicated successfully")

		logger.Info("Elevate: Getting environment from duplicated token")
		newEnv, err := duplicatedToken.Environ(false)
		if err != nil {
			logger.Error("Elevate: Failed to get environment: %v", err)
			duplicatedToken.Close()
			return err
		}
		currentEnv := os.Environ()

		logger.Info("Elevate: Setting thread token to SYSTEM")
		err = windows.SetThreadToken(nil, duplicatedToken)
		duplicatedToken.Close()
		if err != nil {
			logger.Error("Elevate: Failed to set thread token: %v", err)
			return err
		}
		logger.Info("Elevate: ✓ Successfully elevated to SYSTEM, executing function")

		setAllEnv(newEnv)
		err = f()
		setAllEnv(currentEnv)

		if err != nil {
			logger.Error("Elevate: Function execution failed: %v", err)
		} else {
			logger.Info("Elevate: Function executed successfully")
		}
		return err
	}

	if !winlogonFound {
		logger.Error("Elevate: winlogon.exe not found in process list")
		return errors.New("unable to find winlogon.exe process")
	}

	if impersonationError != nil {
		logger.Error("Elevate: Impersonation error: %v", impersonationError)
		return impersonationError
	}
	logger.Error("Elevate: Failed to impersonate winlogon (unknown error)")
	return errors.New("unable to find winlogon.exe process")
}

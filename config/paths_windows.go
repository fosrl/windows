package config

import (
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// UserDataDir is the per-user directory for config, accounts and caches:
// %LOCALAPPDATA%\Pangolin, or %APPDATA%\Pangolin as a fallback.
func UserDataDir() string {
	appData := os.Getenv("LOCALAPPDATA")
	if appData == "" {
		appData = os.Getenv("APPDATA")
	}
	return filepath.Join(appData, AppName)
}

// GetProgramDataDir returns the base ProgramData directory for the application
// The installer should create this directory and place application files here
func GetProgramDataDir() string {
	return filepath.Join(os.Getenv("PROGRAMDATA"), AppName)
}

// GetLogDir returns the directory path for log files
func GetLogDir() string {
	return filepath.Join(GetProgramDataDir(), "logs")
}

// DaemonLogDir is where the manager service and tunnels log. On Windows the
// UI logs there too.
func DaemonLogDir() string {
	return GetLogDir()
}

// systemConfigPath is the machine-wide config file, %ProgramData%\Pangolin\pangolin.json.
func systemConfigPath() string {
	return filepath.Join(GetProgramDataDir(), ConfigFileName)
}

// isLaptop attempts to determine if the system is a laptop by checking for battery presence
func isLaptop() bool {
	// Use GetSystemPowerStatus to check if battery is present
	// This is a simple heuristic: laptops typically have batteries
	var status struct {
		ACLineStatus        byte
		BatteryFlag         byte
		BatteryLifePercent  byte
		Reserved1           byte
		BatteryLifeTime     uint32
		BatteryFullLifeTime uint32
	}

	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	getSystemPowerStatus := kernel32.NewProc("GetSystemPowerStatus")

	ret, _, _ := getSystemPowerStatus.Call(uintptr(unsafe.Pointer(&status)))
	if ret == 0 {
		// If we can't get power status, default to desktop (more common)
		return false
	}

	// BatteryFlag values:
	// 1 = High (more than 66%)
	// 2 = Low (less than 33%)
	// 4 = Critical (less than 5%)
	// 8 = Charging
	// 128 = No battery
	// 255 = Unknown status

	// If BatteryFlag is 128 (no battery) or 255 (unknown), assume desktop
	if status.BatteryFlag == 128 || status.BatteryFlag == 255 {
		return false
	}

	// If we have a valid battery flag, it's likely a laptop
	return status.BatteryFlag != 0
}

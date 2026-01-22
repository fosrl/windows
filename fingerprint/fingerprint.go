//go:build windows

package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/fosrl/newt/logger"
	"golang.org/x/sys/windows/registry"
)

type Fingerprint struct {
	Username            string `json:"username"`
	Hostname            string `json:"hostname"`
	Platform            string `json:"platform"`
	OSVersion           string `json:"osVersion"`
	KernelVersion       string `json:"kernelVersion"`
	Architecture        string `json:"arch"`
	DeviceModel         string `json:"deviceModel"`
	SerialNumber        string `json:"serialNumber"`
	PlatformFingerprint string `json:"platformFingerprint"`
}

type PostureChecks struct {
	// Platform-agnostic checks

	DiskEncrypted   bool `json:"diskEncrypted"`
	FirewallEnabled bool `json:"firewallEnabled"`
	TpmAvailable    bool `json:"tpmAvailable"`

	// Windows-specific posture check information

	WindowsAntivirusEnabled bool `json:"windowsAntivirusEnabled"`
}

func GatherFingerprintInfo() *Fingerprint {
	var username string
	if u, err := user.Current(); err == nil {
		username = u.Username
	}

	hostname, _ := os.Hostname()

	osVersion, kernelVersion := getWindowsVersion()

	deviceModel, serialNumber := getWindowsModelAndSerial()

	return &Fingerprint{
		Username:            username,
		Hostname:            hostname,
		Platform:            "windows",
		OSVersion:           osVersion,
		KernelVersion:       kernelVersion,
		Architecture:        runtime.GOARCH,
		DeviceModel:         deviceModel,
		SerialNumber:        serialNumber,
		PlatformFingerprint: computePlatformFingerprint(),
	}
}

func GatherPostureChecks() *PostureChecks {
	var wg sync.WaitGroup

	var diskEncrypted, firewall, tpm, defender bool

	wg.Go(func() {
		diskEncrypted = windowsDiskEncrypted()
	})

	wg.Go(func() {
		firewall = windowsFirewallEnabled()
	})

	wg.Go(func() {
		tpm = windowsTPMAvailable()
	})

	wg.Go(func() {
		defender = windowsAntivirusEnabled()
	})

	wg.Wait()

	return &PostureChecks{
		DiskEncrypted:           diskEncrypted,
		FirewallEnabled:         firewall,
		TpmAvailable:            tpm,
		WindowsAntivirusEnabled: defender,
	}
}

type rtlOsVersionInfoEx struct {
	dwOSVersionInfoSize uint32
	dwMajorVersion      uint32
	dwMinorVersion      uint32
	dwBuildNumber       uint32
	dwPlatformId        uint32
	szCSDVersion        [128]uint16
}

func getWindowsVersion() (string, string) {
	ntdll := syscall.NewLazyDLL("ntdll.dll")
	proc := ntdll.NewProc("RtlGetVersion")

	var info rtlOsVersionInfoEx
	info.dwOSVersionInfoSize = uint32(unsafe.Sizeof(info))

	_, _, _ = proc.Call(uintptr(unsafe.Pointer(&info)))

	osVersion := strings.TrimSpace(
		strings.Join([]string{
			"Windows",
			strconv.FormatUint(uint64(info.dwMajorVersion), 10),
			strconv.FormatUint(uint64(info.dwMinorVersion), 10),
			"Build",
			strconv.FormatUint(uint64(info.dwBuildNumber), 10),
		}, " "),
	)

	return osVersion, osVersion
}

func getWindowsModelAndSerial() (string, string) {
	k, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Control\SystemInformation`,
		registry.QUERY_VALUE,
	)
	if err != nil {
		return "", ""
	}
	defer k.Close()

	model, _, _ := k.GetStringValue("SystemProductName")
	serial, _, _ := k.GetStringValue("BIOSSerialNumber")

	return model, serial
}

func windowsDiskEncrypted() bool {
	command := "Get-BitLockerVolume -MountPoint 'C:' | Select-Object -ExpandProperty VolumeStatus"
	logger.Debug("Posture check: Disk Encryption - Executing PowerShell command: %s", command)

	cmd := exec.Command("powershell.exe", "-Command", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()

	if err != nil {
		logger.Debug("Posture check: Disk Encryption - Command failed with error: %v", err)
		return false
	}

	rawOutput := string(out)
	logger.Debug("Posture check: Disk Encryption - Raw command output: %q", rawOutput)

	s := strings.TrimSpace(rawOutput)
	logger.Debug("Posture check: Disk Encryption - Trimmed output: %q", s)

	result := s == "FullyEncrypted" || s == "EncryptionInProgress"
	logger.Debug("Posture check: Disk Encryption - Result: %v (status: %q)", result, s)
	return result
}

func windowsFirewallEnabled() bool {
	command := "(Get-NetFirewallProfile | Where-Object { $_.Enabled -eq $true }).Count -gt 0"
	logger.Debug("Posture check: Firewall - Executing PowerShell command: %s", command)

	cmd := exec.Command("powershell.exe", "-Command", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()

	if err != nil {
		logger.Debug("Posture check: Firewall - Command failed with error: %v", err)
		return false
	}

	rawOutput := string(out)
	logger.Debug("Posture check: Firewall - Raw command output: %q", rawOutput)

	s := strings.TrimSpace(rawOutput)
	logger.Debug("Posture check: Firewall - Trimmed output: %q", s)

	result := s == "True"
	logger.Debug("Posture check: Firewall - Result: %v", result)
	return result
}

func windowsTPMAvailable() bool {
	command := "Get-Tpm | Select-Object -ExpandProperty TpmPresent"
	logger.Debug("Posture check: TPM - Executing PowerShell command: %s", command)

	cmd := exec.Command("powershell.exe", "-Command", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()

	if err != nil {
		logger.Debug("Posture check: TPM - Command failed with error: %v", err)
		return false
	}

	rawOutput := string(out)
	logger.Debug("Posture check: TPM - Raw command output: %q", rawOutput)

	s := strings.TrimSpace(rawOutput)
	logger.Debug("Posture check: TPM - Trimmed output: %q", s)

	// If output is empty, assume no TPM
	if s == "" {
		logger.Debug("Posture check: TPM - Empty output, assuming no TPM")
		return false
	}

	result := s == "True"
	logger.Debug("Posture check: TPM - Result: %v", result)
	return result
}

func windowsAntivirusEnabled() bool {
	// Query Windows Security Center for antivirus products
	// Get productState values and check if any antivirus is active
	command := "Get-CimInstance -Namespace 'root/SecurityCenter2' -ClassName AntiVirusProduct | Select-Object -ExpandProperty productState"
	logger.Debug("Posture check: Antivirus - Executing PowerShell command: %s", command)

	cmd := exec.Command("powershell.exe", "-Command", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		logger.Debug("Posture check: Antivirus - Command failed with error: %v", err)
		return false
	}

	rawOutput := string(out)
	logger.Debug("Posture check: Antivirus - Raw command output: %q", rawOutput)

	// Parse output - may contain multiple productState values (one per line)
	lines := strings.Split(strings.TrimSpace(rawOutput), "\n")
	logger.Debug("Posture check: Antivirus - Found %d productState line(s)", len(lines))

	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			logger.Debug("Posture check: Antivirus - Skipping empty line %d", i+1)
			continue
		}

		logger.Debug("Posture check: Antivirus - Processing line %d: %q", i+1, line)

		// Parse productState as integer
		productState, err := strconv.ParseUint(line, 10, 32)
		if err != nil {
			logger.Debug("Posture check: Antivirus - Failed to parse productState %q as decimal: %v", line, err)
			continue
		}

		logger.Debug("Posture check: Antivirus - Parsed productState (decimal): %d", productState)

		// Convert decimal to hex (e.g., 397568 -> "61100", 401664 -> "62100")
		hexStr := strconv.FormatUint(productState, 16)
		logger.Debug("Posture check: Antivirus - Hex conversion (before padding): %q", hexStr)

		// Pad with leading zeros to ensure 6 digits
		// This ensures the 2nd and 3rd hex digits are always at positions 2-3
		if len(hexStr) < 6 {
			hexStr = strings.Repeat("0", 6-len(hexStr)) + hexStr
		}
		hexStr = strings.ToUpper(hexStr)
		logger.Debug("Posture check: Antivirus - Hex string (after padding/uppercase): %q", hexStr)

		if len(hexStr) < 4 {
			logger.Debug("Posture check: Antivirus - Hex string too short, skipping")
			continue
		}

		// Extract 2nd and 3rd hex digits of the original value
		// After padding to 6 digits, the 2nd and 3rd digits are at positions 2-3 (0-indexed)
		// Example: 397568 -> "61100" -> padded to "061100" -> positions 2-3 = "11"
		// Example: 401664 -> "62100" -> padded to "062100" -> positions 2-3 = "21"
		statusDigits := hexStr[2:4]
		logger.Debug("Posture check: Antivirus - Status digits (2nd and 3rd hex): %q (from hex: %q)", statusDigits, hexStr)

		// "10" or "11" = ACTIVE, "20" or "21" = INACTIVE/PASSIVE
		if statusDigits == "10" || statusDigits == "11" {
			logger.Debug("Posture check: Antivirus - Found ACTIVE antivirus (status: %q)", statusDigits)
			return true
		} else {
			logger.Debug("Posture check: Antivirus - Antivirus is INACTIVE/PASSIVE (status: %q)", statusDigits)
		}
	}

	logger.Debug("Posture check: Antivirus - No active antivirus found, result: false")
	return false
}

func (p *Fingerprint) ToMap() map[string]any {
	b, err := json.Marshal(p)
	if err != nil {
		return map[string]any{}
	}

	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return map[string]any{}
	}

	return m
}

func (p *PostureChecks) ToMap() map[string]any {
	b, err := json.Marshal(p)
	if err != nil {
		return map[string]any{}
	}

	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return map[string]any{}
	}

	return m
}

func computePlatformFingerprint() string {
	parts := []string{
		runtime.GOOS,
		runtime.GOARCH,
		cpuFingerprint(),
		dmiFingerprint(),
	}

	fmt.Println("parts")

	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}

	raw := strings.Join(out, "|")
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

func cpuFingerprint() string {
	k, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`HARDWARE\DESCRIPTION\System\CentralProcessor\0`,
		registry.QUERY_VALUE,
	)
	if err != nil {
		return ""
	}
	defer k.Close()

	var parts []string

	if v, _, err := k.GetStringValue("VendorIdentifier"); err == nil {
		parts = append(parts, "vendor="+normalize(v))
	}
	if v, _, err := k.GetStringValue("ProcessorNameString"); err == nil {
		parts = append(parts, "model_name="+normalize(v))
	}
	if v, _, err := k.GetStringValue("Identifier"); err == nil {
		parts = append(parts, "identifier="+normalize(v))
	}

	return strings.Join(parts, "|")
}

func dmiFingerprint() string {
	k, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Control\SystemInformation`,
		registry.QUERY_VALUE,
	)
	if err != nil {
		return ""
	}
	defer k.Close()

	var parts []string

	read := func(name, key string) {
		if v, _, err := k.GetStringValue(name); err == nil && v != "" {
			parts = append(parts, key+"="+normalize(v))
		}
	}

	read("SystemManufacturer", "sys_vendor")
	read("SystemProductName", "product_name")
	read("SystemSKU", "sku")
	read("BaseBoardManufacturer", "board_vendor")
	read("BaseBoardProduct", "board_name")

	return strings.Join(parts, "|")
}

func normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.Join(strings.Fields(s), " ")
}

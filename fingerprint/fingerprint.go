//go:build windows

package fingerprint

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/fosrl/newt/logger"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const powerShellTimeout = 60 * time.Second
const powerShellRetryTimeout = 20 * time.Second
const systemQueryMaxAttempts = 2
const systemQueryRetryBackoff = 750 * time.Millisecond

// One PowerShell process gathers all WMI-dependent fingerprint and posture data.
// $serial is captured synchronously first so a slow/hanging BitLocker, TPM,
// firewall, or antivirus query can never prevent it from being emitted: the
// four checks below run concurrently on in-process runspaces (no extra
// powershell.exe processes) bounded by a single shared deadline, so a hang in
// any one of them degrades that check to its default value instead of taking
// down the whole script and losing the serial number.
const windowsSystemQueryScript = `
$ErrorActionPreference = 'SilentlyContinue'

$serial = (Get-CimInstance Win32_ComputerSystemProduct | Select-Object -ExpandProperty IdentifyingNumber)

function Start-Async {
    param([scriptblock]$ScriptBlock)
    $ps = [powershell]::Create()
    [void]$ps.AddScript($ScriptBlock.ToString())
    [PSCustomObject]@{ PS = $ps; Handle = $ps.BeginInvoke() }
}

$tasks = @{
    diskEncrypted = Start-Async {
        $ErrorActionPreference = 'SilentlyContinue'
        $status = Get-BitLockerVolume -MountPoint 'C:' | Select-Object -ExpandProperty VolumeStatus
        ($status -eq 'FullyEncrypted' -or $status -eq 'EncryptionInProgress')
    }
    firewallEnabled = Start-Async {
        $ErrorActionPreference = 'SilentlyContinue'
        @((Get-NetFirewallProfile | Where-Object { $_.Enabled -eq $true })).Count -gt 0
    }
    tpmAvailable = Start-Async {
        $ErrorActionPreference = 'SilentlyContinue'
        $tpm = Get-Tpm
        if ($null -ne $tpm) { [bool]$tpm.TpmPresent } else { $false }
    }
    antivirusProductStates = Start-Async {
        $ErrorActionPreference = 'SilentlyContinue'
        @(Get-CimInstance -Namespace 'root/SecurityCenter2' -ClassName AntiVirusProduct |
            ForEach-Object { [uint32]$_.productState })
    }
}

$deadline = (Get-Date).AddSeconds(20)
$diskEncrypted = $false
$firewallEnabled = $false
$tpmAvailable = $false
$antivirusProductStates = @()

foreach ($key in $tasks.Keys) {
    $task = $tasks[$key]
    $remainingMs = [Math]::Max(0, [int](($deadline - (Get-Date)).TotalMilliseconds))
    if ($task.Handle.AsyncWaitHandle.WaitOne($remainingMs)) {
        try {
            $result = $task.PS.EndInvoke($task.Handle)
            switch ($key) {
                'diskEncrypted' { $diskEncrypted = [bool]$result }
                'firewallEnabled' { $firewallEnabled = [bool]$result }
                'tpmAvailable' { $tpmAvailable = [bool]$result }
                'antivirusProductStates' { $antivirusProductStates = @($result) }
            }
        } catch {}
        $task.PS.Dispose()
    }
    # If the task didn't complete in time, its runspace is left running and is
    # abandoned rather than blocked on here; the short-lived host process
    # exits (or is killed by the caller's timeout) shortly after this script
    # returns, which reclaims it.
}

[PSCustomObject]@{
  serialNumber = $serial
  diskEncrypted = [bool]$diskEncrypted
  firewallEnabled = [bool]$firewallEnabled
  tpmAvailable = [bool]$tpmAvailable
  antivirusProductStates = $antivirusProductStates
} | ConvertTo-Json -Compress
`

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

type windowsSystemQueryResult struct {
	SerialNumber           string   `json:"serialNumber"`
	DiskEncrypted          bool     `json:"diskEncrypted"`
	FirewallEnabled        bool     `json:"firewallEnabled"`
	TpmAvailable           bool     `json:"tpmAvailable"`
	AntivirusProductStates []uint32 `json:"antivirusProductStates"`
}

func GatherFingerprintInfo() *Fingerprint {
	fp, _ := gatherDevicePosture()
	return fp
}

func GatherPostureChecks() *PostureChecks {
	_, postures := gatherDevicePosture()
	return postures
}

func gatherDevicePosture() (*Fingerprint, *PostureChecks) {
	logger.Debug("Fingerprint: gatherDevicePosture() starting")

	var username string
	if u, err := user.Current(); err == nil {
		username = u.Username
	}

	var (
		hostname      string
		osVersion     string
		kernelVersion string
		deviceModel   string
		sysQuery      windowsSystemQueryResult
		sysQueryOK    bool
		wg            sync.WaitGroup
	)

	wg.Go(func() {
		hostname, _ = os.Hostname()
	})

	wg.Go(func() {
		osVersion, kernelVersion = getWindowsVersion()
	})

	wg.Go(func() {
		deviceModel = getDeviceModelFromRegistry()
	})

	wg.Go(func() {
		sysQuery, sysQueryOK = gatherWindowsSystemQueries()
	})

	wg.Wait()

	serialNumber := resolveSerialNumber(sysQueryOK, sysQuery.SerialNumber)
	platformFP := computePlatformFingerprint(serialNumber)

	fp := &Fingerprint{
		Username:            username,
		Hostname:            hostname,
		Platform:            "windows",
		OSVersion:           osVersion,
		KernelVersion:       kernelVersion,
		Architecture:        runtime.GOARCH,
		DeviceModel:         deviceModel,
		SerialNumber:        serialNumber,
		PlatformFingerprint: platformFP,
	}

	postures := postureChecksFromSystemQuery(sysQuery, sysQueryOK)

	logger.Debug("Fingerprint: gatherDevicePosture() finished (hostname=%q, model=%q, hasSerial=%v, sysQueryOK=%v)",
		fp.Hostname, fp.DeviceModel, fp.SerialNumber != "", sysQueryOK)
	return fp, postures
}

func postureChecksFromSystemQuery(query windowsSystemQueryResult, ok bool) *PostureChecks {
	if !ok {
		return &PostureChecks{}
	}

	return &PostureChecks{
		DiskEncrypted:           query.DiskEncrypted,
		FirewallEnabled:         query.FirewallEnabled,
		TpmAvailable:            query.TpmAvailable,
		WindowsAntivirusEnabled: antivirusEnabledFromProductStates(query.AntivirusProductStates),
	}
}

// gatherWindowsSystemQueries runs the single-process PowerShell system query,
// retrying the whole invocation once if it fails outright (e.g. a transient
// exec/parse error) rather than giving up on the first hiccup. The retry uses
// a shorter timeout than the initial attempt so a truly wedged PowerShell
// host can't multiply the worst-case blocking time (StartTunnel blocks on
// this synchronously on a cache miss).
func gatherWindowsSystemQueries() (windowsSystemQueryResult, bool) {
	var lastErr error
	for attempt := 1; attempt <= systemQueryMaxAttempts; attempt++ {
		timeout := powerShellTimeout
		if attempt > 1 {
			timeout = powerShellRetryTimeout
		}

		result, err := runSystemQueryOnce(timeout)
		if err == nil {
			return result, true
		}

		lastErr = err
		if attempt < systemQueryMaxAttempts {
			logger.Debug("Fingerprint: system query attempt %d/%d failed, retrying: %v", attempt, systemQueryMaxAttempts, err)
			time.Sleep(systemQueryRetryBackoff)
		}
	}

	logger.Debug("Fingerprint: system query failed after %d attempts: %v", systemQueryMaxAttempts, lastErr)
	return windowsSystemQueryResult{}, false
}

func runSystemQueryOnce(timeout time.Duration) (windowsSystemQueryResult, error) {
	logger.Debug("Fingerprint: gathering WMI posture and serial via single PowerShell invocation")

	out, err := runPowerShellScript(windowsSystemQueryScript, timeout)
	if err != nil {
		return windowsSystemQueryResult{}, fmt.Errorf("system query script failed: %w", err)
	}

	rawOutput := strings.TrimSpace(string(out))
	logger.Debug("Fingerprint: system query raw output: %q", rawOutput)

	var result windowsSystemQueryResult
	if err := json.Unmarshal([]byte(rawOutput), &result); err != nil {
		return windowsSystemQueryResult{}, fmt.Errorf("failed to parse system query JSON: %w", err)
	}

	logger.Debug("Fingerprint: system query parsed (hasSerial=%v, diskEncrypted=%v, firewall=%v, tpm=%v, avStates=%d)",
		strings.TrimSpace(result.SerialNumber) != "",
		result.DiskEncrypted,
		result.FirewallEnabled,
		result.TpmAvailable,
		len(result.AntivirusProductStates),
	)
	return result, nil
}

func runPowerShellScript(script string, timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(
		ctx,
		getPowerShellPath(),
		"-NoProfile",
		"-NonInteractive",
		"-Command",
		script,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Output()
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

	marketingName := "Windows 10"
	if info.dwMajorVersion == 10 && info.dwBuildNumber >= 22000 {
		marketingName = "Windows 11"
	} else if info.dwMajorVersion < 10 {
		marketingName = fmt.Sprintf("Windows %d", info.dwMajorVersion)
	}

	osVersionFull := fmt.Sprintf("%s (%d.%d.%d)",
		marketingName,
		info.dwMajorVersion,
		info.dwMinorVersion,
		info.dwBuildNumber,
	)

	return osVersionFull, osVersionFull
}

func getDeviceModelFromRegistry() string {
	k, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Control\SystemInformation`,
		registry.QUERY_VALUE,
	)
	if err != nil {
		return ""
	}
	defer k.Close()

	model, _, _ := k.GetStringValue("SystemProductName")
	return model
}

func getSerialFromRegistry() string {
	k, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`HARDWARE\DESCRIPTION\System\BIOS`,
		registry.QUERY_VALUE,
	)
	if err != nil {
		return ""
	}
	defer k.Close()

	serial, _, _ := k.GetStringValue("SystemSerialNumber")
	return strings.TrimSpace(serial)
}

// resolveSerialNumber picks the serial number that feeds computePlatformFingerprint.
// A serial that was successfully resolved is cached to disk; if every source
// fails on a given run (e.g. a transient WMI/registry hiccup), the last
// known-good cached serial is reused instead of returning "". That matters
// because computePlatformFingerprint omits the serial component entirely when
// it's empty, which changes the hash's structure - not just its value - and
// would otherwise make a purely transient failure look like a new device.
func resolveSerialNumber(sysQueryOK bool, wmiSerial string) string {
	wmiSerial = strings.TrimSpace(wmiSerial)
	if sysQueryOK && isUsefulSerial(wmiSerial) {
		saveCachedSerial(wmiSerial)
		return wmiSerial
	}

	if regSerial := getSerialFromRegistry(); isUsefulSerial(regSerial) {
		saveCachedSerial(regSerial)
		return regSerial
	}

	if sysQueryOK && wmiSerial != "" {
		saveCachedSerial(wmiSerial)
		return wmiSerial
	}

	if cached := loadCachedSerial(); cached != "" {
		logger.Debug("Fingerprint: all serial sources failed this run, falling back to cached serial")
		return cached
	}

	return ""
}

func isUsefulSerial(serial string) bool {
	serial = strings.TrimSpace(serial)
	if serial == "" {
		return false
	}

	switch strings.ToLower(serial) {
	case "to be filled by o.e.m.",
		"default string",
		"none",
		"not specified",
		"system serial number",
		"0123456789",
		"123456789",
		"00000000":
		return false
	}

	return true
}

// getPowerShellPath returns the full path to PowerShell executable.
// It uses the Windows system directory to construct the path, which ensures
// PowerShell can be found even when it's not in PATH (e.g., in service contexts).
func getPowerShellPath() string {
	systemDir, err := windows.GetSystemDirectory()
	if err != nil {
		logger.Debug("Fingerprint: failed to get system directory, falling back to 'powershell.exe': %v", err)
		return "powershell.exe"
	}
	return filepath.Join(systemDir, "WindowsPowerShell", "v1.0", "powershell.exe")
}

func antivirusEnabledFromProductStates(productStates []uint32) bool {
	logger.Debug("Posture check: Antivirus - evaluating %d productState value(s)", len(productStates))

	for i, productState := range productStates {
		logger.Debug("Posture check: Antivirus - Processing value %d: %d", i+1, productState)

		hexStr := strconv.FormatUint(uint64(productState), 16)
		if len(hexStr) < 6 {
			hexStr = strings.Repeat("0", 6-len(hexStr)) + hexStr
		}
		hexStr = strings.ToUpper(hexStr)

		if len(hexStr) < 4 {
			continue
		}

		statusDigits := hexStr[2:4]
		logger.Debug("Posture check: Antivirus - Status digits (2nd and 3rd hex): %q (from hex: %q)", statusDigits, hexStr)

		if statusDigits == "10" || statusDigits == "11" {
			logger.Debug("Posture check: Antivirus - Found ACTIVE antivirus (status: %q)", statusDigits)
			return true
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

func computePlatformFingerprint(serialNumber string) string {
	parts := []string{
		runtime.GOOS,
		runtime.GOARCH,
		cpuFingerprint(),
		dmiFingerprint(),
	}

	if serialNumber != "" {
		parts = append(parts, "serial="+normalize(serialNumber))
	}

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

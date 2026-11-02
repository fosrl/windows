package fingerprint

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/config"
	"golang.org/x/sys/unix"
)

// PostureChecks are the device posture checks reported on Linux. The field
// names match fosrl/cli's Linux client, which the server already accepts.
type PostureChecks struct {
	// Platform-agnostic checks

	DiskEncrypted   bool `json:"diskEncrypted"`
	FirewallEnabled bool `json:"firewallEnabled"`
	TpmAvailable    bool `json:"tpmAvailable"`

	// Linux-specific posture check information

	LinuxAppArmorEnabled bool `json:"linuxAppArmorEnabled"`
	LinuxSELinuxEnabled  bool `json:"linuxSELinuxEnabled"`
}

const (
	dmiDir         = "/sys/class/dmi/id"
	commandTimeout = 10 * time.Second
)

// gatherDevicePosture collects the fingerprint and posture checks. The daemon
// runs it as root, which the serial number and firewall checks need.
func gatherDevicePosture() (*Fingerprint, *PostureChecks) {
	logger.Debug("Fingerprint: gatherDevicePosture() starting")

	fp := &Fingerprint{
		Platform:     "linux",
		Architecture: runtime.GOARCH,
		OSVersion:    osVersion(),
		DeviceModel:  readTrimmed(filepath.Join(dmiDir, "product_name")),
	}
	if u, err := user.Current(); err == nil {
		fp.Username = u.Username
	}
	fp.Hostname, _ = os.Hostname()
	var uts unix.Utsname
	if unix.Uname(&uts) == nil {
		fp.KernelVersion = unix.ByteSliceToString(uts.Release[:])
	}

	postures := &PostureChecks{}
	var wg sync.WaitGroup
	wg.Go(func() { postures.DiskEncrypted = rootFilesystemEncrypted() })
	wg.Go(func() { postures.FirewallEnabled = firewallEnabled() })
	wg.Go(func() {
		postures.TpmAvailable = fileExists("/dev/tpmrm0") || fileExists("/dev/tpm0")
		postures.LinuxAppArmorEnabled = strings.HasPrefix(
			strings.ToUpper(readTrimmed("/sys/module/apparmor/parameters/enabled")), "Y")
		// The kernel reports enforcing mode here without needing getenforce.
		postures.LinuxSELinuxEnabled = readTrimmed("/sys/fs/selinux/enforce") == "1"
	})
	wg.Wait()

	fp.SerialNumber = resolveSerialNumber()
	fp.PlatformFingerprint = computePlatformFingerprint(fp.SerialNumber)

	logger.Debug("Fingerprint: gatherDevicePosture() finished (hostname=%q, model=%q, hasSerial=%v)",
		fp.Hostname, fp.DeviceModel, fp.SerialNumber != "")
	return fp, postures
}

// resolveSerialNumber reads the DMI serial (root-only) and falls back to the
// last good value, so a failed read doesn't change the platform fingerprint.
func resolveSerialNumber() string {
	serial := readTrimmed(filepath.Join(dmiDir, "product_serial"))
	if isUsefulSerial(serial) {
		saveCachedSerial(serial)
		return serial
	}
	if cached := loadCachedSerial(); cached != "" {
		logger.Debug("Fingerprint: DMI serial unavailable, falling back to cached serial")
		return cached
	}
	return ""
}

// serialCachePath is in the daemon's state directory when running as root,
// otherwise in the user's data directory.
func serialCachePath() string {
	if os.Geteuid() == 0 {
		return filepath.Join(config.GetProgramDataDir(), serialCacheFileName)
	}
	return filepath.Join(config.UserDataDir(), serialCacheFileName)
}

// cpuFingerprint takes stable fields of the first CPU in /proc/cpuinfo, in the
// same form as fosrl/cli's Linux client.
func cpuFingerprint() string {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	defer f.Close()

	allowed := map[string]string{
		"vendor_id":       "vendor",
		"model name":      "model_name",
		"cpu family":      "family",
		"model":           "model",
		"stepping":        "stepping",
		"cpu cores":       "cores",
		"siblings":        "siblings",
		"cpu implementer": "implementer",
		"cpu part":        "part",
		"cpu revision":    "revision",
	}
	values := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		key, val, ok := strings.Cut(scanner.Text(), ":")
		if !ok {
			continue
		}
		if name, ok := allowed[strings.ToLower(strings.TrimSpace(key))]; ok {
			if _, seen := values[name]; !seen {
				if v := normalize(val); v != "" {
					values[name] = v
				}
			}
		}
	}

	var out []string
	for _, k := range []string{"vendor", "model_name", "family", "model", "stepping", "cores", "siblings", "implementer", "part", "revision"} {
		if v, ok := values[k]; ok {
			out = append(out, k+"="+v)
		}
	}
	return strings.Join(out, "|")
}

// dmiFingerprint reads the same DMI fields the Windows client reads from the
// registry, under the same keys.
func dmiFingerprint() string {
	var parts []string
	for _, f := range []struct{ file, key string }{
		{"sys_vendor", "sys_vendor"},
		{"product_name", "product_name"},
		{"product_sku", "sku"},
		{"board_vendor", "board_vendor"},
		{"board_name", "board_name"},
	} {
		if v := readTrimmed(filepath.Join(dmiDir, f.file)); v != "" {
			parts = append(parts, f.key+"="+normalize(v))
		}
	}
	return strings.Join(parts, "|")
}

// osVersion returns e.g. "Ubuntu 24.04.1 LTS (Noble Numbat)" or "Arch Linux".
func osVersion() string {
	release := parseOSRelease()
	if release["NAME"] != "" && release["VERSION"] != "" {
		return release["NAME"] + " " + release["VERSION"]
	}
	if release["PRETTY_NAME"] != "" {
		return release["PRETTY_NAME"]
	}
	return release["NAME"]
}

func parseOSRelease() map[string]string {
	values := make(map[string]string)
	f, err := os.Open("/etc/os-release")
	if err != nil {
		if f, err = os.Open("/usr/lib/os-release"); err != nil {
			return values
		}
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if key, val, ok := strings.Cut(line, "="); ok {
			values[key] = strings.Trim(val, `"'`)
		}
	}
	return values
}

// rootFilesystemEncrypted reports whether / sits on a dm-crypt (LUKS) device,
// following device-mapper and LVM layers down through /sys/class/block.
func rootFilesystemEncrypted() bool {
	var st unix.Stat_t
	if err := unix.Stat("/", &st); err != nil {
		return false
	}
	dev := blockDeviceName(unix.Major(st.Dev), unix.Minor(st.Dev))
	if dev == "" {
		// Btrfs and other filesystems report a virtual device; fall back to
		// the source in the mount table.
		dev = rootMountSourceDevice()
	}
	return dev != "" && blockDeviceEncrypted(dev, 0)
}

func blockDeviceName(major, minor uint32) string {
	link, err := os.Readlink("/sys/dev/block/" + strconv.FormatUint(uint64(major), 10) + ":" + strconv.FormatUint(uint64(minor), 10))
	if err != nil {
		return ""
	}
	return filepath.Base(link)
}

func rootMountSourceDevice() string {
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		// Fields: id parent major:minor root mountpoint ... - fstype source options
		pre, post, ok := strings.Cut(scanner.Text(), " - ")
		fields := strings.Fields(pre)
		if !ok || len(fields) < 5 || fields[4] != "/" {
			continue
		}
		after := strings.Fields(post)
		if len(after) < 2 {
			return ""
		}
		resolved, err := filepath.EvalSymlinks(after[1])
		if err != nil || !strings.HasPrefix(resolved, "/dev/") {
			return ""
		}
		return filepath.Base(resolved)
	}
	return ""
}

func blockDeviceEncrypted(dev string, depth int) bool {
	if depth > 8 {
		return false
	}
	base := filepath.Join("/sys/class/block", dev)
	if strings.HasPrefix(readTrimmed(filepath.Join(base, "dm", "uuid")), "CRYPT-") {
		return true
	}
	// A partition's holder chain starts at its parent disk's entry, but its
	// slaves are listed on the partition itself.
	slaves, _ := os.ReadDir(filepath.Join(base, "slaves"))
	for _, s := range slaves {
		if blockDeviceEncrypted(s.Name(), depth+1) {
			return true
		}
	}
	return false
}

// firewallEnabled uses the same checks as fosrl/cli's Linux client: an active
// ufw or firewalld, or any nftables or iptables rules.
func firewallEnabled() bool {
	if out, ok := runCommand("ufw", "status"); ok && strings.Contains(strings.ToLower(out), "status: active") {
		return true
	}
	if out, ok := runCommand("firewall-cmd", "--state"); ok && strings.TrimSpace(out) == "running" {
		return true
	}
	if out, ok := runCommand("nft", "list", "ruleset"); ok && strings.TrimSpace(out) != "" {
		return true
	}
	if out, ok := runCommand("iptables", "-S"); ok {
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(line, "-A") {
				return true
			}
		}
	}
	return false
}

func runCommand(name string, args ...string) (string, bool) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, args...).Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}

func readTrimmed(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

package managers

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPolkitSubject(t *testing.T) {
	pid := int32(os.Getpid())
	subject := polkitSubject(pid, 1000)
	parts := strings.Split(subject, ",")
	if len(parts) != 3 || parts[0] != fmt.Sprint(pid) || parts[2] != "1000" || parts[1] == "" {
		t.Fatalf("polkitSubject = %q", subject)
	}
	if s := polkitSubject(0, 1000); s != "" {
		t.Fatalf("polkitSubject(0) = %q, want empty", s)
	}
}

func TestCheckELF(t *testing.T) {
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := checkELF(exe); err != nil {
		t.Fatalf("checkELF(test binary) = %v", err)
	}
	page := filepath.Join(dir, "page")
	os.WriteFile(page, []byte("<html>Not Found</html>"), 0o600)
	if err := checkELF(page); err == nil {
		t.Fatal("checkELF accepted an HTML page")
	}
}

// TestInstallCLI downloads the real CLI release, so it only runs with
// PANGOLIN_TEST_CLI_DOWNLOAD=1.
func TestInstallCLI(t *testing.T) {
	if os.Getenv("PANGOLIN_TEST_CLI_DOWNLOAD") != "1" {
		t.Skip("set PANGOLIN_TEST_CLI_DOWNLOAD=1 to download the CLI")
	}
	cliInstallDir = t.TempDir()
	if err := InstallCLI(0); err == nil {
		t.Fatal("InstallCLI(0) succeeded without authorization")
	}
	if err := InstallCLI(1); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(cliInstallDir, cliBinaryName)
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v, want 0755", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(cliInstallDir)
	if len(entries) != 1 {
		t.Fatalf("install dir holds %d files, want only %s", len(entries), cliBinaryName)
	}
}

// TestPolkitAuthorize checks a running process against the CLI install
// action. It needs polkit and the policy installed, so it only runs with
// PANGOLIN_TEST_POLKIT_PID and PANGOLIN_TEST_POLKIT_UID set; the result must
// be nil when PANGOLIN_TEST_POLKIT_WANT is "ok", or else contain it.
func TestPolkitAuthorize(t *testing.T) {
	pidEnv, uidEnv := os.Getenv("PANGOLIN_TEST_POLKIT_PID"), os.Getenv("PANGOLIN_TEST_POLKIT_UID")
	if pidEnv == "" || uidEnv == "" {
		t.Skip("set PANGOLIN_TEST_POLKIT_PID and PANGOLIN_TEST_POLKIT_UID")
	}
	var pid int32
	var uid uint32
	fmt.Sscan(pidEnv, &pid)
	fmt.Sscan(uidEnv, &uid)
	err := polkitAuthorize(cliInstallAction, polkitSubject(pid, uid))
	want := os.Getenv("PANGOLIN_TEST_POLKIT_WANT")
	t.Logf("polkitAuthorize: %v", err)
	if want == "ok" {
		if err != nil {
			t.Fatal(err)
		}
	} else if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("got %v, want an error containing %q", err, want)
	}
}

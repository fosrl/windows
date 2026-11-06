package managers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/fosrl/newt/logger"
)

// polkitTimeout bounds how long an authentication dialog can stay open.
const polkitTimeout = 5 * time.Minute

// polkitSubject names a process to polkit as "pid,start-time,uid". The start
// time stops a later process that reuses the PID from passing as this one.
// It returns "" if the process is already gone.
func polkitSubject(pid int32, uid uint32) string {
	if pid <= 0 {
		return ""
	}
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return ""
	}
	// The command name, in parentheses, may contain spaces; the fields after
	// it start with the state, which is field 3. The start time is field 22.
	end := bytes.LastIndexByte(stat, ')')
	if end < 0 {
		return ""
	}
	fields := strings.Fields(string(stat[end+1:]))
	if len(fields) < 20 {
		return ""
	}
	if _, err := strconv.ParseUint(fields[19], 10, 64); err != nil {
		return ""
	}
	return fmt.Sprintf("%d,%s,%d", pid, fields[19], uid)
}

// polkitAuthorize asks polkit whether subject may perform action, letting it
// show an authentication dialog in the subject's session. It returns nil only
// when the action is authorized.
func polkitAuthorize(action, subject string) error {
	pkcheck, err := exec.LookPath("pkcheck")
	if err != nil {
		return errors.New("this needs an administrator: a member of the sudo or wheel group (polkit isn't installed to ask for a password)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), polkitTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, pkcheck,
		"--action-id", action, "--process", subject, "--allow-user-interaction").CombinedOutput()
	if err == nil {
		return nil
	}
	detail := strings.TrimSpace(string(out))
	logger.Info("polkit refused %s for %s: %v %s", action, subject, err, detail)
	if ctx.Err() != nil {
		return errors.New("timed out waiting for authentication")
	}
	if strings.Contains(detail, "is not registered") {
		return fmt.Errorf("polkit doesn't know the %s action; reinstall Pangolin", action)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		switch exitErr.ExitCode() {
		case 1:
			return errors.New("not authorized: an administrator's password is needed")
		case 2:
			return errors.New("an administrator's password is needed, but no polkit authentication agent is running in your session")
		case 3:
			return errors.New("authentication was cancelled")
		}
	}
	return fmt.Errorf("polkit check failed: %v %s", err, detail)
}

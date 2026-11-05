package managers

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows-linux/config"
	"github.com/fosrl/windows-linux/updater"
	"golang.org/x/sys/unix"
)

// The Linux daemon runs as root under systemd. Unlike the Windows service it
// doesn't start the UI: the UI starts in the user's session (app menu or
// autostart) and attaches over a Unix socket. The daemon checks the caller's
// UID with SO_PEERCRED, then hands it the same three pipes the Windows
// service gives the UI process, so the IPC protocol is shared.

// Requests a client sends as the first byte on the socket.
const (
	daemonRequestAttachUI byte = 'U' // reply: attachStatus, plus pipe FDs on success
	daemonRequestUIAction byte = 'A' // followed by a uint32 UIAction; reply: 0 delivered, 1 no UI
	// daemonRequestCLISecrets serves the Pangolin CLI's companion mode. It is
	// followed by a uint32 UID, then the request in cli_secrets.go. The UID
	// must be the caller's own, except that root (the CLI under sudo) may
	// name the user it acts for.
	daemonRequestCLISecrets byte = 'S'
)

// Replies to daemonRequestAttachUI.
const (
	attachOK             byte = 0
	attachAlreadyRunning byte = 1
	attachFailed         byte = 2
)

// daemonSocketPath is where the daemon listens. Any local user may connect;
// requests are checked against the caller's UID.
func daemonSocketPath() string {
	return filepath.Join(config.RuntimeDir(), "manager.sock")
}

type attachedUI struct {
	done chan struct{}
	// subject names the UI process to polkit: "pid,start-time,uid".
	subject string
}

var (
	attachedUIs     = make(map[uint32]*attachedUI)
	attachedUIsLock sync.Mutex
	daemonStopping  bool
)

// Run runs the daemon until SIGTERM or SIGINT.
func Run() error {
	logger.Info("Pangolin daemon starting")
	if os.Geteuid() != 0 {
		return errors.New("the daemon must run as root")
	}

	runtimeDir := config.RuntimeDir()
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", runtimeDir, err)
	}
	if err := os.Chmod(runtimeDir, 0o755); err != nil {
		return err
	}
	socketPath := daemonSocketPath()
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		return fmt.Errorf("listen on %s: %w", socketPath, err)
	}
	listener.SetUnlinkOnClose(true)
	if err := os.Chmod(socketPath, 0o666); err != nil {
		listener.Close()
		return err
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		for {
			conn, err := listener.AcceptUnix()
			if err != nil {
				if !errors.Is(err, net.ErrClosed) {
					logger.Error("Daemon: accept failed: %v", err)
				}
				return
			}
			go handleDaemonConn(conn)
		}
	}()
	logger.Info("Pangolin daemon listening on %s", socketPath)
	go restartUIsAfterUpdate()
	go updater.RemoveDownloads()
	go checkForUpdates()

	sig := <-signals
	logger.Info("Pangolin daemon stopping on %v", sig)
	attachedUIsLock.Lock()
	daemonStopping = true
	attachedUIsLock.Unlock()
	listener.Close()
	IPCServerNotifyManagerStopping()
	stopAllTunnelProcesses()
	logger.Info("Pangolin daemon stopped")
	return nil
}

func handleDaemonConn(conn *net.UnixConn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	uid, pid, err := peerCred(conn)
	if err != nil {
		logger.Error("Daemon: failed to read peer credentials: %v", err)
		return
	}
	var request [1]byte
	if _, err := conn.Read(request[:]); err != nil {
		return
	}
	switch request[0] {
	case daemonRequestAttachUI:
		attachUI(conn, uid, pid)
	case daemonRequestUIAction:
		var action uint32
		if err := binary.Read(conn, binary.LittleEndian, &action); err != nil {
			return
		}
		reply := byte(1)
		if uiAttached(uid) {
			logger.Info("UI action %d requested for uid %d", action, uid)
			IPCServerNotifyUIAction(uid, UIAction(action))
			reply = 0
		}
		_, _ = conn.Write([]byte{reply})
	case daemonRequestCLISecrets:
		var forUID uint32
		if err := binary.Read(conn, binary.LittleEndian, &forUID); err != nil {
			return
		}
		if uid != 0 && forUID != uid {
			logger.Error("Daemon: uid %d asked for the CLI secrets of uid %d", uid, forUID)
			writeCLISecretsResponse(conn, cliSecretsStatusError, []byte("permission denied"))
			return
		}
		serveCLISecrets(conn, strconv.FormatUint(uint64(forUID), 10))
	default:
		logger.Error("Daemon: unknown request %q from uid %d", request[0], uid)
	}
}

// peerCred returns the UID and PID of the process on the other end of conn.
func peerCred(conn *net.UnixConn) (uid uint32, pid int32, err error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, 0, err
	}
	var cred *unix.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return 0, 0, err
	}
	if credErr != nil {
		return 0, 0, credErr
	}
	return cred.Uid, cred.Pid, nil
}

func uiAttached(uid uint32) bool {
	attachedUIsLock.Lock()
	defer attachedUIsLock.Unlock()
	_, ok := attachedUIs[uid]
	return ok
}

// attachUI connects a UI process to the IPC server. Each user gets one UI;
// a second one is told the first is running.
func attachUI(conn *net.UnixConn, uid uint32, pid int32) {
	attachedUIsLock.Lock()
	if daemonStopping {
		attachedUIsLock.Unlock()
		_, _ = conn.Write([]byte{attachFailed})
		return
	}
	if _, ok := attachedUIs[uid]; ok {
		attachedUIsLock.Unlock()
		_, _ = conn.Write([]byte{attachAlreadyRunning})
		return
	}
	ui := &attachedUI{done: make(chan struct{}), subject: polkitSubject(pid, uid)}
	attachedUIs[uid] = ui
	attachedUIsLock.Unlock()

	detach := func() {
		attachedUIsLock.Lock()
		if attachedUIs[uid] == ui {
			delete(attachedUIs, uid)
		}
		attachedUIsLock.Unlock()
		close(ui.done)
	}

	pipes, err := newIPCPipes()
	if err != nil {
		logger.Error("Daemon: failed to create IPC pipes for uid %d: %v", uid, err)
		_, _ = conn.Write([]byte{attachFailed})
		detach()
		return
	}
	rights := unix.UnixRights(int(pipes.theirReader.Fd()), int(pipes.theirWriter.Fd()), int(pipes.theirEvents.Fd()))
	_, _, err = conn.WriteMsgUnix([]byte{attachOK}, rights, nil)
	pipes.closeTheirs()
	if err != nil {
		logger.Error("Daemon: failed to pass IPC pipes to uid %d: %v", uid, err)
		pipes.closeOurs()
		detach()
		return
	}

	key := strconv.FormatUint(uint64(uid), 10)
	logger.Info("UI attached for uid %d", uid)
	done := IPCServerListen(pipes.ourReader, pipes.ourWriter, pipes.ourEvents, tokenForUID(uid), key)
	go func() {
		<-done
		pipes.closeOurs()
		detach()
		logger.Info("UI detached for uid %d", uid)
		relaunchUIIfAlwaysOn(uid, key)
	}()
}

type ipcPipes struct {
	ourReader, ourWriter, ourEvents       *os.File
	theirReader, theirWriter, theirEvents *os.File
}

func newIPCPipes() (*ipcPipes, error) {
	p := &ipcPipes{}
	var err error
	if p.ourReader, p.theirWriter, err = os.Pipe(); err != nil {
		return nil, err
	}
	if p.theirReader, p.ourWriter, err = os.Pipe(); err != nil {
		p.closeOurs()
		p.closeTheirs()
		return nil, err
	}
	if p.theirEvents, p.ourEvents, err = os.Pipe(); err != nil {
		p.closeOurs()
		p.closeTheirs()
		return nil, err
	}
	return p, nil
}

func closeFiles(files ...*os.File) {
	for _, f := range files {
		if f != nil {
			f.Close()
		}
	}
}

func (p *ipcPipes) closeOurs()   { closeFiles(p.ourReader, p.ourWriter, p.ourEvents) }
func (p *ipcPipes) closeTheirs() { closeFiles(p.theirReader, p.theirWriter, p.theirEvents) }

// Always-On keeps the UI running, as on Windows: if the UI goes away while
// Always-On is set (Quit clears it first), it is started again in the
// user's session. A run of quick failures gives up and clears Always-On.
const (
	alwaysOnRelaunchDelay   = time.Second
	alwaysOnRelaunchWindow  = time.Minute
	alwaysOnRelaunchMaxRuns = 5
)

var (
	relaunchTimes     = make(map[uint32][]time.Time)
	relaunchTimesLock sync.Mutex
)

func relaunchUIIfAlwaysOn(uid uint32, key string) {
	attachedUIsLock.Lock()
	stopping := daemonStopping
	attachedUIsLock.Unlock()
	if stopping || !userAlwaysOn(key) {
		return
	}

	relaunchTimesLock.Lock()
	now := time.Now()
	recent := relaunchTimes[uid][:0]
	for _, t := range relaunchTimes[uid] {
		if now.Sub(t) < alwaysOnRelaunchWindow {
			recent = append(recent, t)
		}
	}
	tooMany := len(recent) >= alwaysOnRelaunchMaxRuns
	if !tooMany {
		recent = append(recent, now)
	}
	relaunchTimes[uid] = recent
	relaunchTimesLock.Unlock()
	if tooMany {
		logger.Error("Always-On: UI for uid %d keeps exiting, giving up", uid)
		setUserAlwaysOn(key, false)
		return
	}

	time.Sleep(alwaysOnRelaunchDelay)
	if uiAttached(uid) {
		return
	}
	if err := launchUIForUser(uid); err != nil {
		logger.Error("Always-On: failed to restart the UI for uid %d: %v", uid, err)
		setUserAlwaysOn(key, false)
		return
	}
	logger.Info("Always-On: restarted the UI for uid %d", uid)
}

// launchUIForUser starts the UI as a transient unit in the user's systemd
// instance, which carries their graphical session's environment.
func launchUIForUser(uid uint32) error {
	u, err := user.LookupId(strconv.FormatUint(uint64(uid), 10))
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	out, err := exec.Command("systemd-run", "--user", "--machine="+u.Username+"@.host",
		"--collect", "--quiet", "--unit=pangolin-client-ui-"+strconv.FormatInt(time.Now().Unix(), 10),
		exe).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, out)
	}
	return nil
}

// prepareUpdateRestart records whose UI is open before an update, since the
// package restarts the daemon, which closes them.
func prepareUpdateRestart() {
	attachedUIsLock.Lock()
	uids := make([]string, 0, len(attachedUIs))
	for uid := range attachedUIs {
		uids = append(uids, strconv.FormatUint(uint64(uid), 10))
	}
	attachedUIsLock.Unlock()
	if err := os.MkdirAll(filepath.Dir(updater.RestartUIFile), 0o700); err != nil {
		logger.Error("Updater: %v", err)
		return
	}
	if err := os.WriteFile(updater.RestartUIFile, []byte(strings.Join(uids, "\n")), 0o600); err != nil {
		logger.Error("Updater: failed to record open UIs: %v", err)
	}
}

// restartUIsAfterUpdate reopens the UIs that were open when an update
// started, once the updated daemon is running.
func restartUIsAfterUpdate() {
	data, err := os.ReadFile(updater.RestartUIFile)
	if err != nil {
		return
	}
	os.Remove(updater.RestartUIFile)
	for _, field := range strings.Fields(string(data)) {
		uid, err := strconv.ParseUint(field, 10, 32)
		if err != nil || uiAttached(uint32(uid)) {
			continue
		}
		if err := launchUIForUser(uint32(uid)); err != nil {
			logger.Error("Failed to reopen the UI for uid %d after the update: %v", uid, err)
		} else {
			logger.Info("Reopened the UI for uid %d after the update", uid)
		}
	}
}

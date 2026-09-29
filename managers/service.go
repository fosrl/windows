//go:build windows

package managers

import (
	"encoding/binary"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/Microsoft/go-winio"
	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/config"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
)

type managerService struct{}

func (service *managerService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (svcSpecificEC bool, exitCode uint32) {
	changes <- svc.Status{State: svc.StartPending}

	var err error

	defer func() {
		if err != nil {
			logger.Error("Manager service error: %v", err)
		}
		changes <- svc.Status{State: svc.StopPending}
	}()

	logger.Info("Pangolin Manager service starting")

	// WTSQueryUserToken requires SeTcbPrivilege (act as part of OS); enable it for this process.
	// Without it, WTSQueryUserToken returns error 1314 on some systems even when running as LocalSystem.
	if err := enableSeTcbPrivilege(); err != nil {
		logger.Error("Failed to enable SeTcbPrivilege (WTSQueryUserToken may fail): %v", err)
	}

	path, err := os.Executable()
	if err != nil {
		logger.Error("Failed to determine executable path: %v", err)
		return false, 1
	}

	procs := make(map[uint32]*uiProcess)
	aliveSessions := make(map[uint32]bool)
	procsLock := sync.Mutex{}
	stoppingManager := false
	// operatorGroupSid, _ := windows.CreateWellKnownSid(windows.WinBuiltinNetworkConfigurationOperatorsSid) // TODO: Use when LimitedOperatorUI is implemented

	startProcess := func(session uint32) {
		defer func() {
			runtime.UnlockOSThread()
			procsLock.Lock()
			delete(aliveSessions, session)
			procsLock.Unlock()
		}()

		logger.Debug("UI launch (service): startProcess called for session %d", session)
		var userToken windows.Token
		err := windows.WTSQueryUserToken(session, &userToken)
		if err != nil {
			logger.Error("UI launch (service): startProcess WTSQueryUserToken(session %d) failed: %v", session, err)
			var errno syscall.Errno
			if errors.As(err, &errno) {
				logger.Error("UI launch (service): Windows error code %d (1314=privilege not held, 1008=token does not exist)", uint32(errno))
			}
			return
		}
		logger.Debug("UI launch (service): startProcess WTSQueryUserToken(session %d) succeeded", session)
		// Check if token is elevated
		isAdmin := userToken.IsElevated()
		// Also check if it can be elevated via UAC
		if !isAdmin {
			// Try to get linked token (UAC elevation token)
			// This works for users in Administrators group
			linkedToken, err := userToken.GetLinkedToken()
			if err == nil {
				isAdmin = linkedToken.IsElevated()
				linkedToken.Close()
			}

			// If still not elevated, check if user is in Administrators group
			// (can be elevated via UAC, even if not currently elevated)
			if !isAdmin {
				adminGroupSid, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
				if err == nil {
					isAdminMember, err := userToken.IsMember(adminGroupSid)
					isAdmin = isAdminMember && err == nil
				}
			}
		}
		// TODO: Implement LimitedOperatorUI support when config management is added
		// isOperator := false
		// if !isAdmin && conf.AdminBool("LimitedOperatorUI") && operatorGroupSid != nil {
		// 	linkedToken, err := userToken.GetLinkedToken()
		// 	var impersonationToken windows.Token
		// 	if err == nil {
		// 		err = windows.DuplicateTokenEx(linkedToken, windows.TOKEN_QUERY, nil, windows.SecurityImpersonation, windows.TokenImpersonation, &impersonationToken)
		// 		linkedToken.Close()
		// 	} else {
		// 		err = windows.DuplicateTokenEx(userToken, windows.TOKEN_QUERY, nil, windows.SecurityImpersonation, windows.TokenImpersonation, &impersonationToken)
		// 	}
		// 	if err == nil {
		// 		isOperator, err = impersonationToken.IsMember(operatorGroupSid)
		// 		isOperator = isOperator && err == nil
		// 		impersonationToken.Close()
		// 	}
		// }
		// Allow all logged-in users to run the UI
		// The manager service is already running (installed/started with elevation),
		// so it can handle privileged operations. The UI runs in user context.
		// Standard users who can elevate via UAC (enter admin password) should be able to use the app.
		// if !isAdmin && !isOperator {
		// 	userToken.Close()
		// 	return
		// }
		user, err := userToken.GetTokenUser()
		if err != nil {
			logger.Error("Unable to lookup user from token: %v", err)
			userToken.Close()
			return
		}
		username, domain, accType, err := user.User.Sid.LookupAccount("")
		if err != nil {
			logger.Error("Unable to lookup username from sid: %v", err)
			userToken.Close()
			return
		}
		if accType != windows.SidTypeUser {
			userToken.Close()
			return
		}
		userProfileDirectory, _ := userToken.GetUserProfileDirectory()
		var elevatedToken, runToken windows.Token
		if isAdmin {
			if userToken.IsElevated() {
				elevatedToken = userToken
				runToken = elevatedToken
			} else {
				// Try to get linked token (UAC elevation token)
				linkedToken, err := userToken.GetLinkedToken()
				if err == nil && linkedToken.IsElevated() {
					elevatedToken = linkedToken
					runToken = elevatedToken
					userToken.Close()
				} else {
					if linkedToken != 0 {
						linkedToken.Close()
					}
					// User is in Administrators group but not currently elevated
					// Allow UI to start with non-elevated token, use zero token for IPC
					// (IPC server can handle zero token for operations that don't require elevation)
					elevatedToken = 0
					runToken = userToken
				}
			}
		} else {
			runToken = userToken
		}
		defer runToken.Close()
		userToken = 0

		// Keep the UI process running while Always-On is enabled for this user.
		// A normal exit stays down so the user can start the app again themselves.
		clientWindowsSID := user.User.Sid.String()
		for {
			procsLock.Lock()
			alive := aliveSessions[session]
			procsLock.Unlock()
			if !alive {
				setUserAlwaysOn(clientWindowsSID, false)
				return
			}
			if stoppingManager {
				return
			}

			ourReader, theirWriter, err := os.Pipe()
			if err != nil {
				logger.Error("Unable to create pipe: %v", err)
				return
			}
			theirReader, ourWriter, err := os.Pipe()
			if err != nil {
				logger.Error("Unable to create pipe: %v", err)
				return
			}
			theirEvents, ourEvents, err := os.Pipe()
			if err != nil {
				logger.Error("Unable to create pipe: %v", err)
				return
			}
			IPCServerListen(ourReader, ourWriter, ourEvents, elevatedToken, clientWindowsSID)

			logger.Info("Starting UI process for user '%s@%s' for session %d", username, domain, session)
			procsLock.Lock()
			var proc *uiProcess
			if alive := aliveSessions[session]; alive {
				proc, err = launchUIProcess(path, []string{
					path,
					"/ui",
					strconv.FormatUint(uint64(theirReader.Fd()), 10),
					strconv.FormatUint(uint64(theirWriter.Fd()), 10),
					strconv.FormatUint(uint64(theirEvents.Fd()), 10),
				}, userProfileDirectory, []windows.Handle{
					windows.Handle(theirReader.Fd()),
					windows.Handle(theirWriter.Fd()),
					windows.Handle(theirEvents.Fd()),
				}, runToken)
			} else {
				err = errors.New("Session has logged out")
			}
			procsLock.Unlock()
			theirReader.Close()
			theirWriter.Close()
			theirEvents.Close()
			if err != nil {
				ourReader.Close()
				ourWriter.Close()
				ourEvents.Close()
				logger.Error("Unable to start manager UI process for user '%s@%s' for session %d: %v", username, domain, session, err)
				procsLock.Lock()
				stillAlive := aliveSessions[session]
				procsLock.Unlock()
				if stoppingManager || !stillAlive || !userAlwaysOn(clientWindowsSID) {
					return
				}
				time.Sleep(time.Second)
				continue
			}

			procsLock.Lock()
			procs[session] = proc
			procsLock.Unlock()

			if exitCode, waitErr := proc.Wait(); waitErr == nil {
				logger.Info("Exited UI process for user '%s@%s' for session %d with status %x", username, domain, session, exitCode)
			} else {
				logger.Error("Unable to wait for UI process for user '%s@%s' for session %d: %v", username, domain, session, waitErr)
			}

			procsLock.Lock()
			delete(procs, session)
			stillAlive := aliveSessions[session]
			procsLock.Unlock()
			ourReader.Close()
			ourWriter.Close()
			ourEvents.Close()

			if stoppingManager || !stillAlive || !userAlwaysOn(clientWindowsSID) {
				if !stillAlive {
					setUserAlwaysOn(clientWindowsSID, false)
				}
				return
			}
			logger.Info("Always-On: restarting UI process for user '%s@%s' for session %d", username, domain, session)
			time.Sleep(time.Second)
		}
	}
	procsGroup := sync.WaitGroup{}
	goStartProcess := func(session uint32) {
		procsGroup.Add(1)
		go func() {
			startProcess(session)
			procsGroup.Done()
		}()
	}

	go checkForUpdates()

	// TODO: Add driver cleanup when driver package is implemented
	// go driver.UninstallLegacyWintun()

	// Do not auto-start UI processes at service start. Starting before the user's
	// shell is ready shows no tray, and then the exe thinks a UI is already running.
	// UI starts when the user runs the exe, after an update, or at session logon
	// when that user has open-at-login or connect-at-start enabled.

	// Listen for UI launch requests from standard users (named pipe).
	requestUILaunchChan := make(chan uint32)
	var pipeListener net.Listener
	var cliSecretsPipeListener net.Listener
	pipeConfig := &winio.PipeConfig{
		SecurityDescriptor: "D:(A;;GA;;;WD)", // Allow Everyone to connect
	}
	listener, listenErr := winio.ListenPipe(uiLaunchPipePath, pipeConfig)
	if listenErr != nil {
		logger.Error("Failed to create UI launch pipe listener: %v", listenErr)
	} else {
		pipeListener = listener
		go runUILaunchPipeListener(listener, requestUILaunchChan, procs, aliveSessions, &procsLock)
	}

	var uiActionPipeListener net.Listener
	if l, err := winio.ListenPipe(uiActionPipePath, pipeConfig); err != nil {
		logger.Error("Failed to create UI action pipe listener: %v", err)
	} else {
		uiActionPipeListener = l
		go runUIActionPipeListener(l, procs, &procsLock)
	}

	cliSecretsListener, cliSecretsErr := winio.ListenPipe(cliSecretsPipePath, pipeConfig)
	if cliSecretsErr != nil {
		logger.Error("Failed to create CLI secrets pipe listener: %v", cliSecretsErr)
	} else {
		cliSecretsPipeListener = cliSecretsListener
		go runCLISecretsPipeListener(cliSecretsListener)
	}

	changes <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptSessionChange}

	// If restart-ui-after-update flag exists (written before MSI run), launch UI for active session then remove flag.
	go func() {
		flagPath := filepath.Join(config.GetProgramDataDir(), "restart-ui-after-update.flag")
		if _, statErr := os.Stat(flagPath); statErr != nil {
			return
		}
		sessionID := windows.WTSGetActiveConsoleSessionId()
		if sessionID == 0 {
			logger.Info("Restart-ui flag present but no active console session, removing flag")
			_ = os.Remove(flagPath)
			return
		}
		procsLock.Lock()
		aliveSessions[sessionID] = true
		procsLock.Unlock()
		requestUILaunchChan <- sessionID
		if err := os.Remove(flagPath); err != nil && !os.IsNotExist(err) {
			logger.Error("Failed to remove restart-ui flag: %v", err)
		} else {
			logger.Info("Launched UI for session %d after update and removed restart-ui flag", sessionID)
		}
	}()

	uninstall := false
loop:
	for {
		select {
		case sessionID := <-requestUILaunchChan:
			procsLock.Lock()
			if _, ok := procs[sessionID]; !ok && aliveSessions[sessionID] {
				goStartProcess(sessionID)
			}
			procsLock.Unlock()
		case <-quitManagersChan:
			uninstall = true
			// Set stoppingManager immediately to prevent startProcess goroutines
			// from restarting UI processes after they exit
			procsLock.Lock()
			stoppingManager = true
			procsLock.Unlock()
			break loop
		case c := <-r:
			switch c.Cmd {
			case svc.Stop:
				break loop
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.SessionChange:
				sessionNotification := (*windows.WTSSESSION_NOTIFICATION)(unsafe.Pointer(c.EventData))
				if uintptr(sessionNotification.Size) != unsafe.Sizeof(*sessionNotification) {
					logger.Error("Unexpected size of WTSSESSION_NOTIFICATION: %d", sessionNotification.Size)
					continue
				}
				switch c.EventType {
				case windows.WTS_SESSION_LOGOFF:
					procsLock.Lock()
					delete(aliveSessions, sessionNotification.SessionID)
					if proc, ok := procs[sessionNotification.SessionID]; ok {
						proc.Kill()
					}
					procsLock.Unlock()
				case windows.WTS_SESSION_LOGON:
					sessionID := sessionNotification.SessionID
					procsLock.Lock()
					alreadyAlive := aliveSessions[sessionID]
					if !alreadyAlive {
						aliveSessions[sessionID] = true
					}
					procsLock.Unlock()
					if alreadyAlive {
						continue
					}
					go func() {
						if !launchUIAtLoginForSession(sessionID) {
							return
						}
						waitForSessionExplorer(sessionID, 60*time.Second)
						procsLock.Lock()
						if !stoppingManager {
							if _, ok := procs[sessionID]; !ok && aliveSessions[sessionID] {
								goStartProcess(sessionID)
							}
						}
						procsLock.Unlock()
					}()
				default:
					// Ignore other session change events
					continue
				}

			default:
				logger.Error("Unexpected service control request #%d", c)
			}
		}
	}

	changes <- svc.Status{State: svc.StopPending}
	procsLock.Lock()
	stoppingManager = true
	IPCServerNotifyManagerStopping()
	for _, proc := range procs {
		proc.Kill()
	}
	procsLock.Unlock()
	if pipeListener != nil {
		_ = pipeListener.Close()
	}
	if uiActionPipeListener != nil {
		_ = uiActionPipeListener.Close()
	}
	if cliSecretsPipeListener != nil {
		_ = cliSecretsPipeListener.Close()
	}
	procsGroup.Wait()
	if uninstall {
		err = UninstallManager()
		if err != nil {
			logger.Error("Unable to uninstall manager when quitting: %v", err)
		}
	}
	return false, 0
}

// runUILaunchPipeListener accepts connections on the named pipe and handles UI launch requests.
func runUILaunchPipeListener(listener net.Listener, requestCh chan<- uint32, procs map[uint32]*uiProcess, aliveSessions map[uint32]bool, procsLock *sync.Mutex) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		go handleUILaunchConn(conn, requestCh, procs, aliveSessions, procsLock)
	}
}

// runUIActionPipeListener forwards UI action requests (e.g. from a toast click,
// which Windows delivers to a new non-elevated process) to the session's UI.
func runUIActionPipeListener(listener net.Listener, procs map[uint32]*uiProcess, procsLock *sync.Mutex) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		go func() {
			defer conn.Close()
			var req [2]uint32 // session ID, action
			if err := binary.Read(conn, binary.LittleEndian, &req); err != nil {
				logger.Error("UI action pipe: failed to read request: %v", err)
				return
			}
			procsLock.Lock()
			_, running := procs[req[0]]
			procsLock.Unlock()
			var response uint32 = 1
			if running {
				logger.Info("UI action %d requested for session %d", req[1], req[0])
				IPCServerNotifyUIAction(req[0], UIAction(req[1]))
				response = 0
			}
			if err := binary.Write(conn, binary.LittleEndian, response); err != nil {
				logger.Error("UI action pipe: failed to write response: %v", err)
			}
		}()
	}
}

// handleUILaunchConn reads a session ID from the client, validates it, and either responds with
// 0 (launching), 1 (already running), or 2 (session not found).
func handleUILaunchConn(conn net.Conn, requestCh chan<- uint32, procs map[uint32]*uiProcess, aliveSessions map[uint32]bool, procsLock *sync.Mutex) {
	defer conn.Close()

	var sessionID uint32
	if err := binary.Read(conn, binary.LittleEndian, &sessionID); err != nil {
		logger.Error("UI launch pipe: failed to read session ID: %v", err)
		return
	}
	logger.Debug("UI launch (service): request received for session %d", sessionID)

	var response uint32
	procsLock.Lock()
	if _, ok := procs[sessionID]; ok {
		response = 1 // already running
		procsLock.Unlock()
		logger.Debug("UI launch (service): session %d already has UI process, responding 1", sessionID)
	} else {
		// Validate session is active (e.g. user is logged in).
		logger.Debug("UI launch (service): calling WTSQueryUserToken(session %d)", sessionID)
		var token windows.Token
		if err := windows.WTSQueryUserToken(sessionID, &token); err != nil {
			logger.Error("UI launch pipe: WTSQueryUserToken(session %d) failed: %v", sessionID, err)
			var errno syscall.Errno
			if errors.As(err, &errno) {
				logger.Error("UI launch (service): Windows error code %d (1314=privilege not held, 1008=token does not exist)", uint32(errno))
			}
			response = 2 // session not found or not active
			procsLock.Unlock()
		} else {
			token.Close()
			aliveSessions[sessionID] = true
			procsLock.Unlock()
			logger.Debug("UI launch (service): WTSQueryUserToken(session %d) succeeded", sessionID)
			select {
			case requestCh <- sessionID:
				response = 0 // success
				logger.Debug("UI launch (service): responding 0 (launching UI for session %d)", sessionID)
			default:
				response = 2 // channel full or service shutting down
				logger.Debug("UI launch (service): request channel full or service shutting down, responding 2")
			}
		}
	}

	if err := binary.Write(conn, binary.LittleEndian, response); err != nil {
		logger.Error("UI launch pipe: failed to write response: %v", err)
	}
}

// enableSeTcbPrivilege enables SeTcbPrivilege (act as part of the operating system) on the
// current process token. WTSQueryUserToken requires this privilege; without it, it returns
// error 1314 (required privilege not held) on some systems even when the service runs as LocalSystem.
func enableSeTcbPrivilege() error {
	logger.Debug("UI launch (service): enabling SeTcbPrivilege on process token")
	var h windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY|windows.TOKEN_ADJUST_PRIVILEGES, &h); err != nil {
		return err
	}
	defer h.Close()
	var privileges windows.Tokenprivileges
	privileges.PrivilegeCount = 1
	privileges.Privileges[0].Attributes = windows.SE_PRIVILEGE_ENABLED
	if err := windows.LookupPrivilegeValue(nil, windows.StringToUTF16Ptr("SeTcbPrivilege"), &privileges.Privileges[0].Luid); err != nil {
		return err
	}
	err := windows.AdjustTokenPrivileges(h, false, &privileges, uint32(unsafe.Sizeof(privileges)), nil, nil)
	if err != nil {
		return err
	}
	logger.Debug("UI launch (service): SeTcbPrivilege enabled successfully")
	return nil
}

// launchUIAtLoginForSession reports whether the user logged into sessionID
// should have the UI opened at sign-in. The manager runs as LocalSystem, so
// the setting is read from that user's LOCALAPPDATA rather than the process environment.
func launchUIAtLoginForSession(sessionID uint32) bool {
	logger.Debug("Open at login: querying token for session %d", sessionID)
	var token windows.Token
	if err := windows.WTSQueryUserToken(sessionID, &token); err != nil {
		logger.Error("Open at login: WTSQueryUserToken(session %d) failed: %v", sessionID, err)
		return false
	}
	defer token.Close()

	localAppData, err := localAppDataFromToken(token)
	if err != nil {
		logger.Error("Open at login: failed to read LOCALAPPDATA for session %d: %v", sessionID, err)
		return false
	}
	if localAppData == "" {
		logger.Error("Open at login: LOCALAPPDATA is empty for session %d", sessionID)
		return false
	}
	enabled := config.LaunchUIAtLoginEnabled(localAppData)
	logger.Info("Open UI at login for session %d: %v", sessionID, enabled)
	return enabled
}

func localAppDataFromToken(token windows.Token) (string, error) {
	var block *uint16
	if err := windows.CreateEnvironmentBlock(&block, token, false); err != nil {
		return "", err
	}
	defer windows.DestroyEnvironmentBlock(block)

	const key = "LOCALAPPDATA="
	p := unsafe.Pointer(block)
	for {
		entry := windows.UTF16PtrToString((*uint16)(p))
		if entry == "" {
			return "", nil
		}
		if len(entry) >= len(key) && strings.EqualFold(entry[:len(key)], key) {
			return entry[len(key):], nil
		}
		// StringToUTF16 includes the terminating NUL. Each unit is two bytes.
		p = unsafe.Add(p, len(windows.StringToUTF16(entry))*2)
	}
}

// waitForSessionExplorer polls until explorer.exe is running in the session.
// Launching the tray before the shell is ready leaves a UI process with no icon.
func waitForSessionExplorer(sessionID uint32, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for {
		if sessionHasExplorer(sessionID) {
			logger.Debug("Auto-connect: explorer.exe is running in session %d", sessionID)
			return
		}
		if time.Now().After(deadline) {
			logger.Info("Auto-connect: explorer.exe not seen in session %d after %v, launching UI anyway", sessionID, timeout)
			return
		}
		time.Sleep(time.Second)
	}
}

func sessionHasExplorer(sessionID uint32) bool {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		logger.Error("Auto-connect: process snapshot failed: %v", err)
		return false
	}
	defer windows.CloseHandle(snapshot)

	processEntry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(snapshot, &processEntry); err == nil; err = windows.Process32Next(snapshot, &processEntry) {
		if !strings.EqualFold(windows.UTF16ToString(processEntry.ExeFile[:]), "explorer.exe") {
			continue
		}
		var processSession uint32
		if err := windows.ProcessIdToSessionId(processEntry.ProcessID, &processSession); err != nil {
			continue
		}
		if processSession == sessionID {
			return true
		}
	}
	return false
}

func Run() error {
	return svc.Run(config.AppName+"Manager", &managerService{})
}

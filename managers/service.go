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
	"sync"
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

		var userToken windows.Token
		err := windows.WTSQueryUserToken(session, &userToken)
		if err != nil {
			return
		}
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

		// Start UI process once; do not auto-restart when it exits (user can run exe again to get UI)
		procsLock.Lock()
		if alive := aliveSessions[session]; !alive {
			procsLock.Unlock()
			return
		}
		procsLock.Unlock()

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
		IPCServerListen(ourReader, ourWriter, ourEvents, elevatedToken)
		// TODO: Add log mapping handle when ringlogger is implemented
		// theirLogMapping, err := ringlogger.Global.ExportInheritableMappingHandle()
		// if err != nil {
		// 	logger.Error("Unable to export inheritable mapping handle for logging: %v", err)
		// 	return
		// }

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
				// strconv.FormatUint(uint64(theirLogMapping), 10), // TODO: Add when ringlogger is implemented
			}, userProfileDirectory, []windows.Handle{
				windows.Handle(theirReader.Fd()),
				windows.Handle(theirWriter.Fd()),
				windows.Handle(theirEvents.Fd()),
				// theirLogMapping, // TODO: Add when ringlogger is implemented
			}, runToken)
		} else {
			err = errors.New("Session has logged out")
		}
		procsLock.Unlock()
		theirReader.Close()
		theirWriter.Close()
		theirEvents.Close()
		// windows.CloseHandle(theirLogMapping) // TODO: Add when ringlogger is implemented
		if err != nil {
			ourReader.Close()
			ourWriter.Close()
			ourEvents.Close()
			logger.Error("Unable to start manager UI process for user '%s@%s' for session %d: %v", username, domain, session, err)
			return
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
		procsLock.Unlock()
		ourReader.Close()
		ourWriter.Close()
		ourEvents.Close()
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

	// Do not auto-start UI processes at boot; they would often start before the user's
	// shell is ready and show no tray, and then the exe would think a UI is already running.
	// UI is started only when the user runs the exe (RequestUILaunch) or on session logon.

	// Listen for UI launch requests from standard users (named pipe).
	requestUILaunchChan := make(chan uint32)
	var pipeListener net.Listener
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
					procsLock.Lock()
					if alive := aliveSessions[sessionNotification.SessionID]; !alive {
						aliveSessions[sessionNotification.SessionID] = true
						// Do not start UI here; only start when user runs the exe (RequestUILaunch)
					}
					procsLock.Unlock()
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

// handleUILaunchConn reads a session ID from the client, validates it, and either responds with
// 0 (launching), 1 (already running), or 2 (session not found).
func handleUILaunchConn(conn net.Conn, requestCh chan<- uint32, procs map[uint32]*uiProcess, aliveSessions map[uint32]bool, procsLock *sync.Mutex) {
	defer conn.Close()

	var sessionID uint32
	if err := binary.Read(conn, binary.LittleEndian, &sessionID); err != nil {
		logger.Error("UI launch pipe: failed to read session ID: %v", err)
		return
	}

	var response uint32
	procsLock.Lock()
	if _, ok := procs[sessionID]; ok {
		response = 1 // already running
		procsLock.Unlock()
	} else {
		// Validate session is active (e.g. user is logged in).
		var token windows.Token
		if err := windows.WTSQueryUserToken(sessionID, &token); err != nil {
			response = 2 // session not found or not active
			procsLock.Unlock()
		} else {
			token.Close()
			aliveSessions[sessionID] = true
			procsLock.Unlock()
			select {
			case requestCh <- sessionID:
				response = 0 // success
			default:
				response = 2 // channel full or service shutting down
			}
		}
	}

	if err := binary.Write(conn, binary.LittleEndian, response); err != nil {
		logger.Error("UI launch pipe: failed to write response: %v", err)
	}
}

func Run() error {
	return svc.Run(config.AppName+"Manager", &managerService{})
}

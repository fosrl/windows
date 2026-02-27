//go:build windows

package managers

import (
	"encoding/binary"
	"io"
	"os"
	"time"

	"github.com/Microsoft/go-winio"
	"github.com/fosrl/newt/logger"
	"golang.org/x/sys/windows"
)

const uiLaunchPipePath = `\\.\pipe\pangolin-manager-ui-launch`

// RequestUILaunch connects to the manager service via named pipe and requests
// a UI launch for the current session. Returns true if the UI was successfully
// launched (or already running), false otherwise.
func RequestUILaunch() bool {
	pid := uint32(os.Getpid())
	var sessionID uint32
	if err := windows.ProcessIdToSessionId(pid, &sessionID); err != nil {
		logger.Error("UI launch: ProcessIdToSessionId(%d) failed: %v", pid, err)
		sessionID = windows.WTSGetActiveConsoleSessionId()
		logger.Debug("UI launch: falling back to WTSGetActiveConsoleSessionId() which returned session %d", sessionID)
	} else {
		logger.Debug("UI launch: ProcessIdToSessionId(%d) returned session %d", pid, sessionID)
	}
	if sessionID == 0 {
		logger.Error("Failed to get current session ID (got 0)")
		return false
	}

	// Connect to named pipe
	logger.Debug("UI launch: connecting to named pipe %s", uiLaunchPipePath)
	conn, err := winio.DialPipe(uiLaunchPipePath, nil)
	if err != nil {
		logger.Error("Failed to connect to manager service named pipe: %v", err)
		return false
	}
	defer conn.Close()
	logger.Debug("UI launch: connected to pipe, sending session ID %d", sessionID)

	// Send session ID
	err = binary.Write(conn, binary.LittleEndian, sessionID)
	if err != nil {
		logger.Error("Failed to send session ID to manager service: %v", err)
		return false
	}

	// Read response: 0 = success, 1 = already running, 2 = session not found
	var response uint32
	err = binary.Read(conn, binary.LittleEndian, &response)
	if err != nil {
		if err == io.EOF {
			logger.Error("Manager service closed connection unexpectedly")
		} else {
			logger.Error("Failed to read response from manager service: %v", err)
		}
		return false
	}
	logger.Debug("UI launch: manager response for session %d: %d (0=launching, 1=already running, 2=session not found)", sessionID, response)

	switch response {
	case 0:
		logger.Info("UI launch requested successfully for session %d", sessionID)
		return true
	case 1:
		logger.Info("UI already running for session %d", sessionID)
		return true
	case 2:
		logger.Error("Session %d not found or not active", sessionID)
		return false
	default:
		logger.Error("Unexpected response from manager service: %d", response)
		return false
	}
}

// RequestUILaunchWithRetry calls RequestUILaunch repeatedly with exponential backoff until
// it succeeds or the timeout is reached. Backoff: 200ms, 400ms, 800ms, 1600ms, cap at 2s.
func RequestUILaunchWithRetry(timeout time.Duration) bool {
	logger.Debug("UI launch: starting retry loop (timeout %v)", timeout)
	deadline := time.Now().Add(timeout)
	attempt := 0
	for time.Now().Before(deadline) {
		if attempt > 0 {
			logger.Debug("UI launch: retry attempt %d", attempt)
		}
		if RequestUILaunch() {
			return true
		}
		// Exponential backoff: 200*2^attempt ms, cap 2000ms
		backoff := 200 * (1 << attempt)
		if backoff > 2000 {
			backoff = 2000
		}
		sleep := time.Duration(backoff) * time.Millisecond
		if time.Now().Add(sleep).After(deadline) {
			break
		}
		time.Sleep(sleep)
		attempt++
	}
	logger.Debug("UI launch: retry loop gave up after %d attempts (timeout reached)", attempt)
	return false
}

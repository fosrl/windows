//go:build windows

package managers

import (
	"encoding/binary"
	"io"
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
	// Get current session ID
	sessionID := windows.WTSGetActiveConsoleSessionId()
	if sessionID == 0 {
		logger.Error("Failed to get current session ID (returned 0)")
		return false
	}

	// Connect to named pipe
	conn, err := winio.DialPipe(uiLaunchPipePath, nil)
	if err != nil {
		logger.Error("Failed to connect to manager service named pipe: %v", err)
		return false
	}
	defer conn.Close()

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
	deadline := time.Now().Add(timeout)
	attempt := 0
	for time.Now().Before(deadline) {
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
	return false
}

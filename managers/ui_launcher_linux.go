package managers

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/fosrl/newt/logger"
	"golang.org/x/sys/unix"
)

// ErrUIAlreadyRunning is returned by AttachUI when this user already has a UI.
var ErrUIAlreadyRunning = errors.New("the Pangolin UI is already running")

// currentSessionID identifies this UI to the daemon. Linux runs one UI per
// user, so the UID plays the part of the Windows session ID.
func currentSessionID() uint32 {
	return uint32(os.Getuid())
}

func dialDaemon() (*net.UnixConn, error) {
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: daemonSocketPath(), Net: "unix"})
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	return conn, nil
}

// AttachUI connects this UI to the daemon and returns the IPC pipes to pass to
// InitializeIPCClient.
func AttachUI() (reader, writer, events *os.File, err error) {
	conn, err := dialDaemon()
	if err != nil {
		return nil, nil, nil, err
	}
	defer conn.Close()
	if _, err := conn.Write([]byte{daemonRequestAttachUI}); err != nil {
		return nil, nil, nil, err
	}

	buf := make([]byte, 1)
	oob := make([]byte, unix.CmsgSpace(3*4))
	n, oobn, _, _, err := conn.ReadMsgUnix(buf, oob)
	if err != nil {
		return nil, nil, nil, err
	}
	var fds []int
	if oobn > 0 {
		msgs, err := unix.ParseSocketControlMessage(oob[:oobn])
		if err != nil {
			return nil, nil, nil, err
		}
		for _, m := range msgs {
			if got, err := unix.ParseUnixRights(&m); err == nil {
				fds = append(fds, got...)
			}
		}
	}
	closeFDs := func() {
		for _, fd := range fds {
			unix.Close(fd)
		}
	}
	if n != 1 {
		closeFDs()
		return nil, nil, nil, errors.New("empty reply from the daemon")
	}
	switch buf[0] {
	case attachOK:
	case attachAlreadyRunning:
		closeFDs()
		return nil, nil, nil, ErrUIAlreadyRunning
	default:
		closeFDs()
		return nil, nil, nil, errors.New("the daemon refused the connection")
	}
	if len(fds) != 3 {
		closeFDs()
		return nil, nil, nil, fmt.Errorf("expected 3 pipes from the daemon, got %d", len(fds))
	}
	for _, fd := range fds {
		unix.CloseOnExec(fd)
	}
	return os.NewFile(uintptr(fds[0]), "ipc-reader"),
		os.NewFile(uintptr(fds[1]), "ipc-writer"),
		os.NewFile(uintptr(fds[2]), "ipc-events"), nil
}

// RequestUIAction asks this user's running UI to perform action. Returns
// false if the daemon could not be reached or no UI is running.
func RequestUIAction(action UIAction) bool {
	conn, err := dialDaemon()
	if err != nil {
		logger.Error("Failed to connect to the Pangolin daemon: %v", err)
		return false
	}
	defer conn.Close()
	if _, err := conn.Write([]byte{daemonRequestUIAction}); err != nil {
		return false
	}
	if err := binary.Write(conn, binary.LittleEndian, uint32(action)); err != nil {
		return false
	}
	var reply [1]byte
	if _, err := conn.Read(reply[:]); err != nil {
		return false
	}
	return reply[0] == 0
}

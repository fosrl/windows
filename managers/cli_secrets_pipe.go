//go:build windows

package managers

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"syscall"
	"unsafe"

	"github.com/fosrl/newt/logger"
	"golang.org/x/sys/windows"
)

const cliSecretsPipePath = `\\.\pipe\pangolin-manager-cli-secrets`

const (
	cliSecretsStatusOK       uint32 = 0
	cliSecretsStatusNotFound uint32 = 1
	cliSecretsStatusError    uint32 = 2
)

var (
	modKernel32                     = windows.NewLazySystemDLL("kernel32.dll")
	procGetNamedPipeClientProcessId = modKernel32.NewProc("GetNamedPipeClientProcessId")
)

func runCLISecretsPipeListener(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		go handleCLISecretsConn(conn)
	}
}

func handleCLISecretsConn(conn net.Conn) {
	defer conn.Close()

	windowsSID, err := pipeClientWindowsSID(conn)
	if err != nil {
		logger.Error("CLI secrets pipe: resolve caller SID failed: %v", err)
		writeCLISecretsResponse(conn, cliSecretsStatusError, []byte("failed to resolve caller identity"))
		return
	}

	userID, err := readCLISecretsUserID(conn)
	if err != nil {
		logger.Error("CLI secrets pipe: read user id failed: %v", err)
		writeCLISecretsResponse(conn, cliSecretsStatusError, []byte("invalid request"))
		return
	}

	secrets, err := secretStore.Load(windowsSID, userID)
	if err != nil {
		logger.Error("CLI secrets pipe: load secrets failed (userId=%s): %v", userID, err)
		writeCLISecretsResponse(conn, cliSecretsStatusError, []byte(err.Error()))
		return
	}

	if secrets.SessionToken == "" && secrets.OlmId == "" && secrets.OlmSecret == "" {
		writeCLISecretsResponse(conn, cliSecretsStatusNotFound, []byte("{}"))
		return
	}

	payload, err := json.Marshal(secrets)
	if err != nil {
		writeCLISecretsResponse(conn, cliSecretsStatusError, []byte("failed to encode secrets"))
		return
	}
	writeCLISecretsResponse(conn, cliSecretsStatusOK, payload)
}

func pipeClientWindowsSID(conn net.Conn) (string, error) {
	handleProvider, ok := conn.(interface{ Fd() uintptr })
	if !ok {
		return "", syscall.EINVAL
	}

	pipeHandle := windows.Handle(handleProvider.Fd())
	var pid uint32
	r0, _, err := procGetNamedPipeClientProcessId.Call(uintptr(pipeHandle), uintptr(unsafe.Pointer(&pid)))
	if r0 == 0 {
		if err != nil && err != syscall.Errno(0) {
			return "", err
		}
		return "", syscall.EINVAL
	}

	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(process)

	var token windows.Token
	if err := windows.OpenProcessToken(process, windows.TOKEN_QUERY, &token); err != nil {
		return "", err
	}
	defer token.Close()

	user, err := token.GetTokenUser()
	if err != nil {
		return "", err
	}
	return user.User.Sid.String(), nil
}

func readCLISecretsUserID(conn net.Conn) (string, error) {
	var length uint32
	if err := binary.Read(conn, binary.LittleEndian, &length); err != nil {
		return "", err
	}
	if length == 0 || length > 256 {
		return "", io.ErrUnexpectedEOF
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

func writeCLISecretsResponse(conn net.Conn, status uint32, payload []byte) {
	_ = binary.Write(conn, binary.LittleEndian, status)
	_ = binary.Write(conn, binary.LittleEndian, uint32(len(payload)))
	if len(payload) > 0 {
		_, _ = conn.Write(payload)
	}
}

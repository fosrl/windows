package managers

import (
	"net"
	"syscall"
	"unsafe"

	"github.com/fosrl/newt/logger"
	"golang.org/x/sys/windows"
)

const cliSecretsPipePath = `\\.\pipe\pangolin-manager-cli-secrets`

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

	serveCLISecrets(conn, windowsSID)
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

package managers

import (
	"encoding/binary"
	"encoding/json"
	"io"

	"github.com/fosrl/newt/logger"
)

// Companion mode: the Pangolin CLI signs in with this app's session. It reads
// account details from the user's accounts.json and asks the manager for the
// secrets, which only the manager can read. The request is a little-endian
// uint32 length and the Pangolin user id; the reply is a uint32 status, a
// uint32 length and a payload (secrets JSON, or an error message). Windows
// serves this on a named pipe, Linux on the daemon socket.

const (
	cliSecretsStatusOK       uint32 = 0
	cliSecretsStatusNotFound uint32 = 1
	cliSecretsStatusError    uint32 = 2
)

// serveCLISecrets answers one CLI secrets request for the caller identified
// by userKey (a Windows SID, or a UID on Linux).
func serveCLISecrets(conn io.ReadWriter, userKey string) {
	userID, err := readCLISecretsUserID(conn)
	if err != nil {
		logger.Error("CLI secrets: read user id failed: %v", err)
		writeCLISecretsResponse(conn, cliSecretsStatusError, []byte("invalid request"))
		return
	}

	secrets, err := secretStore.Load(userKey, userID)
	if err != nil {
		logger.Error("CLI secrets: load secrets failed (userId=%s): %v", userID, err)
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

func readCLISecretsUserID(r io.Reader) (string, error) {
	var length uint32
	if err := binary.Read(r, binary.LittleEndian, &length); err != nil {
		return "", err
	}
	if length == 0 || length > 256 {
		return "", io.ErrUnexpectedEOF
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

func writeCLISecretsResponse(w io.Writer, status uint32, payload []byte) {
	_ = binary.Write(w, binary.LittleEndian, status)
	_ = binary.Write(w, binary.LittleEndian, uint32(len(payload)))
	if len(payload) > 0 {
		_, _ = w.Write(payload)
	}
}

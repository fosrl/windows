package tunnel

import (
	"context"
	"net"

	"github.com/Microsoft/go-winio"
)

// OLMSocketPath is the named pipe OLM serves its API on.
const OLMSocketPath = `\\.\pipe\pangolin-olm`

func dialOLM(ctx context.Context) (net.Conn, error) {
	return winio.DialPipe(OLMSocketPath, nil)
}

package tunnel

import (
	"context"
	"net"
)

// OLMSocketPath is the Unix socket OLM serves its API on. /run/pangolin-client
// is root-only, so only the daemon can reach it.
const OLMSocketPath = "/run/pangolin-client/olm.sock"

func dialOLM(ctx context.Context) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "unix", OLMSocketPath)
}

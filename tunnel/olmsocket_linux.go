package tunnel

import (
	"context"
	"net"
)

// OLMSocketPath is the Unix socket OLM serves its API on. OLM makes it
// world-writable, as the Windows pipe allows Everyone, so the Pangolin CLI
// can show and stop this app's tunnel.
const OLMSocketPath = "/run/pangolin-client/olm.sock"

func dialOLM(ctx context.Context) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "unix", OLMSocketPath)
}

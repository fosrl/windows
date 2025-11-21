module windows

go 1.25

require (
	github.com/fosrl/newt v0.0.0
	github.com/tailscale/walk v0.0.0-20251016200523-963e260a8227
	github.com/tailscale/win v0.0.0-20250213223159-5992cb43ca35
)

require (
	github.com/dblohm7/wingoes v0.0.0-20231019175336-f6e33aa7cc34 // indirect
	golang.org/x/exp v0.0.0-20251113190631-e25ba8c21ef6 // indirect
	golang.org/x/sys v0.38.0 // indirect
	gopkg.in/Knetic/govaluate.v3 v3.0.0 // indirect
)

replace github.com/fosrl/olm v0.0.0 => ../olm

replace github.com/fosrl/newt v0.0.0 => ../newt

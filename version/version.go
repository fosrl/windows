package version

// Number is the app version. It is set at build time via -ldflags (see the
// Makefile's VERSION variable):
//
//	go build -ldflags "-X github.com/fosrl/windows-linux/version.Number=<version>"
//
// A plain `go build` leaves it as "dev".
var Number = "dev"

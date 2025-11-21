//go:build !windows

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "This application only runs on Windows.")
	fmt.Fprintln(os.Stderr, "Please build for Windows using: GOOS=windows GOARCH=amd64 go build")
	os.Exit(1)
}

// Package icons embeds the application icons.
package icons

import _ "embed"

var (
	//go:embed icon-orange.ico
	Orange []byte

	//go:embed icon-gray.ico
	Gray []byte
)

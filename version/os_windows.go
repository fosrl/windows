package version

import (
	"fmt"

	"golang.org/x/sys/windows"
)

func OsName() string {
	versionInfo := windows.RtlGetVersion()
	winType := ""
	switch versionInfo.ProductType {
	case 3:
		winType = " Server"
	case 2:
		winType = " Controller"
	}
	return fmt.Sprintf("Windows%s %d.%d.%d", winType, versionInfo.MajorVersion, versionInfo.MinorVersion, versionInfo.BuildNumber)
}

// OSDisplayName is the OS name shown to users and the server.
const OSDisplayName = "Windows"

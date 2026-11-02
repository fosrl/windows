package updater

import "errors"

// errNotImplemented marks in-app updates on Linux, which arrive with the
// deb/rpm packaging.
var errNotImplemented = errors.New("in-app updates are not implemented on Linux yet")

func CheckForUpdate() (*UpdateFound, error) { return nil, errNotImplemented }

// DownloadVerifyAndExecute reports errNotImplemented on the returned channel.
func DownloadVerifyAndExecute(userToken uintptr) chan DownloadProgress {
	progress := make(chan DownloadProgress, 1)
	progress <- DownloadProgress{Error: errNotImplemented}
	close(progress)
	return progress
}

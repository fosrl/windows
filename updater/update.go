package updater

import (
	"hash"

	"golang.org/x/crypto/blake2b"
)

type DownloadProgress struct {
	Activity        string
	BytesDownloaded uint64
	BytesTotal      uint64
	Error           error
	Complete        bool
}

type progressHashWatcher struct {
	dp        *DownloadProgress
	c         chan DownloadProgress
	hashState hash.Hash
}

func (pm *progressHashWatcher) Write(p []byte) (int, error) {
	bytes := len(p)
	pm.dp.BytesDownloaded += uint64(bytes)
	pm.c <- *pm.dp
	pm.hashState.Write(p)
	return bytes, nil
}

type UpdateFound struct {
	name             string
	version          string
	hash             [blake2b.Size256]byte
	downloadLocation string // Can be empty (use default), a relative path, or a full URL
}

// Name returns the filename of the update MSI
func (u *UpdateFound) Name() string {
	return u.name
}

// Version returns the version of the update, parsed from its filename
func (u *UpdateFound) Version() string {
	return u.version
}

//go:build windows

package fingerprint

import "sync"

var (
	postureMemMu       sync.RWMutex
	postureMemFP       map[string]any
	postureMemPostures map[string]any
	postureMemOK       bool
)

func RefreshPostureMemory() {
	fp := GatherFingerprintInfo().ToMap()
	postures := GatherPostureChecks().ToMap()

	postureMemMu.Lock()
	defer postureMemMu.Unlock()
	postureMemFP = fp
	postureMemPostures = postures
	postureMemOK = len(fp) > 0 && len(postures) > 0
}

func SnapshotPostureMemory() (fp map[string]any, postures map[string]any, ok bool) {
	postureMemMu.RLock()
	defer postureMemMu.RUnlock()
	if !postureMemOK {
		return nil, nil, false
	}
	return postureMemFP, postureMemPostures, true
}

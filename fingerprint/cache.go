//go:build windows

package fingerprint

import (
	"sync"

	"github.com/fosrl/newt/logger"
)

var (
	postureMemMu       sync.RWMutex
	postureMemFP       map[string]any
	postureMemPostures map[string]any
	postureMemOK       bool
)

func RefreshPostureMemory() {
	logger.Debug("Fingerprint: RefreshPostureMemory() starting")
	fp := GatherFingerprintInfo().ToMap()
	logger.Debug("Fingerprint: RefreshPostureMemory() fingerprint map keys=%d", len(fp))

	logger.Debug("Fingerprint: RefreshPostureMemory() gathering posture checks")
	postures := GatherPostureChecks().ToMap()
	logger.Debug("Fingerprint: RefreshPostureMemory() posture map keys=%d", len(postures))

	postureMemMu.Lock()
	defer postureMemMu.Unlock()
	postureMemFP = fp
	postureMemPostures = postures
	postureMemOK = len(fp) > 0 && len(postures) > 0
	logger.Debug("Fingerprint: RefreshPostureMemory() finished (postureMemOK=%v)", postureMemOK)
}

func SnapshotPostureMemory() (fp map[string]any, postures map[string]any, ok bool) {
	postureMemMu.RLock()
	defer postureMemMu.RUnlock()
	if !postureMemOK {
		logger.Debug("Fingerprint: SnapshotPostureMemory() miss (cache not ready)")
		return nil, nil, false
	}
	logger.Debug("Fingerprint: SnapshotPostureMemory() hit (fp keys=%d, posture keys=%d)", len(postureMemFP), len(postureMemPostures))
	return postureMemFP, postureMemPostures, true
}

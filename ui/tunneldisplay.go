package ui

import "time"

// tunnelDropDelay is how long a drop in the tunnel state must last before the
// menu shows it, as in the macOS menu (MenuBarView.swift).
const tunnelDropDelay = 1500 * time.Millisecond

// phaseDebouncer holds back brief tunnel drops (connected to registering or
// disconnected, registering to disconnected) so a blip doesn't flash in the menu.
type phaseDebouncer struct {
	shown   tunnelPhase
	pending tunnelPhase
	since   time.Time
	waiting bool
}

func isTunnelDrop(from, to tunnelPhase) bool {
	switch from {
	case phaseRunning:
		return to == phaseStarting || to == phaseStopped || to == phaseOther
	case phaseStarting:
		return to == phaseStopped
	}
	return false
}

// update returns the phase to show for raw. When a drop is being held back it
// also returns how long until update should be called again. immediate skips
// the delay, for drops the user asked for.
func (d *phaseDebouncer) update(raw tunnelPhase, now time.Time, immediate bool) (tunnelPhase, time.Duration) {
	if immediate || !isTunnelDrop(d.shown, raw) {
		d.shown = raw
		d.waiting = false
		return raw, 0
	}
	if !d.waiting || d.pending != raw {
		d.pending, d.since, d.waiting = raw, now, true
	}
	if elapsed := now.Sub(d.since); elapsed < tunnelDropDelay {
		return d.shown, tunnelDropDelay - elapsed
	}
	d.shown = raw
	d.waiting = false
	return raw, 0
}

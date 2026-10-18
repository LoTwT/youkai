package windows

import "time"

// ShutdownReport is the "resource wrap-up" handover category of T2
// §4.2: the stop request and the release outcomes reported separately.
// "Stop requested" and "everything released" are distinct facts — a
// completed stop request does not mean the hotkey was unregistered or
// every key was released, and the report says exactly what happened to
// each item. Already-inserted input events cannot be recalled; that
// boundary is inherent and is not papered over.
type ShutdownReport struct {
	StopRequestedAt time.Time
	// PermissionClosed: input permission was closed by this shutdown.
	PermissionClosed bool
	// PermissionCloseReason is the reason recorded with the close.
	PermissionCloseReason string
	// HeldAtStop lists the keys/buttons still down after the last
	// operation settled (the close waits for an in-flight operation).
	HeldAtStop []HeldKey
	// Releases holds one record per stop-time release attempt; entries
	// with Released=false name keys that may still be down.
	Releases []ReleaseRecord
	// HotkeyState is the hotkey state after the shutdown attempt.
	HotkeyState HotkeyState
	// HotkeyTriggers is the trigger count at stop time.
	HotkeyTriggers int
	// HotkeyUnregisterErr is the raw error text of the unregistration,
	// empty when it was clean.
	HotkeyUnregisterErr string
}

// Shutdown runs the platform-side wrap-up: close input permission
// (synchronizing with the dispatch entry so no queued operation slips
// past the cancel), release every platform-pressed key or button, and
// unregister the stop hotkey. Each step runs even when an earlier step
// failed; failures are recorded in the report, never silently assumed
// resolved. In the real wiring the hotkey loop thread performs its own
// clean unregistration when stopped; the manager's Unregister here is
// idempotent and covers the pure-logic path.
func Shutdown(actor *InputActor, hotkey *HotkeyManager, reason string, at time.Time) ShutdownReport {
	report := ShutdownReport{
		StopRequestedAt:       at,
		PermissionCloseReason: reason,
	}
	if actor != nil {
		actor.ClosePermission(reason)
		report.PermissionClosed = true
		report.HeldAtStop = actor.Held()
		report.Releases = actor.ReleaseHeld()
	}
	if hotkey != nil {
		report.HotkeyTriggers = hotkey.TriggerCount()
		if err := hotkey.Unregister(); err != nil {
			report.HotkeyUnregisterErr = err.Error()
		}
		report.HotkeyState = hotkey.State()
	}
	return report
}

package collect

import (
	"errors"
	"fmt"
	"strings"

	"github.com/LoTwT/youkai/internal/scan"
)

// maxIncompletePositions bounds how many wheel positions may end incomplete
// before the walk stops the whole scan (decision §4.6 K5's escalation
// guard). The inventory has six wheel positions, so three is half of them:
// one or two isolated failures are the content-determined hard frames the
// 2026-10-29 ablation found stable and not worth re-observing, and the
// remaining four positions still yield a usable partial result. Three or
// more incomplete positions mean half the scope is unproven, which points
// at a systemic perception failure (capture, filter handling or wheel
// switching) rather than an unlucky frame; continuing would hide it behind
// a plausible partial result. The count is per scan, not consecutive:
// independent positions failing repeatedly is itself the signal.
const maxIncompletePositions = 3

// positionIncompleteError marks a fault that invalidates only the current
// wheel position — its preparation (wheel switch, filter panel, return to
// top) or its position proof (a scroll without a verifiable end, a lost
// position). The run records the position as incomplete with this stage and
// cause, then continues with the next position (decision §4.6 K5, T5 §1).
//
// It must never wrap a session-fatal fault: page verification, focus,
// target identity, geometry, permissions, capture, storage/evidence,
// cancellation and observed filter/sort/layout changes still stop the
// whole scan. Call sites choose the wrapper deliberately; the run loop does
// not classify by code alone, because navigationMismatch is also a
// page-level and item-level fault.
type positionIncompleteError struct {
	stage string
	err   error
}

func (e *positionIncompleteError) Error() string { return e.err.Error() }
func (e *positionIncompleteError) Unwrap() error { return e.err }

// positionIncomplete wraps an existing fault as a position-scoped failure
// of stage.
func positionIncomplete(stage string, err error) error {
	if err == nil {
		return nil
	}
	return &positionIncompleteError{stage: stage, err: err}
}

// positionFault builds a position-scoped diagnostic fault.
func positionFault(stage, code, format string, args ...any) error {
	return &positionIncompleteError{stage: stage, err: scan.NewFault(code, fmt.Errorf(format, args...))}
}

// asPositionIncomplete reports whether err invalidates only one position.
func asPositionIncomplete(err error) (*positionIncompleteError, bool) {
	var incomplete *positionIncompleteError
	if errors.As(err, &incomplete) {
		return incomplete, true
	}
	return nil, false
}

// faultDiagnosticCode returns the stable diagnostic code carried by err.
func faultDiagnosticCode(err error) string {
	var fault *scan.FaultError
	if errors.As(err, &fault) {
		return fault.Code
	}
	return ""
}

// positionLimitError builds the session fault raised when the incomplete
// positions reach maxIncompletePositions. It names every degraded position
// so the stop is diagnosable from the session trigger alone.
func positionLimitError(reasons []string) error {
	return scan.NewFault(CodePositionFailureLimit, fmt.Errorf(
		"collect: %d wheel positions are incomplete (limit %d); stopping instead of continuing so a systemic failure cannot be hidden: %s",
		len(reasons), maxIncompletePositions, strings.Join(reasons, "; ")))
}

// positionProofFailure builds a scroll or bottom confirmation fault. It
// marks the position incomplete only while the last observed frame is still
// a verifiable warehouse list. A page that left the warehouse (popup,
// loading page, lost stream) is session-fatal: continuing would hide the
// real page fault and keep dispatching input into an unknown page. A
// candidate retained from before the departure does not change that — the
// last frame, not the candidate, decides whether the position is the
// unresolved phenomenon or the page itself went away.
func positionProofFailure(stage, code string, out scrollOutcome, format string, args ...any) error {
	fault := scan.NewFault(code, fmt.Errorf(format, args...))
	if out.sawFrame && out.lastFrameIsList {
		return positionIncomplete(stage, fault)
	}
	return fault
}

// advanceStateFault describes a wheel or grid change observed after an
// advance action. The scanner never touches the wheel and never stops
// reading the grid on its own, so either change means someone operated the
// game or the page stopped being verifiable: both are session-fatal, not a
// position-level proof failure. The message distinguishes a changed wheel
// from a warehouse page whose grid could not be read.
func (run *inventoryRun) advanceStateFault(action string, after PageMatch) error {
	if !run.listVerdict(after) {
		return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
			"collect: wheel position %d: %s left a warehouse page whose grid could not be verified (last verdict %s): %s",
			run.position, action, after.Page, after.Evidence))
	}
	if after.Wheel == nil {
		return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
			"collect: wheel position %d: %s left the wheel position unreadable; the scanner never touches the wheel: %s",
			run.position, action, after.Evidence))
	}
	return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
		"collect: wheel position %d: %s changed the wheel position to %d; the scanner never touches the wheel: %s",
		run.position, action, after.Wheel.Position, after.Evidence))
}

// topWheelChange reports the latest page verdict when it no longer proves
// the prepared wheel position. The newest observation is preferred over
// run.listMatch because an unsettled Lost may keep an old candidate in the
// committed state while the latest frame already shows another wheel
// position or a page that left the warehouse. With no observation at all,
// the committed state is the only evidence available.
func (run *inventoryRun) topWheelChange(out scrollOutcome) (PageMatch, bool) {
	latest := run.listMatch
	if out.sawFrame {
		latest = out.lastMatch
	}
	if run.positionListVerdict(latest) {
		return PageMatch{}, false
	}
	return latest, true
}

// topProofFault wraps a top failure as position-scoped only while the latest
// observation still proves the prepared wheel position. A changed or
// unreadable wheel, a gridless warehouse page, or a page that left the
// warehouse is session-fatal: the scanner never touches the wheel, so the
// position-level explanation would hide someone operating the game.
func (run *inventoryRun) topProofFault(out scrollOutcome, fault error) error {
	if latest, changed := run.topWheelChange(out); changed {
		return run.advanceStateFault("the return to top", latest)
	}
	return positionIncomplete("top", fault)
}

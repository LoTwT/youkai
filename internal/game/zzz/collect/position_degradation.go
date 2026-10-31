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

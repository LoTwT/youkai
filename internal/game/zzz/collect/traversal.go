package collect

import (
	"context"

	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/scan"
)

// Traversal is the seam between the session controller and the ZZZ
// inventory traversal (T5). Precheck verifies the page and range
// preconditions of the locked session; Collect runs the traversal loop
// under the kernel's capacity and input-permission rules. PR 4a's only
// implementation is ObserveOnly; PR 4c adds Inventory, and the real
// traversal (anchors calibrated, recognition wired) keeps working
// through the same seam.
type Traversal interface {
	Precheck(ctx context.Context, sess platform.Session) error
	Collect(ctx context.Context, sess platform.Session, h *scan.ControllerHandle) error
}

// ObserveOnly is the PR 4a traversal: it dispatches no input, admits no
// observation and simply holds the session open until the scan context
// ends — a user stop, a fault or the quit path all arrive through
// there. Its Precheck deliberately checks nothing: no page evidence
// exists yet.
type ObserveOnly struct{}

// Precheck implements Traversal: PR 4a checks nothing.
func (ObserveOnly) Precheck(ctx context.Context, sess platform.Session) error {
	return nil
}

// Collect implements Traversal: wait for the end of the scan.
func (ObserveOnly) Collect(ctx context.Context, sess platform.Session, h *scan.ControllerHandle) error {
	<-ctx.Done()
	return ctx.Err()
}

// SessionView is the traversal's own narrow view of a platform session:
// only the surface T5's navigation consumes — observe the platform
// premises, take frames, dispatch one bounded input. platform.Session
// satisfies it by construction, so the Traversal seam adapts a real
// session implicitly, and a traversal's tests drive a scripted view
// without the rest of a session.
type SessionView interface {
	Observe() (platform.Observation, error)
	Frame(ctx context.Context, req platform.FrameRequest) (platform.Frame, error)
	Dispatch(op platform.InputOp) platform.InputResult
}

// Diagnostic codes of the inventory traversal. navigationMismatch
// follows the scan JSON contract's suggested vocabulary; the others are
// the traversal's own stable identifiers.
const (
	// CodeAnchorsMissing: the layout anchors are absent or structurally
	// invalid (the PR 4c skeleton era — 4b's field calibration supplies
	// the values), so no honest input coordinate or page evidence
	// exists. Reported in the preparing phase.
	CodeAnchorsMissing = "anchorsMissing"
	// CodePageUnverified: the precheck's frames did not establish the
	// warehouse page preconditions (the page, the top evidence or the
	// grid occupancy does not verify). Reported in the preparing phase.
	CodePageUnverified = "pageUnverified"
	// CodeNavigationMismatch: a dispatched action's effect could not be
	// verified within its retry budget — the click, close or drag never
	// moved the page where the navigation expected it, or the page became
	// undecidable. Stopping semantics: keep what was admitted, keep the
	// navigation-failure evidence, invent nothing.
	CodeNavigationMismatch = "navigationMismatch"
	// CodeDetailUnattributed: the detail could not be attributed to the
	// clicked target within the bounded observation (T5 §4) — the
	// selection evidence points elsewhere or never settles. The position
	// stays unresolved; the collection stops rather than skipping to the
	// next item.
	CodeDetailUnattributed = "detailUnattributed"
	// CodePositionLost: a scroll's position correspondence could not be
	// established (T5 §5) — the list moved but no unique overlap
	// correspondence verifies where it moved to. The machine stops rather
	// than guessing how many positions were skipped.
	CodePositionLost = "positionLost"
)

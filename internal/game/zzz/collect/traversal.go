package collect

import (
	"context"
	"encoding/json"

	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/scan"
)

// Traversal is the seam between the session controller and the ZZZ
// inventory traversal (T5). Precheck verifies the page and range
// preconditions of the locked session; Collect runs the traversal loop
// under the kernel's capacity and input-permission rules. ObserveOnly is
// the no-input path; Inventory is the calibrated single-page traversal.
// Recognition wiring and the 4b-2 preparation loop keep working through
// the same seam.
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

// ScopeReporter is the optional Traversal capability that publishes the
// range its precheck actually observed (T5 §3's 筛选、排序、数量 — what
// the page evidence really established, as opposed to the declared
// scope the application fixed at acceptance). The controller relays the
// report once the precheck succeeded; the application's record wiring
// persists it as the session's observed range. Inventory implements it.
type ScopeReporter interface {
	ObservedScope() json.RawMessage
}

// Diagnostic codes of the inventory traversal. navigationMismatch
// follows the scan JSON contract's suggested vocabulary; the others are
// the traversal's own stable identifiers.
const (
	// CodeAnchorsMissing: the layout anchors are absent or structurally
	// invalid, or the captured image size/content region is not the
	// calibrated display configuration, so no honest input coordinate or
	// page evidence exists. Reported in the preparing phase.
	CodeAnchorsMissing = "anchorsMissing"
	// CodePageUnverified: the precheck's frames did not establish the
	// warehouse page preconditions (the page, the top evidence or the
	// grid occupancy does not verify). Reported in the preparing phase.
	CodePageUnverified = "pageUnverified"
	// CodeNavigationMismatch: a dispatched action's effect could not be
	// verified within its retry budget — the click or drag never moved the
	// page where the navigation expected it, or the page became
	// undecidable. Stopping semantics: keep what was admitted, keep the
	// navigation-failure evidence, invent nothing.
	CodeNavigationMismatch = "navigationMismatch"
	// CodeDetailUnattributed: the detail could not be attributed to the
	// clicked target within the bounded observation (T5 §4) — the
	// selection evidence points elsewhere, the detail never changed or
	// never settled. The position stays unresolved; the collection stops
	// rather than skipping to the next item. A target that was already
	// selected before any click is admitted as an initial read; after a
	// click it has to prove a content change.
	CodeDetailUnattributed = "detailUnattributed"
	// CodeDetailUnresolved: the detail was attributed but the pane did not
	// yield a decidable rarity or substat row count — for example an A/B
	// badge or a fourth row that is neither empty nor a calibrated value.
	// The position stays unresolved; the collection stops and keeps the
	// admitted results.
	CodeDetailUnresolved = "detailUnresolved"
	// CodePositionLost: a scroll's position correspondence could not be
	// established (T5 §5) — the list moved but no unique overlap
	// correspondence verifies where it moved to. The machine stops rather
	// than guessing how many positions were skipped.
	CodePositionLost = "positionLost"
)

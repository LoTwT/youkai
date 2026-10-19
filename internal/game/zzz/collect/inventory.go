package collect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"time"

	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/scan"
)

// Inventory is the ZZZ drive-disc inventory traversal (T5): the
// navigation state machine over a calibrated layout and a page matcher
// — select a disc, open its detail, wait for the action's effect to
// verify and the page to settle, capture, attribute the detail to the
// clicked target, admit, close back, next; scroll by drag when the
// viewport is exhausted, advancing only on verified position
// correspondences; complete only on independent end evidence.
//
// PR 4c ships the skeleton: the machine, its evidence rules and its
// failure semantics, all driven through the Matcher and Layout
// contracts. The anchors' pixel values and the matcher's real
// implementation arrive with 4b's field calibration; until then an
// empty layout fails the precheck (anchorsMissing) and the Uncalibrated
// matcher fails it with pageUnverified — the skeleton never runs on
// guessed evidence.
//
// One Inventory serves one session (the controller's Traversal factory
// builds a fresh one per start); Precheck and Collect run on the
// producer goroutine in that order.
type Inventory struct {
	// Layout is the calibrated anchor set; see Layout. Empty or
	// inconsistent anchors make Precheck fail with anchorsMissing.
	Layout Layout
	// Matcher is the page evidence engine; nil takes Uncalibrated,
	// whose every verdict is “cannot determine”.
	Matcher Matcher

	// ActionBudget bounds one action's whole effect-and-settle wait —
	// from the dispatch to the stable expected verdict. Zero takes the
	// default.
	ActionBudget time.Duration
	// StableFrames is the consecutive same-verdict observation count
	// that counts as settled. Zero takes the default.
	StableFrames int
	// MaxRetries bounds re-dispatches of one action whose effect the
	// evidence showed never happened (the pre-action page still held at
	// the deadline). Zero takes the default; negative disables retries —
	// the first ineffective action already stops the walk.
	MaxRetries int
	// RecheckEvery paces re-observation after an idle (Reused) frame.
	// Zero takes the default.
	RecheckEvery time.Duration

	// boundGeometry is the session's baseline geometry version Precheck
	// bound the anchors to; Collect's every input names it and every
	// evidence frame must carry it.
	boundGeometry platform.GeometryVersion
	// observedScope is the range report Precheck assembled; ObservedScope
	// relays it to the controller.
	observedScope json.RawMessage
}

// Traversal pacing and bound defaults — starting points for the skeleton
// era, to be tuned from the field rounds' timing data (T5 §7: 超时时间、
// 稳定采样跨度与相似阈值来自三种场景的调参资料，并用未参与调整的序列验收).
const (
	// DefaultActionBudget bounds one action's effect-and-settle wait:
	// cloud latency plus a page transition, generous for the cloud
	// clients.
	DefaultActionBudget = 2 * time.Second
	// DefaultStableFrames is the consecutive same-verdict observation
	// count that counts as settled.
	DefaultStableFrames = 3
	// DefaultMaxRetries bounds re-dispatches of one ineffective action.
	DefaultMaxRetries = 2
	// DefaultRecheckEvery paces re-observation after an idle frame.
	DefaultRecheckEvery = 20 * time.Millisecond
)

func (inv *Inventory) actionBudget() time.Duration {
	if inv.ActionBudget > 0 {
		return inv.ActionBudget
	}
	return DefaultActionBudget
}

func (inv *Inventory) stableFrames() int {
	if inv.StableFrames > 0 {
		return inv.StableFrames
	}
	return DefaultStableFrames
}

func (inv *Inventory) maxRetries() int {
	if inv.MaxRetries < 0 {
		return 0
	}
	if inv.MaxRetries > 0 {
		return inv.MaxRetries
	}
	return DefaultMaxRetries
}

func (inv *Inventory) recheckEvery() time.Duration {
	if inv.RecheckEvery > 0 {
		return inv.RecheckEvery
	}
	return DefaultRecheckEvery
}

func (inv *Inventory) matcher() Matcher {
	if inv.Matcher != nil {
		return inv.Matcher
	}
	return Uncalibrated{}
}

// Observation is what the inventory traversal admits into the pipeline:
// one captured detail frame and the navigation evidence behind its
// admission. The kernel carries it opaque to the recognizer and the
// persistence layer; the fields align with the scan JSON contract's
// capture record (frameId, frameReceivedAt, geometryRevision,
// visitIndex, viewportIndex, row, column, association).
type Observation struct {
	// Image is the captured frame; ownership moves to the kernel at
	// admission. Not serialized — the pixels live in the record's
	// evidence files once the recognition pipeline's wiring persists
	// them (4b).
	Image *image.RGBA `json:"-"`
	// Content is the frame's valid content region, in Image pixels.
	Content image.Rectangle
	// FrameID, ArrivedAt and Geometry are the capture's provenance:
	// which frame, when it reached the platform, under which geometry.
	FrameID   uint64                   `json:"frameId"`
	ArrivedAt time.Time                `json:"frameReceivedAt"`
	Geometry  platform.GeometryVersion `json:"geometryRevision"`
	// Viewport, Row and Column locate the visit in the traversal
	// ledger's terms (0-based viewport and grid coordinates).
	Viewport int `json:"viewportIndex"`
	Row      int `json:"row"`
	Column   int `json:"column"`
	// VisitIndex is this admission's visit number (1-based). The
	// skeleton numbers admitted visits in admission order; the full
	// ledger — numbering every started visit, including unresolved and
	// non-S skips — lands with the ledger persistence work.
	VisitIndex int `json:"visitIndex"`
	// Association says how the detail was attributed to the target.
	Association Association `json:"association"`
	// PreClickFrameID is the pre-action list frame the click derived
	// from — the page-pair evidence's other half.
	PreClickFrameID uint64 `json:"preClickFrameId"`
	// Evidence is the admission's grounds in prose: the page pair, the
	// selection association, the stability and the matcher's own
	// confidence description (T5 §6: 每件保留…另附目标格位、对应的导航
	// 步骤与接纳理由).
	Evidence string `json:"evidence"`
}

// scopePayload is the observed-range report the skeleton's precheck
// assembles: only the facts its evidence actually established. The
// filters, sort order and count label T5 §3 demands stay absent until
// the recognition pipeline can read them (4b) — absence is explicit,
// never a guessed default.
type scopePayload struct {
	Kind     string `json:"kind"`
	Page     string `json:"page"`
	Geometry uint64 `json:"geometry"`
	Slots    int    `json:"slots"`
	Top      bool   `json:"top"`
}

// Precheck implements Traversal: verify, without dispatching any input,
// the page preconditions T5 §3 demands — the anchors are structurally
// sound and fit the captured content region, the session's baseline
// geometry is bound to them, and the current frame is the warehouse
// list at its top with a grid-shaped occupancy verdict. Failures carry
// anchorsMissing or pageUnverified; the matcher's evidence rides along.
func (inv *Inventory) Precheck(ctx context.Context, sess platform.Session) error {
	if err := inv.Layout.Validate(); err != nil {
		return scan.NewFault(CodeAnchorsMissing, err)
	}
	var view SessionView = sess
	if err := ctx.Err(); err != nil {
		return err
	}
	obs, err := view.Observe()
	if err != nil {
		return wrapObservationErr(err)
	}
	if !obs.Ready {
		if len(obs.Problems) > 0 {
			return scan.NewFault(obs.Problems[0].Code, obs.Problems[0])
		}
		return scan.NewFault(platform.CodeObservationFailed, errors.New("the observation is not ready and reports no problem"))
	}
	geo, err := bindGeometry(obs)
	if err != nil {
		return scan.NewFault(platform.CodeObservationFailed, err)
	}
	frame, err := view.Frame(ctx, platform.FrameRequest{})
	if err != nil {
		return wrapObservationErr(err)
	}
	if frame.Geometry != geo {
		return scan.NewFault(platform.CodeGeometryChanged, fmt.Errorf(
			"collect: precheck: frame %d arrived under geometry %d while the observation bound %d",
			frame.ID, frame.Geometry, geo))
	}
	if !inv.Layout.FitsContent(frame.Content) {
		return scan.NewFault(CodeAnchorsMissing, fmt.Errorf(
			"collect: precheck: the anchors do not fit the captured content region %v — not this geometry's calibration",
			frame.Content))
	}
	m := inv.matcher().Match(frame)
	if m.Page != PageWarehouse || m.List == nil || len(m.List.Occupied) != len(inv.Layout.Slots) {
		return scan.NewFault(CodePageUnverified, fmt.Errorf(
			"collect: precheck: the current frame is not a verifiable warehouse list (verdict %s): %s",
			m.Page, m.Evidence))
	}
	if !m.List.Top {
		return scan.NewFault(CodePageUnverified, errors.New(
			"collect: precheck: no top evidence — the list may be mid-scroll, and a mid-list start cannot be marked as covering the inventory"))
	}
	scope, err := json.Marshal(scopePayload{
		Kind:     "zzzInventory",
		Page:     string(PageWarehouse),
		Geometry: uint64(geo),
		Slots:    len(inv.Layout.Slots),
		Top:      true,
	})
	if err != nil {
		return fmt.Errorf("collect: precheck: marshal observed scope: %w", err)
	}
	inv.boundGeometry = geo
	inv.observedScope = scope
	return nil
}

// ObservedScope implements ScopeReporter: the range facts Precheck
// actually established. Nil before a successful precheck.
func (inv *Inventory) ObservedScope() json.RawMessage {
	return append(json.RawMessage(nil), inv.observedScope...)
}

// Collect implements Traversal: run the navigation state machine on the
// prepared session.
func (inv *Inventory) Collect(ctx context.Context, sess platform.Session, h *scan.ControllerHandle) error {
	var view SessionView = sess
	run := &inventoryRun{
		inv:     inv,
		view:    view,
		h:       h,
		geo:     inv.boundGeometry,
		visited: make([]bool, len(inv.Layout.Slots)),
	}
	return run.run(ctx)
}

// inventoryRun is one Collect's mutable state. It lives on the producer
// goroutine only.
type inventoryRun struct {
	inv  *Inventory
	view SessionView
	h    *scan.ControllerHandle

	// geo is the geometry version the anchors are bound to; every input
	// names it and every evidence frame must carry it.
	geo platform.GeometryVersion
	// lastArrived is the arrival time of the newest frame observed — the
	// After of “current content” requests.
	lastArrived time.Time
	// viewport is the current viewport index: 0 at the start, +1 per
	// verified scroll (the traversal ledger's viewportIndex).
	viewport int
	// visited tracks, per slot of the current viewport in reading order,
	// whether the position was already visited. Overlap positions a
	// verified scroll maps keep their state; genuinely new positions
	// start unvisited.
	visited []bool
	// visitIndex counts admitted visits; the next admission takes
	// visitIndex+1.
	visitIndex int
	// listMatch is the current list state: the stable warehouse verdict
	// the next step builds on (its frame is re-taken fresh whenever a
	// click needs pre-action provenance).
	listMatch PageMatch
}

// run is the collection loop: visit every occupied, unvisited position
// of the current viewport in reading order; when the viewport is
// exhausted, complete on independent end evidence or advance by one
// verified scroll (T5 §5). It returns nil only on that end evidence —
// never because the frames stopped changing.
func (run *inventoryRun) run(ctx context.Context) error {
	if err := run.establishList(ctx); err != nil {
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !run.h.InputPermitted() {
			return ctx.Err()
		}
		i := run.nextSlot()
		if i < 0 {
			// The viewport is exhausted: every position is visited or
			// unoccupied. Unoccupied slots are not visits (T5 §5: 空格
			// 不产生物品记录).
			if run.listMatch.List.Bottom {
				// Independent end evidence (T5 §5: 末端证据) — the walk
				// is complete. The coverage conclusion itself is the
				// record layer's to draw; this return only says the
				// machine finished its walk.
				return nil
			}
			if err := run.scroll(ctx); err != nil {
				return err
			}
			continue
		}
		if err := run.visitSlot(ctx, i); err != nil {
			return err
		}
	}
}

// nextSlot returns the next slot to visit: the first reading-order slot
// that holds an item and is not yet visited, or -1 when the viewport is
// exhausted.
func (run *inventoryRun) nextSlot() int {
	for i := range run.visited {
		if !run.visited[i] && run.listMatch.List.Occupied[i] {
			return i
		}
	}
	return -1
}

// establishList waits for the warehouse list to show stably — the state
// every click builds its pre-click evidence on. The top evidence
// itself was Precheck's to verify; the machine has not scrolled, so it
// does not re-demand it here.
func (run *inventoryRun) establishList(ctx context.Context) error {
	out := run.awaitSettled(ctx, run.listVerdict)
	switch out.class {
	case awaitStable:
		run.listMatch = out.match
		return nil
	case awaitCanceled, awaitFrameErr:
		return out.err
	case awaitGeometry:
		return run.geometryChanged(out.frame)
	default:
		return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
			"collect: the warehouse list never showed stably within the budget (last verdict %s): %s",
			out.match.Page, out.match.Evidence))
	}
}

// listVerdict is the structural predicate of a usable warehouse
// verdict: the warehouse page with an occupancy verdict shaped like the
// layout's grid. A matcher reporting another shape is a contract
// violation, surfaced as a fault rather than papered over.
func (run *inventoryRun) listVerdict(m PageMatch) bool {
	return m.Page == PageWarehouse && m.List != nil && len(m.List.Occupied) == len(run.inv.Layout.Slots)
}

// visitSlot processes one occupied, unvisited slot: acquire capacity,
// re-check the premises, verify the pre-click list evidence, click,
// wait for the detail to settle on the target, admit the capture with
// its evidence, and close back to the list — one verified action at a
// time (T5 §4).
func (run *inventoryRun) visitSlot(ctx context.Context, i int) error {
	row, col := run.inv.Layout.slotPosition(i)
	target := fmt.Sprintf("viewport %d slot %d (row %d, column %d)", run.viewport, i, row, col)
	expectDetail := func(m PageMatch) bool {
		return m.Page == PageDiscDetail && m.Detail != nil && m.Detail.Selected != nil && *m.Detail.Selected == i
	}
	expectList := func(m PageMatch) bool {
		return run.listVerdict(m) && m.List.Occupied[i]
	}
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !run.h.InputPermitted() {
			return ctx.Err()
		}
		// Capacity first (T5 §4: 控制者先取得队列容量), then the platform
		// premises (重查平台条件), then the pre-click evidence.
		slot := run.h.Acquire(ctx)
		if slot == nil {
			return ctx.Err()
		}
		if err := run.observeReady(ctx); err != nil {
			slot.Discard()
			return err
		}
		// The pre-click evidence: the list shows stably with the target
		// slot occupied (T5 §4.1: 列表仍属于同一范围，目标格位有物品且位置
		// 可对应).
		pre := run.awaitSettled(ctx, expectList)
		switch pre.class {
		case awaitStable:
		case awaitCanceled, awaitFrameErr:
			slot.Discard()
			return pre.err
		case awaitGeometry:
			slot.Discard()
			return run.geometryChanged(pre.frame)
		default:
			slot.Discard()
			if pre.match.Page == PageWarehouse && !run.listVerdict(pre.match) {
				return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
					"collect: the matcher's list evidence does not fit the %d-slot grid: %s",
					len(run.inv.Layout.Slots), pre.match.Evidence))
			}
			return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
				"collect: the list never showed %s occupied and stable within the budget (last verdict %s)",
				target, pre.match.Page))
		}

		// One short click on the target slot (T5 §4: 再发一次短点击).
		res := run.view.Dispatch(platform.InputOp{
			Kind:     platform.InputClick,
			Geometry: run.geo,
			FrameID:  pre.frame.ID,
			At:       run.inv.Layout.slotCenter(i),
		})
		if err := run.dispatchOutcome(ctx, res); err != nil {
			slot.Discard()
			return err
		}

		// The effect wait: the action's finish time is the frame time
		// lower bound (Frame(After)), and the page verdict — never the
		// dispatch result — confirms the effect (T5 §4.2/4.3).
		out := run.awaitSettled(ctx, expectDetail)
		switch out.class {
		case awaitStable:
			// The T5 §4 gate held: the page pair (warehouse list →
			// detail of the target), the selection on the target, the
			// geometry bound, the verdict settled. Admit the capture
			// with its evidence.
			run.visitIndex++
			run.visited[i] = true
			obs := Observation{
				Image:           out.frame.Image,
				Content:         out.frame.Content,
				FrameID:         out.frame.ID,
				ArrivedAt:       out.frame.ArrivedAt,
				Geometry:        out.frame.Geometry,
				Viewport:        run.viewport,
				Row:             row,
				Column:          col,
				VisitIndex:      run.visitIndex,
				Association:     AssociationTransition,
				PreClickFrameID: pre.frame.ID,
				Evidence:        run.admissionEvidence(target, pre.frame, out, attempt+1),
			}
			if _, err := slot.Admit(obs); err != nil {
				slot.Discard()
				return fmt.Errorf("collect: admit %s: %w", target, err)
			}
			// Close back to the list; the close wait's stable verdict
			// becomes the current list state.
			return run.closeDetail(ctx, out.frame)
		case awaitCanceled, awaitFrameErr:
			slot.Discard()
			return out.err
		case awaitGeometry:
			slot.Discard()
			return run.geometryChanged(out.frame)
		default: // awaitDeadline
			slot.Discard()
			switch {
			case out.expected:
				// The detail appeared but never settled on the target:
				// the action took effect, so re-clicking is not the
				// remedy — the position stays unresolved (T5 §4: 完成
				// 有界观察…仍无法确认详情归属时停止本次采集，按未解决
				// 位置记账，不跳到下一件).
				return scan.NewFault(CodeDetailUnattributed, fmt.Errorf(
					"collect: the detail of %s appeared but never settled: last verdict %s: %s",
					target, out.match.Page, out.match.Evidence))
			case out.match.Page == PageDiscDetail:
				// A detail shows but its selection evidence does not
				// attribute it to the target.
				return scan.NewFault(CodeDetailUnattributed, fmt.Errorf(
					"collect: the shown detail does not attribute to %s (selection %s): %s",
					target, selectionOf(out.match), out.match.Evidence))
			case out.match.Page == PageWarehouse && run.listVerdict(out.match) && attempt < run.inv.maxRetries():
				// The click never took effect — the pre-action page held
				// to the deadline. Retry within the bound; the next
				// attempt re-establishes every precondition.
				continue
			default:
				// Unknown page or retries exhausted: re-clicking would be
				// blind (T5: 不因到期而盲目补点) — stop with the evidence.
				return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
					"collect: clicking %s never opened its detail within the budget (last verdict %s, attempt %d): %s",
					target, out.match.Page, attempt+1, out.match.Evidence))
			}
		}
	}
}

// selectionOf renders a detail verdict's selection evidence for
// diagnostics.
func selectionOf(m PageMatch) string {
	if m.Detail == nil || m.Detail.Selected == nil {
		return "none"
	}
	return fmt.Sprintf("slot %d", *m.Detail.Selected)
}

// closeDetail returns from the admitted detail to the warehouse list:
// click the close control and wait for the list to show stably — that
// stable verdict becomes the current list state — with bounded retries
// when the close never took.
func (run *inventoryRun) closeDetail(ctx context.Context, detailFrame platform.Frame) error {
	frameID := detailFrame.ID
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !run.h.InputPermitted() {
			return ctx.Err()
		}
		if err := run.observeReady(ctx); err != nil {
			return err
		}
		res := run.view.Dispatch(platform.InputOp{
			Kind:     platform.InputClick,
			Geometry: run.geo,
			FrameID:  frameID,
			At:       run.inv.Layout.closeCenter(),
		})
		if err := run.dispatchOutcome(ctx, res); err != nil {
			return err
		}
		out := run.awaitSettled(ctx, run.listVerdict)
		switch out.class {
		case awaitStable:
			run.listMatch = out.match
			return nil
		case awaitCanceled, awaitFrameErr:
			return out.err
		case awaitGeometry:
			return run.geometryChanged(out.frame)
		default: // awaitDeadline
			frameID = out.frame.ID
			switch {
			case out.expected:
				return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
					"collect: the list returned after the close but never settled: %s", out.match.Evidence))
			case out.match.Page == PageDiscDetail && attempt < run.inv.maxRetries():
				continue // the close never took; retry within the bound
			default:
				return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
					"collect: the close never returned the list within the budget (last verdict %s, attempt %d)",
					out.match.Page, attempt+1))
			}
		}
	}
}

// scroll advances the list by one small drag step and verifies the
// transition by correspondence (T5 §5): the pages before and after are
// the same warehouse, so only the matcher's overlap correspondences
// prove where the list went — a stable warehouse verdict alone proves
// nothing.
func (run *inventoryRun) scroll(ctx context.Context) error {
	// The before-state: the current list frame and verdict.
	before, err := run.currentList(ctx)
	if err != nil {
		return err
	}
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !run.h.InputPermitted() {
			return ctx.Err()
		}
		if err := run.observeReady(ctx); err != nil {
			return err
		}
		res := run.view.Dispatch(platform.InputOp{
			Kind:     platform.InputDrag,
			Geometry: run.geo,
			FrameID:  before.ID,
			At:       run.inv.Layout.ScrollFrom,
			To:       run.inv.Layout.ScrollTo,
		})
		if err := run.dispatchOutcome(ctx, res); err != nil {
			return err
		}
		verdict, err := run.awaitScroll(ctx, before)
		if err != nil {
			return err
		}
		if !verdict.corresponded {
			// The list verifiably did not move: the position is safe, so
			// the same drag may be retried within the bound. (No
			// displacement is not end evidence — T5 §5 — so exhausting
			// the retries stops the walk rather than completing it.)
			if attempt < run.inv.maxRetries() {
				continue
			}
			return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
				"collect: the drag never displaced the list within the budget (attempt %d) and no end evidence held", attempt+1))
		}
		if err := run.applyTransition(before, verdict.after, run.listMatch, verdict.afterMatch, verdict.match); err != nil {
			return err
		}
		run.listMatch = verdict.afterMatch
		return nil
	}
}

// currentList takes the current list frame — the latest content, fresh
// or idle-reused — and requires a usable warehouse verdict on it.
func (run *inventoryRun) currentList(ctx context.Context) (platform.Frame, error) {
	f, err := run.view.Frame(ctx, platform.FrameRequest{After: run.lastArrived})
	if err != nil {
		return platform.Frame{}, wrapObservationErr(err)
	}
	if f.Geometry != run.geo {
		return platform.Frame{}, run.geometryChanged(f)
	}
	if !f.Reused {
		run.lastArrived = f.ArrivedAt
	}
	m := run.inv.matcher().Match(f)
	if !run.listVerdict(m) {
		return platform.Frame{}, scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
			"collect: the current list frame (id %d) is not a verifiable warehouse list: %s", f.ID, m.Evidence))
	}
	run.listMatch = m
	return f, nil
}

// scrollOutcome is one awaitScroll's terminal result: a correspondence
// the matcher reported (still to be validated by applyTransition), or
// the verifiable non-effect of the drag.
type scrollOutcome struct {
	corresponded bool
	after        platform.Frame
	afterMatch   PageMatch
	match        ScrollMatch
}

// awaitScroll waits for the scroll's effect: a fresh warehouse frame
// whose correspondence with the before-frame the matcher reports. Only
// fresh frames count — the pages before and after are the same
// warehouse, so an idle re-confirmation of the before-frame proves
// nothing moved — and every fresh warehouse frame gets a correspondence
// verdict: a reporting verdict is itself the settle evidence, since
// correspondences are only readable on a settled overlap.
//
// Deadline classification: displacement was seen but never verified →
// positionLost (the position is at risk; stop — no more drags, no
// guessing how far the list went); no displacement at all → the caller
// may retry the same drag, because the list verifiably did not move and
// a retry cannot skip positions.
func (run *inventoryRun) awaitScroll(ctx context.Context, before platform.Frame) (scrollOutcome, error) {
	deadline := time.Now().Add(run.inv.actionBudget())
	after := before.ArrivedAt
	displaced := false
	for {
		fctx, cancel := context.WithDeadline(ctx, deadline)
		f, err := run.view.Frame(fctx, platform.FrameRequest{After: after})
		cancel()
		if err != nil {
			switch {
			case ctx.Err() != nil:
				return scrollOutcome{}, ctx.Err()
			case errors.Is(err, context.DeadlineExceeded):
				if displaced {
					return scrollOutcome{}, scan.NewFault(CodePositionLost, errors.New(
						"collect: the list displaced but no unique correspondence verified where it went"))
				}
				return scrollOutcome{}, nil
			default:
				return scrollOutcome{}, wrapObservationErr(err)
			}
		}
		if f.Geometry != run.geo {
			return scrollOutcome{}, run.geometryChanged(f)
		}
		if !f.Reused {
			run.lastArrived = f.ArrivedAt
			after = f.ArrivedAt
		}
		m := run.inv.matcher().Match(f)
		if m.Page == PageDiscDetail {
			// A drag does not open details in this machine's model; the
			// navigation diverged from reality.
			return scrollOutcome{}, scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
				"collect: a detail page showed after the scroll drag: %s", m.Evidence))
		}
		if !run.listVerdict(m) {
			// Transition or animation frame: keep observing within the
			// deadline.
			if f.Reused {
				if err := sleepCtx(ctx, run.inv.recheckEvery()); err != nil {
					return scrollOutcome{}, err
				}
			}
			continue
		}
		sm := run.inv.matcher().MatchScroll(before, f)
		if sm.Displaced {
			displaced = true
		}
		if sm.Displaced && len(sm.Matches) > 0 {
			return scrollOutcome{corresponded: true, after: f, afterMatch: m, match: sm}, nil
		}
		if f.Reused {
			if err := sleepCtx(ctx, run.inv.recheckEvery()); err != nil {
				return scrollOutcome{}, err
			}
		}
	}
}

// applyTransition validates one scroll correspondence (T5 §5) and
// carries the visited bookkeeping across it. A correspondence is
// accepted only when it is complete and consistent: every pair in
// bounds and one-to-one, columns preserved, the row displacement
// uniform and within one small step that keeps at least a full row of
// overlap, the overlap fully accounted for (an unaccounted overlap
// position could hold an old or a new item — the machine does not guess
// which), and the same item's occupancy agreeing on both sides.
// Anything else is positionLost: the machine stops rather than guessing
// how far the list went.
func (run *inventoryRun) applyTransition(before, after platform.Frame, beforeMatch, afterMatch PageMatch, sm ScrollMatch) error {
	l := run.inv.Layout
	fail := func(reason string) error {
		return scan.NewFault(CodePositionLost, fmt.Errorf(
			"collect: the scroll transition %d → %d was rejected: %s (correspondence evidence: %s)",
			before.ID, after.ID, reason, sm.Evidence))
	}
	if len(sm.Matches) == 0 {
		return fail("no correspondences")
	}
	fromSeen := make(map[int]bool, len(sm.Matches))
	toSeen := make(map[int]bool, len(sm.Matches))
	shift := -1
	for _, p := range sm.Matches {
		if p.FromRow < 0 || p.FromRow >= l.Rows || p.FromColumn < 0 || p.FromColumn >= l.Columns ||
			p.ToRow < 0 || p.ToRow >= l.Rows || p.ToColumn < 0 || p.ToColumn >= l.Columns {
			return fail(fmt.Sprintf("the pair (%d,%d)→(%d,%d) is outside the %d×%d grid",
				p.FromRow, p.FromColumn, p.ToRow, p.ToColumn, l.Rows, l.Columns))
		}
		from, to := l.positionSlot(p.FromRow, p.FromColumn), l.positionSlot(p.ToRow, p.ToColumn)
		if fromSeen[from] || toSeen[to] {
			return fail("the correspondences are not one-to-one")
		}
		fromSeen[from], toSeen[to] = true, true
		if p.FromColumn != p.ToColumn {
			return fail(fmt.Sprintf("the pair (%d,%d)→(%d,%d) changes column",
				p.FromRow, p.FromColumn, p.ToRow, p.ToColumn))
		}
		pairShift := p.FromRow - p.ToRow
		if pairShift < 1 || pairShift > l.Rows-1 {
			return fail(fmt.Sprintf("the row shift %d of the pair (%d,%d)→(%d,%d) is not one small step that keeps a full row of overlap",
				pairShift, p.FromRow, p.FromColumn, p.ToRow, p.ToColumn))
		}
		if shift == -1 {
			shift = pairShift
		} else if shift != pairShift {
			return fail("the correspondences disagree on how far the list moved")
		}
		// The same item cannot appear or disappear: occupancy must agree
		// on both sides of a correspondence.
		if beforeMatch.List.Occupied[from] != afterMatch.List.Occupied[to] {
			return fail(fmt.Sprintf("the correspondence (%d,%d)→(%d,%d) changes occupancy",
				p.FromRow, p.FromColumn, p.ToRow, p.ToColumn))
		}
	}
	// Full overlap coverage: the after-viewport's overlap rows must be
	// exactly the matched to-positions.
	for row := 0; row < l.Rows-shift; row++ {
		for col := 0; col < l.Columns; col++ {
			if !toSeen[l.positionSlot(row, col)] {
				return fail(fmt.Sprintf("the overlap position (%d,%d) has no correspondence — the machine cannot tell old from new there",
					row, col))
			}
		}
	}
	// Carry the bookkeeping: matched positions inherit their visit
	// state; the remaining after-positions are genuinely new.
	newVisited := make([]bool, len(run.visited))
	for _, p := range sm.Matches {
		newVisited[l.positionSlot(p.ToRow, p.ToColumn)] =
			run.visited[l.positionSlot(p.FromRow, p.FromColumn)]
	}
	run.visited = newVisited
	run.viewport++
	return nil
}

// awaitClass classifies how one bounded wait ended.
type awaitClass int

const (
	awaitStable   awaitClass = iota // the expected verdict held for StableFrames consecutive observations
	awaitDeadline                   // the action budget expired first
	awaitCanceled                   // the scan context ended
	awaitFrameErr                   // Frame failed
	awaitGeometry                   // a frame arrived under a different geometry version
)

// awaitOutcome is one bounded wait's result: the class, the last
// observed frame and verdict (set for every class but awaitCanceled),
// the error for the error classes, and whether any observation carried
// the expected verdict.
type awaitOutcome struct {
	class    awaitClass
	frame    platform.Frame
	match    PageMatch
	err      error
	expected bool
}

// awaitSettled waits for the expected page verdict to hold stably:
// StableFrames consecutive observations with the verdict, where an
// observation is a fresh frame or an idle (Reused) confirmation of the
// frame the previous observation rested on — unchanged content is
// still current content, so an idle confirmation is stability evidence
// too. A contradicting observation resets the count (a transition
// frame, an animation frame, another page). The whole wait is bounded
// by the action budget; Frame runs under a deadline-bounded context so
// a silent stream cannot outlive the budget.
//
// Tradeoff, noted for 4b: whole-verdict stability is coarser than T5
// §4.2's 文字和图标区域的稳定性 — a background animation under a settled
// detail region would not reset this count. The refined per-region rule
// belongs to the calibrated matcher.
func (run *inventoryRun) awaitSettled(ctx context.Context, expect func(PageMatch) bool) awaitOutcome {
	deadline := time.Now().Add(run.inv.actionBudget())
	samples := 0
	out := awaitOutcome{}
	after := run.lastArrived
	for {
		fctx, cancel := context.WithDeadline(ctx, deadline)
		f, err := run.view.Frame(fctx, platform.FrameRequest{After: after})
		cancel()
		if err != nil {
			switch {
			case ctx.Err() != nil:
				out.class, out.err = awaitCanceled, ctx.Err()
			case errors.Is(err, context.DeadlineExceeded):
				out.class = awaitDeadline
			default:
				out.class, out.err = awaitFrameErr, wrapObservationErr(err)
			}
			return out
		}
		if f.Geometry != run.geo {
			out.class, out.frame = awaitGeometry, f
			return out
		}
		m := run.inv.matcher().Match(f)
		out.frame, out.match = f, m
		if !f.Reused {
			run.lastArrived = f.ArrivedAt
			after = f.ArrivedAt
		}
		if expect(m) {
			out.expected = true
			samples++
			if samples >= run.inv.stableFrames() {
				out.class = awaitStable
				return out
			}
		} else {
			samples = 0
		}
		if f.Reused {
			// Nothing new: pace the re-observation; the deadline bounds
			// the rest.
			if err := sleepCtx(ctx, run.inv.recheckEvery()); err != nil {
				out.class, out.err = awaitCanceled, err
				return out
			}
		}
	}
}

// observeReady re-checks the platform premises before an input: the
// observation must succeed and be Ready, under the bound geometry (T5
// §4's 重查平台条件). A failed observation or an unmet premise stops the
// session with its fault — premise problems during collection are not
// waited out; stopping is one-way.
func (run *inventoryRun) observeReady(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	obs, err := run.view.Observe()
	if err != nil {
		return wrapObservationErr(err)
	}
	if !obs.Ready {
		if len(obs.Problems) > 0 {
			return scan.NewFault(obs.Problems[0].Code, obs.Problems[0])
		}
		return scan.NewFault(platform.CodeObservationFailed, errors.New(
			"collect: the observation is not ready and reports no problem"))
	}
	if obs.Geometry != run.geo {
		return scan.NewFault(platform.CodeGeometryChanged, fmt.Errorf(
			"collect: the observation carries geometry %d; the anchors are bound to %d",
			obs.Geometry, run.geo))
	}
	return nil
}

// dispatchOutcome interprets one dispatch result per the platform
// contract: posted and unaborted partial proceed to effect verification
// — only the page evidence confirms an effect, never the dispatch
// result; notDispatched and failed carry the platform's refusal and
// stop the session; an aborted drag means the input gate closed
// mid-operation — the stop path is already running, so the machine
// unwinds without dispatching anything further (T5: the platform never
// replays).
func (run *inventoryRun) dispatchOutcome(ctx context.Context, res platform.InputResult) error {
	switch res.Status {
	case platform.InputPosted:
		return nil
	case platform.InputPartial:
		if res.Aborted {
			if err := ctx.Err(); err != nil {
				return err
			}
			return scan.NewFault(platform.CodePermitClosed, errors.New(
				"collect: the drag aborted because the input gate closed"))
		}
		// Some events ran; the effect evidence decides.
		return nil
	case platform.InputNotDispatched, platform.InputFailed:
		if res.Denial != nil {
			return scan.NewFault(res.Denial.Code, res.Denial)
		}
		return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
			"collect: dispatch reported %s without a denial", res.Status))
	default:
		return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
			"collect: dispatch reported unknown status %q", res.Status))
	}
}

// geometryChanged stops on a frame that arrived under a different
// geometry version than the anchors are bound to: old anchors must not
// judge new frames (the platform's watcher reports the same change as a
// session fault on its own).
func (run *inventoryRun) geometryChanged(f platform.Frame) error {
	return scan.NewFault(platform.CodeGeometryChanged, fmt.Errorf(
		"collect: frame %d arrived under geometry %d; the anchors are bound to %d",
		f.ID, f.Geometry, run.geo))
}

// admissionEvidence assembles one admission's prose grounds: the page
// pair, the selection association, the stability and the matcher's own
// confidence description (T5 §6).
func (run *inventoryRun) admissionEvidence(target string, preFrame platform.Frame, out awaitOutcome, attempts int) string {
	return fmt.Sprintf(
		"%s admitted after %d attempt(s): list frame %d showed the target occupied; the detail settled on frame %d (geometry %d) with the selection on the target; matcher: %s",
		target, attempts, preFrame.ID, out.frame.ID, out.frame.Geometry, out.match.Evidence)
}

// wrapObservationErr wraps a non-fault frame/observation error as an
// observationFailed fault; faults pass through with their code.
func wrapObservationErr(err error) error {
	var f *platform.Fault
	if errors.As(err, &f) {
		return scan.NewFault(f.Code, f)
	}
	return scan.NewFault(platform.CodeObservationFailed, err)
}

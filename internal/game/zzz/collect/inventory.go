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
// navigation state machine over a calibrated layout and a page matcher.
// The macOS cloud client shows the warehouse as one page — list, detail
// pane and wheel share the frame — so a visit selects a disc, waits for
// the selection and the detail to settle in the same page, captures and
// attributes the detail to the clicked target, admits, and moves on; no
// close step exists. Scrolls advance only on verified position
// correspondences; the walk completes only on independent end evidence.
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
	// Project converts an admitted capture before it reaches the kernel.
	// The traversal cannot import the application's projection (that
	// would cycle collect → app), so the caller injects it. Nil admits
	// the Observation unchanged.
	Project func(Observation) (any, error)

	// ActionBudget bounds one action's whole effect-and-settle wait —
	// from the dispatch to the stable expected verdict. Zero takes the
	// default.
	ActionBudget time.Duration
	// ScrollBudget bounds a scroll's settle wait after the drag. It is
	// longer than ActionBudget because a 1000ms drag plus inertial
	// settling outlasts a click. Zero takes the default. The default is
	// provisional (T5 §5: 拖拽停稳未单独标定).
	ScrollBudget time.Duration
	// StableFrames is the consecutive same-verdict observation count that
	// counts as settled. Zero takes the default.
	StableFrames int
	// DetailStableSpan is the wall-clock span over which the detail pane
	// must stay unchanged after a selection transition (or before an
	// initial read). Wall-clock, not Frame.ArrivedAt: platform idle
	// confirmations (Reused) carry the last fresh frame's arrival time.
	// Zero takes DefaultDetailStableSpan; negative disables the span check
	// (tests use that to keep scripted sequences fast).
	DetailStableSpan time.Duration
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

// Traversal pacing and bound defaults. The detail window's provisional
// value comes from the sample round's click-to-detail timings (change
// starts at 167–212ms, settles at 374–415ms; T5 §4).
const (
	DefaultActionBudget     = 2 * time.Second
	DefaultScrollBudget     = 3 * time.Second
	DefaultStableFrames     = 3
	DefaultDetailStableSpan = 200 * time.Millisecond
	DefaultMaxRetries       = 2
	DefaultRecheckEvery     = 20 * time.Millisecond
)

func (inv *Inventory) actionBudget() time.Duration {
	if inv.ActionBudget > 0 {
		return inv.ActionBudget
	}
	return DefaultActionBudget
}

func (inv *Inventory) scrollBudget() time.Duration {
	if inv.ScrollBudget > 0 {
		return inv.ScrollBudget
	}
	return DefaultScrollBudget
}

func (inv *Inventory) stableFrames() int {
	if inv.StableFrames > 0 {
		return inv.StableFrames
	}
	return DefaultStableFrames
}

func (inv *Inventory) detailStableSpan() time.Duration {
	if inv.DetailStableSpan < 0 {
		return 0
	}
	if inv.DetailStableSpan > 0 {
		return inv.DetailStableSpan
	}
	return DefaultDetailStableSpan
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
	Content image.Rectangle `json:"content"`
	// FrameID, ArrivedAt and Geometry are the capture's provenance:
	// which frame, when it reached the platform, under which geometry.
	FrameID   uint64                   `json:"frameId"`
	ArrivedAt time.Time                `json:"frameReceivedAt"`
	Geometry  platform.GeometryVersion `json:"geometryRevision"`
	// Viewport, Row and Column locate the visit in the traversal
	// ledger's terms (0-based viewport and grid coordinates). Viewport
	// restarts at 0 for every wheel position; Position says which
	// position the visit belongs to.
	Viewport int `json:"viewportIndex"`
	Row      int `json:"row"`
	Column   int `json:"column"`
	// Position is the wheel position (1..6) this visit belongs to.
	Position int `json:"position"`
	// Quality is the calibrated rarity evidence the admission rested on.
	Quality Quality `json:"quality"`
	// Lines are the detail pane's line regions actually present on this
	// disc, in reading order: name, level, main, sub1..sub3 and, only for
	// a four-substat disc, sub4.
	Lines []LineRegion `json:"lines"`
	// Context are the non-recognition regions that establish the
	// admission: the rarity badge and the selected cell's border. The
	// projection writes them as evidence; the recognizer never reads
	// them.
	Context []LineRegion `json:"context"`
	// VisitIndex is this admission's visit number (1-based). The
	// skeleton numbers admitted visits in admission order; the full
	// ledger — numbering every started visit, including unresolved and
	// non-S skips — lands with the ledger persistence work.
	VisitIndex int `json:"visitIndex"`
	// Association says how the detail was attributed to the target:
	// initialVerified when the target already carried the selection
	// border before any click, transitionVerified when the selection
	// moved after a click.
	Association Association `json:"association"`
	// PreClickFrameID is the pre-action list frame the click derived
	// from — the page-pair evidence's other half. It is 0 for an initial
	// read: no click and no pre-click frame exist.
	PreClickFrameID uint64 `json:"preClickFrameId"`
	// Evidence is the admission's grounds in prose: the page pair, the
	// selection association, the stability and the matcher's own
	// confidence description (T5 §6: 每件保留…另附目标格位、对应的导航
	// 步骤与接纳理由).
	Evidence string `json:"evidence"`
}

// scopePayload is the observed-range report Precheck assembles: only the
// facts its evidence actually established. Quantity and the sort order
// stay unverified (sort is recorded as "unverified"); the 4b-2 preparation
// round owns reading them.
type scopePayload struct {
	Kind          string `json:"kind"`
	Page          string `json:"page"`
	Layout        string `json:"layout,omitempty"`
	Geometry      uint64 `json:"geometry"`
	Slots         int    `json:"slots"`
	StartPosition int    `json:"startPosition"`
	FilterAtStart string `json:"filterAtStart"`
	Sort          string `json:"sort"`
}

// Precheck implements Traversal: verify, without dispatching any input,
// the page preconditions T5 §3 demands — the anchors are structurally
// sound and exactly match the captured display configuration, the
// session's baseline geometry is bound to them, and the current frame is
// the warehouse page with a grid-shaped occupancy verdict and a unique
// wheel position. Filter state and list position are not preconditions:
// Collect prepares every position itself (filter reset and top evidence).
// Failures carry anchorsMissing or pageUnverified; the matcher's evidence
// rides along.
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
	if !inv.Layout.MatchesFrame(frame.Image.Bounds().Size(), frame.Content) {
		return scan.NewFault(CodeAnchorsMissing, fmt.Errorf(
			"collect: precheck: the captured configuration (image %v, content %v) is not this layout's (image %v, content %v)",
			boundsOrNil(frame.Image), frame.Content, inv.Layout.Image, inv.Layout.Content))
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
	if m.Wheel == nil || m.Wheel.Position < 1 || m.Wheel.Position > 6 {
		return scan.NewFault(CodePageUnverified, fmt.Errorf(
			"collect: precheck: no unique wheel position is selected: %s", m.Evidence))
	}
	filterAtStart := FilterUnknown
	if m.Filter != nil {
		filterAtStart = m.Filter.State
	}
	scope, err := json.Marshal(scopePayload{
		Kind:          "zzzInventory",
		Page:          string(PageWarehouse),
		Layout:        inv.Layout.Name,
		Geometry:      uint64(geo),
		Slots:         len(inv.Layout.Slots),
		StartPosition: m.Wheel.Position,
		FilterAtStart: string(filterAtStart),
		Sort:          "unverified",
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
		inv:  inv,
		view: view,
		h:    h,
		geo:  inv.boundGeometry,
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
	// position is the wheel position (1..6) currently being traversed.
	position int
	// viewport is the current position's viewport index: 0 after the top
	// confirmation, +1 per verified scroll. It restarts for every wheel
	// position (the scan JSON contract's per-position viewportIndex).
	viewport int
	// visited tracks, per slot of the current viewport in reading order,
	// whether the position was already visited. Overlap positions a
	// verified scroll maps keep their state; genuinely new positions
	// start unvisited. It is cleared for every wheel position.
	visited []bool
	// visitIndex counts admitted visits; the next admission takes
	// visitIndex+1.
	visitIndex int
	// allowInitial is true until the first click of the current wheel
	// position. The initial-read path is only available then: a target
	// that already carries the selection border after a click can only be
	// a transition (or a misdirected click), never a first read.
	allowInitial bool
	// lastScrollRows is the verified row displacement of the newest
	// scroll of this position; 0 when none happened. A displacement below
	// the full step is one of the two independent bottom evidence forms.
	lastScrollRows int
	// listMatch is the current list state: the stable warehouse verdict
	// the next step builds on (its frame is re-taken fresh whenever a
	// click needs pre-action provenance).
	listMatch PageMatch
	// lastFrame is the newest stable warehouse frame observed.
	lastFrame platform.Frame
}

// maxTopFlings bounds one position's automatic return-to-top.
const maxTopFlings = 16

// run is the two-layer collection loop (T5 §1): for each wheel position,
// prepare the position automatically — switch the wheel, reset the
// filters and verify the funnel is clear, then reach and confirm the top —
// and walk its list with the single-page state machine. The next position
// starts only after the current one completed; any preparation or
// traversal failure stops the walk and keeps what was already admitted
// (the user's "execute automatically, stop on failure" decision).
func (run *inventoryRun) run(ctx context.Context) error {
	for pos := 1; pos <= 6; pos++ {
		if err := run.preparePosition(ctx, pos); err != nil {
			return err
		}
		if err := run.collectPosition(ctx); err != nil {
			return err
		}
	}
	return nil
}

// preparePosition resets one wheel position's traversal state and performs
// its automatic preparation. The state reset comes first so a failed
// preparation never inherits another position's bookkeeping.
func (run *inventoryRun) preparePosition(ctx context.Context, pos int) error {
	run.position = pos
	run.viewport = 0
	run.visited = make([]bool, len(run.inv.Layout.Slots))
	run.allowInitial = true
	run.lastScrollRows = 0

	if err := run.establishList(ctx); err != nil {
		return err
	}
	if err := run.switchWheel(ctx, pos); err != nil {
		return err
	}
	if err := run.resetFilters(ctx); err != nil {
		return err
	}
	return run.scrollToTop(ctx)
}

// collectPosition walks the current wheel position's list: visit every
// occupied, unvisited slot of the current viewport in reading order; when
// the viewport is exhausted, complete on independent end evidence or
// advance by one verified scroll (T5 §5). It returns nil only for this
// position's verified end — never because the frames stopped changing.
func (run *inventoryRun) collectPosition(ctx context.Context) error {
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
				// Two independent bottom evidence forms satisfy T5 §5: a
				// final scroll that displaced less than the full step, or
				// one more drag that leaves the list stationary. The
				// coverage conclusion itself is the record layer's to draw.
				if run.lastScrollRows > 0 && run.lastScrollRows < run.inv.Layout.Rows-1 {
					return nil
				}
				return run.confirmBottom(ctx)
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

// switchWheel selects the target wheel position. A click whose effect
// never appears is retried within the bound; a page that becomes
// undecidable is not re-clicked blindly.
func (run *inventoryRun) switchWheel(ctx context.Context, pos int) error {
	if run.listMatch.Wheel == nil {
		return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
			"collect: wheel position %d: the current page has no wheel verdict: %s", pos, run.listMatch.Evidence))
	}
	if run.listMatch.Wheel.Position == pos {
		return nil
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
			Kind:     platform.InputClick,
			Geometry: run.geo,
			FrameID:  run.lastFrame.ID,
			At:       run.inv.Layout.Wheel[pos-1],
		})
		if err := run.dispatchOutcome(ctx, res); err != nil {
			return err
		}
		out := run.awaitSettled(ctx, func(m PageMatch) bool {
			return run.listVerdict(m) && m.Wheel != nil && m.Wheel.Position == pos
		})
		switch out.class {
		case awaitStable:
			run.listMatch = out.match
			run.lastFrame = out.frame
			return nil
		case awaitCanceled, awaitFrameErr:
			return out.err
		case awaitGeometry:
			return run.geometryChanged(out.frame)
		default:
			if run.listVerdict(out.match) && out.match.Wheel != nil && out.match.Wheel.Position != pos {
				if attempt < run.inv.maxRetries() {
					continue
				}
				return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
					"collect: the wheel click never switched to position %d within the budget (still %d, attempt %d): %s",
					pos, out.match.Wheel.Position, attempt+1, out.match.Evidence))
			}
			return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
				"collect: the page became undecidable while switching to wheel position %d (last verdict %s): %s",
				pos, out.match.Page, out.match.Evidence))
		}
	}
}

// resetFilters opens the filter panel, resets it, closes it and requires
// the funnel to be clear. The reset is executed for every wheel position
// because the panel's state persists across positions and sessions; a
// funnel that stays active stops the walk, since proceeding could traverse
// a hidden subset (T5 §3 范围).
func (run *inventoryRun) resetFilters(ctx context.Context) error {
	if err := run.openFilterPanel(ctx); err != nil {
		return err
	}
	if err := run.resetFilterPanel(ctx); err != nil {
		return err
	}
	if err := run.closeFilterPanel(ctx); err != nil {
		return err
	}
	if run.listMatch.Wheel == nil || run.listMatch.Wheel.Position != run.position {
		return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
			"collect: wheel position %d: the wheel moved during the filter reset: %s",
			run.position, run.listMatch.Evidence))
	}
	if run.listMatch.Filter == nil || run.listMatch.Filter.State != FilterClear {
		state := FilterUnknown
		if run.listMatch.Filter != nil {
			state = run.listMatch.Filter.State
		}
		return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
			"collect: wheel position %d: the funnel is %q after the filter reset; refusing to continue while a filter may hide discs",
			run.position, state))
	}
	return nil
}

// openFilterPanel clicks the funnel and waits for the panel page to settle.
func (run *inventoryRun) openFilterPanel(ctx context.Context) error {
	return run.clickUntilPage(ctx, run.inv.Layout.FilterButton, PageFilterPanel, "the filter panel never opened")
}

// resetFilterPanel clicks the panel's reset control and waits for the
// still-open panel to settle.
func (run *inventoryRun) resetFilterPanel(ctx context.Context) error {
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
			FrameID:  run.lastFrame.ID,
			At:       run.inv.Layout.PanelReset,
		})
		if err := run.dispatchOutcome(ctx, res); err != nil {
			return err
		}
		out := run.awaitSettled(ctx, run.panelVerdict)
		switch out.class {
		case awaitStable:
			run.listMatch = out.match
			run.lastFrame = out.frame
			return nil
		case awaitCanceled, awaitFrameErr:
			return out.err
		case awaitGeometry:
			return run.geometryChanged(out.frame)
		default:
			if run.panelVerdict(out.match) {
				if attempt < run.inv.maxRetries() {
					continue
				}
				return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
					"collect: wheel position %d: the filter panel never settled after the reset (attempt %d)", run.position, attempt+1))
			}
			return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
				"collect: wheel position %d: the filter panel did not stay open while resetting (last verdict %s): %s",
				run.position, out.match.Page, out.match.Evidence))
		}
	}
}

// closeFilterPanel clicks the panel's close control and waits for the
// warehouse page to settle.
func (run *inventoryRun) closeFilterPanel(ctx context.Context) error {
	return run.clickUntilPage(ctx, run.inv.Layout.PanelClose, PageWarehouse, "the filter panel never closed")
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
// every click builds its pre-click evidence on. The top evidence is
// established per position by scrollToTop after the filter reset.
func (run *inventoryRun) establishList(ctx context.Context) error {
	out := run.awaitSettled(ctx, run.listVerdict)
	switch out.class {
	case awaitStable:
		run.listMatch = out.match
		run.lastFrame = out.frame
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

// clickUntilPage dispatches one click and waits for the wanted page to
// settle. A click that verifiably did nothing (the source page still
// holds at the deadline) is retried within the bound; an undecidable page
// stops the walk.
func (run *inventoryRun) clickUntilPage(ctx context.Context, at image.Point, want PageKind, what string) error {
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
			FrameID:  run.lastFrame.ID,
			At:       at,
		})
		if err := run.dispatchOutcome(ctx, res); err != nil {
			return err
		}
		out := run.awaitSettled(ctx, run.pageVerdict(want))
		switch out.class {
		case awaitStable:
			run.listMatch = out.match
			run.lastFrame = out.frame
			return nil
		case awaitCanceled, awaitFrameErr:
			return out.err
		case awaitGeometry:
			return run.geometryChanged(out.frame)
		default:
			if run.sourcePageVerdict(out.match, want) {
				if attempt < run.inv.maxRetries() {
					continue
				}
				return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
					"collect: wheel position %d: %s within the budget (attempt %d): %s",
					run.position, what, attempt+1, out.match.Evidence))
			}
			return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
				"collect: wheel position %d: the page became undecidable while waiting for %s (last verdict %s): %s",
				run.position, want, out.match.Page, out.match.Evidence))
		}
	}
}

// pageVerdict is the settled-page predicate for clickUntilPage.
func (run *inventoryRun) pageVerdict(want PageKind) func(PageMatch) bool {
	switch want {
	case PageFilterPanel:
		return run.panelVerdict
	case PageWarehouse:
		return run.listVerdict
	default:
		return func(m PageMatch) bool { return m.Page == want }
	}
}

// sourcePageVerdict reports whether the last verdict is the page the click
// was supposed to leave — the evidence that the click did nothing.
func (run *inventoryRun) sourcePageVerdict(m PageMatch, want PageKind) bool {
	switch want {
	case PageFilterPanel:
		return run.listVerdict(m)
	case PageWarehouse:
		return run.panelVerdict(m)
	default:
		return false
	}
}

// panelVerdict is the structural predicate of a filter-panel verdict.
func (run *inventoryRun) panelVerdict(m PageMatch) bool {
	return m.Page == PageFilterPanel && m.Panel != nil
}

// scrollToTop flings toward the list's beginning until the scrollbar's
// top evidence holds, requires each fling to move the thumb's start
// upward (or, when the matcher cannot report the thumb, to displace the
// list), and stops after two consecutive flings without progress or the
// fling bound. Lost during these flings is allowed: the position has no
// visit records yet, and the next fling re-establishes the base. After
// the top claim, one more fling toward the top must be stationary.
func (run *inventoryRun) scrollToTop(ctx context.Context) error {
	flings := 0
	noProgress := 0
	for {
		if run.listMatch.List != nil && run.listMatch.List.Top {
			break
		}
		if noProgress >= 2 {
			return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
				"collect: wheel position %d: two consecutive flings produced no progress toward the top: %s",
				run.position, run.listMatch.Evidence))
		}
		if flings >= maxTopFlings {
			return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
				"collect: wheel position %d: the top was not reached after %d flings: %s",
				run.position, flings, run.listMatch.Evidence))
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !run.h.InputPermitted() {
			return ctx.Err()
		}
		if err := run.observeReady(ctx); err != nil {
			return err
		}
		beforeStart, beforeOK := run.thumbStart(run.lastFrame)
		res := run.view.Dispatch(platform.InputOp{
			Kind:     platform.InputDrag,
			Geometry: run.geo,
			FrameID:  run.lastFrame.ID,
			At:       run.inv.Layout.TopFrom,
			To:       run.inv.Layout.TopTo,
			Steps:    10,
			Duration: run.inv.Layout.TopDuration,
		})
		if err := run.dispatchOutcome(ctx, res); err != nil {
			return err
		}
		out, err := run.awaitScroll(ctx, run.lastFrame)
		if err != nil {
			return err
		}
		flings++
		progressed := false
		switch {
		case out.lost:
			if !run.listVerdict(out.afterMatch) {
				return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
					"collect: wheel position %d: the top fling lost the list and left no warehouse verdict: %s",
					run.position, out.match.Evidence))
			}
			run.listMatch = out.afterMatch
			run.lastFrame = out.after
			// A fling that overshot the overlap certainly moved; the
			// direction is the fling's own. This is allowed here because
			// no visit exists in this position yet.
			progressed = true
		case out.displaced:
			run.listMatch = out.afterMatch
			run.lastFrame = out.after
			progressed = true
			if beforeOK {
				if afterStart, ok := run.thumbStart(out.after); ok {
					progressed = afterStart < beforeStart
				}
			}
			if run.listMatch.List != nil && run.listMatch.List.Top {
				progressed = true
			}
		case out.stationary:
			run.listMatch = out.afterMatch
			run.lastFrame = out.after
			progressed = out.afterMatch.List != nil && out.afterMatch.List.Top
		default:
			// Undecided: neither progress nor a verdict. The fling bound
			// still caps the attempts.
		}
		if progressed {
			noProgress = 0
		} else {
			noProgress++
		}
	}
	return run.confirmTop(ctx)
}

// thumbStart reads the current frame's scrollbar thumb start through the
// matcher's optional capability. Without it, scrollToTop falls back to the
// displacement verdict as its progress evidence.
func (run *inventoryRun) thumbStart(f platform.Frame) (int, bool) {
	if r, ok := run.inv.matcher().(thumbReporter); ok {
		return r.ThumbStart(f)
	}
	return 0, false
}

// thumbReporter is the matcher's optional scrollbar-position capability.
// The Matcher contract needs no thumb position; the top progress check
// uses it when the calibrated implementation offers it.
type thumbReporter interface {
	ThumbStart(frame platform.Frame) (int, bool)
}

// confirmTop performs the "drag again and nothing changes" top evidence:
// once the scrollbar claims the top, one more fling toward the beginning
// must leave the list stationary (T5 §5).
func (run *inventoryRun) confirmTop(ctx context.Context) error {
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
			FrameID:  run.lastFrame.ID,
			At:       run.inv.Layout.TopFrom,
			To:       run.inv.Layout.TopTo,
			Steps:    10,
			Duration: run.inv.Layout.TopDuration,
		})
		if err := run.dispatchOutcome(ctx, res); err != nil {
			return err
		}
		out, err := run.awaitScroll(ctx, run.lastFrame)
		if err != nil {
			return err
		}
		switch {
		case out.stationary:
			run.listMatch = out.afterMatch
			run.lastFrame = out.after
			return nil
		case out.lost:
			return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
				"collect: wheel position %d: the top confirmation lost the list position: %s",
				run.position, out.match.Evidence))
		case out.displaced:
			return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
				"collect: wheel position %d: the top confirmation moved the list, so the top evidence did not hold: %s",
				run.position, out.match.Evidence))
		default:
			if attempt < run.inv.maxRetries() {
				continue
			}
			return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
				"collect: wheel position %d: the top confirmation produced no verifiable verdict within the budget (attempt %d)",
				run.position, attempt+1))
		}
	}
}

// confirmBottom pairs the scrollbar's bottom evidence with one more drag
// in the advance direction that must leave the list stationary: at the
// list's end the content cannot move further (T5 §5's "再拖一次，前后帧
// 列表区误差 < 4"). The complete walk needs both kinds of evidence.
func (run *inventoryRun) confirmBottom(ctx context.Context) error {
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
			FrameID:  run.lastFrame.ID,
			At:       run.inv.Layout.ScrollFrom,
			To:       run.inv.Layout.ScrollTo,
			Steps:    10,
			Duration: run.inv.Layout.ScrollDuration,
		})
		if err := run.dispatchOutcome(ctx, res); err != nil {
			return err
		}
		out, err := run.awaitScroll(ctx, run.lastFrame)
		if err != nil {
			return err
		}
		switch {
		case out.stationary:
			run.listMatch = out.afterMatch
			run.lastFrame = out.after
			return nil
		case out.lost:
			return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
				"collect: the bottom confirmation could not verify that the list stayed at the end: %s", out.match.Evidence))
		case out.displaced:
			return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
				"collect: the bottom confirmation moved the list, contradicting the scrollbar's bottom evidence: %s", out.match.Evidence))
		default:
			if attempt < run.inv.maxRetries() {
				continue
			}
			return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
				"collect: the bottom confirmation produced no verifiable verdict within the budget (attempt %d)", attempt+1))
		}
	}
}

// listVerdict is the structural predicate of a usable warehouse verdict:
// the warehouse page with a grid-shaped occupancy verdict. A matcher
// reporting another shape is a contract violation, surfaced as a fault
// rather than papered over.
func (run *inventoryRun) listVerdict(m PageMatch) bool {
	return m.Page == PageWarehouse && m.List != nil && len(m.List.Occupied) == len(run.inv.Layout.Slots)
}

// visitSlot processes one occupied, unvisited slot: acquire capacity,
// re-check the premises, verify the pre-click list evidence, click (or
// take the initial read when the target already carries the selection),
// wait for the detail to settle on the target with a content change as the
// association evidence, admit the capture and continue in the same page.
func (run *inventoryRun) visitSlot(ctx context.Context, i int) error {
	row, col := run.inv.Layout.slotPosition(i)
	target := fmt.Sprintf("wheel position %d viewport %d slot %d (row %d, column %d)", run.position, run.viewport, i, row, col)
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
		// 可对应). The frame is re-taken before every click.
		pre := run.awaitSettled(ctx, func(m PageMatch) bool {
			return run.listVerdict(m) && m.List.Occupied[i]
		})
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
		run.listMatch = pre.match
		run.lastFrame = pre.frame
		preSelected := pre.match.List.Selected

		// Initial read: the target already carries the selection border
		// and no click has been sent yet in this position. Clicking an
		// already-selected target cannot produce the content change the
		// transition rule requires, so this is the only path that admits
		// it.
		if preSelected == i {
			if !run.allowInitial {
				slot.Discard()
				return scan.NewFault(CodeDetailUnattributed, fmt.Errorf(
					"collect: %s is already selected after a click was sent earlier in this position; the shown detail cannot be attributed to a transition",
					target))
			}
			out, err := run.awaitDetail(ctx, platform.Frame{}, i, true)
			if err != nil {
				slot.Discard()
				return err
			}
			switch out.class {
			case awaitStable:
				return run.admit(slot, out, pre.frame, i, row, col, AssociationInitial, 0, attempt+1, target)
			case awaitCanceled, awaitFrameErr:
				slot.Discard()
				return out.err
			case awaitGeometry:
				slot.Discard()
				return run.geometryChanged(out.frame)
			default:
				slot.Discard()
				return run.detailFault(out, pre.frame, preSelected, i, true, target)
			}
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
		run.allowInitial = false

		out, err := run.awaitDetail(ctx, pre.frame, i, false)
		if err != nil {
			slot.Discard()
			return err
		}
		switch out.class {
		case awaitStable:
			return run.admit(slot, out, pre.frame, i, row, col, AssociationTransition, pre.frame.ID, attempt+1, target)
		case awaitCanceled, awaitFrameErr:
			slot.Discard()
			return out.err
		case awaitGeometry:
			slot.Discard()
			return run.geometryChanged(out.frame)
		default: // awaitDeadline
			if run.ineffectiveClick(out, pre.frame, preSelected) {
				if attempt < run.inv.maxRetries() {
					// The click verifiably did nothing: re-establish every
					// precondition and re-dispatch within the bound.
					slot.Discard()
					continue
				}
				slot.Discard()
				return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
					"collect: clicking %s never took effect within the budget (attempt %d): %s",
					target, attempt+1, out.match.Evidence))
			}
			slot.Discard()
			return run.detailFault(out, pre.frame, preSelected, i, false, target)
		}
	}
}

// ineffectiveClick reports the deadline case the table calls "选中框没动，
// 详情 Same": the selection is still where it was before the click and the
// detail did not change, so re-clicking cannot skip a position. A page
// that became undecidable is never retried blindly.
func (run *inventoryRun) ineffectiveClick(out detailOutcome, pre platform.Frame, preSelected int) bool {
	if !run.listVerdict(out.match) {
		return false
	}
	if out.lastSelected != preSelected || out.sawDifferent {
		return false
	}
	return run.inv.matcher().CompareDetail(pre, out.frame) == DetailSame
}

// detailFault classifies a detail wait that ended without a stable,
// attributable detail (design §3's deadline table).
func (run *inventoryRun) detailFault(out detailOutcome, pre platform.Frame, preSelected, target int, initial bool, targetName string) error {
	if !run.listVerdict(out.match) {
		return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
			"collect: the page became undecidable while waiting for the detail of %s (last verdict %s): %s",
			targetName, out.match.Page, out.match.Evidence))
	}
	selected := out.match.List.Selected
	if initial {
		switch {
		case selected != target:
			return scan.NewFault(CodeDetailUnattributed, fmt.Errorf(
				"collect: the initially selected target %s lost its selection border while its detail settled (now slot %d)",
				targetName, selected))
		case !out.sawPopulated:
			return scan.NewFault(CodeDetailUnresolved, fmt.Errorf(
				"collect: the detail pane of the initially selected %s never became decidable within the budget: %s",
				targetName, out.match.Evidence))
		default:
			return scan.NewFault(CodeDetailUnattributed, fmt.Errorf(
				"collect: the initial detail of %s never held stable within the budget: %s",
				targetName, out.match.Evidence))
		}
	}
	switch {
	case selected == target && !out.sawPopulated:
		return scan.NewFault(CodeDetailUnresolved, fmt.Errorf(
			"collect: the detail pane of %s never became decidable within the budget: %s",
			targetName, out.match.Evidence))
	case selected == target && !out.sawDifferent:
		return scan.NewFault(CodeDetailUnattributed, fmt.Errorf(
			"collect: the selection reached %s but the detail never changed from the pre-click frame — the same-detail neighbor round-trip verification is not implemented yet (T5 §4): %s",
			targetName, out.match.Evidence))
	case selected == target:
		return scan.NewFault(CodeDetailUnattributed, fmt.Errorf(
			"collect: the detail of %s changed but never held stable for the required window: %s",
			targetName, out.match.Evidence))
	case selected == preSelected:
		return scan.NewFault(CodeDetailUnattributed, fmt.Errorf(
			"collect: the selection's effect from clicking %s could not be verified (still slot %d, detail evidence %v): %s",
			targetName, selected, run.inv.matcher().CompareDetail(pre, out.frame), out.match.Evidence))
	default:
		return scan.NewFault(CodeDetailUnattributed, fmt.Errorf(
			"collect: the selection landed on slot %d instead of %s: %s",
			selected, targetName, out.match.Evidence))
	}
}

// admit checks the admission gates and hands one capture to the kernel:
// the target is selected, the detail changed and held stable, and the pane
// shows a calibrated S disc with 3 or 4 substats. A calibrated NotS disc
// is marked visited and discarded without a record; an undecidable rarity
// or line count stops the walk with detailUnresolved.
func (run *inventoryRun) admit(slot *scan.Slot, out detailOutcome, pre platform.Frame, i, row, col int, assoc Association, preID uint64, attempt int, target string) error {
	if out.match.Detail == nil {
		slot.Discard()
		return scan.NewFault(CodeDetailUnresolved, fmt.Errorf(
			"collect: %s settled without a decidable detail pane: %s", target, out.match.Evidence))
	}
	switch out.match.Detail.Quality {
	case QualityNotS:
		run.visited[i] = true
		run.listMatch = out.match
		run.lastFrame = out.frame
		slot.Discard()
		return nil
	case QualityS:
	default:
		slot.Discard()
		return scan.NewFault(CodeDetailUnresolved, fmt.Errorf(
			"collect: %s settled with an undecidable rarity: %s", target, out.match.Evidence))
	}
	if out.match.Detail.Substats != 3 && out.match.Detail.Substats != 4 {
		slot.Discard()
		return scan.NewFault(CodeDetailUnresolved, fmt.Errorf(
			"collect: %s settled with an undecidable substat row count: %s", target, out.match.Evidence))
	}
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
		Position:        run.position,
		Quality:         out.match.Detail.Quality,
		Lines:           run.inv.Layout.detailLines(out.match.Detail.Substats),
		Context:         run.inv.Layout.contextLines(i),
		VisitIndex:      run.visitIndex,
		Association:     assoc,
		PreClickFrameID: preID,
		Evidence:        run.admissionEvidence(target, pre, out, attempt, assoc),
	}
	payload := any(obs)
	if run.inv.Project != nil {
		projected, err := run.inv.Project(obs)
		if err != nil {
			slot.Discard()
			return err
		}
		payload = projected
	}
	if _, err := slot.Admit(payload); err != nil {
		slot.Discard()
		return fmt.Errorf("collect: admit %s: %w", target, err)
	}
	run.listMatch = out.match
	run.lastFrame = out.frame
	return nil
}

// admissionEvidence assembles one admission's prose grounds: the page
// pair, the selection association, the stability and the matcher's own
// confidence description (T5 §6).
func (run *inventoryRun) admissionEvidence(target string, pre platform.Frame, out detailOutcome, attempts int, assoc Association) string {
	lead := fmt.Sprintf("%s admitted after %d attempt(s): the selection border was already on the target before any click (initial read); the detail held stable on frame %d (geometry %d)",
		target, attempts, out.frame.ID, out.frame.Geometry)
	if assoc == AssociationTransition {
		lead = fmt.Sprintf("%s admitted after %d attempt(s): pre-click list frame %d showed the target occupied; the detail changed after the click and held stable on frame %d (geometry %d)",
			target, attempts, pre.ID, out.frame.ID, out.frame.Geometry)
	}
	stability := ""
	if span := run.inv.detailStableSpan(); span > 0 {
		stability = fmt.Sprintf(" for at least %s", span)
	}
	return fmt.Sprintf("%s%s; matcher: %s", lead, stability, out.match.Evidence)
}

// awaitDetail waits for the detail pane to become attributable to the
// target.
//
// Transition: the frame must show the target selected and a decidable
// detail; at least one observation must differ from the pre-click frame;
// after that, adjacent observations must stay the same for StableFrames
// samples and a wall-clock span of DetailStableSpan. Wall-clock, not
// Frame.ArrivedAt: Reused confirmations carry the last fresh frame's
// arrival time but still prove the content is unchanged now.
//
// Initial: no pre-click frame exists; consecutive observations must stay
// the same over the same window, starting from the first decidable frame.
func (run *inventoryRun) awaitDetail(ctx context.Context, baseline platform.Frame, target int, initial bool) (detailOutcome, error) {
	deadline := time.Now().Add(run.inv.actionBudget())
	span := run.inv.detailStableSpan()
	out := detailOutcome{lastSelected: -2}
	after := run.lastArrived
	var last platform.Frame
	lastSet := false
	runLen := 0
	var runStart time.Time
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
			return out, nil
		}
		if f.Geometry != run.geo {
			out.class, out.frame = awaitGeometry, f
			return out, nil
		}
		m := run.inv.matcher().Match(f)
		out.frame, out.match = f, m
		if !f.Reused {
			run.lastArrived = f.ArrivedAt
			after = f.ArrivedAt
		}
		usable := false
		if run.listVerdict(m) {
			out.lastSelected = m.List.Selected
			if m.List.Selected == target {
				out.sawTarget = true
				if m.Detail != nil {
					out.sawPopulated = true
					usable = true
				}
			}
		}
		if !usable {
			runLen, lastSet = 0, false
		} else if initial {
			if !lastSet {
				runLen, runStart, last, lastSet = 1, time.Now(), f, true
			} else if run.inv.matcher().CompareDetail(last, f) == DetailSame {
				runLen++
				last = f
			} else {
				runLen, runStart, last = 1, time.Now(), f
			}
		} else if !out.sawDifferent {
			if run.inv.matcher().CompareDetail(baseline, f) == DetailDifferent {
				out.sawDifferent = true
				runLen, runStart, last, lastSet = 1, time.Now(), f, true
			} else {
				// Same or undecided: the click may not have taken effect
				// yet. Nothing can be called stable before a change.
				runLen, lastSet = 0, false
			}
		} else if !lastSet {
			runLen, runStart, last, lastSet = 1, time.Now(), f, true
		} else if run.inv.matcher().CompareDetail(last, f) == DetailSame {
			runLen++
			last = f
		} else {
			// Another change or an undecidable comparison: restart the
			// stable run at this frame.
			runLen, runStart, last = 1, time.Now(), f
		}
		if runLen >= run.inv.stableFrames() && (span <= 0 || time.Since(runStart) >= span) {
			out.class = awaitStable
			out.stable = true
			return out, nil
		}
		if f.Reused {
			if err := sleepCtx(ctx, run.inv.recheckEvery()); err != nil {
				out.class, out.err = awaitCanceled, err
				return out, nil
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
			Steps:    10,
			Duration: run.inv.Layout.ScrollDuration,
		})
		if err := run.dispatchOutcome(ctx, res); err != nil {
			return err
		}
		out, err := run.awaitScroll(ctx, before)
		if err != nil {
			return err
		}
		switch {
		case out.lost:
			return scan.NewFault(CodePositionLost, fmt.Errorf(
				"collect: the list displaced but no unique correspondence verified where it went (frame %d): %s",
				out.after.ID, out.match.Evidence))
		case out.displaced:
			if len(out.match.Matches) == 0 {
				return scan.NewFault(CodePositionLost, errors.New(
					"collect: the list displaced but the matcher reported no correspondences"))
			}
			if err := run.applyTransition(before, out.after, run.listMatch, out.afterMatch, out.match); err != nil {
				return err
			}
			run.listMatch = out.afterMatch
			run.lastFrame = out.after
			run.lastScrollRows = out.match.Rows
			return nil
		case out.stationary:
			if attempt < run.inv.maxRetries() {
				continue
			}
			return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
				"collect: the drag never displaced the list within the budget (attempt %d) and no end evidence held", attempt+1))
		default:
			if attempt < run.inv.maxRetries() {
				continue
			}
			return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
				"collect: the scroll drag produced no verifiable verdict within the budget (attempt %d)", attempt+1))
		}
	}
}

// currentList takes the current list frame — the latest content, fresh or
// idle-reused — and requires a usable warehouse verdict on it.
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
	run.lastFrame = f
	return f, nil
}

// scrollOutcome is one awaitScroll's terminal result.
type scrollOutcome struct {
	stationary bool
	displaced  bool
	lost       bool
	after      platform.Frame
	afterMatch PageMatch
	match      ScrollMatch
}

// awaitScroll waits for the scroll's effect on a fresh warehouse frame.
// Only fresh frames count as verdicts: a Reused confirmation carries the
// content the next request already saw, so it cannot prove the drag moved
// the list. Stationary is a deadline verdict, not an early one — the
// drag's effect may still be in flight right after the dispatch.
func (run *inventoryRun) awaitScroll(ctx context.Context, before platform.Frame) (scrollOutcome, error) {
	deadline := time.Now().Add(run.inv.scrollBudget())
	after := run.lastArrived
	out := scrollOutcome{}
	sawDisplaced := false
	sawStationary := false
	for {
		fctx, cancel := context.WithDeadline(ctx, deadline)
		f, err := run.view.Frame(fctx, platform.FrameRequest{After: after})
		cancel()
		if err != nil {
			switch {
			case ctx.Err() != nil:
				return scrollOutcome{}, ctx.Err()
			case errors.Is(err, context.DeadlineExceeded):
				switch {
				case sawDisplaced:
					out.lost = true
					return out, nil
				case sawStationary:
					out.stationary = true
					return out, nil
				default:
					return out, nil
				}
			default:
				return scrollOutcome{}, wrapObservationErr(err)
			}
		}
		if f.Geometry != run.geo {
			return scrollOutcome{}, run.geometryChanged(f)
		}
		m := run.inv.matcher().Match(f)
		if f.Reused {
			// An idle confirmation carries the last fresh content. It can
			// prove "still stationary" (the content is unchanged), but it
			// cannot prove a displacement that has not appeared fresh; the
			// deadline decides.
			if run.listVerdict(m) {
				out.after, out.afterMatch = f, m
				sm := run.inv.matcher().MatchScroll(before, f)
				out.match = sm
				switch {
				case sm.Displaced && len(sm.Matches) > 0:
					out.displaced = true
					return out, nil
				case sm.Lost:
					out.lost = true
					return out, nil
				case sm.Stationary:
					sawStationary = true
				}
			}
			if err := sleepCtx(ctx, run.inv.recheckEvery()); err != nil {
				return scrollOutcome{}, err
			}
			continue
		}
		run.lastArrived = f.ArrivedAt
		after = f.ArrivedAt
		out.after, out.afterMatch = f, m
		if !run.listVerdict(m) {
			// Transition or animation frame: keep observing within the
			// deadline.
			continue
		}
		sm := run.inv.matcher().MatchScroll(before, f)
		out.match = sm
		switch {
		case sm.Displaced && len(sm.Matches) > 0:
			out.displaced = true
			return out, nil
		case sm.Lost:
			out.lost = true
			return out, nil
		case sm.Stationary:
			sawStationary = true
		case sm.Displaced:
			sawDisplaced = true
		}
	}
}

// applyTransition validates one scroll correspondence (T5 §5) and carries
// the visited bookkeeping across it. A correspondence is accepted only
// when it is complete and consistent: every pair in bounds and one-to-one,
// columns preserved, the forward row displacement uniform and equal to the
// matcher's verified row count, and the overlap fully accounted for (an
// unaccounted overlap position could hold an old or a new item — the
// machine does not guess which). Anything else is positionLost: the
// machine stops rather than guessing how far the list went.
func (run *inventoryRun) applyTransition(before, after platform.Frame, beforeMatch, afterMatch PageMatch, sm ScrollMatch) error {
	l := run.inv.Layout
	fail := func(reason string) error {
		return scan.NewFault(CodePositionLost, fmt.Errorf(
			"collect: the scroll transition %d → %d was rejected: %s (correspondence evidence: %s)",
			before.ID, after.ID, reason, sm.Evidence))
	}
	if sm.Rows < 1 || sm.Rows > l.Rows-1 {
		return fail(fmt.Sprintf("the verified row count %d is outside 1..%d", sm.Rows, l.Rows-1))
	}
	if len(sm.Matches) == 0 {
		return fail("no correspondences")
	}
	fromSeen := make(map[int]bool, len(sm.Matches))
	toSeen := make(map[int]bool, len(sm.Matches))
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
		if p.FromRow-p.ToRow != sm.Rows {
			return fail(fmt.Sprintf("the pair (%d,%d)→(%d,%d) disagrees with the verified %d-row displacement",
				p.FromRow, p.FromColumn, p.ToRow, p.ToColumn, sm.Rows))
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
	for row := 0; row < l.Rows-sm.Rows; row++ {
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

// awaitOutcome is one bounded wait's result: the class, the last observed
// frame and verdict (set for every class but awaitCanceled), the error for
// the error classes, and whether any observation carried the expected
// verdict.
type awaitOutcome struct {
	class    awaitClass
	frame    platform.Frame
	match    PageMatch
	err      error
	expected bool
}

// detailOutcome is one detail wait's result. lastSelected is -2 until a
// warehouse list verdict with the layout's slot count is seen.
type detailOutcome struct {
	class        awaitClass
	frame        platform.Frame
	match        PageMatch
	err          error
	sawTarget    bool
	sawPopulated bool
	sawDifferent bool
	lastSelected int
	stable       bool
}

// awaitSettled waits for the expected page verdict to hold stably:
// StableFrames consecutive observations with the verdict, where an
// observation is a fresh frame or an idle (Reused) confirmation of the
// frame the previous observation rested on — unchanged content is still
// current content, so an idle confirmation is stability evidence too. A
// contradicting observation resets the count (a transition frame, an
// animation frame, another page). The whole wait is bounded by the action
// budget; Frame runs under a deadline-bounded context so a silent stream
// cannot outlive the budget.
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

// wrapObservationErr wraps a non-fault frame/observation error as an
// observationFailed fault; faults pass through with their code.
func wrapObservationErr(err error) error {
	var f *platform.Fault
	if errors.As(err, &f) {
		return scan.NewFault(f.Code, f)
	}
	return scan.NewFault(platform.CodeObservationFailed, err)
}

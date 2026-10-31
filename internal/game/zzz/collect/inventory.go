package collect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"log"
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
	// DiagnosticDir, when non-empty, receives failure diagnostics for a
	// scroll wait that stopped the walk: a bounded frame sequence plus a
	// manifest in a timestamped subdirectory, so a field failure can be
	// re-examined against the matcher's thresholds. The artifacts, limits
	// and retention policy are described in docs/development.md's
	// "扫描耗时诊断" section. It is observation only: write failures are
	// logged and swallowed. Empty disables the dumps.
	DiagnosticDir string

	// ActionBudget bounds one action's whole effect-and-settle wait —
	// from the dispatch to the stable expected verdict. Zero takes the
	// default.
	ActionBudget time.Duration
	// ScrollBudget bounds a scroll's settle wait after the drag. It is
	// longer than ActionBudget because a 1200ms drag plus inertial
	// settling outlasts a click. Zero takes the default. The default was
	// raised to 5s after a field scroll displaced four rows but did not
	// settle within the former 3s budget (T5 §5). It must cover the
	// scroll timing profile's MotionStartBudget: F7 measured a drag's
	// motion becoming observable only ~1s after the action, so stillness
	// inside that window is not evidence the drag failed.
	ScrollBudget time.Duration
	// ScrollTiming is the client's scroll timing profile (decision §4.3
	// K2, F7): the settle window measured from the first observed movement
	// and the consecutive settle confirmations required. Nil takes the
	// macOS cloud profile. It only widens the settle wait for a motion
	// observed near the budget's end; it never shortens the ScrollBudget.
	ScrollTiming *ScrollTiming
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
	// scope is the live observed-range report; observedScope is its
	// marshaled form, replaced after every scope change so the controller
	// can relay the final facts after Collect.
	scope         scopePayload
	observedScope json.RawMessage
	// sortChecks and sortVerifiedChecks count every opened filter panel
	// and those whose sort region matched the reference. One mismatch
	// keeps the session's sort unverified: a mid-scan change must not be
	// averaged away.
	sortChecks         int
	sortVerifiedChecks int
}

// Traversal pacing and bound defaults. The detail window's provisional
// value comes from the sample round's click-to-detail timings (change
// starts at 167–212ms, settles at 374–415ms; T5 §4).
const (
	DefaultActionBudget     = 2 * time.Second
	DefaultScrollBudget     = 5 * time.Second
	DefaultStableFrames     = 3
	DefaultDetailStableSpan = 200 * time.Millisecond
	DefaultMaxRetries       = 2
	DefaultRecheckEvery     = 20 * time.Millisecond
)

// Observed-scope vocabulary. Sort stays unverified until every opened
// panel matched the calibrated reference; coverage stays unknown until a
// position completes, becomes partial on a stopped or unverified-sort
// traversal, and becomes complete only for all six verified positions
// with the sort verified.
const (
	scopeSortUnverified   = "unverified"
	scopeSortVerified     = "verified"
	scopeCoverageUnknown  = "unknown"
	scopeCoveragePartial  = "partial"
	scopeCoverageComplete = "complete"
	scopePositionCount    = 6
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

// scrollTiming returns the client's scroll timing profile; nil takes the
// macOS cloud default (decision §4.3 K2, F7). The profile governs when the
// settle window runs and how many consecutive confirmations commit a
// candidate; it never shortens the plain scroll budget.
func (inv *Inventory) scrollTiming() ScrollTiming {
	if inv.ScrollTiming != nil {
		if err := inv.ScrollTiming.validate(); err == nil {
			return *inv.ScrollTiming
		}
	}
	return MacCloudScrollTiming()
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
	// RarityFilter is the rarity filter the position's preparation verified
	// before this capture: "S" when the S-only filter was active on the
	// funnel. It records the observed preparation state, not the declared
	// range (that stays in the session scope's filterAtStart).
	RarityFilter string `json:"rarityFilter,omitempty"`
	// ManagementState is the confirmed management state: "locked",
	// "discarded" or "none". Empty means the list badge and the top
	// indicator were unreadable or disagreed; it must never be read as
	// "none".
	ManagementState string `json:"managementState,omitempty"`
	// ManagementEvidence describes the per-frame list badges and the top
	// indicator the state was confirmed from.
	ManagementEvidence string `json:"managementEvidence,omitempty"`
	// EquippedState is "equipped" or "unequipped" when the detail avatar
	// and the list avatar agree. Empty means the evidence was unreadable
	// or conflicting; it is not unequipped.
	EquippedState string `json:"equippedState,omitempty"`
	// EquippedEvidence describes the detail and list avatar verdicts the
	// presence decision rested on. The equipper's identity is matched from
	// the avatar crops in Context by the application projection; this
	// field stays presence-only.
	EquippedEvidence string `json:"equippedEvidence,omitempty"`
	// ScrollEvidence records the latest verified viewport transition that
	// preceded this admission in the same position, if any, and is empty
	// once a capture has carried it.
	ScrollEvidence string `json:"scrollEvidence,omitempty"`
	// Lines are the detail pane's line regions actually present on this
	// disc, in reading order: name, level, main, sub1..sub3 and, only for
	// a four-substat disc, sub4.
	Lines []LineRegion `json:"lines"`
	// Context are the non-recognition regions that establish the
	// admission: the rarity badge, the selected cell's border and the
	// detail/list equipped-agent avatars. The projection writes them as
	// evidence; the recognizer never reads them. The avatar crops are the
	// identity matcher's input and are saved as evidence files, not
	// exported in the usable data.
	Context []LineRegion `json:"context"`
	// VisitIndex is this admission's visit number (1-based). The
	// skeleton numbers admitted visits in admission order; the full
	// ledger — numbering every started visit, including unresolved and
	// non-S skips — lands with the ledger persistence work.
	VisitIndex int `json:"visitIndex"`
	// Association says how the detail was attributed to the target:
	// initialVerified when the target already carried the selection
	// border before any click, transitionVerified when the selection
	// moved after a click, referenceVerified when a same-detail
	// transition was resolved by one reference round trip.
	Association Association `json:"association"`
	// Reference carries the reference round trip this admission rested
	// on: which visible occupied slot was selected as the distinguishable
	// reference and the stable frame its detail was read from. It is nil
	// for every other association.
	Reference *ReferenceEvidence `json:"reference,omitempty"`
	// PreClickFrameID is the pre-action list frame the click derived
	// from — the page-pair evidence's other half. It is 0 for an initial
	// read: no click and no pre-click frame exist. For a reference
	// admission it is the target's original pre-click frame; the return
	// click's pre-click frame is Reference.FrameID.
	PreClickFrameID uint64 `json:"preClickFrameId"`
	// Evidence is the admission's grounds in prose: the page pair, the
	// selection association, the stability and the matcher's own
	// confidence description (T5 §6: 每件保留…另附目标格位、对应的导航
	// 步骤与接纳理由).
	Evidence string `json:"evidence"`
}

// ReferenceEvidence is the machine-readable half of a
// referenceVerified admission: the distinguishable reference slot, its
// grid position in the viewport and the stable frame that showed its
// distinct detail. That frame is also the return click's pre-click
// frame, so the target's returned detail is tied to the frame that
// showed the reference.
type ReferenceEvidence struct {
	Slot    int    `json:"slot"`
	Row     int    `json:"row"`
	Column  int    `json:"column"`
	FrameID uint64 `json:"frameId"`
}

// scopePayload is the observed-range report the traversal assembles:
// only the facts its evidence actually established. The precheck fills
// the initial facts and writes sort/coverage as unverified/unknown; the
// traversal updates them as panel and position evidence arrives.
type scopePayload struct {
	Kind          string          `json:"kind"`
	Page          string          `json:"page"`
	Layout        string          `json:"layout,omitempty"`
	Geometry      uint64          `json:"geometry"`
	Slots         int             `json:"slots"`
	StartPosition int             `json:"startPosition"`
	FilterAtStart string          `json:"filterAtStart"`
	Sort          string          `json:"sort"`
	SortLabel     string          `json:"sortLabel,omitempty"`
	Coverage      string          `json:"coverage,omitempty"`
	Positions     []positionScope `json:"positions,omitempty"`
}

// positionScope is one wheel position's ledger entry: how many viewports
// it traversed, how many occupied slots were visited, and whether the top
// and bottom evidence completed. Complete is true only when both ends
// verified; a stopped position keeps its partial counts.
type positionScope struct {
	Position  int  `json:"position"`
	Viewports int  `json:"viewports"`
	Visits    int  `json:"visits"`
	Top       bool `json:"topVerified"`
	Bottom    bool `json:"bottomVerified"`
	Complete  bool `json:"complete"`
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
	inv.scope = scopePayload{
		Kind:          "zzzInventory",
		Page:          string(PageWarehouse),
		Layout:        inv.Layout.Name,
		Geometry:      uint64(geo),
		Slots:         len(inv.Layout.Slots),
		StartPosition: m.Wheel.Position,
		FilterAtStart: string(filterAtStart),
		Sort:          scopeSortUnverified,
		Coverage:      scopeCoverageUnknown,
	}
	inv.boundGeometry = geo
	if err := inv.writeScope(); err != nil {
		return fmt.Errorf("collect: precheck: marshal observed scope: %w", err)
	}
	return nil
}

// ObservedScope implements ScopeReporter: the range facts the traversal
// has established so far. Nil before a successful precheck. The report is
// refreshed during Collect, so the controller relays the final facts after
// the traversal ends as well as the precheck facts before it starts.
func (inv *Inventory) ObservedScope() json.RawMessage {
	return append(json.RawMessage(nil), inv.observedScope...)
}

// writeScope replaces the marshaled observed-scope report. It runs on the
// producer goroutine only (Precheck, then Collect); the controller reads
// the result on the same goroutine after each phase.
func (inv *Inventory) writeScope() error {
	raw, err := json.Marshal(inv.scope)
	if err != nil {
		return err
	}
	inv.observedScope = raw
	return nil
}

// observeSort records one opened panel's sort evidence. The session's
// sort is verified only when every checked panel matched the reference;
// the first mismatch sticks, because a changed order mid-scan cannot be
// represented by the calibrated label.
func (inv *Inventory) observeSort(panel *PanelEvidence) {
	if panel == nil {
		return
	}
	inv.sortChecks++
	if panel.SortVerified && panel.SortLabel == sortLabelRarity {
		inv.sortVerifiedChecks++
	}
	inv.scope.Sort = scopeSortUnverified
	inv.scope.SortLabel = ""
	if inv.sortChecks > 0 && inv.sortVerifiedChecks == inv.sortChecks {
		inv.scope.Sort = scopeSortVerified
		inv.scope.SortLabel = sortLabelRarity
	}
	inv.updateCoverage()
	_ = inv.writeScope()
}

// recordPosition appends one wheel position's ledger entry and refreshes
// the coverage conclusion. A position is complete only when its top and
// bottom were verified; the run records an incomplete entry before it
// returns an error so a stopped traversal still reports where it stopped.
func (inv *Inventory) recordPosition(p positionScope) {
	inv.scope.Positions = append(inv.scope.Positions, p)
	inv.updateCoverage()
	_ = inv.writeScope()
}

// updateCoverage derives the conclusion from the ledger and the sort
// evidence. Complete needs all six positions complete and every panel's
// sort verified; anything shorter but non-empty is partial; no completed
// evidence at all stays unknown. Expected count is not inferred here.
func (inv *Inventory) updateCoverage() {
	complete := len(inv.scope.Positions) == scopePositionCount && inv.scope.Sort == scopeSortVerified
	if complete {
		for i, p := range inv.scope.Positions {
			if p.Position != i+1 || !p.Complete || !p.Top || !p.Bottom {
				complete = false
				break
			}
		}
	}
	switch {
	case complete:
		inv.scope.Coverage = scopeCoverageComplete
	case len(inv.scope.Positions) > 0:
		inv.scope.Coverage = scopeCoveragePartial
	default:
		inv.scope.Coverage = scopeCoverageUnknown
	}
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
	// lastArrived is the arrival time of the newest frame observed and the
	// lower bound for the next Frame request. Before a dispatch it is the
	// current content's arrival; after a posted (or unaborted partial)
	// action it is advanced to the action's FinishedAt, so no frame
	// predating the action can be consumed as its effect.
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
	// selectionTracked reports that this position has resolved at least
	// one target, so the next pre-click frame can be checked for an
	// unexpected list shift through the selected cell. A click can make
	// the client scroll the selected item into view after the detail
	// settled; the selection border then moves or disappears, and the
	// next click would silently act on a different grid position.
	selectionTracked bool
	// visitIndex counts admitted visits; the next admission takes
	// visitIndex+1.
	visitIndex int
	// positionVisits counts the current wheel position's resolved targets
	// (admitted S discs and calibrated NotS discs), the ledger's per-
	// position visit count.
	positionVisits int
	// listEstablished and topVerified record how far preparation got; a
	// stopped position's ledger entry must not claim a viewport or a top
	// it never verified.
	listEstablished bool
	topVerified     bool
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
	// lastDetailFrame is the stable frame of the last admitted detail
	// (including a calibrated NotS disc). It is the baseline for the next
	// same-viewport detail comparison; a newly taken pre-click list frame
	// can still show the previous item's stale detail under cloud video
	// lag, which would otherwise make an identical neighbour look changed.
	lastDetailFrame platform.Frame
	// lastScrollEvidence describes the latest verified scroll of this
	// position; the next admitted observation carries it, then it is
	// cleared so one viewport transition is not repeated on every item.
	lastScrollEvidence string
	// positionVisitMs, positionScrollMs, positionBottomMs and
	// positionScrolls are the current position's diagnostic accumulators:
	// the summed per-item totals, the summed scroll-call totals, the
	// bottom confirmation drag, and the count of verified scrolls. They
	// are reset for every wheel position and never feed a navigation
	// decision.
	positionVisitMs  time.Duration
	positionScrollMs time.Duration
	positionBottomMs time.Duration
	positionScrolls  int
	// diagnosticDumps counts this walk's scan-failure diagnostic groups
	// (see dumpScrollTrace); it never feeds a navigation decision.
	diagnosticDumps int
	// diagnosticOKDumps counts this walk's committed-scroll frame-time
	// dumps, under its own (larger) cap: a walk commits many scrolls but
	// only stops on its first failure.
	diagnosticOKDumps int
}

// positionTiming is one wheel position's preparation stage durations for
// the position summary. It is diagnostic only and lives on the producer
// goroutine.
type positionTiming struct {
	establish time.Duration
	wheel     time.Duration
	filter    time.Duration
	top       time.Duration
}

// visitTiming is one visitSlot call's stage durations for the per-item
// diagnostic line. The helpers that perform a stage add their own
// duration; no timing value influences a decision.
type visitTiming struct {
	acquire     time.Duration
	ready       time.Duration
	preClick    time.Duration
	selectWait  time.Duration
	click       time.Duration
	detail      time.Duration
	reference   time.Duration
	project     time.Duration
	admit       time.Duration
	retries     int
	quality     Quality
	association Association
	visitIndex  int
	seq         int
	admitted    bool
	completed   bool
}

// maxTopFlings bounds one position's automatic return-to-top.
const maxTopFlings = 16

// run is the two-layer collection loop (T5 §1): for each wheel position,
// prepare the position automatically — switch the wheel, reset the
// filters, select S and verify the funnel is active, then reach and
// confirm the top —
// and walk its list with the single-page state machine. The next position
// starts only after the current one completed; any preparation or
// traversal failure stops the walk and keeps what was already admitted
// (the user's "execute automatically, stop on failure" decision).
func (run *inventoryRun) run(ctx context.Context) error {
	for pos := 1; pos <= 6; pos++ {
		positionStart := time.Now()
		var timing positionTiming
		if err := run.preparePosition(ctx, pos, &timing); err != nil {
			positionMs := time.Since(positionStart)
			run.inv.recordPosition(run.positionScope(false))
			run.logPositionSummary(timing, 0, positionMs, false)
			return err
		}
		collectStart := time.Now()
		err := run.collectPosition(ctx)
		collectMs := time.Since(collectStart)
		positionMs := time.Since(positionStart)
		run.inv.recordPosition(run.positionScope(err == nil))
		run.logPositionSummary(timing, collectMs, positionMs, err == nil)
		if err != nil {
			return err
		}
	}
	return nil
}

// positionScope snapshots the current position's ledger entry. Viewports
// stays zero when the list was never established; Top and Bottom are only
// true after the corresponding evidence completed.
func (run *inventoryRun) positionScope(complete bool) positionScope {
	viewports := 0
	if run.listEstablished {
		viewports = run.viewport + 1
	}
	return positionScope{
		Position:  run.position,
		Viewports: viewports,
		Visits:    run.positionVisits,
		Top:       run.topVerified,
		Bottom:    complete,
		Complete:  complete,
	}
}

// logVisit writes one resolved visit's fixed-field timing line: every
// accepted S disc and every calibrated non-S skip. The values are
// diagnostic only and never feed a decision.
func (run *inventoryRun) logVisit(i int, timing *visitTiming, total time.Duration) {
	outcome := "skipped"
	if timing.admitted {
		outcome = "admitted"
	}
	quality := "unknown"
	if timing.quality != QualityUnknown {
		quality = string(timing.quality)
	}
	association := "none"
	if timing.admitted {
		association = string(timing.association)
	}
	log.Printf("youkai: collect: item position=%d viewport=%d slot=%d visit_index=%d seq=%d outcome=%s quality=%s association=%s retries=%d acquire_ms=%d ready_ms=%d pre_click_ms=%d select_wait_ms=%d click_ms=%d click_detail_ms=%d reference_ms=%d project_ms=%d admit_ms=%d total_ms=%d",
		run.position, run.viewport, i, timing.visitIndex, timing.seq, outcome, quality, association,
		timing.retries, timing.acquire.Milliseconds(), timing.ready.Milliseconds(),
		timing.preClick.Milliseconds(), timing.selectWait.Milliseconds(), timing.click.Milliseconds(),
		timing.detail.Milliseconds(), timing.reference.Milliseconds(), timing.project.Milliseconds(),
		timing.admit.Milliseconds(), total.Milliseconds())
}

// logPositionSummary writes one wheel position's fixed-field stage totals
// when its preparation and walk ended, successfully or not. visits counts
// every resolved visit, admitted S discs and skipped non-S discs alike.
func (run *inventoryRun) logPositionSummary(timing positionTiming, collect, total time.Duration, complete bool) {
	log.Printf("youkai: collect: position=%d summary complete=%t establish_ms=%d wheel_ms=%d filter_ms=%d top_ms=%d visit_ms=%d scroll_ms=%d bottom_ms=%d collect_ms=%d total_ms=%d visits=%d scrolls=%d",
		run.position, complete, timing.establish.Milliseconds(), timing.wheel.Milliseconds(),
		timing.filter.Milliseconds(), timing.top.Milliseconds(), run.positionVisitMs.Milliseconds(),
		run.positionScrollMs.Milliseconds(), run.positionBottomMs.Milliseconds(),
		collect.Milliseconds(), total.Milliseconds(),
		run.positionVisits, run.positionScrolls)
}

// topStep runs scrollToTop, records the phase's decision span, and only
// then writes any failure dump. Keeping the write outside the span keeps
// top_ms a decision cost, matching scroll_ms and bottom_ms.
func (run *inventoryRun) topStep(ctx context.Context, timing *positionTiming) error {
	started := time.Now()
	frames, err := run.scrollToTop(ctx)
	timing.top = time.Since(started)
	run.dumpFailure(frames)
	return err
}

// preparePosition resets one wheel position's traversal state and performs
// its automatic preparation. The state reset comes first so a failed
// preparation never inherits another position's bookkeeping.
func (run *inventoryRun) preparePosition(ctx context.Context, pos int, timing *positionTiming) error {
	run.position = pos
	run.viewport = 0
	run.visited = make([]bool, len(run.inv.Layout.Slots))
	run.selectionTracked = false
	run.allowInitial = true
	run.positionVisits = 0
	run.listEstablished = false
	run.topVerified = false
	run.lastScrollRows = 0
	run.lastScrollEvidence = ""
	run.lastDetailFrame = platform.Frame{}
	run.positionVisitMs = 0
	run.positionScrollMs = 0
	run.positionBottomMs = 0
	run.positionScrolls = 0

	started := time.Now()
	err := run.establishList(ctx)
	timing.establish = time.Since(started)
	if err != nil {
		return err
	}
	run.listEstablished = true
	started = time.Now()
	err = run.switchWheel(ctx, pos)
	timing.wheel = time.Since(started)
	if err != nil {
		return err
	}
	log.Printf("youkai: collect: position %d: wheel selected", pos)
	started = time.Now()
	err = run.resetFilters(ctx)
	timing.filter = time.Since(started)
	if err != nil {
		return err
	}
	log.Printf("youkai: collect: position %d: filter reset and S selected", pos)
	err = run.topStep(ctx, timing)
	if err != nil {
		return err
	}
	run.topVerified = true
	log.Printf("youkai: collect: position %d prepared: S filter active, top confirmed", pos)
	return nil
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
		if !run.positionListVerdict(run.listMatch) {
			return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
				"collect: wheel position %d: the list no longer shows this position: %s",
				run.position, run.listMatch.Evidence))
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
				return run.confirmBottomStep(ctx)
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

// resetFilters applies the S rarity filter in one panel visit: open, reset,
// select S, close and require the funnel to be active. Reset and S cannot be
// observed separately without closing the panel between them; the user
// decided on 2026-10-25 to keep one visit in every mode. The Active reading
// after closing proves the S filter is in effect, while a reset that failed
// to remove an older filter remains a documented residual risk (T5 §3).
// An unconfirmed S selection stops the walk instead of guessing.
func (run *inventoryRun) resetFilters(ctx context.Context) error {
	if err := run.openFilterPanel(ctx); err != nil {
		return err
	}
	if err := run.resetFilterPanel(ctx); err != nil {
		return err
	}
	if err := run.selectRarityS(ctx); err != nil {
		return err
	}
	if err := run.closeFilterPanel(ctx); err != nil {
		return err
	}
	if err := run.requireWheelPosition("S filter selection"); err != nil {
		return err
	}
	if err := run.requireFilterState(FilterActive, "the S rarity filter selection was not confirmed"); err != nil {
		return err
	}
	return nil
}

// requireFilterState accepts only the exact funnel verdict the preparation
// asked for. Unknown and the opposite state both stop the walk: a missing
// verdict is not evidence that the filters are in the wanted state.
func (run *inventoryRun) requireFilterState(want FilterState, what string) error {
	state := FilterUnknown
	if run.listMatch.Filter != nil {
		state = run.listMatch.Filter.State
	}
	if state == want {
		return nil
	}
	return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
		"collect: wheel position %d: %s; the funnel is %q, not %q — refusing to continue so filters cannot be assumed",
		run.position, what, state, want))
}

// observedRarityFilter names the rarity filter the current warehouse frame
// proves. Only an active funnel after the S selection can claim "S";
// anything else stays empty, so the observation never repeats the declared
// range as if it had been observed.
func (run *inventoryRun) observedRarityFilter() string {
	if run.listMatch.Filter != nil && run.listMatch.Filter.State == FilterActive {
		return "S"
	}
	return ""
}

// requireWheelPosition keeps every close of the filter panel tied to the
// wheel position the position's preparation started on.
func (run *inventoryRun) requireWheelPosition(step string) error {
	if run.listMatch.Wheel != nil && run.listMatch.Wheel.Position == run.position {
		return nil
	}
	return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
		"collect: wheel position %d: the wheel moved during the %s: %s",
		run.position, step, run.listMatch.Evidence))
}

// openFilterPanel clicks the funnel and waits for the panel page to settle.
// The settled panel carries the sort evidence; the traversal records every
// check so a later mismatch cannot be hidden by an earlier match.
func (run *inventoryRun) openFilterPanel(ctx context.Context) error {
	if err := run.clickUntilPage(ctx, run.inv.Layout.FilterButton, PageFilterPanel, "the filter panel never opened"); err != nil {
		return err
	}
	run.inv.observeSort(run.listMatch.Panel)
	return nil
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

// selectRarityS clicks the panel's S rarity control and waits for the
// still-open panel to settle. The selected state itself has no calibrated
// pixel probe yet; the effect is verified after closing through the funnel
// (active) and the first admitted disc's S badge. A panel that unexpectedly
// closes is a failure, not something to click around.
func (run *inventoryRun) selectRarityS(ctx context.Context) error {
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
			At:       run.inv.Layout.PanelS,
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
					"collect: wheel position %d: the filter panel never settled after selecting S (attempt %d)", run.position, attempt+1))
			}
			return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
				"collect: wheel position %d: the filter panel did not stay open while selecting S (last verdict %s): %s",
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
// established per position by scrollToTop after the filter reset. A first
// wait that ends at its deadline is retried once: activating the cloud
// client can restart the video stream, and the first stream frames after
// activation may not yield a decidable page at all. The retry only delays
// a real page fault by one action budget; it never accepts a page that was
// not seen as the warehouse for StableFrames consecutive observations.
func (run *inventoryRun) establishList(ctx context.Context) error {
	for attempt := 0; ; attempt++ {
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
			if attempt == 0 && ctx.Err() == nil {
				log.Printf("youkai: collect: the warehouse list did not settle within %s; retrying once (last verdict %s, %s): %s",
					run.inv.actionBudget(), out.match.Page, frameDiagnostic(out.frame), out.match.Evidence)
				continue
			}
			return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
				"collect: the warehouse list never showed stably within the budget (last verdict %s, %s): %s",
				out.match.Page, frameDiagnostic(out.frame), out.match.Evidence))
		}
	}
}

// frameDiagnostic names the frame a failed wait rested on. The frame ID,
// geometry, bounds and Reused flag are what distinguish a missing/reshaped
// capture from a stable frame whose verdict fell between tiers.
func frameDiagnostic(f platform.Frame) string {
	return fmt.Sprintf("frame %d (geometry %d, image %v, content %v, reused %t)",
		f.ID, f.Geometry, boundsOrNil(f.Image), f.Content, f.Reused)
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
// fling bound. Lost or Undecided during these flings is allowed: the
// position has no visit records yet, but neither proves the destination
// or that the top was reached, so the next fling re-establishes the base
// from the newest verified frame. After a settled top claim, confirmTop
// must prove the list can move no farther; there an Undecided wait stops
// instead of re-dragging.
func (run *inventoryRun) scrollToTop(ctx context.Context) (*failureFrames, error) {
	flings := 0
	noProgress := 0
	topProven := run.listMatch.List != nil && run.listMatch.List.Top
	var lastBefore platform.Frame
	var lastOut scrollOutcome
	haveWait := false
	for {
		if topProven && run.listMatch.List != nil && run.listMatch.List.Top {
			break
		}
		if noProgress >= 2 {
			return topWaitFailure("top", lastBefore, lastOut, haveWait), scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
				"collect: wheel position %d: two consecutive flings produced no progress toward the top: %s",
				run.position, run.listMatch.Evidence))
		}
		if flings >= maxTopFlings {
			return topWaitFailure("top", lastBefore, lastOut, haveWait), scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
				"collect: wheel position %d: the top was not reached after %d flings: %s",
				run.position, flings, run.listMatch.Evidence))
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !run.h.InputPermitted() {
			return nil, ctx.Err()
		}
		if err := run.observeReady(ctx); err != nil {
			return nil, err
		}
		before := run.lastFrame
		beforeStart, beforeOK := run.thumbStart(before)
		res := run.view.Dispatch(platform.InputOp{
			Kind:     platform.InputDrag,
			Geometry: run.geo,
			FrameID:  before.ID,
			At:       run.inv.Layout.TopFrom,
			To:       run.inv.Layout.TopTo,
			Steps:    10,
			Duration: run.inv.Layout.TopDuration,
		})
		if err := run.dispatchOutcome(ctx, res); err != nil {
			return nil, err
		}
		// Release the previous wait's trace before the next wait allocates
		// its own; two capped traces must not be live together.
		lastOut = scrollOutcome{}
		out, err := run.awaitScroll(ctx, before)
		if err != nil {
			return nil, err
		}
		lastBefore, lastOut, haveWait = before, out, true
		afterStart, afterOK := run.thumbStart(out.after)
		log.Printf("youkai: collect: position %d: top fling %d: thumb %d/%t -> %d/%t, stationary=%t displaced=%t lost=%t rows=%d %s",
			run.position, flings+1, beforeStart, beforeOK, afterStart, afterOK,
			out.stationary, out.displaced, out.lost, out.match.Rows,
			scrollMarginFields(run.inv.matcher(), out.match, out.settleMatch))
		flings++
		progressed := false
		topProven = false
		switch {
		case out.lost:
			if !run.listVerdict(out.afterMatch) {
				return waitFailure(waitKind("top", out), before, out), scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
					"collect: wheel position %d: the top fling lost the list and left no warehouse verdict: %s",
					run.position, out.match.Evidence))
			}
			run.listMatch = out.afterMatch
			run.lastFrame = out.after
			// A fling that overshot the overlap certainly moved; the
			// direction is the fling's own. This is allowed here because
			// no visit exists in this position yet. Its final position is
			// unknown, so it cannot prove the top.
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
				topProven = true
			}
		case out.stationary:
			run.listMatch = out.afterMatch
			run.lastFrame = out.after
			progressed = out.afterMatch.List != nil && out.afterMatch.List.Top
			topProven = progressed
		default:
			// Undecided: a fling is an exploratory action toward a hard
			// boundary, so like Lost it keeps moving toward the top
			// instead of stacking a verification action. It still counts
			// as no progress and stays inside the fling and no-progress
			// bounds. Continuing requires the latest observation to be
			// the verifiable warehouse list; a page that left the
			// warehouse or a wait with no frame at all is a page-level
			// failure, as in the Lost branch above.
			if !out.sawFrame || !out.lastFrameIsList {
				evidence := "no frame received"
				if out.sawFrame {
					evidence = out.lastEvidence
					if evidence == "" {
						evidence = "the last frame had no page evidence"
					}
				}
				return waitFailure(waitKind("top", out), before, out), scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
					"collect: wheel position %d: the top fling ended without a verifiable warehouse list: %s",
					run.position, evidence))
			}
			run.listMatch = out.afterMatch
			run.lastFrame = out.after
		}
		if progressed {
			noProgress = 0
		} else {
			noProgress++
		}
	}
	return run.confirmTop(ctx)
}

// scrollOutcomeName names one wait's terminal verdict for logs, attempt
// records and dump kinds.
func scrollOutcomeName(out scrollOutcome) string {
	switch {
	case out.lost:
		return "lost"
	case out.displaced && len(out.match.Matches) == 0:
		return "ambiguous"
	case out.displaced:
		return "displaced"
	case out.stationary:
		return "stationary"
	default:
		return "undecided"
	}
}

// waitKind names a wait-phase failure by the last wait's verdict, so the
// dump directory follows the scroll dump's outcome naming.
func waitKind(prefix string, out scrollOutcome) string {
	return prefix + "-" + scrollOutcomeName(out)
}

// topWaitFailure is the deferred dump for a top-phase failure. A failure
// before the first wait has no frame sequence to report.
func topWaitFailure(prefix string, before platform.Frame, out scrollOutcome, haveWait bool) *failureFrames {
	if !haveWait {
		return nil
	}
	return waitFailure(waitKind(prefix, out), before, out)
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

// awaitTopBase waits until two consecutive fresh observations are
// pixel-stationary before the first top-confirmation drag. The top claim
// itself is already proven by scrollToTop; this wait only keeps a
// still-settling list from wasting a confirmation attempt. A timeout is
// not a failure: confirmTop still validates its own drag, so the walk
// proceeds with the latest verified frame.
func (run *inventoryRun) awaitTopBase(ctx context.Context) error {
	if run.lastFrame.ID == 0 {
		return nil
	}
	deadline := time.Now().Add(run.inv.actionBudget())
	prev := run.lastFrame
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !time.Now().Before(deadline) {
			log.Printf("youkai: collect: position %d: top base did not prove stillness within %s; confirming from frame %d",
				run.position, run.inv.actionBudget(), run.lastFrame.ID)
			return nil
		}
		fctx, cancel := context.WithDeadline(ctx, deadline)
		f, err := run.view.Frame(fctx, platform.FrameRequest{After: run.lastArrived})
		cancel()
		if err != nil {
			switch {
			case ctx.Err() != nil:
				return ctx.Err()
			case errors.Is(err, context.DeadlineExceeded):
				log.Printf("youkai: collect: position %d: no frame for the top base before the budget; confirming from frame %d",
					run.position, run.lastFrame.ID)
				return nil
			default:
				return wrapObservationErr(err)
			}
		}
		if f.Geometry != run.geo {
			return run.geometryChanged(f)
		}
		if !f.Reused {
			run.lastArrived = f.ArrivedAt
		}
		m := run.inv.matcher().Match(f)
		if !run.positionListVerdict(m) {
			if f.Reused {
				if err := sleepCtx(ctx, run.inv.recheckEvery()); err != nil {
					return err
				}
			}
			continue
		}
		run.listMatch = m
		run.lastFrame = f
		if sameFrameContent(prev, f) || run.inv.matcher().MatchScroll(prev, f).Stationary {
			return nil
		}
		prev = f
		if f.Reused {
			if err := sleepCtx(ctx, run.inv.recheckEvery()); err != nil {
				return err
			}
		}
	}
}

// confirmTop performs the "drag again and nothing changes" top evidence:
// once the scrollbar claims the top, further flings toward the beginning
// must leave the list stationary (T5 §5). Lost or moving outcomes are not
// top evidence: they mean the earlier claim was premature or the list was
// still settling, so the loop keeps flinging toward the beginning. An
// undecided wait proves neither movement nor stillness and stops instead
// of stacking another drag. The bounded attempt count stops the walk
// rather than admitting items from an unverified position.
func (run *inventoryRun) confirmTop(ctx context.Context) (*failureFrames, error) {
	// The return-to-top fling can still be settling when the top claim
	// first holds. Confirming immediately then compares the first drag
	// against a moving base frame; every observed miss costs a full
	// ScrollBudget before the next attempt. Wait for two frame
	// observations to be pixel-stationary first. This is only a pacing
	// optimization: if stillness cannot be proven within the action
	// budget, the confirmation below still verifies the drag itself.
	if err := run.awaitTopBase(ctx); err != nil {
		return nil, err
	}
	before := run.lastFrame
	var lastOut scrollOutcome
	haveWait := false
	for attempt := 0; attempt < maxTopFlings; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !run.h.InputPermitted() {
			return nil, ctx.Err()
		}
		if err := run.observeReady(ctx); err != nil {
			return nil, err
		}
		before = run.lastFrame
		beforeStart, beforeOK := run.thumbStart(before)
		res := run.view.Dispatch(platform.InputOp{
			Kind:     platform.InputDrag,
			Geometry: run.geo,
			FrameID:  before.ID,
			At:       run.inv.Layout.TopFrom,
			To:       run.inv.Layout.TopTo,
			Steps:    10,
			Duration: run.inv.Layout.TopDuration,
		})
		if err := run.dispatchOutcome(ctx, res); err != nil {
			return nil, err
		}
		// Release the previous wait's trace before the next wait allocates
		// its own; two capped traces must not be live together.
		lastOut = scrollOutcome{}
		out, err := run.awaitScroll(ctx, before)
		if err != nil {
			return nil, err
		}
		lastOut, haveWait = out, true
		afterStart, afterOK := run.thumbStart(out.after)
		listOK := run.listVerdict(out.afterMatch) && out.afterMatch.List != nil
		log.Printf("youkai: collect: position %d: top confirmation attempt %d: top=%t thumb %d/%t -> %d/%t stationary=%t displaced=%t lost=%t evidence=%s %s",
			run.position, attempt+1,
			listOK && out.afterMatch.List.Top,
			beforeStart, beforeOK, afterStart, afterOK,
			out.stationary, out.displaced, out.lost, out.match.Evidence,
			scrollMarginFields(run.inv.matcher(), out.match, out.settleMatch))
		if !listOK && (out.lost || out.displaced) {
			return waitFailure(waitKind("top", out), before, out), scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
				"collect: wheel position %d: the top confirmation moved the list without a warehouse verdict: %s",
				run.position, out.match.Evidence))
		}
		if listOK {
			run.listMatch = out.afterMatch
			run.lastFrame = out.after
		}
		// The scrollbar is independent of the animated list pixels: a
		// stable thumb at the top plus a settled non-moving outcome proves
		// the drag did not scroll the list even when the pixel comparison
		// is undecided because of live UI animation. Lost or displaced is
		// movement, so it cannot support a top claim.
		if topConfirmed(out, beforeOK, afterOK, beforeStart, afterStart) {
			if out.stationary {
				log.Printf("youkai: collect: position %d: top confirmation accepted: the list stayed still", run.position)
			} else {
				log.Printf("youkai: collect: position %d: top confirmation accepted: the thumb stayed at the top", run.position)
			}
			return nil, nil
		}
		// Lost or displaced means the list moved toward the beginning, so
		// the earlier top claim was premature; another drag is the same
		// upward action from the latest verified frame. An undecided wait
		// proves neither movement nor stillness: another drag would stack
		// an action on an unknown effect (T5 §4).
		if !out.lost && !out.displaced && !out.stationary {
			return waitFailure(waitKind("top", out), before, out), scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
				"collect: wheel position %d: the top confirmation produced no verifiable verdict within the budget (attempt %d); no further drag is sent",
				run.position, attempt+1))
		}
	}
	return topWaitFailure("top", before, lastOut, haveWait), scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
		"collect: wheel position %d: the top confirmation did not settle after %d attempts", run.position, maxTopFlings))
}

// topConfirmed accepts a warehouse frame at the scrollbar's top. A settled
// stationary outcome is direct evidence. An undecided pixel comparison
// with an independent, unchanged thumb start is also safe: live UI
// animation can raise the zero-shift error without moving the list, and a
// real row displacement is not Undecided. A Displaced outcome always
// rejects. A Lost outcome also rejects: under the latest-verdict rule a
// later Undecided comparison clears Lost, so the "transient Lost, final
// comparison Undecided" case arrives as an undecided terminal and is
// accepted by the thumb guard below. The Lost branch keeps its match check
// as a safety net for a lost candidate whose final comparison is undecided.
func topConfirmed(out scrollOutcome, beforeOK, afterOK bool, beforeStart, afterStart int) bool {
	if out.afterMatch.Page != PageWarehouse || out.afterMatch.List == nil || !out.afterMatch.List.Top {
		return false
	}
	if out.displaced {
		return false
	}
	thumbStable := beforeOK && afterOK && absInt(afterStart-beforeStart) <= 2
	if out.lost {
		return out.match.Undecided && thumbStable
	}
	if out.stationary {
		return true
	}
	return thumbStable
}

// failureFrames is a failed step's wait evidence, returned to the caller
// so the diagnostic write happens after the step's timing is recorded. A
// nil value means the failure has no usable after frame.
type failureFrames struct {
	kind         string
	before       platform.Frame
	after        platform.Frame
	trace        *scrollTrace
	candidate    platform.Frame
	hasCandidate bool
	out          scrollOutcome
}

// waitFailure captures one failed wait for a dump. The wait's trace must
// already exist (awaitScroll always builds one).
func waitFailure(kind string, before platform.Frame, out scrollOutcome) *failureFrames {
	return &failureFrames{
		kind:         kind,
		before:       before,
		after:        out.after,
		trace:        out.trace,
		candidate:    out.candidate,
		hasCandidate: out.hasCandidate,
		out:          out,
	}
}

// dumpFailure writes a failed step's deferred dump, if any. Callers invoke
// it after the step's timing has been recorded.
func (run *inventoryRun) dumpFailure(frames *failureFrames) {
	if frames == nil {
		return
	}
	run.dumpScrollTrace(frames.kind, 0, frames.out, frames.before, frames.after, frames.trace, frames.candidate, frames.hasCandidate, false)
}

// confirmBottomStep runs confirmBottom, accumulates its duration, and only
// then writes any failure crops. Keeping the write outside the timed span
// keeps bottom_ms a decision cost, matching scroll_ms.
func (run *inventoryRun) confirmBottomStep(ctx context.Context) error {
	started := diagnosticNow()
	frames, err := run.confirmBottom(ctx)
	run.positionBottomMs += diagnosticNow().Sub(started)
	run.dumpFailure(frames)
	return err
}

// confirmBottom pairs the scrollbar's bottom evidence with one more drag
// in the advance direction that must leave the list stationary: at the
// list's end the content cannot move further (T5 §5's "再拖一次，前后帧
// 列表区误差 < 4"). The complete walk needs both kinds of evidence. A
// terminal Undecided means the drag's effect is unknown, so it stops the
// walk instead of stacking another drag on an unproven result (T5 §4).
func (run *inventoryRun) confirmBottom(ctx context.Context) (*failureFrames, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !run.h.InputPermitted() {
		return nil, ctx.Err()
	}
	if err := run.observeReady(ctx); err != nil {
		return nil, err
	}
	before := run.lastFrame
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
		return nil, err
	}
	out, err := run.awaitScroll(ctx, before)
	if err != nil {
		return nil, err
	}
	log.Printf("youkai: collect: position %d: bottom confirmation attempt 1: stationary=%t displaced=%t lost=%t rows=%d %s",
		run.position, out.stationary, out.displaced, out.lost, out.match.Rows,
		scrollMarginFields(run.inv.matcher(), out.match, out.settleMatch))
	if out.afterMatch.Page == PageWarehouse && !run.positionListVerdict(out.afterMatch) {
		fault := scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
			"collect: wheel position %d: the bottom confirmation left the list at another wheel position: %s",
			run.position, out.afterMatch.Evidence))
		if out.after.ID != 0 && out.after.Image != nil {
			return waitFailure("bottom-wrong-position", before, out), fault
		}
		return nil, fault
	}
	switch {
	case out.stationary:
		run.listMatch = out.afterMatch
		run.lastFrame = out.after
		return nil, nil
	case out.lost:
		fault := scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
			"collect: the bottom confirmation could not verify that the list stayed at the end: %s", out.match.Evidence))
		return waitFailure("bottom-lost", before, out), fault
	case out.displaced:
		fault := scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
			"collect: the bottom confirmation moved the list, contradicting the scrollbar's bottom evidence: %s", out.match.Evidence))
		return waitFailure("bottom-displaced", before, out), fault
	default:
		fault := scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
			"collect: the bottom confirmation produced no verifiable verdict within the budget; no further drag is sent"))
		if out.after.ID != 0 && out.after.Image != nil {
			return waitFailure("bottom-undecided", before, out), fault
		}
		return nil, fault
	}
}

// listVerdict is the structural predicate of a usable warehouse verdict:
// the warehouse page with a grid-shaped occupancy verdict. A matcher
// reporting another shape is a contract violation, surfaced as a fault
// rather than papered over.
func (run *inventoryRun) listVerdict(m PageMatch) bool {
	return m.Page == PageWarehouse && m.List != nil && len(m.List.Occupied) == len(run.inv.Layout.Slots)
}

// positionListVerdict adds the wheel invariant to listVerdict: collection
// is only allowed to act while the frame still shows the wheel position
// this traversal prepared. A wheel change under the same page/geometry
// would otherwise keep the old position's visited map and range.
func (run *inventoryRun) positionListVerdict(m PageMatch) bool {
	return run.listVerdict(m) && m.Wheel != nil && m.Wheel.Position == run.position
}

// selectionMoved reports an unexpected grid shift between the last
// committed list frame and the fresh pre-click frame. It only applies
// after this position resolved a target: the first target may legitimately
// arrive with any selection. A previous frame with no selection (for
// example after the selected item scrolled out of view) has no identity to
// compare and stays unguarded rather than failing a valid walk.
func (run *inventoryRun) selectionMoved(pre PageMatch) bool {
	if !run.selectionTracked || pre.List == nil ||
		run.listMatch.List == nil || run.listMatch.List.Selected < 0 {
		return false
	}
	return pre.List.Selected != run.listMatch.List.Selected
}

// awaitSelectionReturned gives the selected border a bounded chance to
// reappear after a transient animation or a single undecidable frame. It
// still requires the target to stay occupied and the previous selection to
// be stable again; a persistent move or disappearance reaches the deadline
// and remains a position loss.
func (run *inventoryRun) awaitSelectionReturned(ctx context.Context, target, expected int) awaitOutcome {
	return run.awaitSettledSeeded(ctx, func(m PageMatch) bool {
		return run.positionListVerdict(m) && m.List.Occupied[target] && m.List.Selected == expected
	}, nil)
}

// visitSlot processes one occupied, unvisited slot: acquire capacity,
// re-check the premises, verify the pre-click list evidence, click (or
// take the initial read when the target already carries the selection),
// wait for the detail to settle on the target with a content change as the
// association evidence, admit the capture and continue in the same page.
func (run *inventoryRun) visitSlot(ctx context.Context, i int) (err error) {
	row, col := run.inv.Layout.slotPosition(i)
	target := fmt.Sprintf("wheel position %d viewport %d slot %d (row %d, column %d)", run.position, run.viewport, i, row, col)
	timing := &visitTiming{}
	started := time.Now()
	defer func() {
		if err != nil || !timing.completed {
			return
		}
		elapsed := time.Since(started)
		run.positionVisitMs += elapsed
		run.logVisit(i, timing, elapsed)
	}()
	for attempt := 0; ; attempt++ {
		timing.retries = attempt
		if err := ctx.Err(); err != nil {
			return err
		}
		if !run.h.InputPermitted() {
			return ctx.Err()
		}
		// Capacity first (T5 §4: 控制者先取得队列容量), then the platform
		// premises (重查平台条件), then the pre-click evidence.
		acquireStart := time.Now()
		slot := run.h.Acquire(ctx)
		timing.acquire += time.Since(acquireStart)
		if slot == nil {
			return ctx.Err()
		}
		readyStart := time.Now()
		readyErr := run.observeReady(ctx)
		timing.ready += time.Since(readyStart)
		if readyErr != nil {
			slot.Discard()
			return readyErr
		}
		// The pre-click evidence: the list shows stably with the target
		// slot occupied (T5 §4.1: 列表仍属于同一范围，目标格位有物品且位置
		// 可对应). The last admitted frame is already a stable observation
		// made after the previous action, so it can seed this wait; fresh
		// frames still have to confirm the current list before the click.
		preStart := time.Now()
		pre := run.awaitSettledSeeded(ctx, func(m PageMatch) bool {
			return run.positionListVerdict(m) && m.List.Occupied[i]
		}, run.preClickSeed(i))
		timing.preClick += time.Since(preStart)
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
		// A click can make the client scroll the selected item into view
		// after the detail settled. No input since the last admitted item
		// should move the list, so a changed or vanished selection means
		// the next click would address a different grid position. A live
		// cloud stream can also lose the selection border for a frame or
		// two while the UI animates; do not turn that transient miss into
		// a position loss until a bounded fresh observation confirms it.
		if run.selectionMoved(pre.match) {
			expected := run.listMatch.List.Selected
			log.Printf("youkai: collect: %s: selection moved from slot %d to %d between visits (last frame %d reused=%t, pre-click frame %d reused=%t); waiting for slot %d to return before the click (%s)",
				target, expected, pre.match.List.Selected,
				run.lastFrame.ID, run.lastFrame.Reused, pre.frame.ID, pre.frame.Reused, expected, pre.match.Evidence)
			restoreStart := time.Now()
			restored := run.awaitSelectionReturned(ctx, i, expected)
			restoreElapsed := time.Since(restoreStart)
			timing.preClick += restoreElapsed
			timing.selectWait += restoreElapsed
			switch restored.class {
			case awaitStable:
				log.Printf("youkai: collect: %s: selection returned to slot %d on frame %d; continuing with the pre-click evidence",
					target, expected, restored.frame.ID)
				pre = restored
			case awaitCanceled, awaitFrameErr:
				slot.Discard()
				return restored.err
			case awaitGeometry:
				slot.Discard()
				return run.geometryChanged(restored.frame)
			default:
				slot.Discard()
				lastSeen := -2
				if restored.match.List != nil {
					lastSeen = restored.match.List.Selected
				}
				return scan.NewFault(CodePositionLost, fmt.Errorf(
					"collect: the list selection did not return to slot %d (last seen %d) while preparing %s; the grid no longer maps to the visited positions (%s)",
					expected, lastSeen, target, restored.match.Evidence))
			}
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
			detailStart := time.Now()
			out, err := run.awaitDetail(ctx, platform.Frame{}, i, true)
			timing.detail += time.Since(detailStart)
			if err != nil {
				slot.Discard()
				return err
			}
			switch out.class {
			case awaitStable:
				return run.admit(slot, out, pre.frame, pre.match, i, row, col, AssociationInitial, 0, nil, attempt+1, target, timing)
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
		clickStart := time.Now()
		res := run.view.Dispatch(platform.InputOp{
			Kind:     platform.InputClick,
			Geometry: run.geo,
			FrameID:  pre.frame.ID,
			At:       run.inv.Layout.slotCenter(i),
		})
		timing.click += time.Since(clickStart)
		if err := run.dispatchOutcome(ctx, res); err != nil {
			slot.Discard()
			return err
		}
		run.allowInitial = false

		baseline := run.detailBaseline(pre.frame)
		detailStart := time.Now()
		out, err := run.awaitDetail(ctx, baseline, i, false)
		timing.detail += time.Since(detailStart)
		if err != nil {
			slot.Discard()
			return err
		}
		switch out.class {
		case awaitStable:
			return run.admit(slot, out, pre.frame, pre.match, i, row, col, AssociationTransition, pre.frame.ID, nil, attempt+1, target, timing)
		case awaitCanceled, awaitFrameErr:
			slot.Discard()
			return out.err
		case awaitGeometry:
			slot.Discard()
			return run.geometryChanged(out.frame)
		default: // awaitDeadline
			if run.ineffectiveClick(out, baseline, preSelected) {
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
			if run.sameDetailSelection(out, i) {
				// The selection reached the target but the settled detail
				// stayed the same as the comparison baseline. One reference
				// round trip may still prove the target's detail; it is not
				// a retry of the ineffective click above (T5 §4).
				return run.referenceRoundTrip(ctx, slot, out, pre.frame, pre.match, i, preSelected, row, col, attempt, target, timing)
			}
			slot.Discard()
			return run.detailFault(out, baseline, preSelected, i, false, target)
		}
	}
}

// preClickSeed returns the last admitted frame as the first stability
// sample for the next pre-click wait when it still proves the target
// occupied in the same wheel position. No input happened after that frame,
// so it is a valid action-before observation; the wait still requires
// fresh observations to confirm the list is current.
func (run *inventoryRun) preClickSeed(i int) *awaitOutcome {
	if run.lastFrame.ID == 0 || run.lastFrame.Geometry != run.geo {
		return nil
	}
	if !run.listVerdict(run.listMatch) || !run.listMatch.List.Occupied[i] {
		return nil
	}
	if run.listMatch.Wheel == nil || run.listMatch.Wheel.Position != run.position {
		return nil
	}
	return &awaitOutcome{frame: run.lastFrame, match: run.listMatch, expected: true}
}

// detailBaseline chooses the detail comparison frame for the next click.
// The last admitted detail is a known item and is not subject to the
// pre-click list frame's cloud-video lag; fall back to the fresh
// pre-click frame only before the first admission of a wheel position.
func (run *inventoryRun) detailBaseline(pre platform.Frame) platform.Frame {
	if run.lastDetailFrame.ID != 0 {
		return run.lastDetailFrame
	}
	return pre
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

// sameDetailSelection reports the deadline case the attribution rule
// sends to one reference round trip: the selection reached the target and
// the pane settled on content that was not proven different from the
// baseline. That covers a stable equal pane and a stable comparison that
// falls between the calibrated same/different bands; the reference round
// trip is the only honest way to decide the latter. A transient crossfade
// or a never-settled pane is unclear rather than "same", and a click that
// never moved the selection is an ineffective click with its own retry
// path. Sending a verification click while the first click's effect is
// still unclear would violate T5 §4's rule against a next action from an
// unclear result.
func (run *inventoryRun) sameDetailSelection(out detailOutcome, target int) bool {
	return run.listVerdict(out.match) && out.match.List.Selected == target &&
		out.sawPopulated && out.settledNotDifferent
}

// referenceSlot picks the one visible occupied slot used as the
// distinguishable reference for a same-detail target. The target is
// excluded, and so is the slot selected before the target's click: the
// pre-click frame carried exactly the detail the target repeated, so
// clicking that slot back would only repeat the same ambiguity. Among the
// remaining occupied slots the nearest grid neighbor wins (reading order
// breaks ties), keeping the click inside the current viewport. No scroll is
// ever used to find a reference.
func (run *inventoryRun) referenceSlot(m PageMatch, target, previous int) (int, bool) {
	if !run.listVerdict(m) {
		return 0, false
	}
	l := run.inv.Layout
	targetRow, targetColumn := l.slotPosition(target)
	best, bestDistance := -1, 0
	for i, occupied := range m.List.Occupied {
		if !occupied || i == target || i == previous {
			continue
		}
		row, column := l.slotPosition(i)
		distance := absInt(row-targetRow) + absInt(column-targetColumn)
		if best < 0 || distance < bestDistance {
			best, bestDistance = i, distance
		}
	}
	return best, best >= 0
}

// referenceRoundTrip resolves one same-detail target with the single
// round trip T5 §4 allows: click a different visible occupied slot, prove
// its detail differs from the target's and holds stable, then click the
// target back and prove the returned detail differs from the reference and
// holds stable. Success admits the target once, with the reference slot
// and frame as evidence; the reference slot itself is never admitted and
// never consumes a visited position. Any failure — no usable reference, an
// ineffective reference click, the same reference detail, a failed return
// click or an unstable pane — stops the walk and keeps the already
// admitted results. There is exactly one round trip: no retry, no scroll.
func (run *inventoryRun) referenceRoundTrip(ctx context.Context, slot *scan.Slot, targetOut detailOutcome, pre platform.Frame, preMatch PageMatch, target, previous, row, column, attempt int, targetName string, timing *visitTiming) error {
	referenceStart := time.Now()
	if timing == nil {
		timing = &visitTiming{}
	}
	ref, ok := run.referenceSlot(targetOut.match, target, previous)
	if !ok {
		slot.Discard()
		return scan.NewFault(CodeDetailUnattributed, fmt.Errorf(
			"collect: the selection reached %s but its settled detail was not proven different from the comparison baseline, and no other visible occupied slot is available as a distinguishable reference (T5 §4): %s",
			targetName, targetOut.match.Evidence))
	}
	refRow, refColumn := run.inv.Layout.slotPosition(ref)
	log.Printf("youkai: collect: %s: the settled detail was not proven different after the click; trying one reference round trip via slot %d (row %d, column %d)",
		targetName, ref, refRow, refColumn)

	// The reference click needs its own stable pre-click frame with the
	// target still selected; this only re-observes, it never scrolls.
	preRef := run.awaitSettledSeeded(ctx, func(m PageMatch) bool {
		return run.listVerdict(m) && m.List.Selected == target && m.List.Occupied[ref] && m.Detail != nil
	}, &awaitOutcome{frame: targetOut.frame, match: targetOut.match, expected: true})
	switch preRef.class {
	case awaitStable:
	case awaitCanceled, awaitFrameErr:
		slot.Discard()
		return preRef.err
	case awaitGeometry:
		slot.Discard()
		return run.geometryChanged(preRef.frame)
	default:
		slot.Discard()
		return scan.NewFault(CodeDetailUnattributed, fmt.Errorf(
			"collect: the same-detail reference round trip for %s could not establish a stable pre-reference frame with the target selected and reference slot %d occupied: %s",
			targetName, ref, preRef.match.Evidence))
	}
	run.listMatch = preRef.match
	run.lastFrame = preRef.frame
	if got := run.inv.matcher().CompareDetail(targetOut.frame, preRef.frame); got != DetailSame {
		slot.Discard()
		return scan.NewFault(CodeDetailUnattributed, fmt.Errorf(
			"collect: the target %s's detail changed while preparing the reference (frames %d -> %d compare %v); one round trip can no longer attribute the click",
			targetName, targetOut.frame.ID, preRef.frame.ID, got))
	}

	if err := run.observeReady(ctx); err != nil {
		slot.Discard()
		return err
	}
	res := run.view.Dispatch(platform.InputOp{
		Kind:     platform.InputClick,
		Geometry: run.geo,
		FrameID:  preRef.frame.ID,
		At:       run.inv.Layout.slotCenter(ref),
	})
	if err := run.dispatchOutcome(ctx, res); err != nil {
		slot.Discard()
		return err
	}
	refOut, err := run.awaitDetail(ctx, preRef.frame, ref, false)
	if err != nil {
		slot.Discard()
		return err
	}
	switch refOut.class {
	case awaitStable:
	case awaitCanceled, awaitFrameErr:
		slot.Discard()
		return refOut.err
	case awaitGeometry:
		slot.Discard()
		return run.geometryChanged(refOut.frame)
	default:
		slot.Discard()
		return run.referenceFault("the reference click", refOut, targetName, ref)
	}
	run.listMatch = refOut.match
	run.lastFrame = refOut.frame
	log.Printf("youkai: collect: %s: reference slot %d selected, its detail differs from the target's and held stable on frame %d",
		targetName, ref, refOut.frame.ID)

	if err := run.observeReady(ctx); err != nil {
		slot.Discard()
		return err
	}
	res = run.view.Dispatch(platform.InputOp{
		Kind:     platform.InputClick,
		Geometry: run.geo,
		FrameID:  refOut.frame.ID,
		At:       run.inv.Layout.slotCenter(target),
	})
	if err := run.dispatchOutcome(ctx, res); err != nil {
		slot.Discard()
		return err
	}
	backOut, err := run.awaitDetail(ctx, refOut.frame, target, false)
	if err != nil {
		slot.Discard()
		return err
	}
	switch backOut.class {
	case awaitStable:
		log.Printf("youkai: collect: %s: return click selected the target and its detail differs from reference frame %d, stable on frame %d",
			targetName, refOut.frame.ID, backOut.frame.ID)
		timing.reference += time.Since(referenceStart)
		return run.admit(slot, backOut, pre, preMatch, target, row, column, AssociationReference, pre.ID,
			&referenceCapture{slot: ref, row: refRow, column: refColumn, frame: refOut.frame}, attempt+1, targetName, timing)
	case awaitCanceled, awaitFrameErr:
		slot.Discard()
		return backOut.err
	case awaitGeometry:
		slot.Discard()
		return run.geometryChanged(backOut.frame)
	default:
		slot.Discard()
		return run.referenceFault("the return click on the target", backOut, targetName, ref)
	}
}

// referenceFault classifies a failed leg of the reference round trip. A
// page that stopped being a warehouse verdict is a navigation fault; every
// other outcome is unattributed, because the target's detail was never
// proven distinct from the reference with a stable pair.
func (run *inventoryRun) referenceFault(step string, out detailOutcome, target string, ref int) error {
	if !run.listVerdict(out.match) {
		return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
			"collect: the page became undecidable during %s of the same-detail reference round trip for %s (last verdict %s): %s",
			step, target, out.match.Page, out.match.Evidence))
	}
	return scan.NewFault(CodeDetailUnattributed, fmt.Errorf(
		"collect: the same-detail reference round trip for %s failed at %s (reference slot %d, last selection %d, detail decidable %t, detail changed %t): %s",
		target, step, ref, out.lastSelected, out.sawPopulated, out.sawDifferent, out.match.Evidence))
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
			"collect: the selection reached %s but the detail never changed from its comparison baseline and no reference round trip resolved it (T5 §4): %s",
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

// referenceCapture is one successful reference round trip's provenance
// before it becomes the Observation's public ReferenceEvidence.
type referenceCapture struct {
	slot, row, column int
	frame             platform.Frame
}

// mergeManagement unions per-frame badge verdicts. The lock flashes, so a
// single frame may miss its annulus; a positive lock in any frame wins
// over none or unknown, and a red trash glyph in any frame wins over none.
// Locked and discarded together are contradictory and stay unknown.
func mergeManagement(states ...ManagementState) ManagementState {
	locked, discarded, none, unknown := false, false, false, false
	for _, state := range states {
		switch state {
		case ManagementLocked:
			locked = true
		case ManagementDiscarded:
			discarded = true
		case ManagementNone:
			none = true
		default:
			unknown = true
		}
	}
	switch {
	case locked && discarded:
		return ManagementUnknown
	case locked:
		return ManagementLocked
	case discarded:
		return ManagementDiscarded
	case unknown:
		return ManagementUnknown
	case none:
		return ManagementNone
	default:
		return ManagementUnknown
	}
}

// managementVerdict combines the list badges from the target's pre-click
// and final frames with the detail page's top indicator. The list badge is
// primary: locked must be confirmed by a filled top indicator, and
// discarded/none by an open one. A missing, unreadable or disagreeing
// source yields an empty observation state (JSON null), never none.
func managementVerdict(pre, out PageMatch, slot int) (string, string) {
	var states []ManagementState
	for _, match := range []PageMatch{pre, out} {
		if match.List != nil && slot >= 0 && slot < len(match.List.Management) {
			states = append(states, match.List.Management[slot])
		}
	}
	listState := mergeManagement(states...)
	topState := TopManagementUnknown
	if out.List != nil {
		topState = out.List.TopManagement
	}
	evidence := fmt.Sprintf("list badge %s across %d frame(s), top indicator %s",
		managementStateName(listState), len(states), topManagementName(topState))
	if listState == ManagementUnknown {
		return "", evidence
	}
	switch listState {
	case ManagementLocked:
		if topState != TopManagementLocked {
			return "", evidence + ": the top indicator does not confirm locked"
		}
	case ManagementDiscarded, ManagementNone:
		if topState != TopManagementUnlocked {
			return "", evidence + ": the top indicator does not confirm an unlocked state"
		}
	default:
		return "", evidence
	}
	return string(listState), evidence
}

// mergeAvatar unions per-frame avatar verdicts. A positive avatar wins
// over an unreadable frame, but present and absent together are
// contradictory and stay unknown.
func mergeAvatar(states ...AvatarState) AvatarState {
	present, absent, unknown := false, false, false
	for _, state := range states {
		switch state {
		case AvatarPresent:
			present = true
		case AvatarAbsent:
			absent = true
		default:
			unknown = true
		}
	}
	switch {
	case present && absent:
		return AvatarUnknown
	case present:
		return AvatarPresent
	case unknown:
		return AvatarUnknown
	case absent:
		return AvatarAbsent
	default:
		return AvatarUnknown
	}
}

// equippedVerdict combines the detail avatar (primary) with the target
// cell's top-right avatar (confirmation). Only present/present becomes
// equipped and absent/absent unequipped; a conflict or an unreadable
// source yields an empty observation state (JSON null). Agent identity is
// not read here (PR 6b).
func equippedVerdict(pre, out PageMatch, slot int) (string, string) {
	detail := AvatarUnknown
	if out.Detail != nil {
		detail = out.Detail.Avatar
	}
	var states []AvatarState
	for _, match := range []PageMatch{pre, out} {
		if match.List != nil && slot >= 0 && slot < len(match.List.Avatars) {
			states = append(states, match.List.Avatars[slot])
		}
	}
	list := mergeAvatar(states...)
	evidence := fmt.Sprintf("detail avatar %s, list avatar %s across %d frame(s)",
		avatarName(detail), avatarName(list), len(states))
	switch {
	case detail == AvatarPresent && list == AvatarPresent:
		return EquippedStateEquipped, evidence
	case detail == AvatarAbsent && list == AvatarAbsent:
		return EquippedStateUnequipped, evidence
	default:
		return "", evidence + ": the detail and list evidence do not agree on presence"
	}
}

func managementStateName(state ManagementState) string {
	if state == ManagementUnknown {
		return "unknown"
	}
	return string(state)
}

func topManagementName(state TopManagementState) string {
	if state == TopManagementUnknown {
		return "unknown"
	}
	return string(state)
}

func avatarName(state AvatarState) string {
	if state == AvatarUnknown {
		return "unknown"
	}
	return string(state)
}

// admit checks the admission gates and hands one capture to the kernel:
// the target is selected, the detail changed and held stable, and the pane
// shows a calibrated S disc with 3 or 4 substats. A calibrated NotS disc
// is marked visited and discarded without a record; an undecidable rarity
// or line count stops the walk with detailUnresolved. ref is non-nil only
// for a referenceVerified admission.
func (run *inventoryRun) admit(slot *scan.Slot, out detailOutcome, pre platform.Frame, preMatch PageMatch, i, row, col int, assoc Association, preID uint64, ref *referenceCapture, attempt int, target string, timing *visitTiming) error {
	if out.match.Detail == nil {
		slot.Discard()
		return scan.NewFault(CodeDetailUnresolved, fmt.Errorf(
			"collect: %s settled without a decidable detail pane: %s", target, out.match.Evidence))
	}
	if timing == nil {
		timing = &visitTiming{}
	}
	timing.quality = out.match.Detail.Quality
	timing.association = assoc
	switch out.match.Detail.Quality {
	case QualityNotS:
		run.positionVisits++
		run.visited[i] = true
		run.selectionTracked = true
		run.listMatch = out.match
		run.lastFrame = out.frame
		run.lastDetailFrame = out.frame
		slot.Discard()
		timing.completed = true
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
	run.positionVisits++
	run.visited[i] = true
	managementState, managementEvidence := managementVerdict(preMatch, out.match, i)
	equippedState, equippedEvidence := equippedVerdict(preMatch, out.match, i)
	obs := Observation{
		Image:              out.frame.Image,
		Content:            out.frame.Content,
		FrameID:            out.frame.ID,
		ArrivedAt:          out.frame.ArrivedAt,
		Geometry:           out.frame.Geometry,
		Viewport:           run.viewport,
		Row:                row,
		Column:             col,
		Position:           run.position,
		Quality:            out.match.Detail.Quality,
		RarityFilter:       run.observedRarityFilter(),
		ScrollEvidence:     run.lastScrollEvidence,
		ManagementState:    managementState,
		ManagementEvidence: managementEvidence,
		EquippedState:      equippedState,
		EquippedEvidence:   equippedEvidence,
		Lines:              run.inv.Layout.detailLines(out.match.Detail.Substats),
		Context:            run.inv.Layout.contextLines(i),
		VisitIndex:         run.visitIndex,
		Association:        assoc,
		PreClickFrameID:    preID,
		Evidence:           run.admissionEvidence(target, pre, out, attempt, assoc, ref),
	}
	if ref != nil {
		obs.Reference = &ReferenceEvidence{
			Slot:    ref.slot,
			Row:     ref.row,
			Column:  ref.column,
			FrameID: ref.frame.ID,
		}
	}
	payload := any(obs)
	if run.inv.Project != nil {
		projectStart := time.Now()
		projected, err := run.inv.Project(obs)
		timing.project += time.Since(projectStart)
		if err != nil {
			slot.Discard()
			return scan.NewFault(CodeEvidenceFailed, fmt.Errorf(
				"collect: project %s: %w", target, err))
		}
		payload = projected
	}
	admitStart := time.Now()
	seq, admitErr := slot.Admit(payload)
	timing.admit += time.Since(admitStart)
	if admitErr != nil {
		slot.Discard()
		return scan.NewFault(CodeEvidenceFailed, fmt.Errorf(
			"collect: admit %s: %w", target, admitErr))
	}
	timing.admitted = true
	timing.visitIndex = run.visitIndex
	timing.seq = int(seq)
	timing.completed = true
	run.listMatch = out.match
	run.lastFrame = out.frame
	run.lastDetailFrame = out.frame
	run.selectionTracked = true
	run.lastScrollEvidence = ""
	return nil
}

// admissionEvidence assembles one admission's prose grounds: the page
// pair, the selection association, the stability and the matcher's own
// confidence description (T5 §6).
func (run *inventoryRun) admissionEvidence(target string, pre platform.Frame, out detailOutcome, attempts int, assoc Association, ref *referenceCapture) string {
	lead := fmt.Sprintf("%s admitted after %d attempt(s): the selection border was already on the target before any click (initial read); the detail held stable on frame %d (geometry %d)",
		target, attempts, out.frame.ID, out.frame.Geometry)
	switch assoc {
	case AssociationTransition:
		lead = fmt.Sprintf("%s admitted after %d attempt(s): pre-click list frame %d showed the target occupied; the detail changed after the click and held stable on frame %d (geometry %d)",
			target, attempts, pre.ID, out.frame.ID, out.frame.Geometry)
	case AssociationReference:
		if ref == nil {
			// admit enforces the pairing; this is only a defensive fallback
			// so evidence never silently loses its provenance.
			lead = fmt.Sprintf("%s admitted after %d attempt(s): a same-detail reference round trip resolved the target; the returned detail held stable on frame %d (geometry %d)",
				target, attempts, out.frame.ID, out.frame.Geometry)
			break
		}
		lead = fmt.Sprintf("%s admitted after %d attempt(s): pre-click frame %d showed the target occupied but the settled detail was not proven different from the comparison baseline; one reference round trip selected reference slot %d (row %d, column %d), whose detail differed and held stable on frame %d, then the return click on the target showed a detail different from that reference and stable on frame %d (geometry %d)",
			target, attempts, pre.ID, ref.slot, ref.row, ref.column, ref.frame.ID, out.frame.ID, out.frame.Geometry)
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
// detail; adjacent observations must settle on the same content for
// StableFrames samples and a wall-clock span of DetailStableSpan, and that
// settled pane is then compared with the baseline. A settled pane that
// differs is a transition; one that is equal or falls between the
// calibrated same/different bands at the deadline is the same-detail case
// the reference round trip resolves. A transient animation frame never
// decides the comparison. Wall-clock, not
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
		// A live stream must not extend the detail wait past its budget.
		if err := ctx.Err(); err != nil {
			out.class, out.err = awaitCanceled, err
			return out, nil
		}
		if !time.Now().Before(deadline) {
			out.class = awaitDeadline
			return out, nil
		}
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
			out.settledNotDifferent = false
		} else {
			// Settle on the frame sequence itself first. A click can pass
			// through a transient crossfade or dim frame that differs from
			// the baseline; treating that frame as the transition would
			// hide an identical neighbour and skip the reference round
			// trip. Only the stable pane is compared with the baseline.
			cmp := DetailSame
			if !initial {
				cmp = run.inv.matcher().CompareDetail(baseline, f)
				if cmp == DetailDifferent {
					// Keep the any-frame evidence for the ineffective-click
					// gate, which must not retry after a visible change.
					out.sawDifferent = true
				}
			}
			if !lastSet {
				runLen, runStart, last, lastSet = 1, time.Now(), f, true
				out.settledNotDifferent = false
			} else if run.inv.matcher().CompareDetail(last, f) == DetailSame {
				runLen++
				last = f
			} else {
				// Another change or an undecidable comparison: restart the
				// stable run at this frame.
				runLen, runStart, last = 1, time.Now(), f
				out.settledNotDifferent = false
			}
			if runLen >= run.inv.stableFrames() && (span <= 0 || time.Since(runStart) >= span) {
				switch {
				case initial || cmp == DetailDifferent:
					out.class = awaitStable
					out.stable = true
					return out, nil
				case cmp == DetailSame || cmp == DetailUndecided:
					// The settled pane is not proven different from the
					// baseline. Keep waiting for the budget: a delayed
					// detail update can still replace this pane. Content
					// that remains unchanged or ambiguous at the deadline
					// goes to the reference round trip instead of being
					// admitted as a transition.
					out.settledNotDifferent = true
				default:
					out.settledNotDifferent = false
				}
			}
		}
		if f.Reused {
			if err := sleepCtx(ctx, run.inv.recheckEvery()); err != nil {
				out.class, out.err = awaitCanceled, err
				return out, nil
			}
		}
	}
}

// scrollAttempt retains one drag attempt's evidence until the whole scroll
// action ends. A failed scroll writes every attempt after its timings are
// recorded; a retry is only possible when the earlier attempt ended
// stationary and unmoved. At most MaxRetries+1 traces are live at once, and
// each trace keeps its own frame and byte caps.
type scrollAttempt struct {
	number  int
	before  platform.Frame
	outcome string
	out     scrollOutcome
}

// scroll advances the list by one small drag step and verifies the
// transition by correspondence (T5 §5): the pages before and after are
// the same warehouse, so only the matcher's overlap correspondences
// prove where the list went — a stable warehouse verdict alone proves
// nothing.
func (run *inventoryRun) scroll(ctx context.Context) (err error) {
	before, err := run.currentList(ctx)
	if err != nil {
		return err
	}
	started := time.Now()
	fromViewport := run.viewport
	var drag, settle time.Duration
	attempts := 0
	rows := 0
	var afterID uint64
	var waitOut scrollOutcome
	var attemptLog []scrollAttempt
	outcome := "error"
	defer func() {
		if attempts == 0 {
			return
		}
		// Time and log first: the dump below encodes and writes PNGs, and
		// that cost must not inflate total_ms or the position's scroll_ms.
		elapsed := time.Since(started)
		run.positionScrollMs += elapsed
		log.Printf("youkai: collect: scroll position=%d viewport_from=%d viewport_to=%d outcome=%s attempts=%d drag_ms=%d settle_ms=%d total_ms=%d rows=%d before_frame=%d after_frame=%d %s %s",
			run.position, fromViewport, run.viewport, outcome, attempts,
			drag.Milliseconds(), settle.Milliseconds(), elapsed.Milliseconds(),
			rows, before.ID, afterID,
			scrollTimingFields(waitOut), scrollMarginFields(run.inv.matcher(), waitOut.match, waitOut.settleMatch))
		if err == nil && outcome == "displaced" {
			// A committed scroll also lands its frame time series (decision
			// §4.3: stage 2 records precise frame times through the
			// application diagnostics): the manifest keeps every retained
			// frame's verdicts and arrival times, with PNG crops only for
			// the key frames, so a successful wait stays replayable without
			// paying the failure dumps' image volume.
			for _, a := range attemptLog {
				if a.out.after.ID == 0 || a.outcome != "displaced" {
					continue
				}
				run.dumpScrollTrace("scroll-ok", a.number, a.out, a.before, a.out.after, a.out.trace, a.out.candidate, a.out.hasCandidate, true)
			}
			return
		}
		if err != nil && outcome != "error" {
			// Every attempt keeps its own before frame, trace and
			// verdict; the dumps are written after scroll_ms and the
			// position total are recorded above.
			for _, a := range attemptLog {
				if a.out.after.ID == 0 {
					continue
				}
				run.dumpScrollTrace("scroll-"+a.outcome, a.number, a.out, a.before, a.out.after, a.out.trace, a.out.candidate, a.out.hasCandidate, false)
			}
		}
	}()
	for attempt := 0; ; attempt++ {
		// A retry starts a fresh judgement: a failure before this attempt
		// produces a verdict (focus loss, geometry change, cancellation)
		// leaves the action's outcome as an error, so no dump is
		// attributed to this attempt. Earlier completed attempts keep
		// their own evidence in attemptLog.
		afterID = 0
		waitOut = scrollOutcome{}
		outcome = "error"
		if err := ctx.Err(); err != nil {
			return err
		}
		if !run.h.InputPermitted() {
			return ctx.Err()
		}
		if err := run.observeReady(ctx); err != nil {
			return err
		}
		dispatchStart := time.Now()
		res := run.view.Dispatch(platform.InputOp{
			Kind:     platform.InputDrag,
			Geometry: run.geo,
			FrameID:  before.ID,
			At:       run.inv.Layout.ScrollFrom,
			To:       run.inv.Layout.ScrollTo,
			Steps:    10,
			Duration: run.inv.Layout.ScrollDuration,
		})
		drag += time.Since(dispatchStart)
		attempts++
		if err := run.dispatchOutcome(ctx, res); err != nil {
			return err
		}
		settleStart := time.Now()
		out, err := run.awaitScroll(ctx, before)
		settle += time.Since(settleStart)
		if err != nil {
			return err
		}
		waitOut = out
		afterID = out.after.ID
		outcome = scrollOutcomeName(out)
		if out.afterMatch.Page == PageWarehouse && !run.positionListVerdict(out.afterMatch) {
			outcome = "wrong-position"
		}
		attemptLog = append(attemptLog, scrollAttempt{number: attempts, before: before, outcome: outcome, out: out})
		log.Printf("youkai: collect: scroll position=%d viewport_from=%d attempt=%d before_frame=%d after_frame=%d outcome=%s unmoved=%t rows=%d %s",
			run.position, fromViewport, attempts, before.ID, out.after.ID, outcome, out.unmoved, out.match.Rows,
			scrollMarginFields(run.inv.matcher(), out.match, out.settleMatch))
		if outcome == "wrong-position" {
			return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
				"collect: wheel position %d: the scroll left the list at another wheel position: %s",
				run.position, out.afterMatch.Evidence))
		}
		switch {
		case out.lost:
			log.Printf("youkai: collect: position %d scroll %d -> %d lost: unsettled=%t after=%d rows=%d matches=%d evidence=%s",
				run.position, before.ID, out.after.ID, out.unsettled, out.after.ID,
				out.match.Rows, len(out.match.Matches), out.match.Evidence)
			if out.unsettled {
				return scan.NewFault(CodePositionLost, fmt.Errorf(
					"collect: the list displaced but did not settle within the scroll budget, so its final position is unverified (last candidate frame %d): %s",
					out.after.ID, out.match.Evidence))
			}
			return scan.NewFault(CodePositionLost, fmt.Errorf(
				"collect: the list displaced but no unique correspondence verified where it went (frame %d): %s",
				out.after.ID, out.match.Evidence))
		case out.displaced:
			if len(out.match.Matches) == 0 {
				log.Printf("youkai: collect: position %d scroll %d -> %d displaced without correspondences: rows=%d evidence=%s",
					run.position, before.ID, out.after.ID, out.match.Rows, out.match.Evidence)
				return scan.NewFault(CodePositionLost, errors.New(
					"collect: the list displaced but the matcher reported no correspondences"))
			}
			if err := run.applyTransition(before, out.after, run.listMatch, out.afterMatch, out.match); err != nil {
				outcome = "rejected"
				attemptLog[len(attemptLog)-1].outcome = outcome
				log.Printf("youkai: collect: position %d scroll %d -> %d rejected: %v", run.position, before.ID, out.after.ID, err)
				return err
			}
			rows = out.match.Rows
			run.positionScrolls++
			run.listMatch = out.afterMatch
			run.lastFrame = out.after
			run.lastScrollRows = out.match.Rows
			run.lastScrollEvidence = fmt.Sprintf(
				"frame %d -> %d: %d rows, %d correspondences; matcher: %s",
				before.ID, out.after.ID, out.match.Rows, len(out.match.Matches), out.match.Evidence)
			log.Printf("youkai: collect: position %d scroll %d -> %d: %d rows, %d correspondences",
				run.position, before.ID, out.after.ID, out.match.Rows, len(out.match.Matches))
			return nil
		case out.stationary:
			if !out.unmoved {
				// The last comparison was Stationary, but the wait also
				// saw movement or an undecided comparison: the drag's
				// effect is not proven, so no second drag is sent.
				return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
					"collect: the drag ended stationary but the wait also observed movement or an undecided comparison, so its effect is unverified (attempt %d)", attempt+1))
			}
			if attempt < run.inv.maxRetries() {
				// Only a proven unmoved drag may be retried, and the
				// retry must compare against the newest verified frame
				// instead of the first attempt's stale baseline. A
				// navigation mismatch on that read is a verdict failure:
				// keep this attempt's outcome so the deferred handler
				// writes the collected evidence. Operational errors
				// (cancellation, focus loss, geometry change) keep the
				// error outcome and write no dump.
				next, err := run.currentList(ctx)
				if err != nil {
					var fault *scan.FaultError
					if !errors.As(err, &fault) || fault.Code != CodeNavigationMismatch {
						outcome = "error"
						afterID = 0
						waitOut = scrollOutcome{}
					}
					return err
				}
				before = next
				continue
			}
			return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
				"collect: the drag never displaced the list within the budget (attempt %d) and no end evidence held", attempt+1))
		default:
			// No verifiable verdict: the drag's effect is unknown, so a
			// second drag would stack an action on an unproven result.
			return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
				"collect: the scroll drag produced no verifiable verdict within the budget (attempt %d); no further drag is sent", attempt+1))
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
	if !run.positionListVerdict(m) {
		return platform.Frame{}, scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
			"collect: the current list frame (id %d) is not a verifiable warehouse list at wheel position %d: %s",
			f.ID, run.position, m.Evidence))
	}
	run.listMatch = m
	run.lastFrame = f
	return f, nil
}

// sameFrameContent reports whether two observations carry the same complete
// frame. The scripted tests and replay may reuse one immutable image, but
// production copies the latest frame into a new RGBA buffer for every
// request; an idle confirmation keeps the complete frame's ID. Frame IDs are
// session-monotonic, so equal non-zero IDs under one geometry are the
// content invariant even when the buffer differs.
func sameFrameContent(a, b platform.Frame) bool {
	if a.Content != b.Content || a.Geometry != b.Geometry {
		return false
	}
	if a.ID != 0 && a.ID == b.ID {
		return true
	}
	return a.Image != nil && a.Image == b.Image
}

// scrollOutcome is one awaitScroll's terminal result.
type scrollOutcome struct {
	stationary bool
	displaced  bool
	lost       bool
	// unmoved reports that every observation in the wait was proof the
	// list had not moved: the same content as the pre-drag frame or a
	// Stationary comparison, with no displacement, Lost, Undecided or
	// unreadable page at any point. Only then may a caller treat the
	// Stationary deadline as "the drag did nothing" and send one more
	// drag (T5 §5).
	unmoved bool
	// sawFrame reports that the wait received at least one observation.
	// lastFrameIsList reports that the most recently received observation
	// was a verifiable warehouse list. Together they let a caller tell a
	// list that merely did not decide from a page that left the warehouse
	// (popup, loading) or a wait with no frames at all.
	sawFrame        bool
	lastFrameIsList bool
	// lastEvidence is the page-match evidence of the most recently
	// received observation, even when that frame is not a verifiable
	// warehouse list. A stop message can then describe what was actually
	// last seen instead of an older list frame or "no frame received".
	lastEvidence string
	// unsettled reports a displacement that never settled relative to its
	// candidate within the budget. It is a Lost outcome, but the position
	// was verified to have moved; only where it finally stopped is unknown.
	unsettled  bool
	after      platform.Frame
	afterMatch PageMatch
	match      ScrollMatch
	// settleMatch is the candidate-relative comparison that confirmed the
	// candidate stopped moving. It is zero when the settle rested on
	// identical frame content instead of a matcher comparison, or when the
	// wait did not settle.
	settleMatch ScrollMatch
	// settled reports that the wait committed a displacement whose
	// candidate stayed stable for the timing profile's consecutive settle
	// observations; settleFrameID is the observation that completed the
	// streak. The diagnostics use them, never a navigation decision.
	settled       bool
	settleFrameID uint64
	// motionStarted, motionAt and motionFrameID record the first
	// observation that the list's content changed after the drag (F7's
	// late-start window ends there). The stage 2 diagnostics calibrate the
	// timing budgets from them; they never feed a navigation decision.
	motionStarted bool
	motionAt      time.Time
	motionFrameID uint64
	// waitStart is when the wait began (after the drag's dispatch), the
	// origin of the manifest's frame-time axis.
	waitStart time.Time
	// candidate and hasCandidate describe the wait's final candidate for
	// the failure dump; they never feed a decision.
	candidate    platform.Frame
	hasCandidate bool
	// trace retains the frames the wait received. The failure dump writes
	// it after the callers record their timings.
	trace *scrollTrace
}

// waitVerdict is the latest judgement one awaitScroll produced. The
// deadline reports this state, not a union of everything the wait ever
// saw: a later Undecided supersedes an earlier Stationary or Lost instead
// of leaving a stale flag behind (2026-10-28 field failure: a Lost frame
// followed by 34 Undecided frames still ended as positionLost). A frame
// that cannot be read as the warehouse list also leaves the wait
// undecided until a later readable frame decides it.
type waitVerdict int

const (
	waitVerdictNone waitVerdict = iota
	waitVerdictStationary
	waitVerdictDisplaced
	waitVerdictLost
	waitVerdictAmbiguous
	waitVerdictUndecided
)

// awaitScroll waits for the scroll's effect and its settling on fresh
// warehouse frames, under the stage 2 timing model (decision §4.3 K2, F7):
// the wait first observes that the list's movement started, and only then
// judges the settle. A displacement is only a candidate: the matcher may
// still be watching an inertial or snapping animation, so the candidate must
// stay stable relative to itself for ScrollTiming.SettleObservations
// consecutive independent observations — each one a fresh frame whose
// comparison against the candidate is Stationary (or whose content is the
// candidate's) — before the traversal commits anything. Platform idle
// confirmations (Reused frames) are neutral: they repeat the newest fresh
// frame's already-judged content, so the same idle state re-served every
// recheck interval is one observation, not many, and must not complete the
// streak by itself. A later total displacement relative to the pre-drag
// frame replaces the candidate (late inertia and bottom bounces land here)
// and restarts the stability count; a total Lost keeps the wait alive to
// the budget (the list may still settle) but ends as Lost when no settling
// confirmation arrives. Reused confirmations never create a displacement of
// their own. Without a candidate, Stationary remains a deadline verdict,
// not an early one (the drag's effect may still be in flight; F7 measured
// motion starting ~1s after the action).
//
// The wait's deadline is the scroll budget, with two profile-driven
// widenings: before any motion is observed the deadline is at least
// MotionStartBudget after the wait began (F7: stillness inside the
// late-start window is not evidence the drag failed, whatever the
// configured scroll budget), and a motion observed near the budget's end
// gets its own SettleBudget settle window instead of being cut off
// mid-settle. Neither widening ever shortens the plain scroll budget.
//
// The terminal verdict is the latest one: Undecided supersedes an earlier
// Stationary or Lost. An existing candidate still dominates — it cannot be
// committed without a settling confirmation — and the separate unmoved
// flag records whether the whole wait only ever proved stillness.
func (run *inventoryRun) awaitScroll(ctx context.Context, before platform.Frame) (scrollOutcome, error) {
	timing := run.inv.scrollTiming()
	waitStart := time.Now()
	deadline := waitStart.Add(run.inv.scrollBudget())
	// motionStarted and motionAt record the first observation that the
	// list's content changed (a displacement, a lost overlap, or a
	// zero-shift normalized correlation clearly below the stationary
	// threshold); motionFrameID names the frame. Stillness before that is
	// the F7 late-start window and proves nothing about the drag's effect.
	// The zero-shift MAD alone is deliberately not movement evidence:
	// codec noise keeps a static cloud list at 5–10 against the 4.0
	// threshold, so an err0-based rule would mark motion on the first
	// noisy frame.
	var (
		motionStarted bool
		motionAt      time.Time
		motionFrameID uint64
		settleStreak  int
	)
	// effectiveDeadline is the scroll budget with the profile's widenings:
	// the motion-start floor before any movement, and the settle window
	// measured from the observed motion afterwards. Zero arrival times (a
	// scripted view without frame times) never widen anything.
	effectiveDeadline := func() time.Time {
		if motionStarted {
			if !motionAt.IsZero() {
				if extended := motionAt.Add(timing.SettleBudget); extended.After(deadline) {
					return extended
				}
			}
			return deadline
		}
		if floor := waitStart.Add(timing.MotionStartBudget); floor.After(deadline) {
			return floor
		}
		return deadline
	}
	// observeMotion records the first movement evidence on a fresh frame:
	// a verified displacement, a lost overlap, or a zero-shift normalized
	// correlation clearly below the stationary threshold. The MAD error
	// alone never counts (see the motionStarted comment).
	observeMotion := func(f platform.Frame, sm ScrollMatch) {
		if motionStarted {
			return
		}
		moved := sm.Displaced || sm.Lost
		if threshold := run.stationaryNCC(); threshold > 0 &&
			sm.Metrics != nil && sm.Metrics.NCCMeasured {
			if ncc0, ok := sm.Metrics.RowNCC[0]; ok && ncc0 < threshold {
				moved = true
			}
		}
		if moved {
			motionStarted = true
			motionAt, motionFrameID = f.ArrivedAt, f.ID
		}
	}
	after := run.lastArrived
	trace := newScrollTrace(run.inv.Layout, before)
	out := scrollOutcome{trace: trace, waitStart: waitStart}
	var (
		candidate       platform.Frame
		candidateMatch  PageMatch
		candidateScroll ScrollMatch
		haveCandidate   bool
		verdict         = waitVerdictNone
		stillOnly       = true
	)
	settle := func(confirming platform.Frame) (scrollOutcome, error) {
		out.displaced = true
		out.after, out.afterMatch, out.match = candidate, candidateMatch, candidateScroll
		out.candidate, out.hasCandidate = candidate, true
		out.settled, out.settleFrameID = true, confirming.ID
		out.motionStarted, out.motionAt, out.motionFrameID = motionStarted, motionAt, motionFrameID
		return out, nil
	}
	finish := func() (scrollOutcome, error) {
		// A retry is only safe when the wait never saw anything but
		// proof that the list stayed where it was. A Stationary final
		// verdict after an Undecided or a displacement is not that proof.
		out.unmoved = stillOnly && verdict == waitVerdictStationary && !haveCandidate
		out.motionStarted, out.motionAt, out.motionFrameID = motionStarted, motionAt, motionFrameID
		switch {
		case haveCandidate:
			// A displacement was seen but never settled: commit
			// nothing and stop at the unverified position (T5 §5).
			out.lost, out.unsettled = true, true
			out.after, out.afterMatch, out.match = candidate, candidateMatch, candidateScroll
			out.candidate, out.hasCandidate = candidate, true
		case verdict == waitVerdictLost || verdict == waitVerdictAmbiguous || verdict == waitVerdictDisplaced:
			out.lost = true
		case verdict == waitVerdictStationary:
			out.stationary = true
		}
		return out, nil
	}
	for {
		// A live stream keeps delivering frames; the deadline must be
		// enforced here as well, not only by Frame's context.
		if err := ctx.Err(); err != nil {
			return scrollOutcome{}, err
		}
		if !time.Now().Before(effectiveDeadline()) {
			return finish()
		}
		fctx, cancel := context.WithDeadline(ctx, effectiveDeadline())
		f, err := run.view.Frame(fctx, platform.FrameRequest{After: after})
		cancel()
		if err != nil {
			switch {
			case ctx.Err() != nil:
				return scrollOutcome{}, ctx.Err()
			case errors.Is(err, context.DeadlineExceeded):
				return finish()
			default:
				return scrollOutcome{}, wrapObservationErr(err)
			}
		}
		trace.record(f)
		if f.Geometry != run.geo {
			return scrollOutcome{}, run.geometryChanged(f)
		}
		m := run.inv.matcher().Match(f)
		out.sawFrame = true
		out.lastFrameIsList = run.listVerdict(m)
		out.lastEvidence = m.Evidence
		if f.Reused {
			// Unchanged content can prove the candidate stayed still; it
			// cannot prove a displacement not yet seen fresh. It also never
			// drives the settle streak: an idle confirmation repeats the
			// newest fresh frame's content — the same idle state re-served
			// every recheck interval is one observation, not many, and a
			// fresh confirmation followed by its reused copy must not count
			// as two. The streak advances only on fresh frames (below);
			// reused frames neither count nor reset it.
			if run.listVerdict(m) {
				out.after, out.afterMatch = f, m
				if haveCandidate {
					if !sameFrameContent(candidate, f) {
						rel := run.inv.matcher().MatchScroll(candidate, f)
						trace.noteCandidate(candidate, f, rel)
						out.match = rel
						if rel.Stationary {
							out.settleMatch = rel
						}
					}
				} else {
					if sameFrameContent(before, f) {
						verdict = waitVerdictStationary
					} else {
						sm := run.inv.matcher().MatchScroll(before, f)
						trace.noteBefore(f, sm)
						out.match = sm
						switch {
						case sm.Stationary:
							verdict = waitVerdictStationary
						case sm.Lost:
							verdict, stillOnly = waitVerdictLost, false
						default:
							// A reused frame cannot prove a displacement,
							// and any other comparison leaves the effect
							// unproven.
							verdict, stillOnly = waitVerdictUndecided, false
						}
					}
				}
			} else {
				// Not a readable warehouse list: no proof of stillness.
				verdict, stillOnly = waitVerdictUndecided, false
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
			// deadline, but the last observation proves nothing.
			verdict, stillOnly = waitVerdictUndecided, false
			settleStreak = 0
			continue
		}
		if haveCandidate {
			if sameFrameContent(candidate, f) {
				settleStreak++
				if settleStreak >= timing.SettleObservations {
					return settle(f)
				}
				continue
			}
			rel := run.inv.matcher().MatchScroll(candidate, f)
			trace.noteCandidate(candidate, f, rel)
			out.match = rel
			if rel.Stationary {
				out.settleMatch = rel
				settleStreak++
				if settleStreak >= timing.SettleObservations {
					return settle(f)
				}
				continue
			}
			settleStreak = 0
			total := run.inv.matcher().MatchScroll(before, f)
			trace.noteBefore(f, total)
			observeMotion(f, total)
			switch {
			case total.Displaced && len(total.Matches) > 0:
				// The list moved further: the latest total displacement is
				// the candidate, still uncommitted until it settles.
				candidate, candidateMatch, candidateScroll = f, m, total
				trace.pinCandidate(f)
				out.match = total
				verdict = waitVerdictDisplaced
			case total.Lost:
				// Possibly still mid-animation; wait to the budget for a
				// settling confirmation.
				verdict = waitVerdictLost
				out.match = total
			case total.Stationary:
				// The list returned to the pre-drag position: there is no
				// displacement to commit.
				haveCandidate = false
				verdict = waitVerdictStationary
			case total.Displaced:
				// Moved, but without a unique correspondence: cannot keep
				// updating the candidate; a later verified total may still
				// produce one.
				haveCandidate = false
				verdict = waitVerdictAmbiguous
				out.match = total
			default:
				// Undecided: the candidate stays pending and still needs
				// its settling confirmation.
				verdict = waitVerdictUndecided
				out.match = total
			}
			continue
		}
		if sameFrameContent(before, f) {
			// The same immutable frame content as the pre-drag frame is
			// proof the list did not move; Stationary is still a deadline
			// verdict, so keep observing.
			out.match = ScrollMatch{Stationary: true, Evidence: fmt.Sprintf(
				"frames %d → %d: the same immutable frame content — the list did not move", before.ID, f.ID)}
			verdict = waitVerdictStationary
			continue
		}
		sm := run.inv.matcher().MatchScroll(before, f)
		trace.noteBefore(f, sm)
		observeMotion(f, sm)
		out.match = sm
		switch {
		case sm.Displaced && len(sm.Matches) > 0:
			haveCandidate = true
			candidate, candidateMatch, candidateScroll = f, m, sm
			trace.pinCandidate(f)
			verdict = waitVerdictDisplaced
			stillOnly = false
			settleStreak = 0
		case sm.Lost:
			verdict, stillOnly = waitVerdictLost, false
		case sm.Stationary:
			verdict = waitVerdictStationary
		case sm.Displaced:
			verdict, stillOnly = waitVerdictAmbiguous, false
		default:
			// Undecided — including a matcher that set no verdict at all:
			// it proves neither movement nor stillness.
			verdict, stillOnly = waitVerdictUndecided, false
		}
	}
}

// stationaryNCC reports the matcher's zero-shift normalized-correlation
// stationary threshold when the implementation exposes it. awaitScroll's
// motion detection uses it: a zero-shift correlation clearly below the
// threshold is movement evidence even when the displacement itself is not
// yet verifiable, while the MAD error alone never is (cloud codec noise
// keeps a static list above the MAD threshold).
func (run *inventoryRun) stationaryNCC() float64 {
	if reporter, ok := run.inv.matcher().(scrollFeatureReporter); ok {
		stationary, _, _, _ := reporter.ScrollFeatureThresholds()
		return stationary
	}
	return 0
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
// warehouse list verdict with the layout's slot count is seen. sawDifferent
// records any usable frame that differed from the baseline, so an
// ineffective click is never retried after a visible change.
// settledNotDifferent records that the pane held stable on content that
// was not proven different from the baseline: an equal pane, or one whose
// comparison fell between the calibrated same/different bands. The
// reference round trip then decides the target's detail instead of the
// traversal guessing a transition.
type detailOutcome struct {
	class               awaitClass
	frame               platform.Frame
	match               PageMatch
	err                 error
	sawTarget           bool
	sawPopulated        bool
	sawDifferent        bool
	settledNotDifferent bool
	lastSelected        int
	stable              bool
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
	return run.awaitSettledSeeded(ctx, expect, nil)
}

// awaitSettledSeeded is awaitSettled with one optional prior observation.
// A seed must already carry the expected verdict and the current geometry;
// it counts as the first stable sample, so the remaining samples still
// come from fresh or reused frames returned by the session. This preserves
// the "current content" proof while avoiding a duplicate settle after an
// action whose resulting frame is already known and unchanged.
func (run *inventoryRun) awaitSettledSeeded(ctx context.Context, expect func(PageMatch) bool, seed *awaitOutcome) awaitOutcome {
	deadline := time.Now().Add(run.inv.actionBudget())
	samples := 0
	out := awaitOutcome{}
	if seed != nil && seed.frame.ID != 0 && seed.frame.Geometry == run.geo && expect(seed.match) {
		out.frame, out.match, out.expected = seed.frame, seed.match, true
		samples = 1
	}
	after := run.lastArrived
	for {
		// A live stream must not extend the wait past its budget.
		if err := ctx.Err(); err != nil {
			out.class, out.err = awaitCanceled, err
			return out
		}
		if !time.Now().Before(deadline) {
			out.class = awaitDeadline
			return out
		}
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
		run.advanceFrameCursor(res.FinishedAt)
		return nil
	case platform.InputPartial:
		if res.Aborted {
			if err := ctx.Err(); err != nil {
				return err
			}
			return scan.NewFault(platform.CodePermitClosed, errors.New(
				"collect: the drag aborted because the input gate closed"))
		}
		// Some events ran; the effect evidence decides, but no frame may
		// predate the events that did run.
		run.advanceFrameCursor(res.FinishedAt)
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

// advanceFrameCursor moves the next Frame request's lower bound past a
// dispatched action (the platform contract's FinishedAt): the action's
// effect cannot be observed in a frame that predates its last event. The
// cursor never moves backwards.
func (run *inventoryRun) advanceFrameCursor(finished time.Time) {
	if finished.After(run.lastArrived) {
		run.lastArrived = finished
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

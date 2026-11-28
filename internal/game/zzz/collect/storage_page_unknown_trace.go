//go:build page_unknown_trace

package collect

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LoTwT/youkai/internal/platform"
)

const pageUnknownTraceEnv = "YOUKAI_PAGE_UNKNOWN_TRACE"

// StoragePageTraceBuilt reports whether this binary includes the QA trace.
const StoragePageTraceBuilt = true

type pageUnknownTraceRing struct {
	frames []pageUnknownTraceFrame
	input  *pageUnknownTraceInput
	inputs []pageUnknownTraceInput
	dumped bool
	// stage names the round-trip phase that produced the current observation:
	// reference preparation, reference wait, return preparation or return
	// wait. Early page/geometry/filter checks record the frame under this
	// stage before returning.
	stage string

	weakFrameID uint64
	weakSlot    int
	weakHits    int
	weakAt      time.Time
}

type pageUnknownTraceBadge struct {
	ExpectedPosition int  `json:"expected_position"`
	Position         int  `json:"position"`
	Known            bool `json:"known"`
	Conflict         bool `json:"conflict"`
}

type pageUnknownTraceFrame struct {
	TargetBadge         *pageUnknownTraceBadge      `json:"target_badge,omitempty"`
	ID                  uint64                      `json:"frame"`
	ArrivedAt           time.Time                   `json:"arrived_at"`
	Reused              bool                        `json:"reused"`
	Geometry            platform.GeometryVersion    `json:"geometry"`
	Content             [4]int                      `json:"content"`
	Image               [2]int                      `json:"image"`
	Stage               string                      `json:"stage,omitempty"`
	Matched             bool                        `json:"matched"`
	Page                PageKind                    `json:"page"`
	Evidence            string                      `json:"evidence"`
	Selected            int                         `json:"selected"`
	Occupied            int                         `json:"occupied"`
	Filter              FilterState                 `json:"filter"`
	Detail              *pageUnknownTraceDetail     `json:"detail,omitempty"`
	Target              int                         `json:"target"`
	Weak                bool                        `json:"weak,omitempty"`
	WeakSlot            int                         `json:"weak_slot,omitempty"`
	WeakHits            int                         `json:"weak_hits,omitempty"`
	AnchorFrame         uint64                      `json:"prior_verified_viewport_frame,omitempty"`
	SawTarget           bool                        `json:"prior_saw_target,omitempty"`
	SawTargetDetail     bool                        `json:"prior_saw_target_detail,omitempty"`
	SelectionCandidates []pageUnknownTraceCandidate `json:"selection_candidates,omitempty"`
	Crops               []pageUnknownTraceCrop      `json:"crops"`
}

type pageUnknownTraceCandidate struct {
	Slot   int    `json:"slot"`
	Hits   int    `json:"hits"`
	Reason string `json:"reason"`
}

type pageUnknownTraceDetail struct {
	Present  bool    `json:"present"`
	Quality  Quality `json:"quality,omitempty"`
	Substats int     `json:"substats,omitempty"`
	Position int     `json:"position,omitempty"`
	Conflict bool    `json:"conflict,omitempty"`
}

type pageUnknownTraceCrop struct {
	Role   string `json:"role"`
	Reason string `json:"reason,omitempty"`
	Slot   int    `json:"slot,omitempty"`
	Hits   int    `json:"hits,omitempty"`
	Rect   [4]int `json:"rect"`
	File   string `json:"file"`
	img    *image.RGBA
}

type pageUnknownTraceInput struct {
	Kind         string                   `json:"kind"`
	Status       string                   `json:"status"`
	Target       int                      `json:"target"`
	FrameID      uint64                   `json:"source_frame"`
	Geometry     platform.GeometryVersion `json:"geometry"`
	EventsPlan   int                      `json:"events_planned"`
	EventsPosted int                      `json:"events_posted"`
	StartedAt    time.Time                `json:"started_at"`
	FinishedAt   time.Time                `json:"finished_at"`
	At           [2]int                   `json:"at"`
}

type pageUnknownTraceManifest struct {
	Schema        string                  `json:"schema"`
	Kind          string                  `json:"kind"`
	Capture       string                  `json:"capture"`
	Origin        string                  `json:"origin"`
	DiagnosticEnv string                  `json:"diagnostic_env"`
	GeneratedAt   time.Time               `json:"generated_at"`
	Position      int                     `json:"position"`
	Viewport      int                     `json:"viewport"`
	Layout        string                  `json:"layout"`
	Target        int                     `json:"target"`
	Stage         string                  `json:"stage,omitempty"`
	Page          PageKind                `json:"page"`
	Evidence      string                  `json:"evidence"`
	Cause         string                  `json:"cause"`
	Error         string                  `json:"error"`
	TimedOut      bool                    `json:"timed_out,omitempty"`
	FailureFrame  uint64                  `json:"failure_frame"`
	TerminalFrame uint64                  `json:"terminal_frame,omitempty"`
	TerminalAt    time.Time               `json:"terminal_arrived_at,omitempty"`
	LastFrame     uint64                  `json:"last_observation_frame,omitempty"`
	LastAt        time.Time               `json:"last_observation_arrived_at,omitempty"`
	WeakFrame     uint64                  `json:"weak_frame,omitempty"`
	WeakSlot      int                     `json:"weak_slot,omitempty"`
	WeakHits      int                     `json:"weak_hits,omitempty"`
	WeakAt        time.Time               `json:"weak_arrived_at,omitempty"`
	Input         *pageUnknownTraceInput  `json:"input,omitempty"`
	Inputs        []pageUnknownTraceInput `json:"inputs,omitempty"`
	Frames        []pageUnknownTraceFrame `json:"frames"`
	CropCount     int                     `json:"crop_count"`
	PostCapture   int                     `json:"post_capture_frames"`
}

func pageUnknownTraceEnabled() bool { return os.Getenv(pageUnknownTraceEnv) == "1" }

func tracePageUnknownInput(run *inventoryRun, target int, frameID uint64, res platform.InputResult) {
	if !pageUnknownTraceEnabled() || run == nil || run.inv == nil || !run.inv.storagePage() || !run.inv.fullScope() || frameID == 0 {
		return
	}
	// Each target action owns its ring. Keeping an earlier action's frames
	// would let a previous successful target's last frame be paired with this
	// action's input receipt and misread as the current terminal frame.
	run.pageUnknownTrace = new(pageUnknownTraceRing)
	tracePageUnknownAction(run, "target-click", target, frameID, res)
}

// tracePageUnknownAction appends one real input receipt. The first target
// click remains Input; later reference/return clicks are retained in Inputs
// so a terminal manifest can distinguish the source actions without
// substituting a later click for the original target action.
func tracePageUnknownAction(run *inventoryRun, kind string, target int, frameID uint64, res platform.InputResult) {
	if !pageUnknownTraceEnabled() || run == nil || run.inv == nil || !run.inv.storagePage() || !run.inv.fullScope() || frameID == 0 {
		return
	}
	if run.pageUnknownTrace == nil {
		run.pageUnknownTrace = new(pageUnknownTraceRing)
	}
	input := pageUnknownTraceInput{
		Kind: kind, Status: string(res.Status), Target: target, FrameID: frameID,
		Geometry: run.geo, EventsPlan: res.EventsPlanned, EventsPosted: res.EventsPosted,
		StartedAt: res.StartedAt, FinishedAt: res.FinishedAt,
		At: [2]int{run.inv.Layout.slotCenter(target).X, run.inv.Layout.slotCenter(target).Y},
	}
	ring := run.pageUnknownTrace
	ring.inputs = append(ring.inputs, input)
	if ring.input == nil {
		ring.input = new(pageUnknownTraceInput)
		*ring.input = input
	}
}

// tracePageUnknownStage selects the round-trip phase for subsequent
// observations and early-return faults.
func tracePageUnknownStage(run *inventoryRun, stage string) {
	if !pageUnknownTraceEnabled() || run == nil || run.inv == nil || !run.inv.storagePage() || !run.inv.fullScope() {
		return
	}
	if run.pageUnknownTrace == nil {
		run.pageUnknownTrace = new(pageUnknownTraceRing)
	}
	run.pageUnknownTrace.stage = stage
}

func tracePageUnknownObserve(run *inventoryRun, f platform.Frame, m PageMatch, target int, out detailOutcome) {
	tracePageUnknownObserveAt(run, f, m, target, run.pageUnknownTraceStage(), true)
	if run == nil || run.pageUnknownTrace == nil || len(run.pageUnknownTrace.frames) == 0 {
		return
	}
	frames := run.pageUnknownTrace.frames
	entry := &frames[len(frames)-1]
	if entry.ID == f.ID {
		entry.AnchorFrame = out.lastVerifiedFrame.ID
		entry.SawTarget = out.sawTarget
		entry.SawTargetDetail = out.sawTargetDetail
	}
}

// tracePageUnknownObserveUnmatched records the real pixels and geometry of a
// frame that a geometry check rejected before matching. It deliberately
// clears page, selection, filter and detail fields: those values belong to a
// different frame and must not be inherited here.
func tracePageUnknownObserveUnmatched(run *inventoryRun, f platform.Frame, target int) {
	tracePageUnknownObserveAt(run, f, PageMatch{}, target, run.pageUnknownTraceStage(), false)
}

func (run *inventoryRun) pageUnknownTraceStage() string {
	if run == nil || run.pageUnknownTrace == nil {
		return ""
	}
	return run.pageUnknownTrace.stage
}

func tracePageUnknownObserveAt(run *inventoryRun, f platform.Frame, m PageMatch, target int, stage string, matched bool) {
	if !pageUnknownTraceEnabled() || run == nil || run.inv == nil || !run.inv.storagePage() || !run.inv.fullScope() ||
		run.inv.DiagnosticDir == "" || f.Image == nil {
		return
	}
	if run.pageUnknownTrace == nil {
		run.pageUnknownTrace = &pageUnknownTraceRing{}
	}
	ring := run.pageUnknownTrace
	if len(ring.frames) > 0 && ring.frames[len(ring.frames)-1].ID == f.ID {
		return
	}
	if !matched {
		m = PageMatch{}
	}
	entry := pageUnknownTraceFrame{
		ID: f.ID, ArrivedAt: f.ArrivedAt, Reused: f.Reused, Geometry: f.Geometry,
		Content: pageUnknownRect(f.Content), Image: [2]int{f.Image.Bounds().Dx(), f.Image.Bounds().Dy()},
		Stage: stage, Matched: matched, Page: m.Page, Evidence: m.Evidence, Target: target,
		Selected: -1, Filter: FilterUnknown,
	}
	if m.List != nil {
		entry.Selected = m.List.Selected
		entry.Occupied = countOccupied(m.List.Occupied)
	}

	if m.List != nil && target >= 0 && target < len(m.List.Positions) {
		badge := m.List.Positions[target]
		entry.TargetBadge = &pageUnknownTraceBadge{Position: badge.Position, Known: badge.known(), Conflict: badge.Conflict}
		if run.ledger != nil {
			if cells := run.ledger.visible(); target < len(cells) {
				entry.TargetBadge.ExpectedPosition = cells[target].Position
			}
		}
	}
	if m.Filter != nil {
		entry.Filter = m.Filter.State
	}
	if m.Detail != nil {
		entry.Detail = &pageUnknownTraceDetail{
			Present: true, Quality: m.Detail.Quality, Substats: m.Detail.Substats,
			Position: m.Detail.Position.Position, Conflict: m.Detail.Position.Conflict,
		}
	}
	entry.SelectionCandidates = tracePageUnknownSelectionCandidates(run, f)
	entry.Crops = tracePageUnknownCrops(run, f, m, entry.SelectionCandidates)
	ring.frames = append(ring.frames, entry)
	if len(ring.frames) > 3 {
		ring.frames = ring.frames[len(ring.frames)-3:]
	}
}

// tracePageUnknownWeak marks the accepted between-tier target observation.
// It is metadata only: the weak frame remains non-admitted and the marker is
// kept in the bounded action ring in case a later frame or the deadline
// replaces the visible weak evidence.
func tracePageUnknownWeak(run *inventoryRun, f platform.Frame, slot, hits int) {
	if !pageUnknownTraceEnabled() || run == nil || run.inv == nil || !run.inv.storagePage() || !run.inv.fullScope() {
		return
	}
	if run.pageUnknownTrace == nil {
		return
	}
	ring := run.pageUnknownTrace
	ring.weakFrameID, ring.weakSlot, ring.weakHits, ring.weakAt = f.ID, slot, hits, f.ArrivedAt
	for i := range ring.frames {
		if ring.frames[i].ID == f.ID {
			ring.frames[i].Weak = true
			ring.frames[i].WeakSlot = slot
			ring.frames[i].WeakHits = hits
		}
	}
}

// tracePageUnknownTerminal records a non-PageUnknown terminal fault from the
// weak-selection path. The actual terminal frame and the last ring observation
// are kept separate so a later frame cannot erase the weak signal.
func tracePageUnknownTerminal(run *inventoryRun, f platform.Frame, m PageMatch, target int, cause error, kind string) {
	if !pageUnknownTraceEnabled() || run == nil || run.inv == nil || !run.inv.storagePage() || !run.inv.fullScope() ||
		run.pageUnknownTrace == nil || cause == nil {
		return
	}
	tracePageUnknownDumpKind(run, f, m, target, cause, "weak-selection-terminal", kind)
}

// tracePageUnknownReferenceFault records a terminal failure of the later
// reference/return actions. It is not a timeout: the target action's input
// receipt stays in the ring, while the actual reference-side observation is
// marked as a distinct fault kind.
func tracePageUnknownReferenceFault(run *inventoryRun, target int, f platform.Frame, m PageMatch, cause error) {
	if !pageUnknownTraceEnabled() || run == nil || run.inv == nil || !run.inv.storagePage() || !run.inv.fullScope() ||
		run.pageUnknownTrace == nil || cause == nil || run.pageUnknownTrace.weakFrameID == 0 {
		return
	}
	tracePageUnknownDumpKind(run, f, m, target, cause, "reference-fault", "weak-selection-reference")
}

// tracePageUnknownFrameFault records the frame at the check that rejected it
// and then marks that same frame as the terminal observation. A matched
// verdict keeps its page/selection/filter evidence; an unmatched frame (a
// geometry rejection before matching) keeps only its pixels and geometry.
func tracePageUnknownFrameFault(run *inventoryRun, target int, f platform.Frame, m PageMatch, matched bool, cause error) {
	if !pageUnknownTraceEnabled() || run == nil || run.inv == nil || cause == nil || f.ID == 0 {
		return
	}
	if !matched {
		tracePageUnknownObserveUnmatched(run, f, target)
		m = PageMatch{}
	} else {
		tracePageUnknownObserveAt(run, f, m, target, run.pageUnknownTraceStage(), true)
	}
	tracePageUnknownReferenceFault(run, target, f, m, cause)
}

// tracePageUnknownWaitFault is the shared early-return record for a detail
// wait. It is active only inside a reference phase; only a frame that the
// matcher could not judge is marked unmatched.
func tracePageUnknownWaitFault(run *inventoryRun, target int, f platform.Frame, m PageMatch, cause error) {
	if run == nil || run.pageUnknownTrace == nil || run.pageUnknownTrace.stage == "" {
		return
	}
	matched := f.Geometry == run.geo
	tracePageUnknownFrameFault(run, target, f, m, matched, cause)
}

// tracePageUnknownWaitTimeout records a pure wait-budget expiry: the last
// legal observation stays in LastFrame/ring, but there is no terminal frame
// and TimedOut is true.
func tracePageUnknownWaitTimeout(run *inventoryRun, target int, out detailOutcome, cause error) {
	if !pageUnknownTraceEnabled() || run == nil || run.inv == nil || !run.inv.storagePage() || !run.inv.fullScope() ||
		run.pageUnknownTrace == nil || cause == nil || run.pageUnknownTrace.stage == "" {
		return
	}
	if out.frame.ID != 0 {
		tracePageUnknownObserveAt(run, out.frame, out.match, target, run.pageUnknownTraceStage(), true)
	}
	tracePageUnknownDumpKindTimeout(run, out.frame, out.match, target, cause,
		"reference-fault", "weak-selection-reference", true)
}

// tracePageUnknownTimeout records the action budget expiring after a weak
// target observation. FailureFrame/TerminalFrame stay zero because no
// PageUnknown rejection occurred; LastFrame is the last relevant observation.
func tracePageUnknownTimeout(run *inventoryRun, target int, out detailOutcome) {
	if !pageUnknownTraceEnabled() || run == nil || run.inv == nil || !run.inv.storagePage() || !run.inv.fullScope() ||
		run.pageUnknownTrace == nil || (!out.sawWeakSelection && !out.sawTargetBadgeTransient) {
		return
	}
	last := out.frame
	if last.ID == 0 && len(run.pageUnknownTrace.frames) > 0 {
		last.ID = run.pageUnknownTrace.frames[len(run.pageUnknownTrace.frames)-1].ID
		last.ArrivedAt = run.pageUnknownTrace.frames[len(run.pageUnknownTrace.frames)-1].ArrivedAt
	}
	if out.sawTargetBadgeTransient {
		tracePageUnknownDumpKindTimeout(run, last, out.match, target,
			errors.New("the detail wait reached its deadline after an unconfirmed target badge"),
			"target-badge-timeout", "target-badge-deadline", true)
		return
	}
	tracePageUnknownDumpKind(run, last, out.match, target,
		errors.New("the detail wait reached its deadline after a weak target observation; no terminal PageUnknown rejection was observed"),
		"detail-timeout", "weak-selection-deadline")
}

// tracePageUnknownReject records only the terminal page-unknown fault from the
// production detail-recovery gate. A PageUnknown frame on its own is not a
// terminal rejection: the next legal frame may recover and the wait may pass.
func tracePageUnknownReject(run *inventoryRun, f platform.Frame, m PageMatch, target int, cause error) {
	if !pageUnknownTraceEnabled() || run == nil || run.inv == nil || !run.inv.storagePage() || !run.inv.fullScope() ||
		run.pageUnknownTrace == nil || cause == nil || m.Page != PageUnknown {
		return
	}
	tracePageUnknownDump(run, f, m, target, cause)
}

func tracePageUnknownPixelMatcher(m Matcher) *PixelMatcher {
	switch v := m.(type) {
	case *PixelMatcher:
		return v
	case *ScrollEvidenceMatcher:
		return v.PixelMatcher
	default:
		return nil
	}
}

// tracePageUnknownSelectionCandidates reads the existing selection-border
// probes without relying on List evidence. A PageUnknown rejection from a
// double-selected frame has List=nil, so the candidate slots and their
// border pixels are the only evidence of which cells triggered the
// rejection.
func tracePageUnknownSelectionCandidates(run *inventoryRun, f platform.Frame) []pageUnknownTraceCandidate {
	if run == nil || run.inv == nil || f.Image == nil {
		return nil
	}
	pm := tracePageUnknownPixelMatcher(run.inv.matcher())
	if pm == nil {
		candidates := make([]pageUnknownTraceCandidate, 0, len(run.inv.Layout.Slots))
		for i := range run.inv.Layout.Slots {
			candidates = append(candidates, pageUnknownTraceCandidate{
				Slot: i, Reason: "border-probe-uncalibrated",
			})
		}
		return candidates
	}
	candidates := make([]pageUnknownTraceCandidate, 0, len(pm.L.Slots))
	for i := range pm.L.Slots {
		border := pm.L.BorderRect(i)
		if !tracePageUnknownROIAvailable(f.Image, border) {
			candidates = append(candidates, pageUnknownTraceCandidate{
				Slot: i, Reason: "border-roi-unavailable",
			})
			continue
		}
		hits := countSelectionPixels(f.Image, border)
		switch {
		case hits >= pm.C.SelectionHitMin:
			candidates = append(candidates, pageUnknownTraceCandidate{
				Slot: i, Hits: hits, Reason: "selected-border",
			})
		case hits >= pm.C.SelectionClearMax:
			candidates = append(candidates, pageUnknownTraceCandidate{
				Slot: i, Hits: hits, Reason: "border-between-tiers",
			})
		}
	}
	return candidates
}

func tracePageUnknownCrops(run *inventoryRun, f platform.Frame, m PageMatch, candidates []pageUnknownTraceCandidate) []pageUnknownTraceCrop {
	l := run.inv.Layout
	img := f.Image
	crops := make([]pageUnknownTraceCrop, 0, 16+len(l.PanelOpen)+len(l.Slots)*2)
	maxCrops := 64
	add := func(role, reason string, slot, hits int, r image.Rectangle) {
		if len(crops) >= maxCrops || r.Empty() {
			return
		}
		r = r.Intersect(img.Bounds())
		if r.Empty() {
			return
		}
		crops = append(crops, pageUnknownTraceCrop{
			Role: role, Reason: reason, Slot: slot, Hits: hits,
			Rect: pageUnknownRect(r), img: tracePageUnknownCopy(img, r),
		})
	}
	add("page-title", "layout-probe", 0, 0, l.PageTitle)
	for i, p := range l.PanelOpen {
		add(fmt.Sprintf("panel-open-%d", i), "layout-probe", 0, 0, p.At)
	}
	add("funnel", "layout-probe", 0, 0, l.Funnel.At)
	if !l.SortLabel.Empty() {
		add("sort-label", "layout-probe", 0, 0, l.SortLabel)
	}
	add("quality-badge", "layout-probe", 0, 0, l.QualityBadge)
	add("detail-avatar", "layout-probe", 0, 0, l.DetailAvatar)
	add("top-management", "layout-probe", 0, 0, l.TopManagement)
	add("detail-position", "layout-probe", 0, 0, l.DetailPositionBadge)
	add("substat4", "layout-probe", 0, 0, l.Substat4Probe)
	if m.List != nil && m.List.Selected >= 0 && m.List.Selected < len(l.Slots) {
		add("selected-border", "known-selected", m.List.Selected, 0, l.BorderRect(m.List.Selected))
		add("selected-bar", "known-selected", m.List.Selected, 0, l.BarRect(m.List.Selected))
	}
	if m.List == nil {
		for _, candidate := range candidates {
			if candidate.Slot < 0 || candidate.Slot >= len(l.Slots) {
				continue
			}
			add(fmt.Sprintf("selection-border-%02d", candidate.Slot), candidate.Reason,
				candidate.Slot, candidate.Hits, l.BorderRect(candidate.Slot))
		}
	}
	for i := range l.Slots {
		add(fmt.Sprintf("bar-%02d", i), "layout-probe", i, 0, l.BarRect(i))
	}
	// Keep the original crop budget, with one extra slot for each badge and the thumb column.
	maxCrops += len(l.Slots) + 1
	track := l.ScrollTrack
	if tracePageUnknownROIAvailable(img, track) {
		add("scroll-thumb-column", "layout-probe", 0, 0, track)
	}
	for i := range l.Slots {
		badge := l.CardPositionRect(i)
		if tracePageUnknownROIAvailable(img, badge) {
			add(fmt.Sprintf("card-position-%02d", i), "layout-probe", i, 0, badge)
		}
	}
	return crops
}

func tracePageUnknownDump(run *inventoryRun, f platform.Frame, m PageMatch, target int, cause error) {
	tracePageUnknownDumpKind(run, f, m, target, cause, "page-unknown", "storageBadgeDisposition-page-unknown")
}

func tracePageUnknownDumpKind(run *inventoryRun, f platform.Frame, m PageMatch, target int, cause error, kind, causeName string) {
	tracePageUnknownDumpKindTimeout(run, f, m, target, cause, kind, causeName, kind == "detail-timeout")
}

func tracePageUnknownDumpKindTimeout(run *inventoryRun, f platform.Frame, m PageMatch, target int, cause error, kind, causeName string, timedOut bool) {
	if !pageUnknownTraceEnabled() || run == nil || run.inv == nil || run.pageUnknownTrace == nil || run.pageUnknownTrace.dumped {
		return
	}
	ring := run.pageUnknownTrace
	ring.dumped = true
	dir := run.inv.DiagnosticDir
	if dir == "" {
		return
	}
	stamp := time.Now().Format("20060102-150405.000")
	sub := filepath.Join(dir, fmt.Sprintf("%s-page-unknown-pos%d-vp%d", tracePageUnknownStamp(stamp), run.position, run.viewport))
	if err := os.MkdirAll(sub, 0o700); err != nil {
		log.Printf("youkai: collect: page-unknown trace: create %s: %v", filepath.Base(sub), err)
		return
	}
	lastID, lastAt := f.ID, f.ArrivedAt
	if n := len(ring.frames); n > 0 {
		lastID, lastAt = ring.frames[n-1].ID, ring.frames[n-1].ArrivedAt
	}
	terminalFrame, terminalAt, failureFrame := f.ID, f.ArrivedAt, f.ID
	if timedOut {
		terminalFrame, terminalAt, failureFrame = 0, time.Time{}, 0
	}
	manifest := pageUnknownTraceManifest{
		Schema: "storage-page-unknown-trace-v1", Kind: kind, Capture: "original-ring",
		Origin: "candidate-platform", DiagnosticEnv: pageUnknownTraceEnv + "=1",
		GeneratedAt: time.Now(), Position: run.position, Viewport: run.viewport,
		Layout: run.inv.Layout.Name, Target: target, Stage: ring.stage,
		Page: m.Page, Evidence: m.Evidence,
		Cause: causeName, Error: cause.Error(), TimedOut: timedOut,
		FailureFrame: failureFrame, TerminalFrame: terminalFrame, TerminalAt: terminalAt,
		LastFrame: lastID, LastAt: lastAt,
		WeakFrame: ring.weakFrameID, WeakSlot: ring.weakSlot, WeakHits: ring.weakHits, WeakAt: ring.weakAt,
		Input: ring.input, Inputs: append([]pageUnknownTraceInput(nil), ring.inputs...), PostCapture: 0,
	}
	for i := range ring.frames {
		fr := &ring.frames[i]
		for j := range fr.Crops {
			c := &fr.Crops[j]
			crop := c.img
			if crop == nil {
				continue
			}
			name := fmt.Sprintf("f%d-%02d-%s.png", fr.ID, j, tracePageUnknownRole(c.Role))
			if err := tracePageUnknownPNG(filepath.Join(sub, name), crop); err != nil {
				log.Printf("youkai: collect: page-unknown trace: write %s: %v", name, err)
				continue
			}
			c.File = name
			manifest.CropCount++
		}
		manifest.Frames = append(manifest.Frames, *fr)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(filepath.Join(sub, "manifest.json"), append(data, '\n'), 0o600); err != nil {
		log.Printf("youkai: collect: page-unknown trace: write manifest: %v", err)
		return
	}
	log.Printf("youkai: collect: page-unknown trace: frame %d, %d frames, %d crops -> %s", f.ID, len(manifest.Frames), manifest.CropCount, filepath.Base(sub))
}

func tracePageUnknownCopy(src *image.RGBA, r image.Rectangle) *image.RGBA {
	if src == nil || r.Empty() || !r.In(src.Bounds()) {
		return nil
	}
	dst := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	for y := 0; y < r.Dy(); y++ {
		srcOff := src.PixOffset(r.Min.X, r.Min.Y+y)
		dstOff := dst.PixOffset(0, y)
		copy(dst.Pix[dstOff:dstOff+r.Dx()*4], src.Pix[srcOff:srcOff+r.Dx()*4])
	}
	return dst
}

func tracePageUnknownPNG(path string, img *image.RGBA) error {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	return writeDiagnosticFile(path, buf.Bytes())
}

func tracePageUnknownRole(s string) string {
	s = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			return r
		}
		return '-'
	}, s)
	return strings.Trim(s, "-")
}

func tracePageUnknownROIAvailable(img *image.RGBA, r image.Rectangle) bool {
	return img != nil && !r.Empty() && r.In(img.Bounds())
}

func pageUnknownRect(r image.Rectangle) [4]int {
	if r.Empty() {
		return [4]int{}
	}
	return [4]int{r.Min.X, r.Min.Y, r.Dx(), r.Dy()}
}

func tracePageUnknownStamp(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '.' {
			return -1
		}
		return r
	}, s)
}

func tracePageUnknownTargetBadgeFault(run *inventoryRun, f platform.Frame, m PageMatch, target int, cause error) {
	if !pageUnknownTraceEnabled() || run == nil || run.pageUnknownTrace == nil {
		return
	}
	tracePageUnknownObserveAt(run, f, m, target, run.pageUnknownTraceStage(), true)
	tracePageUnknownDumpKind(run, f, m, target, cause, "target-badge-terminal", "storageBadgeDisposition-target-badge")
}

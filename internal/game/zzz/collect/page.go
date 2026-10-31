package collect

import (
	"fmt"

	"github.com/LoTwT/youkai/internal/platform"
)

// The page model follows the macOS cloud client's warehouse screen: the
// list, the detail pane and the wheel share one page (T5 §1/§4). A click
// only moves the selection border and swaps the detail content, so the
// matcher reports the list, detail, wheel and funnel evidence of the same
// frame. The filter panel overlays that page and is the only other page
// kind; every frame the anchors cannot decide — login, cloud queue,
// reconnect, popups, transition animations — is PageUnknown. The matcher
// owns the judgment; the state machine only follows verdicts.

// PageKind identifies one page of the ZZZ drive-disc warehouse.
type PageKind string

const (
	// PageWarehouse: list (left), detail pane (center) and wheel (right)
	// share one screen on the macOS cloud client; a click only moves the
	// selection border and swaps the detail content.
	PageWarehouse PageKind = "warehouse"
	// PageFilterPanel: the filter panel overlays the warehouse; the list is
	// dimmed and the funnel is unreadable while it is open.
	PageFilterPanel PageKind = "filterPanel"
	// PageUnknown is every frame the matcher cannot attribute to a known
	// page with the evidence it has. It is the zero value on purpose: an
	// absent verdict must never read as a page.
	PageUnknown PageKind = ""
)

// String renders the kind for logs and evidence prose.
func (p PageKind) String() string {
	if p == PageUnknown {
		return "unknown"
	}
	return string(p)
}

// Association is how one admission's detail was attributed to its
// target — the scan JSON contract's capture.association vocabulary.
type Association string

const (
	// AssociationInitial: the target already carried the selection border
	// when the position's preparation finished and no click had been sent
	// yet; the detail was admitted from the stable initial page (T5 §4's
	// 首次读取已经选中的物品).
	AssociationInitial Association = "initialVerified"
	// AssociationTransition: the selection transition was verified — the
	// pre-click frame, the click, and the post-click detail whose
	// selection evidence rests on the clicked slot (T5 §4's 选中切换).
	AssociationTransition Association = "transitionVerified"
	// AssociationReference: the target's click changed the selection but
	// its detail stayed the same as the pre-click frame, so one reference
	// round trip proved the attribution: a different visible occupied slot
	// showed a distinct, stable detail, and the return click on the target
	// showed a detail different from the reference and stable (T5 §4's
	// 相邻相同详情参照往返). The Observation's Reference carries the
	// reference slot and frame; PreClickFrameID remains the target's
	// original pre-click frame.
	AssociationReference Association = "referenceVerified"
)

// PageMatch is one matcher verdict over one frame: which page the frame
// shows, the structured evidence the navigation and admission rules
// consume, and a prose description of what the verdict rests on — a
// confidence description, never a calibrated probability.
type PageMatch struct {
	// Page is the matched page kind; PageUnknown means the matcher
	// cannot decide, with the reason in Evidence. The matcher never
	// guesses a page kind (T5 §3: 登录、云排队、重连、弹窗及其他页面不开始).
	Page PageKind
	// List carries the warehouse evidence when Page is PageWarehouse;
	// nil otherwise.
	List *ListEvidence
	// Detail carries the detail evidence when Page is PageWarehouse and
	// the detail pane is decidable; nil otherwise.
	Detail *DetailEvidence
	// Wheel carries the wheel position when Page is PageWarehouse; nil
	// otherwise.
	Wheel *WheelEvidence
	// Filter carries the funnel state when Page is PageWarehouse; nil
	// otherwise. The funnel is unreadable while the panel is open.
	Filter *FilterEvidence
	// Panel carries the panel evidence when Page is PageFilterPanel; nil
	// otherwise.
	Panel *PanelEvidence
	// Evidence describes what the verdict rests on — the anchors
	// consulted and what they showed — for logs, diagnostics and the
	// admission record. Empty is allowed but unhelpful.
	Evidence string
}

// ListEvidence is what a warehouse match reports about the list. The
// fields are verdicts, not observations: each one says the matcher
// holds the evidence for it, and absent evidence reads as false.
type ListEvidence struct {
	// Occupied reports, per layout slot in reading order, whether the
	// matcher found an item in the slot (T5 §4.1's 目标格位有物品). The
	// traversal clicks only occupied slots; an unoccupied slot is not a
	// visit and produces no record (T5 §5's 空格不产生物品记录).
	Occupied []bool
	// Selected is the slot carrying the selection border; -1 when no slot
	// in the viewport carries it (the selected item scrolled away). Two
	// bordered slots make the whole verdict PageUnknown, never a pick.
	Selected int
	// Management is the bottom-right badge verdict per slot, in reading
	// order: locked, discarded, none, or unknown. Unknown is never none:
	// a badge the matcher cannot read stays undecided, and the traversal
	// combines several frames to cover the lock's flash.
	Management []ManagementState
	// Avatars is the top-right equipped-agent avatar verdict per slot, in
	// reading order. Present means the avatar badge is visible; absent
	// means the corner is a readable empty placeholder; unknown means the
	// corner is obscured or falls between tiers.
	Avatars []AvatarState
	// TopManagement is the detail page's lock indicator for the selected
	// item, the confirmation evidence for the list badge's locked
	// dimension. The indicator cannot distinguish discarded from none.
	TopManagement TopManagementState
	// Top/Bottom: the scrollbar thumb rests at the track's extremes —
	// single-frame supporting evidence; the traversal pairs it with a
	// stationary drag before it believes an end.
	Top, Bottom bool
}

// ManagementState is one cell's management badge: the mutually exclusive
// locked/discarded/none states, or unknown when the badge cannot be read.
// Unknown must never be collapsed into none.
type ManagementState string

const (
	ManagementUnknown   ManagementState = ""
	ManagementLocked    ManagementState = "locked"
	ManagementDiscarded ManagementState = "discarded"
	ManagementNone      ManagementState = "none"
)

// AvatarState is one avatar probe's verdict. Present and absent are
// readable answers; unknown is not absent.
type AvatarState string

const (
	AvatarUnknown AvatarState = ""
	AvatarPresent AvatarState = "present"
	AvatarAbsent  AvatarState = "absent"
)

// TopManagementState is the detail page's lock indicator: locked (filled
// circle), unlocked (open circle with a bright glyph), or unknown.
type TopManagementState string

const (
	TopManagementUnknown  TopManagementState = ""
	TopManagementLocked   TopManagementState = "locked"
	TopManagementUnlocked TopManagementState = "unlocked"
)

// Equipment-presence vocabulary of an admission. Empty means the
// evidence was unreadable or conflicting; it is not unequipped.
const (
	EquippedStateEquipped   = "equipped"
	EquippedStateUnequipped = "unequipped"
)

// Quality is the drive disc's rarity as the detail pane shows it. Only S
// is calibrated: every sample of the macOS cloud client was S, and the
// A/B badges have no samples yet. QualityNotS exists for the calibrated
// matcher that eventually learns them; the current matcher never returns
// it and reports QualityUnknown instead of guessing.
type Quality string

const (
	QualityUnknown Quality = ""
	QualityS       Quality = "S"
	QualityNotS    Quality = "notS" // only after the A/B badges are calibrated
)

// DetailEvidence is what the warehouse match reports about the shown
// detail pane.
type DetailEvidence struct {
	Quality  Quality
	Substats int // 3 or 4; 0 when the line count cannot be determined
	// Avatar is the level-right equipped-agent avatar: the primary
	// equipment-presence evidence. Unknown is not absent.
	Avatar AvatarState
}

// WheelEvidence is the wheel's selected position.
type WheelEvidence struct{ Position int } // 1..6; 0 when undecidable

// FilterState is the funnel's verdict.
type FilterState string

const (
	FilterUnknown FilterState = ""
	FilterClear   FilterState = "clear"
	FilterActive  FilterState = "active"
)

// FilterEvidence is the funnel's state on the warehouse page.
type FilterEvidence struct{ State FilterState }

// PanelEvidence marks the filter panel as open. The first version only
// recognizes that the panel is open; its controls are the preparation
// step's business (4b-2).
// PanelEvidence is what a filter-panel match reports about the panel.
// The sort fields are empty/false when the layout has no calibrated
// reference or the region does not match it; the page verdict stays
// usable either way, but the traversal records the order as unverified.
type PanelEvidence struct {
	// SortLabel is the recognized sort label when SortVerified is true.
	SortLabel string `json:"sortLabel,omitempty"`
	// SortVerified reports that the panel's sort region matches the
	// calibrated reference. It proves the recorded order, not a guess
	// about an unrecognized label.
	SortVerified bool `json:"sortVerified,omitempty"`
}

// DetailComparison is the verdict over the detail-pane line regions of
// two frames.
type DetailComparison int

const (
	// DetailUndecided: the comparison falls between the same and different
	// thresholds; the caller must not treat it as either.
	DetailUndecided DetailComparison = iota
	DetailSame
	DetailDifferent
)

// PositionPair is one unique overlap correspondence of a verified
// scroll (the scan JSON contract's transitions.matches element): the
// grid position (row, column) an item occupies in the before-viewport
// and the position the same item occupies in the after-viewport.
type PositionPair struct {
	FromRow, FromColumn int
	ToRow, ToColumn     int
}

// ScrollMatch is the matcher's verdict over one scroll transition
// between the stable warehouse frames before and after a drag.
type ScrollMatch struct {
	// Stationary: the list verifiably did not move — the list region's
	// error at zero displacement is below the stationary threshold. This
	// is the evidence behind "drag again and nothing changes", but it is
	// not end evidence by itself (T5 §5).
	Stationary bool
	// Displaced reports the list actually moved — real displacement
	// evidence from the overlap regions (T5 §5: 滚轮刻度、滚动时长或
	// “图变了”都不算位移证据). A drag that changed nothing leaves it false.
	Displaced bool
	// Rows is the verified displacement in whole rows (1..Rows-1); 0 when
	// the verdict is not Displaced.
	Rows int
	// Matches are the unique one-to-one correspondences of the overlap
	// positions. They are the only thing that lets the traversal carry
	// its visited-position bookkeeping across the scroll; without them
	// the new viewport's positions cannot be told apart from the old
	// ones and the machine stops rather than guessing (T5 §5's 存在多个
	// 可能对应关系时不任选一个).
	Matches []PositionPair
	// Lost: the list moved but no acceptable alignment was found in the
	// overlap range (a fling overshot it). The position is at risk; the
	// machine stops rather than guessing how far it went.
	Lost bool
	// Undecided: the pixels changed but neither displacement nor loss met
	// their thresholds (for example a live UI animation over a list that
	// did not move). It is not movement evidence; callers may combine it
	// with an independent probe such as the scrollbar thumb.
	Undecided bool
	// Basis names which evidence accepted a Displaced verdict, for field
	// diagnostics and the stage 5 calibration. It is empty for every other
	// verdict. The values distinguish the normal competition-margin pass
	// from the gap-limited pass that only the scrollbar's independent
	// travel can authorize.
	Basis string
	// Evidence describes what the verdict rests on, in prose.
	Evidence string
	// Metrics are the raw numbers behind the verdict. They exist for field
	// diagnostics — threshold calibration needs to know how close a frame
	// came to each limit — and never feed a traversal decision. Nil means
	// the matcher could not measure the pair.
	Metrics *ScrollMetrics
}

// Displaced-verdict evidence bases. The selection box is the primary
// anchor and the normalized feature must corroborate its row; the
// scrollbar is the independent cross-check that can break a tie only when
// the feature's runner-up gap is below the competition margin.
const (
	// ScrollBasisSelectionFeature is the ordinary selection-box path: the
	// named row clears the correlation floor and beats the runner-up by at
	// least ScrollNCCMargin.
	ScrollBasisSelectionFeature = "selection+feature"
	// ScrollBasisSelectionFeatureThumb is the gap-limited selection-box
	// path: the named row clears the correlation and information floors
	// and no candidate beats it, but the runner-up gap is below the
	// competition margin; the scrollbar's independently derived direction
	// and travel accept the displacement.
	ScrollBasisSelectionFeatureThumb = "selection+feature+scrollbar"
	// ScrollBasisFeatureThumb is the feature-only path used when no usable
	// selection pair exists: the stronger feature threshold, the
	// competition margin and the scrollbar's travel all agree.
	ScrollBasisFeatureThumb = "feature+scrollbar"
)

// ScrollMetrics is the numeric evidence behind one ScrollMatch: the
// zero-displacement error and, when the matcher searched shifts, the best
// shift with its error and the best error of every whole-row candidate.
// RowErrors is keyed by signed rows.
type ScrollMetrics struct {
	// Err0 is the list viewport's mean absolute error at zero displacement.
	Err0 float64
	// Measured reports that the shift search ran; when false, BestDy,
	// BestErr and RowErrors are unset (the stationary path decides on Err0
	// alone and does not pay for the search).
	Measured  bool
	BestDy    int
	BestErr   float64
	RowErrors map[int]float64
	// NCCMeasured reports that the normalized-feature search ran; when
	// false the fields below are unset (the verdict decided before it, for
	// example pixel-identical frames proving stillness).
	NCCMeasured bool
	// RowNCC is the windowed zero-mean normalized correlation of every
	// whole-row candidate, keyed by signed rows including +0. Each non-zero
	// value is the best correlation within the row-tolerance window, so
	// sub-row jitter does not deflate a true alignment; the +0 value is the
	// correlation at exactly zero displacement (stillness admits no
	// window).
	RowNCC map[int]float64
	// BestNCCRow and BestNCC name the strongest non-zero candidate.
	BestNCCRow int
	BestNCC    float64
	// InfoStd is the winning comparison's support-region grayscale std, the
	// information gate behind the normalized verdicts.
	InfoStd float64
	// SelectionBefore and SelectionAfter are the selection-box slot
	// readings the comparison used (-1 none, -2 unreadable or ambiguous).
	SelectionBefore int
	SelectionAfter  int
}

// Matcher is the traversal's evidence engine: it turns frames into the
// page verdicts and position evidence T5's navigation and admission
// rules consume. The interface is the contract; implementations may
// hold per-session state. PixelMatcher is the calibrated implementation
// for the macOS cloud layout; Uncalibrated remains the honest fallback
// when no calibration is injected.
type Matcher interface {
	// Match reports which page the frame shows. It consumes the whole
	// frame — image, content region and geometry version — and never
	// guesses: an undecidable frame is PageUnknown with the reason in
	// Evidence.
	Match(frame platform.Frame) PageMatch
	// MatchScroll verifies one scroll transition between the stable
	// warehouse frames before and after a drag: whether the list
	// displaced, and the unique overlap correspondences that carry the
	// position bookkeeping across. The traversal validates what comes
	// back (bounds, one-to-one, uniform displacement, full overlap
	// coverage) and refuses the transition otherwise.
	MatchScroll(before, after platform.Frame) ScrollMatch
	// CompareDetail compares the detail-pane line regions of two frames.
	// The comparison is the association evidence between the pre-click
	// frame and the frame after the click; it never substitutes for the
	// selection border.
	CompareDetail(a, b platform.Frame) DetailComparison
}

// Uncalibrated is the no-calibration fallback: without the anchors there
// is no honest page, correspondence or detail evidence, so every verdict
// is “cannot determine”. A traversal running on it fails its precheck
// (pageUnverified) instead of guessing — the empty-value semantics the
// layout contract promises.
type Uncalibrated struct{}

// Match implements Matcher: no calibration, no verdict.
func (Uncalibrated) Match(frame platform.Frame) PageMatch {
	return PageMatch{
		Page: PageUnknown,
		Evidence: fmt.Sprintf("no calibrated matcher: the frame (id %d) cannot be attributed to a page without sampled anchors",
			frame.ID),
	}
}

// MatchScroll implements Matcher: no calibration, no correspondence.
func (Uncalibrated) MatchScroll(before, after platform.Frame) ScrollMatch {
	return ScrollMatch{
		Evidence: fmt.Sprintf("no calibrated matcher: frames %d → %d cannot be given a displacement verdict without sampled anchors",
			before.ID, after.ID),
	}
}

// CompareDetail implements Matcher: no calibration, no comparison.
func (Uncalibrated) CompareDetail(a, b platform.Frame) DetailComparison {
	return DetailUndecided
}

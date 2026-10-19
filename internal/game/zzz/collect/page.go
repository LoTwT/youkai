package collect

import (
	"fmt"

	"github.com/LoTwT/youkai/internal/platform"
)

// The page model follows the prescribed navigation sequence over T5's
// page vocabulary: the warehouse list page (T5 §3's 驱动盘仓库, the grid
// the machine selects from) and the disc detail page (T5 §4's detail the
// admission reads), with every other frame — login, cloud queue,
// reconnect, popups, transition animations, a page the anchors cannot
// decide — falling to PageUnknown. The matcher owns the judgment of
// what a frame shows; the state machine only follows verdicts.

// PageKind identifies one page of the ZZZ drive-disc warehouse.
type PageKind string

const (
	// PageWarehouse is the drive-disc warehouse list page: the grid of
	// discs the traversal selects from.
	PageWarehouse PageKind = "warehouse"
	// PageDiscDetail is the detail view of the selected drive disc — the
	// page whose stable, attributed capture the traversal admits.
	PageDiscDetail PageKind = "discDetail"
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
	// AssociationTransition: the selection transition was verified — the
	// pre-click list frame, the click, and the post-click detail whose
	// selection evidence rests on the clicked slot (T5 §4's 选中切换).
	// Every skeleton admission carries it: the machine always clicks
	// before it reads, so there is no initial read and no neighbor
	// round-trip yet (both are calibrated-layout work).
	AssociationTransition Association = "transitionVerified"
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
	// Detail carries the detail evidence when Page is PageDiscDetail;
	// nil otherwise.
	Detail *DetailEvidence
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
	// Top reports visible top evidence — the scrollbar or list structure
	// that proves the list is at its beginning (T5 §3's 起点: “向上滚动
	// 很多次”与画面不再变化都不能单独证明顶部). Without it the traversal
	// refuses to start: a mid-list start cannot be marked as covering
	// the inventory.
	Top bool
	// Bottom reports independent end evidence — the scrollbar bottomed,
	// the list tail structure, the last occupied slot (T5 §5's 末端证据).
	// A scroll that changes nothing is not such evidence; only Bottom
	// completes the walk.
	Bottom bool
}

// DetailEvidence is what a disc-detail match reports about the shown
// detail.
type DetailEvidence struct {
	// Selected is the layout slot the matcher attributes the shown
	// detail to — the association evidence between the click's target
	// and the detail content (T5 §4.1's 选中标记位于该目标 and §4.3's
	// 选中与详情之间的关联证据). For a layout whose selection marker stays
	// visible it is the slot the marker rests on; what a separate
	// detail-page layout uses as its sample-verified association is its
	// calibrated matcher's business, mapped into the same slot
	// coordinate. nil means the matcher holds no association evidence:
	// the detail is not attributable and not admissible.
	Selected *int
}

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
	// Displaced reports the list actually moved — real displacement
	// evidence from the overlap regions (T5 §5: 滚轮刻度、滚动时长或
	// “图变了”都不算位移证据). A drag that changed nothing leaves it false.
	Displaced bool
	// Matches are the unique one-to-one correspondences of the overlap
	// positions. They are the only thing that lets the traversal carry
	// its visited-position bookkeeping across the scroll; without them
	// the new viewport's positions cannot be told apart from the old
	// ones and the machine stops rather than guessing (T5 §5's 存在多个
	// 可能对应关系时不任选一个).
	Matches []PositionPair
	// Evidence describes what the verdict rests on, in prose.
	Evidence string
}

// Matcher is the traversal's evidence engine: it turns frames into the
// page verdicts and position evidence T5's navigation and admission
// rules consume. The interface is the contract; implementations may
// hold per-session state (a calibrated matcher tracks the list position
// across frames, for example), and PR 4c ships only the honest empty
// semantics (Uncalibrated). The real implementation — pixel anchors
// sampled from the supported display configurations — arrives with
// 4b's field calibration and is injected through the Inventory.
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
}

// Uncalibrated is the matcher of the pre-calibration era: without the
// anchors 4b's field sampling still has to produce, there is no honest
// page or correspondence evidence, so every verdict is “cannot
// determine”. A traversal running on it fails its precheck
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

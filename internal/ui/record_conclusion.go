package ui

import (
	"fmt"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/record"
)

// recordConclusionTone picks a conclusion's text color and tag.
type recordConclusionTone int

const (
	recordToneNeutral recordConclusionTone = iota
	recordToneMuted
	recordToneWarning
	recordToneDanger
)

// recordConclusion is one record's verdict: an optional tag drawn as a
// shape plus text, and one conclusion sentence (C-26).
type recordConclusion struct {
	tag  string
	text string
	tone recordConclusionTone
}

// recordOutcomeKind is the shared ending classification. The list and the
// summary both start here, then write their own wording.
type recordOutcomeKind int

const (
	recordOutcomeUnreadable recordOutcomeKind = iota
	recordOutcomeSaveFailed
	recordOutcomeRunning
	recordOutcomeEmpty
	recordOutcomeInterrupted
	recordOutcomeFault
	recordOutcomeCancelled
	recordOutcomeCompleted
	recordOutcomeUnknown
)

// recordConclusionInput is the subset of HistoryEntry and the open record
// that decides a conclusion. The list and the summary share the ending
// classification instead of keeping two copies of the four-class and
// termination rules.
type recordConclusionInput struct {
	CapturedCount int64
	Coverage      record.CoverageStatus
	// Termination is the session's first termination trigger; nil for a
	// record whose checkpoint predates the field or never ended.
	Termination *record.Termination
	// FailureParams carries the localized-fault facts; the list does not
	// have them and its fault wording stays code-only.
	FailureParams map[string]string
	Counts        app.ItemClassCounts
	// Interrupted marks a record recovery finished after a crash or a
	// lost process.
	Interrupted bool
	// IncompletePositions is meaningful only when Known is true.
	IncompletePositions      int
	IncompletePositionsKnown bool
	// PositionOutcomes and PositionOutcomesKnown are the expanded ledger
	// facts: the list uses them for the partial-rescan wording and the
	// summary for per-position text.
	PositionOutcomes      []app.PositionOutcome
	PositionOutcomesKnown bool
	// PlannedPositions is the declared partial-rescan range; nil means the
	// historical full scan. Rescan marks a record that deliberately covers
	// only that range, so wording never calls unplanned positions missing.
	// RangeInvalid marks damaged partial-rescan data that cannot be treated
	// as either a full or a partial range.
	PlannedPositions []int
	Rescan           bool
	RangeInvalid     bool
	// Sort and SortKnown are the summary's sort verdict; the list does not
	// need them for its shorter wording.
	Sort       record.SortVerification
	SortKnown  bool
	Unreadable bool
	// ScanNotWrapped marks the current session the service still owns. It
	// includes the wrap-up save after collection ends; it is model state,
	// not record data.
	ScanNotWrapped bool
	// SaveFailed is the live record's failed final save. It is model state,
	// not record data; the summary and the active history row both derive
	// it from the service snapshot.
	SaveFailed bool
}

// recordOutcome is one classification plus the facts both wording functions
// need. The order follows 08 §2: unreadable, save failure, running, zero
// items, fault, interrupted, cancelled, completed. Running is not an ending,
// but it must precede the zero-item state so a live scan that has not
// collected anything yet is not described as empty. Save failure must
// precede running: when the final save fails the service still reports Busy,
// so both facts are present and the failure is the action the user needs.
type recordOutcome struct {
	Kind  recordOutcomeKind
	Input recordConclusionInput
}

// recordOutcomeFor classifies one record without writing any surface's
// wording.
func recordOutcomeFor(in recordConclusionInput) recordOutcome {
	kind := recordOutcomeUnknown
	switch {
	case in.Unreadable:
		kind = recordOutcomeUnreadable
	case in.SaveFailed:
		kind = recordOutcomeSaveFailed
	case in.ScanNotWrapped:
		kind = recordOutcomeRunning
	case in.CapturedCount == 0 && in.Counts.Total() == 0:
		kind = recordOutcomeEmpty
	case in.Termination != nil && in.Termination.Kind == record.TerminationFailed:
		kind = recordOutcomeFault
	case in.Interrupted || (in.Termination != nil && in.Termination.Kind == record.TerminationInterrupted):
		kind = recordOutcomeInterrupted
	case in.Termination != nil && in.Termination.Kind == record.TerminationCancelled:
		kind = recordOutcomeCancelled
	case in.Termination != nil && in.Termination.Kind == record.TerminationCompleted:
		kind = recordOutcomeCompleted
	}
	return recordOutcome{Kind: kind, Input: in}
}

// plannedPositionOutcomes narrows the expanded ledger to the session's
// declared range. A full scan keeps all six; a partial rescan never shows
// a position it did not plan as failed or unreached.
func plannedPositionOutcomes(in recordConclusionInput) []app.PositionOutcome {
	if len(in.PlannedPositions) == 0 {
		return in.PositionOutcomes
	}
	planned := make(map[int]bool, len(in.PlannedPositions))
	for _, position := range in.PlannedPositions {
		planned[position] = true
	}
	out := make([]app.PositionOutcome, 0, len(in.PlannedPositions))
	for _, outcome := range in.PositionOutcomes {
		if planned[outcome.Position] {
			out = append(out, outcome)
		}
	}
	return out
}

// plannedPositionNumbers formats the declared range as "1–3、5".
func plannedPositionNumbers(positions []int) string {
	return summaryPositionRanges(positions)
}

// recordConclusionInterrupted applies one definition to both surfaces: a
// non-live session with no clean termination counts as interrupted even when
// recovery did not mark any individual item. An explicit failed, cancelled or
// completed termination is the session's actual ending and wins over an item
// flag; an explicit interrupted termination is always an interruption.
func recordConclusionInterrupted(term *record.Termination, itemInterrupted, live bool) bool {
	if term != nil {
		switch term.Kind {
		case record.TerminationInterrupted:
			return true
		case record.TerminationFailed, record.TerminationCancelled, record.TerminationCompleted:
			return false
		}
	}
	return itemInterrupted || (!live && app.InterruptedSession(term))
}

// recordConclusionFor states the records list's conclusion. The list keeps
// its short wording from #133; the summary uses recordSummaryWording on the
// same classification.
func recordConclusionFor(in recordConclusionInput) recordConclusion {
	return recordListConclusion(recordOutcomeFor(in))
}

// rescanRangeText names the declared partial-rescan positions.
func rescanRangeText(in recordConclusionInput) string {
	if len(in.PlannedPositions) == 0 {
		return ""
	}
	return plannedPositionNumbers(in.PlannedPositions) + " 号位"
}

// rescanIncompletePositions lists only the planned positions that did not
// complete, so a complete position in the selected range is not named in
// an "incomplete" conclusion.
func rescanIncompletePositions(in recordConclusionInput) []int {
	var incomplete []int
	for _, outcome := range plannedPositionOutcomes(in) {
		if outcome.State != app.PositionOutcomeComplete {
			incomplete = append(incomplete, outcome.Position)
		}
	}
	return incomplete
}

// rescanWordingSupplement states that the record is independent from the
// source it was requested from.
const rescanWordingSupplement = "独立补扫记录，原记录没有改变。"

// recordListConclusion is the records list's wording. It never shows a raw
// fault code: an unknown code degrades to the ending alone (09 §4.3).
func recordListConclusion(out recordOutcome) recordConclusion {
	in := out.Input
	if in.RangeInvalid && (out.Kind == recordOutcomeEmpty ||
		out.Kind == recordOutcomeCompleted || out.Kind == recordOutcomeUnknown) {
		return recordConclusion{text: "范围记录无效，无法确认覆盖", tone: recordToneWarning}
	}
	switch out.Kind {
	case recordOutcomeUnreadable:
		return recordConclusion{tag: "!", text: "无法打开：记录不可读", tone: recordToneDanger}
	case recordOutcomeSaveFailed:
		return recordConclusion{tag: "!", text: "保存失败", tone: recordToneDanger}
	case recordOutcomeRunning:
		return recordConclusion{text: "扫描进行中"}
	case recordOutcomeEmpty:
		return emptyListConclusion(in)
	case recordOutcomeInterrupted:
		if in.Rescan {
			return recordConclusion{tag: "中断", text: "补扫中断 · 已恢复已保存部分", tone: recordToneWarning}
		}
		return recordConclusion{tag: "中断", text: "已恢复已保存部分", tone: recordToneWarning}
	case recordOutcomeFault:
		return listFaultConclusion(in)
	case recordOutcomeCancelled:
		if in.Rescan {
			return recordConclusion{text: "补扫停止 · " + rescanRangeText(in), tone: recordToneWarning}
		}
		return recordConclusion{text: "手动停止 · 覆盖：" + coverageText(in.Coverage)}
	case recordOutcomeCompleted:
		if in.Rescan {
			return rescanCompletedConclusion(in)
		}
		return completedRecordConclusion(in)
	default:
		return recordConclusion{text: "结束状态未知", tone: recordToneMuted}
	}
}

// emptyListConclusion preserves the list's pre-existing wording for a
// zero-item record: an interrupted record keeps its recovery tag, a fault
// keeps its cause, and only a normal end says "没有采集到驱动盘".
func emptyListConclusion(in recordConclusionInput) recordConclusion {
	if in.Interrupted || (in.Termination != nil && in.Termination.Kind == record.TerminationInterrupted) {
		if in.Rescan {
			return recordConclusion{tag: "中断", text: "补扫中断 · 已恢复已保存部分", tone: recordToneWarning}
		}
		return recordConclusion{tag: "中断", text: "已恢复已保存部分", tone: recordToneWarning}
	}
	term := in.Termination
	if term == nil {
		return recordConclusion{text: "结束状态未知", tone: recordToneMuted}
	}
	switch term.Kind {
	case record.TerminationCancelled:
		if in.Rescan {
			return recordConclusion{text: "补扫停止 · 覆盖：部分", tone: recordToneWarning}
		}
		return recordConclusion{text: "手动停止 · 覆盖：" + coverageText(in.Coverage)}
	case record.TerminationFailed:
		return listFaultConclusion(in)
	case record.TerminationCompleted:
		if in.Rescan {
			if rangeText := rescanRangeText(in); rangeText != "" && in.PositionOutcomesKnown {
				text := "补扫完成 · " + rangeText + "完整"
				if incomplete := rescanIncompletePositions(in); len(incomplete) > 0 {
					text = "补扫完成 · " + plannedPositionNumbers(incomplete) + " 号位不完整"
				}
				return recordConclusion{text: text}
			}
			return recordConclusion{text: "补扫完成 · 没有采集到驱动盘", tone: recordToneWarning}
		}
		return recordConclusion{text: "正常结束 · 没有采集到驱动盘", tone: recordToneWarning}
	default:
		return recordConclusion{text: "结束状态未知", tone: recordToneMuted}
	}
}

// terminationNotStarted reports a session that ended during preparation:
// it never opened input, so its record must say "没有开始", never "没有采集到
// 任何驱动盘" (which implies a scan operated the game).
func terminationNotStarted(term *record.Termination) bool {
	if term == nil || term.Phase != record.PhasePreparing {
		return false
	}
	switch term.Kind {
	case record.TerminationFailed, record.TerminationCancelled:
		return true
	default:
		return false
	}
}

// listFaultConclusion is the list's fault wording, including the preparing
// phase's "没有开始" variant.
func listFaultConclusion(in recordConclusionInput) recordConclusion {
	term := in.Termination
	prefix := ""
	if in.Rescan {
		prefix = "补扫"
	}
	if term == nil {
		return recordConclusion{text: "中途停止", tone: recordToneDanger}
	}
	cause := faultCauseText(term.Code, nil)
	if term.Phase == record.PhasePreparing && in.CapturedCount == 0 {
		if cause == "" {
			return recordConclusion{text: prefix + "没有开始", tone: recordToneWarning}
		}
		return recordConclusion{tag: "失败", text: prefix + "没有开始：" + cause, tone: recordToneDanger}
	}
	if cause == "" {
		return recordConclusion{text: "中途停止", tone: recordToneDanger}
	}
	return recordConclusion{tag: "失败", text: prefix + "中途停止：" + cause, tone: recordToneDanger}
}

// rescanCompletedConclusion is a partial rescan's own ending. Its coverage
// stays partial by contract, but unselected positions are not failures:
// only the declared range is described.
func rescanCompletedConclusion(in recordConclusionInput) recordConclusion {
	rangeText := rescanRangeText(in)
	if rangeText == "" || !in.PositionOutcomesKnown {
		return recordConclusion{text: "补扫完成 · 覆盖：部分", tone: recordToneMuted}
	}
	if incomplete := rescanIncompletePositions(in); len(incomplete) > 0 {
		return recordConclusion{text: "补扫完成 · " + plannedPositionNumbers(incomplete) + " 号位不完整",
			tone: recordToneWarning}
	}
	return recordConclusion{text: "补扫完成 · " + rangeText + "完整"}
}

// completedRecordConclusion is the conclusion of a normally ended scan: the
// coverage first, then the four classes' pending work, then the warning
// count that still lets the record export.
func completedRecordConclusion(in recordConclusionInput) recordConclusion {
	counts := in.Counts
	switch in.Coverage {
	case record.CoverageComplete:
		// Assessed below.
	case record.CoveragePartial:
		if in.IncompletePositionsKnown && in.IncompletePositions > 0 {
			return recordConclusion{
				text: fmt.Sprintf("没有扫全 · %d 个号位不完整", in.IncompletePositions),
				tone: recordToneWarning,
			}
		}
		return recordConclusion{text: "没有扫全 · 覆盖：部分", tone: recordToneWarning}
	default:
		return recordConclusion{text: "正常结束 · 覆盖：无法确认", tone: recordToneMuted}
	}
	if counts.Total() == 0 {
		// A record from before the four-class counts: the ending is all it
		// can honestly say.
		return recordConclusion{text: "正常结束 · 覆盖：全部"}
	}
	if pending := counts.Pending(); pending > 0 {
		switch {
		case counts.PendingBackfill > 0 && counts.PendingReview > 0:
			return recordConclusion{text: fmt.Sprintf("扫全了，%s 件待处理", formatCount(pending))}
		case counts.PendingBackfill > 0:
			return recordConclusion{text: fmt.Sprintf("扫全了，%s 件待补录", formatCount(counts.PendingBackfill))}
		default:
			return recordConclusion{text: fmt.Sprintf("扫全了，%s 件待校对", formatCount(counts.PendingReview))}
		}
	}
	if counts.Warning > 0 {
		return recordConclusion{
			text: fmt.Sprintf("扫全了，%s 件有警告", formatCount(counts.Warning)),
			tone: recordToneWarning,
		}
	}
	return recordConclusion{text: "扫全了，可以直接导出"}
}

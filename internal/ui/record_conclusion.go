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
	// PositionOutcomes and PositionOutcomesKnown are the summary's expanded
	// ledger facts. The list only needs the degraded count above.
	PositionOutcomes      []app.PositionOutcome
	PositionOutcomesKnown bool
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

// recordListConclusion is the records list's wording. It never shows a raw
// fault code: an unknown code degrades to the ending alone (09 §4.3).
func recordListConclusion(out recordOutcome) recordConclusion {
	in := out.Input
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
		return recordConclusion{tag: "中断", text: "已恢复已保存部分", tone: recordToneWarning}
	case recordOutcomeFault:
		return listFaultConclusion(in)
	case recordOutcomeCancelled:
		return recordConclusion{text: "手动停止 · 覆盖：" + coverageText(in.Coverage)}
	case recordOutcomeCompleted:
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
		return recordConclusion{tag: "中断", text: "已恢复已保存部分", tone: recordToneWarning}
	}
	term := in.Termination
	if term == nil {
		return recordConclusion{text: "结束状态未知", tone: recordToneMuted}
	}
	switch term.Kind {
	case record.TerminationCancelled:
		return recordConclusion{text: "手动停止 · 覆盖：" + coverageText(in.Coverage)}
	case record.TerminationFailed:
		return listFaultConclusion(in)
	case record.TerminationCompleted:
		return recordConclusion{text: "正常结束 · 没有采集到驱动盘", tone: recordToneWarning}
	default:
		return recordConclusion{text: "结束状态未知", tone: recordToneMuted}
	}
}

// listFaultConclusion is the list's fault wording, including the preparing
// phase's "没有开始" variant.
func listFaultConclusion(in recordConclusionInput) recordConclusion {
	term := in.Termination
	if term == nil {
		return recordConclusion{text: "中途停止", tone: recordToneDanger}
	}
	cause := faultCauseText(term.Code, nil)
	if term.Phase == record.PhasePreparing && in.CapturedCount == 0 {
		if cause == "" {
			return recordConclusion{text: "没有开始", tone: recordToneWarning}
		}
		return recordConclusion{tag: "失败", text: "没有开始：" + cause, tone: recordToneDanger}
	}
	if cause == "" {
		return recordConclusion{text: "中途停止", tone: recordToneDanger}
	}
	return recordConclusion{tag: "失败", text: "中途停止：" + cause, tone: recordToneDanger}
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

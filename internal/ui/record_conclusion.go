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

// recordConclusionInput is the subset of HistoryEntry and app.HistoryRecord
// that decides a conclusion. It keeps the records list and the summary on
// one wording instead of two copies of the four-class mapping.
type recordConclusionInput struct {
	CapturedCount int64
	Coverage      record.CoverageStatus
	// Termination is the session's first termination trigger; nil for a
	// record whose checkpoint predates the field or never ended.
	Termination *record.Termination
	Counts      app.ItemClassCounts
	// Interrupted marks a record recovery finished after a crash or a
	// lost process.
	Interrupted bool
	// IncompletePositions is meaningful only when Known is true.
	IncompletePositions      int
	IncompletePositionsKnown bool
	Unreadable               bool
	// Running marks the live session whose scan has not wrapped. It is
	// model state, not record data.
	Running bool
}

// recordConclusionFor states one record's conclusion. It never shows a raw
// fault code: an unknown code degrades to the ending alone (09 §4.3).
func recordConclusionFor(in recordConclusionInput) recordConclusion {
	if in.Unreadable {
		return recordConclusion{tag: "!", text: "无法打开：记录不可读", tone: recordToneDanger}
	}
	if in.Interrupted || (in.Termination != nil && in.Termination.Kind == record.TerminationInterrupted) {
		return recordConclusion{tag: "中断", text: "已恢复已保存部分", tone: recordToneWarning}
	}
	if in.Running {
		return recordConclusion{text: "扫描进行中"}
	}
	term := in.Termination
	if term == nil {
		return recordConclusion{text: "结束状态未知", tone: recordToneMuted}
	}
	switch term.Kind {
	case record.TerminationCancelled:
		return recordConclusion{text: "手动停止 · 覆盖：" + coverageText(in.Coverage)}
	case record.TerminationFailed:
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
	case record.TerminationCompleted:
		return completedRecordConclusion(in)
	default:
		return recordConclusion{text: "结束状态未知", tone: recordToneMuted}
	}
}

// completedRecordConclusion is the conclusion of a normally ended scan: the
// coverage first, then the four classes' pending work, then the warning
// count that still lets the record export.
func completedRecordConclusion(in recordConclusionInput) recordConclusion {
	counts := in.Counts
	if in.CapturedCount == 0 && counts.Total() == 0 {
		return recordConclusion{text: "正常结束 · 没有采集到驱动盘", tone: recordToneWarning}
	}
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
		return recordConclusion{text: "正常结束 · 覆盖：完整"}
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

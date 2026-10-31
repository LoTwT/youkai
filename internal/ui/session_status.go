package ui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/LoTwT/youkai/internal/record"
	"github.com/egoist/mygo/ui"
)

// observedPositionNote is one wheel position's ledger entry as the
// traversal writes it into the session's observed scope (T5 §1). The UI
// reads only the position failure facts; the rest of the scope stays
// opaque.
type observedPositionNote struct {
	Position   int    `json:"position"`
	Complete   bool   `json:"complete"`
	Stage      string `json:"stage"`
	ReasonCode string `json:"reasonCode"`
	Reason     string `json:"reason"`
}

// incompletePositionsText names every position the traversal marked
// incomplete with its preparation/proof stage, cause code and reason
// (decision §4.6 K5). It returns "" when the observed scope has no degraded
// position or cannot be decoded; a record without scope detail simply shows
// the coverage conclusion.
func incompletePositionsText(observed json.RawMessage) string {
	if len(observed) == 0 {
		return ""
	}
	var scope struct {
		Positions []observedPositionNote `json:"positions"`
	}
	if err := json.Unmarshal(observed, &scope); err != nil {
		return ""
	}
	var parts []string
	for _, p := range scope.Positions {
		if p.Complete || p.Stage == "" {
			continue
		}
		detail := positionStageText(p.Stage)
		if p.ReasonCode != "" {
			detail += " · " + p.ReasonCode
		}
		if p.Reason != "" {
			detail += "：" + p.Reason
		}
		parts = append(parts, fmt.Sprintf("位置 %d %s", p.Position, detail))
	}
	if len(parts) == 0 {
		return ""
	}
	return "不完整位置：" + strings.Join(parts, "；")
}

// positionStageText names the preparation or proof step a degraded position
// failed at. An unknown stage still says the position is incomplete rather
// than inventing a cause.
func positionStageText(stage string) string {
	switch stage {
	case "wheel":
		return "转盘切换失败"
	case "filter":
		return "筛选准备失败"
	case "top":
		return "回顶确认失败"
	case "scroll":
		return "滚动推进失败"
	case "bottom":
		return "底部确认失败"
	default:
		return "位置未完成"
	}
}

// sessionTerminationShown reports whether the results page already explains
// a non-completed session. It keeps the incomplete-position note from
// appearing twice (the termination notice and the completion card).
func (m *Model) sessionTerminationShown() bool {
	if m.rec == nil {
		return false
	}
	_, _, ok := sessionTerminationNotice(m.rec.Shell.Termination, m.rec.Interrupted, len(m.rec.Rows))
	return ok
}

// incompletePositionsNote is the current record's degraded-position note,
// read from the observed scope the traversal persisted in the checkpoint.
func (m *Model) incompletePositionsNote() string {
	if m.rec == nil {
		return ""
	}
	return incompletePositionsText(m.rec.Shell.ObservedScope)
}

// sessionTerminationNotice explains a non-completed session on the results
// page. A completed session has no notice: its row and coverage evidence
// already say what happened. A nil termination with interrupted rows is a
// recovery from a process that never reached a terminal checkpoint, so the
// notice still explains why the record is incomplete.
func sessionTerminationNotice(term *record.Termination, interrupted bool, rows int) (string, record.TerminationKind, bool) {
	if term != nil {
		switch term.Kind {
		case record.TerminationFailed:
			text := "扫描失败：" + term.Code + "（" + terminationPhaseText(term.Phase) + "）"
			if rows == 0 {
				text += "，未保留任何条目"
			}
			return text, term.Kind, true
		case record.TerminationCancelled:
			text := "扫描已停止：结果只覆盖已采集的条目"
			if rows == 0 {
				text += "，未保留任何条目"
			}
			return text, term.Kind, true
		case record.TerminationInterrupted:
			return "扫描中断：仅保留已完成条目", term.Kind, true
		}
	}
	if interrupted {
		return "扫描中断：仅保留已完成条目", record.TerminationInterrupted, true
	}
	return "", "", false
}

func terminationPhaseText(phase record.Phase) string {
	switch phase {
	case record.PhasePreparing:
		return "准备阶段"
	case record.PhaseCapturing:
		return "采集阶段"
	case record.PhaseFinalizing:
		return "收尾阶段"
	default:
		return string(phase)
	}
}

// historyTerminationLabel is the compact state the history table can show
// inside its coverage column. The full failure code fits the history row;
// the results page repeats it with its phase and row count.
func historyTerminationLabel(term *record.Termination) string {
	if term == nil {
		return ""
	}
	switch term.Kind {
	case record.TerminationFailed:
		if term.Code == "" {
			return "失败"
		}
		return "失败：" + term.Code
	case record.TerminationCancelled:
		return "已停止"
	case record.TerminationInterrupted:
		return "中断"
	default:
		return ""
	}
}

func (m *Model) sessionTerminationArea(c *ui.Context, t *ui.Theme) {
	if m.rec == nil {
		return
	}
	text, kind, ok := sessionTerminationNotice(m.rec.Shell.Termination, m.rec.Interrupted, len(m.rec.Rows))
	if !ok {
		return
	}
	color := t.Warning
	if kind == record.TerminationFailed {
		color = t.Danger
	}
	ui.Text(c, text).TextColor(color)
	// A session-fatal stop can follow position-level degradation; name the
	// degraded positions beside the termination so the partial scope is not
	// reduced to one code.
	if note := m.incompletePositionsNote(); note != "" {
		ui.Text(c, note).TextColor(t.Warning)
	}
}

func (m *Model) emptyResultsTextForTermination() string {
	if m.rec == nil {
		return ""
	}
	term := m.rec.Shell.Termination
	if term == nil && m.rec.Interrupted {
		return "本次扫描中断，没有已保存的条目。"
	}
	if term == nil {
		return ""
	}
	switch term.Kind {
	case record.TerminationFailed:
		return "本次扫描失败，没有已保存的条目。"
	case record.TerminationCancelled:
		return "本次扫描已停止，没有已保存的条目。"
	case record.TerminationInterrupted:
		return "本次扫描中断，没有已保存的条目。"
	default:
		return ""
	}
}

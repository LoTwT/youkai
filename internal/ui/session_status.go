package ui

import (
	"github.com/LoTwT/youkai/internal/record"
	"github.com/egoist/mygo/ui"
)

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

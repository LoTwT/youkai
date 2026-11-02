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

// failureText maps a platform or traversal fault code plus its structured
// parameters to the user-facing Chinese explanation. It returns "" for a
// code without a dedicated explanation, so callers keep the diagnostic
// code (and any English platform detail) as the fallback.
//
// The platform and collector never build these sentences: they return
// stable codes and parameters (sizes, rectangles, window identification,
// integrity levels), and this is the single place that localizes them.
func failureText(code string, params map[string]string) string {
	get := func(key string) string {
		if params == nil {
			return ""
		}
		return params[key]
	}
	size := func(key string) string {
		return strings.ReplaceAll(get(key), "x", "×")
	}
	allowed := func() string {
		raw := get("allowed")
		if raw == "" {
			return "已开放的显示档位"
		}
		parts := strings.Split(raw, ",")
		for i, part := range parts {
			parts[i] = strings.ReplaceAll(strings.TrimSpace(part), "x", "×")
		}
		return strings.Join(parts, "、")
	}
	switch code {
	case "clientTierNotOpened":
		current := size("current")
		if current == "" {
			current = "当前档位"
		}
		return fmt.Sprintf("当前游戏分辨率 %s 属于已知但尚未开放的档位。请在游戏设置中切换到 %s（窗口模式）后重新开始扫描。", current, allowed())
	case "clientTierUnsupported":
		current := size("current")
		if current == "" {
			current = "当前尺寸"
		}
		return fmt.Sprintf("当前分辨率 %s 不受支持。请在游戏设置中切换到 %s（窗口模式）后重新开始扫描。", current, allowed())
	case "windowOutsideWorkArea":
		detail := ""
		if client, work := get("client"), get("workArea"); client != "" && work != "" {
			detail = fmt.Sprintf("（客户区 %s，显示器工作区 %s）", client, work)
		}
		return "游戏窗口超出屏幕或被任务栏遮挡" + detail + "。请切换到 " + allowed() + " 窗口模式，并把窗口完整放在屏幕内后重新开始扫描。"
	case "windowOccluded":
		detail := "游戏窗口被其他窗口遮挡"
		if probe := get("probe"); probe != "" {
			detail += "（检查点 " + probe
			if class := get("hitClass"); class != "" {
				detail += "，命中窗口类 " + class
			}
			if process := get("hitProcess"); process != "" {
				detail += "，进程 " + process
			}
			detail += "）"
		}
		return detail + "。请关闭遮挡窗口后重新开始扫描。"
	case "frameContentBlank":
		return "游戏画面边缘为纯黑，可能处于独占全屏或窗口在屏幕外。请切换到窗口模式或无边框模式，并把窗口完整放在屏幕内后重新开始扫描。"
	case "exclusiveFullscreen":
		return "游戏正在以独占全屏运行。请切换到窗口模式或无边框模式后重新开始扫描。"
	case "integrityMismatch":
		return "游戏正在以管理员身份运行，youkai 需要同样的权限才能操作游戏。请右键 youkai，选择“以管理员身份运行”后重新打开。youkai 只向游戏发送鼠标操作，不加载驱动、不注入、不读取游戏内存。"
	case "integrityUnverifiable":
		detail := ""
		if target, self := get("target"), get("self"); target != "" || self != "" {
			detail = fmt.Sprintf("（游戏 %s，youkai %s）", integrityLevelText(target), integrityLevelText(self))
		}
		return "无法读取游戏进程或 youkai 自身的权限级别" + detail + "，出于安全考虑不能开始扫描。请右键 youkai，选择“以管理员身份运行”后重新打开。youkai 只向游戏发送鼠标操作，不加载驱动、不注入、不读取游戏内存。"
	case "targetOffscreen":
		return "请把云·绝区零窗口完整放在一块屏幕内后重新开始扫描。"
	case "windowSizeUnsupported":
		expected := size("expected")
		if expected == "" {
			expected = "1164×762"
		}
		return "请把云·绝区零窗口调整为 " + expected + " 后重新开始扫描。"
	case "frameConfigMismatch":
		if get("client") == "macosCloud" {
			return "游戏画面尺寸与已标定的云客户端配置不一致。请把云·绝区零窗口调整为 1164×762 后重新开始扫描。"
		}
		return "游戏画面尺寸与已标定的显示配置不一致。请恢复到已开放的 " + allowed() + " 窗口模式后重新开始扫描。"
	case "anchorsMissing":
		return "游戏画面与当前客户端的布局不匹配。请确认游戏分辨率与窗口尺寸后重新开始扫描。"
	}
	return ""
}

// integrityLevelText names a mandatory integrity level in Chinese; an
// unknown or unreadable level stays explicit rather than becoming
// "untrusted" from a zero value.
func integrityLevelText(level string) string {
	switch level {
	case "untrusted":
		return "不受信任"
	case "low":
		return "低"
	case "medium":
		return "中"
	case "mediumPlus":
		return "中+"
	case "high":
		return "高"
	case "system":
		return "系统"
	case "protected":
		return "受保护"
	case "", "unknown":
		return "未知"
	default:
		return level
	}
}

// sessionTerminationShown reports whether the results page already explains
// a non-completed session. It keeps the incomplete-position note from
// appearing twice (the termination notice and the completion card).
func (m *Model) sessionTerminationShown() bool {
	if m.rec == nil {
		return false
	}
	_, _, ok := sessionTerminationNotice(m.rec.Shell.Termination, m.rec.Shell.FailureParams, m.rec.Interrupted, len(m.rec.Rows))
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
func sessionTerminationNotice(term *record.Termination, params map[string]string, interrupted bool, rows int) (string, record.TerminationKind, bool) {
	if term != nil {
		switch term.Kind {
		case record.TerminationFailed:
			text := "扫描失败："
			if detail := failureText(term.Code, params); detail != "" {
				text += detail
			} else {
				text += term.Code
			}
			text += "（" + terminationPhaseText(term.Phase) + "）"
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
	text, kind, ok := sessionTerminationNotice(m.rec.Shell.Termination, m.rec.Shell.FailureParams, m.rec.Interrupted, len(m.rec.Rows))
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

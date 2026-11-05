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
// reads the shared conclusion fields plus the diagnostic reason prose; the
// rest of the scope stays opaque.
type observedPositionNote struct {
	record.PositionConclusion
	Reason string `json:"reason"`
}

// incompletePositionsText names every position the traversal marked
// incomplete with its preparation/proof stage and the Chinese cause from
// the shared fault table (decision §4.6 K5). An unknown code keeps the
// raw diagnostic code and reason, because no localized cause exists. It
// returns "" when the observed scope has no degraded position or cannot be
// decoded; a record without scope detail simply shows the coverage
// conclusion.
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
		if !p.Degraded() {
			continue
		}
		detail := positionStageText(p.Stage)
		switch cause := faultCauseText(p.ReasonCode, nil); cause {
		case "":
			if p.ReasonCode != "" {
				detail += " · " + p.ReasonCode
			}
			if p.Reason != "" {
				detail += "：" + p.Reason
			}
		default:
			detail += "：" + cause
		}
		parts = append(parts, fmt.Sprintf("%d 号位 %s", p.Position, detail))
	}
	if len(parts) == 0 {
		return ""
	}
	return "不完整号位：" + strings.Join(parts, "；")
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
		return "未完成"
	}
}

// faultExit names where a fault's recovery action leads. The value is
// presentation metadata for the preparation checklist, summary and
// notification surfaces; it is never shown as text.
type faultExit int

const (
	faultExitNone faultExit = iota
	// Preparation-check rows of 08 §1.1.
	faultExitTargetClient
	faultExitAdmin
	faultExitResolution
	faultExitWindowPosition
	faultExitDisplayScale
	faultExitHotkey
	faultExitGamePage
	// Exits outside the checklist.
	faultExitSystemSettings
	faultExitCleanup // 去清理; the disk-space check has no fault code of its own.
	faultExitRetrySave
	faultExitDiagnostics
	faultExitRestart
)

// faultGuidance is one fault code's user-facing explanation: what
// happened (cause), what the user can do about it (action), and where the
// recovery action leads (exit). Cause has no trailing full stop so a
// caller can put it after a colon in a title or history label; action is
// a complete sentence.
//
// The platform and collector never build these sentences: they return
// stable codes and parameters (sizes, rectangles, window identification,
// integrity levels), and this table is the single place that localizes
// them.
type faultGuidance struct {
	cause  string
	action string
	exit   faultExit
	// diagnostics offers "复制诊断信息" as a secondary action for faults
	// whose cause may need the team rather than another user attempt.
	diagnostics bool
}

// faultFacts reads the structured parameters a fault carries. A missing
// fact degrades the sentence rather than putting an English placeholder
// on screen.
type faultFacts map[string]string

func (f faultFacts) get(key string) string { return f[key] }

func (f faultFacts) size(key string) string {
	return strings.ReplaceAll(f.get(key), "x", "×")
}

// allowedTiers renders the opened display tiers; "" means the fault
// carried no list and the caller should use a general phrase.
func (f faultFacts) allowedTiers() string {
	raw := f.get("allowed")
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, ",")
	for i, part := range parts {
		parts[i] = strings.ReplaceAll(strings.TrimSpace(part), "x", "×")
	}
	return strings.Join(parts, "、")
}

func (f faultFacts) rect(key string) string {
	return strings.ReplaceAll(f.get(key), "x", "×")
}

// staticGuidance repeats a sentence pair that has no parameter-dependent
// parts.
func staticGuidance(cause, action string, exit faultExit, diagnostics bool) func(faultFacts) faultGuidance {
	return func(faultFacts) faultGuidance {
		return faultGuidance{cause: cause, action: action, exit: exit, diagnostics: diagnostics}
	}
}

// faultGuidanceTable maps every diagnostic code the platform, collector
// and kernel can report to its cause, action and exit. It is the intended
// single source of user-facing fault text, shared by the preparation
// checklist, summary, notification and diagnostics surfaces; only the
// cause and action fields are rendered today. Coverage is enforced by
// TestFaultGuidanceCoversEveryCode, including new constants in the
// platform packages.
var faultGuidanceTable = map[string]func(faultFacts) faultGuidance{
	// Platform setup faults.

	"permissionMissing": staticGuidance(
		"youkai 缺少运行所需的系统能力",
		"这通常是 youkai 自身的显示缩放支持不完整，请复制诊断信息反馈。",
		faultExitDiagnostics, true),
	"hotkeyUnavailable": staticGuidance(
		"停止热键已被其他程序占用",
		"请关闭占用该热键的程序后重试。",
		faultExitHotkey, false),
	"targetNotFound": staticGuidance(
		"没有找到《绝区零》的游戏窗口",
		"请启动游戏并打开驱动盘仓库页。",
		faultExitTargetClient, false),
	"screenRecordingUnavailable": staticGuidance(
		"无法使用屏幕录制权限",
		"请在系统设置的隐私与安全性中允许 youkai 录制屏幕，然后重新打开。",
		faultExitSystemSettings, false),
	"unsupportedPlatform": staticGuidance(
		"当前系统不支持扫描",
		"当前版本只支持 Windows 本地客户端。",
		faultExitDiagnostics, true),
	"targetOffscreen": staticGuidance(
		"游戏窗口跨屏或超出了屏幕范围",
		"请把云·绝区零窗口完整放在一块屏幕内。",
		faultExitWindowPosition, false),
	"windowSizeUnsupported": func(f faultFacts) faultGuidance {
		expected := f.size("expected")
		cause := "游戏窗口尺寸不是要求的尺寸"
		if expected != "" {
			cause = "游戏窗口尺寸不是 " + expected
		} else {
			expected = "1164×762"
		}
		return faultGuidance{
			cause:  cause,
			action: "请把云·绝区零窗口调整为 " + expected + "。",
			exit:   faultExitResolution,
		}
	},
	"frameConfigMismatch": func(f faultFacts) faultGuidance {
		if f.get("client") == "macosCloud" {
			return faultGuidance{
				cause:  "游戏画面尺寸与已标定的云客户端配置不一致",
				action: "请把云·绝区零窗口调整为 1164×762。",
				exit:   faultExitResolution,
			}
		}
		action := "请恢复到已开放的窗口模式。"
		if allowed := f.allowedTiers(); allowed != "" {
			action = "请恢复到已开放的 " + allowed + " 窗口模式。"
		}
		return faultGuidance{
			cause:  "游戏画面尺寸与已标定的显示配置不一致",
			action: action,
			exit:   faultExitResolution,
		}
	},
	"clientTierNotOpened": func(f faultFacts) faultGuidance {
		cause := "当前游戏分辨率是已知但尚未开放的档位"
		if current := f.size("current"); current != "" {
			cause = "当前游戏分辨率 " + current + " 是已知但尚未开放的档位"
		}
		action := "请在游戏设置中切换到已开放的显示档位（窗口模式）。"
		if allowed := f.allowedTiers(); allowed != "" {
			action = "请在游戏设置中切换到 " + allowed + "（窗口模式）。"
		}
		return faultGuidance{cause: cause, action: action, exit: faultExitResolution}
	},
	"clientTierUnsupported": func(f faultFacts) faultGuidance {
		cause := "当前游戏分辨率不受支持"
		if current := f.size("current"); current != "" {
			cause = "当前分辨率 " + current + " 不受支持"
		}
		action := "请在游戏设置中切换到已开放的显示档位（窗口模式）。"
		if allowed := f.allowedTiers(); allowed != "" {
			action = "请在游戏设置中切换到 " + allowed + "（窗口模式）。"
		}
		return faultGuidance{cause: cause, action: action, exit: faultExitResolution}
	},
	"windowOutsideWorkArea": func(f faultFacts) faultGuidance {
		cause := "游戏窗口超出屏幕或被任务栏遮挡"
		if client, work := f.rect("client"), f.rect("workArea"); client != "" && work != "" {
			cause += "（客户区 " + client + "，显示器工作区 " + work + "）"
		}
		action := "请把游戏窗口完整放在屏幕内。"
		if allowed := f.allowedTiers(); allowed != "" {
			action = "请切换到 " + allowed + " 窗口模式，并把窗口完整放在屏幕内。"
		}
		return faultGuidance{cause: cause, action: action, exit: faultExitWindowPosition}
	},
	"exclusiveFullscreen": staticGuidance(
		"游戏正在以独占全屏运行",
		"请切换到窗口模式或无边框模式。",
		faultExitGamePage, false),
	"integrityMismatch": staticGuidance(
		"游戏正在以管理员身份运行，youkai 需要同样的权限才能操作游戏",
		"请先从托盘退出 youkai，再右键 youkai 选择“以管理员身份运行”重新打开。youkai 只向游戏发送鼠标操作，不加载驱动、不注入、不读取游戏内存。",
		faultExitAdmin, false),
	"integrityUnverifiable": func(f faultFacts) faultGuidance {
		cause := "无法读取游戏进程或 youkai 自身的权限级别，出于安全考虑不能开始扫描"
		if target, self := f.get("target"), f.get("self"); target != "" || self != "" {
			cause += "（游戏 " + integrityLevelText(target) + "，youkai " + integrityLevelText(self) + "）"
		}
		return faultGuidance{
			cause:       cause,
			action:      "请先从托盘退出 youkai，再右键 youkai 选择“以管理员身份运行”重新打开。youkai 只向游戏发送鼠标操作，不加载驱动、不注入、不读取游戏内存。",
			exit:        faultExitAdmin,
			diagnostics: true,
		}
	},
	"dpiAwarenessMismatch": staticGuidance(
		"youkai 的显示缩放支持不完整",
		"这是 youkai 自身的问题，请复制诊断信息反馈。",
		faultExitDiagnostics, true),
	"dpiVirtualized": staticGuidance(
		"游戏窗口的显示缩放与显示器不一致",
		"请右键游戏程序 › 属性 › 兼容性 › 更改高 DPI 设置，关闭“替代高 DPI 缩放行为”，重新打开游戏后再试；仍不通过时复制诊断信息。",
		faultExitDisplayScale, true),
	"dpiCheckUnavailable": staticGuidance(
		"无法读取游戏窗口或显示器的显示缩放",
		"请重新打开游戏和 youkai 后再试；仍无法读取时复制诊断信息。",
		faultExitDisplayScale, true),
	"geometryUnavailable": staticGuidance(
		"无法稳定测量游戏窗口的位置和大小",
		"请重新开始扫描；反复出现时复制诊断信息。",
		faultExitWindowPosition, true),
	"captureFailed": staticGuidance(
		"截取游戏画面失败",
		"请重新开始扫描；反复出现时复制诊断信息。",
		faultExitRestart, true),

	// Platform premise and broken faults.

	"focusLost": staticGuidance(
		"游戏失去了焦点",
		"扫描时请不要切换应用。",
		faultExitGamePage, false),
	"targetHidden": staticGuidance(
		"游戏窗口被最小化或隐藏",
		"请让游戏窗口保持显示并位于前台。",
		faultExitGamePage, false),
	"targetAmbiguous": staticGuidance(
		"找到多个可能的游戏窗口",
		"请只保留游戏主窗口。",
		faultExitTargetClient, false),
	"positionOutside": staticGuidance(
		"要点击的位置不在游戏画面内",
		"请确认游戏分辨率和窗口位置后重新开始扫描。",
		faultExitResolution, false),
	"staleGeometry": staticGuidance(
		"游戏窗口的位置或大小已经变化",
		"请重新开始扫描。",
		faultExitWindowPosition, false),
	"windowOccluded": func(f faultFacts) faultGuidance {
		cause := "游戏窗口被其他窗口遮挡"
		if probe := f.get("probe"); probe != "" {
			cause += "（检查点 " + probe
			if class := f.get("hitClass"); class != "" {
				cause += "，命中窗口类 " + class
			}
			if process := f.get("hitProcess"); process != "" {
				cause += "，进程 " + process
			}
			cause += "）"
		}
		return faultGuidance{cause: cause, action: "请关闭遮挡窗口。", exit: faultExitGamePage}
	},
	"frameContentBlank": staticGuidance(
		"游戏画面边缘为纯黑",
		"请切换到窗口模式或无边框模式，并把窗口完整放在屏幕内。",
		faultExitGamePage, false),
	"targetLost": staticGuidance(
		"游戏窗口已关闭或更换",
		"请重新打开游戏后重新开始扫描。",
		faultExitTargetClient, false),
	"geometryChanged": staticGuidance(
		"游戏窗口的尺寸、位置或显示缩放发生了变化",
		"请重新开始扫描。",
		faultExitWindowPosition, false),
	"geometryInvalid": staticGuidance(
		"游戏窗口的画面区域无效",
		"请重新开始扫描；反复出现时复制诊断信息。",
		faultExitRestart, true),
	"captureStopped": staticGuidance(
		"屏幕采集已停止",
		"请重新开始扫描；反复出现时复制诊断信息。",
		faultExitRestart, true),
	"permissionRevoked": staticGuidance(
		"系统权限被撤销",
		"请重新授权后重新开始扫描。",
		faultExitSystemSettings, false),
	"inputBlocked": staticGuidance(
		"系统拦截了 youkai 的鼠标操作",
		"请先从托盘退出 youkai，再右键 youkai 选择“以管理员身份运行”重新打开，然后重新开始扫描。youkai 只向游戏发送鼠标操作，不加载驱动、不注入、不读取游戏内存。",
		faultExitAdmin, false),
	"inputFailed": staticGuidance(
		"鼠标操作没有成功发送",
		"请重新开始扫描；反复出现时复制诊断信息。",
		faultExitRestart, true),
	"observationFailed": staticGuidance(
		"读取游戏窗口状态失败",
		"请重新开始扫描；反复出现时复制诊断信息。",
		faultExitRestart, true),
	"invalidOperation": staticGuidance(
		"youkai 内部状态异常",
		"请重新开始扫描；反复出现时复制诊断信息。",
		faultExitRestart, true),
	"permitClosed": staticGuidance(
		"扫描已经停止",
		"请重新开始扫描。",
		faultExitRestart, false),

	// Collector faults.

	"prepareTimeout": staticGuidance(
		"30 秒内游戏没有就绪",
		"请让游戏保持在前台并停在驱动盘仓库页，然后重新开始扫描。",
		faultExitGamePage, false),
	"pageUnverified": staticGuidance(
		"游戏页面不是驱动盘仓库页",
		"请打开驱动盘仓库页后重新开始扫描。",
		faultExitGamePage, false),
	"anchorsMissing": staticGuidance(
		"游戏画面与当前客户端的布局不匹配",
		"请确认游戏分辨率和窗口尺寸后重新开始扫描。",
		faultExitResolution, false),
	"navigationMismatch": staticGuidance(
		"点击或拖拽后页面没有按预期变化",
		"请重新开始扫描；反复出现时复制诊断信息。",
		faultExitRestart, true),
	"detailUnattributed": staticGuidance(
		"打开的详情不属于所点击的件",
		"请重新开始扫描；反复出现时复制诊断信息。",
		faultExitRestart, true),
	"detailUnresolved": staticGuidance(
		"详情里的稀有度或副词条无法判定",
		"请重新开始扫描；反复出现时复制诊断信息。",
		faultExitRestart, true),
	"evidenceFailed": staticGuidance(
		"采集到的截图没有保存成功",
		"请重新开始扫描；反复出现时复制诊断信息。",
		faultExitRestart, true),
	"positionLost": staticGuidance(
		"列表滚动后无法确认滚到了哪里",
		"请重新开始扫描；反复出现时复制诊断信息。",
		faultExitRestart, true),
	"positionFailureLimit": staticGuidance(
		"多个号位没有完成",
		"请重新开始扫描；反复出现时复制诊断信息。",
		faultExitRestart, true),

	// Kernel faults and the wiring-fault fallback.

	"controllerFailed": staticGuidance(
		"youkai 内部出错",
		"请重新开始扫描；反复出现时复制诊断信息。",
		faultExitRestart, true),
	"runtimeFailure": staticGuidance(
		"识别过程中出错",
		"请重新开始扫描；反复出现时复制诊断信息。",
		faultExitRestart, true),
	"storageFailure": staticGuidance(
		"扫描结果没有保存",
		"请重试保存；仍失败时复制诊断信息。",
		faultExitRetrySave, true),
	"releaseFailed": staticGuidance(
		"结束扫描时出错",
		"扫描结果不受影响；反复出现时复制诊断信息。",
		faultExitDiagnostics, true),
	"unknownEnd": staticGuidance(
		"扫描以未知状态结束",
		"请重新开始扫描；反复出现时复制诊断信息。",
		faultExitRestart, true),
}

// faultGuidanceFor applies a code's table row to its structured
// parameters. An unknown code returns false so callers keep the
// diagnostic code as the fallback (T2 §3.2).
func faultGuidanceFor(code string, params map[string]string) (faultGuidance, bool) {
	build, ok := faultGuidanceTable[code]
	if !ok {
		return faultGuidance{}, false
	}
	return build(faultFacts(params)), true
}

// faultCauseText is the compact cause for a history row or a
// notification title; it never includes the action or an English code.
func faultCauseText(code string, params map[string]string) string {
	guidance, ok := faultGuidanceFor(code, params)
	if !ok {
		return ""
	}
	return guidance.cause
}

// failureText renders a code's cause and action as one Chinese sentence
// pair. It returns "" for an unknown code, so callers keep the diagnostic
// code as the fallback.
func failureText(code string, params map[string]string) string {
	guidance, ok := faultGuidanceFor(code, params)
	if !ok {
		return ""
	}
	if guidance.action == "" {
		return guidance.cause + "。"
	}
	return guidance.cause + "。" + guidance.action
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
			} else if term.Code != "" {
				text += term.Code
			} else {
				text += "原因未知"
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
// inside its coverage column. A failed row keeps the cause on the record
// page; the row itself never shows a raw diagnostic code.
func historyTerminationLabel(term *record.Termination) string {
	if term == nil {
		return ""
	}
	switch term.Kind {
	case record.TerminationFailed:
		return "失败"
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

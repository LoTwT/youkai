package ui

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/scan"
	"github.com/egoist/mygo/ui"
)

// The scan page's read-only preparation checklist (08 §1.1, R-01). One
// pass returns every row's conclusion, current and required values, fault
// code and check time; the page only localizes those facts and never
// probes the game itself. Starting still runs the full precheck (P-03).

// readinessCheckInterval is how old the checklist may get before the page
// re-checks on its own. The service reports the check time; the trigger
// belongs here (08 §1.1, 09 §4.8).
const readinessCheckInterval = 10 * time.Second

// readinessRowStatus is the row's user-facing conclusion. Unknown is
// rendered as a question, never as a pass.
type readinessRowStatus int

const (
	readinessSatisfied readinessRowStatus = iota
	readinessUnsatisfied
	readinessUnknown
	readinessAfterStart
	// readinessConfirm is the target row's state while a discovered window
	// still waits for the user's confirmation (P-01). The check itself is
	// satisfied; the confirmation is a separate start requirement.
	readinessConfirm
)

type readinessActionKind int

const (
	readinessActionNone readinessActionKind = iota
	readinessActionExit
	readinessActionCleanup
	readinessActionDiagnostics
	readinessActionChooseTarget
	readinessActionChangeTarget
)

// readinessRow is one checklist row after localization. why/how stay
// empty for satisfied rows; a failed row always carries both.
type readinessRow struct {
	item   app.ReadinessItem
	title  string
	status readinessRowStatus
	value  string
	why    string
	how    string
	action readinessActionKind
	// startReason is the compact reason shown left of the start button when
	// the row's own value would be too long or misleading there.
	startReason string
	// failureOnly marks a row whose displayed failure comes from the last
	// preparation-stage refusal while the read-only check does not block a
	// retry. Such a row stays pinned and expanded but never disables the
	// start (08 §1.2).
	failureOnly bool
}

// readinessItems is the checklist order of 08 §1.1. Rows are sorted for
// display but keep this order inside each group.
var readinessItems = []app.ReadinessItem{
	app.ReadinessTargetClient,
	app.ReadinessAdministrator,
	app.ReadinessGameResolution,
	app.ReadinessWindowPosition,
	app.ReadinessDisplayScale,
	app.ReadinessStopHotkey,
	app.ReadinessDiskSpace,
	app.ReadinessRecognitionResources,
	app.ReadinessGamePage,
}

// refreshReadiness starts one read-only pass with the confirmed target, if
// any. The candidate is frozen on the UI thread before the worker starts:
// the worker must never read the candidate slice or its index while the
// user changes the target. A serial discards a pass that a newer refresh
// superseded, and a selection token discards a pass that judged a target
// the user has since replaced.
func (m *Model) refreshReadiness() {
	if !m.source.Available || m.upd == nil {
		return
	}
	m.readinessSerial++
	serial := m.readinessSerial
	target := m.chosenTarget()
	selection := m.targetSerial
	m.readinessLoading = true
	m.goAsync(func() {
		report := m.cfg.Service.CheckReadiness(target)
		m.update(func() {
			if serial != m.readinessSerial {
				return
			}
			if selection != m.targetSerial {
				// The report judged the target the user changed while this
				// pass ran. Applying its candidates and target row could
				// clear or reorder the newer choice; a fresh pass for the
				// current selection owns that state instead.
				m.readinessLoading = false
				m.refreshReadiness()
				return
			}
			m.readinessLoading = false
			m.readinessLoaded = true
			if report.CheckedAt.IsZero() {
				// A source without timestamps still needs a freshness anchor;
				// using the completion time keeps the ten-second rule from
				// re-checking on every frame.
				report.CheckedAt = m.now()
			}
			m.readiness = report
			m.applyReadinessTargets(report)
			m.replaceRecheckedStartFailure()
			if !m.readinessFocusGiven {
				// Only the first completed pass moves the keyboard to the
				// first actionable item (or the start button when everything
				// is satisfied). Later passes, including ones that reorder
				// rows, never take it.
				m.readinessFocusGiven = true
				m.readinessFocus = true
				primary := m.readinessPrimaryAction()
				attention := m.readinessCount()
				switch {
				case primary != "":
					// The row's own control consumes the request.
				case attention == 0:
					m.readinessFocusStart = true
				default:
					// No row offers a primary control: every failed row only
					// explains, or the target row's diagnostics deliberately
					// yields to 重新检查. The refresh entry is the actionable
					// place left.
					m.readinessFocusRefresh = true
				}
			}
			m.scheduleReadinessCheck()
		})
	})
}

// WindowFocused re-checks the preparation checklist when the window
// regains focus, so a game setting changed in the background is reflected
// immediately (08 §1.1).
func (m *Model) WindowFocused() {
	if m.displayPage() != pageScan || !m.source.Available || m.readinessLoading ||
		m.snap.Busy || m.startReqID != "" {
		return
	}
	m.refreshReadiness()
}

// refreshReadinessIfStale re-checks when the last result is older than ten
// seconds, and otherwise arms the timer for the remaining time.
func (m *Model) refreshReadinessIfStale() {
	if !m.source.Available || m.displayPage() != pageScan || m.readinessLoading ||
		m.snap.Busy || m.startReqID != "" {
		return
	}
	if !m.readinessLoaded {
		if !m.readinessLoading {
			m.refreshReadiness()
		}
		return
	}
	age := m.now().Sub(m.readiness.CheckedAt)
	if age >= readinessCheckInterval {
		m.refreshReadiness()
		return
	}
	m.scheduleReadinessCheck()
}

// scheduleReadinessCheck arms one ten-second timer. The timer runs a plain
// update — not model-owned background work — so view tests that do not
// inject After never wait on it.
func (m *Model) scheduleReadinessCheck() {
	if m.cfg.After == nil || m.readinessTimerArmed || !m.source.Available {
		return
	}
	m.readinessTimerArmed = true
	ch := m.cfg.After(readinessCheckInterval)
	go func() {
		<-ch
		m.update(func() {
			m.readinessTimerArmed = false
			m.refreshReadinessIfStale()
		})
	}()
}

// applyReadinessTargets replaces the candidate list from the same pass and
// keeps the choice only when the same window instance is still present. A
// vanished or restarted window requires a fresh choice. Only an initial
// unambiguous discovery is selected automatically.
func (m *Model) applyReadinessTargets(report app.ReadinessReport) {
	chosen := m.chosenTarget()
	m.targets = report.Candidates
	m.targetsAccessDenied = report.AccessDeniedWindows
	m.targetsUnreadable = report.UnreadableGameWindows
	m.targetChoice = -1
	if chosen != nil {
		for i, candidate := range m.targets {
			if sameTarget(*chosen, candidate) {
				m.targetChoice = i
				return
			}
		}
		m.targetNeedsChoice = true
		m.targetInvalidated = true
	}
	if len(m.targets) == 1 && !m.targetNeedsChoice {
		m.targetChoice = 0
	}
}

// loadDurationEstimate reads P-14's expected duration once per history
// change; a failed read keeps the unknown copy rather than an old number.
func (m *Model) loadDurationEstimate() {
	m.goAsync(func() {
		estimate, err := m.cfg.Service.ScanDurationEstimate()
		m.update(func() {
			if err != nil {
				m.estimate = app.ScanDurationEstimate{}
				m.estimateLoaded = true
				return
			}
			m.estimate = estimate
			m.estimateLoaded = true
		})
	})
}

// estimateText renders the bottom bar's expected duration. Unknown states
// both the missing total and that no percentage is shown (P-14).
func (m *Model) estimateText() string {
	if !m.estimateLoaded {
		return "预计：正在读取…"
	}
	if !m.estimate.Known() {
		return "预计：未知（总数未知，不显示百分比）"
	}
	minutes := int(m.estimate.Median.Round(time.Minute) / time.Minute)
	if minutes < 1 {
		minutes = 1
	}
	return fmt.Sprintf("预计：约 %d 分钟（依据最近 %d 次扫描）", minutes, m.estimate.Samples)
}

func (m *Model) now() time.Time {
	if m.cfg.Now != nil {
		return m.cfg.Now()
	}
	return time.Now()
}

// readinessRows localizes the report and applies the page's ordering:
// the last start's failed item, then rows that need the user, then
// unknown rows, satisfied rows and finally the post-start rows.
func (m *Model) readinessRows() []readinessRow {
	rows := make([]readinessRow, 0, len(readinessItems))
	for _, item := range readinessItems {
		rows = append(rows, m.readinessRow(m.readinessCheck(item)))
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return m.readinessRank(rows[i]) < m.readinessRank(rows[j])
	})
	return rows
}

func (m *Model) readinessRank(row readinessRow) int {
	if m.startFailure != "" && row.item == m.startFailureItem {
		return 0
	}
	switch row.status {
	case readinessUnsatisfied, readinessConfirm:
		return 1
	case readinessUnknown:
		return 2
	case readinessSatisfied:
		return 3
	default:
		return 4
	}
}

func (m *Model) readinessCheck(item app.ReadinessItem) app.ReadinessCheck {
	for _, check := range m.readiness.Checks {
		if check.Item == item {
			return check
		}
	}
	return app.ReadinessCheck{Item: item, Verdict: app.ReadinessUnknown}
}

// readinessRow localizes one check, including the target confirmation and
// the last preparation-stage failure.
func (m *Model) readinessRow(check app.ReadinessCheck) readinessRow {
	var row readinessRow
	switch check.Item {
	case app.ReadinessTargetClient:
		row = m.targetReadinessRow(check)
	case app.ReadinessAdministrator:
		row = readinessAdministratorRow(check)
	case app.ReadinessGameResolution:
		row = readinessResolutionRow(check)
	case app.ReadinessWindowPosition:
		row = readinessWindowPositionRow(check)
	case app.ReadinessDisplayScale:
		row = readinessDisplayScaleRow(check)
	case app.ReadinessStopHotkey:
		row = readinessAfterStartRow(check, "停止热键", shortcutStopScan)
	case app.ReadinessDiskSpace:
		row = readinessDiskSpaceRow(check)
	case app.ReadinessRecognitionResources:
		row = readinessRecognitionResourcesRow(check)
	case app.ReadinessGamePage:
		row = readinessAfterStartRow(check, "游戏页面、遮挡、独占全屏", "请打开驱动仓库页。")
	default:
		row = readinessRow{item: check.Item, title: string(check.Item), status: readinessUnknown}
	}
	// Without a game window the window-dependent rows have nothing to judge:
	// say so instead of "cannot read" (08 §1.1's not-found state).
	switch check.Item {
	case app.ReadinessAdministrator, app.ReadinessGameResolution,
		app.ReadinessWindowPosition, app.ReadinessDisplayScale:
		if m.waitingForGame() {
			row.status = readinessUnknown
			row.value = "等待找到游戏"
			row.why, row.how = "", ""
			row.action = readinessActionNone
		}
	}
	if m.startFailureItem == check.Item && m.startFailure != "" {
		// The refusal is presentation only: it pins and explains the row,
		// but a row that the read-only check does not fail must not keep the
		// start disabled (the beginSession deadlock in the round-1 review).
		blocking := row.status == readinessUnsatisfied || row.status == readinessConfirm
		row.status = readinessUnsatisfied
		row.value = m.startFailureValue(check.Item)
		row.why, row.how = readinessFailureParts(m.startFailureCode, m.startFailureParams)
		row.action = readinessActionNone
		row.failureOnly = !blocking
		if row.why == row.value {
			row.why = ""
		}
		if row.why == "" && row.how == "" {
			row.why = m.startFailureMessage
		}
	}
	return row
}

// replaceRecheckedStartFailure drops a read-only row's failure marker once
// the next pass judging that row has completed. The hotkey and game-page
// rows stay marked until the next start, because only a start can check
// them (Q-B, 08 §1.2).
func (m *Model) replaceRecheckedStartFailure() {
	if m.startFailureItem == "" || m.readinessSerial == m.startFailureReadinessSerial {
		return
	}
	if !readinessReadOnlyItem(m.startFailureItem) {
		return
	}
	m.startFailureItem = ""
}

// readinessReadOnlyItem reports whether a completed checklist pass replaces
// the row's start-failure marker.
func readinessReadOnlyItem(item app.ReadinessItem) bool {
	return item != app.ReadinessStopHotkey && item != app.ReadinessGamePage
}

// waitingForGame reports the not-found state: the target row has no window
// to judge and no unreadable/denied explanation, so dependent rows say
// "等待找到游戏" rather than inventing a read failure.
func (m *Model) waitingForGame() bool {
	if m.source.TargetSelection && len(m.targets) == 0 &&
		m.targetsAccessDenied == 0 && m.targetsUnreadable == 0 {
		return true
	}
	check := m.readinessCheck(app.ReadinessTargetClient)
	if check.FaultCode == "targetNotFound" {
		return true
	}
	if check.FaultCode == "targetHidden" {
		return false
	}
	return check.Verdict == app.ReadinessUnsatisfied && len(m.targets) == 0 &&
		m.targetsAccessDenied == 0 && m.targetsUnreadable == 0
}

// targetReadinessRow merges the check with the discovered candidates. A
// selected window shows its actual identity and keeps a 更换目标 action.
func (m *Model) targetReadinessRow(check app.ReadinessCheck) readinessRow {
	row := readinessRow{item: check.Item, title: "目标客户端", status: readinessSatisfied}
	if check.Verdict == app.ReadinessUnsatisfied && check.FaultCode == "targetHidden" {
		row.status = readinessUnsatisfied
		row.why, row.how = readinessParts(check)
		row.value = row.why
		row.startReason = row.why + "。"
		if target := m.chosenTarget(); m.source.TargetSelection && target != nil {
			row.value = targetLabel(*target)
			row.action = readinessActionChangeTarget
		} else {
			row.why = ""
		}
		return row
	}
	if !m.source.TargetSelection {
		row.value = "已配置游戏窗口"
		if check.Verdict == app.ReadinessUnsatisfied {
			row.status = readinessUnsatisfied
			row.value = "未找到已配置的游戏窗口"
		} else if check.Verdict == app.ReadinessUnknown {
			row.status = readinessUnknown
			row.value = "无法读取游戏窗口"
		}
		row.why, row.how = readinessParts(check)
		if row.status == readinessUnsatisfied && row.why == "" {
			row.why = "没有找到《绝区零》的游戏窗口"
			row.how = "请启动游戏并打开驱动仓库页，然后点“重新检查”。"
		}
		return row
	}
	switch {
	case len(m.targets) > 0 && m.chosenTarget() != nil:
		row.value = targetLabel(*m.chosenTarget())
		row.why, row.how = readinessParts(check)
		row.action = readinessActionChangeTarget
	case len(m.targets) == 1:
		row.status = readinessConfirm
		row.value = "已找到 1 个窗口 → 请确认要扫描的一个"
		row.action = readinessActionChooseTarget
	case len(m.targets) > 1:
		row.status = readinessConfirm
		row.value = fmt.Sprintf("找到 %d 个窗口 → 请选择要扫描的一个", len(m.targets))
		row.action = readinessActionChooseTarget
	case m.targetsAccessDenied > 0 && m.targetNeedsElevation():
		// P-05: the permission row carries the full explanation and the
		// same exit; this row says the short cause and never repeats the
		// passage next to its button.
		row.status = readinessUnsatisfied
		row.value = "无法读取游戏窗口（游戏以管理员身份运行）"
		row.action = readinessActionExit
		row.startReason = "youkai 需要与游戏相同的管理员权限。"
	case m.targetsAccessDenied > 0:
		row.status = readinessUnsatisfied
		row.value = "无法读取游戏窗口"
		row.how = targetUnreadableText
		row.action = readinessActionDiagnostics
		row.startReason = "无法读取游戏窗口。"
	case m.targetsUnreadable > 0:
		row.status = readinessUnsatisfied
		row.value = "无法读取游戏进程信息"
		row.how = targetUnreadableText
		row.action = readinessActionDiagnostics
		row.startReason = "无法读取游戏进程信息。"
	case check.FaultCode == "observationFailed" && len(m.targets) == 0:
		row.status = readinessUnsatisfied
		row.value = "查找游戏窗口失败"
		row.how = targetDiscoveryFailedAction
		row.action = readinessActionDiagnostics
		row.startReason = "查找游戏窗口失败。"
	default:
		row.status = readinessUnsatisfied
		row.value = "未找到《绝区零》窗口 → 启动游戏并打开驱动仓库页"
		row.how = "找到后点“重新检查”。"
	}
	if m.targetInvalidated && row.action == readinessActionChooseTarget {
		row.why = "原目标已失效，请重新选择游戏窗口。"
	}
	return row
}

// targetNeedsElevation reports the P-05 shape: the game window is visible
// but its process is denied while youkai runs at a lower integrity level.
// The permission row, not this one, carries the full elevation passage.
func (m *Model) targetNeedsElevation() bool {
	check := m.readinessCheck(app.ReadinessAdministrator)
	return check.Verdict == app.ReadinessUnsatisfied && check.FaultCode == "integrityMismatch"
}

func readinessAdministratorRow(check app.ReadinessCheck) readinessRow {
	row := readinessRow{item: check.Item, title: "管理员权限", status: readinessSatisfied}
	target := readinessKV(check.Current, "target")
	self := readinessKV(check.Current, "self")
	row.why, row.how = readinessParts(check)
	switch {
	case check.Verdict == app.ReadinessSatisfied:
		row.value = fmt.Sprintf("youkai 与游戏权限相同（%s）", readinessIntegrityText(self))
	case target == "missing":
		row.status = readinessUnknown
		row.value = "无法读取游戏权限（未找到窗口） → 需要：与游戏相同"
	case target == "unreadable" || target == "unknown":
		row.status = readinessUnknown
		row.value = "无法读取游戏权限 → 需要：与游戏相同（管理员）"
	case target != "":
		row.status = readinessUnsatisfied
		row.value = fmt.Sprintf("youkai：%s → 需要：与游戏相同（%s）",
			readinessIntegrityText(self), readinessIntegrityText(target))
	default:
		row.status = readinessUnknown
		row.value = "无法读取权限级别 → 需要：与游戏相同"
	}
	if row.status == readinessUnsatisfied || (row.status == readinessUnknown && check.FaultCode != "") {
		row.action = readinessActionExit
	}
	return row
}

func readinessResolutionRow(check app.ReadinessCheck) readinessRow {
	row := readinessRow{item: check.Item, title: "游戏分辨率", status: readinessSatisfied}
	row.why, row.how = readinessParts(check)
	current := readinessSizeText(check.Current)
	required := readinessSizeText(check.Required)
	if required != "" {
		required = strings.Join(strings.Split(required, ","), " 或 ")
	}
	switch check.Verdict {
	case app.ReadinessSatisfied:
		row.value = current + "（窗口或无边框）"
	case app.ReadinessUnsatisfied:
		row.status = readinessUnsatisfied
		row.value = current + " → " + required + "（窗口或无边框）"
	default:
		row.status = readinessUnknown
		if current != "" {
			row.value = current + " → 需要已开放的显示档位（窗口或无边框）"
		} else {
			row.value = "无法读取游戏分辨率 → 需要已开放的显示档位（窗口或无边框）"
		}
	}
	return row
}

func readinessWindowPositionRow(check app.ReadinessCheck) readinessRow {
	row := readinessRow{item: check.Item, title: "窗口位置", status: readinessSatisfied}
	row.why, row.how = readinessParts(check)
	current := readinessRectText(check.Current)
	required := readinessWorkAreaText(check.Required)
	switch check.Verdict {
	case app.ReadinessSatisfied:
		row.value = "客户区完整位于屏幕工作区内"
	case app.ReadinessUnsatisfied:
		row.status = readinessUnsatisfied
		if current != "" {
			row.value = current + " → 完整位于屏幕工作区内"
		} else {
			row.value = "游戏窗口超出屏幕或被任务栏遮挡 → 完整位于屏幕工作区内"
		}
	default:
		row.status = readinessUnknown
		row.value = "无法读取窗口位置 → 需要完整位于屏幕工作区内"
	}
	if required != "" && row.status == readinessSatisfied {
		row.value = current + " · " + required
	}
	return row
}

func readinessDisplayScaleRow(check app.ReadinessCheck) readinessRow {
	row := readinessRow{item: check.Item, title: "显示缩放（DPI）", status: readinessSatisfied}
	row.why, row.how = readinessParts(check)
	switch check.Verdict {
	case app.ReadinessSatisfied:
		row.value = "可读取且未被虚拟化"
	case app.ReadinessUnsatisfied:
		row.status = readinessUnsatisfied
		row.value = "youkai 与游戏窗口、显示器的缩放不一致 → 需要一致"
	default:
		row.status = readinessUnknown
		row.value = "无法读取显示缩放 → 需要可读取且未被虚拟化"
	}
	if check.FaultCode == "dpiAwarenessMismatch" {
		row.value = "youkai 的显示缩放支持不完整 → 这是 youkai 自身的问题"
		// 08 §1.1 requires a diagnostic entry on this row; the shared copy
		// action is the only one it can offer.
		row.action = readinessActionDiagnostics
	}
	if check.FaultCode != "" && check.FaultCode != "dpiAwarenessMismatch" &&
		row.status == readinessUnsatisfied {
		row.value = row.why + " → 需要一致"
	}
	return row
}

func readinessAfterStartRow(check app.ReadinessCheck, title, value string) readinessRow {
	return readinessRow{
		item:   check.Item,
		title:  title,
		status: readinessAfterStart,
		value:  value,
	}
}

func readinessDiskSpaceRow(check app.ReadinessCheck) readinessRow {
	row := readinessRow{item: check.Item, title: "磁盘空间", status: readinessSatisfied}
	required := readinessBytesText(check.Required)
	if required == "" {
		required = formatBytes(app.ReadinessMinFreeBytes)
	}
	current := readinessBytesText(check.Current)
	switch check.Verdict {
	case app.ReadinessSatisfied:
		row.value = fmt.Sprintf("可用 %s（至少 %s）", current, required)
	case app.ReadinessUnsatisfied:
		row.status = readinessUnsatisfied
		row.value = fmt.Sprintf("可用 %s → 至少 %s", current, required)
		row.how = "清理磁盘空间后重新检查。"
		row.action = readinessActionCleanup
	default:
		row.status = readinessUnknown
		row.value = fmt.Sprintf("无法读取可用空间 → 至少 %s", required)
	}
	return row
}

func readinessRecognitionResourcesRow(check app.ReadinessCheck) readinessRow {
	row := readinessRow{item: check.Item, title: "识别资源", status: readinessSatisfied}
	switch check.Verdict {
	case app.ReadinessSatisfied:
		row.value = "安装完整"
	case app.ReadinessUnsatisfied:
		row.status = readinessUnsatisfied
		row.value = "安装不完整 → 需要完整的识别资源"
		row.why = "识别资源缺失或损坏"
		row.how = "请重新安装 youkai。"
		row.action = readinessActionDiagnostics
	default:
		row.status = readinessUnknown
		row.value = "无法确认识别资源"
		row.action = readinessActionDiagnostics
	}
	return row
}

// readinessParts renders a check's fault as the row's why/how pair.
func readinessParts(check app.ReadinessCheck) (string, string) {
	if check.FaultCode == "" {
		return "", ""
	}
	return readinessFailureParts(check.FaultCode, check.FaultParams)
}

func readinessFailureParts(code string, params map[string]string) (string, string) {
	guidance, ok := faultGuidanceFor(code, params)
	if !ok {
		return "", ""
	}
	return guidance.cause, guidance.action
}

// readinessFailureValue names the failed item's conclusion after a
// preparation-stage refusal. The hotkey keeps its Q-B wording; everything
// else uses the shared cause.
func (m *Model) startFailureValue(item app.ReadinessItem) string {
	if item == app.ReadinessStopHotkey {
		return "停止热键已被其他程序占用"
	}
	if cause := faultCauseText(m.startFailureCode, m.startFailureParams); cause != "" {
		return cause
	}
	return m.startFailureMessage
}

// readinessCount counts the rows the header reports as needing attention.
// It includes a presentation-only failure row so the header never says
// 全部满足 beside a red row.
func (m *Model) readinessCount() int {
	attention := 0
	for _, row := range m.readinessRows() {
		if row.status == readinessUnsatisfied || row.status == readinessConfirm {
			attention++
		}
	}
	return attention
}

// readinessBlockingCount counts the rows that actually stop a start.
// Unknown rows and presentation-only failures do not block: the session's
// own precheck stays authoritative, while a row known not to hold must be
// fixed first.
func (m *Model) readinessBlockingCount() int {
	attention := 0
	for _, row := range m.readinessRows() {
		if row.failureOnly {
			continue
		}
		if row.status == readinessUnsatisfied || row.status == readinessConfirm {
			attention++
		}
	}
	return attention
}

func (m *Model) readinessBlocked() bool {
	return m.readinessBlockingCount() > 0
}

// readinessPrimaryAction names the row the keyboard should land on: the
// first row that needs the user and has a control. Diagnostics counts as a
// control on every row except the target row: copying it cannot resolve a
// denied or unreadable target, so 重新检查 is that row's first-entry target.
// The lookup stops there instead of moving to a later row, because the
// first unsatisfied row owns the focus (08 §1). An unsatisfied row with
// only an explanation falls through to the next actionable row; when no row
// offers a control the start button is the target ("" here, with 重新检查
// used when rows remain).
func (m *Model) readinessPrimaryAction() app.ReadinessItem {
	for _, row := range m.readinessRows() {
		if row.status != readinessUnsatisfied && row.status != readinessConfirm {
			continue
		}
		if row.action == readinessActionNone {
			continue
		}
		if row.item == app.ReadinessTargetClient && row.action == readinessActionDiagnostics {
			return ""
		}
		return row.item
	}
	return ""
}

// readinessStartReason is the compact reason shown left of the start
// button. It names one concrete failure, or how many rows remain.
func (m *Model) readinessStartReason() string {
	rows := m.readinessRows()
	var attention []readinessRow
	for _, row := range rows {
		if row.failureOnly {
			continue
		}
		if row.status == readinessUnsatisfied || row.status == readinessConfirm {
			attention = append(attention, row)
		}
	}
	if len(attention) == 0 {
		return ""
	}
	if len(attention) > 1 {
		return fmt.Sprintf("还有 %d 项需要处理", len(attention))
	}
	return readinessShortReason(attention[0])
}

func readinessShortReason(row readinessRow) string {
	if row.startReason != "" {
		return row.startReason
	}
	switch row.item {
	case app.ReadinessTargetClient:
		if row.status == readinessConfirm {
			return "请先确认要扫描的游戏窗口。"
		}
		return "未找到可扫描的游戏窗口。"
	case app.ReadinessAdministrator:
		return "管理员权限不一致。"
	case app.ReadinessGameResolution:
		return "游戏分辨率不受支持。"
	case app.ReadinessWindowPosition:
		return "游戏窗口被遮挡或超出屏幕。"
	case app.ReadinessDisplayScale:
		return "显示缩放不一致。"
	case app.ReadinessStopHotkey:
		return "停止热键被占用。"
	case app.ReadinessDiskSpace:
		return "磁盘空间不足。"
	case app.ReadinessRecognitionResources:
		return "识别资源缺失。"
	case app.ReadinessGamePage:
		return "游戏页面或窗口状态不满足。"
	default:
		return "还有一项需要处理。"
	}
}

// readinessStatusText is the row's short conclusion, drawn before the
// title; the shape plus the word keeps it usable without color.
func readinessStatusText(status readinessRowStatus) (text string, severity string) {
	switch status {
	case readinessSatisfied:
		return "✓", "ok"
	case readinessUnsatisfied:
		return "!", "bad"
	case readinessUnknown:
		return "?", "muted"
	case readinessConfirm:
		return "!", "warn"
	default:
		return "后", "muted"
	}
}

func readinessIntegrityText(level string) string {
	switch level {
	case "high":
		return "管理员"
	case "system":
		return "系统"
	case "protected":
		return "受保护"
	case "mediumPlus":
		return "普通权限（中+）"
	case "medium":
		return "普通权限"
	case "low":
		return "低权限"
	case "untrusted":
		return "不受信任"
	default:
		return "未知"
	}
}

func readinessKV(value, key string) string {
	for _, part := range strings.Fields(value) {
		if k, v, ok := strings.Cut(part, "="); ok && k == key {
			return v
		}
	}
	return ""
}

func readinessSizeText(value string) string {
	return strings.ReplaceAll(value, "x", "×")
}

// readinessBytesText formats the stable decimal byte count the service
// reports; an unparsable or empty value stays empty so the caller can
// choose honest copy.
func readinessBytesText(value string) string {
	n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || n < 0 {
		return ""
	}
	return formatBytes(n)
}

// readinessRectText renders "x,y WxH" as "客户区 (x, y)，W×H". A value the
// service did not report stays empty.
func readinessRectText(value string) string {
	fields := strings.Fields(value)
	if len(fields) != 2 {
		return ""
	}
	origin := strings.SplitN(fields[0], ",", 2)
	size := strings.SplitN(fields[1], "x", 2)
	if len(origin) != 2 || len(size) != 2 {
		return ""
	}
	return fmt.Sprintf("客户区 (%s, %s)，%s×%s", origin[0], origin[1], size[0], size[1])
}

// readinessWorkAreaText extracts the work-area half of the position row's
// required value ("workArea=x,y WxH monitor=...").
func readinessWorkAreaText(required string) string {
	i := strings.Index(required, "workArea=")
	if i < 0 {
		return ""
	}
	rest := required[i+len("workArea="):]
	if j := strings.Index(rest, " monitor="); j >= 0 {
		rest = rest[:j]
	}
	fields := strings.Fields(rest)
	if len(fields) != 2 {
		return ""
	}
	origin := strings.SplitN(fields[0], ",", 2)
	size := strings.SplitN(fields[1], "x", 2)
	if len(origin) != 2 || len(size) != 2 {
		return ""
	}
	return fmt.Sprintf("工作区 (%s, %s)，%s×%s", origin[0], origin[1], size[0], size[1])
}

// scanFailureFromSnapshot derives the "没有开始" state from a session that
// ended before input permission ever opened. Such a session is a refused
// start, not a scan with zero items.
func (m *Model) notePreparationFailure(snap app.Snapshot) {
	if snap.SessionID == "" || snap.Busy || snap.Scan.Phase != scan.PhaseEnded ||
		snap.Scan.InputOpened || snap.Scan.Trigger == nil {
		return
	}
	if m.startFailure == snap.SessionID {
		return
	}
	trigger := snap.Scan.Trigger
	if trigger.Kind != scan.TriggerFault && trigger.Kind != scan.TriggerUser {
		return
	}
	m.startFailure = snap.SessionID
	m.startFailureCode = trigger.Code
	m.startFailureParams = trigger.Params
	m.startFailureItem = readinessItemForFault(trigger.Code)
	m.startFailureReadinessSerial = m.readinessSerial
	if trigger.Kind == scan.TriggerUser {
		m.startFailureMessage = "没有开始：已手动停止"
	} else if cause := faultCauseText(trigger.Code, trigger.Params); cause != "" {
		m.startFailureMessage = "没有开始：" + cause
	} else {
		m.startFailureMessage = "没有开始：原因未知"
	}
	// Returning from a refused start moves the keyboard to the failed
	// row's control, or to 重新检查 when it has none. This is a one-shot
	// request, not the first-entry rule.
	m.readinessFocus = true
	if m.readinessPrimaryAction() == "" {
		m.readinessFocusRefresh = true
	}
	m.refreshReadiness()
}

// readinessItemForFault maps a fault's recovery exit to the checklist row
// that must be promoted (08 §1.2). Hotkey failure is the Q-B case.
func readinessItemForFault(code string) app.ReadinessItem {
	if code == "hotkeyUnavailable" {
		return app.ReadinessStopHotkey
	}
	guidance, ok := faultGuidanceFor(code, nil)
	if !ok {
		return ""
	}
	switch guidance.exit {
	case faultExitTargetClient:
		return app.ReadinessTargetClient
	case faultExitAdmin:
		return app.ReadinessAdministrator
	case faultExitResolution:
		return app.ReadinessGameResolution
	case faultExitWindowPosition:
		return app.ReadinessWindowPosition
	case faultExitDisplayScale:
		return app.ReadinessDisplayScale
	case faultExitHotkey:
		return app.ReadinessStopHotkey
	case faultExitGamePage:
		return app.ReadinessGamePage
	default:
		return ""
	}
}

// scanArea is the scan page's one stable region: the checklist while idle,
// the live stage while a session runs, and the save failure while a result
// waits for its retry (08 §1.2).
func (m *Model) scanArea(c *ui.Context, t *ui.Theme, tok *Tokens) {
	m.rescanBanner(c, t, tok)
	switch {
	case !m.source.Available:
		m.noScanSourceArea(c, t)
	case m.snap.SaveError != "":
		m.scanSaveFailureArea(c, t)
	case m.snap.Busy:
		m.scanStageArea(c, t)
	default:
		m.readinessChecklist(c, t, tok)
	}
}

func (m *Model) noScanSourceArea(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Gap(t.Space(2)).Children(func() {
		ui.Text(c, "这个版本还不能扫描").FontSize(t.FontSize * 1.25).FontWeight(600)
		if m.source.Reason != "" {
			ui.Text(c, m.source.Reason).TextColor(t.TextMuted)
		}
		ui.Text(c, "记录和设置照常可用。").TextColor(t.TextMuted)
	})
}

// readinessChecklist renders 08 §1.1: every row, all details open, the
// rows that need the user first.
func (m *Model) readinessChecklist(c *ui.Context, t *ui.Theme, tok *Tokens) {
	rows := m.readinessRows()
	attention := m.readinessCount()
	positions := "1–6"
	if p := m.pendingRescan; p != nil {
		positions = summaryPositionRanges(p.positions)
	}
	unknown := 0
	for _, row := range rows {
		if row.status == readinessUnknown {
			unknown++
		}
	}
	ui.Column(c).Gap(t.Space(2)).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "准备检查").FontSize(t.FontSize * 16 / 14).FontWeight(600)
			if attention > 0 {
				ui.Textf(c, "! %d 项需要处理", attention).TextColor(t.Warning)
			} else if m.readinessLoaded && unknown > 0 {
				ui.Textf(c, "%d 项无法判断", unknown).TextColor(t.TextMuted)
			} else if m.readinessLoaded {
				ui.Text(c, "全部满足 · 可以开始").TextColor(t.Success)
			}
			ui.Box(c).Grow(1)
			ui.Text(c, "S 级 · "+positions+" 号位").TextColor(tok.TextSecondary).Background(tok.AccentSoft).Padding(4, 12).Radius(4)
		})
		if m.startFailureMessage != "" {
			ui.Text(c, m.startFailureMessage).TextColor(t.Danger).FontWeight(600)
		}
		ui.Column(c).Background(tok.SurfaceCard).Children(func() {
			for _, row := range rows {
				m.readinessRowView(c, t, tok, row)
			}
		})
		if m.diagnosticsNotice != "" {
			ui.Text(c, m.diagnosticsNotice).TextColor(t.TextMuted).FontSize(t.FontSize * 0.9)
		}
		if m.diagnosticsErr != "" {
			ui.Text(c, m.diagnosticsErr).TextColor(t.Danger).FontSize(t.FontSize * 0.9)
		}
	})
}

func (m *Model) readinessRowView(c *ui.Context, t *ui.Theme, tok *Tokens, row readinessRow) {
	primary := m.readinessPrimaryAction() == row.item
	_, severity := readinessStatusText(row.status)
	status := map[readinessRowStatus]string{readinessSatisfied: "已满足", readinessUnsatisfied: "需处理", readinessUnknown: "待检查", readinessAfterStart: "开始后检查", readinessConfirm: "待确认"}[row.status]
	color, glyph := tok.AccentFG, "◷"
	switch severity {
	case "ok":
		color, glyph = tok.SuccessFG, "✓"
	case "bad":
		color, glyph = tok.DangerFG, "!"
	case "warn":
		color, glyph = tok.WarningFG, "!"
	}
	ui.Column(c).Gap(8).Padding(16, 0).Children(func() {
		ui.Row(c).Gap(16).MinHeight(24).AlignItems(ui.Center).Children(func() {
			ui.Text(c, glyph).TextColor(color).Width(24).Shrink(0)
			// Reserve the full longest label using the actual runtime font and
			// text scale. The current-value column receives the remaining width.
			labelWidth, _ := c.MeasureText(0, ui.Span{Text: "游戏页面、遮挡、独占全屏", Size: t.FontSize})
			ui.Text(c, row.title).Width(float32(math.Ceil(float64(labelWidth)/4)) * 4).Shrink(0)
			ui.Text(c, row.value).Grow(1).Basis(0).MinWidth(0)
			if label, ok := readinessActionLabel(row.action); ok {
				action := button(c, label)
				if primary {
					action = action.Bind(&m.scanRowRef)
					if m.readinessFocus {
						action.Focus()
						m.readinessFocus = false
					}
				}
				if action.Clicked() {
					m.runReadinessAction(c, row.action)
				}
			}
			ui.Text(c, status).TextColor(color).FontSize(t.FontSize * 12 / 14).Shrink(0)
		})
		if row.why != "" {
			ui.Text(c, "为什么："+row.why).TextColor(t.TextMuted).FontSize(t.FontSize * 12 / 14)
		}
		if row.how != "" {
			ui.Text(c, "怎么做："+row.how).TextColor(t.TextMuted).FontSize(t.FontSize * 12 / 14)
		}
		if row.action == readinessActionChooseTarget {
			m.readinessTargetChoices(c, primary)
		}
	})
	ui.Divider(c)
}

func readinessActionLabel(action readinessActionKind) (string, bool) {
	switch action {
	case readinessActionExit:
		return "退出 youkai", true
	case readinessActionCleanup:
		return "去清理", true
	case readinessActionDiagnostics:
		return "复制诊断信息", true
	case readinessActionChangeTarget:
		return "更换目标", true
	default:
		return "", false
	}
}

func (m *Model) runReadinessAction(c *ui.Context, action readinessActionKind) {
	switch action {
	case readinessActionExit:
		m.requestExit(false)
	case readinessActionCleanup:
		m.ShowStorageFromPrepareCheck()
	case readinessActionDiagnostics:
		m.copyDiagnostics(c)
	case readinessActionChangeTarget:
		// Asking for another window retires the failure of the old choice,
		// like a new confirmation; the past event belongs to that choice.
		m.startErr = ""
		m.targetChoice = -1
		m.targetSerial++
		m.targetNeedsChoice = true
	}
}

// readinessTargetChoices is the in-row radio list replacing PR-05's
// temporary prepare-area list. A single candidate appears only when a
// previous instance was invalidated or the user explicitly changes target.
func (m *Model) readinessTargetChoices(c *ui.Context, primary bool) {
	before := m.targetChoice
	ui.RadioGroup(c, func() {
		for i, candidate := range m.targets {
			radio := ui.Radio(c, &m.targetChoice, i, targetLabel(candidate))
			if i == 0 && primary {
				radio = radio.Bind(&m.scanRowRef)
				if m.readinessFocus {
					radio = radio.Focus()
					m.readinessFocus = false
				}
			}
		}
	}).Label("目标客户端")
	if m.targetChoice != before {
		m.targetNeedsChoice = false
		m.targetInvalidated = false
		m.targetSerial++
		// Confirming the window lets the remaining rows judge that window
		// instead of staying unknown (R-01). The start is not blocked by
		// this refresh; the session's own precheck remains authoritative.
		// A new confirmation also retires the refusal of the earlier choice.
		m.startErr = ""
		m.refreshReadiness()
	}
}

// scanStageArea is 08 §1.2's stage block. The countdown is the page's own
// calculation from the reported deadline; no per-tick revision is spent on
// it. Counts are limited to captured items (R-11's first version).
func (m *Model) scanStageArea(c *ui.Context, t *ui.Theme) {
	snap := m.snap.Scan
	tok := m.effectiveTokens()
	stage := ui.Column(c).Gap(24)
	if snap.Phase == scan.PhaseCapturing || snap.Phase == scan.PhaseFinalizing {
		width := min(float32(800), float32(m.contentWidth)-m.railWidth()-m.pageLeftPadding()-m.pagePadding()-48)
		stage = stage.Width(width).AlignSelf(ui.Center).MinHeight(max(280, float32(m.contentHeight)-300)).Justify(ui.Center)
	}
	stage.Children(func() {
		switch snap.Phase {
		case scan.PhasePreparing:
			ui.Text(c, m.prepareCountdownText()).FontSize(t.FontSize * 2.6).FontWeight(600)
			ui.Text(c, "请在倒计时结束前让游戏保持在前台")
			ui.Text(c, "正在等待游戏就绪").TextColor(t.TextMuted)
			switch snap.Activation {
			case scan.ActivationActivated:
				ui.Text(c, "已把游戏切到前台").TextColor(t.Success)
			case scan.ActivationRefused:
				ui.Text(c, "未能切换，请自己切换到游戏").TextColor(t.Warning)
			}
			ui.Text(c, "请确保 youkai 没有盖住游戏窗口").TextColor(t.TextMuted)
			m.schedulePrepareTick()
		case scan.PhaseCapturing:
			scanStageHeading(c, t, tok, "正在扫描")
			ui.Row(c).Gap(24).Children(func() {
				scanMetric(c, t, tok, "已采集", formatCount(int64(snap.Counts.Captured))+" 件")
				position := "—"
				if m.snap.DisplayPosition >= 1 && m.snap.DisplayPosition <= 6 {
					position = fmt.Sprintf("%d 号位", m.snap.DisplayPosition)
				}
				scanMetric(c, t, tok, "当前号位", position)
			})

		case scan.PhaseFinalizing:
			scanStageHeading(c, t, tok, "识别和保存")
			remaining := snap.Counts.Queued + snap.Counts.Running
			ui.Row(c).Gap(24).Children(func() {
				scanMetric(c, t, tok, "已采集", formatCount(int64(snap.Counts.Captured))+" 件")
				scanMetric(c, t, tok, "剩余识别", formatCount(int64(remaining))+" 件")
			})
			if remaining > 0 {
				ui.Textf(c, "已停止操作游戏，正在识别剩余 %d 件并保存", remaining)
			} else {
				ui.Text(c, "已停止操作游戏，正在保存结果")
			}
		default:
			ui.Text(c, "正在保存结果").FontSize(t.FontSize * 24 / 14).FontWeight(700)
		}
	})
}

// prepareCountdownText formats the remaining preparation budget; a
// missing deadline says so instead of inventing 30 seconds.
func (m *Model) prepareCountdownText() string {
	deadline := m.snap.Scan.PrepareDeadline
	if deadline.IsZero() {
		return "正在准备…"
	}
	remaining := deadline.Sub(m.now())
	if remaining <= 0 {
		return "0 秒"
	}
	seconds := int(remaining.Round(time.Second) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	return fmt.Sprintf("%d 秒", seconds)
}

// schedulePrepareTick redraws the countdown once a second while the
// session prepares. Like the readiness timer it is a plain update, not
// model-owned work, and tests without After drive frames directly.
func (m *Model) schedulePrepareTick() {
	if m.cfg.After == nil || m.prepareTimerArmed {
		return
	}
	m.prepareTimerArmed = true
	ch := m.cfg.After(time.Second)
	go func() {
		<-ch
		m.update(func() { m.prepareTimerArmed = false })
	}()
}

// scanSaveFailureArea raises the unsaved result to the page's primary
// problem (08 §1.2): there is no new scan, update or exit until the retry
// succeeds.
func (m *Model) scanSaveFailureArea(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Gap(t.Space(2)).Children(func() {
		ui.Text(c, "扫描结果还没有保存").FontSize(t.FontSize * 1.35).FontWeight(600).TextColor(t.Danger)
		ui.Text(c, m.snap.SaveError).TextColor(t.Danger)
		if m.snap.SavedCount > 0 {
			ui.Textf(c, "已保存 %d 条；其余已采集结果尚未保存。", m.snap.SavedCount).TextColor(t.TextMuted)
		} else {
			ui.Textf(c, "本次扫描已采集的 %d 件结果还没有写入本机。", m.snap.Scan.Counts.Captured).
				TextColor(t.TextMuted)
		}
		ui.Text(c, "此时不能开始新扫描、不能安装更新，退出会被拒绝。").TextColor(t.TextMuted)
		ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
			if !m.exitFailureDialogOpen && primaryButton(c, "重试保存").Clicked() {
				m.RetrySave()
			}
			if button(c, "复制诊断信息").Clicked() {
				m.copyDiagnostics(c)
			}
		})
	})
}

func (m *Model) scanHeader(c *ui.Context, t *ui.Theme) {
	pageHeader(c).Children(func() {
		pageTitle(c, "扫描")
		ui.Box(c).Grow(1)
		if m.snap.Busy {
			return
		}
		if m.readinessLoading {
			ui.Text(c, "正在检查…").TextColor(t.TextMuted)
		} else if !m.readiness.CheckedAt.IsZero() {
			ui.Text(c, m.readiness.CheckedAt.Local().Format("15:04")+" 检查").FontSize(t.FontSize * 12 / 14).TextColor(t.TextMuted)
		}
		refresh := button(c, "重新检查").Bind(&m.scanRefreshRef)
		if m.readinessFocus && m.readinessFocusRefresh {
			refresh.Focus()
			m.readinessFocus = false
			m.readinessFocusRefresh = false
		}
		if refresh.Clicked() {
			m.startErr = ""
			m.refreshReadiness()
		}
	})
}

func scanMetric(c *ui.Context, t *ui.Theme, tok *Tokens, label, value string) {
	panel(c, tok).Background(tok.SurfaceSunken).Grow(1).Basis(0).MinHeight(176).Padding(24).Gap(24).Children(func() {
		ui.Text(c, label).TextColor(tok.TextSecondary)
		parts := strings.Fields(value)
		ui.Row(c).Gap(8).AlignItems(ui.End).Children(func() {
			if len(parts) > 0 {
				ui.Text(c, parts[0]).FontSize(t.FontSize * 48 / 14).FontWeight(700).TextColor(tok.AccentFG)
			}
			if len(parts) > 1 {
				ui.Text(c, strings.Join(parts[1:], " "))
			}
		})
	})
}

func scanStageHeading(c *ui.Context, t *ui.Theme, tok *Tokens, label string) {
	ui.Row(c).Gap(24).AlignItems(ui.Center).Children(func() {
		ui.Row(c).Size(80, 80).Radius(16).Background(tok.AccentSoft).Center().Children(func() { ui.Icon(c, scanIcon).Size(32, 32).TextColor(tok.AccentFG) })
		ui.Column(c).Gap(8).Children(func() {
			ui.Text(c, label).FontSize(t.FontSize * 32 / 14).FontWeight(700)
			ui.Text(c, "绝区零 · 驱动盘").TextColor(tok.TextSecondary)
		})
	})
}

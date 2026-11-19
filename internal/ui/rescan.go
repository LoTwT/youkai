package ui

import (
	"errors"
	"fmt"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
	"github.com/egoist/mygo/ui"
)

// P-12's first UI slice: choose the incomplete positions of a saved record
// and carry that range into an ordinary start. The intent is bound to the
// preparation/start result: a Busy answer, a refusal that may recover, or a
// preparation failure before any input opened keeps the same source and
// selection for a retry. Only an actual scan start, an explicit cancel or
// switch, or an explicit context change clears it — a later start never
// silently becomes a whole-warehouse scan.

// pendingRescan is the exact range the next start requests. The source label
// is display-only; the service proves the ids again from the store.
type pendingRescan struct {
	sourceID    string
	sourceLabel string
	positions   []int
	// blocked marks a range the service refused for good (source or scope
	// no longer valid). Starts are disabled until the user explicitly
	// returns to the record or switches to a whole-warehouse scan.
	blocked       bool
	blockedReason string
	// retryNotice explains a retryable preparation failure on the banner.
	retryNotice string
}

func (p *pendingRescan) request() app.RescanRequest {
	return app.RescanRequest{
		SourceRecordID: p.sourceID,
		Positions:      append([]int(nil), p.positions...),
	}
}

// rescanAttempt ties one accepted start to the session that will prove
// whether the scan actually began. intent is the exact prepared instance:
// selecting the same source and positions again creates a new instance,
// so an older response or snapshot cannot consume it.
type rescanAttempt struct {
	intent       *pendingRescan
	sessionID    string
	snapshotSeen bool
}

// rescanDialog is the summary's position picker. chosen is indexed by
// wheel position (1..6), so the checkbox addresses stay stable across
// frames.
type rescanDialog struct {
	open        bool
	sourceID    string
	sourceLabel string
	candidates  []app.PositionOutcome
	chosen      [record.PositionCount + 1]bool
}

// openRescanDialog starts a fresh picker for the open record. Every offered
// position starts checked; the user can uncheck any of them.
func (m *Model) openRescanDialog() {
	candidates, ok := m.rescanCandidates()
	if !ok || len(candidates) == 0 {
		return
	}
	m.rescanNotice = ""
	dialog := rescanDialog{
		open:        true,
		sourceID:    m.rec.ID,
		sourceLabel: m.rescanSourceLabel(),
		candidates:  candidates,
	}
	for _, outcome := range candidates {
		dialog.chosen[outcome.Position] = true
	}
	m.rescanDialog = dialog
}

// rescanSourceLabel names the record the range came from without inventing
// a name the record does not carry.
func (m *Model) rescanSourceLabel() string {
	if m.rec == nil {
		return ""
	}
	if !m.rec.Shell.StartedAt.IsZero() {
		return m.rec.Shell.StartedAt.Local().Format("2006-01-02 15:04") + " · " + m.rec.ID
	}
	return m.rec.ID
}

// rescanDialogPositions returns the checked positions in traversal order.
func (m *Model) rescanDialogPositions() []int {
	var positions []int
	for _, outcome := range m.rescanDialog.candidates {
		if m.rescanDialog.chosen[outcome.Position] {
			positions = append(positions, outcome.Position)
		}
	}
	return positions
}

// confirmRescanDialog arms the range through the shared navigation guard.
// It revalidates the record identity and the current eligible options, so a
// picker left open while the record changed cannot confirm the old range.
func (m *Model) confirmRescanDialog() {
	if m.rec == nil || m.rec.ID != m.rescanDialog.sourceID {
		m.rescanDialog.open = false
		return
	}
	candidates, ok := m.rescanCandidates()
	if !ok || len(candidates) == 0 {
		m.rescanDialog.open = false
		m.rescanNotice = "记录或号位状态已经变化，请重新选择补扫号位。"
		return
	}
	available := make(map[int]bool, len(candidates))
	for _, outcome := range candidates {
		available[outcome.Position] = true
	}
	positions := m.rescanDialogPositions()
	if len(positions) == 0 {
		return
	}
	for _, position := range positions {
		if !available[position] {
			m.rescanDialog.open = false
			m.rescanNotice = "选中的号位已经变化，请重新选择补扫号位。"
			return
		}
	}
	req := app.RescanRequest{
		SourceRecordID: m.rescanDialog.sourceID,
		Positions:      positions,
	}
	label := m.rescanSourceLabel()
	m.rescanDialog.open = false
	m.requestNavigation(navigationRequest{
		kind:            navPrepareRescan,
		page:            pageScan,
		rescan:          &req,
		rescanSourceTag: label,
	})
}

// armRescan applies a confirmed range on the scan page. The caller reached
// here through the navigation guard, so no cancelled editor prompt can arm
// it by accident.
func (m *Model) armRescan(req app.RescanRequest, sourceLabel string) {
	m.pendingRescan = &pendingRescan{
		sourceID:    req.SourceRecordID,
		sourceLabel: sourceLabel,
		positions:   append([]int(nil), req.Positions...),
	}
	m.rescanAttempt = nil
	m.rescanDialog.open = false
	m.startErr = ""
}

// clearPendingRescan drops the armed range and any attempt tracking.
func (m *Model) clearPendingRescan() {
	m.pendingRescan = nil
	m.rescanAttempt = nil
}

// dropRescanContext drops every leave-with-context rescan state: the armed
// range, its attempt, and an open picker. Navigation and explicit record
// opens call this in the places that already own context changes.
func (m *Model) dropRescanContext() {
	m.clearPendingRescan()
	m.rescanDialog.open = false
	m.rescanNotice = ""
}

// rescanStartBlocked reports a non-retryable range refusal. Starts stay
// disabled until the user explicitly returns to the source record or
// switches to a whole-warehouse scan.
func (m *Model) rescanStartBlocked() bool {
	return m.pendingRescan != nil && m.pendingRescan.blocked
}

// switchToFullScan is the explicit alternative after a blocked rescan.
func (m *Model) switchToFullScan() {
	m.dropRescanContext()
	m.rescanNotice = ""
	m.startErr = ""
}

// returnToRescanSource routes back to the source record through the shared
// navigation guard. If the draft prompt is cancelled the rescan state stays;
// if the navigation proceeds, runNavigation drops the range and opens the
// record so the user can select again.
func (m *Model) returnToRescanSource() {
	p := m.pendingRescan
	if p == nil || p.sourceID == "" {
		m.dropRescanContext()
		m.rescanNotice = "补扫来源记录已不可用，请从记录列表重新选择。"
		return
	}
	m.requestNavigation(navigationRequest{kind: navOpenRecord, recordID: p.sourceID})
}

// observeRescanStartResult consumes one Start answer. Busy means this
// rescan did not start and the range stays armed. A source/scope refusal is
// final for this range; transient refusals keep it for the next click.
func (m *Model) observeRescanStartResult(intent *pendingRescan, req app.StartRequest,
	outcome app.StartOutcome, err error) {
	if req.Rescan == nil || intent == nil || m.pendingRescan != intent {
		// A response for an old instance must not write to, consume or
		// block the replacement the user just prepared.
		return
	}
	if err != nil {
		if rescanScopeRefusal(err) {
			m.pendingRescan.blocked = true
			m.pendingRescan.blockedReason = fmt.Sprintf("%s 请返回原记录重新选择号位，或明确改为整仓扫描。",
				startErrorText(err))
			m.rescanAttempt = nil
		}
		return
	}
	if outcome.Busy && !outcome.Replayed {
		return
	}
	if outcome.Replayed && !outcome.Busy {
		// The request was already served and that session has ended. It
		// cannot be started again, but it also has no active snapshot to
		// wait for; leave the range retryable under a fresh request id.
		return
	}
	if outcome.SessionID == "" {
		return
	}
	m.rescanAttempt = &rescanAttempt{
		intent:    intent,
		sessionID: string(outcome.SessionID),
	}
	// The subscription may already have delivered the session's snapshot
	// before this answer landed; apply it now so a fast pre-input failure or
	// an opened input is not missed.
	m.observeRescanSnapshot(m.snap)
}

// rescanAwaitingSnapshot reports an accepted rescan whose session snapshot
// has not reached the model yet. A second Start must wait: the accepted
// session already exists even though the old idle snapshot still says
// otherwise.
func (m *Model) rescanAwaitingSnapshot() bool {
	return m.rescanAttempt != nil && !m.rescanAttempt.snapshotSeen &&
		m.pendingRescan != nil && m.rescanAttempt.intent == m.pendingRescan
}

// rescanScopeRefusal reports the refusals that a retry with the same source
// and positions cannot fix.
func rescanScopeRefusal(err error) bool {
	return errors.Is(err, app.ErrRescanUnsupported) ||
		errors.Is(err, app.ErrRescanRequest) ||
		errors.Is(err, app.ErrRescanSource) ||
		errors.Is(err, app.ErrRescanScope)
}

// observeRescanSnapshot binds the pending intent to the accepted session's
// actual start. Once input opens, this start consumed the intent and later
// scans are ordinary. A session that ends before input opens keeps the
// intent for a retry and clears only its attempt marker.
func (m *Model) observeRescanSnapshot(snap app.Snapshot) {
	a := m.rescanAttempt
	if a == nil || snap.SessionID == "" || string(snap.SessionID) != a.sessionID {
		return
	}
	if m.pendingRescan == nil || m.pendingRescan != a.intent {
		// The replacement intent already owns any later start; the old
		// session's reports cannot consume or annotate it.
		m.rescanAttempt = nil
		return
	}
	a.snapshotSeen = true
	if snap.Scan.InputOpened {
		m.pendingRescan = nil
		m.rescanAttempt = nil
		return
	}
	if snap.Scan.Phase == scan.PhaseEnded {
		m.rescanAttempt = nil
		m.pendingRescan.retryNotice = "上次补扫没有开始；修正准备检查后可以重试同一范围。"
	}
}

// rescanDialogView renders the position picker over the record workspace.
func (m *Model) rescanDialogView(c *ui.Context, t *ui.Theme) {
	if !m.rescanDialog.open {
		return
	}
	// A record change or a fresh eligibility read closes the picker before
	// it can confirm an old source.
	if m.rec == nil || m.rec.ID != m.rescanDialog.sourceID {
		m.rescanDialog.open = false
		return
	}
	if candidates, ok := m.rescanCandidates(); !ok || len(candidates) == 0 {
		m.rescanDialog.open = false
		return
	}
	open := true
	tok := m.effectiveTokens()
	m.modal(c, &open, func() {
		ui.Column(c).Width(t.Space(112)).Gap(t.Space(2)).Children(func() {
			ui.Text(c, "补扫不完整号位").FontWeight(600)
			ui.Text(c, "来源记录："+m.rescanDialog.sourceLabel).TextColor(t.TextMuted)
			ui.Text(c, "补扫会新建一条独立记录，只访问选中的号位；原记录、图片、修订和导出的文件都不会改变。").
				TextColor(t.TextMuted)
			ui.Column(c).Gap(t.Space(1)).Children(func() {
				for _, outcome := range m.rescanDialog.candidates {
					position := outcome.Position
					ui.Checkbox(c, &m.rescanDialog.chosen[position],
						fmt.Sprintf("%d 号位 · %s", position, rescanCandidateDetail(outcome)))
				}
			})
			if len(m.rescanDialogPositions()) == 0 {
				ui.Text(c, "至少选择一个号位。").TextColor(tok.WarningFG).FontSize(t.FontSize * 0.9)
			}
			actionRow(c).Wrap().Children(func() {
				if button(c, "取消").Clicked() {
					open = false
				}
				selected := m.rescanDialogPositions()
				label := "准备补扫"
				if len(selected) > 0 {
					label = fmt.Sprintf("准备补扫（%s 号位）", summaryPositionRanges(selected))
				}
				start := primaryButton(c, label).Disabled(len(selected) == 0)
				if start.Clicked() {
					open = false
					m.confirmRescanDialog()
				}
			})
		})
	})
	if !open && m.rescanDialog.open {
		m.rescanDialog.open = false
	}
}

// rescanCandidateDetail is the picker's reason text: the same stage and
// cause wording the summary uses, without repeating the position number.
func rescanCandidateDetail(outcome app.PositionOutcome) string {
	switch outcome.State {
	case app.PositionOutcomeDegraded:
		detail := "不完整 · " + positionStageText(outcome.Stage)
		if cause := faultCauseText(outcome.ReasonCode, nil); cause != "" {
			detail += "：" + cause
		}
		return detail
	case app.PositionOutcomeTruncated:
		return "未完成 · 扫描在这里停止"
	case app.PositionOutcomeNotReached:
		return "未到达"
	default:
		return "完整"
	}
}

// rescanBanner states the armed range on the preparation page. The start
// button keeps its ordinary behavior and the service still runs the full
// precheck.
func (m *Model) rescanBanner(c *ui.Context, t *ui.Theme, tok *Tokens) {
	p := m.pendingRescan
	if p == nil {
		return
	}
	if p.blocked {
		m.blockedRescanBanner(c, t, tok, p)
		return
	}
	starting := m.snap.Busy || m.startReqID != "" || m.rescanAwaitingSnapshot()
	ui.Column(c).Gap(8).Padding(16).Radius(controlRadius).
		Background(tok.AccentSoft).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, fmt.Sprintf("补扫准备：只扫描 %s 号位", summaryPositionRanges(p.positions))).
				FontWeight(600)
			ui.Box(c).Grow(1)
			if !starting {
				if button(c, "取消补扫").Clicked() {
					m.clearPendingRescan()
					m.startErr = ""
				}
			}
		})
		ui.Text(c, "来源记录："+p.sourceLabel).TextColor(tok.TextSecondary)
		if starting {
			ui.Text(c, "正在按此范围开始；输入许可打开前失败会保留同一范围供重试。").
				TextColor(tok.TextSecondary).FontSize(t.FontSize * 0.9)
		} else {
			ui.Text(c, "开始后会新建一条独立记录；原记录、图片、修订和导出的文件都不会改变。").
				TextColor(tok.TextSecondary).FontSize(t.FontSize * 0.9)
		}
		if p.retryNotice != "" {
			ui.Text(c, p.retryNotice).TextColor(tok.WarningFG).FontSize(t.FontSize * 0.9)
		}
	})
}

// blockedRescanBanner offers the two explicit ways out of a final range
// refusal: reselect the source record or deliberately switch to a full
// scan. The start button stays disabled while this state is present.
func (m *Model) blockedRescanBanner(c *ui.Context, t *ui.Theme, tok *Tokens, p *pendingRescan) {
	ui.Column(c).Gap(8).Padding(16).Radius(controlRadius).
		Background(tok.WarningSoft).Children(func() {
		ui.Text(c, "补扫范围需要重新选择").FontWeight(600).TextColor(tok.WarningFG)
		ui.Text(c, p.blockedReason).TextColor(tok.WarningFG)
		ui.Text(c, fmt.Sprintf("原范围：%s 号位 · 来源 %s", summaryPositionRanges(p.positions), p.sourceLabel)).
			TextColor(tok.TextSecondary).FontSize(t.FontSize * 0.9)
		actionRow(c).Wrap().Children(func() {
			if primaryButton(c, "返回记录重新选择").Clicked() {
				m.returnToRescanSource()
			}
			if button(c, "改为整仓扫描").Clicked() {
				m.switchToFullScan()
			}
		})
	})
}

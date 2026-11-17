package ui

import (
	"strconv"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/scan"
	"github.com/egoist/mygo/ui"
)

// Settings uses all available width for five vertically scrolling sections.
const settingsSectionInset = 24

// settingsSectionID names a section anchor for business entry points.
// Zero means no pending jump.
type settingsSectionID int

const (
	settingsSectionNone settingsSectionID = iota
	settingsSectionGeneral
	settingsSectionShortcuts
	settingsSectionStorage
	settingsSectionDiagnostics
	settingsSectionUpdate
)

// settingsSectionRefs retain the anchors used by business entry points.
type settingsSectionRefs struct {
	general     ui.Ref
	shortcuts   ui.Ref
	storage     ui.Ref
	diagnostics ui.Ref
	update      ui.Ref
}

func (r *settingsSectionRefs) ref(id settingsSectionID) *ui.Ref {
	switch id {
	case settingsSectionGeneral:
		return &r.general
	case settingsSectionShortcuts:
		return &r.shortcuts
	case settingsSectionStorage:
		return &r.storage
	case settingsSectionDiagnostics:
		return &r.diagnostics
	case settingsSectionUpdate:
		return &r.update
	default:
		return nil
	}
}

// settingsPage keeps the prepare-check return strip and the five sections
// in one full-width scrolling column, without an in-page directory.
func (m *Model) settingsPage(c *ui.Context, t *ui.Theme) {
	tok := m.effectiveTokens()
	m.applySettingsScrollTarget(c)
	if m.settingsReturnToPrepare {
		m.settingsReturnStrip(c, tok)
	}
	contentPanel(c, tok).Children(func() {
		ui.Scroll(c).Grow(1).MinHeight(0).FillWidth().FillHeight().TrackScroll(&m.settingsScroll).Bind(&m.settingsViewportRef).
			Children(func() {
				ui.Column(c).FillWidth().Gap(8).Children(func() {
					m.settingsSection(c, t, settingsSectionGeneral, "常规", func() {
						m.settingsGeneral(c, t, tok)
					})
					m.settingsSection(c, t, settingsSectionShortcuts, "快捷键", func() {
						m.settingsShortcuts(c, t, tok)
					})
					m.settingsSection(c, t, settingsSectionStorage, "存储与清理", func() {
						m.settingsStorage(c, t, tok)
					})
					m.settingsSection(c, t, settingsSectionDiagnostics, "诊断", func() {
						m.settingsDiagnostics(c, t, tok)
					})
					m.settingsSection(c, t, settingsSectionUpdate, "更新", func() {
						m.updateArea(c, t, tok)
						m.settingsLicenses(c, t, tok)
					})
				})
			})
	})
}

// settingsReturnStrip is the prepare check's entry promise: cleaning up
// here returns to the prepare check when it completes. The strip is not the
// update banner the design removed from the page top.
func (m *Model) settingsReturnStrip(c *ui.Context, tok *Tokens) {
	ui.Row(c).AlignItems(ui.Center).Background(tok.AccentSoft).
		Radius(c.Theme().Space(1.5)).Padding(c.Theme().Space(1.25), c.Theme().Space(2)).
		Children(func() {
			ui.Text(c, "完成后返回准备检查").TextColor(tok.AccentFG)
		})
}

// settingsSection retains its inset and jump anchor within the shared panel.
func (m *Model) settingsSection(c *ui.Context, t *ui.Theme,
	id settingsSectionID, title string, body func()) {
	ref := m.settingsSections.ref(id)
	column := ui.Column(c).FillWidth().Padding(12, settingsSectionInset).Gap(16)
	if ref != nil {
		column = column.Bind(ref)
	}
	column.Children(func() {
		ui.Text(c, title).FontSize(t.FontSize * 1.15).FontWeight(700)
		body()
	})
}

// jumpSettingsSection scrolls the content so the section starts at the top
// of the viewport. It uses the previous frame's committed boxes, so a click
// can move the scroll in the very frame it happens; when a box is not
// committed yet, the target waits for the next frame.
func (m *Model) jumpSettingsSection(c *ui.Context, id settingsSectionID) {
	if !m.scrollSettingsSection(c, id) {
		m.settingsScrollTarget = id
	}
}

func (m *Model) applySettingsScrollTarget(c *ui.Context) {
	if m.settingsScrollTarget == settingsSectionNone {
		return
	}
	if m.scrollSettingsSection(c, m.settingsScrollTarget) {
		m.settingsScrollTarget = settingsSectionNone
	}
}

func (m *Model) scrollSettingsSection(c *ui.Context, id settingsSectionID) bool {
	ref := m.settingsSections.ref(id)
	if ref == nil {
		return true
	}
	viewport := m.settingsViewportRef.Bounds(c)
	section := ref.Bounds(c)
	if viewport.H <= 0 || section.H <= 0 {
		return false
	}
	m.settingsScroll.Y += section.Y - viewport.Y
	if m.settingsScroll.Y < 0 {
		m.settingsScroll.Y = 0
	}
	return true
}

// settingsGeneral holds appearance and the close-window preference.
// The default hides to the notification area; the explicit choice
// runs the stop/save/release wrap-up. Without a tray entry hiding has no way
// back, so the close option is fixed to the explicit exit and the reason is
// stated (08 §7).
func (m *Model) settingsGeneral(c *ui.Context, t *ui.Theme, tok *Tokens) {
	m.settingsAppearance(c, t, tok)
	ui.Text(c, "点击窗口的 ✕ 时").TextColor(tok.TextSecondary)
	if m.TrayUnavailable() != "" {
		fixed := string(app.CloseBehaviorExit)
		ui.RadioGroup(c, func() {
			ui.Radio(c, &fixed, string(app.CloseBehaviorExit), "停止扫描并退出").
				Disabled(true)
		})
		ui.Text(c, "通知区域图标不可用。关闭窗口会停止扫描并退出应用。").
			TextColor(t.TextMuted).FontSize(t.FontSize * 0.9)
		return
	}
	selected := 0
	if m.CloseBehavior() == app.CloseBehaviorExit {
		selected = 1
	}
	parts := ui.SegmentedBase(c, &selected, 2)
	control := parts.Track.Label("关闭行为").AlignSelf(ui.Start).Padding(4).Gap(4).
		Radius(controlRadius).Background(tok.SurfaceSunken).Border(1, tok.BorderDefault)
	control.Children(func() {
		for i, label := range []string{"隐藏到通知区域（默认）", "停止扫描并退出"} {
			segment := parts.Segment(i).MinHeight(max(40, t.FontSize*1.5+16)).Padding(8, 16).Radius(4)
			settingsSegment(segment, i == selected, t.Dark, tok).Children(func() { ui.Text(c, label).SingleLine() })
		}
	})
	behavior := app.CloseBehaviorHide
	if selected == 1 {
		behavior = app.CloseBehaviorExit
	}
	if behavior != m.CloseBehavior() {
		m.SetCloseBehavior(behavior)
	}
	if m.trayHintErr != "" {
		ui.Text(c, m.trayHintErr).TextColor(t.Danger).FontSize(t.FontSize * 0.9)
	}
	if m.closeBehaviorErr != "" {
		ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
			ui.Text(c, m.closeBehaviorErr).TextColor(t.Danger).FontSize(t.FontSize * 0.9)
			if button(c, "重试").Clicked() {
				m.retryCloseBehaviorSave()
			}
		})
	}
}

// settingsSegment keeps the selected face and outline visible even
// while hovered or pressed. SegmentedBase retains radio and focus semantics.
func settingsSegment(segment ui.Element, selected, dark bool, tok *Tokens) ui.Element {
	bg, border, fg := ui.Transparent, ui.Transparent, tok.TextSecondary
	if selected {
		bg, border, fg = tok.AccentSoft, tok.AccentFG, tok.AccentFG
		if dark {
			bg, border, fg = tok.SurfaceControl.Mix(tok.TextPrimary, .18), tok.BorderControl, tok.TextPrimary
		}
	}
	if segment.IsDisabled() {
		bg, fg = tok.SurfaceDisabled, tok.TextDisabled
	} else if selected {
		if !dark && segment.Pressed() {
			bg = tok.AccentSoftHover.Mix(tok.AccentFG, .04)
			border, fg = tok.AccentFillPressed, tok.AccentFillPressed
		} else if !dark && segment.Hovered() {
			bg = tok.AccentSoftHover
			border, fg = tok.AccentFillHover, tok.AccentFillHover
		} else if segment.Pressed() {
			bg = bg.Mix(tok.TextPrimary, .12)
		} else if segment.Hovered() {
			bg = bg.Mix(tok.TextPrimary, .06)
		}
	} else if segment.Pressed() {
		bg = tok.SurfacePressed
	} else if segment.Hovered() {
		bg = tok.SurfaceHover
	}
	return segment.Background(bg).Border(1, border).TextColor(fg)
}

// retryCloseBehaviorSave persists the current close-window preference again
// after a failed save. The preference itself already took effect.
func (m *Model) retryCloseBehaviorSave() {
	m.closeBehaviorErr = ""
	if m.cfg.SaveCloseBehavior == nil {
		return
	}
	if err := m.cfg.SaveCloseBehavior(m.CloseBehavior()); err != nil {
		m.closeBehaviorErr = "保存设置失败：" + err.Error()
	}
}

// settingsShortcut is one row of the read-only key table. Only keys this
// build actually registers appear.
type settingsShortcut struct {
	action  string
	context string
	keys    string
}

// settingsShortcuts lists one action and one platform-specific key per
// row. The stop key's context is the registration
// state the snapshot can prove: held after the platform Open (preparing with
// a deadline, or capturing), never claimed before Open or during finalizing,
// the last failure when the session ended with hotkeyUnavailable, and the
// future registration otherwise. The key text comes from the one notation
// table, so it stays the platform's binding.
func (m *Model) settingsShortcuts(c *ui.Context, t *ui.Theme, tok *Tokens) {
	// Horizontal scrolling preserves the complete action and key on one
	// line at large text sizes and narrow windows, without ellipsis.
	// ScrollHorizontal measures its content with an unbounded width. Give
	// the list the card's actual available width as a floor, while allowing
	// its intrinsic width to overflow at narrow widths and large text.
	width, _ := c.Size()
	innerWidth := max(0, width-m.railWidth()-m.pageLeftPadding()-m.pagePadding()-2*(settingsSectionInset+1))
	ui.ScrollHorizontal(c).FillWidth().Focusable().Label("快捷键列表").Children(func() {
		ui.Column(c).MinWidth(innerWidth).Children(func() {
			for _, shortcut := range m.settingsShortcutRows() {
				ui.Row(c).FillWidth().MinHeight(48).PaddingY(12).Gap(24).AlignItems(ui.Center).Children(func() {
					ui.Text(c, shortcut.action+"（"+shortcut.context+"）").SingleLine().Shrink(0)
					ui.Box(c).Grow(1)
					keycaps(c, shortcut.keys)
				})
				ui.Box(c).FillWidth().Height(1).Shrink(0).Background(tok.BorderSubtle)
			}
		})
	})
}

// stopHotkeyStateText derives the stop hotkey's registration state from the
// authoritative snapshot. The hotkey is registered at the start of Prepare's
// Port.Open, and the controller reports PrepareDeadline only after Open
// succeeded, so preparing without a deadline cannot promise it yet. The
// controller marks PhaseFinalizing before it releases the platform session,
// and recognition draining follows that release, so a finalizing snapshot
// cannot prove the hotkey is still held. A failed registration is checked
// first: it ends the session and must stay visible even if the phase around
// that end is ambiguous.
func (m *Model) stopHotkeyStateText() string {
	if trigger := m.snap.Scan.Trigger; trigger != nil && trigger.Code == "hotkeyUnavailable" {
		return "上次注册失败：被其他程序占用"
	}
	switch m.snap.Scan.Phase {
	case scan.PhasePreparing:
		if !m.snap.Scan.PrepareDeadline.IsZero() {
			return "已注册"
		}
		return "正在注册"
	case scan.PhaseCapturing:
		return "已注册"
	case scan.PhaseFinalizing:
		return "扫描收尾中"
	default:
		return "开始扫描时注册"
	}
}

func (m *Model) settingsShortcutRows() []settingsShortcut {
	return []settingsShortcut{
		{action: "停止扫描", context: "全局 · " + m.stopHotkeyStateText(), keys: shortcutStopScan},
		{action: "开始扫描", context: "扫描准备", keys: shortcutStartScan},
		{action: "确认并下一项", context: "待处理件", keys: shortcutConfirmNext},
		{action: "确认修改", context: "记录 · 修改中", keys: shortcutConfirmNext},
		{action: "保存修改", context: "待处理件编辑中", keys: shortcutSave},
		{action: "下一个待处理件", context: "记录 · 逐件", keys: shortcutNextPending},
		{action: "上一个待处理件", context: "记录 · 逐件", keys: shortcutPreviousPending},
		{action: "导出", context: "记录工作区", keys: shortcutExport},
		{action: "截图放大", context: "逐件 · 非文本框", keys: shortcutZoomIn},
		{action: "截图缩小", context: "逐件 · 非文本框", keys: shortcutZoomOut},
		{action: "截图适合窗口", context: "逐件 · 非文本框", keys: shortcutZoomFit},
		{action: "前往下一区域", context: "页面", keys: shortcutFocusRegion},
		{action: "返回上一区域", context: "页面", keys: shortcutFocusRegionBack},
		{action: "打开选中的记录", context: "全部记录", keys: shortcutOpenRecord},
		{action: "关闭浮层或返回表格", context: "非文本框", keys: shortcutCloseOverlay},
	}
}

// settingsStorage is the measured storeroom: the category footprint, the
// data root and the rebuildable clears, then the two destructive entries.
// The deletion confirmations stay under the action row, so the measured
// scope is next to the entry that asked for it.
func (m *Model) settingsStorage(c *ui.Context, t *ui.Theme, tok *Tokens) {
	m.storageSummaryRow(c, t, tok)
	m.storageActionsRow(c, t, tok)
	if m.dataDirectoryErr != "" {
		ui.Text(c, m.dataDirectoryErr).TextColor(t.Danger)
	}
	if m.cleanupNotice != "" {
		ui.Text(c, m.cleanupNotice)
	}
	m.cleanupPromptArea(c, t)
	m.storagePromptArea(c, t)
}

// storageSummaryRow shows the measured footprint, or the state that
// replaces it; a failed measurement is never presented as stale numbers.
func (m *Model) storageSummaryRow(c *ui.Context, t *ui.Theme, tok *Tokens) {
	switch {
	case m.storageLoading:
		ui.Text(c, "正在计算占用……").TextColor(tok.TextMuted)
	case m.storageErr != "":
		ui.Text(c, "占用读取失败："+m.storageErr).TextColor(t.Danger)
	case m.storage != nil:
		metrics := m.storageMetrics()
		width, _ := c.Size()
		innerWidth := max(0, width-m.railWidth()-m.pageLeftPadding()-m.pagePadding()-2*(settingsSectionInset+1))
		gap := t.Space(6)
		// Keep numeric columns aligned without stretching this small table
		// across the whole window. Measure full values before sizing columns.
		capacityWidth, countWidth := t.FontSize*(128.0/14), t.FontSize*(112.0/14)
		for _, metric := range metrics {
			w, _ := c.MeasureText(0, ui.Span{Text: metric.capacity, Size: t.FontSize})
			capacityWidth = max(capacityWidth, w)
			w, _ = c.MeasureText(0, ui.Span{Text: metric.count, Size: t.FontSize})
			countWidth = max(countWidth, w)
		}
		tableWidth := min(innerWidth, max(t.FontSize*(480.0/14), capacityWidth+countWidth+2*gap+t.FontSize*4))
		ui.Column(c).FillWidth().Gap(t.Space(4)).Children(func() {
			ui.Column(c).Width(tableWidth).Children(func() {
				row := func(label, capacity, count string, header bool) {
					color := tok.TextPrimary
					if header {
						color = tok.TextMuted
					}
					ui.Row(c).FillWidth().MinHeight(40).PaddingY(t.Space(2)).Gap(gap).AlignItems(ui.Center).Children(func() {
						ui.Text(c, label).NoWrap().Grow(1).Basis(0).TextColor(color)
						ui.Text(c, capacity).NoWrap().Width(capacityWidth).Shrink(0).TextAlign(ui.End).TextColor(color)
						ui.Text(c, count).NoWrap().Width(countWidth).Shrink(0).TextAlign(ui.End).TextColor(color)
					})
					ui.Box(c).FillWidth().Height(1).Shrink(0).Background(tok.BorderSubtle)
				}
				row("类别", "占用", "数量", true)
				for _, metric := range metrics {
					count := metric.count
					if count == "" {
						count = "—"
					}
					row(metric.label, metric.capacity, count, false)
				}
			})
			ui.Text(c, m.storageDiskText()).NoWrap().TextColor(tok.TextMuted).FontSize(t.FontSize * 0.9)
		})
	}
}

// storageActionsRow exposes the data root, the two rebuildable clears and
// the two measured bulk deletions. The first button is the focus target
// when the prepare check sends the user here to free disk space.
func (m *Model) storageActionsRow(c *ui.Context, t *ui.Theme, tok *Tokens) {
	blocked := m.cleanupBlockedReason()
	focusStorage := m.settingsFocusStorage
	if focusStorage {
		m.settingsFocusStorage = false
		m.settingsFocusPending = false
	}
	actionRow(c).Wrap().Children(func() {
		opened := false
		open := button(c, "打开数据目录").Disabled(m.cfg.OpenDataDirectory == nil)
		if focusStorage && m.cfg.OpenDataDirectory != nil {
			open = open.Focus().ScrollIntoView()
			opened = true
		}
		if m.cfg.OpenDataDirectory != nil && open.Clicked() {
			m.OpenDataDirectory()
		}
		cache := button(c, "清缓存").Disabled(m.storageLoading)
		if focusStorage && !opened {
			cache = cache.Focus().ScrollIntoView()
		}
		if cache.Clicked() {
			m.RequestCacheClear()
		}
		if button(c, "清旧日志").Disabled(m.storageLoading).Clicked() {
			m.RequestOldLogClear()
		}
		danger := func(label string, action func()) {
			button := button(c, label).TextColor(tok.DangerFG).
				Background(tok.SurfaceControl).Border(1, tok.DangerBorder).
				Disabled(blocked != "")
			if button.Clicked() {
				action()
			}
		}
		danger("删除全部截图", m.RequestAllScreenshotCleanup)
		danger("删除全部记录", m.RequestAllRecordCleanup)
	})
	if blocked != "" {
		ui.Text(c, blocked).TextColor(tok.TextMuted).FontSize(t.FontSize * 0.9)
	}
}

// settingsDiagnostics offers the two diagnostic actions every diagnostic
// surface shares (09 §4.6) and states what a copy excludes.
func (m *Model) settingsDiagnostics(c *ui.Context, t *ui.Theme, tok *Tokens) {
	ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Wrap().Children(func() {
		ui.Text(c, "复制的内容不包含窗口标题等隐私").TextColor(tok.TextMuted).
			FontSize(t.FontSize * 0.85)
		actionRow(c).Grow(1).Children(func() {
			label := "复制诊断信息"
			if m.diagnosticsNotice != "" {
				label = "已复制诊断信息"
			}
			if button(c, label).Key("settings-copy-diagnostics").Clicked() {
				m.copyDiagnostics(c)
			}
			if button(c, "打开诊断目录").Disabled(m.cfg.OpenDiagnosticsDirectory == nil).Clicked() {
				m.openDiagnosticsDirectory()
			}
		})
	})
	if m.diagnosticsErr != "" {
		ui.Text(c, m.diagnosticsErr).TextColor(t.Danger)
	}
}

// copyDiagnostics copies the newest session's allowlisted summary. The text
// is built on demand, through the same helper as the record summary, so it
// describes the newest session at the moment the user asks.
func (m *Model) copyDiagnostics(c *ui.Context) {
	text := m.diagnosticsText(app.DiagnosticsScanFromSnapshot(m.snap))
	if text == "" {
		m.diagnosticsNotice = ""
		m.diagnosticsErr = "没有可复制的诊断信息。"
		return
	}
	m.diagnosticsErr = ""
	m.diagnosticsNotice = ""
	c.WriteClipboardThen(text, func(err error) {
		if err != nil {
			m.diagnosticsErr = "复制诊断信息失败：" + err.Error()
			return
		}
		m.diagnosticsNotice = "已复制诊断信息。"
	})
}

func (m *Model) openDiagnosticsDirectory() {
	if m.cfg.OpenDiagnosticsDirectory == nil {
		return
	}
	m.diagnosticsNotice = ""
	if err := m.cfg.OpenDiagnosticsDirectory(); err != nil {
		m.diagnosticsErr = "打开诊断目录失败：" + err.Error()
		return
	}
	m.diagnosticsErr = ""
}

// settingsLicenses stays outside updateArea's disabled-update early return.
func (m *Model) settingsLicenses(c *ui.Context, t *ui.Theme, tok *Tokens) {
	if m.cfg.OpenLicenses == nil {
		return
	}
	entry := ui.ButtonBase(c).Label("开源许可").AlignSelf(ui.Start).
		MinHeight(max(32, t.FontSize*1.5+8)).Padding(4, 8).Radius(4).
		FontSize(t.FontSize * 0.9).TextColor(tok.TextSecondary)
	if entry.Pressed() {
		entry.Background(tok.SurfacePressed)
	} else if entry.Hovered() {
		entry.Background(tok.SurfaceHover)
	}
	entry.Children(func() { ui.Text(c, "开源许可").SingleLine() })
	if entry.Clicked() {
		m.openLicenses()
	}
	if m.licensesErr != "" {
		ui.Text(c, m.licensesErr).TextColor(t.Danger).FontSize(t.FontSize * 0.9)
	}
}

func (m *Model) openLicenses() {
	m.licensesErr = ""
	if err := m.cfg.OpenLicenses(); err != nil {
		m.licensesErr = "打开开源许可失败：" + err.Error()
	}
}

// ShowStorageFromPrepareCheck is the disk-space entry the prepare check
// offers when it reports too little free space (07 §4): settings opens at
// the storage section, its first button takes the keyboard, and the strip
// says where a completed cleanup returns. The entry focus applies on the
// next frame, after navigation settles.
func (m *Model) ShowStorageFromPrepareCheck() {
	m.settingsReturnToPrepare = true
	m.settingsFocusStorage = true
	m.settingsFocusPending = false
	m.settingsScrollTarget = settingsSectionStorage
	if m.displayPage() == pageSettings {
		return
	}
	m.navigatePage(pageSettings)
}

// leaveSettingsPage drops the settings-only entry state when the user moves
// to another destination; a later visit is an ordinary one.
func (m *Model) leaveSettingsPage() {
	m.settingsFocusPending = false
	m.settingsFocusStorage = false
	m.settingsReturnToPrepare = false
	m.settingsScrollTarget = settingsSectionNone
	m.diagnosticsNotice = ""
	m.diagnosticsErr = ""
	m.dataDirectoryErr = ""
	m.licensesErr = ""
}

// returnToPrepareCheck completes the disk-space entry's promise after a
// successful cleanup.
func (m *Model) returnToPrepareCheck() {
	if !m.settingsReturnToPrepare {
		return
	}
	m.settingsReturnToPrepare = false
	m.settingsFocusStorage = false
	m.settingsFocusPending = false
	m.page = pageScan
}

type storageMetric struct {
	label    string
	capacity string
	count    string
}

// storageMetrics separates each category's label, primary capacity and
// optional count without changing the measured scope or byte formatting.
func (m *Model) storageMetrics() []storageMetric {
	usage := m.storage
	if usage == nil {
		return nil
	}
	dataBytes := usage.RecordBytes - usage.EvidenceBytes
	if dataBytes < 0 {
		dataBytes = 0
	}
	return []storageMetric{
		{"记录", formatBytes(dataBytes), strconv.Itoa(usage.Records) + "条"},
		{"截图", formatBytes(usage.EvidenceBytes), strconv.Itoa(usage.EvidenceFiles) + "张"},
		{"缓存", formatBytes(usage.CacheBytes), ""},
		{"日志", formatBytes(usage.LogBytes), ""},
	}
}

// storageDiskText renders the data volume's free and total space; unknown
// stays unknown instead of becoming a number.
func (m *Model) storageDiskText() string {
	usage := m.storage
	if usage == nil || !usage.DiskSpaceKnown {
		return "磁盘可用空间不可用"
	}
	return "磁盘可用 " + formatBytes(usage.DiskFreeBytes) + " / " + formatBytes(usage.DiskTotalBytes)
}

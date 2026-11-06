package ui

import (
	"strconv"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/scan"
	"github.com/egoist/mygo/ui"
)

// Settings is a single page of sections with an in-page directory on the
// left (design 08 §6). The directory disappears in the compact width band;
// the sections keep their reading order either way (design 09 §2).
const (
	settingsTOCWidth         = 150
	settingsContentMaxWidth  = 720
	settingsStandardMinWidth = 1120
)

// settingsSectionID names one section and its in-page directory entry.
// Zero means no pending directory jump.
type settingsSectionID int

const (
	settingsSectionNone settingsSectionID = iota
	settingsSectionGeneral
	settingsSectionShortcuts
	settingsSectionStorage
	settingsSectionDiagnostics
	settingsSectionUpdate
	settingsSectionAbout
)

// settingsSectionInfo is one directory entry and its section title.
type settingsSectionInfo struct {
	id    settingsSectionID
	title string
}

// settingsSectionList is the fixed reading and directory order.
var settingsSectionList = []settingsSectionInfo{
	{id: settingsSectionGeneral, title: "常规"},
	{id: settingsSectionShortcuts, title: "快捷键"},
	{id: settingsSectionStorage, title: "存储与清理"},
	{id: settingsSectionDiagnostics, title: "诊断"},
	{id: settingsSectionUpdate, title: "更新"},
	{id: settingsSectionAbout, title: "关于"},
}

// settingsSectionRefs are the section anchors the in-page directory
// scrolls to by measuring their committed boxes.
type settingsSectionRefs struct {
	general     ui.Ref
	shortcuts   ui.Ref
	storage     ui.Ref
	diagnostics ui.Ref
	update      ui.Ref
	about       ui.Ref
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
	case settingsSectionAbout:
		return &r.about
	default:
		return nil
	}
}

// settingsPage is the settings destination: the optional return strip from
// the prepare check's disk-space entry, the in-page directory in the
// standard width band, and the six sections in one scrolling column.
func (m *Model) settingsPage(c *ui.Context, t *ui.Theme) {
	_, tok := themeWithTokens(t, c.Preferences())
	m.applySettingsScrollTarget(c)
	if m.settingsReturnToPrepare {
		m.settingsReturnStrip(c, tok)
	}
	ui.Row(c).Grow(1).FillHeight().AlignItems(ui.Stretch).Gap(t.Space(4)).Children(func() {
		if m.settingsTableOfContentsVisible() {
			m.settingsTableOfContents(c, t, tok)
		}
		ui.Scroll(c).Grow(1).FillHeight().TrackScroll(&m.settingsScroll).Bind(&m.settingsViewportRef).
			Children(func() {
				ui.Column(c).MaxWidth(settingsContentMaxWidth).Gap(t.Space(3)).Children(func() {
					m.settingsSection(c, t, settingsSectionGeneral, "常规", func() {
						m.settingsGeneral(c, t, tok)
					})
					ui.Divider(c)
					m.settingsSection(c, t, settingsSectionShortcuts, "快捷键", func() {
						m.settingsShortcuts(c, t, tok)
					})
					ui.Divider(c)
					m.settingsSection(c, t, settingsSectionStorage, "存储与清理", func() {
						m.settingsStorage(c, t, tok)
					})
					ui.Divider(c)
					m.settingsSection(c, t, settingsSectionDiagnostics, "诊断", func() {
						m.settingsDiagnostics(c, t, tok)
					})
					ui.Divider(c)
					m.settingsSection(c, t, settingsSectionUpdate, "更新", func() {
						m.updateArea(c, t, tok)
					})
					ui.Divider(c)
					m.settingsSection(c, t, settingsSectionAbout, "关于", func() {
						m.settingsAbout(c, t, tok)
					})
				})
			})
	})
}

// settingsTableOfContentsVisible reports whether the standard width band
// leaves room for the directory. Before the window reports its content
// width, the standard design applies.
func (m *Model) settingsTableOfContentsVisible() bool {
	return m.contentWidth == 0 || m.contentWidth >= settingsStandardMinWidth
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

// settingsSection builds one titled section and binds the anchor the
// in-page directory scrolls to.
func (m *Model) settingsSection(c *ui.Context, t *ui.Theme,
	id settingsSectionID, title string, body func()) {
	ref := m.settingsSections.ref(id)
	column := ui.Column(c).Gap(t.Space(1.5))
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

// settingsTableOfContents is the left directory. It is one vertical focus
// group: Tab enters it, the arrows move between entries, and Enter scrolls
// to the section. "更新" carries the same new-version dot as the rail.
func (m *Model) settingsTableOfContents(c *ui.Context, t *ui.Theme, tok *Tokens) {
	ui.Column(c).Width(settingsTOCWidth).Gap(t.Space(0.5)).
		FocusGroup(ui.Vertical).Children(func() {
		for i, section := range settingsSectionList {
			ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
				entry := ui.Button(c, section.title).TextColor(tok.TextSecondary).
					Background(ui.Color{}).Border(0, ui.Color{}).Radius(t.Space(1)).
					Padding(t.Space(0.75), t.Space(1))
				if i == 0 {
					entry = entry.Bind(&m.settingsFirstRef)
					if m.settingsFocusPending && !m.settingsFocusStorage {
						entry = entry.AutoFocus()
						m.settingsFocusPending = false
					}
				}
				if section.id == settingsSectionUpdate && m.updateDotVisible() {
					updateDot(c, t, tok)
				}
				if entry.Clicked() {
					m.jumpSettingsSection(c, section.id)
				}
			})
		}
	})
}

// settingsGeneral holds the close-window preference and the tray-icon hint
// replay. The default hides to the notification area; the explicit choice
// runs the stop/save/release wrap-up. Without a tray entry hiding has no way
// back, so the close option is fixed to the explicit exit and the reason is
// stated (08 §7).
func (m *Model) settingsGeneral(c *ui.Context, t *ui.Theme, tok *Tokens) {
	ui.Text(c, "点击窗口的 ✕ 时").TextColor(tok.TextSecondary)
	compactFocus := m.settingsFocusPending && !m.settingsTableOfContentsVisible()
	if m.TrayUnavailable() != "" {
		fixed := string(app.CloseBehaviorExit)
		ui.RadioGroup(c, func() {
			radio := ui.Radio(c, &fixed, string(app.CloseBehaviorExit), "停止扫描并退出").
				Disabled(true)
			if compactFocus {
				radio = radio.Bind(&m.settingsFirstRef).AutoFocus()
				m.settingsFocusPending = false
			}
		})
		ui.Text(c, "通知区域图标不可用。关闭窗口会停止扫描并退出应用。").
			TextColor(t.TextMuted).FontSize(t.FontSize * 0.9)
		return
	}
	behavior := string(m.CloseBehavior())
	if behavior != string(app.CloseBehaviorExit) {
		behavior = string(app.CloseBehaviorHide)
	}
	ui.RadioGroup(c, func() {
		ui.Row(c).Gap(t.Space(3)).Children(func() {
			hide := ui.Radio(c, &behavior, string(app.CloseBehaviorHide), "隐藏到通知区域（默认）")
			if behavior == string(app.CloseBehaviorHide) && compactFocus {
				hide = hide.Bind(&m.settingsFirstRef).AutoFocus()
				m.settingsFocusPending = false
			}
			exit := ui.Radio(c, &behavior, string(app.CloseBehaviorExit), "停止扫描并退出")
			if behavior == string(app.CloseBehaviorExit) && compactFocus {
				exit = exit.Bind(&m.settingsFirstRef).AutoFocus()
				m.settingsFocusPending = false
			}
		})
	})
	if chosen := app.CloseBehavior(behavior); chosen != m.CloseBehavior() {
		m.SetCloseBehavior(chosen)
	}
	if ui.Button(c, "再看一次托盘图标位置提示").Clicked() {
		m.ShowTrayHint()
	}
	if m.trayHintErr != "" {
		ui.Text(c, m.trayHintErr).TextColor(t.Danger).FontSize(t.FontSize * 0.9)
	}
	if m.closeBehaviorErr != "" {
		ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
			ui.Text(c, m.closeBehaviorErr).TextColor(t.Danger).FontSize(t.FontSize * 0.9)
			if ui.Button(c, "重试").Clicked() {
				m.retryCloseBehaviorSave()
			}
		})
	}
	if m.windowNotice != "" {
		ui.Text(c, m.windowNotice).TextColor(t.TextMuted).FontSize(t.FontSize * 0.9)
	}
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

// settingsShortcuts lists the registered keys in the design's order,
// Windows notation throughout. The stop key's context is the registration
// state the snapshot can prove: held after the platform Open (preparing with
// a deadline, or capturing), never claimed before Open or during finalizing,
// the last failure when the session ended with hotkeyUnavailable, and the
// future registration otherwise. The key text comes from the one notation
// table, so it stays the platform's binding.
func (m *Model) settingsShortcuts(c *ui.Context, t *ui.Theme, tok *Tokens) {
	for i, shortcut := range m.settingsShortcutRows() {
		if i > 0 {
			ui.Divider(c)
		}
		ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
			ui.Text(c, shortcut.action).TextColor(tok.TextSecondary)
			ui.Text(c, shortcut.context).TextColor(tok.TextMuted).
				FontSize(t.FontSize * 0.85)
			ui.Box(c).Grow(1)
			ui.Text(c, shortcut.keys).FontWeight(600)
		})
	}
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
		{action: "停止扫描", context: m.stopHotkeyStateText(), keys: shortcutStopScan},
		{action: "开始扫描", context: "扫描准备", keys: shortcutStartScan},
		{action: "确认并下一项", context: "记录 · 逐件", keys: shortcutConfirmNext},
		{action: "保存修改", context: "记录 · 逐件，有未保存修改时", keys: shortcutSave},
		{action: "下一个待处理件", context: "记录 · 逐件", keys: shortcutNextPending},
		{action: "导出…", context: "记录工作区", keys: shortcutExport},
		{action: "在大区域间跳转", context: "任意", keys: shortcutFocusRegion + " / " + shortcutFocusRegionBack},
		{action: "打开选中的记录", context: "全部记录", keys: shortcutOpenRecord},
		{action: "关闭浮层/返回表格", context: "任意", keys: shortcutCloseOverlay},
	}
}

// settingsStorage is the measured storeroom: the category footprint, the
// data root and the rebuildable clears, then the two destructive entries.
// The deletion confirmations stay under the action row, so the measured
// scope is next to the entry that asked for it.
func (m *Model) settingsStorage(c *ui.Context, t *ui.Theme, tok *Tokens) {
	m.storageSummaryRow(c, t, tok)
	m.storageActionsRow(c, t, tok)
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
		ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
			ui.Text(c, m.storageBreakdownText()).TextColor(tok.TextSecondary)
			ui.Box(c).Grow(1)
			ui.Text(c, m.storageDiskText()).TextColor(tok.TextMuted).
				FontSize(t.FontSize * 0.9)
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
	ui.Row(c).Gap(t.Space(1.5)).AlignItems(ui.Center).Wrap().Children(func() {
		opened := false
		open := ui.Button(c, "打开数据目录").Disabled(m.cfg.OpenDataDirectory == nil)
		if focusStorage && m.cfg.OpenDataDirectory != nil {
			open = open.AutoFocus().ScrollIntoView()
			opened = true
		}
		if m.cfg.OpenDataDirectory != nil && open.Clicked() {
			m.OpenDataDirectory()
		}
		cache := ui.Button(c, "清缓存").Disabled(m.storageLoading)
		if focusStorage && !opened {
			cache = cache.AutoFocus().ScrollIntoView()
		}
		if cache.Clicked() {
			m.RequestCacheClear()
		}
		if ui.Button(c, "清旧日志").Disabled(m.storageLoading).Clicked() {
			m.RequestOldLogClear()
		}
		danger := func(label string, action func()) {
			button := ui.Button(c, label).TextColor(tok.DangerFG).
				Background(tok.SurfaceControl).Border(1, tok.DangerBorder).
				Disabled(blocked != "")
			if button.Clicked() {
				action()
			}
		}
		danger("删除全部截图…", m.RequestAllScreenshotCleanup)
		danger("删除全部记录…", m.RequestAllRecordCleanup)
	})
	if blocked != "" {
		ui.Text(c, blocked).TextColor(tok.TextMuted).FontSize(t.FontSize * 0.9)
	}
}

// settingsDiagnostics offers the two diagnostic actions every diagnostic
// surface shares (09 §4.6) and states what a copy excludes.
func (m *Model) settingsDiagnostics(c *ui.Context, t *ui.Theme, tok *Tokens) {
	ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Wrap().Children(func() {
		if ui.Button(c, "复制诊断信息").Clicked() {
			m.copyDiagnostics(c)
		}
		if ui.Button(c, "打开诊断目录").Disabled(m.cfg.OpenDiagnosticsDirectory == nil).Clicked() {
			m.openDiagnosticsDirectory()
		}
		ui.Text(c, "复制的内容不包含窗口标题等隐私").TextColor(tok.TextMuted).
			FontSize(t.FontSize * 0.85)
	})
	if m.diagnosticsNotice != "" {
		ui.Text(c, m.diagnosticsNotice).TextColor(tok.TextMuted)
	}
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
	c.WriteClipboard(text)
	m.diagnosticsErr = ""
	m.diagnosticsNotice = "已复制诊断信息。"
}

func (m *Model) openDiagnosticsDirectory() {
	if m.cfg.OpenDiagnosticsDirectory == nil {
		return
	}
	if err := m.cfg.OpenDiagnosticsDirectory(); err != nil {
		m.diagnosticsNotice = ""
		m.diagnosticsErr = "打开诊断目录失败：" + err.Error()
		return
	}
	m.diagnosticsErr = ""
	m.diagnosticsNotice = "已打开诊断目录。"
}

// settingsAbout names the build and links the bundled third-party notices.
func (m *Model) settingsAbout(c *ui.Context, t *ui.Theme, tok *Tokens) {
	version := m.appVersion
	if version == "" {
		version = "未知"
	}
	ui.Text(c, "youkai "+version).TextColor(tok.TextSecondary)
	ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "以 MIT 许可发布。").TextColor(tok.TextMuted).FontSize(t.FontSize * 0.9)
		if m.cfg.OpenLicenses != nil && ui.Button(c, "开源许可").Clicked() {
			m.openLicenses()
		}
	})
	if m.aboutErr != "" {
		ui.Text(c, m.aboutErr).TextColor(t.Danger).FontSize(t.FontSize * 0.9)
	}
}

func (m *Model) openLicenses() {
	m.aboutErr = ""
	if err := m.cfg.OpenLicenses(); err != nil {
		m.aboutErr = "打开开源许可失败：" + err.Error()
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
	m.aboutErr = ""
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

// storageBreakdownText renders the four category sizes. The record count is
// shown with its data size so "记录" is not mistaken for the screenshots.
func (m *Model) storageBreakdownText() string {
	usage := m.storage
	if usage == nil {
		return ""
	}
	dataBytes := usage.RecordBytes - usage.EvidenceBytes
	if dataBytes < 0 {
		dataBytes = 0
	}
	return "记录 " + formatBytes(dataBytes) + "（" + strconv.Itoa(usage.Records) + " 条）" +
		" · 截图 " + strconv.Itoa(usage.EvidenceFiles) + " 张 " + formatBytes(usage.EvidenceBytes) +
		" · 缓存 " + formatBytes(usage.CacheBytes) +
		" · 日志 " + formatBytes(usage.LogBytes)
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

package ui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/egoist/mygo/ui"
)

// DirectExportSaver writes one usable-data file without a save dialog.
// The export dialog uses it to write straight into the system downloads
// directory; the implementation picks a numbered name instead of
// overwriting an existing file. It never reads the record or changes the
// checkpoint.
type DirectExportSaver interface {
	SaveDirect(format app.ExportFormat, defaultName string, data []byte) (path string, err error)
}

// CancelableDirectExportSaver is the optional half of DirectExportSaver
// that lets the export dialog stop before a file is committed. A saver
// that implements it must return an empty path once ctx is canceled and
// leave no file behind; the dialog only promises "已取消，没有写入文件"
// when this contract holds.
type CancelableDirectExportSaver interface {
	SaveDirectContext(ctx context.Context, format app.ExportFormat, defaultName string, data []byte) (path string, err error)
}

// ExportDirectoryError reports that the direct export destination cannot
// take the file. The export dialog shows the reason with 重试 and leaves
// the internal record untouched.
type ExportDirectoryError struct {
	// Directory is the destination the saver tried to use, when known.
	Directory string
	// Err is the underlying filesystem failure.
	Err error
}

func (e *ExportDirectoryError) Error() string {
	if e.Directory == "" {
		return fmt.Sprintf("无法访问下载目录：%v", e.Err)
	}
	return fmt.Sprintf("无法写入下载目录 %s：%v", e.Directory, e.Err)
}

func (e *ExportDirectoryError) Unwrap() error { return e.Err }

// ExportCleanupError reports that a canceled export could not remove a file
// it had created. The dialog shows it as a failure instead of the ordinary
// "已取消，没有写入文件" notice, which would be false. Detail is the
// user-facing cleanup reason; Err keeps the underlying cause for errors.Is.
type ExportCleanupError struct {
	Detail string
	Err    error
}

func (e *ExportCleanupError) Error() string {
	detail := e.Detail
	cause := exportCleanupCause(e.Err)
	switch {
	case detail == "":
		detail = cause
	case cause != "" && !strings.Contains(detail, cause):
		// Keep a plain write failure visible alongside the file left behind
		// (08 §4); a cancellation alone adds nothing the user can act on.
		detail += "；" + cause
	}
	return "导出文件清理失败：" + detail
}

// exportCleanupCause strips the English cancellation text from a joined
// cause while keeping a real write error beside it.
func exportCleanupCause(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	if errors.Is(err, context.Canceled) {
		text = strings.ReplaceAll(text, context.Canceled.Error(), "")
		text = strings.TrimSpace(strings.Trim(text, ";\n"))
	}
	return text
}

func (e *ExportCleanupError) Unwrap() error { return e.Err }

// exportOutcome is the export dialog's completed or failed write. It
// freezes the count and pending total of the revision that was written, so
// a later reload cannot make the result describe different data.
type exportOutcome struct {
	format   app.ExportFormat
	included int
	pending  int64
	recordID string
	path     string
	err      error
}

// exportDialogState is the modal's own state. attempted starts false on
// every opening, so a result or cancellation from an earlier visit never
// reappears as this visit's.
type exportDialogState struct {
	open         bool
	attempted    bool
	format       app.ExportFormat
	canceled     bool
	openDirErr   string
	excludedOpen bool
}

// exportBusy reports whether any export is in flight. Review actions stay
// disabled meanwhile so the file and the on-screen revision cannot part
// ways while the snapshot is written.
func (m *Model) exportBusy() bool {
	return m.exporting
}

// reviewActionsBlocked reports whether review edits and confirmations must
// stay disabled: while a save is in flight, the latest checkpoint is still
// loading (the visible record is an older snapshot then), the last load
// failed (the record on screen is stale), an export holds its snapshot, or a
// screenshot cleanup is running and will reload the record when it finishes.
func (m *Model) reviewActionsBlocked() bool {
	return m.reviewSaving || m.loading || m.recErr != "" || m.exportBusy() || m.exportDialog.open ||
		(m.cleanup != nil && m.cleanup.running)
}

// exportAvailable reports whether this build offers an export entry.
func (m *Model) exportAvailable() bool {
	return m.cfg.DirectExportSaver != nil
}

// rebuildExport freezes the displayed record's current revision for both
// output formats and for the counts the results area shows. It runs on the
// UI thread when a record load or a saved item lands.
func (m *Model) rebuildExport() {
	m.exportData = nil
	m.exportBuildErr = ""
	if m.recData == nil {
		return
	}
	export, err := app.BuildUsableExport(m.recData)
	if err != nil {
		m.exportBuildErr = err.Error()
		return
	}
	m.exportData = export
}

// exportBlockedReason explains why the export dialog cannot write now.
// Empty means both formats may be exported, including an empty file when
// the record has no exportable item (F-22).
func (m *Model) exportBlockedReason() string {
	return m.exportBlockedReasonWithDraft(true)
}

// exportBlockedReasonWithDraft is exportBlockedReason with the dirty-draft
// case optional. The summary's action passes false: only a dirty draft stays
// clickable, because the dialog asks save/discard; every other blocked state
// disables the action and shows its reason (09 §4.1).
func (m *Model) exportBlockedReasonWithDraft(blockDirty bool) string {
	switch {
	case !m.exportAvailable():
		return ""
	case m.exporting:
		return "正在导出……"
	case m.exitState == exitRunning || m.exitState == exitDone:
		return "正在退出应用，不能导出。"
	case m.reviewSaving:
		return "正在保存修改，完成前不能导出。"
	case m.cleanupRunning():
		return "正在清理，完成前不能导出。"
	case blockDirty && m.editingDirty():
		return "有未保存的修改，保存或放弃后可导出。"
	case m.loading:
		return "正在加载最新检查点，加载完成前不能导出。"
	case m.recErr != "":
		return "记录读取失败，不能导出旧快照。"
	case m.rec == nil || m.recData == nil:
		return "结果尚未加载，不能导出。"
	case m.rec.Live && m.snap.Busy:
		return "扫描尚未收尾，结束后可导出已保存的固定修订。"
	case m.rec.Live && (m.snap.SavePending || m.snap.SaveError != ""):
		return "扫描结果尚未保存，保存完成后才能导出。"
	case m.exportBuildErr != "":
		return "无法生成导出：" + m.exportBuildErr
	case m.exportData == nil:
		return "导出清单尚未就绪，不能导出。"
	default:
		return ""
	}
}

// exportEntryBlocked reports whether the workspace's export entry and its
// Ctrl+E shortcut are unusable now. The title-bar button and the shortcut
// share this answer, so they cannot disagree (L-2). A save in flight also
// blocks the entry: the draft must stay attached to its row until the result
// is known. Unsaved review input does not block the entry itself: opening it
// first shows the ordinary save/discard prompt, as the export dialog must not
// open over a dirty draft.
func (m *Model) exportEntryBlocked() bool {
	return m.exportEntryBlockedReason() != ""
}

// exportEntryBlockedReason names why the export entry is disabled, so the
// title bar and the completion form can show the reason beside the disabled
// button (09 §4.1).
func (m *Model) exportEntryBlockedReason() string {
	switch {
	case !m.exportAvailable():
		return "当前构建未启用导出。"
	case m.exporting:
		return "正在导出，完成前不能导出。"
	case m.reviewSaving:
		return "正在保存修改，完成前不能导出。"
	case m.cleanupRunning():
		return "正在清理，完成前不能导出。"
	case m.exitState == exitRunning || m.exitState == exitDone:
		return "正在退出应用，不能导出。"
	case m.rec == nil:
		return "结果尚未加载，不能导出。"
	default:
		return ""
	}
}

// exportDialogJSON starts the JSON export from the dialog.
func (m *Model) exportDialogJSON() { m.startExport(app.ExportJSON) }

// exportDialogCSV starts the CSV export from the dialog.
func (m *Model) exportDialogCSV() { m.startExport(app.ExportCSV) }

// requestExport opens the record's export dialog. A dirty review draft is
// settled first through the ordinary save/discard prompt, so the dialog
// never opens over changes that are not in the exported revision.
func (m *Model) requestExport() {
	if m.exportEntryBlocked() || m.exportDialog.open {
		return
	}
	if m.editingDirty() {
		m.requestNavigation(navigationRequest{kind: navExport})
		return
	}
	m.openExportDialog()
}

// openExportDialog shows the dialog in its pre-export state.
func (m *Model) openExportDialog() {
	if m.exportEntryBlocked() {
		return
	}
	m.clearCleanEditorForExport()
	m.exportOutcome = nil
	m.exportDialog = exportDialogState{open: true}
}

// clearCleanEditorForExport settles an editor that no longer needs a draft
// before the export dialog opens. editingDirty hides a draft while a save is
// in flight, so the save state must be checked separately or that draft would
// be mistaken for a clean editor.
func (m *Model) clearCleanEditorForExport() {
	if m.reviewSaving || m.editingDirty() {
		return
	}
	m.clearEdit()
}

// closeExportDialog hides the dialog. An export in flight keeps it open;
// the worker clears the busy state and the dialog then closes on the next
// dismissal (or stays with its result).
func (m *Model) closeExportDialog() {
	if m.exporting {
		return
	}
	m.exportDialog = exportDialogState{}
	m.exportCancel = nil
	m.replayPendingNotification()
}

// replayPendingNotification opens the record a notification click named
// while the export dialog was writing. Closing in any settled state
// (success, canceled or failure) honors the last click; the normal
// OpenNotification guards apply unchanged.
func (m *Model) replayPendingNotification() {
	recordID := m.pendingNotificationRecordID
	m.pendingNotificationRecordID = ""
	if recordID != "" {
		m.OpenNotification(recordID)
	}
}

// startExport renders and saves one format. Both output formats derive
// from m.exportData, the snapshot built for the record revision currently
// on screen, so a concurrent scan save or a reopened record cannot mix
// revisions inside the file.
func (m *Model) startExport(format app.ExportFormat) {
	save := m.cfg.DirectExportSaver
	if save == nil || m.exportData == nil || m.exportBlockedReason() != "" {
		return
	}
	// Capture the workspace's snapshot, identity and count now. A later
	// save during the write cannot change this export.
	export := m.exportData
	count := export.Summary.Included
	pending := export.Summary.Counts.Pending()
	version := m.cfg.AppVersion
	name := app.DefaultExportName(m.recData.Checkpoint.Session, format)
	recordID := m.rec.ID
	now := time.Now().UTC()

	ctx, cancel := context.WithCancel(context.Background())
	m.exportCancel = cancel
	m.exportDialog.attempted = true
	m.exportDialog.format = format
	m.exportDialog.canceled = false
	m.exportDialog.openDirErr = ""
	m.exporting = true
	m.exportOutcome = nil

	m.goAsync(func() {
		var data []byte
		var err error
		switch format {
		case app.ExportJSON:
			data, err = export.JSON(now, version)
		case app.ExportCSV:
			data, err = export.CSV()
		default:
			err = fmt.Errorf("unknown export format %q", format)
		}
		var path string
		if err == nil {
			path, err = saveDirectExport(ctx, save, format, name, data)
		}
		m.update(func() {
			m.exporting = false
			m.exportCancel = nil
			if ctx.Err() != nil && path == "" {
				var cleanupErr *ExportCleanupError
				if errors.As(err, &cleanupErr) {
					// The cancel stopped the write, but a file it made is
					// still there: do not claim nothing was written.
					m.finishExport(format, count, pending, recordID, "", err)
					return
				}
				m.exportDialog.canceled = true
				return
			}
			m.finishExport(format, count, pending, recordID, path, err)
		})
	})
}

// saveDirectExport uses the saver's cancelable form when it has one. A
// saver without it still works; the dialog then reports a completed write
// rather than claiming a cancellation that did not happen.
func saveDirectExport(ctx context.Context, save DirectExportSaver, format app.ExportFormat, name string, data []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if cancelable, ok := save.(CancelableDirectExportSaver); ok {
		return cancelable.SaveDirectContext(ctx, format, name, data)
	}
	return save.SaveDirect(format, name, data)
}

// cancelExport asks the running export to stop before it commits a file.
// The dialog keeps its progress until the worker confirms the outcome, so
// it never claims "没有写入文件" while the writer may still be running.
func (m *Model) cancelExport() {
	if !m.exporting || m.exportDialog.canceled {
		return
	}
	if m.exportCancel != nil {
		m.exportCancel()
	}
	m.exportDialog.canceled = true
}

// finishExport records the result the dialog renders. The record and
// review state are untouched.
func (m *Model) finishExport(format app.ExportFormat, count int, pending int64, recordID, path string, err error) {
	if err == nil && path == "" {
		err = errors.New("导出未写入文件")
	}
	// A write that committed before the cancel landed is a success, not a
	// cancellation, so the dialog must not keep the earlier request.
	m.exportDialog.canceled = false
	m.exportOutcome = &exportOutcome{
		format:   format,
		included: count,
		pending:  pending,
		recordID: recordID,
		path:     path,
		err:      err,
	}
}

// exportDialogOutcome is the result this dialog visit owns.
func (m *Model) exportDialogOutcome() *exportOutcome {
	if !m.exportDialog.attempted || m.exportOutcome == nil {
		return nil
	}
	return m.exportOutcome
}

// exportDialogSuccess is the completed write the dialog replaces its
// content with.
func (m *Model) exportDialogSuccess() *exportOutcome {
	o := m.exportDialogOutcome()
	if o == nil || o.err != nil || o.path == "" {
		return nil
	}
	return o
}

// exportDialogFailure is the failed write shown as a banner with 重试.
func (m *Model) exportDialogFailure() *exportOutcome {
	o := m.exportDialogOutcome()
	if o == nil || o.err == nil {
		return nil
	}
	return o
}

// exportDialogEmpty reports the zero-included state: the title explains
// it and the review link takes the initial focus, while both formats may
// still write an empty file (F-22).
func (m *Model) exportDialogEmpty() bool {
	return m.exportData != nil && m.exportData.Summary.Included == 0
}

// exportDialogTitle is the dialog heading. It states why an empty export
// is empty instead of presenting a successful-looking count.
func (m *Model) exportDialogTitle() string {
	if !m.exportDialogEmpty() {
		return "导出这条记录"
	}
	s := m.exportData.Summary
	switch {
	case s.Captured == 0:
		return "这条记录没有采集到任何件"
	case s.Counts.Pending() > 0:
		return fmt.Sprintf("没有可导出的件：还有 %s 件待处理", formatCount(s.Counts.Pending()))
	default:
		return "没有可导出的件"
	}
}

// exportEntryArea is the record workspace's title-bar entry. A build
// without a saver still shows where export lives, disabled.
func (m *Model) exportEntryArea(c *ui.Context) {
	blocked := m.exportEntryBlocked()
	if ui.Button(c, "导出…").Disabled(blocked).Clicked() {
		m.requestExport()
	}
	if blocked {
		if reason := m.exportEntryBlockedReason(); reason != "" {
			t := c.Theme()
			ui.Text(c, reason).FontSize(t.FontSize * 0.85).
				TextColor(t.TextMuted).SingleLine()
		}
	}
}

// exportDialogView renders the export modal. It is built after the page
// so its backdrop covers the whole window; closing it never leaves a
// stray result behind.
func (m *Model) exportDialogView(c *ui.Context, t *ui.Theme) {
	if !m.exportDialog.open {
		return
	}
	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(t.Space(120)).Gap(t.Space(2.5)).Children(func() {
			m.exportDialogContent(c, t)
		})
	})
	if open {
		return
	}
	if m.exporting {
		// The dialog must stay open while a write is in flight; Esc and a
		// backdrop press are ignored. The next frame rebuilds it.
		return
	}
	m.closeExportDialog()
}

// exportDialogContent switches the modal between the pre-export content
// (with its blocked, canceled and failure notices), the running progress
// and the completed write.
func (m *Model) exportDialogContent(c *ui.Context, t *ui.Theme) {
	if m.exportDialogSuccess() != nil {
		m.exportDialogSuccessContent(c, t)
	} else {
		m.exportDialogPreExport(c, t)
	}
	if m.pendingNotificationRecordID != "" {
		ui.Text(c, "关闭导出对话框后会打开通知对应的记录。").TextColor(t.TextMuted).
			FontSize(t.FontSize * 0.9)
	}
}

// exportDialogPreExport shows what the file will contain, the two
// equal-weight format buttons, the format difference and the failure or
// cancellation notice that returns the user to this state. During a write
// only that content's action area changes: the progress and 取消 replace
// the format buttons, as 08 §4 specifies.
func (m *Model) exportDialogPreExport(c *ui.Context, t *ui.Theme) {
	exporting := m.exporting
	blocked := m.exportBlockedReason()
	failure := m.exportDialogFailure()
	empty := m.exportDialogEmpty()

	// MyGo v0.6.2 has no tab-index API. Its focus order comes from the
	// committed tree: in-flow children first, absolute children after. The
	// real close and review controls are built last and attached to their
	// wireframe places; their placeholders must stay invisible to stay out
	// of the focus order. This depends on MyGo internals, so
	// TestExportDialogTabOrder guards the resulting order: 导出 JSON →
	// 导出 CSV → 去校对 → 关闭.
	var closeSlot ui.Element
	closeShown := false
	ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
		ui.Text(c, m.exportDialogTitle()).FontWeight(600).
			FontSize(t.FontSize * 1.3).Grow(1)
		if exporting {
			return
		}
		closeSlot = ui.Button(c, "✕").Label("关闭导出对话框").Invisible()
		closeShown = true
	})

	if m.exportDialog.canceled && !exporting {
		ui.Text(c, "已取消，没有写入文件。").TextColor(t.TextMuted)
	}
	if failure != nil && !exporting {
		ui.Column(c).Background(t.Surface).Border(1, t.Border).
			Radius(t.Radius).Padding(t.Space(2), t.Space(2.5)).
			Gap(t.Space(1)).Children(func() {
			ui.Text(c, "导出失败："+failure.err.Error()).TextColor(t.Danger)
			ui.Text(c, "内部记录未受影响。").TextColor(t.TextMuted).
				FontSize(t.FontSize * 0.9)
			retry := ui.Button(c, "重试").Disabled(blocked != "")
			retry.AutoFocus()
			if retry.Clicked() {
				m.startExport(failure.format)
			}
		})
	}
	if m.recordCoverage() == record.CoveragePartial {
		ui.Column(c).Background(t.Surface).Border(1, t.Border).
			Radius(t.Radius).Padding(t.Space(2), t.Space(2.5)).
			Children(func() {
				ui.Text(c, "这是部分扫描的结果，不是完整库存，不能据此删除没出现的驱动盘。")
			})
	}
	if blocked != "" && failure == nil && !exporting {
		ui.Text(c, blocked).TextColor(t.TextMuted)
	}

	var reviewSlot ui.Element
	reviewShown := false
	if m.exportData != nil {
		reviewSlot, reviewShown = m.exportDialogSummary(c, t, empty, blocked != "", exporting)
	}

	if exporting {
		m.exportDialogProgress(c, t)
		return
	}

	ui.Row(c).Gap(t.Space(2)).Children(func() {
		c.Key("export-dialog-json")
		json := ui.Button(c, "导出 JSON").Grow(1).Disabled(blocked != "")
		if !empty && blocked == "" && failure == nil {
			json.AutoFocus()
		}
		if json.Clicked() {
			m.exportDialogJSON()
		}
		c.Key("export-dialog-csv")
		csv := ui.Button(c, "导出 CSV").Grow(1).Disabled(blocked != "")
		if csv.Clicked() {
			m.exportDialogCSV()
		}
	})
	ui.Text(c, "JSON：带来源扫描、覆盖结论、筛选方式、排除数量和目录。"+
		"CSV：一行一件的表格，不含这些信息。两者来自同一已保存修订。").
		TextColor(t.TextMuted).FontSize(t.FontSize * 0.9)

	if reviewShown {
		c.Key("export-dialog-review")
		review := ui.Button(c, "去校对").TextColor(t.Accent).
			Background(ui.Color{}).Border(0, ui.Color{}).Padding(0, 0).
			Disabled(blocked != "").
			AttachTo(reviewSlot, ui.AnchorCenter, ui.AnchorCenter)
		if empty && blocked == "" {
			// The zero-exportable state starts on the useful next step,
			// review, instead of an empty file action.
			review.AutoFocus()
		}
		if review.Clicked() {
			m.exportDialogGoToReview()
		}
	}
	if closeShown {
		c.Key("export-dialog-close")
		close := ui.Button(c, "✕").Label("关闭导出对话框").
			AttachTo(closeSlot, ui.AnchorCenter, ui.AnchorCenter)
		if close.Clicked() {
			m.closeExportDialog()
		}
		if blocked != "" {
			close.AutoFocus()
		}
	}
}

// exportDialogProgress is the action area while a write runs: an
// indeterminate progress, the cancel action and the record's deletion
// lock. Esc and the backdrop cannot dismiss the dialog here.
func (m *Model) exportDialogProgress(c *ui.Context, t *ui.Theme) {
	format := strings.ToUpper(string(m.exportDialog.format))
	title := fmt.Sprintf("正在导出 %s……", format)
	if m.exportDialog.canceled {
		title = "正在取消……"
	}
	ui.Progress(c, -1)
	ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
		cancel := ui.Button(c, "取消").Disabled(m.exportDialog.canceled)
		cancel.AutoFocus()
		if cancel.Clicked() {
			m.cancelExport()
		}
		ui.Text(c, title).TextColor(t.TextMuted)
	})
	ui.Text(c, "该记录在导出完成前不能删除。").TextColor(t.TextMuted).
		FontSize(t.FontSize * 0.9)
}

// exportDialogSummary renders the counts, coverage, no-screenshot notice
// and write location the design requires to be visible before choosing a
// format.
func (m *Model) exportDialogSummary(c *ui.Context, t *ui.Theme, empty, blocked, locked bool) (ui.Element, bool) {
	s := m.exportData.Summary
	// While a write runs the review link is replaced by progress, so neither
	// its placeholder nor the separator before it may stay in the layout.
	showReview := (s.Excluded > 0 || empty) && !m.exporting
	var reviewSlot ui.Element
	reviewShown := false
	ui.Column(c).Gap(t.Space(1)).Children(func() {
		ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "纳入").Grow(1)
			ui.Textf(c, "%s 件", formatCount(int64(s.Included))).FontWeight(600)
			if s.Counts.Warning > 0 {
				ui.Textf(c, "（含 %s 件有警告）", formatCount(s.Counts.Warning)).
					TextColor(t.TextMuted).FontSize(t.FontSize * 0.9)
			}
		})
		ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "排除").Grow(1)
			ui.Textf(c, "%s 件", formatCount(int64(s.Excluded))).FontWeight(600)
			if s.Counts.Pending() > 0 {
				pending := formatCount(s.Counts.Pending())
				if showReview {
					pending += " ·"
				}
				ui.Textf(c, "待处理 %s", pending).
					TextColor(t.TextMuted).FontSize(t.FontSize * 0.9)
			}
			if showReview {
				reviewSlot = ui.Button(c, "去校对").TextColor(t.Accent).
					Background(ui.Color{}).Border(0, ui.Color{}).Padding(0, 0).
					Disabled(locked).Invisible()
				reviewShown = true
			}
		})
		if s.Excluded > 0 {
			m.exportDialogExcludedReasons(c, t)
		}
		ui.Row(c).Children(func() {
			ui.Text(c, "覆盖").Grow(1)
			ui.Text(c, coverageText(m.recordCoverage()))
		})
		ui.Row(c).Children(func() {
			ui.Text(c, "不含截图").Grow(1)
			ui.Text(c, "—").TextColor(t.TextMuted)
		})
		ui.Row(c).Children(func() {
			ui.Text(c, "写入位置").Grow(1)
			ui.Text(c, "系统下载目录")
		})
	})
	return reviewSlot, reviewShown
}

// exportDialogExcludedReasons is 08 §4's on-demand breakdown of why items
// are outside the file. The four-class counts partition the captured total,
// so the two pending classes are the excluded total.
func (m *Model) exportDialogExcludedReasons(c *ui.Context, t *ui.Theme) {
	s := m.exportData.Summary
	ui.Collapsible(c, "排除原因明细", &m.exportDialog.excludedOpen, func() {
		if s.Counts.PendingBackfill > 0 {
			ui.Textf(c, "待补录：%s 件", formatCount(s.Counts.PendingBackfill)).
				TextColor(t.TextMuted).FontSize(t.FontSize * 0.9)
		}
		if s.Counts.PendingReview > 0 {
			ui.Textf(c, "待校对：%s 件", formatCount(s.Counts.PendingReview)).
				TextColor(t.TextMuted).FontSize(t.FontSize * 0.9)
		}
		if s.Counts.PendingBackfill == 0 && s.Counts.PendingReview == 0 {
			ui.Textf(c, "其他：%s 件", formatCount(int64(s.Excluded))).
				TextColor(t.TextMuted).FontSize(t.FontSize * 0.9)
		}
	})
}

// exportDialogGoToReview leaves the dialog for the item browser's
// 待处理 segment.
func (m *Model) exportDialogGoToReview() {
	m.pendingNotificationRecordID = ""
	m.exportDialog = exportDialogState{}
	m.exportCancel = nil
	m.page = pageResults
	m.recordTab = recordTabItems
	m.setResultFilter(resultFilterPending)
}

// exportDialogSuccessContent replaces the dialog with the completed file,
// the primary "打开下载目录" action (D-11) and, once nothing is pending,
// the screenshot-deletion link (P-13).
func (m *Model) exportDialogSuccessContent(c *ui.Context, t *ui.Theme) {
	o := m.exportDialogSuccess()
	if o == nil {
		return
	}
	ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
		ui.Textf(c, "已导出 %s 件", formatCount(int64(o.included))).FontWeight(600).
			FontSize(t.FontSize * 1.3).Grow(1)
	})
	ui.Column(c).Gap(t.Space(0.5)).Children(func() {
		ui.Text(c, filepath.Base(o.path)).FontWeight(600)
		ui.Text(c, "已写入："+filepath.Dir(o.path)).TextColor(t.TextMuted).
			FontSize(t.FontSize * 0.9)
	})
	if m.exportDialog.openDirErr != "" {
		ui.Text(c, m.exportDialog.openDirErr).TextColor(t.Danger)
	}
	ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
		if m.cfg.OpenDownloadsDirectory != nil {
			open := ui.PrimaryButton(c, "打开下载目录").Grow(1)
			open.AutoFocus()
			if open.Clicked() {
				m.openExportDirectory()
			}
		}
		done := ui.Button(c, "完成")
		if m.cfg.OpenDownloadsDirectory == nil {
			done.AutoFocus()
		}
		if done.Clicked() {
			m.closeExportDialog()
		}
	})
	// P-13 offers screenshot deletion only once nothing needs a person;
	// the outcome froze the four-class pending total with the exported
	// revision, including the backfill items the review-count helper
	// deliberately leaves out.
	if o.pending == 0 {
		link := ui.Button(c, "删除这条记录的截图…").TextColor(t.Accent).
			Background(ui.Color{}).Border(0, ui.Color{}).Padding(0, 0)
		if link.Clicked() {
			m.exportDialogDeleteScreenshots(o.recordID)
		}
	}
}

// openExportDirectory reveals the system downloads directory after a
// successful write. The dialog stays on screen when the system refuses.
func (m *Model) openExportDirectory() {
	if m.cfg.OpenDownloadsDirectory == nil {
		return
	}
	if err := m.cfg.OpenDownloadsDirectory(); err != nil {
		m.exportDialog.openDirErr = "打开下载目录失败：" + err.Error()
		return
	}
	m.exportDialog.openDirErr = ""
}

// exportDialogDeleteScreenshots closes the dialog and asks for the
// measured screenshot deletion of the record that was just exported. The
// confirmation appears on the record's summary, where the same delete
// entry lives.
func (m *Model) exportDialogDeleteScreenshots(recordID string) {
	if recordID == "" {
		return
	}
	// This explicit transfer owns the destination: the exported record's
	// summary and cleanup prompt. A pending notification click is discarded
	// rather than competing with it (PR-19b, item 14).
	m.pendingNotificationRecordID = ""
	m.exportDialog = exportDialogState{}
	m.exportCancel = nil
	m.page = pageResults
	m.showRecordTab(recordTabSummary)
	m.RequestScreenshotCleanup(recordID)
}

// coverageText keeps the coverage conclusion explicit; unknown stays
// unknown and partial is never rounded up to complete.
func coverageText(status record.CoverageStatus) string {
	switch status {
	case record.CoverageComplete:
		return "全部"
	case record.CoveragePartial:
		return "部分"
	case record.CoverageUnknown:
		return "无法确认"
	default:
		return string(status)
	}
}

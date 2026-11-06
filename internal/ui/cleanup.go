package ui

import (
	"fmt"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
	"github.com/egoist/mygo/ui"
)

// cleanupTarget names one of the four confirmed cleanup entries. The
// screenshot actions keep records and data; the record actions remove the
// whole directory. All four ask for a measured preview and an explicit
// confirmation first.
type cleanupTarget int

const (
	cleanupNone cleanupTarget = iota
	cleanupOneScreenshots
	cleanupOneRecord
	cleanupAllScreenshots
	cleanupAllRecords
)

func (t cleanupTarget) all() bool {
	return t == cleanupAllScreenshots || t == cleanupAllRecords
}

// cleanupPrompt is one measured deletion waiting for confirmation.
// previewing means the numbers are still being computed; err keeps a
// failed preview or deletion visible while the entry stays retryable.
type cleanupPrompt struct {
	target     cleanupTarget
	recordID   string
	preview    app.CleanupPreview
	previewing bool
	ready      bool
	running    bool
	err        string
}

// storageClearTarget names the two rebuildable housekeeping entries. They
// never touch records, revisions, review state or the active log.
type storageClearTarget int

const (
	storageClearNone storageClearTarget = iota
	storageClearCache
	storageClearOldLogs
)

// storagePrompt is one measured cache/log clear awaiting confirmation.
type storagePrompt struct {
	target     storageClearTarget
	preview    app.StorageClearPreview
	previewing bool
	ready      bool
	running    bool
	err        string
}

// RequestScreenshotCleanup opens the measured confirmation for one
// record's screenshots.
func (m *Model) RequestScreenshotCleanup(recordID string) {
	m.requestCleanup(cleanupOneScreenshots, recordID)
}

// RequestRecordCleanup opens the measured confirmation for removing one
// record and its screenshots.
func (m *Model) RequestRecordCleanup(recordID string) {
	m.requestCleanup(cleanupOneRecord, recordID)
}

// RequestAllScreenshotCleanup opens the measured confirmation for every
// record's screenshots.
func (m *Model) RequestAllScreenshotCleanup() {
	m.requestCleanup(cleanupAllScreenshots, "")
}

// RequestAllRecordCleanup opens the measured confirmation for every
// record.
func (m *Model) RequestAllRecordCleanup() {
	m.requestCleanup(cleanupAllRecords, "")
}

func (m *Model) requestCleanup(target cleanupTarget, recordID string) {
	if target == cleanupNone {
		return
	}
	m.cleanupNotice = ""
	if reason := m.cleanupBlockedReason(); reason != "" {
		m.cleanup = nil
		m.cleanupNotice = reason
		return
	}
	m.cleanup = &cleanupPrompt{target: target, recordID: recordID, previewing: true}
	m.goAsync(func() {
		preview, err := m.previewCleanup(target, recordID)
		m.update(func() {
			p := m.cleanup
			if p == nil || p.target != target || p.recordID != recordID {
				return
			}
			p.previewing = false
			if err != nil {
				p.err = "无法生成清理预览：" + err.Error()
				return
			}
			p.preview = preview
			if !cleanupHasContent(target, preview) {
				m.cleanup = nil
				m.cleanupNotice = cleanupNothingNotice(target)
				return
			}
			p.ready = true
		})
	})
}

func (m *Model) previewCleanup(target cleanupTarget, recordID string) (app.CleanupPreview, error) {
	switch target {
	case cleanupOneScreenshots:
		return m.cfg.Service.PreviewRecordScreenshots(recordID)
	case cleanupOneRecord:
		return m.cfg.Service.PreviewRecordDeletion(recordID)
	case cleanupAllScreenshots:
		return m.cfg.Service.PreviewAllScreenshots()
	case cleanupAllRecords:
		return m.cfg.Service.PreviewAllRecords()
	default:
		return app.CleanupPreview{}, fmt.Errorf("unknown cleanup target")
	}
}

// CancelCleanup closes the confirmation without touching anything.
func (m *Model) CancelCleanup() {
	if m.cleanup == nil || m.cleanup.running {
		return
	}
	m.cleanup = nil
}

// ConfirmCleanup executes the previewed deletion. The prompt stays open
// with the error when anything failed, so the remaining entries can be
// retried; the history and storage measurements are refreshed either way.
func (m *Model) ConfirmCleanup() {
	p := m.cleanup
	if p == nil || p.running || p.previewing || !p.ready {
		return
	}
	if reason := m.cleanupBlockedReason(); reason != "" {
		p.err = reason
		return
	}
	p.running = true
	p.err = ""
	target, recordID := p.target, p.recordID
	switch target {
	case cleanupOneScreenshots:
		m.goAsync(func() {
			removal, err := m.cfg.Service.DeleteRecordScreenshots(recordID)
			m.update(func() { m.finishScreenshotCleanup(p, removal, err) })
		})
	case cleanupAllScreenshots:
		m.goAsync(func() {
			batch, err := m.cfg.Service.DeleteAllScreenshots()
			m.update(func() { m.finishScreenshotBatch(p, batch, err) })
		})
	case cleanupOneRecord:
		m.goAsync(func() {
			deletion, err := m.cfg.Service.DeleteRecord(recordID)
			m.update(func() { m.finishRecordCleanup(p, deletion, err) })
		})
	case cleanupAllRecords:
		m.goAsync(func() {
			batch, err := m.cfg.Service.DeleteAllRecords()
			m.update(func() { m.finishRecordBatch(p, batch, err) })
		})
	default:
		p.running = false
		p.err = "未知的清理操作。"
	}
}

// RequestCacheClear opens the measured confirmation for the rebuildable
// cache.
func (m *Model) RequestCacheClear() { m.requestStorageClear(storageClearCache) }

// RequestOldLogClear opens the measured confirmation for rotated log
// backups.
func (m *Model) RequestOldLogClear() { m.requestStorageClear(storageClearOldLogs) }

func (m *Model) requestStorageClear(target storageClearTarget) {
	if target == storageClearNone {
		return
	}
	m.cleanupNotice = ""
	if reason := m.storageClearBlockedReason(target); reason != "" {
		m.storagePrompt = nil
		m.cleanupNotice = reason
		return
	}
	m.storagePrompt = &storagePrompt{target: target, previewing: true}
	m.goAsync(func() {
		preview, err := m.previewStorageClear(target)
		m.update(func() {
			p := m.storagePrompt
			if p == nil || p.target != target {
				return
			}
			p.previewing = false
			if err != nil {
				p.err = "无法生成清理预览：" + err.Error()
				return
			}
			p.preview = preview
			if preview.Files == 0 {
				m.storagePrompt = nil
				m.cleanupNotice = storageClearNothingNotice(target)
				return
			}
			p.ready = true
		})
	})
}

func (m *Model) previewStorageClear(target storageClearTarget) (app.StorageClearPreview, error) {
	switch target {
	case storageClearCache:
		return m.cfg.Service.PreviewCacheClear()
	case storageClearOldLogs:
		return m.cfg.Service.PreviewOldLogClear()
	default:
		return app.StorageClearPreview{}, fmt.Errorf("unknown storage clear target")
	}
}

// CancelStorageClear closes the confirmation without touching anything.
func (m *Model) CancelStorageClear() {
	if m.storagePrompt == nil || m.storagePrompt.running {
		return
	}
	m.storagePrompt = nil
}

// ConfirmStorageClear executes the previewed cache or old-log clear. A
// failure keeps the prompt open with its error so the action can be
// retried. The storage measurements are refreshed either way.
func (m *Model) ConfirmStorageClear() {
	p := m.storagePrompt
	if p == nil || p.running || p.previewing || !p.ready {
		return
	}
	if reason := m.storageClearBlockedReason(p.target); reason != "" {
		p.err = reason
		return
	}
	p.running = true
	p.err = ""
	target := p.target
	m.goAsync(func() {
		var preview app.StorageClearPreview
		var err error
		switch target {
		case storageClearCache:
			preview, err = m.cfg.Service.ClearCache()
		case storageClearOldLogs:
			preview, err = m.cfg.Service.ClearOldLogs()
		default:
			err = fmt.Errorf("unknown storage clear target")
		}
		m.update(func() {
			p.running = false
			if m.storagePrompt != p {
				m.refreshStorage()
				return
			}
			if err != nil {
				p.err = "清理失败：" + err.Error()
				m.refreshStorage()
				return
			}
			m.storagePrompt = nil
			m.cleanupNotice = storageClearSuccessNotice(target, preview)
			m.refreshStorage()
			m.returnToPrepareCheck()
		})
	})
}

func (m *Model) storageClearBlockedReason(target storageClearTarget) string {
	switch {
	case m.exitState == exitRunning || m.exitState == exitDone:
		return "正在退出应用，不能清理。"
	case m.storagePrompt != nil && m.storagePrompt.running:
		return "正在执行清理，完成前不能开始另一项。"
	case target == storageClearCache && m.snap.Busy:
		return "扫描会话进行中，结束后才能清理缓存。"
	case target == storageClearCache && m.exporting:
		return "正在导出，完成前不能清理缓存。"
	case target == storageClearCache && (m.updatesState == updateInstalling || m.updatesState == updateInstalled):
		return "正在更新，完成前不能清理缓存。"
	default:
		return ""
	}
}

// OpenDataDirectory asks the entry point to reveal the managed data root.
// The callback is deliberately absent in tests that do not exercise the
// system file manager.
func (m *Model) OpenDataDirectory() {
	if m.cfg.OpenDataDirectory == nil {
		return
	}
	if err := m.cfg.OpenDataDirectory(); err != nil {
		m.cleanupNotice = "打开数据目录失败：" + err.Error()
		return
	}
	m.cleanupNotice = "已打开数据目录。"
}

func storageClearNothingNotice(target storageClearTarget) string {
	if target == storageClearOldLogs {
		return "没有可清理的旧日志备份。"
	}
	return "没有可清理的缓存文件。"
}

func storageClearSuccessNotice(target storageClearTarget, preview app.StorageClearPreview) string {
	if target == storageClearOldLogs {
		return fmt.Sprintf("已清理 %d 个旧日志备份（%s）；当前日志文件保留。",
			preview.Files, formatBytes(preview.Bytes))
	}
	return fmt.Sprintf("已清理缓存 %d 个文件（%s）；历史记录与校对状态不受影响。",
		preview.Files, formatBytes(preview.Bytes))
}

func storagePromptText(p *storagePrompt) string {
	if p.target == storageClearOldLogs {
		return fmt.Sprintf("将删除 %d 个旧日志备份（%s）；当前日志文件保留。",
			p.preview.Files, formatBytes(p.preview.Bytes))
	}
	return fmt.Sprintf("将删除缓存 %d 个文件（%s）；历史记录、修订与校对状态不受影响。",
		p.preview.Files, formatBytes(p.preview.Bytes))
}

// storagePromptArea is the measured confirmation for cache and old-log
// housekeeping. It is separate from the record/screenshot prompt because
// it never reports record or review counts.
func (m *Model) storagePromptArea(c *ui.Context, t *ui.Theme) {
	p := m.storagePrompt
	if p == nil {
		return
	}
	ui.Column(c).Gap(t.Space(1)).Children(func() {
		if p.previewing {
			ui.Text(c, "正在计算清理预览……").TextColor(t.TextMuted)
		} else if p.ready {
			ui.Text(c, storagePromptText(p))
		}
		ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
			// The prompt opens on its safe choice: Enter never clears
			// anything until the user deliberately moves to confirm.
			cancel := ui.Button(c, "取消").Disabled(p.running)
			cancel.AutoFocus()
			if cancel.Clicked() {
				m.CancelStorageClear()
			}
			confirm := ui.Button(c, "确认清理").Disabled(p.running || p.previewing || !p.ready)
			if confirm.Clicked() {
				m.ConfirmStorageClear()
			}
			if p.running {
				ui.Text(c, "正在清理……").TextColor(t.TextMuted)
			}
		})
		if p.err != "" {
			ui.Text(c, p.err).TextColor(t.Danger)
		}
	})
}

func (m *Model) finishScreenshotCleanup(p *cleanupPrompt, removal record.EvidenceRemoval, err error) {
	p.running = false
	if m.cleanup != p {
		m.loadHistory()
		m.refreshStorage()
		return
	}
	if err != nil {
		p.err = "删除截图失败：" + err.Error()
	} else {
		m.cleanup = nil
		m.cleanupNotice = fmt.Sprintf("已删除 %d 张截图（%s），结果与校对状态保留。",
			removal.RemovedFiles, formatBytes(removal.RemovedBytes))
		m.returnToPrepareCheck()
	}
	if m.rec != nil && m.rec.ID == p.recordID {
		m.reloadRecord(m.rec.ID, m.rec.Live)
	}
	m.loadHistory()
	m.refreshStorage()
}

func (m *Model) finishScreenshotBatch(p *cleanupPrompt, batch record.EvidenceBatch, err error) {
	p.running = false
	if m.cleanup != p {
		m.loadHistory()
		m.refreshStorage()
		return
	}
	cleaned := len(batch.Results) - len(batch.Failed)
	partial := fmt.Sprintf("已清理 %d 条记录：删除 %d 张截图（%s）。",
		cleaned, batch.RemovedFiles, formatBytes(batch.RemovedBytes))
	if err != nil {
		p.err = "删除全部截图失败：" + err.Error()
		m.cleanupNotice = partial + "失败条目已保留，可重试。"
	} else {
		m.cleanup = nil
		m.cleanupNotice = partial
		m.returnToPrepareCheck()
	}
	if m.rec != nil {
		m.reloadRecord(m.rec.ID, m.rec.Live)
	}
	m.loadHistory()
	m.refreshStorage()
}

func (m *Model) finishRecordCleanup(p *cleanupPrompt, deletion record.RecordDeletion, err error) {
	p.running = false
	if m.cleanup != p {
		m.loadHistory()
		m.refreshStorage()
		return
	}
	if err != nil {
		p.err = "删除记录失败：" + err.Error() + "。记录保留，可重试。"
	} else {
		m.cleanup = nil
		m.cleanupNotice = fmt.Sprintf("已删除记录 %s（%s）。外部导出的 JSON/CSV 不受影响。",
			deletion.RecordID, formatBytes(deletion.Bytes))
		if m.rec != nil && m.rec.ID == deletion.RecordID {
			m.clearResultsLocked()
		}
		m.returnToPrepareCheck()
	}
	m.loadHistory()
	m.refreshStorage()
}

func (m *Model) finishRecordBatch(p *cleanupPrompt, batch record.RecordDeletionBatch, err error) {
	p.running = false
	if m.cleanup != p {
		m.loadHistory()
		m.refreshStorage()
		return
	}
	partial := fmt.Sprintf("已删除 %d 条记录（%s）。", batch.Records, formatBytes(batch.Bytes))
	if err != nil {
		p.err = "删除全部记录失败：" + err.Error()
		m.cleanupNotice = partial + "失败条目已保留，可重试。"
	} else {
		m.cleanup = nil
		m.cleanupNotice = partial + "外部导出的 JSON/CSV 不受影响。"
		m.returnToPrepareCheck()
	}
	// The results area may have shown a record that is now gone; a
	// partial failure could have removed it too, so close it rather
	// than keep a stale view.
	m.clearResultsLocked()
	m.loadHistory()
	m.refreshStorage()
}

// clearResultsLocked closes the results area and returns it to its empty
// state. It is used when the displayed record was deleted.
func (m *Model) clearResultsLocked() {
	m.recLoad++
	m.loading = false
	m.rec = nil
	m.recData = nil
	m.recErr = ""
	m.exportData = nil
	m.exportBuildErr = ""
	m.exportAfterLoadID = ""
	m.selected = -1
	m.liveSavedShown = 0
	m.edit = nil
	m.navPrompt = nil
	m.reviewErr = ""
	m.reviewNotice = ""
}

// cleanupBlockedReason explains why cleanup is refused now; empty means
// it may proceed. An unsaved edit blocks every entry because the per-record
// actions include its record; a scan, save, export or exit blocks them
// all too. The interface therefore uses one consistent reason.
func (m *Model) cleanupBlockedReason() string {
	switch {
	case !m.exportAvailable():
		return "当前构建未启用导出，历史清理不可用。"
	case m.cleanup != nil && m.cleanup.running:
		return "正在执行清理，完成前不能开始另一项。"
	case m.exitState == exitRunning || m.exitState == exitDone:
		return "正在退出应用，不能清理。"
	case m.snap.SaveError != "":
		return "有保存失败的扫描结果待重试保存，先完成保存再清理。"
	case m.snap.Busy:
		if m.snap.Scan.Phase == scan.PhaseFinalizing {
			return "扫描正在收尾保存，完成前不能清理。"
		}
		return "扫描会话进行中，结束后才能清理。"
	case m.snap.SavePending:
		return "有扫描内容待写入，等保存完成后再清理。"
	case m.exporting:
		return "正在导出，完成前不能清理。"
	case m.reviewSaving:
		return "正在保存修改，完成前不能清理。"
	case m.editingDirty():
		return "当前记录有未保存的修改，保存或放弃后才能清理。"
	default:
		return ""
	}
}

// refreshStorage measures the storeroom in the background. The old
// numbers are dropped first: a failed measurement is reported as such
// rather than presented as an exact, stale value.
func (m *Model) refreshStorage() {
	m.storageLoading = true
	m.storageErr = ""
	m.storage = nil
	m.goAsync(func() {
		usage, err := m.cfg.Service.StorageUsage()
		m.update(func() {
			m.storageLoading = false
			if err != nil {
				m.storageErr = err.Error()
				m.storage = nil
				return
			}
			m.storage = &usage
		})
	})
}

func cleanupHasContent(target cleanupTarget, preview app.CleanupPreview) bool {
	switch target {
	case cleanupOneScreenshots, cleanupAllScreenshots:
		return preview.EvidenceFiles > 0
	default:
		return preview.Records > 0
	}
}

func cleanupNothingNotice(target cleanupTarget) string {
	switch target {
	case cleanupOneScreenshots, cleanupAllScreenshots:
		return "没有可删除的截图。"
	default:
		return "没有可删除的记录。"
	}
}

// cleanupPromptArea is the measured confirmation shared by the results
// page and the settings danger zone.
func (m *Model) cleanupPromptArea(c *ui.Context, t *ui.Theme) {
	p := m.cleanup
	if p == nil {
		return
	}
	ui.Column(c).Gap(t.Space(1)).Children(func() {
		if p.previewing {
			ui.Text(c, "正在计算清理预览……").TextColor(t.TextMuted)
		} else if p.ready {
			ui.Text(c, cleanupPreviewText(p))
		}
		if p.preview.Unreadable > 0 {
			ui.Textf(c, "其中 %d 条记录无法读取，待校对数量不完整。", p.preview.Unreadable).
				TextColor(t.TextMuted).FontSize(t.FontSize * 0.9)
		}
		ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
			// The destructive confirmation opens on 取消, so a stray Enter
			// closes it instead of deleting anything.
			cancel := ui.Button(c, "取消").Disabled(p.running)
			cancel.AutoFocus()
			if cancel.Clicked() {
				m.CancelCleanup()
			}
			confirm := ui.Button(c, "确认删除").Disabled(p.running || p.previewing || !p.ready)
			if confirm.Clicked() {
				m.ConfirmCleanup()
			}
			if p.running {
				ui.Text(c, "正在删除……").TextColor(t.TextMuted)
			}
		})
		if p.err != "" {
			ui.Text(c, p.err).TextColor(t.Danger)
		}
	})
}

func cleanupPreviewText(p *cleanupPrompt) string {
	preview := p.preview
	switch p.target {
	case cleanupOneScreenshots:
		return fmt.Sprintf("将删除记录 %s 的 %d 张截图（%s），保留结果、修订与校对状态；待校对 %d 项。",
			p.recordID, preview.EvidenceFiles, formatBytes(preview.EvidenceBytes), preview.PendingReview)
	case cleanupOneRecord:
		return fmt.Sprintf("将永久删除记录 %s（共 %s，其中截图 %d 张 %s）；待校对 %d 项。外部导出的 JSON/CSV 不受影响。",
			p.recordID, formatBytes(preview.TotalBytes), preview.EvidenceFiles,
			formatBytes(preview.EvidenceBytes), preview.PendingReview)
	case cleanupAllScreenshots:
		return fmt.Sprintf("将删除 %d 条记录的截图（%d 张，%s），保留结果、修订与校对状态；待校对 %d 项。",
			preview.Records, preview.EvidenceFiles, formatBytes(preview.EvidenceBytes), preview.PendingReview)
	default: // cleanupAllRecords
		return fmt.Sprintf("将永久删除 %d 条记录（共 %s，其中截图 %d 张 %s）；待校对 %d 项。外部导出的 JSON/CSV 不受影响。",
			preview.Records, formatBytes(preview.TotalBytes), preview.EvidenceFiles,
			formatBytes(preview.EvidenceBytes), preview.PendingReview)
	}
}

// formatBytes renders a byte count for measurements. Values below one
// KiB stay exact bytes; larger ones keep one decimal, which is enough for
// a cleanup decision and never pretends to be more precise than it is.
func formatBytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	value := float64(n)
	for _, unit := range []string{"KB", "MB", "GB", "TB"} {
		value /= 1024
		if value < 1024 || unit == "TB" {
			return fmt.Sprintf("%.1f %s", value, unit)
		}
	}
	return fmt.Sprintf("%d B", n)
}

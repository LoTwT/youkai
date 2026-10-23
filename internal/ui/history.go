package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/egoist/mygo/ui"
)

// historyBulkKind names the two multi-select cleanup actions.
type historyBulkKind int

const (
	historyBulkNone historyBulkKind = iota
	historyBulkScreenshots
	historyBulkRecords
)

// historyBulkPrompt is a measured selected-record cleanup. Preview and
// confirmation are bound to the IDs captured when the prompt opened, so a
// later filter change cannot silently widen the action.
type historyBulkPrompt struct {
	kind       historyBulkKind
	ids        []string
	preview    app.CleanupPreview
	previewing bool
	ready      bool
	running    bool
	err        string
}

// historyExportsBlocked reports whether the per-row export actions must be
// disabled. A different record's export still respects the current
// record's unsaved edit and every shared export/exit guard.
func (m *Model) historyExportsBlocked() bool {
	return !m.exportAvailable() ||
		m.historyExporting != "" ||
		m.exporting ||
		m.exitState == exitRunning || m.exitState == exitDone ||
		m.reviewSaving ||
		m.editingDirty()
}

// ExportHistory exports one history record from its newest verifiable
// checkpoint. It does not change the results area's current record or its
// frozen export snapshot.
func (m *Model) ExportHistory(recordID string, format app.ExportFormat) {
	save, direct := m.exportWriter()
	if recordID == "" || save == nil || m.historyExportsBlocked() {
		return
	}
	version := m.cfg.AppVersion
	m.historyExporting = recordID
	m.historyNotice = ""
	m.historyError = ""
	m.goAsync(func() {
		fail := func(err error) {
			m.update(func() {
				m.historyExporting = ""
				if direct {
					m.finishDirectExport(format, 0, recordID, false, "", err)
					return
				}
				m.historyError = "导出失败：" + err.Error()
			})
		}
		rec, err := m.cfg.Service.LoadRecord(recordID)
		if err != nil {
			fail(fmt.Errorf("读取记录：%w", err))
			return
		}
		export, err := app.BuildUsableExport(rec)
		if err != nil {
			fail(fmt.Errorf("生成导出：%w", err))
			return
		}
		var data []byte
		switch format {
		case app.ExportJSON:
			data, err = export.JSON(time.Now().UTC(), version)
		case app.ExportCSV:
			data, err = export.CSV()
		default:
			err = fmt.Errorf("未知导出格式 %q", format)
		}
		if err != nil {
			fail(err)
			return
		}
		path, err := save(format, app.DefaultExportName(rec.Checkpoint.Session, format), data)
		m.update(func() {
			m.historyExporting = ""
			if direct {
				m.finishDirectExport(format, export.Summary.Included, recordID, false, path, err)
				return
			}
			switch {
			case err != nil:
				m.historyError = "导出失败：" + err.Error()
			case path == "":
				m.historyNotice = "已取消导出。"
			default:
				m.historyNotice = fmt.Sprintf("已导出 %s：%s", strings.ToUpper(string(format)), path)
			}
		})
	})
}

// RequestSelectedScreenshotCleanup opens the measured confirmation for the
// selected records' screenshots.
func (m *Model) RequestSelectedScreenshotCleanup() {
	m.requestHistoryBulk(historyBulkScreenshots)
}

// RequestSelectedRecordCleanup opens the measured confirmation for the
// selected records and their screenshots.
func (m *Model) RequestSelectedRecordCleanup() {
	m.requestHistoryBulk(historyBulkRecords)
}

func (m *Model) requestHistoryBulk(kind historyBulkKind) {
	ids := m.selectedHistoryIDs()
	if kind == historyBulkNone || len(ids) == 0 {
		return
	}
	if reason := m.cleanupBlockedReason(); reason != "" {
		m.historyNotice = reason
		return
	}
	m.historyNotice = ""
	m.historyError = ""
	m.historyBulk = &historyBulkPrompt{kind: kind, ids: ids, previewing: true}
	m.goAsync(func() {
		preview, err := m.previewHistoryBulk(kind, ids)
		m.update(func() {
			p := m.historyBulk
			if p == nil || p.kind != kind {
				return
			}
			p.previewing = false
			if err != nil {
				p.err = "无法生成清理预览：" + err.Error()
				return
			}
			p.preview = preview
			if kind == historyBulkScreenshots && preview.EvidenceFiles == 0 {
				m.historyBulk = nil
				m.historyNotice = "选中的记录没有可删除的截图。"
				return
			}
			p.ready = true
		})
	})
}

func (m *Model) previewHistoryBulk(kind historyBulkKind, ids []string) (app.CleanupPreview, error) {
	var out app.CleanupPreview
	for _, id := range ids {
		var preview app.CleanupPreview
		var err error
		switch kind {
		case historyBulkScreenshots:
			preview, err = m.cfg.Service.PreviewRecordScreenshots(id)
		case historyBulkRecords:
			preview, err = m.cfg.Service.PreviewRecordDeletion(id)
		default:
			return app.CleanupPreview{}, fmt.Errorf("未知的清理类型")
		}
		if err != nil {
			return app.CleanupPreview{}, fmt.Errorf("%s：%w", id, err)
		}
		out.Records++
		out.EvidenceFiles += preview.EvidenceFiles
		out.EvidenceBytes += preview.EvidenceBytes
		out.TotalBytes += preview.TotalBytes
		out.PendingReview += preview.PendingReview
		out.Unreadable += preview.Unreadable
	}
	return out, nil
}

// CancelHistoryBulk closes the selected cleanup without touching anything.
func (m *Model) CancelHistoryBulk() {
	if m.historyBulk == nil || m.historyBulk.running {
		return
	}
	m.historyBulk = nil
}

// ConfirmHistoryBulk executes the selected cleanup. Failures are reported
// per run; successfully deleted records are not restored.
func (m *Model) ConfirmHistoryBulk() {
	p := m.historyBulk
	if p == nil || p.running || p.previewing || !p.ready {
		return
	}
	if reason := m.cleanupBlockedReason(); reason != "" {
		p.err = reason
		return
	}
	p.running = true
	p.err = ""
	kind := p.kind
	ids := append([]string(nil), p.ids...)
	m.goAsync(func() {
		var removedFiles int
		var removedBytes int64
		var failures int
		var firstErr error
		failedIDs := make([]string, 0)
		deleted := make(map[string]bool, len(ids))
		for _, id := range ids {
			switch kind {
			case historyBulkScreenshots:
				removal, err := m.cfg.Service.DeleteRecordScreenshots(id)
				if err != nil {
					failures++
					failedIDs = append(failedIDs, id)
					if firstErr == nil {
						firstErr = err
					}
					continue
				}
				removedFiles += removal.RemovedFiles
				removedBytes += removal.RemovedBytes
			case historyBulkRecords:
				deletion, err := m.cfg.Service.DeleteRecord(id)
				if err != nil {
					failures++
					failedIDs = append(failedIDs, id)
					if firstErr == nil {
						firstErr = err
					}
					continue
				}
				deleted[id] = true
				removedBytes += deletion.Bytes
			}
		}
		m.update(func() {
			p.running = false
			if m.historyBulk != p {
				m.loadHistory()
				m.refreshStorage()
				return
			}
			if failures > 0 {
				p.err = fmt.Sprintf("已处理 %d 条，%d 条失败，可重试。首个错误：%v",
					len(ids)-failures, failures, firstErr)
				m.ClearHistorySelection()
				for _, id := range failedIDs {
					m.setHistorySelected(id, true)
				}
				p.ids = append([]string(nil), failedIDs...)
				p.preview = app.CleanupPreview{}
				p.ready = false
				p.previewing = true
				m.loadHistory()
				m.refreshStorage()
				m.goAsync(func() {
					preview, err := m.previewHistoryBulk(kind, failedIDs)
					m.update(func() {
						if m.historyBulk != p {
							return
						}
						p.previewing = false
						if err != nil {
							p.err = "无法生成重试预览：" + err.Error()
							return
						}
						p.preview = preview
						p.ready = true
					})
				})
				return
			}
			m.historyBulk = nil
			if kind == historyBulkScreenshots {
				m.historyNotice = fmt.Sprintf("已删除选中记录的 %d 张截图（%s），结果与校对状态保留。",
					removedFiles, formatBytes(removedBytes))
			} else {
				m.historyNotice = fmt.Sprintf("已删除选中 %d 条记录（%s）。外部导出的 JSON/CSV 不受影响。",
					len(ids), formatBytes(removedBytes))
			}
			if m.rec != nil {
				if deleted[m.rec.ID] {
					m.clearResultsLocked()
				} else if kind == historyBulkScreenshots && containsID(ids, m.rec.ID) {
					m.reloadRecord(m.rec.ID, m.rec.Live)
				}
			}
			m.ClearHistorySelection()
			m.historyError = ""
			m.loadHistory()
			m.refreshStorage()
		})
	})
}

func containsID(ids []string, id string) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}

// ClearHistorySelection drops every checkbox without changing records.
func (m *Model) ClearHistorySelection() {
	for id := range m.historySelected {
		delete(m.historySelected, id)
	}
}

func (m *Model) historyBulkArea(c *ui.Context, t *ui.Theme) {
	p := m.historyBulk
	if p == nil {
		return
	}
	ui.Column(c).Gap(t.Space(1)).Children(func() {
		if p.previewing {
			ui.Text(c, "正在计算选中记录的清理预览……").TextColor(t.TextMuted)
		} else if p.ready {
			ui.Text(c, m.historyBulkPreviewText(p))
		}
		if p.preview.Unreadable > 0 {
			ui.Textf(c, "其中 %d 条记录无法读取，待校对数量不完整。", p.preview.Unreadable).
				TextColor(t.TextMuted).FontSize(t.FontSize * 0.9)
		}
		ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
			confirm := ui.Button(c, "确认删除选中").Disabled(p.running || p.previewing || !p.ready)
			if confirm.Clicked() {
				m.ConfirmHistoryBulk()
			}
			if ui.Button(c, "取消").Disabled(p.running).Clicked() {
				m.CancelHistoryBulk()
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

func (m *Model) historyBulkPreviewText(p *historyBulkPrompt) string {
	if p.kind == historyBulkScreenshots {
		return fmt.Sprintf("将删除选中 %d 条记录的截图（%d 张，%s），保留结果、修订与校对状态；待校对 %d 项。",
			p.preview.Records, p.preview.EvidenceFiles, formatBytes(p.preview.EvidenceBytes), p.preview.PendingReview)
	}
	return fmt.Sprintf("将永久删除选中 %d 条记录（共 %s，其中截图 %d 张 %s）；待校对 %d 项。外部导出的 JSON/CSV 不受影响。",
		p.preview.Records, formatBytes(p.preview.TotalBytes), p.preview.EvidenceFiles,
		formatBytes(p.preview.EvidenceBytes), p.preview.PendingReview)
}

package ui

import (
	"fmt"
	"time"

	"github.com/LoTwT/youkai/internal/app"
)

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
// checkpoint into the downloads directory. It does not change the results
// area's current record or its frozen export snapshot.
func (m *Model) ExportHistory(recordID string, format app.ExportFormat) {
	save := m.cfg.DirectExportSaver
	if recordID == "" || save == nil || m.historyExportsBlocked() {
		return
	}
	version := m.cfg.AppVersion
	m.historyExporting = recordID
	m.goAsync(func() {
		fail := func(err error) {
			m.update(func() {
				m.historyExporting = ""
				m.finishDirectExport(format, 0, recordID, false, "", err)
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
		path, err := save.SaveDirect(format, app.DefaultExportName(rec.Checkpoint.Session, format), data)
		m.update(func() {
			m.historyExporting = ""
			m.finishDirectExport(format, export.Summary.Included, recordID, false, path, err)
		})
	})
}

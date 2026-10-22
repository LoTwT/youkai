package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/egoist/mygo/ui"
)

// ExportSaver chooses where one usable-data file goes and writes it. The
// production implementation uses the system save dialog followed by an
// atomic write; tests inject a fake. A cancelled dialog returns an empty
// path and no error. The interface never reads the record or changes the
// checkpoint: the bytes are already a frozen revision.
type ExportSaver interface {
	Save(format app.ExportFormat, defaultName string, data []byte) (path string, err error)
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

// exportBlockedReason explains why the export buttons are disabled. Empty
// means both formats may be exported now. A missing saver hides the
// controls entirely rather than showing permanently disabled buttons.
func (m *Model) exportBlockedReason() string {
	switch {
	case m.cfg.ExportSaver == nil:
		return ""
	case m.exporting:
		return "正在导出……"
	case m.historyExporting != "":
		return "正在导出历史记录，完成前不能导出。"
	case m.exitState == exitRunning || m.exitState == exitDone:
		return "正在退出应用，不能导出。"
	case m.reviewSaving:
		return "正在保存修改，完成前不能导出。"
	case m.editingDirty():
		return "有未保存的修改，保存或放弃后可导出。"
	case m.rec == nil || m.recData == nil:
		return "结果尚未加载，不能导出。"
	case m.rec.Live && m.snap.Busy:
		return "扫描尚未收尾，结束后可导出已保存的固定修订。"
	case m.exportBuildErr != "":
		return "无法生成导出：" + m.exportBuildErr
	case m.exportData == nil:
		return "导出清单尚未就绪，不能导出。"
	case m.exportData.Summary.Included == 0:
		return "没有可导出的条目，不能导出。"
	default:
		return ""
	}
}

// ExportJSON starts the JSON export: the chosen target and the write run
// asynchronously, so the dialog never blocks the window. The state of the
// internal record is untouched on cancel, refusal or write failure.
func (m *Model) ExportJSON() { m.export(app.ExportJSON) }

// ExportCSV starts the CSV export from the same frozen snapshot.
func (m *Model) ExportCSV() { m.export(app.ExportCSV) }

// export renders and saves one format. Both output formats derive from
// m.exportData, the snapshot built for the record revision currently on
// screen, so a concurrent scan save or a reopened record cannot mix
// revisions inside the file.
func (m *Model) export(format app.ExportFormat) {
	if m.cfg.ExportSaver == nil || m.exportData == nil || m.exportBlockedReason() != "" {
		return
	}
	export := m.exportData
	saver := m.cfg.ExportSaver
	version := m.cfg.AppVersion
	name := app.DefaultExportName(m.recData.Checkpoint.Session, format)
	now := time.Now().UTC()

	m.exporting = true
	m.exportNotice = ""
	m.exportError = ""
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
		if err != nil {
			m.update(func() {
				m.exporting = false
				m.exportError = "导出失败：" + err.Error()
			})
			return
		}
		path, err := saver.Save(format, name, data)
		m.update(func() {
			m.exporting = false
			switch {
			case err != nil:
				m.exportError = "导出失败：" + err.Error()
			case path == "":
				m.exportNotice = "已取消导出。"
			default:
				m.exportNotice = fmt.Sprintf("已导出 %s：%s", strings.ToUpper(string(format)), path)
			}
		})
	})
}

// exportArea is the results area's export control: the two formats side by
// side with no default, the usable/excluded counts, the coverage
// conclusion and the no-screenshots notice.
func (m *Model) exportArea(c *ui.Context, t *ui.Theme) {
	if m.cfg.ExportSaver == nil {
		return
	}
	blocked := m.exportBlockedReason()
	ui.Column(c).Gap(t.Space(1)).Children(func() {
		ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
			if ui.Button(c, "导出 JSON").Disabled(blocked != "").Clicked() {
				m.ExportJSON()
			}
			if ui.Button(c, "导出 CSV").Disabled(blocked != "").Clicked() {
				m.ExportCSV()
			}
		})
		ui.Text(c, m.exportSummaryText()).TextColor(t.TextMuted)
		if blocked != "" && !m.exporting {
			ui.Text(c, blocked).TextColor(t.TextMuted).FontSize(t.FontSize * 0.9)
		}
		if m.exportError != "" {
			ui.Text(c, m.exportError).TextColor(t.Danger)
		}
		if m.exportNotice != "" {
			ui.Text(c, m.exportNotice)
		}
	})
}

// exportSummaryText renders what the two files will contain: captured,
// exportable, pending, failed and unprocessed counts, the coverage
// conclusion and the fact that no evidence is included. The categories are
// mutually exclusive so the reader can see why an item is not exported.
func (m *Model) exportSummaryText() string {
	if m.exportData == nil {
		return "导出：数据尚未就绪 · 不含截图"
	}
	s := m.exportData.Summary
	c := m.recordCounts()
	text := fmt.Sprintf("已采集 %d 件 · 可导出 %d 件 · 待校对 %d 件 · 失败 %d 件 · 未处理 %d 件",
		s.Captured, s.Included, c.Pending, c.Failed, c.Unprocessed)
	if c.OtherExcluded > 0 {
		text += fmt.Sprintf(" · 其他排除 %d 件", c.OtherExcluded)
	}
	text += fmt.Sprintf(" · 覆盖：%s · 不含截图", coverageText(s.Coverage))
	if s.ExpectedCount != nil {
		text += fmt.Sprintf("（预期 %d 件）", *s.ExpectedCount)
	}
	return text
}

// coverageText keeps the coverage conclusion explicit; unknown stays
// unknown and partial is never rounded up to complete.
func coverageText(status record.CoverageStatus) string {
	switch status {
	case record.CoverageComplete:
		return "完整"
	case record.CoveragePartial:
		return "部分"
	case record.CoverageUnknown:
		return "无法确认"
	default:
		return string(status)
	}
}

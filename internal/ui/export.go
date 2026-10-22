package ui

import (
	"errors"
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

// DirectExportSaver writes one usable-data file without opening a dialog.
// The page shell uses it to write straight into the system downloads
// directory; the implementation picks a numbered name instead of
// overwriting an existing file. It never reads the record or changes the
// checkpoint.
type DirectExportSaver interface {
	SaveDirect(format app.ExportFormat, defaultName string, data []byte) (path string, err error)
}

// ExportDirectoryError reports that the direct export destination cannot
// take the file. The page shell turns it into the "open directory" and
// "go to settings" actions instead of a save dialog.
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

// exportOutcome is the last direct export result. It is global rather than
// slot-local so a destination switch cannot hide or move the notice.
type exportOutcome struct {
	format   app.ExportFormat
	included int
	recordID string
	live     bool
	path     string
	err      error
	dirErr   *ExportDirectoryError
	openErr  string
}

// exportBusy reports whether any export is in flight. Review actions stay
// disabled meanwhile so the file and the on-screen revision cannot part
// ways while the snapshot is written.
func (m *Model) exportBusy() bool {
	return m.exporting || m.historyExporting != ""
}

// reviewActionsBlocked reports whether review edits and confirmations must
// stay disabled: while a save is in flight, the latest checkpoint is still
// loading (the visible record is an older snapshot then), or an export
// holds its snapshot.
func (m *Model) reviewActionsBlocked() bool {
	return m.reviewSaving || m.loading || m.exportBusy()
}

// exportAvailable reports whether this build offers an export entry. The
// page shell prefers the direct writer and keeps the save dialog for the
// legacy layout.
func (m *Model) exportAvailable() bool {
	return m.cfg.ExportSaver != nil || m.cfg.DirectExportSaver != nil
}

// exportWriter picks the save capability for the current layout. The page
// shell writes directly; the legacy layout keeps the save dialog. A
// configuration with only the dialog stays usable in either layout.
func (m *Model) exportWriter() (save func(app.ExportFormat, string, []byte) (string, error), direct bool) {
	if m.cfg.PageLayout && m.cfg.DirectExportSaver != nil {
		return m.cfg.DirectExportSaver.SaveDirect, true
	}
	if m.cfg.ExportSaver != nil {
		return m.cfg.ExportSaver.Save, false
	}
	return nil, false
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
	case !m.exportAvailable():
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
	case m.loading:
		return "正在加载最新检查点，加载完成前不能导出。"
	case m.recErr != "":
		return "记录读取失败，不能导出旧快照。"
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
	save, direct := m.exportWriter()
	if save == nil || m.exportData == nil || m.exportBlockedReason() != "" {
		return
	}
	// Capture the slot's snapshot, identity and count now. A destination
	// switch or a later save during the write cannot change this export.
	export := m.exportData
	count := export.Summary.Included
	version := m.cfg.AppVersion
	name := app.DefaultExportName(m.recData.Checkpoint.Session, format)
	recordID := m.rec.ID
	live := m.rec.Live
	now := time.Now().UTC()

	m.exporting = true
	m.exportNotice = ""
	m.exportError = ""
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
		if err != nil {
			m.update(func() {
				m.exporting = false
				if direct {
					m.finishDirectExport(format, count, recordID, live, "", err)
					return
				}
				m.exportError = "导出失败：" + err.Error()
			})
			return
		}
		path, err := save(format, name, data)
		m.update(func() {
			m.exporting = false
			if direct {
				m.finishDirectExport(format, count, recordID, live, path, err)
				return
			}
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

// finishDirectExport records the result globally. The record and review
// state are untouched; only the status strip changes.
func (m *Model) finishDirectExport(format app.ExportFormat, count int, recordID string, live bool, path string, err error) {
	if err == nil && path == "" {
		err = errors.New("导出未写入文件")
	}
	outcome := &exportOutcome{
		format:   format,
		included: count,
		recordID: recordID,
		live:     live,
		path:     path,
		err:      err,
	}
	var dirErr *ExportDirectoryError
	if errors.As(err, &dirErr) {
		outcome.dirErr = dirErr
	}
	m.exportOutcome = outcome
}

// exportOutcomeText is the status strip's message: format and included
// count on success, the path and reason on failure, and which record the
// file belongs to when the user has already moved elsewhere.
func (o *exportOutcome) text() string {
	recordName := "当前结果"
	if o.live {
		recordName = "本次扫描"
	} else if o.recordID != "" {
		recordName = "历史记录 " + o.recordID
	}
	formatName := strings.ToUpper(string(o.format))
	if o.err != nil {
		return fmt.Sprintf("导出失败（%s，%s）：%v", formatName, recordName, o.err)
	}
	return fmt.Sprintf("已导出 %s（%d 件，%s）：%s", formatName, o.included, recordName, o.path)
}

// exportArea is the results area's export control: the two formats side by
// side with no default, the usable/excluded counts, the coverage
// conclusion and the no-screenshots notice.
func (m *Model) exportArea(c *ui.Context, t *ui.Theme) {
	if !m.exportAvailable() {
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

// exportStatusVisible reports whether the page shell must reserve a line
// for the export status. The notice is global so switching destinations
// keeps it visible.
func (m *Model) exportStatusVisible() bool {
	return m.cfg.PageLayout && (m.exporting || m.exportOutcome != nil)
}

// exportStatusArea is the page shell's status strip for a direct export:
// the running state, the success message with format and count, or an
// actionable directory failure. It never replaces the page or a dialog.
func (m *Model) exportStatusArea(c *ui.Context, t *ui.Theme) {
	if m.exporting {
		ui.Text(c, "正在导出……").TextColor(t.TextMuted)
		return
	}
	o := m.exportOutcome
	if o == nil {
		return
	}
	ui.Column(c).Gap(t.Space(1)).Children(func() {
		ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
			color := t.Text
			if o.err != nil {
				color = t.Danger
			}
			ui.Text(c, o.text()).TextColor(color).SingleLine()
			ui.Box(c).Grow(1)
			if o.dirErr == nil {
				return
			}
			if m.cfg.OpenDownloadsDirectory != nil {
				if ui.Button(c, "打开目录").Clicked() {
					if err := m.cfg.OpenDownloadsDirectory(); err != nil {
						o.openErr = "打开目录失败：" + err.Error()
					} else {
						o.openErr = ""
					}
				}
			}
			if ui.Button(c, "去设置").Clicked() {
				m.page = pageSettings
			}
		})
		if o.openErr != "" {
			ui.Text(c, o.openErr).TextColor(t.Danger).FontSize(t.FontSize * 0.9)
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

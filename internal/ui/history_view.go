package ui

import (
	"github.com/LoTwT/youkai/internal/app"
	"github.com/egoist/mygo/ui"
)

// historyPage is the saved-records destination: the record list with its
// date filter and per-record actions.
func (m *Model) historyPage(c *ui.Context, t *ui.Theme) {
	ui.Text(c, "历史记录").FontWeight(600)
	ui.Text(c, "已保存的扫描修订；打开记录后回到结果与校对。").TextColor(t.TextMuted).
		FontSize(t.FontSize * 0.9)
	if len(m.history) == 0 {
		ui.Text(c, "暂无已保存的记录。").TextColor(t.TextMuted)
		return
	}
	m.historyActions(c, t)
}

// historyActions lists the visible saved records with their metadata and
// per-record actions. The picker above stays for keyboard selection; this
// list is the per-record entry point T6 requires, kept compact with a
// bounded scroll.
func (m *Model) historyActions(c *ui.Context, t *ui.Theme) {
	blocked := m.cleanupBlockedReason()
	selected := m.selectedHistoryIDs()
	ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "日期筛选").TextColor(t.TextMuted)
		ui.TextInput(c, &m.historyFilter).Width(110).Placeholder("YYYY-MM-DD")
		if ui.Button(c, "清除筛选").Clicked() {
			m.historyFilter = ""
		}
		if m.historyFilterInvalid() {
			ui.Text(c, "格式应为 YYYY-MM-DD。").TextColor(t.Danger).FontSize(t.FontSize * 0.9)
		}
		if len(selected) > 0 {
			ui.Textf(c, "已选 %d 条", len(selected)).TextColor(t.TextMuted)
			if ui.Button(c, "删除选中截图").Disabled(blocked != "").Clicked() {
				m.RequestSelectedScreenshotCleanup()
			}
			if ui.Button(c, "删除选中记录").Disabled(blocked != "").Clicked() {
				m.RequestSelectedRecordCleanup()
			}
			if ui.Button(c, "清除选择").Clicked() {
				m.ClearHistorySelection()
			}
		}
	})
	ui.ScrollBoth(c).MaxHeight(180).Children(func() {
		for _, entry := range m.visibleHistory() {
			m.historyRow(c, t, entry, blocked)
		}
	})
	if m.historyBulk != nil {
		m.historyBulkArea(c, t)
	}
	if m.historyNotice != "" {
		ui.Text(c, m.historyNotice)
	}
	if m.historyError != "" {
		ui.Text(c, m.historyError).TextColor(t.Danger)
	}
	if blocked != "" {
		ui.Text(c, blocked).TextColor(t.TextMuted).FontSize(t.FontSize * 0.9)
	}
}

func (m *Model) historyRow(c *ui.Context, t *ui.Theme, entry HistoryEntry, blocked string) {
	ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
		selected := m.historySelected[entry.ID]
		if ui.Checkbox(c, &selected, "").Changed() {
			m.setHistorySelected(entry.ID, selected)
		}
		text := m.historyRowText(entry)
		if m.rec != nil && m.rec.ID == entry.ID {
			text += "（当前）"
		}
		ui.Text(c, text).TextColor(t.TextMuted).SingleLine().Grow(1)
		if ui.Button(c, "打开").Clicked() {
			m.histChoice = entry.Label
			m.OpenHistory()
		}
		if !m.cfg.PageLayout {
			if m.cfg.ExportSaver != nil {
				if ui.Button(c, "导出 JSON").Disabled(m.historyExportsBlocked()).Clicked() {
					m.ExportHistory(entry.ID, app.ExportJSON)
				}
				if ui.Button(c, "导出 CSV").Disabled(m.historyExportsBlocked()).Clicked() {
					m.ExportHistory(entry.ID, app.ExportCSV)
				}
			}
			if ui.Button(c, "删除截图").Disabled(blocked != "").Clicked() {
				m.RequestScreenshotCleanup(entry.ID)
			}
			if ui.Button(c, "删除当前记录").Disabled(blocked != "").Clicked() {
				m.RequestRecordCleanup(entry.ID)
			}
		}
	})
}

func (m *Model) historyEmptyText() string {
	if m.histErr != "" {
		return "读取失败。"
	}
	if m.historyFilterInvalid() {
		return "日期格式应为 YYYY-MM-DD。"
	}
	if len(m.history) > 0 && len(m.visibleHistory()) == 0 {
		return "没有匹配筛选条件的记录。"
	}
	return "暂无已保存的记录。"
}

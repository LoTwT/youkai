package ui

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestPageLayoutTabsRouteToEachPage(t *testing.T) {
	m, q := newTestModel(t, Config{Service: &fakeService{}, PageLayout: true})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	if !tt.HasText(scanHint) {
		t.Fatal("scan page does not show the scan hint")
	}
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("关闭窗口时停止扫描并退出应用") {
		t.Fatal("settings page does not show the close preference")
	}
	if err := tt.Click("历史"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("暂无已保存的记录。") {
		t.Fatal("history page does not show its empty state")
	}
	if err := tt.Click("扫描"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("结果与校对"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("还没有扫描结果。开始一次扫描，或从历史记录打开一条已保存的记录。") {
		t.Fatal("results page does not show its empty state")
	}
	if err := tt.Click("扫描"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText(scanHint) {
		t.Fatal("scan tab did not return to the scan page")
	}
}

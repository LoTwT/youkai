package ui

import (
	"image"
	"image/color"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
	"github.com/egoist/mygo/ui"
)

// TestPageLayoutSidebarWidths guards the design's two rail widths: 168
// DIP on wide windows and 144 DIP from 1024 down to 860.
func TestPageLayoutSidebarWidths(t *testing.T) {
	for _, tc := range []struct {
		width int
		want  float32
	}{
		{1440, sidebarWideWidth},
		{1024, sidebarNarrowWidth},
		{1020, sidebarNarrowWidth},
		{860, sidebarNarrowWidth},
	} {
		m, _ := newTestModel(t, Config{Service: &fakeService{}, PageLayout: true})
		m.SetContentSize(tc.width, 700)
		if got := m.sidebarWidth(); got != tc.want {
			t.Errorf("sidebar width at %d = %v, want %v", tc.width, got, tc.want)
		}
	}
}

// TestPageLayoutSidebarItemsFillTheRail checks the rectangular items and
// the bottom-anchored settings entry at both rail widths.
func TestPageLayoutSidebarItemsFillTheRail(t *testing.T) {
	for _, tc := range []struct {
		width int
		rail  float32
	}{
		{1440, sidebarWideWidth},
		{1020, sidebarNarrowWidth},
		{860, sidebarNarrowWidth},
	} {
		m, q := newTestModel(t, Config{Service: &fakeService{}, PageLayout: true})
		m.SetContentSize(tc.width, 700)
		height := 700
		if tc.width == 860 {
			height = 560
		}
		tt := ui.NewTester(m.View, tc.width, height)
		settle(t, m, q, tt)

		for _, label := range []string{"扫描", "历史", "设置"} {
			rect, ok := tt.Find(label)
			if !ok {
				t.Fatalf("at %d: the %q item is missing", tc.width, label)
			}
			if rect.X < 0 || rect.X+rect.W > tc.rail {
				t.Errorf("at %d: %q at %+v does not fit the %v-DIP rail", tc.width, label, rect, tc.rail)
			}
			if rect.H != 38 {
				t.Errorf("at %d: %q is %v high, want the 38-DIP rectangular item", tc.width, label, rect.H)
			}
		}
		scan, _ := tt.Find("扫描")
		settings, _ := tt.Find("设置")
		if settings.Y <= scan.Y {
			t.Errorf("at %d: settings at %v is not below scan at %v", tc.width, settings.Y, scan.Y)
		}
		if settings.Y+settings.H < float32(height)*0.8 {
			t.Errorf("at %d: settings at %+v is not pinned to the bottom", tc.width, settings)
		}
	}
}

// countColorInRect counts the pixels of a rendered frame within a small
// tolerance of c inside one region.
func countColorInRect(img *image.RGBA, c color.RGBA, r image.Rectangle) int {
	n := 0
	rect := r.Intersect(img.Bounds())
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			p := img.RGBAAt(x, y)
			dr, dg, db := int(p.R)-int(c.R), int(p.G)-int(c.G), int(p.B)-int(c.B)
			if dr < 0 {
				dr = -dr
			}
			if dg < 0 {
				dg = -dg
			}
			if db < 0 {
				db = -db
			}
			if dr+dg+db <= 12 {
				n++
			}
		}
	}
	return n
}

// TestPageLayoutSidebarActiveState checks that the current destination has
// the design's accent-soft face and the other items do not.
func TestPageLayoutSidebarActiveState(t *testing.T) {
	m, q := newTestModel(t, Config{Service: &fakeService{}, PageLayout: true})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	soft := tokensFor(false).AccentSoft
	active := color.RGBA{R: soft.R, G: soft.G, B: soft.B, A: 255}
	rectImage := func(r ui.Rect) image.Rectangle {
		return image.Rect(int(r.X), int(r.Y), int(r.X+r.W), int(r.Y+r.H))
	}
	faceIsActive := func(label string) bool {
		rect, ok := tt.Find(label)
		if !ok {
			t.Fatalf("the %q item is missing", label)
		}
		want := int(rect.W * rect.H / 2)
		return countColorInRect(tt.Image(), active, rectImage(rect)) >= want
	}

	if m.displayPage() != pageScan {
		t.Fatalf("the initial page is %v, want scan", m.displayPage())
	}
	if !faceIsActive("扫描") || faceIsActive("历史") || faceIsActive("设置") {
		t.Fatal("the scan destination alone is not active at startup")
	}

	if err := tt.Click("历史"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.displayPage() != pageHistory {
		t.Fatalf("the page is %v after clicking history, want history", m.displayPage())
	}
	if faceIsActive("扫描") || !faceIsActive("历史") || faceIsActive("设置") {
		t.Fatal("the history destination alone is not active")
	}

	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.displayPage() != pageSettings {
		t.Fatalf("the page is %v after clicking settings, want settings", m.displayPage())
	}
	if faceIsActive("扫描") || faceIsActive("历史") || !faceIsActive("设置") {
		t.Fatal("the settings destination alone is not active")
	}
}

// TestPageLayoutPagesRouteWithoutStageSwitch checks the shell's page
// model: the scan item opens prepare or the current results, there is no
// in-page stage switch, and the results header returns to prepare without
// discarding the loaded record.
func TestPageLayoutPagesRouteWithoutStageSwitch(t *testing.T) {
	svc := &fakeService{
		list: []record.RecordInfo{{
			ID: "r1", LatestRevision: 1,
			SavedAt: time.Date(2026, 10, 17, 12, 30, 0, 0, time.UTC),
		}},
		loads: map[string]*record.Recovery{"r1": testRecord()},
	}
	m, q := newTestModel(t, Config{Service: svc, PageLayout: true})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	m.page = pageScan
	tt.Frame()

	if !tt.HasText(scanHint) {
		t.Fatal("the scan page does not show the scan hint")
	}
	if tt.HasText("结果与校对") {
		t.Fatal("the scan page still shows the in-page stage switch")
	}

	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("关闭窗口时停止扫描并退出应用") {
		t.Fatal("the settings page does not show the close preference")
	}
	if err := tt.Click("历史"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("日期筛选") {
		t.Fatalf("the history page does not show its records: %q", tt.Texts())
	}
	if err := tt.Click("打开"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.displayPage() != pageResults {
		t.Fatalf("opening a record shows page %v, want results", m.displayPage())
	}
	if !tt.HasText("+ 准备新扫描") {
		t.Fatalf("the results header does not offer a new scan: %q", tt.Texts())
	}
	if err := tt.Click("+ 准备新扫描"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.displayPage() != pageScan || !tt.HasText(scanHint) {
		t.Fatalf("preparing a new scan shows page %v, want the scan page", m.displayPage())
	}
	if m.rec == nil {
		t.Fatal("preparing a new scan discarded the loaded record")
	}
	if err := tt.Click("扫描"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.displayPage() != pageResults {
		t.Fatalf("the scan item shows page %v, want the loaded results", m.displayPage())
	}
	if !tt.HasText("+ 准备新扫描") {
		t.Fatal("the scan item did not return to the results page")
	}
}

// TestPageLayoutScanActions checks the scan page's bottom action bar: it
// starts while idle, stops while a session runs, and the results header
// offers the return-to-progress action instead of a new scan.
func TestPageLayoutScanActions(t *testing.T) {
	svc := &fakeService{}
	m, q := newTestModel(t, Config{Service: svc, PageLayout: true})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	if err := tt.Click("开始扫描"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	if len(svc.starts) != 1 {
		t.Fatalf("the action bar issued %d starts, want 1", len(svc.starts))
	}

	applyAndSettle(t, m, q, tt, snap("s1", 1, true, scan.PhaseCapturing,
		scan.Counts{Captured: 2, Produced: 1}, 0, false, ""))
	if m.displayPage() != pageScan {
		t.Fatalf("a running scan shows page %v, want the scan page", m.displayPage())
	}
	if !tt.HasText("停止扫描") {
		t.Fatalf("the running scan has no bottom stop action: %q", tt.Texts())
	}
	if err := tt.Click("停止扫描"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	if svc.stops != 1 {
		t.Fatalf("the stop action issued %d stops, want 1", svc.stops)
	}

	// The results page opened during the scan returns to the progress
	// page rather than starting another scan.
	m.page = pageResults
	tt.Frame()
	if !tt.HasText("返回扫描中") {
		t.Fatalf("the results header during a scan does not offer the return action: %q", tt.Texts())
	}
	if err := tt.Click("返回扫描中"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.displayPage() != pageScan {
		t.Fatalf("returning to the running scan shows page %v, want scan", m.displayPage())
	}

	// A finished session lands on the results page with the new-scan
	// action; that action only returns to prepare.
	ended := snap("s1", 2, false, scan.PhaseEnded,
		scan.Counts{Captured: 2, Produced: 1}, 1, false, "")
	applyAndSettle(t, m, q, tt, ended)
	if m.displayPage() != pageResults {
		t.Fatalf("the ended scan shows page %v, want results", m.displayPage())
	}
	if !tt.HasText("+ 准备新扫描") {
		t.Fatalf("the results header does not offer a new scan: %q", tt.Texts())
	}
	if err := tt.Click("+ 准备新扫描"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.displayPage() != pageScan || !tt.HasText(scanHint) {
		t.Fatalf("the new-scan action shows page %v, want prepare", m.displayPage())
	}
}

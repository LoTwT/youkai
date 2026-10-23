package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
	"github.com/egoist/mygo/ui"
)

// TestScanStartShortcutStartsOnPrepare checks ⌘R on the scan prepare page:
// the status bar names it and the key issues the same start as the button.
func TestScanStartShortcutStartsOnPrepare(t *testing.T) {
	svc := &fakeService{}
	m, q := newTestModel(t, Config{Service: svc})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	if got := m.statusShortcutText(); got != "⌘R 开始扫描" {
		t.Fatalf("the prepare status bar names %q", got)
	}
	if !tt.HasText("⌘R 开始扫描") {
		t.Fatalf("the prepare status bar does not show ⌘R: %q", tt.Texts())
	}
	tt.Key(ui.Cmd, ui.KeyR)
	settle(t, m, q, tt)
	if len(svc.starts) != 1 {
		t.Fatalf("⌘R issued %d starts, want 1", len(svc.starts))
	}
}

// TestScanStartShortcutRequiresAPreparableScan checks ⌘R is neither
// registered nor shown off the prepare page, during a scan, or without a
// source.
func TestScanStartShortcutRequiresAPreparableScan(t *testing.T) {
	t.Run("busy scan", func(t *testing.T) {
		svc := &fakeService{}
		m, q := newTestModel(t, Config{Service: svc})
		tt := ui.NewTester(m.View, 1020, 700)
		settle(t, m, q, tt)
		applyAndSettle(t, m, q, tt, snap("s1", 1, true, scan.PhaseCapturing,
			scan.Counts{Captured: 2}, 0, false, ""))
		m.page = pageScan
		tt.Frame()

		if got := m.statusShortcutText(); got != "⌃⌥⌘S 停止扫描" {
			t.Fatalf("the running status bar names %q", got)
		}
		if tt.HasText("⌘R 开始扫描") {
			t.Fatalf("the running scan still names ⌘R: %q", tt.Texts())
		}
		tt.Key(ui.Cmd, ui.KeyR)
		settle(t, m, q, tt)
		if len(svc.starts) != 0 {
			t.Fatalf("⌘R started %d scans while one runs", len(svc.starts))
		}
	})

	t.Run("no source", func(t *testing.T) {
		svc := &fakeService{}
		m, q := newTestModel(t, Config{
			Service:    svc,
			ScanSource: ScanSourceInfo{Reason: "当前构建没有接入游戏扫描源"},
		})
		tt := ui.NewTester(m.View, 1020, 700)
		settle(t, m, q, tt)

		if got := m.statusShortcutText(); got != "" {
			t.Fatalf("the unprepared status bar names %q", got)
		}
		tt.Key(ui.Cmd, ui.KeyR)
		settle(t, m, q, tt)
		if len(svc.starts) != 0 {
			t.Fatalf("⌘R started %d scans without a source", len(svc.starts))
		}
	})

	t.Run("results page", func(t *testing.T) {
		svc := &fakeService{
			list: []record.RecordInfo{{
				ID: "rec", LatestRevision: 1,
				SavedAt: time.Date(2026, 10, 21, 14, 2, 0, 0, time.UTC),
			}},
			loads: map[string]*record.Recovery{"rec": responsiveResultRecord(t)},
		}
		m, q := newTestModel(t, Config{Service: svc})
		m.SetContentSize(1024, 700)
		tt := ui.NewTester(m.View, 1024, 700)
		settle(t, m, q, tt)
		m.histChoice = slotHistoryLabel(t, m, "rec")
		m.OpenHistory()
		settle(t, m, q, tt)

		tt.Key(ui.Cmd, ui.KeyR)
		settle(t, m, q, tt)
		if len(svc.starts) != 0 {
			t.Fatalf("⌘R started %d scans from the results page", len(svc.starts))
		}
	})
}

// TestEditConfirmShortcutSavesTheDraft checks ⌘↵ in the editor: it saves
// the draft as the confirmation and advances, and a failed save keeps the
// editor and the input in place.
func TestEditConfirmShortcutSavesTheDraft(t *testing.T) {
	t.Run("saves and advances", func(t *testing.T) {
		m, q, tt, svc := openReviewRecordPage(t,
			reviewRowItem(t, 1, 12),
			reviewRowItem(t, 2, 10),
		)
		if err := tt.Click("编辑"); err != nil {
			t.Fatal(err)
		}
		settle(t, m, q, tt)
		m.edit.draft.Level = "13"
		tt.Frame()
		if got := m.statusShortcutText(); got != "⌘↵ 保存并确认并下一项  ·  ⌘S 保存修改" {
			t.Fatalf("the editing status bar names %q", got)
		}
		if !tt.HasText("⌘↵ 保存并确认并下一项") || !tt.HasText("⌘S 保存修改") {
			t.Fatalf("the editing status bar does not show its keys: %q", tt.Texts())
		}

		tt.Key(ui.Cmd, ui.KeyEnter)
		settle(t, m, q, tt)
		if svc.saveCount() != 1 || !svc.saves[0].edit.Confirmed {
			t.Fatalf("⌘↵ saves = %+v, want one confirmed save", svc.saves)
		}
		if level := svc.saves[0].edit.Data.Level; level == nil || *level != 13 {
			t.Fatalf("⌘↵ saved level %v, want the draft's 13", level)
		}
		if m.edit != nil {
			t.Fatal("⌘↵ left the editor open after a successful save")
		}
		if m.selected != 1 {
			t.Fatalf("⌘↵ did not advance to the next pending item: selected %d", m.selected)
		}
		if m.rec.Rows[0].Quality != record.QualityHumanConfirmed || m.rec.Rows[0].Revision != 1 {
			t.Fatalf("the confirmed row = quality %q revision %d",
				m.rec.Rows[0].Quality, m.rec.Rows[0].Revision)
		}
	})

	t.Run("save failure stays", func(t *testing.T) {
		m, q, tt, svc := openReviewRecordPage(t,
			reviewRowItem(t, 1, 12),
			reviewRowItem(t, 2, 10),
		)
		svc.saveFn = func(string, int64, int64, app.ItemRevisionEdit) (app.ItemRevisionResult, error) {
			return app.ItemRevisionResult{}, errors.New("disk full")
		}
		if err := tt.Click("编辑"); err != nil {
			t.Fatal(err)
		}
		settle(t, m, q, tt)
		m.edit.draft.Level = "13"
		tt.Frame()

		tt.Key(ui.Cmd, ui.KeyEnter)
		settle(t, m, q, tt)
		if m.edit == nil || m.edit.draft.Level != "13" {
			t.Fatalf("a failed ⌘↵ lost the input: %+v", m.edit)
		}
		if m.selected != 0 {
			t.Fatalf("a failed ⌘↵ moved to %d", m.selected)
		}
		if m.rec.Rows[0].Quality != record.QualityNeedsReview || m.rec.Rows[0].Revision != 0 {
			t.Fatalf("a failed ⌘↵ changed the row: quality %q revision %d",
				m.rec.Rows[0].Quality, m.rec.Rows[0].Revision)
		}
		if !strings.Contains(m.reviewErr, "disk full") {
			t.Fatalf("the save failure = %q", m.reviewErr)
		}
		if !tt.HasText("保存失败：disk full") {
			t.Fatalf("the save failure does not show: %q", tt.Texts())
		}
	})
}

// TestScanStartShortcutPromptsForUnsavedEdit checks that a dirty editor
// holds the start behind the ordinary save/discard/stay prompt, and that
// saving then continues into the scan.
func TestScanStartShortcutPromptsForUnsavedEdit(t *testing.T) {
	m, q, tt, svc := openReviewRecordPage(t, reviewRowItem(t, 1, 12))
	if err := tt.Click("编辑"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	m.edit.draft.Level = "13"
	tt.Frame()

	m.page = pageScan
	tt.Frame()
	if !tt.HasText("⌘R 开始扫描") {
		t.Fatalf("the prepare page lost ⌘R with a dirty draft: %q", tt.Texts())
	}

	tt.Key(ui.Cmd, ui.KeyR)
	settle(t, m, q, tt)
	if m.navPrompt == nil || m.navPrompt.kind != navStartScan {
		t.Fatalf("⌘R did not hold a start behind the prompt: %+v", m.navPrompt)
	}
	if len(svc.starts) != 0 {
		t.Fatalf("⌘R started %d scans before the draft settled", len(svc.starts))
	}
	if !tt.HasText("当前条目有未保存的修改。") || !tt.HasText("留在当前项") {
		t.Fatalf("the unsaved-edit prompt does not show: %q", tt.Texts())
	}

	if err := tt.Click("留在当前项"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.navPrompt != nil || m.edit == nil {
		t.Fatalf("staying lost the draft or the prompt: prompt %v edit %+v", m.navPrompt, m.edit)
	}
	if len(svc.starts) != 0 {
		t.Fatalf("staying started %d scans", len(svc.starts))
	}

	tt.Key(ui.Cmd, ui.KeyR)
	settle(t, m, q, tt)
	if err := tt.Click("保存修改并继续"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if svc.saveCount() != 1 {
		t.Fatalf("save-and-start issued %d saves", svc.saveCount())
	}
	if len(svc.starts) != 1 {
		t.Fatalf("save-and-start issued %d starts", len(svc.starts))
	}
	if m.edit != nil || m.navPrompt != nil {
		t.Fatalf("save-and-start did not settle the draft: edit %+v prompt %+v", m.edit, m.navPrompt)
	}
}

// TestScanButtonAndShortcutPromptAlike checks the scan button and ⌘R use
// the same navStartScan path: both prompt for a dirty draft and neither
// starts before it is settled.
func TestScanButtonAndShortcutPromptAlike(t *testing.T) {
	m, q, tt, svc := openReviewRecordPage(t,
		reviewRowItem(t, 1, 12),
		reviewRowItem(t, 2, 10),
	)
	if err := tt.Click("编辑"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	m.edit.draft.Level = "13"
	tt.Frame()
	m.page = pageScan
	tt.Frame()

	tt.Key(ui.Cmd, ui.KeyR)
	settle(t, m, q, tt)
	if m.navPrompt == nil || m.navPrompt.kind != navStartScan {
		t.Fatalf("⌘R did not prompt for the dirty draft: %+v", m.navPrompt)
	}
	if len(svc.starts) != 0 {
		t.Fatalf("⌘R started %d scans before the draft settled", len(svc.starts))
	}
	if err := tt.Click("留在当前项"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.navPrompt != nil || len(svc.starts) != 0 {
		t.Fatalf("staying after ⌘R left prompt %+v and %d starts", m.navPrompt, len(svc.starts))
	}

	if err := tt.Click("开始扫描"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.navPrompt == nil || m.navPrompt.kind != navStartScan {
		t.Fatalf("the start button did not prompt for the dirty draft: %+v", m.navPrompt)
	}
	if len(svc.starts) != 0 {
		t.Fatalf("the start button started %d scans before the draft settled", len(svc.starts))
	}
	if !tt.HasText("当前条目有未保存的修改。") {
		t.Fatalf("the unsaved prompt does not show: %q", tt.Texts())
	}

	if err := tt.Click("保存修改并继续"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if svc.saveCount() != 1 || len(svc.starts) != 1 {
		t.Fatalf("save-and-start from the button issued %d saves and %d starts",
			svc.saveCount(), len(svc.starts))
	}
	if m.edit != nil || m.navPrompt != nil {
		t.Fatalf("save-and-start did not settle: edit %+v prompt %+v", m.edit, m.navPrompt)
	}
}

// TestEditSaveShortcut checks ⌘S in the editor: it saves modifications,
// reports an unchanged draft, and keeps the editor and its input when the
// save fails.
func TestEditSaveShortcut(t *testing.T) {
	t.Run("saves modifications", func(t *testing.T) {
		m, q, tt, svc := openReviewRecordPage(t, reviewRowItem(t, 1, 12))
		if err := tt.Click("编辑"); err != nil {
			t.Fatal(err)
		}
		settle(t, m, q, tt)
		m.edit.draft.Level = "13"
		tt.Frame()
		if got := m.statusShortcutText(); got != "⌘↵ 保存并确认并下一项  ·  ⌘S 保存修改" {
			t.Fatalf("the editing status bar names %q", got)
		}
		if !tt.HasText("⌘↵ 保存并确认并下一项") || !tt.HasText("⌘S 保存修改") {
			t.Fatalf("the editing status bar does not show its keys: %q", tt.Texts())
		}

		tt.Key(ui.Cmd, ui.KeyS)
		settle(t, m, q, tt)
		if svc.saveCount() != 1 || svc.saves[0].edit.Confirmed {
			t.Fatalf("⌘S saves = %+v, want one unconfirmed save", svc.saves)
		}
		if m.edit != nil {
			t.Fatal("a successful ⌘S left the editor open")
		}
		if m.rec.Rows[0].LevelText() != "13 级" {
			t.Fatalf("the saved row shows %q", m.rec.Rows[0].LevelText())
		}
		if got := m.statusShortcutText(); got == "⌘↵ 保存并确认并下一项  ·  ⌘S 保存修改" {
			t.Fatal("the closed editor still names its keys")
		}
	})

	t.Run("no modifications", func(t *testing.T) {
		m, q, tt, svc := openReviewRecordPage(t, reviewRowItem(t, 1, 12))
		if err := tt.Click("编辑"); err != nil {
			t.Fatal(err)
		}
		settle(t, m, q, tt)

		tt.Key(ui.Cmd, ui.KeyS)
		settle(t, m, q, tt)
		if svc.saveCount() != 0 {
			t.Fatalf("⌘S saved an unchanged draft %d times", svc.saveCount())
		}
		if m.edit == nil {
			t.Fatal("⌘S closed the unchanged editor")
		}
		if m.reviewNotice != "没有修改需要保存。" {
			t.Fatalf("the unchanged notice = %q", m.reviewNotice)
		}
		if !tt.HasText("没有修改需要保存。") {
			t.Fatalf("the unchanged notice does not show: %q", tt.Texts())
		}
	})

	t.Run("save failure stays in the editor", func(t *testing.T) {
		m, q, tt, svc := openReviewRecordPage(t, reviewRowItem(t, 1, 12))
		svc.saveFn = func(string, int64, int64, app.ItemRevisionEdit) (app.ItemRevisionResult, error) {
			return app.ItemRevisionResult{}, errors.New("disk full")
		}
		if err := tt.Click("编辑"); err != nil {
			t.Fatal(err)
		}
		settle(t, m, q, tt)
		m.edit.draft.Level = "13"
		tt.Frame()

		tt.Key(ui.Cmd, ui.KeyS)
		settle(t, m, q, tt)
		if m.edit == nil || m.edit.draft.Level != "13" {
			t.Fatalf("a failed ⌘S lost the input: %+v", m.edit)
		}
		if !strings.Contains(m.reviewErr, "disk full") {
			t.Fatalf("the save failure = %q", m.reviewErr)
		}
		if m.rec.Rows[0].LevelText() != "12 级" {
			t.Fatalf("a failed ⌘S changed the row: %q", m.rec.Rows[0].LevelText())
		}
		if !tt.HasText("保存失败：disk full") {
			t.Fatalf("the save failure does not show: %q", tt.Texts())
		}
	})
}

// TestEscapeClosesTheRowMenu checks the first Escape step: MyGo's popover
// takes the key before the shell and closes the menu.
func TestEscapeClosesTheRowMenu(t *testing.T) {
	svc := &fakeService{history: historyTableRecords(3)}
	m, q := newTestModel(t, Config{Service: svc, DirectExportSaver: &fakeDirectExportSaver{}})
	m.SetContentSize(1020, 700)
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	m.openHistoryMenu("r01")
	tt.Frame()
	if !m.historyMenuOpen {
		t.Fatal("the row menu did not open")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if m.historyMenuOpen {
		t.Fatal("Escape did not close the row menu")
	}
}

// TestEscapeClosesTheSelectPopup checks that an open Select keeps Escape
// with MyGo: the popup closes and the shell does not move the focus to the
// table behind it.
func TestEscapeClosesTheSelectPopup(t *testing.T) {
	_, _, tt := openResponsiveResults(t, responsiveResultRecord(t), 1024, 700)
	// The chosen row's slot is in the detail, so another slot's exact
	// option label exists only while the popup is open.
	if _, ok := tt.Find("2 号位"); ok {
		t.Fatalf("the slot option is visible before the popup opens: %q", tt.Texts())
	}
	if err := tt.Click("全部槽位"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("2 号位"); !ok {
		t.Fatalf("the slot popup did not open: %q", tt.Texts())
	}

	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if _, ok := tt.Find("2 号位"); ok {
		t.Fatal("Escape did not close the select popup")
	}
	if tt.Focused("河豚电音") {
		t.Fatal("the shell took Escape from the select popup")
	}
}

// TestEscapeCancelsTheUnsavedPrompt checks the second Escape step: with a
// held navigation it stays on the current item and keeps the draft.
func TestEscapeCancelsTheUnsavedPrompt(t *testing.T) {
	m, q, tt, _ := openReviewRecordPage(t, reviewRowItem(t, 1, 12))
	if err := tt.Click("编辑"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	m.edit.draft.Level = "13"
	tt.Frame()

	m.OpenHistory()
	settle(t, m, q, tt)
	if m.navPrompt == nil {
		t.Fatal("the dirty switch did not prompt")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if m.navPrompt != nil {
		t.Fatal("Escape did not cancel the unsaved prompt")
	}
	if m.edit == nil || m.edit.draft.Level != "13" {
		t.Fatalf("Escape lost the draft: %+v", m.edit)
	}
}

// TestEscapeReturnsTheFocusToTheTable checks the last Escape step on both
// table destinations: from a page control the key gives the table the
// keyboard again.
func TestEscapeReturnsTheFocusToTheTable(t *testing.T) {
	t.Run("history", func(t *testing.T) {
		svc := &fakeService{history: historyTableRecords(3)}
		m, q := newTestModel(t, Config{
			Service: svc, DirectExportSaver: &fakeDirectExportSaver{},
			OpenDataDirectory: func() error { return nil },
		})
		m.SetContentSize(1020, 700)
		tt := ui.NewTester(m.View, 1020, 700)
		settle(t, m, q, tt)

		if err := tt.Click("打开数据目录"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		tt.Key(0, ui.KeyEscape)
		tt.Frame()
		tt.Frame()
		if !tt.Focused("10-21 14:02") {
			t.Fatalf("Escape did not give the history table the focus: %q", tt.Texts())
		}
	})

	t.Run("results", func(t *testing.T) {
		_, _, tt := openResponsiveResults(t, responsiveResultRecord(t), 1024, 700)
		if err := tt.Click("显示证据"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		tt.Key(0, ui.KeyEscape)
		tt.Frame()
		tt.Frame()
		if !tt.Focused("河豚电音") {
			t.Fatalf("Escape did not give the results table the focus: %q", tt.Texts())
		}
	})
}

// TestEscapeStaysInATextField checks that Escape in a text input is not
// pulled away to the table. MyGo's text inputs leave the key unhandled, so
// the shell has to keep its hands off while one holds the keyboard.
func TestEscapeStaysInATextField(t *testing.T) {
	svc := &fakeService{history: historyTableRecords(3)}
	m, q := newTestModel(t, Config{Service: svc, DirectExportSaver: &fakeDirectExportSaver{}})
	m.SetContentSize(1020, 700)
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	// Tab until the date input takes what is typed. Typing is the probe:
	// every other focus stop on this page ignores text.
	focused := false
	for i := 0; i < 12; i++ {
		tt.Key(0, ui.KeyTab)
		tt.Frame()
		tt.Type("2")
		tt.Frame()
		if m.historyFilter == "2" {
			focused = true
			break
		}
	}
	if !focused {
		t.Fatal("Tab never reached the history date input")
	}
	m.historyFilter = ""
	tt.Frame()

	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	tt.Frame()
	if tt.Focused("10-21 14:02") {
		t.Fatal("Escape pulled the focus out of the text field")
	}
	tt.Type("3")
	tt.Frame()
	if m.historyFilter != "3" {
		t.Fatalf("Escape moved the keyboard out of the input: filter %q", m.historyFilter)
	}
}

// TestEditNextShortcutPromptsForUnsavedEdit checks that ⌘J in the editor
// runs through the shared unsaved prompt: it stays on 留在当前项 and moves
// only after the draft is saved or discarded.
func TestEditNextShortcutPromptsForUnsavedEdit(t *testing.T) {
	setup := func(t *testing.T) (*Model, *queueUpdater, *ui.Tester, *fakeService) {
		t.Helper()
		m, q, tt, svc := openReviewRecordPage(t,
			reviewRowItem(t, 1, 12),
			reviewRowItem(t, 2, 10),
		)
		if err := tt.Click("编辑"); err != nil {
			t.Fatal(err)
		}
		settle(t, m, q, tt)
		m.edit.draft.Level = "13"
		tt.Frame()
		return m, q, tt, svc
	}

	t.Run("stays", func(t *testing.T) {
		m, q, tt, svc := setup(t)
		tt.Key(ui.Cmd, ui.KeyJ)
		settle(t, m, q, tt)
		if m.navPrompt == nil {
			t.Fatal("⌘J bypassed the unsaved prompt")
		}
		if m.selected != 0 || svc.saveCount() != 0 {
			t.Fatalf("⌘J moved before the draft settled: selected %d saves %d", m.selected, svc.saveCount())
		}
		if !tt.HasText("当前条目有未保存的修改。") || !tt.HasText("留在当前项") {
			t.Fatalf("the unsaved prompt does not show: %q", tt.Texts())
		}
		if err := tt.Click("留在当前项"); err != nil {
			t.Fatal(err)
		}
		settle(t, m, q, tt)
		if m.navPrompt != nil || m.selected != 0 || m.edit == nil {
			t.Fatalf("staying moved or lost the draft: prompt %v selected %d edit %+v",
				m.navPrompt, m.selected, m.edit)
		}
	})

	t.Run("saves and moves", func(t *testing.T) {
		m, q, tt, svc := setup(t)
		tt.Key(ui.Cmd, ui.KeyJ)
		settle(t, m, q, tt)
		if m.navPrompt == nil {
			t.Fatal("⌘J bypassed the unsaved prompt")
		}
		if err := tt.Click("保存修改并继续"); err != nil {
			t.Fatal(err)
		}
		settle(t, m, q, tt)
		if svc.saveCount() != 1 || svc.saves[0].edit.Confirmed {
			t.Fatalf("⌘J saves = %+v, want one unconfirmed save", svc.saves)
		}
		if m.selected != 1 || m.edit != nil {
			t.Fatalf("save-and-continue did not move: selected %d edit %+v", m.selected, m.edit)
		}
	})

	t.Run("discards and moves", func(t *testing.T) {
		m, q, tt, svc := setup(t)
		tt.Key(ui.Cmd, ui.KeyJ)
		settle(t, m, q, tt)
		if m.navPrompt == nil {
			t.Fatal("⌘J bypassed the unsaved prompt")
		}
		if err := tt.Click("放弃修改"); err != nil {
			t.Fatal(err)
		}
		settle(t, m, q, tt)
		if svc.saveCount() != 0 {
			t.Fatalf("discarding saved %d revisions", svc.saveCount())
		}
		if m.selected != 1 || m.edit != nil || m.navPrompt != nil {
			t.Fatalf("discard-and-continue did not move: selected %d edit %+v prompt %+v",
				m.selected, m.edit, m.navPrompt)
		}
	})

	t.Run("form change", func(t *testing.T) {
		m, q, tt, _ := openReviewRecordPage(t,
			reviewRowItem(t, 1, 12),
			reviewRowItem(t, 2, 10),
		)
		if err := tt.Click("编辑"); err != nil {
			t.Fatal(err)
		}
		settle(t, m, q, tt)

		// Make the draft dirty through the form, not a test-only field
		// assignment: the picker writes the bound draft value.
		if err := tt.Click("31100 河豚电音"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if err := tt.Click("31200 震星迪斯科"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if m.edit == nil || m.edit.draft.SetID == m.edit.original.SetID {
			t.Fatalf("the form change did not dirty the draft: %+v", m.edit)
		}

		tt.Key(ui.Cmd, ui.KeyJ)
		settle(t, m, q, tt)
		if m.navPrompt == nil {
			t.Fatal("⌘J dropped a draft changed through the form")
		}
		if m.selected != 0 {
			t.Fatalf("⌘J moved before the form draft settled: selected %d", m.selected)
		}
	})
}

// TestStatusBarNamesOnlyUsableShortcuts checks the destination-specific
// key line: prepare names ⌘R, an editor names its save-and-confirm and save
// keys, results name their list keys, history names Return, and settings
// names none.
func TestStatusBarNamesOnlyUsableShortcuts(t *testing.T) {
	m, q := newTestModel(t, Config{Service: &fakeService{}})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	if got := m.statusShortcutText(); got != "⌘R 开始扫描" {
		t.Fatalf("the prepare status = %q", got)
	}

	m2, q2, tt2 := openResponsiveResults(t, responsiveResultRecord(t), 1024, 700)
	if got := m2.statusShortcutText(); got != "⌘↵ 确认并下一项  ·  ⌘J 下一个待校对" {
		t.Fatalf("the results status = %q", got)
	}
	if tt2.HasText("⌘S 保存修改") {
		t.Fatalf("the results list names the editor key: %q", tt2.Texts())
	}
	if err := tt2.Click("编辑"); err != nil {
		t.Fatal(err)
	}
	settle(t, m2, q2, tt2)
	if got := m2.statusShortcutText(); got != "⌘↵ 保存并确认并下一项  ·  ⌘S 保存修改" {
		t.Fatalf("the editing status = %q", got)
	}
	if tt2.HasText("⌘J 下一个待校对") || tt2.HasText("⌘↵ 确认并下一项") {
		t.Fatalf("the editing status still names review keys: %q", tt2.Texts())
	}

	svc := &fakeService{history: historyTableRecords(3)}
	m3, q3 := newTestModel(t, Config{Service: svc, DirectExportSaver: &fakeDirectExportSaver{}})
	m3.SetContentSize(1020, 700)
	tt3 := ui.NewTester(m3.View, 1020, 700)
	settle(t, m3, q3, tt3)
	if got := m3.statusShortcutText(); got != "↵ 打开记录" {
		t.Fatalf("the history status = %q", got)
	}
	openSettings(t, m3, q3, tt3)
	if got := m3.statusShortcutText(); got != "" {
		t.Fatalf("the settings status names %q", got)
	}
}

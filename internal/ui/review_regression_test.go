package ui

import (
	"testing"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/game/zzz"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/egoist/mygo/ui"
)

func TestReviewCorrectedDraftConfirmsAndAdvances(t *testing.T) {
	for _, tc := range []struct {
		name  string
		level *int
	}{
		{name: "invalid", level: intPtr(16)},
		{name: "unread"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first := withStructured(t, reviewRowItem(t, 1, 12), func(s *zzz.StructuredDisc) {
				s.Data.Level = tc.level
				check := app.CheckDiscData(s.Data)
				s.Issues = check.Issues
				s.Validation = check.Validation
			})
			m, q, tt, svc := openReviewRecordPage(t, first, reviewRowItem(t, 2, 10))
			if reason := m.reviewConfirmBlockedReason(m.selectedRow()); reason == "" {
				t.Fatal("the unresolved draft should block confirmation")
			}
			m.edit.draft.Level = "13"
			tt.Frame()
			if reason := m.reviewConfirmBlockedReason(m.selectedRow()); reason != "" {
				t.Fatalf("the corrected draft still blocks confirmation: %s", reason)
			}
			if err := tt.Click("确认并下一项"); err != nil {
				t.Fatal(err)
			}
			settle(t, m, q, tt)
			if svc.saveCount() != 1 || !svc.saves[0].edit.Confirmed ||
				m.rec.Rows[0].LevelText() != "13 级" || m.selectedSeq() != 2 {
				t.Fatalf("corrected confirmation: saves %+v, level %q, selected %d",
					svc.saves, m.rec.Rows[0].LevelText(), m.selectedSeq())
			}
		})
	}
}

func TestReviewEquipmentSelectionClearsAgent(t *testing.T) {
	for _, tc := range []struct {
		name  string
		key   ui.Key
		state string
	}{
		{name: "unequipped", key: ui.KeyEnd, state: zzz.EquippedStateUnequipped},
		{name: "unread", key: ui.KeyHome},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _, _, _ := openReviewRecordPage(t, reviewRowItem(t, 1, 12))
			m.edit.draft.EquippedState = describeEquipped(zzz.EquippedStateEquipped)
			m.edit.draft.AgentID = describeAgent(1311)
			tt := ui.NewTester(func(c *ui.Context) {
				m.reviewFieldControl(c, c.Theme(), m.selectedRow(), FormFieldEquipped)
			}, 560, 240)
			tt.Frame()
			if err := tt.Click("装备状态"); err != nil {
				t.Fatal(err)
			}
			tt.Key(0, tc.key)
			tt.Key(0, ui.KeyEnter)
			if got := optionValue(m.edit.draft.EquippedState); got != tc.state {
				t.Fatalf("equipment state = %q, want %q", got, tc.state)
			}
			m.edit.refresh()
			if got := optionValue(m.edit.draft.AgentID); got != "" || !m.edit.canConfirm() {
				t.Fatalf("equipment state %q left agent %q and errors %+v", tc.state, got, m.edit.fieldErrors)
			}
		})
	}
}

func TestReviewHistoryUsesSavedRevisionCounts(t *testing.T) {
	m, q, tt, svc := openReviewRecordPage(t, reviewRowItem(t, 1, 12))
	svc.mu.Lock()
	svc.historyFn = func() ([]app.HistoryRecord, error) {
		svc.mu.Lock()
		defer svc.mu.Unlock()
		return []app.HistoryRecord{{
			ID: "r1", CapturedCount: 1,
			Counts: app.ClassifyItems(svc.loads["r1"].Checkpoint.Items),
		}}, nil
	}
	svc.mu.Unlock()
	m.loadHistory()
	settle(t, m, q, tt)
	if len(m.history) != 1 || m.history[0].Counts.PendingReview != 1 {
		t.Fatalf("the cached history did not start with one pending item: %+v", m.history)
	}
	if err := tt.Click("确认并下一项"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if svc.saveCount() != 1 || m.rec.Rows[0].Quality != record.QualityHumanConfirmed {
		t.Fatalf("confirmation did not save: %+v", svc.saves)
	}
	if err := tt.Click("‹ 全部记录"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.displayPage() != pageHistory || m.history[0].Counts.PendingReview != 0 || m.history[0].Counts.Exportable() != 1 {
		t.Fatalf("history still shows the old revision: page %v, counts %+v", m.displayPage(), m.history[0].Counts)
	}
}

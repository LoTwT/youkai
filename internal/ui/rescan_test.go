package ui

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
)

// rescanRecord is a saved Windows local record with a degraded position 2
// and an unreached position 3. Its declared range is the full warehouse, so
// all three missing positions are eligible.
func rescanRecord() *record.Recovery {
	rec := recovery()
	rec.Checkpoint.Session.Scope = []byte(`{"kind":"zzzInventory","mode":"traverse"}`)
	rec.Checkpoint.Session.Coverage = record.Coverage{Status: record.CoveragePartial}
	rec.Checkpoint.Session.PositionLedger = &record.PositionLedger{
		Sort: record.SortVerificationUnverified,
		Positions: []record.PositionConclusion{
			{Position: 1, Complete: true},
			{Position: 2, Stage: "filter", ReasonCode: "navigationMismatch"},
			{Position: 3},
		},
	}
	return rec
}

func rescanModel(rec *record.Recovery) *Model {
	m := summaryModel(&RecordView{ID: rec.Checkpoint.Session.SessionID, Shell: rec.Checkpoint.Session}, app.Snapshot{})
	m.rec.ID = "rec"
	return m
}

func TestRescanCandidatesGate(t *testing.T) {
	cases := []struct {
		name  string
		set   func(*record.Recovery, *Model)
		want  []int
		known bool
	}{
		{
			name:  "full record offers every incomplete planned position",
			want:  []int{2, 3, 4, 5, 6},
			known: true,
		},
		{
			name: "non windows record",
			set: func(rec *record.Recovery, m *Model) {
				rec.Checkpoint.Session.Client = record.ClientMacOSCloud
			},
		},
		{
			name: "not a drive disc record",
			set: func(rec *record.Recovery, m *Model) {
				rec.Checkpoint.Session.Entity = "agent"
			},
		},
		{
			name: "running record",
			set: func(_ *record.Recovery, m *Model) {
				m.rec.Live = true
				m.snap.Busy = true
			},
		},
		{
			name: "unsaved record",
			set: func(_ *record.Recovery, m *Model) {
				m.rec.Live = true
				m.snap.SaveError = "disk full"
			},
		},
		{
			name: "unreadable record",
			set: func(_ *record.Recovery, m *Model) {
				m.recErr = "broken"
			},
		},
		{
			name: "no ledger",
			set: func(rec *record.Recovery, _ *Model) {
				rec.Checkpoint.Session.PositionLedger = nil
				rec.Checkpoint.Session.ObservedScope = nil
			},
		},
		{
			name: "all planned positions complete",
			set: func(rec *record.Recovery, _ *Model) {
				rec.Checkpoint.Session.PositionLedger = &record.PositionLedger{
					Sort: record.SortVerificationVerified, Positions: summaryCompletePositions(),
				}
			},
			known: true,
		},
		{
			name: "damaged partial scope is not a full record",
			set: func(rec *record.Recovery, _ *Model) {
				rec.Checkpoint.Session.Scope = []byte(`{"positions":[4],"rescanOf":""}`)
			},
		},
		{
			name: "scoped record offers only its planned incomplete position",
			set: func(rec *record.Recovery, _ *Model) {
				rec.Checkpoint.Session.Scope = []byte(`{"kind":"zzzInventory","mode":"traverse","positions":[4],"rescanOf":"src"}`)
				rec.Checkpoint.Session.PositionLedger = &record.PositionLedger{
					Sort: record.SortVerificationUnverified,
					Positions: []record.PositionConclusion{
						{Position: 1, Complete: true},
						{Position: 4, Stage: "scroll", ReasonCode: "positionLost"},
					},
				}
			},
			want:  []int{4},
			known: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := rescanRecord()
			m := rescanModel(rec)
			if tc.set != nil {
				tc.set(rec, m)
				m.rec.Shell = rec.Checkpoint.Session
			}
			candidates, known := m.rescanCandidates()
			if known != tc.known {
				t.Fatalf("known = %v, want %v", known, tc.known)
			}
			var got []int
			for _, candidate := range candidates {
				got = append(got, candidate.Position)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("candidates = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRescanDialogArmsExactlyTheSelectedRange(t *testing.T) {
	rec := rescanRecord()
	m := rescanModel(rec)
	action, ok := m.rescanIncompleteAction()
	if !ok {
		t.Fatal("the eligible record did not offer the partial rescan")
	}
	if !strings.Contains(action.Label, "2, 3, 4, 5, 6") && !strings.Contains(action.Label, "2–6") {
		t.Fatalf("action label = %q, want the offered positions", action.Label)
	}
	m.openRescanDialog()
	if !m.rescanDialog.open {
		t.Fatal("the dialog did not open")
	}
	if got := m.rescanDialogPositions(); !reflect.DeepEqual(got, []int{2, 3, 4, 5, 6}) {
		t.Fatalf("initial selection = %v, want every offered position", got)
	}
	m.rescanDialog.chosen[3] = false
	m.rescanDialog.chosen[5] = false
	m.confirmRescanDialog()
	if m.rescanDialog.open {
		t.Fatal("confirming left the dialog open")
	}
	if m.displayPage() != pageScan {
		t.Fatalf("confirming landed on %v, want the preparation page", m.displayPage())
	}
	if m.pendingRescan == nil || !reflect.DeepEqual(m.pendingRescan.positions, []int{2, 4, 6}) ||
		m.pendingRescan.sourceID != "rec" {
		t.Fatalf("pending range = %+v, want 2,4,6 from rec", m.pendingRescan)
	}
	if m.pendingRescan.sourceLabel == "" {
		t.Fatal("the range lost its source label")
	}
	req := m.pendingRescan.request()
	if !reflect.DeepEqual(req.Positions, []int{2, 4, 6}) || req.SourceRecordID != "rec" {
		t.Fatalf("armed request = %+v", req)
	}
}

func TestRescanRangeDoesNotSurviveContextChanges(t *testing.T) {
	rec := rescanRecord()
	m := rescanModel(rec)
	m.armRescan(app.RescanRequest{SourceRecordID: "rec", Positions: []int{4}}, "rec")
	m.navigatePage(pageHistory)
	if m.pendingRescan != nil {
		t.Fatal("leaving the preparation page kept the range")
	}

	m.armRescan(app.RescanRequest{SourceRecordID: "rec", Positions: []int{4}}, "rec")
	m.performSummaryAction(nil, summaryActionRescan)
	if m.pendingRescan != nil {
		t.Fatal("the ordinary rescan entry kept a partial range")
	}
}

func TestRescanIntentSurvivesRetryableStartFailures(t *testing.T) {
	svc := &fakeService{startFn: func(req app.StartRequest) (app.StartOutcome, error) {
		return app.StartOutcome{}, errors.New("temporary refusal")
	}}
	m, q := newTestModel(t, Config{Service: svc})
	drainAll(t, m, q)
	m.armRescan(app.RescanRequest{SourceRecordID: "rec", Positions: []int{2, 4}}, "rec")
	m.Start()
	m.Start() // the second click is still in flight and must not start twice
	drainAll(t, m, q)
	if len(svc.starts) != 1 {
		t.Fatalf("starts = %+v", svc.starts)
	}
	if svc.starts[0].Rescan == nil || !reflect.DeepEqual(svc.starts[0].Rescan.Positions, []int{2, 4}) {
		t.Fatalf("start request = %+v, want the armed range", svc.starts[0])
	}
	if m.pendingRescan == nil || m.pendingRescan.blocked {
		t.Fatalf("a retryable refusal dropped or blocked the range: %+v", m.pendingRescan)
	}
	m.Start()
	drainAll(t, m, q)
	if len(svc.starts) != 2 || svc.starts[1].Rescan == nil ||
		!reflect.DeepEqual(svc.starts[1].Rescan.Positions, []int{2, 4}) {
		t.Fatalf("the retry did not keep the range: %+v", svc.starts)
	}
}

func TestRescanIntentKeepsBusyAsNotStarted(t *testing.T) {
	svc := &fakeService{startFn: func(req app.StartRequest) (app.StartOutcome, error) {
		return app.StartOutcome{SessionID: scan.ID("other"), Busy: true}, nil
	}}
	m, q := newTestModel(t, Config{Service: svc})
	drainAll(t, m, q)
	m.armRescan(app.RescanRequest{SourceRecordID: "rec", Positions: []int{4}}, "rec")
	m.Start()
	drainAll(t, m, q)
	if m.pendingRescan == nil || m.pendingRescan.blocked {
		t.Fatalf("Busy consumed or blocked the range: %+v", m.pendingRescan)
	}
	m.Start()
	drainAll(t, m, q)
	if len(svc.starts) != 2 || svc.starts[1].Rescan == nil {
		t.Fatalf("the start after Busy did not retry the range: %+v", svc.starts)
	}
}

func TestRescanSourceRefusalRequiresAnExplicitChoice(t *testing.T) {
	svc := &fakeService{startFn: func(req app.StartRequest) (app.StartOutcome, error) {
		return app.StartOutcome{}, app.ErrRescanSource
	}}
	m, q := newTestModel(t, Config{Service: svc})
	drainAll(t, m, q)
	m.armRescan(app.RescanRequest{SourceRecordID: "rec", Positions: []int{4}}, "rec")
	m.Start()
	drainAll(t, m, q)
	if !m.rescanStartBlocked() {
		t.Fatalf("a source refusal did not block the range: %+v", m.pendingRescan)
	}
	if m.canStart() {
		t.Fatal("the start button stayed usable after a blocked rescan")
	}
	if !strings.Contains(m.scanStartBlockedReason(), "重新选择") {
		t.Fatalf("blocked reason = %q", m.scanStartBlockedReason())
	}
	m.Start()
	drainAll(t, m, q)
	if len(svc.starts) != 1 {
		t.Fatalf("a blocked range still started: %+v", svc.starts)
	}
	m.switchToFullScan()
	if m.pendingRescan != nil || !m.canStart() {
		t.Fatalf("explicit switch left pending=%+v canStart=%v", m.pendingRescan, m.canStart())
	}
	m.Start()
	drainAll(t, m, q)
	if len(svc.starts) != 2 || svc.starts[1].Rescan != nil {
		t.Fatalf("the explicit switch did not start an ordinary scan: %+v", svc.starts)
	}
}

func TestRescanPreInputFailureKeepsTheRangeAndInputOpenConsumesIt(t *testing.T) {
	sessions := 0
	svc := &fakeService{startFn: func(req app.StartRequest) (app.StartOutcome, error) {
		if req.Rescan == nil {
			return app.StartOutcome{SessionID: scan.ID("ordinary")}, nil
		}
		sessions++
		return app.StartOutcome{SessionID: scan.ID(fmt.Sprintf("rescan-%d", sessions))}, nil
	}}
	m, q := newTestModel(t, Config{Service: svc})
	drainAll(t, m, q)
	m.armRescan(app.RescanRequest{SourceRecordID: "rec", Positions: []int{4}}, "rec")
	m.Start()
	drainAll(t, m, q)
	if m.rescanAttempt == nil || m.rescanAttempt.sessionID != "rescan-1" {
		t.Fatalf("accepted rescan was not tracked: %+v", m.rescanAttempt)
	}
	m.Apply(rescanEndedBeforeInput("rescan-1", 2))
	drainAll(t, m, q)
	if m.pendingRescan == nil || m.pendingRescan.retryNotice == "" {
		t.Fatalf("pre-input failure did not keep a retryable range: %+v", m.pendingRescan)
	}
	m.Start()
	drainAll(t, m, q)
	if len(svc.starts) != 2 || svc.starts[1].Rescan == nil {
		t.Fatalf("the retry after a pre-input failure lost the range: %+v", svc.starts)
	}
	m.Apply(snap("rescan-2", 3, true, scan.PhaseCapturing, scan.Counts{}, 0, false, ""))
	if m.pendingRescan != nil || m.rescanAttempt != nil {
		t.Fatalf("opened input did not consume the range: pending=%+v attempt=%+v",
			m.pendingRescan, m.rescanAttempt)
	}
	m.Apply(snap("rescan-2", 4, false, scan.PhaseEnded, scan.Counts{}, 0, false, ""))
	drainAll(t, m, q)
	m.Start()
	drainAll(t, m, q)
	if len(svc.starts) != 3 || svc.starts[2].Rescan != nil {
		t.Fatalf("a later ordinary start inherited the range: %+v", svc.starts)
	}
}

func rescanEndedBeforeInput(session string, rev uint64) app.Snapshot {
	return app.Snapshot{
		SessionID: scan.ID(session),
		Revision:  rev,
		Busy:      false,
		Scan: scan.Snapshot{
			SessionID:   scan.ID(session),
			Revision:    rev,
			Phase:       scan.PhaseEnded,
			InputOpened: false,
			End:         scan.EndFailed,
			Trigger: &scan.Trigger{
				Kind:  scan.TriggerFault,
				Code:  "pageUnverified",
				Phase: scan.PhasePreparing,
			},
		},
	}
}

func TestRescanLateStartResponseCannotConsumeANewInstance(t *testing.T) {
	for _, tc := range []struct {
		name    string
		respond func() (app.StartOutcome, error)
	}{
		{
			name: "old accepted response",
			respond: func() (app.StartOutcome, error) {
				return app.StartOutcome{SessionID: scan.ID("old-rescan")}, nil
			},
		},
		{
			name: "old scope refusal",
			respond: func() (app.StartOutcome, error) {
				return app.StartOutcome{}, app.ErrRescanScope
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			started := make(chan struct{})
			release := make(chan struct{})
			var calls int
			svc := &fakeService{startFn: func(req app.StartRequest) (app.StartOutcome, error) {
				calls++
				if calls == 1 {
					close(started)
					<-release
					return tc.respond()
				}
				return app.StartOutcome{SessionID: scan.ID("new-rescan")}, nil
			}}
			m, q := newTestModel(t, Config{Service: svc})
			drainAll(t, m, q)
			m.armRescan(app.RescanRequest{SourceRecordID: "rec", Positions: []int{4}}, "rec")
			m.Start()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("the first start did not reach the service")
			}

			// The user returned and prepared the same source and positions
			// again; equality must not make the old response belong to it.
			m.armRescan(app.RescanRequest{SourceRecordID: "rec", Positions: []int{4}}, "rec")
			newIntent := m.pendingRescan
			close(release)
			drainAll(t, m, q)
			m.Apply(rescanEndedBeforeInput("old-rescan", 2))
			drainAll(t, m, q)

			if m.pendingRescan != newIntent || m.pendingRescan.blocked ||
				m.pendingRescan.retryNotice != "" {
				t.Fatalf("old response changed the replacement: pending=%+v", m.pendingRescan)
			}
			if m.startErr != "" {
				t.Fatalf("old response wrote a start error: %q", m.startErr)
			}
			m.Start()
			drainAll(t, m, q)
			if len(svc.starts) != 2 || svc.starts[1].Rescan == nil ||
				!reflect.DeepEqual(svc.starts[1].Rescan.Positions, []int{4}) {
				t.Fatalf("replacement did not start its own range: %+v", svc.starts)
			}
		})
	}
}

func TestRescanAcceptedResponseWaitsForItsSessionSnapshot(t *testing.T) {
	svc := &fakeService{startFn: func(req app.StartRequest) (app.StartOutcome, error) {
		return app.StartOutcome{SessionID: scan.ID("rescan-1")}, nil
	}}
	m, q := newTestModel(t, Config{Service: svc})
	drainAll(t, m, q)
	m.armRescan(app.RescanRequest{SourceRecordID: "rec", Positions: []int{4}}, "rec")
	m.Start()
	drainAll(t, m, q)
	if !m.rescanAwaitingSnapshot() || m.canStart() {
		t.Fatalf("accepted start was not gated: attempt=%+v canStart=%v", m.rescanAttempt, m.canStart())
	}
	if reason := m.scanStartBlockedReason(); !strings.Contains(reason, "等待扫描会话") {
		t.Fatalf("accepted gate reason = %q", reason)
	}
	m.Start()
	drainAll(t, m, q)
	if len(svc.starts) != 1 {
		t.Fatalf("accepted start produced another request: %+v", svc.starts)
	}

	// The matching preparing snapshot releases the gate but the busy slot
	// still owns the start button.
	preparing := snap("rescan-1", 2, true, scan.PhasePreparing, scan.Counts{}, 0, false, "")
	preparing.Scan.InputOpened = false
	m.Apply(preparing)
	drainAll(t, m, q)
	if m.rescanAttempt == nil || !m.rescanAttempt.snapshotSeen || m.pendingRescan == nil {
		t.Fatalf("preparing snapshot did not bind the accepted attempt: %+v", m.rescanAttempt)
	}
	if m.canStart() {
		t.Fatal("the busy accepted session left Start enabled")
	}

	m.Apply(rescanEndedBeforeInput("rescan-1", 3))
	drainAll(t, m, q)
	if m.pendingRescan == nil || m.pendingRescan.retryNotice == "" || m.rescanAttempt != nil {
		t.Fatalf("pre-input end did not leave a retryable range: pending=%+v attempt=%+v",
			m.pendingRescan, m.rescanAttempt)
	}
	if !m.canStart() {
		t.Fatalf("pre-input end kept the start gated: %q", m.scanStartBlockedReason())
	}
	m.Start()
	drainAll(t, m, q)
	if len(svc.starts) != 2 || svc.starts[1].Rescan == nil {
		t.Fatalf("retry after pre-input end lost the range: %+v", svc.starts)
	}
}

func TestRescanInputOpenBeforeSnapshotConsumesOneAttempt(t *testing.T) {
	svc := &fakeService{startFn: func(req app.StartRequest) (app.StartOutcome, error) {
		return app.StartOutcome{SessionID: scan.ID("rescan-1")}, nil
	}}
	m, q := newTestModel(t, Config{Service: svc})
	drainAll(t, m, q)
	m.armRescan(app.RescanRequest{SourceRecordID: "rec", Positions: []int{4}}, "rec")
	m.Start()
	drainAll(t, m, q)
	if !m.rescanAwaitingSnapshot() {
		t.Fatal("accepted start was not gated before its snapshot")
	}
	// Input opening and ending are delivered without an intervening
	// preparing snapshot; the single start still consumes the intent.
	m.Apply(snap("rescan-1", 2, false, scan.PhaseEnded, scan.Counts{}, 0, false, ""))
	drainAll(t, m, q)
	if m.pendingRescan != nil || m.rescanAttempt != nil {
		t.Fatalf("input-open terminal did not consume the intent: pending=%+v attempt=%+v",
			m.pendingRescan, m.rescanAttempt)
	}
	m.Start()
	drainAll(t, m, q)
	if len(svc.starts) != 2 || svc.starts[1].Rescan != nil {
		t.Fatalf("a later start inherited the rescan range: %+v", svc.starts)
	}
}

func TestRescanSaveFailureNotificationKeepsThePreparedRange(t *testing.T) {
	svc := &fakeService{}
	m, q := newTestModel(t, Config{Service: svc})
	drainAll(t, m, q)
	m.page = pageScan
	m.armRescan(app.RescanRequest{SourceRecordID: "rec", Positions: []int{4}}, "rec")
	m.Apply(openCapturing("rec", 1, 0))
	failed := endedSession("rec", 2, 0, 0)
	failed.Busy = true
	failed.FinalSaveFailures = 1
	failed.SaveError = "disk full"
	m.Apply(failed)
	drainAll(t, m, q)
	m.OpenNotification("rec")
	drainAll(t, m, q)
	if m.pendingRescan == nil {
		t.Fatal("the in-place save-failure notification cleared the range")
	}
	m.RetrySave()
	drainAll(t, m, q)
	if svc.retries != 1 {
		t.Fatalf("retry save calls = %d, want 1", svc.retries)
	}
	m.Apply(snap("rec", 3, false, scan.PhaseEnded, scan.Counts{}, 0, false, ""))
	drainAll(t, m, q)
	m.Start()
	drainAll(t, m, q)
	if len(svc.starts) != 1 || svc.starts[0].Rescan == nil ||
		!reflect.DeepEqual(svc.starts[0].Rescan.Positions, []int{4}) {
		t.Fatalf("start after save retry lost the range: %+v", svc.starts)
	}
}

func TestRescanStartUsesEachConfirmedTarget(t *testing.T) {
	targetA := app.TargetCandidate{Handle: 1, PID: 10, ExecutablePath: `C:\game.exe`}
	targetB := app.TargetCandidate{Handle: 2, PID: 20, ExecutablePath: `C:\game.exe`}
	svc := &fakeService{
		targets: []app.TargetCandidate{targetA, targetB},
		startFn: func(req app.StartRequest) (app.StartOutcome, error) {
			return app.StartOutcome{}, errors.New("temporary refusal")
		},
	}
	m, q := newTestModel(t, Config{
		Service:    svc,
		ScanSource: ScanSourceInfo{Available: true, TargetSelection: true},
	})
	drainAll(t, m, q)
	if len(m.targets) != 2 {
		t.Fatalf("targets = %+v", m.targets)
	}
	m.targetChoice = 0
	m.armRescan(app.RescanRequest{SourceRecordID: "rec", Positions: []int{4}}, "rec")
	m.Start()
	drainAll(t, m, q)
	m.targetChoice = 1
	m.Start()
	drainAll(t, m, q)
	if len(svc.starts) != 2 {
		t.Fatalf("starts = %+v", svc.starts)
	}
	if svc.starts[0].Target == nil || svc.starts[0].Target.Handle != targetA.Handle ||
		svc.starts[1].Target == nil || svc.starts[1].Target.Handle != targetB.Handle {
		t.Fatalf("targets = %+v / %+v", svc.starts[0].Target, svc.starts[1].Target)
	}
	for i := range svc.starts {
		if svc.starts[i].Rescan == nil || !reflect.DeepEqual(svc.starts[i].Rescan.Positions, []int{4}) {
			t.Fatalf("start %d lost the range: %+v", i+1, svc.starts[i])
		}
	}
}

func TestRescanPreparationClearsOnRecordEntries(t *testing.T) {
	cases := []struct {
		name string
		open func(*Model)
	}{
		{name: "records sidebar", open: func(m *Model) { m.showRecordsDestination() }},
		{name: "same-record notification", open: func(m *Model) { m.OpenNotification("rec") }},
		{name: "notification to another record", open: func(m *Model) { m.OpenNotification("other") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeService{
				history: []app.HistoryRecord{{ID: "rec"}, {ID: "other"}},
				loads:   map[string]*record.Recovery{"rec": rescanRecord(), "other": recovery()},
			}
			m, q := newTestModel(t, Config{Service: svc})
			drainAll(t, m, q)
			m.rec = &RecordView{ID: "rec", Shell: rescanRecord().Checkpoint.Session}
			m.page = pageScan
			m.armRescan(app.RescanRequest{SourceRecordID: "rec", Positions: []int{4}}, "rec")
			m.rescanDialog = rescanDialog{open: true, sourceID: "rec"}
			tc.open(m)
			drainAll(t, m, q)
			if m.pendingRescan != nil || m.rescanDialog.open {
				t.Fatalf("record entry left rescan state: pending=%+v dialog=%v",
					m.pendingRescan, m.rescanDialog.open)
			}
		})
	}
}

func TestRescanDialogCannotConfirmAfterRecordContextChange(t *testing.T) {
	rec := rescanRecord()
	m := rescanModel(rec)
	cfgSvc := &fakeService{}
	m.cfg.Service = cfgSvc
	m.openRescanDialog()
	if !m.rescanDialog.open {
		t.Fatal("dialog did not open")
	}
	// Simulate a notification opening another record while the picker is
	// still open. The confirm path must reject the old source even if the
	// dialog state was not cleared.
	m.rec = summaryView(1, app.ItemClassCounts{}, record.SessionShell{
		SessionID: "other", Game: "zzz", Entity: "driveDisc",
		Client: record.ClientWindowsLocal, Locale: "zh-CN",
		Scope: []byte(`{"kind":"zzzInventory","mode":"traverse"}`),
	})
	m.rec.ID = "other"
	m.confirmRescanDialog()
	if m.rescanDialog.open || m.pendingRescan != nil {
		t.Fatalf("stale picker confirmed: dialog=%v pending=%+v", m.rescanDialog.open, m.pendingRescan)
	}
	if len(cfgSvc.starts) != 0 {
		t.Fatalf("stale picker started a scan: %+v", cfgSvc.starts)
	}
}

func TestRescanDialogCannotConfirmPositionsThatBecameComplete(t *testing.T) {
	rec := rescanRecord()
	m := rescanModel(rec)
	cfgSvc := &fakeService{}
	m.cfg.Service = cfgSvc
	m.openRescanDialog()
	// The same record now proves every planned position complete before the
	// user confirms the old picker.
	m.rec.Shell.Coverage = record.Coverage{Status: record.CoverageComplete}
	m.rec.Shell.PositionLedger = &record.PositionLedger{
		Sort: record.SortVerificationVerified, Positions: summaryCompletePositions(),
	}
	m.confirmRescanDialog()
	if m.rescanDialog.open || m.pendingRescan != nil {
		t.Fatalf("changed record confirmed the old selection: dialog=%v pending=%+v",
			m.rescanDialog.open, m.pendingRescan)
	}
	if m.rescanNotice == "" {
		t.Fatal("the stale selection has no explanation")
	}
	if len(cfgSvc.starts) != 0 {
		t.Fatalf("stale picker started a scan: %+v", cfgSvc.starts)
	}
}

func TestRescanSummaryRefusesToShowADamagedRangeAsFull(t *testing.T) {
	rec := rescanRecord()
	rec.Checkpoint.Session.Scope = []byte(`{"positions":[4],"rescanOf":""}`)
	rec.Checkpoint.Session.Coverage = record.Coverage{Status: record.CoveragePartial}
	rec.Checkpoint.Session.PositionLedger = &record.PositionLedger{
		Sort:      record.SortVerificationVerified,
		Positions: []record.PositionConclusion{{Position: 4, Complete: true}},
	}
	m := rescanModel(rec)
	if action, ok := m.rescanIncompleteAction(); ok {
		t.Fatalf("damaged range offered a rescan: %+v", action)
	}
	if !strings.Contains(m.summaryCoverageLine(), "范围记录无效") {
		t.Fatalf("coverage line = %q", m.summaryCoverageLine())
	}
	m, q, tt, _ := openSummaryRecord(t, rec, Config{DirectExportSaver: &fakeDirectExportSaver{}}, 1180, 900)
	settle(t, m, q, tt)
	if !tt.HasText("范围记录无效，无法确认覆盖范围") ||
		!tt.HasText("补扫范围数据不完整；不能导出，也不能继续按该范围补扫。") ||
		tt.HasText("覆盖：全部") {
		t.Fatalf("summary treated a damaged range as a full scan: %q", tt.Texts())
	}
	if m.exportBuildErr == "" || !strings.Contains(strings.Join(tt.Texts(), "\n"), "导出尚未就绪") {
		t.Fatalf("damaged range did not block export: err=%q texts=%q", m.exportBuildErr, tt.Texts())
	}
}

func TestRescanSummaryNamesOnlyThePlannedRange(t *testing.T) {
	rec := rescanRecord()
	rec.Checkpoint.Session.Scope = []byte(`{"kind":"zzzInventory","mode":"traverse","positions":[4],"rescanOf":"src"}`)
	rec.Checkpoint.Session.Coverage = record.Coverage{Status: record.CoveragePartial}
	rec.Checkpoint.Session.PositionLedger = &record.PositionLedger{
		Sort:      record.SortVerificationVerified,
		Positions: []record.PositionConclusion{{Position: 4, Complete: true}},
	}
	m := rescanModel(rec)
	got := m.recordSummaryConclusion()
	if !strings.Contains(got.Title, "补扫完成：4 号位完整") {
		t.Fatalf("title = %q", got.Title)
	}
	if !strings.Contains(got.Supplement, "独立补扫记录") {
		t.Fatalf("supplement = %q, want the independent-record statement", got.Supplement)
	}
	if strings.Contains(strings.Join(got.Notes, "\n"), "1 号位") {
		t.Fatalf("unplanned position leaked into the notes: %q", got.Notes)
	}
	if m.summaryCoverageLine() != "覆盖：部分 · 补扫 4 号位 · 独立补扫记录" {
		t.Fatalf("coverage line = %q", m.summaryCoverageLine())
	}

	rec.Checkpoint.Session.PositionLedger = &record.PositionLedger{
		Sort:      record.SortVerificationUnverified,
		Positions: []record.PositionConclusion{{Position: 4, Stage: "scroll", ReasonCode: "positionLost"}},
	}
	m = rescanModel(rec)
	got = m.recordSummaryConclusion()
	if !strings.Contains(got.Title, "4 号位不完整") {
		t.Fatalf("incomplete title = %q", got.Title)
	}
	notes := strings.Join(got.Notes, "\n")
	if !strings.Contains(notes, "4 号位：不完整") || strings.Contains(notes, "1 号位") {
		t.Fatalf("incomplete notes = %q", notes)
	}

	rec.Checkpoint.Session.Scope = []byte(`{"kind":"zzzInventory","mode":"traverse","positions":[4,5],"rescanOf":"src"}`)
	rec.Checkpoint.Session.PositionLedger = &record.PositionLedger{
		Sort: record.SortVerificationUnverified,
		Positions: []record.PositionConclusion{
			{Position: 4, Complete: true},
			{Position: 5, Stage: "scroll", ReasonCode: "positionLost"},
		},
	}
	m = rescanModel(rec)
	got = m.recordSummaryConclusion()
	if !strings.Contains(got.Title, "5 号位不完整") || strings.Contains(got.Title, "4 号位不完整") {
		t.Fatalf("mixed summary title = %q", got.Title)
	}
}

func TestViewRescanSummaryAndPreparedRange(t *testing.T) {
	rec := rescanRecord()
	rec.Checkpoint.Session.Scope = []byte(`{"kind":"zzzInventory","mode":"traverse","positions":[4],"rescanOf":"src"}`)
	rec.Checkpoint.Session.PositionLedger = &record.PositionLedger{
		Sort:      record.SortVerificationVerified,
		Positions: []record.PositionConclusion{{Position: 4, Complete: true}},
	}
	m, q, tt, _ := openSummaryRecord(t, rec, Config{DirectExportSaver: &fakeDirectExportSaver{}}, 1180, 900)
	if !tt.HasText("补扫完成：4 号位完整") || !tt.HasText("覆盖：部分 · 补扫 4 号位 · 独立补扫记录") {
		t.Fatalf("the summary does not name the independent range: %q", tt.Texts())
	}
	if tt.HasText("1 号位：未扫描") || tt.HasText("2 号位：未扫描") {
		t.Fatalf("a deliberate omission reads as a missing position: %q", tt.Texts())
	}

	m.navigatePage(pageScan)
	settle(t, m, q, tt)
	const fullRange = "计划范围 · S 级 · 1–6 号位"
	if !tt.HasText(fullRange) {
		t.Fatalf("the ordinary preparation page does not state the full range: %q", tt.Texts())
	}
	for _, tc := range []struct {
		text      string
		positions []int
	}{
		{text: "5 号位", positions: []int{5}},
		{text: "2–3、5 号位", positions: []int{2, 3, 5}},
	} {
		t.Run(tc.text, func(t *testing.T) {
			req := app.RescanRequest{SourceRecordID: "rec", Positions: tc.positions}
			m.armRescan(req, "rec")
			settle(t, m, q, tt)
			if !tt.HasText("补扫准备 · S 级 · "+tc.text) || tt.HasText(fullRange) ||
				!tt.HasText("开始后会新建一条独立记录；原记录、图片、修订和导出的文件都不会改变。") {
				t.Fatalf("the preparation banner and checklist do not agree on %s: %q", tc.text, tt.Texts())
			}
			if got := m.pendingRescan.request(); !reflect.DeepEqual(got, req) {
				t.Fatalf("rendering changed the prepared request from %+v to %+v", req, got)
			}
			if err := tt.Click("取消补扫"); err != nil {
				t.Fatal(err)
			}
			settle(t, m, q, tt)
			if m.pendingRescan != nil || !tt.HasText(fullRange) || tt.HasText("补扫准备 · S 级 · "+tc.text) {
				t.Fatalf("cancelling the rescan did not restore the full range: %q", tt.Texts())
			}
		})
	}
}

func TestViewRescanDialogShowsTheOfferedPositions(t *testing.T) {
	rec := rescanRecord()
	m, q, tt, _ := openSummaryRecord(t, rec, Config{DirectExportSaver: &fakeDirectExportSaver{}}, 1180, 900)
	action, ok := m.rescanIncompleteAction()
	if !ok {
		t.Fatal("the eligible record did not offer the partial rescan")
	}
	if err := tt.Click(action.Label); err != nil {
		t.Fatalf("click %q: %v", action.Label, err)
	}
	settle(t, m, q, tt)
	if !tt.HasText("补扫不完整号位") ||
		!tt.HasText("补扫会新建一条独立记录，只访问选中的号位；原记录、图片、修订和导出的文件都不会改变。") {
		t.Fatalf("the picker does not state the selection and its consequence: %q", tt.Texts())
	}
	texts := strings.Join(tt.Texts(), "\n")
	if !strings.Contains(texts, "2 号位 · 不完整 · 筛选准备失败") ||
		!strings.Contains(texts, "3 号位 · 未完成 · 扫描在这里停止") {
		t.Fatalf("the picker does not name the position states: %q", tt.Texts())
	}
	if !strings.Contains(texts, "4 号位 · 未到达") || !strings.Contains(texts, "5 号位 · 未到达") {
		t.Fatalf("the picker does not name the unreached positions: %q", tt.Texts())
	}
	if !tt.HasText("准备补扫（2–6 号位）") {
		t.Fatalf("the confirm label does not name the selected range: %q", tt.Texts())
	}
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	if m.rescanDialog.open {
		t.Fatal("取消 left the picker open")
	}
	if m.pendingRescan != nil {
		t.Fatal("取消 armed a range")
	}
}

func TestRescanNotificationNamesTheIndependentRange(t *testing.T) {
	facts := notificationFacts{
		saved:         2,
		counts:        app.ItemClassCounts{Passed: 2},
		countsKnown:   true,
		coverage:      record.CoveragePartial,
		outcomesKnown: true,
		outcomes: []app.PositionOutcome{
			{Position: 1, State: app.PositionOutcomeNotReached},
			{Position: 2, State: app.PositionOutcomeNotReached},
			{Position: 3, State: app.PositionOutcomeNotReached},
			{Position: 4, State: app.PositionOutcomeComplete},
			{Position: 5, State: app.PositionOutcomeNotReached},
			{Position: 6, State: app.PositionOutcomeNotReached},
		},
		scanPlan: app.ScanPlan{Positions: []int{4}, RescanOf: "src"},
	}
	title, body := completedNotificationWording(facts)
	if title != "补扫完成：4 号位完整" || !strings.Contains(body, "已保存 2 件") {
		t.Fatalf("notification = %q / %q", title, body)
	}
	facts.outcomes[3].State = app.PositionOutcomeDegraded
	title, _ = completedNotificationWording(facts)
	if title != "补扫完成：4 号位仍不完整" {
		t.Fatalf("incomplete notification = %q", title)
	}
	if strings.Contains(incompletePositions(facts), "1 号位") {
		t.Fatalf("unplanned position leaked into the notification: %q", incompletePositions(facts))
	}

	// A complete selected position is not named as incomplete.
	facts.scanPlan.Positions = []int{4, 5}
	facts.outcomes[3].State = app.PositionOutcomeComplete
	facts.outcomes[4].State = app.PositionOutcomeDegraded
	title, _ = completedNotificationWording(facts)
	if title != "补扫完成：5 号位仍不完整" {
		t.Fatalf("mixed notification = %q", title)
	}
}

func TestRescanNotificationUsesTheInvalidRangeWording(t *testing.T) {
	rec := rescanRecord()
	rec.Checkpoint.Session.Scope = []byte(`{"positions":[4],"rescanOf":""}`)
	rec.Checkpoint.Session.Coverage = record.Coverage{Status: record.CoverageComplete}
	rec.Checkpoint.Session.PositionLedger = &record.PositionLedger{
		Positions: []record.PositionConclusion{{Position: 1, Stage: "filter", ReasonCode: "navigationMismatch"}},
	}
	state := endedSession("damaged-range", 1, 1, 1)
	facts := notificationFactsFor(state, rec)
	if !facts.rangeInvalid || facts.coverage != record.CoveragePartial {
		t.Fatalf("damaged facts = invalid %v coverage %q", facts.rangeInvalid, facts.coverage)
	}
	title, body := notificationWording(facts)
	if title != "范围记录无效，无法确认覆盖" {
		t.Fatalf("notification title = %q", title)
	}
	if strings.Contains(title+body, "没有扫全") || strings.Contains(title+body, "1 号位") {
		t.Fatalf("damaged notification guessed a full or position conclusion: %q / %q", title, body)
	}
}

func TestRescanHistoryRowDoesNotClaimAFullScan(t *testing.T) {
	in := recordConclusionInput{
		Termination:      &record.Termination{Kind: record.TerminationCompleted, Code: "verifiedEnd", Phase: record.PhaseCapturing},
		Coverage:         record.CoveragePartial,
		CapturedCount:    2,
		Counts:           app.ItemClassCounts{Passed: 2},
		PlannedPositions: []int{4},
		Rescan:           true,
		PositionOutcomes: []app.PositionOutcome{
			{Position: 4, State: app.PositionOutcomeComplete},
		},
		PositionOutcomesKnown: true,
	}
	got := recordConclusionFor(in)
	if !strings.Contains(got.text, "补扫完成 · 4 号位完整") {
		t.Fatalf("history conclusion = %q", got.text)
	}
	if strings.Contains(got.text, "没有扫全") {
		t.Fatalf("a scoped rescan reads as a failed full scan: %q", got.text)
	}

	in.PositionOutcomes = []app.PositionOutcome{{Position: 4, State: app.PositionOutcomeDegraded}}
	if got := recordConclusionFor(in); !strings.Contains(got.text, "4 号位不完整") {
		t.Fatalf("incomplete history conclusion = %q", got.text)
	}

	// A complete selected position is not named in the incomplete list.
	in.PlannedPositions = []int{4, 5}
	in.PositionOutcomes = []app.PositionOutcome{
		{Position: 4, State: app.PositionOutcomeComplete},
		{Position: 5, State: app.PositionOutcomeNotReached},
	}
	if got := recordConclusionFor(in); !strings.Contains(got.text, "5 号位不完整") ||
		strings.Contains(got.text, "4 号位不完整") {
		t.Fatalf("mixed history conclusion = %q", got.text)
	}

	in.CapturedCount = 0
	in.Counts = app.ItemClassCounts{}
	if got := recordConclusionFor(in); !strings.Contains(got.text, "5 号位不完整") ||
		strings.Contains(got.text, "4 号位不完整") {
		t.Fatalf("mixed empty history conclusion = %q", got.text)
	}

	// Damaged range data is not an ordinary full record; the list must not
	// turn its saved complete coverage into a full-range claim.
	in.RangeInvalid = true
	in.Rescan = false
	in.PlannedPositions = nil
	in.Coverage = record.CoverageComplete
	if got := recordConclusionFor(in); !strings.Contains(got.text, "范围记录无效") ||
		strings.Contains(got.text, "覆盖：全部") {
		t.Fatalf("damaged range history conclusion = %q", got.text)
	}
}

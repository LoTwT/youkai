package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/game/zzz/collect"
	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/platform/platformfake"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
)

type prepareFailController struct{}

func (prepareFailController) Prepare(context.Context, *scan.ControllerHandle) error {
	return scan.NewFault("pageUnverified", errors.New("not the warehouse page"))
}

func (prepareFailController) Collect(context.Context, *scan.ControllerHandle) error { return nil }

// scopedReportingTraversal is the test traversal for app-level rescan
// fixtures: it accepts the controller's position scope and still reports
// the scripted observed scope.
type scopedReportingTraversal struct {
	*reportingTraversal
	positions []int
}

func (t *scopedReportingTraversal) SetScanPositions(positions []int) error {
	t.positions = append([]int(nil), positions...)
	return nil
}

// rescanSource answers only the scoped start: these tests never expect a
// full-warehouse fallback.
type rescanSource struct {
	spec     ScanSpec
	err      error
	requests []RescanRequest
}

func (s *rescanSource) NewScan() (ScanSpec, error) {
	return ScanSpec{}, errors.New("full scan must not start")
}

func (s *rescanSource) NewScanForRescan(_ *TargetCandidate, req RescanRequest) (ScanSpec, error) {
	s.requests = append(s.requests, req)
	return s.spec, s.err
}

// saveRescanSource writes one saved source record directly, so a test can
// pin its declared range and position ledger without running a scan.
func saveRescanSource(t *testing.T, paths Paths, id string, client record.Client, scope json.RawMessage, coverage record.CoverageStatus, sort record.SortVerification, positions []record.PositionConclusion) {
	t.Helper()
	store, err := record.Open(paths.Data)
	if err != nil {
		t.Fatalf("open record store: %v", err)
	}
	if err := store.CreateRecord(id); err != nil {
		t.Fatalf("create source record: %v", err)
	}
	started := time.Date(2026, 11, 19, 10, 0, 0, 0, time.Local)
	ended := started.Add(12 * time.Minute)
	cp := record.Checkpoint{Session: record.SessionShell{
		SessionID: id,
		Game:      "zzz",
		Entity:    "driveDisc",
		Client:    client,
		Locale:    "zh-CN",
		Scope:     scope,
		StartedAt: started,
		EndedAt:   &ended,
		Termination: &record.Termination{
			Kind: record.TerminationCompleted, Code: "verifiedEnd", Phase: record.PhaseCapturing,
		},
		Coverage: record.Coverage{Status: coverage},
	}}
	if sort != record.SortVerificationUnknown {
		cp.Session.PositionLedger = &record.PositionLedger{Sort: sort, Positions: positions}
	}
	if _, err := store.SaveCheckpoint(id, cp); err != nil {
		t.Fatalf("save source record: %v", err)
	}
}

func scopedScope(sourceID string, positions ...int) json.RawMessage {
	raw, err := json.Marshal(struct {
		Kind      string `json:"kind"`
		Mode      string `json:"mode"`
		Positions []int  `json:"positions"`
		RescanOf  string `json:"rescanOf"`
	}{Kind: "zzzInventory", Mode: "traverse", Positions: positions, RescanOf: sourceID})
	if err != nil {
		panic(err)
	}
	return raw
}

func TestDeclaredScanPlan(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want ScanPlan
		ok   bool
	}{
		{name: "historical full scope", raw: `{"kind":"zzzInventory","mode":"traverse"}`, ok: true},
		{name: "scoped rescan", raw: `{"positions":[4],"rescanOf":"src"}`, want: ScanPlan{Positions: []int{4}, RescanOf: "src"}, ok: true},
		{name: "empty positions", raw: `{"positions":[],"rescanOf":"src"}`},
		{name: "duplicate positions", raw: `{"positions":[2,2],"rescanOf":"src"}`},
		{name: "descending positions", raw: `{"positions":[4,2],"rescanOf":"src"}`},
		{name: "out of range", raw: `{"positions":[7],"rescanOf":"src"}`},
		{name: "source without range", raw: `{"rescanOf":"src"}`},
		{name: "scoped range without source", raw: `{"positions":[4]}`},
		{name: "case-variant source", raw: `{"positions":[4],"RescanOf":"src"}`},
		{name: "duplicate positions", raw: `{"positions":[4],"positions":[1,2,3,4,5,6],"rescanOf":"src"}`},
		{name: "duplicate source", raw: `{"positions":[4],"rescanOf":"first","rescanOf":"second"}`},
		{name: "not json", raw: `{`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := DeclaredScanPlan(json.RawMessage(tc.raw))
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v (plan %+v)", ok, tc.ok, got)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("plan = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestScanScopeDamaged(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "old full scope", raw: `{"kind":"zzzInventory","mode":"traverse"}`},
		{name: "valid scoped scope", raw: `{"positions":[4],"rescanOf":"src"}`},
		{name: "empty scope", raw: ``},
		{name: "not json", raw: `{`},
		{name: "json null", raw: `null`, want: true},
		{name: "json array", raw: `[]`, want: true},
		{name: "empty positions", raw: `{"positions":[],"rescanOf":"src"}`, want: true},
		{name: "missing source", raw: `{"positions":[4]}`, want: true},
		{name: "missing positions", raw: `{"rescanOf":"src"}`, want: true},
		{name: "wrong source type", raw: `{"positions":[4],"rescanOf":7}`, want: true},
		{name: "case-variant source", raw: `{"positions":[4],"RescanOf":"src"}`, want: true},
		{name: "case-variant positions", raw: `{"Positions":[4],"rescanOf":"src"}`, want: true},
		{name: "both case variants", raw: `{"Positions":[4],"RescanOf":"src"}`, want: true},
		{name: "duplicate positions", raw: `{"positions":[4],"positions":[1,2,3,4,5,6],"rescanOf":"src"}`, want: true},
		{name: "duplicate source", raw: `{"positions":[4],"rescanOf":"first","rescanOf":"second"}`, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ScanScopeDamaged(json.RawMessage(tc.raw)); got != tc.want {
				t.Fatalf("ScanScopeDamaged(%s) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestRescanCandidatesOf(t *testing.T) {
	full := record.SessionShell{
		Scope: json.RawMessage(`{"kind":"zzzInventory","mode":"traverse"}`),
		PositionLedger: &record.PositionLedger{Sort: record.SortVerificationUnverified, Positions: []record.PositionConclusion{
			{Position: 1, Complete: true},
			{Position: 2, Stage: "filter", ReasonCode: "navigationMismatch"},
			{Position: 3},
		}},
	}
	candidates, ok := RescanCandidatesOf(full)
	if !ok {
		t.Fatal("a full record with a ledger must list candidates")
	}
	var positions []int
	for _, candidate := range candidates {
		positions = append(positions, candidate.Position)
	}
	if want := []int{2, 3, 4, 5, 6}; !reflect.DeepEqual(positions, want) {
		t.Fatalf("full candidates = %v, want %v", positions, want)
	}

	scoped := record.SessionShell{
		Scope: scopedScope("src", 2, 4),
		PositionLedger: &record.PositionLedger{Sort: record.SortVerificationUnverified, Positions: []record.PositionConclusion{
			{Position: 1, Complete: true},
			{Position: 2, Complete: true},
			{Position: 4, Stage: "scroll", ReasonCode: "positionLost"},
		}},
	}
	if _, ok := RescanCandidatesOf(record.SessionShell{Scope: json.RawMessage(`{"Positions":[4],"RescanOf":"src"}`)}); ok {
		t.Fatal("a damaged case-variant scope must not offer candidates")
	}
	candidates, ok = RescanCandidatesOf(scoped)
	if !ok || len(candidates) != 1 || candidates[0].Position != 4 {
		t.Fatalf("scoped candidates = %+v, ok %v; want only position 4", candidates, ok)
	}

	complete := record.SessionShell{
		Scope: scopedScope("src", 4),
		PositionLedger: &record.PositionLedger{Sort: record.SortVerificationVerified, Positions: []record.PositionConclusion{
			{Position: 4, Complete: true},
		}},
	}
	if candidates, ok := RescanCandidatesOf(complete); !ok || len(candidates) != 0 {
		t.Fatalf("complete scoped candidates = %+v, ok %v; want none", candidates, ok)
	}

	if _, ok := RescanCandidatesOf(record.SessionShell{Scope: json.RawMessage(`{"positions":[4],"rescanOf":"src"}`)}); ok {
		t.Fatal("a record without a ledger must not list candidates")
	}
}

func TestRescanRequestValidate(t *testing.T) {
	valid := RescanRequest{SourceRecordID: "src", Positions: []int{2, 4}}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid request refused: %v", err)
	}
	cases := []struct {
		name string
		req  RescanRequest
	}{
		{name: "empty source", req: RescanRequest{Positions: []int{1}}},
		{name: "empty positions", req: RescanRequest{SourceRecordID: "src"}},
		{name: "duplicate", req: RescanRequest{SourceRecordID: "src", Positions: []int{2, 2}}},
		{name: "descending", req: RescanRequest{SourceRecordID: "src", Positions: []int{3, 2}}},
		{name: "outside range", req: RescanRequest{SourceRecordID: "src", Positions: []int{0}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.req.Validate(); !errors.Is(err, ErrRescanRequest) {
				t.Fatalf("Validate = %v, want ErrRescanRequest", err)
			}
		})
	}
}

func TestRescanSourceValidation(t *testing.T) {
	paths := newTestPaths(t)
	saveRescanSource(t, paths, "src-partial", record.ClientWindowsLocal,
		json.RawMessage(`{"kind":"zzzInventory","mode":"traverse"}`),
		record.CoveragePartial, record.SortVerificationUnverified,
		[]record.PositionConclusion{
			{Position: 1, Complete: true},
			{Position: 2, Stage: "filter", ReasonCode: "navigationMismatch"},
		})
	saveRescanSource(t, paths, "src-scoped", record.ClientWindowsLocal,
		scopedScope("src-partial", 2, 4),
		record.CoveragePartial, record.SortVerificationUnverified,
		[]record.PositionConclusion{
			{Position: 2, Complete: true},
			{Position: 4, Stage: "scroll", ReasonCode: "positionLost"},
		})
	saveRescanSource(t, paths, "src-damaged", record.ClientWindowsLocal,
		json.RawMessage(`{"positions":[4],"rescanOf":""}`),
		record.CoveragePartial, record.SortVerificationUnverified,
		[]record.PositionConclusion{{Position: 2, Stage: "filter"}})
	saveRescanSource(t, paths, "src-case-variant", record.ClientWindowsLocal,
		json.RawMessage(`{"Positions":[4],"RescanOf":"src-damaged"}`),
		record.CoveragePartial, record.SortVerificationUnverified,
		[]record.PositionConclusion{{Position: 2, Stage: "filter"}})
	saveRescanSource(t, paths, "src-duplicate", record.ClientWindowsLocal,
		json.RawMessage(`{"positions":[4],"positions":[1,2,3,4,5,6],"rescanOf":"src-duplicate"}`),
		record.CoveragePartial, record.SortVerificationUnverified,
		[]record.PositionConclusion{{Position: 2, Stage: "filter"}})
	saveRescanSource(t, paths, "src-mac", record.ClientMacOSCloud,
		json.RawMessage(`{"kind":"zzzInventory","mode":"traverse"}`),
		record.CoveragePartial, record.SortVerificationUnverified,
		[]record.PositionConclusion{{Position: 2, Stage: "filter"}})
	saveRescanSource(t, paths, "src-noledger", record.ClientWindowsLocal,
		json.RawMessage(`{"kind":"zzzInventory","mode":"traverse"}`),
		record.CoverageUnknown, record.SortVerificationUnknown, nil)
	saveRescanSource(t, paths, "src-broken", record.ClientWindowsLocal,
		json.RawMessage(`{"kind":"zzzInventory","mode":"traverse"}`),
		record.CoveragePartial, record.SortVerificationUnverified,
		[]record.PositionConclusion{{Position: 2, Stage: "filter"}})
	if err := os.WriteFile(filepath.Join(paths.Data, "sessions", "src-broken", "checkpoints", "1.json"), []byte("{"), 0o644); err != nil {
		t.Fatalf("break newest generation: %v", err)
	}
	saveRescanSource(t, paths, "src-evidence", record.ClientWindowsLocal,
		json.RawMessage(`{"kind":"zzzInventory","mode":"traverse"}`),
		record.CoveragePartial, record.SortVerificationUnverified,
		[]record.PositionConclusion{{Position: 2, Stage: "filter"}})
	store, err := record.Open(paths.Data)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	evidenceRec, err := store.Load("src-evidence")
	if err != nil {
		t.Fatalf("load evidence source: %v", err)
	}
	ev, err := store.WriteEvidence("src-evidence", "coverage-end", bytes.Repeat([]byte("c"), 32))
	if err != nil {
		t.Fatalf("write coverage evidence: %v", err)
	}
	evidenceShell := evidenceRec.Checkpoint.Session
	evidenceShell.CoverageEvidenceIDs = []string{ev.ID}
	if _, err := store.SaveCheckpoint("src-evidence", record.Checkpoint{
		Session: evidenceShell, Evidence: []record.EvidenceFile{ev},
	}); err != nil {
		t.Fatalf("save evidence generation: %v", err)
	}
	if err := os.Remove(filepath.Join(paths.Data, "sessions", "src-evidence", "evidence", ev.ID+".png")); err != nil {
		t.Fatalf("remove newest evidence: %v", err)
	}
	saveRescanSource(t, paths, "src-identity", record.ClientWindowsLocal,
		json.RawMessage(`{"kind":"zzzInventory","mode":"traverse"}`),
		record.CoveragePartial, record.SortVerificationUnverified,
		[]record.PositionConclusion{{Position: 2, Stage: "filter"}})
	identityRec, err := store.Load("src-identity")
	if err != nil {
		t.Fatalf("load identity source: %v", err)
	}
	identityCheckpoint := identityRec.Checkpoint
	identityCheckpoint.Session.SessionID = "b7a26d1e-1f95-4b4f-8f3b-64b8ca2a6d11"
	if _, err := store.SaveCheckpoint("src-identity", identityCheckpoint); err != nil {
		t.Fatalf("save cross-directory identity: %v", err)
	}

	svc, _ := newTestService(t, paths, &rescanSource{}, func() (Recognizer, error) { return newFakeRecognizer(), nil })
	cases := []struct {
		name string
		req  RescanRequest
		want error
	}{
		{name: "valid degraded position", req: RescanRequest{SourceRecordID: "src-partial", Positions: []int{2}}},
		{name: "valid unreached position", req: RescanRequest{SourceRecordID: "src-partial", Positions: []int{4}}},
		{name: "scoped valid", req: RescanRequest{SourceRecordID: "src-scoped", Positions: []int{4}}},
		{name: "unknown source", req: RescanRequest{SourceRecordID: "missing", Positions: []int{2}}, want: ErrRescanSource},
		{name: "non windows source", req: RescanRequest{SourceRecordID: "src-mac", Positions: []int{2}}, want: ErrRescanSource},
		{name: "no ledger", req: RescanRequest{SourceRecordID: "src-noledger", Positions: []int{2}}, want: ErrRescanSource},
		{name: "damaged partial scope", req: RescanRequest{SourceRecordID: "src-damaged", Positions: []int{2}}, want: ErrRescanSource},
		{name: "case-variant partial scope", req: RescanRequest{SourceRecordID: "src-case-variant", Positions: []int{2}}, want: ErrRescanSource},
		{name: "duplicate partial scope", req: RescanRequest{SourceRecordID: "src-duplicate", Positions: []int{2}}, want: ErrRescanSource},
		{name: "latest source unreadable", req: RescanRequest{SourceRecordID: "src-broken", Positions: []int{2}}, want: ErrRescanSource},
		{name: "latest evidence missing", req: RescanRequest{SourceRecordID: "src-evidence", Positions: []int{2}}, want: ErrRescanSource},
		{name: "cross-directory identity", req: RescanRequest{SourceRecordID: "src-identity", Positions: []int{2}}, want: ErrRescanSource},
		{name: "complete position", req: RescanRequest{SourceRecordID: "src-partial", Positions: []int{1}}, want: ErrRescanScope},
		{name: "unplanned position", req: RescanRequest{SourceRecordID: "src-scoped", Positions: []int{1}}, want: ErrRescanScope},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.validateRescanSource(context.Background(), tc.req)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("validateRescanSource = %v", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("validateRescanSource = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestStartRefusesUnsupportedRescanSource(t *testing.T) {
	svc, _ := newTestService(t, newTestPaths(t), staticSource{spec: specOf(&gatedController{})}, func() (Recognizer, error) { return newFakeRecognizer(), nil })
	_, err := svc.Start(StartRequest{ID: "rescan", Rescan: &RescanRequest{SourceRecordID: "src", Positions: []int{2}}})
	if !errors.Is(err, ErrRescanUnsupported) {
		t.Fatalf("start = %v, want ErrRescanUnsupported", err)
	}
}

func TestStartRefusesAFullScopeAdapterAnswer(t *testing.T) {
	paths := newTestPaths(t)
	sourceID := "src-partial"
	saveRescanSource(t, paths, sourceID, record.ClientWindowsLocal,
		json.RawMessage(`{"kind":"zzzInventory","mode":"traverse"}`),
		record.CoveragePartial, record.SortVerificationUnverified,
		[]record.PositionConclusion{{Position: 2, Stage: "filter"}})
	fullSpec := ScanSpec{
		Game: "zzz", Entity: "driveDisc", Client: record.ClientWindowsLocal, Locale: "zh-CN",
		Scope:      json.RawMessage(`{"kind":"zzzInventory","mode":"traverse"}`),
		Controller: &gatedController{},
	}
	svc, _ := newTestService(t, paths, &rescanSource{spec: fullSpec}, func() (Recognizer, error) { return newFakeRecognizer(), nil })
	_, err := svc.Start(StartRequest{ID: "rescan", Rescan: &RescanRequest{SourceRecordID: sourceID, Positions: []int{2}}})
	if !errors.Is(err, ErrRescanScope) {
		t.Fatalf("start = %v, want ErrRescanScope", err)
	}
	history, err := svc.HistoryRecords()
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) != 1 || history[0].ID != sourceID {
		t.Fatalf("a refused full fallback left a record: %+v", history)
	}
}

func TestStoppedPartialRescanStaysPartialAndLeavesTheSource(t *testing.T) {
	paths := newTestPaths(t)
	sourceID := "src-partial"
	saveRescanSource(t, paths, sourceID, record.ClientWindowsLocal,
		json.RawMessage(`{"kind":"zzzInventory","mode":"traverse"}`),
		record.CoveragePartial, record.SortVerificationUnverified,
		[]record.PositionConclusion{{Position: 2, Stage: "filter"}})
	before := hashRecordCheckpoint(t, paths, sourceID)

	gate := newGatedController()
	spec := ScanSpec{
		Game: "zzz", Entity: "driveDisc", Client: record.ClientWindowsLocal, Locale: "zh-CN",
		Scope:      scopedScope(sourceID, 4),
		Controller: gate,
	}
	source := &rescanSource{spec: spec}
	svc, _ := newTestService(t, paths, source, func() (Recognizer, error) { return newFakeRecognizer(), nil })
	outcome, err := svc.Start(StartRequest{ID: "rescan-4", Rescan: &RescanRequest{SourceRecordID: sourceID, Positions: []int{4}}})
	if err != nil {
		t.Fatalf("start rescan: %v", err)
	}
	waitSnapshot(t, svc, scan.PhaseCapturing, "rescan collecting")
	progress := svc.Snapshot()
	if !reflect.DeepEqual(progress.RescanPositions, []int{4}) {
		t.Fatalf("active rescan range = %v, want [4]", progress.RescanPositions)
	}
	progress.RescanPositions[0] = 1
	if got := svc.Snapshot().RescanPositions; !reflect.DeepEqual(got, []int{4}) {
		t.Fatalf("mutating a snapshot changed the active range: %v", got)
	}
	svc.Stop()
	waitWrapped(t, svc)
	if got := svc.Snapshot().RescanPositions; len(got) != 0 {
		t.Fatalf("the wrapped scan still reports an active range: %v", got)
	}

	rec, err := svc.LoadRecord(string(outcome.SessionID))
	if err != nil {
		t.Fatalf("load stopped rescan: %v", err)
	}
	if rec.Checkpoint.Session.Coverage.Status == record.CoverageComplete {
		t.Fatalf("a stopped rescan claimed complete coverage: %+v", rec.Checkpoint.Session)
	}
	if after := hashRecordCheckpoint(t, paths, sourceID); after != before {
		t.Fatalf("stopping the rescan changed the source record: %x -> %x", before, after)
	}
	gate.release()
}

// TestPartialRescanCreatesAnIndependentRecord is the user-level path: a
// saved partial record, a selected incomplete position, an ordinary start,
// and a new record whose range is the selection. The source record's bytes
// stay untouched, and the new record never becomes warehouse-complete.
func TestPartialRescanCreatesAnIndependentRecord(t *testing.T) {
	paths := newTestPaths(t)
	sourceID := "src-partial"
	saveRescanSource(t, paths, sourceID, record.ClientWindowsLocal,
		json.RawMessage(`{"kind":"zzzInventory","mode":"traverse"}`),
		record.CoveragePartial, record.SortVerificationUnverified,
		[]record.PositionConclusion{
			{Position: 1, Complete: true},
			{Position: 2, Complete: true},
			{Position: 4, Stage: "scroll", ReasonCode: "positionLost"},
			{Position: 6, Complete: true},
		})
	before := hashRecordCheckpoint(t, paths, sourceID)

	sess := platformfake.NewSession()
	port := &platformfake.Port{NewSession: func() *platformfake.Session { return sess }}
	// The scoped adapter deliberately claims complete coverage for its one
	// position; the record layer must still keep the warehouse conclusion
	// partial.
	final := json.RawMessage(`{"kind":"zzzInventory","sort":"verified","coverage":"complete","positions":[{"position":4,"viewports":1,"visits":2,"topVerified":true,"bottomVerified":true,"complete":true}]}`)
	var scopedTraversal *scopedReportingTraversal
	ctrl := &collect.Controller{
		Port:          port,
		Target:        collect.MacCloudTarget,
		Hotkey:        platform.HotkeySpec{Key: platform.KeyS},
		ScanPositions: []int{4},
		Traversal: func() collect.Traversal {
			scopedTraversal = &scopedReportingTraversal{reportingTraversal: &reportingTraversal{
				scope: json.RawMessage(`{"kind":"zzzInventory","sort":"unverified","coverage":"unknown"}`),
				after: final,
			}}
			return scopedTraversal
		},
	}
	source := &rescanSource{spec: ScanSpec{
		Game: "zzz", Entity: "driveDisc", Client: record.ClientWindowsLocal, Locale: "zh-CN",
		Scope:      scopedScope(sourceID, 4),
		Controller: ctrl,
	}}
	svc, _ := newTestService(t, paths, source, func() (Recognizer, error) { return newFakeRecognizer(), nil })

	outcome, err := svc.Start(StartRequest{
		ID:     "rescan-4",
		Rescan: &RescanRequest{SourceRecordID: sourceID, Positions: []int{4}},
	})
	if err != nil {
		t.Fatalf("start rescan: %v", err)
	}
	waitWrapped(t, svc)
	if len(source.requests) != 1 || !reflect.DeepEqual(source.requests[0].Positions, []int{4}) {
		t.Fatalf("adapter requests = %+v, want one request for position 4", source.requests)
	}
	if scopedTraversal == nil || !reflect.DeepEqual(scopedTraversal.positions, []int{4}) {
		t.Fatalf("traversal positions = %+v, want [4]", scopedTraversal)
	}
	if after := hashRecordCheckpoint(t, paths, sourceID); after != before {
		t.Fatalf("source record bytes changed: %x -> %x", before, after)
	}

	newID := string(outcome.SessionID)
	rec, err := svc.LoadRecord(newID)
	if err != nil {
		t.Fatalf("load rescan record: %v", err)
	}
	shell := rec.Checkpoint.Session
	if shell.Coverage.Status != record.CoveragePartial {
		t.Fatalf("rescan coverage = %q, want partial even when the adapter said complete", shell.Coverage.Status)
	}
	plan, ok := DeclaredScanPlan(shell.Scope)
	if !ok || !reflect.DeepEqual(plan.Positions, []int{4}) || plan.RescanOf != sourceID {
		t.Fatalf("rescan scope = %+v (ok %v), want positions [4] from %s", plan, ok, sourceID)
	}
	ledger := shell.PositionLedger
	if ledger == nil || len(ledger.Positions) != 1 || ledger.Positions[0].Position != 4 || !ledger.Positions[0].Complete {
		t.Fatalf("rescan ledger = %+v, want only complete position 4", ledger)
	}

	history, err := svc.HistoryRecords()
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	var newEntry, sourceEntry *HistoryRecord
	for i := range history {
		switch history[i].ID {
		case newID:
			newEntry = &history[i]
		case sourceID:
			sourceEntry = &history[i]
		}
	}
	if newEntry == nil || sourceEntry == nil {
		t.Fatalf("history missing records: %+v", history)
	}
	if !newEntry.ScanPositionsKnown || !reflect.DeepEqual(newEntry.ScanPositions, []int{4}) || newEntry.RescanOf != sourceID {
		t.Fatalf("history rescan metadata = %+v", newEntry)
	}
	if len(newEntry.PositionOutcomes) != record.PositionCount || newEntry.PositionOutcomes[3].State != PositionOutcomeComplete {
		t.Fatalf("history position outcomes = %+v", newEntry.PositionOutcomes)
	}
	if sourceEntry.RescanOf != "" || len(sourceEntry.ScanPositions) != 0 || !sourceEntry.ScanPositionsKnown {
		t.Fatalf("source history metadata changed: %+v", sourceEntry)
	}

	rescanExport, err := BuildUsableExport(rec)
	if err != nil {
		t.Fatalf("build rescan export: %v", err)
	}
	if !reflect.DeepEqual(rescanExport.session.Scope.Slots, []int{4}) ||
		rescanExport.session.Coverage.Status != record.CoveragePartial ||
		rescanExport.Summary.Included != 0 || rescanExport.Summary.Captured != 0 {
		t.Fatalf("rescan export = slots %v coverage %q counts %+v",
			rescanExport.session.Scope.Slots, rescanExport.session.Coverage.Status, rescanExport.Summary)
	}
}

// Damaged partial-rescan fields are readable records, but the history list
// must not mistake their saved coverage for an ordinary full range.
func TestHistoryMarksADamagedPartialScope(t *testing.T) {
	paths := newTestPaths(t)
	const id = "damaged-range"
	saveRescanSource(t, paths, id, record.ClientWindowsLocal,
		json.RawMessage(`{"positions":[4],"rescanOf":""}`),
		record.CoveragePartial, record.SortVerificationUnknown, nil)
	svc, _ := newTestService(t, paths, &rescanSource{}, func() (Recognizer, error) { return newFakeRecognizer(), nil })
	history, err := svc.HistoryRecords()
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("history = %+v, want one record", history)
	}
	entry := history[0]
	if !entry.RangeInvalid || entry.ScanPositionsKnown || entry.RescanOf != "" {
		t.Fatalf("damaged history metadata = %+v", entry)
	}
}

// A scoped session's observed-scope reports may still say unknown before
// any position exists; the fixed partial conclusion must survive them.
func TestScopedObservedUnknownStaysPartial(t *testing.T) {
	paths := newTestPaths(t)
	sourceID := "src-partial"
	saveRescanSource(t, paths, sourceID, record.ClientWindowsLocal,
		json.RawMessage(`{"kind":"zzzInventory","mode":"traverse"}`),
		record.CoveragePartial, record.SortVerificationUnverified,
		[]record.PositionConclusion{{Position: 4, Stage: "scroll", ReasonCode: "positionLost"}})
	sess := platformfake.NewSession()
	port := &platformfake.Port{NewSession: func() *platformfake.Session { return sess }}
	ctrl := &collect.Controller{
		Port:          port,
		Target:        collect.MacCloudTarget,
		Hotkey:        platform.HotkeySpec{Key: platform.KeyS},
		ScanPositions: []int{4},
		Traversal: func() collect.Traversal {
			return &scopedReportingTraversal{reportingTraversal: &reportingTraversal{scope: json.RawMessage(
				`{"kind":"zzzInventory","page":"warehouse","sort":"unverified","coverage":"unknown"}`)}}
		},
	}
	source := &rescanSource{spec: ScanSpec{
		Game: "zzz", Entity: "driveDisc", Client: record.ClientWindowsLocal, Locale: "zh-CN",
		Scope:      scopedScope(sourceID, 4),
		Controller: ctrl,
	}}
	svc, _ := newTestService(t, paths, source, func() (Recognizer, error) { return newFakeRecognizer(), nil })
	outcome, err := svc.Start(StartRequest{
		ID:     "rescan-observed-unknown",
		Rescan: &RescanRequest{SourceRecordID: sourceID, Positions: []int{4}},
	})
	if err != nil {
		t.Fatalf("start rescan: %v", err)
	}
	waitWrapped(t, svc)
	rec, err := svc.LoadRecord(string(outcome.SessionID))
	if err != nil {
		t.Fatalf("load rescan record: %v", err)
	}
	if rec.Checkpoint.Session.Coverage.Status != record.CoveragePartial {
		t.Fatalf("observed unknown changed scoped coverage to %q", rec.Checkpoint.Session.Coverage.Status)
	}
	if ledger, known := rec.Checkpoint.Session.PositionConclusions(); known {
		t.Fatalf("unknown observation fabricated positions: %+v", ledger)
	}
}

// A scoped rescan keeps the fixed partial conclusion even when preparation
// stops before its first position. The record does not invent a ledger or
// open input; history, disk reload and export read the same partial state.
// An ordinary full scan still starts unknown.
func TestScopedPrecheckFailurePersistsPartialCoverage(t *testing.T) {
	paths := newTestPaths(t)
	sourceID := "src-partial"
	saveRescanSource(t, paths, sourceID, record.ClientWindowsLocal,
		json.RawMessage(`{"kind":"zzzInventory","mode":"traverse"}`),
		record.CoveragePartial, record.SortVerificationUnverified,
		[]record.PositionConclusion{{Position: 4, Stage: "scroll", ReasonCode: "positionLost"}})
	scopedSpec := ScanSpec{
		Game: "zzz", Entity: "driveDisc", Client: record.ClientWindowsLocal, Locale: "zh-CN",
		Scope:      scopedScope(sourceID, 4),
		Controller: prepareFailController{},
	}
	svc, _ := newTestService(t, paths, &rescanSource{spec: scopedSpec}, func() (Recognizer, error) { return newFakeRecognizer(), nil })
	outcome, err := svc.Start(StartRequest{
		ID:     "rescan-precheck-failure",
		Rescan: &RescanRequest{SourceRecordID: sourceID, Positions: []int{4}},
	})
	if err != nil {
		t.Fatalf("start rescan: %v", err)
	}
	waitWrapped(t, svc)
	snap := svc.Snapshot().Scan
	if snap.InputOpened {
		t.Fatal("a preparation failure opened input")
	}
	newID := string(outcome.SessionID)
	rec, err := svc.LoadRecord(newID)
	if err != nil {
		t.Fatalf("load rescan record: %v", err)
	}
	shell := rec.Checkpoint.Session
	if shell.Coverage.Status != record.CoveragePartial {
		t.Fatalf("precheck-failure rescan coverage = %q, want partial", shell.Coverage.Status)
	}
	if shell.Termination == nil || shell.Termination.Phase != record.PhasePreparing {
		t.Fatalf("termination = %+v, want a preparing-stage end", shell.Termination)
	}
	if shell.CapturedCount != 0 || len(rec.Checkpoint.Items) != 0 {
		t.Fatalf("precheck failure fabricated items: %+v", rec.Checkpoint.Items)
	}
	if ledger, known := shell.PositionConclusions(); known {
		t.Fatalf("precheck failure fabricated position outcomes: %+v", ledger)
	}

	store, err := record.Open(paths.Data)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	latest, err := store.LoadLatest(context.Background(), newID)
	if err != nil {
		t.Fatalf("disk reload: %v", err)
	}
	if latest.Checkpoint.Session.Coverage.Status != record.CoveragePartial {
		t.Fatalf("disk coverage = %q, want partial", latest.Checkpoint.Session.Coverage.Status)
	}
	history, err := svc.HistoryRecords()
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	var entry *HistoryRecord
	for i := range history {
		if history[i].ID == newID {
			entry = &history[i]
			break
		}
	}
	if entry == nil || entry.Coverage != record.CoveragePartial {
		t.Fatalf("history entry = %+v, want partial coverage", entry)
	}
	usable, err := BuildUsableExport(rec)
	if err != nil {
		t.Fatalf("export precheck-failure rescan: %v", err)
	}
	if usable.session.Coverage.Status != record.CoveragePartial ||
		!reflect.DeepEqual(usable.session.Scope.Slots, []int{4}) {
		t.Fatalf("export = coverage %q slots %v", usable.session.Coverage.Status, usable.session.Scope.Slots)
	}

	fullSpec := ScanSpec{
		Game: "zzz", Entity: "driveDisc", Client: record.ClientWindowsLocal, Locale: "zh-CN",
		Scope:      json.RawMessage(`{"kind":"zzzInventory","mode":"traverse"}`),
		Controller: prepareFailController{},
	}
	fullSvc, _ := newTestService(t, newTestPaths(t), staticSource{spec: fullSpec}, func() (Recognizer, error) { return newFakeRecognizer(), nil })
	fullOutcome, err := fullSvc.Start(StartRequest{ID: "full-precheck-failure"})
	if err != nil {
		t.Fatalf("start full scan: %v", err)
	}
	waitWrapped(t, fullSvc)
	fullRec, err := fullSvc.LoadRecord(string(fullOutcome.SessionID))
	if err != nil {
		t.Fatalf("load full record: %v", err)
	}
	if fullRec.Checkpoint.Session.Coverage.Status != record.CoverageUnknown {
		t.Fatalf("full precheck-failure coverage = %q, want unknown", fullRec.Checkpoint.Session.Coverage.Status)
	}
}

func TestGameScanSourceRescanDeclaresTheRange(t *testing.T) {
	source := &GameScanSource{
		Port:   &platformfake.Port{},
		Target: platform.TargetSpec{ExecutablePath: `C:\game.exe`},
		Client: record.ClientWindowsLocal,
		Mode:   "traverse",
		TraversalForClient: func(image.Point) (collect.Traversal, error) {
			return &scopedReportingTraversal{}, nil
		},
	}
	spec, err := source.NewScanForRescan(nil, RescanRequest{SourceRecordID: "src", Positions: []int{4}})
	if err != nil {
		t.Fatalf("NewScanForRescan: %v", err)
	}
	if err := spec.validate(); err != nil {
		t.Fatalf("scoped spec: %v", err)
	}
	plan, ok := DeclaredScanPlan(spec.Scope)
	if !ok || plan.RescanOf != "src" || !reflect.DeepEqual(plan.Positions, []int{4}) {
		t.Fatalf("scope plan = %+v (ok %v)", plan, ok)
	}
	ctrl, ok := spec.Controller.(*collect.Controller)
	if !ok || !reflect.DeepEqual(ctrl.ScanPositions, []int{4}) {
		t.Fatalf("controller positions = %+v (controller %T)", ctrl, spec.Controller)
	}
}

func TestValidateRescanSpecRefusesAFullFallback(t *testing.T) {
	source := record.SessionShell{Game: "zzz", Entity: "driveDisc", Client: record.ClientWindowsLocal}
	req := RescanRequest{SourceRecordID: "src", Positions: []int{4}}
	full := ScanSpec{Game: "zzz", Entity: "driveDisc", Client: record.ClientWindowsLocal,
		Scope: json.RawMessage(`{"kind":"zzzInventory","mode":"traverse"}`)}
	if err := validateRescanSpec(full, req, source); !errors.Is(err, ErrRescanScope) {
		t.Fatalf("full fallback = %v, want ErrRescanScope", err)
	}
	wrong := ScanSpec{Game: "zzz", Entity: "driveDisc", Client: record.ClientWindowsLocal,
		Scope: scopedScope("other", 4)}
	if err := validateRescanSpec(wrong, req, source); !errors.Is(err, ErrRescanScope) {
		t.Fatalf("wrong source = %v, want ErrRescanScope", err)
	}
	wrongPositions := ScanSpec{Game: "zzz", Entity: "driveDisc", Client: record.ClientWindowsLocal,
		Scope: scopedScope("src", 2, 4)}
	if err := validateRescanSpec(wrongPositions, req, source); !errors.Is(err, ErrRescanScope) {
		t.Fatalf("wrong positions = %v, want ErrRescanScope", err)
	}
	wrongClient := ScanSpec{Game: "zzz", Entity: "driveDisc", Client: record.ClientMacOSCloud,
		Scope: scopedScope("src", 4)}
	if err := validateRescanSpec(wrongClient, req, source); !errors.Is(err, ErrRescanScope) {
		t.Fatalf("wrong client = %v, want ErrRescanScope", err)
	}
}

type blockingLoadLatestStore struct {
	recordStore
	started      chan struct{}
	release      chan struct{}
	once         sync.Once
	honorContext bool
}

func (s *blockingLoadLatestStore) LoadLatest(ctx context.Context, id string) (*record.Recovery, error) {
	s.once.Do(func() { close(s.started) })
	if s.honorContext {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.release:
		}
		return s.recordStore.LoadLatest(ctx, id)
	}
	// Model one native filesystem read that has already entered the OS:
	// cancellation cannot interrupt it, so the caller must reject after it
	// returns.
	<-s.release
	return s.recordStore.LoadLatest(context.Background(), id)
}

func TestRescanSourceValidationCancelsOnStopAndQuit(t *testing.T) {
	cases := []struct {
		name         string
		honorContext bool
		quit         bool
	}{
		{name: "stop cancels a context-aware read", honorContext: true},
		{name: "stop rejects an uninterruptible read", honorContext: false},
		{name: "quit rejects an uninterruptible read", honorContext: false, quit: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			paths := newTestPaths(t)
			const sourceID = "src-built-in"
			saveRescanSource(t, paths, sourceID, record.ClientWindowsLocal,
				json.RawMessage(`{"kind":"zzzInventory","mode":"traverse"}`),
				record.CoveragePartial, record.SortVerificationUnverified,
				[]record.PositionConclusion{{Position: 4, Stage: "scroll", ReasonCode: "positionLost"}})
			spec := ScanSpec{
				Game: "zzz", Entity: "driveDisc", Client: record.ClientWindowsLocal, Locale: "zh-CN",
				Scope:      scopedScope(sourceID, 4),
				Controller: prepareFailController{},
			}
			source := &rescanSource{spec: spec}
			svc, _ := newTestService(t, paths, source,
				func() (Recognizer, error) { return newFakeRecognizer(), nil },
				func() error { return nil })
			realStore := svc.store
			blocked := &blockingLoadLatestStore{
				recordStore:  realStore,
				started:      make(chan struct{}),
				release:      make(chan struct{}),
				honorContext: tc.honorContext,
			}
			svc.store = blocked

			started := make(chan error, 1)
			go func() {
				_, err := svc.Start(StartRequest{
					ID:     "rescan-cancel",
					Rescan: &RescanRequest{SourceRecordID: sourceID, Positions: []int{4}},
				})
				started <- err
			}()
			select {
			case <-blocked.started:
			case <-time.After(5 * time.Second):
				t.Fatal("source validation did not start")
			}

			var quitDone chan error
			if tc.quit {
				quitDone = make(chan error, 1)
				go func() { quitDone <- svc.Quit() }()
				waitFor(t, time.Second, "quit admission", func() bool {
					return svc.Snapshot().StartBlocked == StartBlockedQuitting
				})
			} else {
				svc.Stop()
			}
			close(blocked.release)

			select {
			case err := <-started:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("Start = %v, want context.Canceled", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("Start did not return after cancellation")
			}
			if tc.quit {
				select {
				case err := <-quitDone:
					if err != nil {
						t.Fatalf("Quit = %v", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("Quit did not join the cancelled start")
				}
			}
			if len(source.requests) != 0 {
				t.Fatalf("cancelled validation entered the adapter: %+v", source.requests)
			}
			records, err := realStore.ListRecords()
			if err != nil {
				t.Fatalf("list records: %v", err)
			}
			if len(records) != 1 || records[0].ID != sourceID {
				t.Fatalf("cancelled start persisted a record: %+v", records)
			}
			if svc.Snapshot().Busy {
				t.Fatal("a cancelled rescan left the active slot occupied")
			}
		})
	}
}

func hashRecordCheckpoint(t *testing.T, paths Paths, id string) [32]byte {
	t.Helper()
	store, err := record.Open(paths.Data)
	if err != nil {
		t.Fatalf("open record store: %v", err)
	}
	rec, err := store.Load(id)
	if err != nil {
		t.Fatalf("load record %s: %v", id, err)
	}
	path := filepath.Join(paths.Data, "sessions", id, "checkpoints", fmt.Sprintf("%d.json", rec.Checkpoint.SavedRevision))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read checkpoint: %v", err)
	}
	return sha256.Sum256(data)
}

package app

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/game/zzz"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
)

// reviewData is a complete structured data object that passes the
// current validation rules (slot 4 with a crit-rate main stat, two legal
// substats, a confirmed management state and an unequipped state).
func reviewData() zzz.DiscData {
	setID, rarity, slot, level := 31100, "S", 4, 12
	mainAttribute, mainValue, mainUnit := "critRate", "19.2", "percent"
	subAttribute1, subValue1 := "critDamage", "19.2"
	subAttribute2, subValue2, subUnit2 := "penetration", "9", "flat"
	roll1, roll2 := 3, 0
	management := "none"
	return zzz.DiscData{
		SetID:  &setID,
		Rarity: &rarity,
		Slot:   &slot,
		Level:  &level,
		MainStat: &zzz.StatData{
			Attribute: &mainAttribute,
			Value:     &mainValue,
			Unit:      &mainUnit,
		},
		Substats: []zzz.SubstatData{
			{
				StatData:         zzz.StatData{Attribute: &subAttribute1, Value: &subValue1, Unit: &mainUnit},
				EnhancementRolls: &roll1,
			},
			{
				StatData:         zzz.StatData{Attribute: &subAttribute2, Value: &subValue2, Unit: &subUnit2},
				EnhancementRolls: &roll2,
			},
		},
		ManagementState: &management,
		Equipped:        &zzz.Equipped{State: zzz.EquippedStateUnequipped},
	}
}

// reviewItem builds one produced item whose payload carries the structured
// data plus the raw recognized lines the review flow must preserve.
func reviewItem(t *testing.T, sequence int64, data zzz.DiscData) record.ItemRecord {
	t.Helper()
	structured := zzz.StructuredDisc{
		Data:       data,
		Issues:     []zzz.Issue{},
		Validation: zzz.Validation{Status: zzz.ValidationUnavailable, Issues: []zzz.Issue{}},
	}
	payload, err := json.Marshal(map[string]any{
		"lines":      []RecognizedLine{{Role: "name", Text: "河豚电音[4]", Score: 0.99}},
		"structured": structured,
	})
	if err != nil {
		t.Fatalf("marshal review payload: %v", err)
	}
	return record.ItemRecord{
		Sequence: sequence,
		Status:   record.StatusProduced,
		Quality:  record.QualityNeedsReview,
		Data:     payload,
	}
}

// reviewStore creates one record with the items and opens the service
// over it.
func reviewService(t *testing.T, store recordStore, items ...record.ItemRecord) (*Service, string) {
	t.Helper()
	paths := newTestPaths(t)
	if store == nil {
		opened, err := record.Open(paths.Data)
		if err != nil {
			t.Fatalf("record.Open: %v", err)
		}
		store = opened
	}
	id, err := record.NewRecordID()
	if err != nil {
		t.Fatalf("NewRecordID: %v", err)
	}
	if err := store.CreateRecord(id); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	cp := record.Checkpoint{
		Session: record.SessionShell{
			SessionID: id,
			Game:      "zzz",
			Entity:    "driveDisc",
			Client:    record.ClientWindowsLocal,
			Locale:    "zh-CN",
			Scope:     json.RawMessage(`{"rarity":"S"}`),
			StartedAt: time.Now().UTC(),
			Coverage:  record.Coverage{Status: record.CoverageUnknown},
		},
		Items: items,
	}
	cp.Session.CapturedCount = int64(len(items))
	if _, err := store.SaveCheckpoint(id, cp); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}
	svc := newServiceWithStore(t, paths, store, nil, func() (Recognizer, error) { return newFakeRecognizer(), nil })
	return svc, id
}

// reviewPayload decodes one saved item's payload.
func reviewPayload(t *testing.T, data json.RawMessage) RecognitionResult {
	t.Helper()
	var res RecognitionResult
	if err := json.Unmarshal(data, &res); err != nil {
		t.Fatalf("decode payload: %v\n%s", err, data)
	}
	return res
}

// loadedItem loads one item from the service's record.
func loadedItem(t *testing.T, svc *Service, id string, sequence int64) record.ItemRecord {
	t.Helper()
	rec, err := svc.LoadRecord(id)
	if err != nil {
		t.Fatalf("LoadRecord: %v", err)
	}
	for _, item := range rec.Checkpoint.Items {
		if item.Sequence == sequence {
			return item
		}
	}
	t.Fatalf("item %d not found", sequence)
	return record.ItemRecord{}
}

func TestCheckDiscDataPassesCompleteData(t *testing.T) {
	data := reviewData()
	check := CheckDiscData(data)
	if len(check.Blocking) != 0 || len(check.Gaps) != 0 {
		t.Fatalf("complete data reported issues: %+v", check)
	}
	if check.Validation.Status != zzz.ValidationPassed || !check.Confirmable() {
		t.Fatalf("validation = %+v, want passed", check.Validation)
	}
}

func TestCheckDiscDataSeparatesGapsFromInvalidValues(t *testing.T) {
	data := reviewData()
	data.Level = nil
	data.ManagementState = nil
	check := CheckDiscData(data)
	if len(check.Blocking) != 0 {
		t.Fatalf("unread fields blocked saving: %+v", check.Blocking)
	}
	if len(check.Gaps) != 2 {
		t.Fatalf("gaps = %+v, want level and management state", check.Gaps)
	}
	if check.Validation.Status != zzz.ValidationUnavailable || check.Confirmable() {
		t.Fatalf("validation = %+v, want unavailable", check.Validation)
	}

	badSet, badValue := 999999, "9.60"
	data = reviewData()
	data.SetID = &badSet
	data.MainStat.Value = &badValue
	check = CheckDiscData(data)
	if len(check.Blocking) < 2 {
		t.Fatalf("blocking = %+v, want the unknown set and the non-canonical value", check.Blocking)
	}
	if check.Validation.Status != zzz.ValidationFailed || check.Confirmable() {
		t.Fatalf("validation = %+v, want failed", check.Validation)
	}
}

func TestCheckDiscDataReusesSlotMainRules(t *testing.T) {
	data := reviewData()
	// Slot 1 may only carry a flat health main stat; critRate+percent is
	// rejected by the detail parser's slot/main rule.
	check := CheckDiscData(data)
	if len(check.Blocking) != 0 {
		t.Fatalf("slot 4 crit rate should be legal: %+v", check.Blocking)
	}
	slot := 1
	data.Slot = &slot
	check = CheckDiscData(data)
	if len(check.Blocking) == 0 || check.Validation.Status != zzz.ValidationFailed {
		t.Fatalf("slot 1 crit rate = %+v, want a blocking main-stat issue", check)
	}
}

func TestSaveItemRevisionPersistsEditAndNotifies(t *testing.T) {
	svc, id := reviewService(t, nil, reviewItem(t, 1, reviewData()))
	sub := svc.Subscribe()
	defer sub.Close()

	edited := reviewData()
	level := 13
	edited.Level = &level
	agent := 1311
	edited.Equipped = &zzz.Equipped{State: zzz.EquippedStateEquipped, AgentID: &agent}

	result, err := svc.SaveItemRevision(id, 1, 0, ItemRevisionEdit{Data: edited})
	if err != nil {
		t.Fatalf("SaveItemRevision: %v", err)
	}
	if result.Item.Revision != 1 || result.Item.Quality != record.QualityNeedsReview {
		t.Fatalf("saved item = revision %d quality %q, want 1/needsReview",
			result.Item.Revision, result.Item.Quality)
	}
	if len(result.Item.Revisions) != 1 || len(result.Item.Revisions[0].Changes) == 0 {
		t.Fatalf("revision history = %+v, want the changed fields", result.Item.Revisions)
	}
	// The raw recognized lines survive; the structured data and its
	// validation are the edited revision's.
	res := reviewPayload(t, result.Item.Data)
	if len(res.Lines) != 1 || res.Lines[0].Text != "河豚电音[4]" {
		t.Fatalf("raw lines were lost: %+v", res.Lines)
	}
	if res.Structured == nil || res.Structured.Data.Level == nil || *res.Structured.Data.Level != 13 {
		t.Fatalf("structured data = %+v, want level 13", res.Structured)
	}
	if res.Structured.Validation.Status != zzz.ValidationPassed {
		t.Fatalf("validation = %+v, want passed", res.Structured.Validation)
	}

	// A fresh load sees the revision.
	loaded := loadedItem(t, svc, id, 1)
	if loaded.Revision != 1 || loaded.Quality != record.QualityNeedsReview {
		t.Fatalf("loaded item = revision %d quality %q", loaded.Revision, loaded.Quality)
	}

	select {
	case note := <-sub.Changes:
		if note.Revision <= sub.Initial.Revision {
			t.Fatalf("notification revision %d did not advance from %d", note.Revision, sub.Initial.Revision)
		}
	default:
		t.Fatal("a successful revision did not notify subscribers")
	}
}

func TestSaveItemRevisionConfirmationAndInvalidation(t *testing.T) {
	svc, id := reviewService(t, nil, reviewItem(t, 1, reviewData()))

	result, err := svc.SaveItemRevision(id, 1, 0, ItemRevisionEdit{Data: reviewData(), Confirmed: true})
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if result.Item.Quality != record.QualityHumanConfirmed || result.Item.Revision != 1 {
		t.Fatalf("confirmed item = revision %d quality %q", result.Item.Revision, result.Item.Quality)
	}

	// Editing the confirmed item without confirming again invalidates the
	// old confirmation: revision 2 must need review.
	edited := reviewData()
	level := 11
	edited.Level = &level
	result, err = svc.SaveItemRevision(id, 1, 1, ItemRevisionEdit{Data: edited})
	if err != nil {
		t.Fatalf("edit after confirmation: %v", err)
	}
	if result.Item.Revision != 2 || result.Item.Quality != record.QualityNeedsReview {
		t.Fatalf("item after re-edit = revision %d quality %q, want 2/needsReview",
			result.Item.Revision, result.Item.Quality)
	}
	loaded := loadedItem(t, svc, id, 1)
	if loaded.Quality != record.QualityNeedsReview || len(loaded.Revisions) != 2 || loaded.Revisions[1].Confirmed {
		t.Fatalf("loaded history = %+v", loaded.Revisions)
	}
}

func TestSaveItemRevisionAllowsIncompleteSaveButNotConfirmation(t *testing.T) {
	svc, id := reviewService(t, nil, reviewItem(t, 1, reviewData()))
	incomplete := reviewData()
	incomplete.Level = nil
	incomplete.Equipped = nil

	if _, err := svc.SaveItemRevision(id, 1, 0, ItemRevisionEdit{Data: incomplete, Confirmed: true}); !errors.Is(err, ErrRevisionInvalid) {
		t.Fatalf("confirming incomplete data = %v, want ErrRevisionInvalid", err)
	}
	if got := loadedItem(t, svc, id, 1); got.Revision != 0 {
		t.Fatalf("a refused confirmation wrote revision %d", got.Revision)
	}

	result, err := svc.SaveItemRevision(id, 1, 0, ItemRevisionEdit{Data: incomplete})
	if err != nil {
		t.Fatalf("saving incomplete data: %v", err)
	}
	if result.Item.Quality != record.QualityNeedsReview {
		t.Fatalf("quality = %q, want needsReview", result.Item.Quality)
	}
	res := reviewPayload(t, result.Item.Data)
	if res.Structured == nil || res.Structured.Validation.Status != zzz.ValidationUnavailable {
		t.Fatalf("validation = %+v, want unavailable (never a fabricated passed)", res.Structured.Validation)
	}
	var sawError, sawEquippedWarning bool
	for _, issue := range res.Structured.Validation.Issues {
		switch {
		case issue.Severity == zzz.SeverityError:
			sawError = true
		case issue.Field == "equipped" && issue.Severity == zzz.SeverityWarning:
			sawEquippedWarning = true
		}
	}
	if !sawError || !sawEquippedWarning {
		t.Fatalf("validation issues = %+v, want an error and an equipped warning",
			res.Structured.Validation.Issues)
	}
}

func TestSaveItemRevisionRejectsInvalidValues(t *testing.T) {
	svc, id := reviewService(t, nil, reviewItem(t, 1, reviewData()))
	data := reviewData()
	unit := "bogus"
	data.Substats[1].Unit = &unit

	_, err := svc.SaveItemRevision(id, 1, 0, ItemRevisionEdit{Data: data})
	if !errors.Is(err, ErrRevisionInvalid) {
		t.Fatalf("invalid unit = %v, want ErrRevisionInvalid", err)
	}
	if got := loadedItem(t, svc, id, 1); got.Revision != 0 || string(got.Data) == "" {
		t.Fatalf("an invalid edit reached the store: revision %d", got.Revision)
	}
}

func TestSaveItemRevisionRefusesActiveRecordUntilScanWrapped(t *testing.T) {
	svc, id := reviewService(t, nil, reviewItem(t, 1, reviewData()))

	svc.mu.Lock()
	svc.active = &activeSession{id: scan.ID(id)}
	svc.mu.Unlock()

	_, err := svc.SaveItemRevision(id, 1, 0, ItemRevisionEdit{Data: reviewData()})
	if !errors.Is(err, ErrRecordActive) {
		t.Fatalf("editing an active record = %v, want ErrRecordActive", err)
	}
	if got := loadedItem(t, svc, id, 1); got.Revision != 0 {
		t.Fatalf("the refused edit wrote revision %d", got.Revision)
	}

	svc.mu.Lock()
	svc.active.wrapped = true
	svc.mu.Unlock()
	if _, err := svc.SaveItemRevision(id, 1, 0, ItemRevisionEdit{Data: reviewData()}); err != nil {
		t.Fatalf("editing after the scan wrapped: %v", err)
	}
}

func TestSaveItemRevisionAllowsAnotherRecordWhileScanActive(t *testing.T) {
	svc, id := reviewService(t, nil, reviewItem(t, 1, reviewData()))
	svc.mu.Lock()
	svc.active = &activeSession{id: scan.ID("00000000-0000-4000-8000-000000000000")}
	svc.mu.Unlock()

	if _, err := svc.SaveItemRevision(id, 1, 0, ItemRevisionEdit{Data: reviewData()}); err != nil {
		t.Fatalf("editing an unrelated record during a scan: %v", err)
	}
}

func TestSaveItemRevisionConflictDoesNotOverwrite(t *testing.T) {
	svc, id := reviewService(t, nil, reviewItem(t, 1, reviewData()))
	if _, err := svc.SaveItemRevision(id, 1, 0, ItemRevisionEdit{Data: reviewData()}); err != nil {
		t.Fatalf("first edit: %v", err)
	}
	edited := reviewData()
	level := 3
	edited.Level = &level
	_, err := svc.SaveItemRevision(id, 1, 0, ItemRevisionEdit{Data: edited})
	if !errors.Is(err, record.ErrRevisionConflict) {
		t.Fatalf("stale edit = %v, want record.ErrRevisionConflict", err)
	}
	got := loadedItem(t, svc, id, 1)
	if got.Revision != 1 {
		t.Fatalf("conflict changed the revision: %d", got.Revision)
	}
	res := reviewPayload(t, got.Data)
	if res.Structured.Data.Level == nil || *res.Structured.Data.Level != 12 {
		t.Fatalf("conflict overwrote the stored level: %+v", res.Structured.Data.Level)
	}
	if !strings.Contains(string(got.Data), `"lines"`) {
		t.Fatalf("payload lost its raw lines: %s", got.Data)
	}
}

// failingRevisionStore injects a SaveRevision failure over a real store.
type failingRevisionStore struct {
	*record.Store
	err error
}

func (f *failingRevisionStore) SaveRevision(recordID string, sequence, expectedRevision int64, edit record.ItemEdit) (record.ItemRecord, int64, error) {
	return record.ItemRecord{}, 0, f.err
}

func TestSaveItemRevisionFailureKeepsPreviousRevision(t *testing.T) {
	paths := newTestPaths(t)
	store, err := record.Open(paths.Data)
	if err != nil {
		t.Fatalf("record.Open: %v", err)
	}
	failing := &failingRevisionStore{Store: store, err: errors.New("disk full")}
	svc, id := reviewService(t, failing, reviewItem(t, 1, reviewData()))

	_, err = svc.SaveItemRevision(id, 1, 0, ItemRevisionEdit{Data: reviewData()})
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("save failure = %v", err)
	}
	got := loadedItem(t, svc, id, 1)
	if got.Revision != 0 || got.Quality != record.QualityNeedsReview {
		t.Fatalf("failed save changed the item: revision %d quality %q", got.Revision, got.Quality)
	}
	// The same expected revision still applies once the store heals.
	failing.err = nil
	if _, err := svc.SaveItemRevision(id, 1, 0, ItemRevisionEdit{Data: reviewData()}); err != nil {
		t.Fatalf("retry after a failed save: %v", err)
	}
}

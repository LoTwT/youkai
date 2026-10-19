package app

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/game/zzz"
	"github.com/LoTwT/youkai/internal/record"
)

func intPtr(v int) *int { return &v }

func exportTestTime() time.Time {
	return time.Date(2026, 10, 20, 9, 30, 0, 123_000_000, time.UTC)
}

func exportTestShell(itemCount int) record.SessionShell {
	ended := time.Date(2026, 10, 20, 8, 2, 0, 0, time.UTC)
	return record.SessionShell{
		SessionID:     "d95a372b-3726-46c9-a73f-1210c369145a",
		Game:          "zzz",
		Entity:        "driveDisc",
		Client:        record.ClientMacOSCloud,
		Locale:        "zh-CN",
		Scope:         []byte(`{"kind":"zzzInventory","mode":"traverse"}`),
		ObservedScope: []byte(`{"kind":"zzzInventory","sort":"unverified","filterAtStart":"clear"}`),
		StartedAt:     time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC),
		EndedAt:       &ended,
		Termination:   &record.Termination{Kind: record.TerminationCompleted, Code: "verifiedEnd", Phase: record.PhaseCapturing},
		Coverage:      record.Coverage{Status: record.CoverageUnknown},
		CapturedCount: int64(itemCount),
	}
}

func exportTestRecovery(items ...record.ItemRecord) *record.Recovery {
	return &record.Recovery{Checkpoint: record.Checkpoint{
		Format:        record.FormatName,
		FormatVersion: record.FormatVersion,
		SavedRevision: 4,
		Session:       exportTestShell(len(items)),
		Items:         items,
	}}
}

func testDisc(setID, slot int, mainAttribute, mainValue string, mainUnit zzz.Unit) zzz.DiscRecord {
	level := 15
	return zzz.DiscRecord{
		SetID: &setID,
		Slot:  slot,
		Level: &level,
		MainStat: zzz.Stat{
			Attribute: zzz.Attribute(mainAttribute),
			Value:     mainValue,
			Unit:      mainUnit,
		},
		Substats: []zzz.Substat{
			{Stat: zzz.Stat{Attribute: "critDamage", Value: "19.2", Unit: zzz.UnitPercent}, EnhancementRolls: intPtr(3)},
			{Stat: zzz.Stat{Attribute: "attack", Value: "38", Unit: zzz.UnitFlat}, EnhancementRolls: intPtr(1)},
			{Stat: zzz.Stat{Attribute: "health", Value: "112", Unit: zzz.UnitFlat}, EnhancementRolls: intPtr(0)},
			{Stat: zzz.Stat{Attribute: "anomalyProficiency", Value: "9", Unit: zzz.UnitFlat}, EnhancementRolls: intPtr(0)},
		},
	}
}

func exportStructuredItem(t *testing.T, seq int64, quality record.Quality, disc zzz.DiscRecord, state zzz.CaptureState) record.ItemRecord {
	t.Helper()
	return exportItemWithStructured(t, seq, record.StatusProduced, quality, zzz.ToStructuredWithState(disc, zzz.RarityS, state, nil))
}

func exportItemWithStructured(t *testing.T, seq int64, status record.Status, quality record.Quality, structured zzz.StructuredDisc) record.ItemRecord {
	t.Helper()
	raw, err := json.Marshal(structured)
	if err != nil {
		t.Fatalf("marshal structured item: %v", err)
	}
	payload, err := json.Marshal(map[string]json.RawMessage{"lines": json.RawMessage("[]"), "structured": raw})
	if err != nil {
		t.Fatalf("marshal item payload: %v", err)
	}
	return record.ItemRecord{
		Sequence: seq,
		Status:   status,
		Quality:  quality,
		Revision: 0,
		Data:     payload,
	}
}

// exportFixtureRecord covers every acceptance class: two usable items, a
// pending review, a failed validation, a persisted error issue, an
// unevaluated item, a failed observation, an unprocessed observation, an
// incomplete state and a further excluded set/agent.
func exportFixtureRecord(t *testing.T) *record.Recovery {
	t.Helper()
	none := zzz.CaptureState{ManagementState: zzz.ManagementNone, EquippedState: zzz.EquippedStateUnequipped}
	locked := zzz.CaptureState{ManagementState: zzz.ManagementLocked, EquippedState: zzz.EquippedStateEquipped, AgentID: 1311}
	discarded := zzz.CaptureState{ManagementState: zzz.ManagementDiscarded, EquippedState: zzz.EquippedStateUnequipped}

	items := []record.ItemRecord{
		exportStructuredItem(t, 1, record.QualityAutoAccepted,
			testDisc(31100, 4, "critRate", "24", zzz.UnitPercent), none),
		exportStructuredItem(t, 2, record.QualityHumanConfirmed,
			testDisc(31000, 6, "impact", "18", zzz.UnitPercent), locked),
		exportStructuredItem(t, 3, record.QualityNeedsReview,
			testDisc(31200, 1, "health", "2200", zzz.UnitFlat), discarded),
		exportStructuredItem(t, 4, record.QualityAutoAccepted,
			testDisc(31300, 2, "attack", "abc", zzz.UnitFlat), none),
	}

	// A persisted recognition error keeps an otherwise passing item out,
	// even though the current revalidation would pass.
	withIssue := zzz.ToStructuredWithState(testDisc(31400, 3, "defense", "46", zzz.UnitPercent), zzz.RarityS, none, nil)
	withIssue.Issues = []zzz.Issue{{Field: "mainStat", Code: "rawMismatch", Severity: zzz.SeverityError, Message: "原始识别不一致"}}
	items = append(items, exportItemWithStructured(t, 5, record.StatusProduced, record.QualityHumanConfirmed, withIssue))

	items = append(items,
		exportStructuredItem(t, 6, record.QualityNotEvaluated,
			testDisc(31500, 4, "health", "2200", zzz.UnitFlat), none),
	)

	failed := record.ItemRecord{Sequence: 7, Status: record.StatusFailed, Failure: "识别失败", Quality: record.QualityNotEvaluated}
	unprocessed := record.ItemRecord{Sequence: 8, Status: record.StatusUnprocessed, Reason: record.ReasonCancelled, Quality: record.QualityNotEvaluated}
	items = append(items, failed, unprocessed)

	// Incomplete management state: validation stays unavailable.
	items = append(items, exportStructuredItem(t, 9, record.QualityAutoAccepted,
		testDisc(31600, 5, "attack", "19.2", zzz.UnitPercent), zzz.CaptureState{EquippedState: zzz.EquippedStateUnequipped}))

	// Another set and agent appear only on an excluded item and must not
	// enter the catalog subset.
	items = append(items, exportStructuredItem(t, 10, record.QualityNeedsReview,
		testDisc(31800, 2, "health", "2200", zzz.UnitFlat),
		zzz.CaptureState{ManagementState: zzz.ManagementNone, EquippedState: zzz.EquippedStateEquipped, AgentID: 1011}))

	return exportTestRecovery(items...)
}

func TestBuildUsableExportFiltersAndFreezesCatalog(t *testing.T) {
	export, err := BuildUsableExport(exportFixtureRecord(t))
	if err != nil {
		t.Fatalf("BuildUsableExport: %v", err)
	}
	if export.Summary.Captured != 10 || export.Summary.Included != 2 || export.Summary.Excluded != 8 {
		t.Fatalf("summary = %+v, want captured 10 included 2 excluded 8", export.Summary)
	}
	if export.Summary.Coverage != record.CoverageUnknown {
		t.Fatalf("coverage = %q, want unknown", export.Summary.Coverage)
	}
	if len(export.items) != 2 || export.items[0].Sequence != 1 || export.items[1].Sequence != 2 {
		t.Fatalf("items = %+v, want sequences 1 and 2", export.items)
	}
	if export.items[0].Quality != record.QualityAutoAccepted || export.items[1].Quality != record.QualityHumanConfirmed {
		t.Fatalf("qualities = %q/%q", export.items[0].Quality, export.items[1].Quality)
	}
	if export.items[0].Validation.Status != zzz.ValidationPassed ||
		export.items[1].Validation.Status != zzz.ValidationPassed {
		t.Fatalf("validation = %+v", []zzz.Validation{export.items[0].Validation, export.items[1].Validation})
	}
	if len(export.items[0].Issues) != 0 || len(export.items[0].Validation.Issues) != 0 {
		t.Fatalf("usable item carries issues: %+v", export.items[0])
	}
	wantSets := map[string]string{"31100": "河豚电音", "31000": "啄木鸟电音"}
	if !reflect.DeepEqual(export.catalog.Sets, wantSets) {
		t.Fatalf("catalog sets = %v, want %v", export.catalog.Sets, wantSets)
	}
	wantAgents := map[string]string{"1311": "耀嘉音"}
	if !reflect.DeepEqual(export.catalog.Agents, wantAgents) {
		t.Fatalf("catalog agents = %v, want %v", export.catalog.Agents, wantAgents)
	}
}

func TestJSONExportMatchesContract(t *testing.T) {
	export, err := BuildUsableExport(exportFixtureRecord(t))
	if err != nil {
		t.Fatalf("BuildUsableExport: %v", err)
	}
	data, err := export.JSON(exportTestTime(), "test-1")
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	assertKeys(t, "document", doc, "format", "formatVersion", "exportedAt", "producer", "session",
		"catalog", "fieldPolicy", "validationRules", "excludedCount", "items")

	if got := rawString(t, doc["format"]); got != "youkai.scan" {
		t.Fatalf("format = %q", got)
	}
	if got := rawInt(t, doc["formatVersion"]); got != 1 {
		t.Fatalf("formatVersion = %d", got)
	}
	if got := rawString(t, doc["exportedAt"]); got != "2026-10-20T09:30:00.123Z" {
		t.Fatalf("exportedAt = %q", got)
	}
	if bytes.Contains(data, []byte("evidence")) {
		t.Fatalf("export contains evidence: %s", data)
	}
	if bytes.Contains(data, []byte("inventorySetup")) || bytes.Contains(data, []byte(`"traversal"`)) {
		t.Fatalf("export invented traversal accounting: %s", data)
	}

	var producer struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	decodeRaw(t, doc["producer"], &producer)
	if producer.Name != "youkai" || producer.Version != "test-1" {
		t.Fatalf("producer = %+v", producer)
	}

	var session map[string]json.RawMessage
	decodeRaw(t, doc["session"], &session)
	assertKeys(t, "session", session, "id", "savedRevision", "game", "entity", "client", "locale",
		"startedAt", "endedAt", "termination", "scope", "coverage", "capturedCount")
	if got := rawInt(t, session["savedRevision"]); got != 4 {
		t.Fatalf("savedRevision = %d", got)
	}
	if got := rawString(t, session["endedAt"]); got == "" {
		t.Fatal("endedAt is null for a finished session")
	}
	if got := rawString(t, session["client"]); got != "macosCloud" {
		t.Fatalf("client = %q", got)
	}
	var termination record.Termination
	decodeRaw(t, session["termination"], &termination)
	if termination != (record.Termination{Kind: record.TerminationCompleted, Code: "verifiedEnd", Phase: record.PhaseCapturing}) {
		t.Fatalf("termination = %+v", termination)
	}
	var scope map[string]json.RawMessage
	decodeRaw(t, session["scope"], &scope)
	assertKeys(t, "scope", scope, "rarity", "slots", "minLevel", "maxLevel", "sets", "locked", "discarded", "equipped")
	if got := rawString(t, scope["rarity"]); got != "S" {
		t.Fatalf("scope rarity = %q", got)
	}
	var coverage record.Coverage
	decodeRaw(t, session["coverage"], &coverage)
	if coverage.Status != record.CoverageUnknown || coverage.ExpectedCount != nil {
		t.Fatalf("coverage = %+v", coverage)
	}
	if got := rawInt(t, session["capturedCount"]); got != 10 {
		t.Fatalf("capturedCount = %d", got)
	}

	var catalog exportCatalog
	decodeRaw(t, doc["catalog"], &catalog)
	if catalog.Namespace != "youkai.zzz" || catalog.Revision != zzz.CatalogRevision {
		t.Fatalf("catalog identity = %q/%q", catalog.Namespace, catalog.Revision)
	}
	if !reflect.DeepEqual(catalog.Sets, map[string]string{"31100": "河豚电音", "31000": "啄木鸟电音"}) {
		t.Fatalf("catalog sets = %v", catalog.Sets)
	}
	if !reflect.DeepEqual(catalog.Agents, map[string]string{"1311": "耀嘉音"}) {
		t.Fatalf("catalog agents = %v", catalog.Agents)
	}

	var policy exportFieldPolicy
	decodeRaw(t, doc["fieldPolicy"], &policy)
	if policy.Management != "required" || policy.Equipped != "required" {
		t.Fatalf("fieldPolicy = %+v", policy)
	}
	var rules exportValidationRules
	decodeRaw(t, doc["validationRules"], &rules)
	if rules.ID != "zzzDriveDisc" || rules.Version != zzz.CatalogRevision {
		t.Fatalf("validationRules = %+v", rules)
	}

	if got := rawInt(t, doc["excludedCount"]); got != 8 {
		t.Fatalf("excludedCount = %d", got)
	}
	var items []ExportItem
	decodeRaw(t, doc["items"], &items)
	if len(items) != 2 {
		t.Fatalf("items = %d", len(items))
	}
	first := items[0]
	if first.Sequence != 1 || first.Revision != 0 || first.Quality != record.QualityAutoAccepted {
		t.Fatalf("first item = %+v", first)
	}
	if first.Data.SetID == nil || *first.Data.SetID != 31100 || first.Data.Slot == nil || *first.Data.Slot != 4 {
		t.Fatalf("first data identity = %+v", first.Data)
	}
	if first.Data.MainStat == nil || first.Data.MainStat.Value == nil || *first.Data.MainStat.Value != "24" ||
		first.Data.MainStat.Unit == nil || *first.Data.MainStat.Unit != "percent" {
		t.Fatalf("first main stat = %+v", first.Data.MainStat)
	}
	if len(first.Data.Substats) != 4 || first.Data.Substats[0].EnhancementRolls == nil ||
		*first.Data.Substats[0].EnhancementRolls != 3 {
		t.Fatalf("first substats = %+v", first.Data.Substats)
	}
	if first.Data.ManagementState == nil || *first.Data.ManagementState != "none" {
		t.Fatalf("first managementState = %v", first.Data.ManagementState)
	}
	if first.Data.Equipped == nil || first.Data.Equipped.State != "unequipped" || first.Data.Equipped.AgentID != nil {
		t.Fatalf("first equipped = %+v", first.Data.Equipped)
	}
	second := items[1]
	if second.Data.Equipped == nil || second.Data.Equipped.AgentID == nil || *second.Data.Equipped.AgentID != 1311 {
		t.Fatalf("second equipped = %+v", second.Data.Equipped)
	}
}

func TestCSVExportIsSameSourceAndReadable(t *testing.T) {
	export, err := BuildUsableExport(exportFixtureRecord(t))
	if err != nil {
		t.Fatalf("BuildUsableExport: %v", err)
	}
	data, err := export.CSV()
	if err != nil {
		t.Fatalf("CSV: %v", err)
	}
	if !bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatal("CSV has no UTF-8 BOM")
	}
	rows, err := csv.NewReader(bytes.NewReader(data[3:])).ReadAll()
	if err != nil {
		t.Fatalf("read CSV: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("CSV rows = %d, want header + 2", len(rows))
	}
	wantHeader := []string{
		"sequence", "sessionId", "revision", "quality", "validationStatus",
		"setId", "setName", "rarity", "slot", "level",
		"mainStatAttribute", "mainStatValue", "mainStatUnit",
		"sub1Attribute", "sub1Value", "sub1Unit", "sub1Rolls",
		"sub2Attribute", "sub2Value", "sub2Unit", "sub2Rolls",
		"sub3Attribute", "sub3Value", "sub3Unit", "sub3Rolls",
		"sub4Attribute", "sub4Value", "sub4Unit", "sub4Rolls",
		"managementState", "equippedState", "equippedAgentId", "agentName",
	}
	if !reflect.DeepEqual(rows[0], wantHeader) {
		t.Fatalf("header = %v", rows[0])
	}
	if len(rows[1]) != len(wantHeader) {
		t.Fatalf("row width = %d, want %d", len(rows[1]), len(wantHeader))
	}
	first := rows[1]
	if first[0] != "1" || first[1] != "d95a372b-3726-46c9-a73f-1210c369145a" ||
		first[2] != "0" || first[3] != "autoAccepted" || first[4] != "passed" {
		t.Fatalf("first row identity = %v", first[:5])
	}
	if first[5] != "31100" || first[6] != "河豚电音" || first[7] != "S" || first[8] != "4" || first[9] != "15" {
		t.Fatalf("first row set/slot = %v", first[5:10])
	}
	if first[10] != "critRate" || first[11] != "24" || first[12] != "percent" {
		t.Fatalf("first main stat = %v", first[10:13])
	}
	if first[13] != "critDamage" || first[14] != "19.2" || first[15] != "percent" || first[16] != "3" {
		t.Fatalf("first sub1 = %v", first[13:17])
	}
	if first[17] != "attack" || first[18] != "38" || first[19] != "flat" || first[20] != "1" {
		t.Fatalf("first sub2 = %v", first[17:21])
	}
	if strings.Contains(strings.Join(first, ","), "19.2%") {
		t.Fatal("CSV turned a percent value into a display string")
	}
	if first[29] != "none" || first[30] != "unequipped" || first[31] != "" || first[32] != "" {
		t.Fatalf("first row state = %v", first[29:33])
	}
	second := rows[2]
	if second[5] != "31000" || second[6] != "啄木鸟电音" || second[30] != "equipped" ||
		second[31] != "1311" || second[32] != "耀嘉音" {
		t.Fatalf("second row = %v", second[5:])
	}
	if second[3] != "humanConfirmed" || second[4] != "passed" {
		t.Fatalf("second row quality = %v", second[2:5])
	}
}

func TestJSONAndCSVShareOneFrozenRevision(t *testing.T) {
	rec := exportFixtureRecord(t)
	export, err := BuildUsableExport(rec)
	if err != nil {
		t.Fatalf("BuildUsableExport: %v", err)
	}
	// A later checkpoint change must not leak into the frozen snapshot.
	rec.Checkpoint.Items[0].Quality = record.QualityNeedsReview

	jsonData, err := export.JSON(exportTestTime(), "test-1")
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	csvData, err := export.CSV()
	if err != nil {
		t.Fatalf("CSV: %v", err)
	}
	var doc struct {
		Items []ExportItem `json:"items"`
	}
	if err := json.Unmarshal(jsonData, &doc); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	rows, err := csv.NewReader(bytes.NewReader(csvData[3:])).ReadAll()
	if err != nil {
		t.Fatalf("read CSV: %v", err)
	}
	if len(doc.Items) != len(rows)-1 {
		t.Fatalf("JSON has %d items, CSV has %d", len(doc.Items), len(rows)-1)
	}
	for i, item := range doc.Items {
		if rows[i+1][0] != jsonIntText(item.Sequence) {
			t.Fatalf("row %d sequence = %s, want %d", i+1, rows[i+1][0], item.Sequence)
		}
	}
}

func TestEmptyExportUsesArraysAndHeaderOnly(t *testing.T) {
	rec := exportTestRecovery()
	export, err := BuildUsableExport(rec)
	if err != nil {
		t.Fatalf("BuildUsableExport: %v", err)
	}
	if export.Summary.Excluded != 0 {
		t.Fatalf("excluded = %d on an empty record", export.Summary.Excluded)
	}
	jsonData, err := export.JSON(exportTestTime(), "test-1")
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	var doc struct {
		Catalog struct {
			Sets   map[string]string `json:"sets"`
			Agents map[string]string `json:"agents"`
		} `json:"catalog"`
		Items []ExportItem `json:"items"`
	}
	if err := json.Unmarshal(jsonData, &doc); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if doc.Items == nil || len(doc.Items) != 0 {
		t.Fatalf("items = %v, want []", doc.Items)
	}
	if doc.Catalog.Sets == nil || doc.Catalog.Agents == nil {
		t.Fatalf("catalog maps = %v/%v, want {}", doc.Catalog.Sets, doc.Catalog.Agents)
	}
	if !bytes.Contains(jsonData, []byte(`"items": []`)) {
		t.Fatalf("items is not an empty array: %s", jsonData)
	}
	csvData, err := export.CSV()
	if err != nil {
		t.Fatalf("CSV: %v", err)
	}
	rows, err := csv.NewReader(bytes.NewReader(csvData[3:])).ReadAll()
	if err != nil {
		t.Fatalf("read CSV: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("empty CSV has %d rows, want header only", len(rows))
	}
}

func TestCoverageNeverClaimsCompleteWithoutVerifiedSort(t *testing.T) {
	cases := []struct {
		name     string
		status   record.CoverageStatus
		observed string
		want     record.CoverageStatus
	}{
		{"complete unverified sort", record.CoverageComplete, `{"sort":"unverified"}`, record.CoveragePartial},
		{"complete missing scope", record.CoverageComplete, "", record.CoveragePartial},
		{"complete verified sort", record.CoverageComplete, `{"sort":"verified"}`, record.CoverageComplete},
		{"partial stays partial", record.CoveragePartial, `{"sort":"unverified"}`, record.CoveragePartial},
		{"unknown stays unknown", record.CoverageUnknown, `{"sort":"verified"}`, record.CoverageUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := exportTestRecovery()
			rec.Checkpoint.Session.Coverage.Status = tc.status
			rec.Checkpoint.Session.ObservedScope = []byte(tc.observed)
			export, err := BuildUsableExport(rec)
			if err != nil {
				t.Fatalf("BuildUsableExport: %v", err)
			}
			if export.Summary.Coverage != tc.want {
				t.Fatalf("coverage = %q, want %q", export.Summary.Coverage, tc.want)
			}
		})
	}
}

func TestInterruptedSessionTerminationIsExplicit(t *testing.T) {
	rec := exportTestRecovery(exportStructuredItem(t, 1, record.QualityNeedsReview,
		testDisc(31100, 1, "health", "2200", zzz.UnitFlat),
		zzz.CaptureState{ManagementState: zzz.ManagementNone, EquippedState: zzz.EquippedStateUnequipped}))
	rec.Checkpoint.Session.Termination = nil
	rec.Checkpoint.Session.EndedAt = nil
	export, err := BuildUsableExport(rec)
	if err != nil {
		t.Fatalf("BuildUsableExport: %v", err)
	}
	if export.session.Termination != (record.Termination{
		Kind: record.TerminationInterrupted, Code: "interrupted", Phase: record.PhaseCapturing,
	}) {
		t.Fatalf("termination = %+v", export.session.Termination)
	}
	if export.session.EndedAt != nil {
		t.Fatalf("endedAt = %v, want null", *export.session.EndedAt)
	}

	empty := exportTestRecovery()
	empty.Checkpoint.Session.Termination = nil
	export, err = BuildUsableExport(empty)
	if err != nil {
		t.Fatalf("BuildUsableExport: %v", err)
	}
	if export.session.Termination.Phase != record.PhasePreparing {
		t.Fatalf("empty interrupted phase = %q", export.session.Termination.Phase)
	}
}

func TestDefaultExportName(t *testing.T) {
	shell := exportTestShell(0)
	shell.SessionID = "0816be6a-3d25-4389-bf84-05ac9f0a0693"
	date := shell.StartedAt.Local().Format("2006-01-02")
	if got, want := DefaultExportName(shell, ExportJSON), "youkai-"+date+"-0816be6a.youkai.json"; got != want {
		t.Fatalf("json name = %q, want %q", got, want)
	}
	if got, want := DefaultExportName(shell, ExportCSV), "youkai-"+date+"-0816be6a.youkai.csv"; got != want {
		t.Fatalf("csv name = %q, want %q", got, want)
	}
}

func assertKeys(t *testing.T, what string, doc map[string]json.RawMessage, want ...string) {
	t.Helper()
	if len(doc) != len(want) {
		t.Fatalf("%s keys = %v, want %v", what, sortedKeys(doc), want)
	}
	for _, key := range want {
		if _, ok := doc[key]; !ok {
			t.Fatalf("%s is missing key %q", what, key)
		}
	}
}

func sortedKeys(doc map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(doc))
	for key := range doc {
		keys = append(keys, key)
	}
	return keys
}

func decodeRaw(t *testing.T, raw json.RawMessage, out any) {
	t.Helper()
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
}

func rawString(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var value string
	decodeRaw(t, raw, &value)
	return value
}

func rawInt(t *testing.T, raw json.RawMessage) int64 {
	t.Helper()
	var value int64
	decodeRaw(t, raw, &value)
	return value
}

func jsonIntText(value int64) string { return strconv.FormatInt(value, 10) }

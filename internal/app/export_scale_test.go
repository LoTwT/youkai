package app

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/game/zzz"
	"github.com/LoTwT/youkai/internal/record"
)

// scaleExportPayload builds one reusable structured payload for the scale
// fixtures. The same bytes back many items so 10,000 records stay cheap to
// prepare; BuildUsableExport still parses and revalidates every item.
func scaleExportPayload(t *testing.T, disc zzz.DiscRecord, state zzz.CaptureState) []byte {
	t.Helper()
	structured := zzz.ToStructuredWithState(disc, zzz.RarityS, state, nil)
	if structured.Validation.Status != zzz.ValidationPassed {
		t.Fatalf("scale fixture validation = %q: %+v", structured.Validation.Status, structured.Validation.Issues)
	}
	raw, err := json.Marshal(structured)
	if err != nil {
		t.Fatalf("marshal scale structured payload: %v", err)
	}
	payload, err := json.Marshal(map[string]json.RawMessage{
		"lines":      json.RawMessage("[]"),
		"structured": raw,
	})
	if err != nil {
		t.Fatalf("marshal scale item payload: %v", err)
	}
	return payload
}

// scaleExportItems builds n items: the first excluded rows are needs-review
// records that the export must count but omit, and the rest are usable.
// Usable items alternate two sets; every twentieth is equipped with a known
// agent so the catalog subset has both kinds of entry.
func scaleExportItems(t *testing.T, n, excluded int) []record.ItemRecord {
	t.Helper()
	none := zzz.CaptureState{ManagementState: zzz.ManagementNone, EquippedState: zzz.EquippedStateUnequipped}
	equipped := zzz.CaptureState{ManagementState: zzz.ManagementNone, EquippedState: zzz.EquippedStateEquipped, AgentID: 1311}
	plain := scaleExportPayload(t, testDisc(31100, 4, "critRate", "24", zzz.UnitPercent), none)
	equippedPayload := scaleExportPayload(t, testDisc(31000, 6, "impact", "18", zzz.UnitPercent), equipped)

	items := make([]record.ItemRecord, n)
	for i := range items {
		seq := int64(i + 1)
		item := record.ItemRecord{
			Sequence: seq,
			Status:   record.StatusProduced,
			Quality:  record.QualityAutoAccepted,
			Data:     plain,
		}
		if i < excluded {
			item.Quality = record.QualityNeedsReview
		} else if i%20 == 0 {
			item.Data = equippedPayload
		}
		items[i] = item
	}
	return items
}

// TestScaleExportThousandUsableItems checks the 1,000 usable items of a
// larger record: the included/excluded accounting, capture order, catalog
// subset and the identical item sequence in both formats.
func TestScaleExportThousandUsableItems(t *testing.T) {
	const excluded = 100
	const n = 1100
	items := scaleExportItems(t, n, excluded)

	start := time.Now()
	export, err := BuildUsableExport(exportTestRecovery(items...))
	if err != nil {
		t.Fatalf("BuildUsableExport: %v", err)
	}
	build := time.Since(start)
	if export.Summary.Captured != n || export.Summary.Included != 1000 || export.Summary.Excluded != excluded {
		t.Fatalf("summary = %+v, want captured %d included 1000 excluded %d", export.Summary, n, excluded)
	}

	start = time.Now()
	jsonData, err := export.JSON(exportTestTime(), "test-1")
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	jsonTime := time.Since(start)
	var doc struct {
		ExcludedCount int `json:"excludedCount"`
		Session       struct {
			SavedRevision int64 `json:"savedRevision"`
			CapturedCount int64 `json:"capturedCount"`
		} `json:"session"`
		Catalog struct {
			Sets   map[string]string `json:"sets"`
			Agents map[string]string `json:"agents"`
		} `json:"catalog"`
		Items []ExportItem `json:"items"`
	}
	if err := json.Unmarshal(jsonData, &doc); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if doc.ExcludedCount != excluded || doc.Session.CapturedCount != n || doc.Session.SavedRevision != 4 {
		t.Fatalf("JSON counts = excluded %d captured %d savedRevision %d",
			doc.ExcludedCount, doc.Session.CapturedCount, doc.Session.SavedRevision)
	}
	if len(doc.Items) != 1000 {
		t.Fatalf("JSON items = %d, want 1000", len(doc.Items))
	}
	for i, item := range doc.Items {
		wantSeq := int64(excluded + 1 + i)
		if item.Sequence != wantSeq {
			t.Fatalf("JSON item %d sequence = %d, want %d", i, item.Sequence, wantSeq)
		}
	}
	wantSets := map[string]string{"31100": "河豚电音", "31000": "啄木鸟电音"}
	if !reflect.DeepEqual(doc.Catalog.Sets, wantSets) {
		t.Fatalf("catalog sets = %v, want %v", doc.Catalog.Sets, wantSets)
	}
	if !reflect.DeepEqual(doc.Catalog.Agents, map[string]string{"1311": "耀嘉音"}) {
		t.Fatalf("catalog agents = %v", doc.Catalog.Agents)
	}

	start = time.Now()
	csvData, err := export.CSV()
	if err != nil {
		t.Fatalf("CSV: %v", err)
	}
	csvTime := time.Since(start)
	rows, err := csv.NewReader(bytes.NewReader(csvData[3:])).ReadAll()
	if err != nil {
		t.Fatalf("read CSV: %v", err)
	}
	if len(rows) != 1001 {
		t.Fatalf("CSV rows = %d, want header + 1000", len(rows))
	}
	if !reflect.DeepEqual(rows[0], csvHeader()) {
		t.Fatalf("CSV header = %v", rows[0])
	}
	for i, item := range doc.Items {
		if rows[i+1][0] != fmt.Sprint(item.Sequence) || rows[i+1][2] != fmt.Sprint(item.Revision) {
			t.Fatalf("CSV row %d = %v, JSON item = %d/%d",
				i+1, rows[i+1][:3], item.Sequence, item.Revision)
		}
	}
	if got := rows[1000][0]; got != "1100" {
		t.Fatalf("CSV last sequence = %s, want 1100", got)
	}
	t.Logf("1000 usable items: build %s, JSON %s (%d bytes), CSV %s (%d bytes)",
		build, jsonTime, len(jsonData), csvTime, len(csvData))
}

// TestScaleExportTenThousandItemsDataLayer checks the 10,000-row export at
// the data layer: every item is included, both formats render in capture
// order, and the generated files stay in memory only.
func TestScaleExportTenThousandItemsDataLayer(t *testing.T) {
	const excluded = 1000
	const n = 10000
	items := scaleExportItems(t, n, excluded)

	start := time.Now()
	export, err := BuildUsableExport(exportTestRecovery(items...))
	if err != nil {
		t.Fatalf("BuildUsableExport: %v", err)
	}
	build := time.Since(start)
	if export.Summary.Captured != n || export.Summary.Included != 9000 || export.Summary.Excluded != excluded {
		t.Fatalf("summary = %+v, want captured %d included 9000 excluded %d", export.Summary, n, excluded)
	}

	start = time.Now()
	jsonData, err := export.JSON(exportTestTime(), "test-1")
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	jsonTime := time.Since(start)
	var doc struct {
		ExcludedCount int          `json:"excludedCount"`
		Items         []ExportItem `json:"items"`
	}
	if err := json.Unmarshal(jsonData, &doc); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if doc.ExcludedCount != excluded || len(doc.Items) != 9000 {
		t.Fatalf("JSON excluded %d items %d, want %d/9000", doc.ExcludedCount, len(doc.Items), excluded)
	}
	if doc.Items[0].Sequence != excluded+1 || doc.Items[8999].Sequence != n {
		t.Fatalf("JSON sequence bounds = %d..%d, want %d..%d",
			doc.Items[0].Sequence, doc.Items[8999].Sequence, excluded+1, n)
	}

	start = time.Now()
	csvData, err := export.CSV()
	if err != nil {
		t.Fatalf("CSV: %v", err)
	}
	csvTime := time.Since(start)
	rows, err := csv.NewReader(bytes.NewReader(csvData[3:])).ReadAll()
	if err != nil {
		t.Fatalf("read CSV: %v", err)
	}
	if len(rows) != 9001 {
		t.Fatalf("CSV rows = %d, want header + 9000", len(rows))
	}
	last := rows[len(rows)-1]
	if last[0] != "10000" || last[2] != "0" {
		t.Fatalf("CSV last row sequence/revision = %v, want 10000/0", last[:3])
	}
	t.Logf("10000 items: build %s, JSON %s (%d bytes), CSV %s (%d bytes)",
		build, jsonTime, len(jsonData), csvTime, len(csvData))
}

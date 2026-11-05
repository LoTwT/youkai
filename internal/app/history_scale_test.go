package app

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/record"
)

// BenchmarkHistoryRecordsFiftyRecordsThousandItems measures the history
// list path the design budgets at under 300 ms on the Mac development
// machine: ListRecords over 50 record directories plus one Peek per record,
// each carrying 1,000 items that are classified here. The setup writes the
// records once; only the listing and classification are timed.
func BenchmarkHistoryRecordsFiftyRecordsThousandItems(b *testing.B) {
	const recordCount = 50
	const itemsPerRecord = 1000
	const excludedPerRecord = 100

	store, err := record.Open(b.TempDir())
	if err != nil {
		b.Fatalf("record.Open: %v", err)
	}
	items := scaleExportItems(b, itemsPerRecord, excludedPerRecord)
	start := time.Date(2026, 11, 5, 8, 0, 0, 0, time.UTC)
	for i := 0; i < recordCount; i++ {
		id := fmt.Sprintf("history-scale-%02d", i)
		if err := store.CreateRecord(id); err != nil {
			b.Fatalf("CreateRecord %s: %v", id, err)
		}
		recordStart := start.Add(time.Duration(i) * time.Hour)
		recordEnd := recordStart.Add(14 * time.Minute)
		cp := record.Checkpoint{
			Session: record.SessionShell{
				SessionID: id,
				Game:      "zzz",
				Entity:    "driveDisc",
				Client:    record.ClientWindowsLocal,
				Locale:    "zh-CN",
				Scope:     json.RawMessage(`{"rarity":"S"}`),
				StartedAt: recordStart,
				EndedAt:   &recordEnd,
				Termination: &record.Termination{
					Kind: record.TerminationCompleted, Code: "verifiedEnd", Phase: record.PhaseCapturing,
				},
				Coverage:      record.Coverage{Status: record.CoverageComplete},
				CapturedCount: itemsPerRecord,
			},
			Items: items,
		}
		if _, err := store.SaveCheckpoint(id, cp); err != nil {
			b.Fatalf("SaveCheckpoint %s: %v", id, err)
		}
	}
	svc := &Service{store: store}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		listed, err := svc.HistoryRecords()
		if err != nil {
			b.Fatalf("HistoryRecords: %v", err)
		}
		if len(listed) != recordCount {
			b.Fatalf("records = %d, want %d", len(listed), recordCount)
		}
		estimate := EstimateScanDuration(listed)
		if estimate.Samples != 3 {
			b.Fatalf("estimate samples = %d, want 3", estimate.Samples)
		}
	}
}

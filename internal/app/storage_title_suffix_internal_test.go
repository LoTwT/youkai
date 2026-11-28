package app

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/LoTwT/youkai/internal/game/zzz"
)

func TestStructuredDiscRepairsTruncatedConfirmedSlot(t *testing.T) {
	for _, tc := range []struct {
		line     string
		position int
		wantSlot bool
	}{
		{"河豚电音[1", 1, true},
		{"河豚电音[1", 0, false},
		{"河豚电音[1", 2, false},
		{"河豚电音[12", 1, false},
		{"河豚电音[", 1, false},
		{"河豚电音[[1", 1, false},
		{"不存在的套装[1", 1, false},
	} {
		lines := detailLines()
		lines[0].Text = tc.line
		got := structuredDisc(Observation{Rarity: zzz.RarityS, Position: tc.position}, lines)
		if got == nil || (got.Data.Slot != nil) != tc.wantSlot {
			t.Fatalf("line %q position %d: got %+v, want slot=%t", tc.line, tc.position, got, tc.wantSlot)
		}
		if tc.wantSlot && (*got.Data.Slot != tc.position || got.Data.SetID == nil || lines[0].Text != tc.line) {
			t.Fatalf("line %q position %d: result did not preserve the raw line and corroborated fields", tc.line, tc.position)
		}
	}
}

func TestStorageTruncatedNameFieldReplay(t *testing.T) {
	path := os.Getenv("YOUKAI_STORAGE_NAME_REPLAY")
	if path == "" {
		t.Skip("Windows private checkpoint")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var ck struct {
		Items []struct {
			Observed struct {
				Capture struct{ Position int }
			}
			Data RecognitionResult
		}
	}
	if err := json.Unmarshal(raw, &ck); err != nil {
		t.Fatal(err)
	}
	restored, stillUnknown, original, changedExisting := 0, 0, 0, 0
	for _, item := range ck.Items {
		position := item.Observed.Capture.Position
		old := item.Data.Structured.Data.Slot
		new := structuredDisc(Observation{Rarity: zzz.RarityS, Position: position}, item.Data.Lines)
		if new == nil {
			t.Fatal("a previously produced item has no structured result")
		}
		switch {
		case old != nil:
			original++
			if new.Data.Slot == nil || *new.Data.Slot != *old {
				changedExisting++
			}
		case new.Data.Slot == nil:
			stillUnknown++
		case *new.Data.Slot != position || new.Data.SetID == nil:
			t.Fatal("repaired record contradicts verified slot or lacks a known set")
		default:
			restored++
		}
	}
	t.Logf("restored=%d unknown=%d originally known=%d changed existing=%d", restored, stillUnknown, original, changedExisting)
	if len(ck.Items) != 1020 || restored != 141 || stillUnknown != 5 || original != 874 || changedExisting != 0 {
		t.Fatal("unexpected frozen checkpoint repair")
	}
}

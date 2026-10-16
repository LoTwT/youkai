package record

import (
	"errors"
	"strings"
	"testing"
)

func TestValidID(t *testing.T) {
	valid := []string{
		"a", "abc", "a1", "0", "detail-12", "a-b-c-d", "zzz",
		"con0", "lpt0", "comfort", // not reserved device names
		strings.Repeat("a", idMaxLen),
	}
	for _, id := range valid {
		if err := checkID("record", id); err != nil {
			t.Errorf("checkID(%q) = %v, want accepted", id, err)
		}
	}
	invalid := []string{
		"",                     // empty
		"-lead", "trail-", "-", // hyphen edges
		"UPPER", "Mixed", // case-insensitive filesystems
		"under_score", "with space", "tab\tx", // other characters
		"a/b", "../escape", "..", ".", // path traversal and parents
		`back\slash`, `..\..\x`, // Windows separators
		"a.b", "a..b", "a:b", "a;b", // separators and drives
		"con", "prn", "aux", "nul", // Windows device names
		"com1", "com9", "lpt1", "lpt9", // Windows ports
		"日本語", "emoji-🙂", // non-ASCII
		strings.Repeat("a", idMaxLen+1), // too long
	}
	for _, id := range invalid {
		if err := checkID("record", id); !errors.Is(err, ErrInvalidID) {
			t.Errorf("checkID(%q) = %v, want ErrInvalidID", id, err)
		}
	}
}

func TestNewRecordID(t *testing.T) {
	seen := make(map[string]struct{})
	for i := 0; i < 100; i++ {
		id, err := NewRecordID()
		if err != nil {
			t.Fatalf("NewRecordID: %v", err)
		}
		if err := checkID("record", id); err != nil {
			t.Fatalf("NewRecordID produced %q: %v", id, err)
		}
		if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
			t.Fatalf("NewRecordID produced %q, want UUID text layout", id)
		}
		// Version 4 in the third group: 4xxx.
		if id[14] != '4' {
			t.Fatalf("NewRecordID produced %q, want version 4", id)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("NewRecordID repeated %q", id)
		}
		seen[id] = struct{}{}
	}
}

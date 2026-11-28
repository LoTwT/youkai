package ui

import (
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
	"testing"
)

func TestDeferLiveRefreshUntilFinalCheckpoint(t *testing.T) {
	t.Setenv("YOUKAI_DEFER_WRAP_LIVE_READ", "1")
	for _, c := range []struct {
		name string
		end  scan.EndKind
		fail bool
	}{
		{"cancelled", scan.EndCancelled, false},
		{"completed", scan.EndCompleted, false},
		{"save failure and retry", scan.EndCompleted, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			svc := &fakeService{loads: map[string]*record.Recovery{"s1": recovery(item(1, record.StatusProduced, "", ""))}}
			m, q := newTestModel(t, Config{Service: svc})
			m.Apply(openCapturing("s1", 1, 1))
			drainAll(t, m, q)
			before := len(svc.loadLog)
			if before != 1 {
				t.Fatalf("capture reloads=%d", before)
			}
			m.Apply(stoppingSession("s1", 2, 2))
			drainAll(t, m, q)
			m.catchUpLive()
			drainAll(t, m, q)
			busy := endedSession("s1", 3, 2, 2)
			busy.Busy = true
			busy.SavePending = true
			if c.fail {
				busy.SaveError = "single injected save failure"
			}
			m.Apply(busy)
			drainAll(t, m, q)
			if len(svc.loadLog) != before || m.liveSavedShown != 1 {
				t.Fatal("wrap started a disk read or advanced the displayed revision")
			}
			if c.fail {
				m.RetrySave()
				drainAll(t, m, q)
				if svc.retries != 1 || len(svc.loadLog) != before {
					t.Fatal("retry started a premature record read")
				}
			}
			svc.loads["s1"] = recovery(item(1, record.StatusProduced, "", ""), item(2, record.StatusProduced, "", ""))
			end := endedSession("s1", 4, 2, 2)
			end.Scan.End = c.end
			m.Apply(end)
			drainAll(t, m, q)
			if len(svc.loadLog) != before+1 || m.liveSavedShown != 2 {
				t.Fatal("final checkpoint did not refresh exactly once")
			}
		})
	}
}

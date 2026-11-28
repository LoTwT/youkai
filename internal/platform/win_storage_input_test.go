package platform

import (
	"image"
	"testing"
	"time"
)

// A wheel must run the same live premise check as the first drag batch.
// Moving the client area after the adapter's entry measurement and before
// the serialized precheck must refuse the wheel with staleGeometry and
// zero inserted events, not post a move plus wheel at stale coordinates.
func TestStorageWheelRemeasuresGeometryBeforeBatch(t *testing.T) {
	w := newWinWorld(t)
	w.adapter.WatchEvery = time.Hour
	session := w.openReady(t)
	defer session.Close()
	calls := 0
	w.fg.mu.Lock()
	w.fg.onForeground = func() {
		calls++
		if calls == 3 {
			w.geo.setOrigin(11, 20)
		}
	}
	w.fg.mu.Unlock()
	got := session.Dispatch(InputOp{Kind: InputScroll, Geometry: 1, At: image.Pt(3, 5), WheelSteps: -1})
	if calls < 3 {
		t.Fatal("the fixture did not move the window during the serialized precheck")
	}
	if got.Status != InputNotDispatched || got.Denial == nil || got.Denial.Code != CodeStaleGeometry {
		t.Fatalf("wheel result = %s (%v), events posted = %d; expected staleGeometry and zero events",
			got.Status, got.Denial, got.EventsPosted)
	}
}

package macos_test

import (
	"sync"
	"testing"
	"time"

	macos "github.com/LoTwT/youkai/internal/platform/macos"
)

// Hold and drag behavior (PR 4a): a click holds its press for the
// configured interval; a drag presses, interpolates moves and releases,
// honoring the step gate between steps; the last post time feeds the
// session-close dwell. Timing is observed through an injected sleep
// recorder, never a real wait.

// sleepRecorder records every sleep the dispatcher takes.
type sleepRecorder struct {
	mu     sync.Mutex
	spans  []time.Duration
	attime func(d time.Duration) // optional hook, called instead of waiting
}

func (s *sleepRecorder) sleep(d time.Duration) {
	s.mu.Lock()
	s.spans = append(s.spans, d)
	hook := s.attime
	s.mu.Unlock()
	if hook != nil {
		hook(d)
	}
}

func (s *sleepRecorder) recorded() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Duration(nil), s.spans...)
}

func holdDispatcher(poster macos.InputPoster, sleep *sleepRecorder) (*macos.InputDispatcher, macos.StreamToken) {
	d := macos.NewInputDispatcher(poster, time.Now)
	d.SetSleep(sleep.sleep)
	if err := d.OpenFor(9); err != nil {
		panic(err)
	}
	d.SetGeometry(4)
	return d, 9
}

func TestClickHoldsPressForConfiguredInterval(t *testing.T) {
	poster := newPosterFake(nil)
	sleep := &sleepRecorder{}
	d, token := holdDispatcher(poster, sleep)

	// Zero hold takes the default.
	res := d.Dispatch(macos.ShortInputRequest{Stream: token, Geometry: 4, Op: macos.InputOperation{
		Kind: macos.OpMouseClick, Position: macos.Point{X: 5, Y: 5}, Button: macos.MouseButtonLeft}})
	if res.Status != macos.DispatchPosted || res.EventsPosted != 2 {
		t.Fatalf("default-hold click: %+v", res)
	}
	if got := sleep.recorded(); len(got) != 1 || got[0] != macos.DefaultHold {
		t.Fatalf("default hold not applied: %v", got)
	}

	// An explicit hold is honored as-is.
	res = d.Dispatch(macos.ShortInputRequest{Stream: token, Geometry: 4, Op: macos.InputOperation{
		Kind: macos.OpMouseClick, Position: macos.Point{X: 5, Y: 5}, Button: macos.MouseButtonLeft, Hold: 25 * time.Millisecond}})
	if res.Status != macos.DispatchPosted {
		t.Fatalf("explicit hold click: %+v", res)
	}
	if got := sleep.recorded(); len(got) != 2 || got[1] != 25*time.Millisecond {
		t.Fatalf("explicit hold not applied: %v", got)
	}

	// Above the bound the click is refused before any event.
	res = d.Dispatch(macos.ShortInputRequest{Stream: token, Geometry: 4, Op: macos.InputOperation{
		Kind: macos.OpMouseClick, Position: macos.Point{X: 5, Y: 5}, Button: macos.MouseButtonLeft, Hold: macos.MaxHold + time.Millisecond}})
	if res.Status != macos.DispatchBlocked || res.BlockedCode != macos.BlockedInvalidOperation || len(poster.posted()) != 4 {
		t.Fatalf("over-bound hold must be refused: %+v", res)
	}
}

func TestDragInterpolatesAndReleasesAtDestination(t *testing.T) {
	poster := newPosterFake(nil)
	sleep := &sleepRecorder{}
	d, token := holdDispatcher(poster, sleep)

	res := d.Dispatch(macos.ShortInputRequest{Stream: token, Geometry: 4, Op: macos.InputOperation{
		Kind:     macos.OpDrag,
		Position: macos.Point{X: 100, Y: 100},
		To:       macos.Point{X: 100, Y: 200},
		Button:   macos.MouseButtonLeft,
		Steps:    4, Duration: 40 * time.Millisecond,
	}})
	if res.Status != macos.DispatchPosted || res.Aborted {
		t.Fatalf("drag result: %+v", res)
	}
	// press + 4 moves + release
	if res.EventsPosted != 6 {
		t.Fatalf("events posted %d, want 6", res.EventsPosted)
	}
	events := poster.posted()
	if events[0] != "button-down" || events[len(events)-1] != "button-up" {
		t.Fatalf("drag must start with a press and end with a release: %v", events)
	}
	if len(sleep.recorded()) != 5 { // one interval before each move and before the release
		t.Fatalf("drag sleeps %v", sleep.recorded())
	}
	for i, span := range sleep.recorded() {
		if span != 10*time.Millisecond { // 40ms over 4 steps
			t.Fatalf("drag step %d slept %v, want 10ms", i, span)
		}
	}
	if held := d.State().Held; len(held) != 0 {
		t.Fatalf("a delivered drag leaves nothing held: %+v", held)
	}
	if d.LastPostAt().IsZero() {
		t.Fatal("the drag posted events, so LastPostAt must be set")
	}

	// Defaults: zero steps take DefaultDragSteps, zero duration
	// DefaultDragDuration.
	res = d.Dispatch(macos.ShortInputRequest{Stream: token, Geometry: 4, Op: macos.InputOperation{
		Kind:     macos.OpDrag,
		Position: macos.Point{X: 0, Y: 0},
		To:       macos.Point{X: 0, Y: 30},
		Button:   macos.MouseButtonLeft,
	}})
	if res.Status != macos.DispatchPosted || res.EventsPosted != macos.DefaultDragSteps+2 {
		t.Fatalf("default drag: %+v (posted %d)", res, res.EventsPosted)
	}
	// Over-bound steps and duration are refused before any event.
	for _, op := range []macos.InputOperation{
		{Kind: macos.OpDrag, Position: macos.Point{X: 0, Y: 0}, To: macos.Point{X: 1, Y: 1}, Steps: macos.MaxDragSteps + 1},
		{Kind: macos.OpDrag, Position: macos.Point{X: 0, Y: 0}, To: macos.Point{X: 1, Y: 1}, Duration: macos.MaxDragDuration + time.Millisecond},
	} {
		res := d.Dispatch(macos.ShortInputRequest{Stream: token, Geometry: 4, Op: op})
		if res.Status != macos.DispatchBlocked || res.BlockedCode != macos.BlockedInvalidOperation {
			t.Fatalf("over-bound drag must be refused: %+v", res)
		}
	}
}

func TestDragAbortsWhenStepGateCloses(t *testing.T) {
	poster := newPosterFake(nil)
	sleep := &sleepRecorder{}
	d, token := holdDispatcher(poster, sleep)

	permit := true
	d.SetStepGate(func() bool { return permit })

	// The gate closes while the second step sleeps.
	var mu sync.Mutex
	step := 0
	sleep.attime = func(time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		step++
		if step == 2 {
			permit = false
		}
	}

	res := d.Dispatch(macos.ShortInputRequest{Stream: token, Geometry: 4, Op: macos.InputOperation{
		Kind:     macos.OpDrag,
		Position: macos.Point{X: 0, Y: 0},
		To:       macos.Point{X: 0, Y: 100},
		Button:   macos.MouseButtonLeft,
		Steps:    5, Duration: 50 * time.Millisecond,
	}})
	if !res.Aborted || res.Status != macos.DispatchPartial {
		t.Fatalf("aborted drag result: %+v", res)
	}
	events := poster.posted()
	// press + one move + the abort release; the drag never reached To.
	if len(events) != 3 || events[0] != "button-down" || events[1] != "move" || events[2] != "button-up" {
		t.Fatalf("abort sequence %v", events)
	}
	if res.EventsPosted != 3 || res.EventsFailed != 0 {
		t.Fatalf("accounting: %+v", res)
	}
	// The aborted press left the ledger: the release was posted.
	if held := d.State().Held; len(held) != 0 {
		t.Fatalf("ledger must be empty after the abort release: %+v", held)
	}

	// A gate that stays open never aborts.
	permit = true
	res = d.Dispatch(macos.ShortInputRequest{Stream: token, Geometry: 4, Op: macos.InputOperation{
		Kind:     macos.OpDrag,
		Position: macos.Point{X: 0, Y: 0},
		To:       macos.Point{X: 0, Y: 100},
		Button:   macos.MouseButtonLeft,
		Steps:    3, Duration: 30 * time.Millisecond,
	}})
	if res.Aborted || res.Status != macos.DispatchPosted {
		t.Fatalf("open-gate drag: %+v", res)
	}
}

func TestLastPostAtTracksSuccessfulPostsOnly(t *testing.T) {
	// Every event creation fails: nothing was posted, LastPostAt stays
	// zero — the session close must not dwell on a session that never
	// delivered an event.
	poster := newPosterFake(map[string]int{"button-down": 99})
	d := macos.NewInputDispatcher(poster, time.Now)
	d.SetSleep(func(time.Duration) {})
	if err := d.OpenFor(3); err != nil {
		t.Fatal(err)
	}
	d.SetGeometry(1)
	before := d.LastPostAt()
	res := d.Dispatch(macos.ShortInputRequest{Stream: 3, Geometry: 1, Op: macos.InputOperation{
		Kind: macos.OpMouseClick, Position: macos.Point{X: 1, Y: 1}}})
	if res.Status != macos.DispatchBlocked || !d.LastPostAt().Equal(before) {
		t.Fatalf("failed posts must not move LastPostAt: %+v / %v", res, d.LastPostAt())
	}
}

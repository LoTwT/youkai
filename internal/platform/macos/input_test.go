package macos_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	macos "github.com/LoTwT/youkai/internal/platform/macos"
)

// Input dispatcher tests (T2 §4.2, §4.3 停止能力的实际边界): permission
// gating, session and geometry-version checks, the readiness hook, the
// posted/partial/blocked result semantics of CGEventPost (which has no
// confirmation return value), the press ledger with separate release
// accounting, and the close-versus-dispatch race.

// posterFake records every posted event and can fail selected ones.
type posterFake struct {
	mu      sync.Mutex
	events  []string
	failOne map[string]int // event name → remaining failures to inject
}

func newPosterFake(fail map[string]int) *posterFake {
	return &posterFake{failOne: fail}
}

func (p *posterFake) post(name string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failOne[name] > 0 {
		p.failOne[name]--
		return errors.New("event creation failed: " + name)
	}
	p.events = append(p.events, name)
	return nil
}

func (p *posterFake) PostMouseMoved(pt macos.Point) error { return p.post("move") }
func (p *posterFake) PostMouseButton(b macos.MouseButton, down bool, at macos.Point) error {
	name := "button-up"
	if down {
		name = "button-down"
	}
	return p.post(name)
}
func (p *posterFake) PostKey(c macos.KeyCode, down bool) error {
	name := "key-up"
	if down {
		name = "key-down"
	}
	return p.post(name)
}
func (p *posterFake) PostScroll(dy float64) error { return p.post("scroll") }

func (p *posterFake) posted() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.events...)
}

func openedDispatcher(poster macos.InputPoster, token macos.StreamToken, geo macos.GeometryVersion) *macos.InputDispatcher {
	t := time.Now()
	d := macos.NewInputDispatcher(poster, func() time.Time { return t })
	if err := d.OpenFor(token); err != nil {
		panic(err)
	}
	d.SetGeometry(geo)
	return d
}

func request(token macos.StreamToken, geo macos.GeometryVersion, op macos.InputOperation) macos.ShortInputRequest {
	return macos.ShortInputRequest{Stream: token, Geometry: geo, Frame: 1, Op: op}
}

func TestDispatchBlockedUntilOpened(t *testing.T) {
	poster := newPosterFake(nil)
	d := macos.NewInputDispatcher(poster, nil)
	res := d.Dispatch(request(1, 1, macos.InputOperation{Kind: macos.OpMouseMoved}))
	if res.Status != macos.DispatchBlocked || res.BlockedCode != macos.BlockedNotOpen {
		t.Fatalf("result %+v", res)
	}
	if len(poster.posted()) != 0 {
		t.Fatal("blocked dispatch posted events")
	}
	if err := d.OpenFor(1); err != nil {
		t.Fatalf("open: %v", err)
	}
}

func TestDispatchPostedForEveryOperationKind(t *testing.T) {
	poster := newPosterFake(nil)
	d := openedDispatcher(poster, 7, 2)
	move := request(7, 2, macos.InputOperation{Kind: macos.OpMouseMoved, Position: macos.Point{X: 100, Y: 200}})
	if res := d.Dispatch(move); res.Status != macos.DispatchPosted || res.EventsPosted != 1 {
		t.Fatalf("move result %+v", res)
	}
	click := request(7, 2, macos.InputOperation{Kind: macos.OpMouseClick, Position: macos.Point{X: 10, Y: 20}, Button: macos.MouseButtonLeft})
	if res := d.Dispatch(click); res.Status != macos.DispatchPosted || res.EventsPosted != 2 {
		t.Fatalf("click result %+v", res)
	}
	key := request(7, 2, macos.InputOperation{Kind: macos.OpKeyPress, KeyCode: 6})
	if res := d.Dispatch(key); res.Status != macos.DispatchPosted || res.EventsPosted != 2 {
		t.Fatalf("key result %+v", res)
	}
	scroll := request(7, 2, macos.InputOperation{Kind: macos.OpScroll, ScrollDeltaY: -120})
	if res := d.Dispatch(scroll); res.Status != macos.DispatchPosted || res.EventsPosted != 1 {
		t.Fatalf("scroll result %+v", res)
	}
	want := []string{"move", "button-down", "button-up", "key-down", "key-up", "scroll"}
	got := poster.posted()
	if len(got) != len(want) {
		t.Fatalf("posted %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("posted %v want %v", got, want)
		}
	}
	// A fully delivered click leaves nothing held.
	if held := d.State().Held; len(held) != 0 {
		t.Fatalf("ledger after delivered ops %+v", held)
	}
}

func TestDispatchPartialWhenSecondEventCreationFails(t *testing.T) {
	// The key-down posts but the key-up creation fails: partial
	// dispatch, the key stays held, and the release accounting is
	// separate from the dispatch result.
	poster := newPosterFake(map[string]int{"key-up": 1})
	d := openedDispatcher(poster, 1, 1)
	res := d.Dispatch(request(1, 1, macos.InputOperation{Kind: macos.OpKeyPress, KeyCode: 6}))
	if res.Status != macos.DispatchPartial || res.EventsPosted != 1 || res.EventsFailed != 1 {
		t.Fatalf("result %+v", res)
	}
	held := d.State().Held
	if len(held) != 1 || held[0].Kind != "key" || held[0].KeyCode != 6 {
		t.Fatalf("ledger %+v", held)
	}
	// A release attempt posts the key-up and clears the ledger; the
	// outcome records what was attempted.
	outcomes := d.ReleaseHeld()
	if len(outcomes) != 1 || !outcomes[0].Released {
		t.Fatalf("outcomes %+v", outcomes)
	}
	held = d.State().Held
	if len(held) != 0 {
		t.Fatalf("ledger after release %+v", held)
	}
}

func TestDispatchBlockedWhenFirstEventCreationFails(t *testing.T) {
	poster := newPosterFake(map[string]int{"button-down": 1})
	d := openedDispatcher(poster, 1, 1)
	res := d.Dispatch(request(1, 1, macos.InputOperation{Kind: macos.OpMouseClick, Position: macos.Point{X: 5, Y: 5}}))
	if res.Status != macos.DispatchBlocked || res.EventsPosted != 0 || res.EventsFailed != 1 {
		t.Fatalf("result %+v", res)
	}
	held := d.State().Held
	if len(held) != 0 {
		t.Fatalf("failed press entered the ledger: %+v", held)
	}
}

func TestDispatchGatesWrongSessionStaleGeometryAndReadiness(t *testing.T) {
	poster := newPosterFake(nil)
	d := openedDispatcher(poster, 7, 2)
	// Wrong session token.
	res := d.Dispatch(request(8, 2, macos.InputOperation{Kind: macos.OpMouseMoved}))
	if res.Status != macos.DispatchBlocked || res.BlockedCode != macos.BlockedWrongSession {
		t.Fatalf("wrong session result %+v", res)
	}
	// Stale geometry version: the dispatcher advanced to 3, the
	// request still carries 2.
	d.SetGeometry(3)
	res = d.Dispatch(request(7, 2, macos.InputOperation{Kind: macos.OpMouseMoved}))
	if res.Status != macos.DispatchBlocked || res.BlockedCode != macos.BlockedStaleGeometry {
		t.Fatalf("stale geometry result %+v", res)
	}
	// Geometry versions only advance; a stale SetGeometry is ignored
	// and must not reopen the previous version.
	d.SetGeometry(2)
	res = d.Dispatch(request(7, 2, macos.InputOperation{Kind: macos.OpMouseMoved}))
	if res.Status != macos.DispatchBlocked || res.BlockedCode != macos.BlockedStaleGeometry {
		t.Fatalf("reverted geometry result %+v", res)
	}
	// Readiness hook denial (the foreground/visibility verdict failed).
	d.SetReadiness(func(macos.ShortInputRequest) error {
		return errors.New("notFrontmost: another application is frontmost")
	})
	res = d.Dispatch(request(7, 3, macos.InputOperation{Kind: macos.OpMouseMoved}))
	if res.Status != macos.DispatchBlocked || res.BlockedCode != macos.BlockedReadiness {
		t.Fatalf("readiness result %+v", res)
	}
	if len(poster.posted()) != 0 {
		t.Fatalf("gated dispatch posted %v", poster.posted())
	}
	// Invalid operations never reach the poster.
	res = d.Dispatch(request(7, 3, macos.InputOperation{Kind: "nonsense"}))
	if res.Status != macos.DispatchBlocked || res.BlockedCode != macos.BlockedInvalidOperation {
		t.Fatalf("invalid op result %+v", res)
	}
}

func TestCloseIsForeverAndReleasesOnlyOwnInputs(t *testing.T) {
	// The key-up keeps failing, so the dispatcher holds one key of its
	// own. Closing must attempt exactly that release and block all
	// further dispatches forever.
	poster := newPosterFake(map[string]int{"key-up": 99})
	d := openedDispatcher(poster, 1, 1)
	d.Dispatch(request(1, 1, macos.InputOperation{Kind: macos.OpKeyPress, KeyCode: 6}))
	held := d.State().Held
	if len(held) != 1 {
		t.Fatalf("ledger before close %+v", held)
	}
	outcomes := d.Close("user stop")
	if len(outcomes) != 1 {
		t.Fatalf("close outcomes %+v", outcomes)
	}
	if outcomes[0].Released || outcomes[0].Err == nil {
		t.Fatalf("failed release misreported: %+v", outcomes[0])
	}
	// The failed release keeps its ledger entry with the attempt
	// recorded, for a later retry.
	state := d.State()
	closed, reason, held := state.Closed, state.Reason, state.Held
	if !closed || reason != "user stop" {
		t.Fatalf("state closed=%v reason=%q", closed, reason)
	}
	if len(held) != 1 || held[0].ReleaseAttempts != 1 || held[0].LastReleaseErr == nil {
		t.Fatalf("ledger after failed release %+v", held)
	}
	// Every further dispatch is blocked with the closed reason.
	for i := 0; i < 3; i++ {
		res := d.Dispatch(request(1, 1, macos.InputOperation{Kind: macos.OpMouseMoved}))
		if res.Status != macos.DispatchBlocked || res.BlockedCode != macos.BlockedClosed {
			t.Fatalf("post-close dispatch %+v", res)
		}
	}
	// A retry of the release posts the key-up when the poster works
	// again and succeeds then.
	poster.mu.Lock()
	poster.failOne = nil
	poster.mu.Unlock()
	outcomes = d.ReleaseHeld()
	if len(outcomes) != 1 || !outcomes[0].Released {
		t.Fatalf("retry outcomes %+v", outcomes)
	}
	// Reopening is impossible after a close.
	if err := d.OpenFor(2); err == nil {
		t.Fatal("closed dispatcher reopened")
	}
	// A second Close is a no-op that releases nothing again.
	if outcomes := d.Close("again"); outcomes != nil {
		t.Fatalf("second close released again: %+v", outcomes)
	}
}

func TestCloseRacesDispatchSafely(t *testing.T) {
	// Close running concurrently with a stream of dispatches: every
	// dispatch is either fully delivered before the close or fully
	// blocked by it; none is half-delivered, and the ledger is empty
	// when the dust settles (every delivered press also delivered its
	// release).
	for round := 0; round < 20; round++ {
		poster := newPosterFake(nil)
		d := openedDispatcher(poster, 1, 1)
		var wg sync.WaitGroup
		done := make(chan struct{})
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					d.Dispatch(request(1, 1, macos.InputOperation{Kind: macos.OpMouseClick, Position: macos.Point{X: 1, Y: 1}}))
				}
			}
		}()
		d.Close("cancel")
		close(done)
		wg.Wait()
		closed, held := d.State().Closed, d.State().Held
		if !closed {
			t.Fatal("dispatcher not closed")
		}
		if len(held) != 0 {
			t.Fatalf("round %d: ledger not drained: %+v", round, held)
		}
		events := poster.posted()
		if len(events)%2 != 0 {
			t.Fatalf("round %d: odd number of events %v", round, events)
		}
		for i := 0; i < len(events); i += 2 {
			if events[i] != "button-down" || events[i+1] != "button-up" {
				t.Fatalf("round %d: interleaved events %v", round, events)
			}
		}
	}
}

func TestDispatchSerializesOperations(t *testing.T) {
	// Concurrent dispatches never interleave their events: every
	// click's down is immediately followed by its own up.
	poster := newPosterFake(nil)
	d := openedDispatcher(poster, 1, 1)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 25; j++ {
				d.Dispatch(request(1, 1, macos.InputOperation{Kind: macos.OpMouseClick, Position: macos.Point{X: 1, Y: 1}}))
			}
		}()
	}
	wg.Wait()
	events := poster.posted()
	if len(events) != 8*25*2 {
		t.Fatalf("event count %d", len(events))
	}
	for i := 0; i < len(events); i += 2 {
		if events[i] != "button-down" || events[i+1] != "button-up" {
			t.Fatalf("interleaved events at %d: %v", i, events)
		}
	}
}

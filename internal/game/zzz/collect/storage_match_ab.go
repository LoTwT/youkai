package collect

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"runtime"
	"time"

	"github.com/LoTwT/youkai/internal/platform"
)

var storageMatchABReuse = os.Getenv("YOUKAI_STORAGE_MATCH_AB") == "B"

// storageCandidateMatch is local to one detail-wait iteration. It contains
// only the pure matcher output. The caller still executes frame gates and
// independently advances freshness, selection and stability state.
type storageCandidateMatch struct {
	matcher    *PixelMatcher
	detailed   PageMatch
	fastBadges bool
}

func (run *inventoryRun) storageCandidate(f platform.Frame) storageCandidateMatch {
	if !storageMatchABReuse || f.Reused || !run.inv.storagePage() {
		return storageCandidateMatch{}
	}
	m := storagePixelMatcher(run.inv.matcher())
	if m == nil || !m.valid || m.L.Page != PageDriveStorage || !m.frameFits(f) {
		return storageCandidateMatch{}
	}
	return storageCandidateMatch{matcher: m}
}

// preClickFrameMatcher uses a private matcher copy for the ordinary
// pre-click list check. The session matcher remains the authority for all
// other stages and the same checked-frame gates apply to both modes.
func (run *inventoryRun) preClickFrameMatcher() func(platform.Frame) (PageMatch, error) {
	base := storagePixelMatcher(run.inv.matcher())
	if !run.inv.storagePage() || base == nil || !base.fastBeforeClick || base.fastCardBadges {
		return run.matchFrame
	}
	fast := *base
	fast.fastCardBadges = true
	return func(f platform.Frame) (PageMatch, error) {
		if f.Reused {
			return run.matchFrame(f)
		}
		sample := run.storageABFrame(f, run.storageABStage())
		var started time.Time
		if sample != nil {
			started = time.Now()
		}
		m := fast.Match(f)
		if sample != nil {
			sample.PublicNS = time.Since(started).Nanoseconds()
			sample.Page = m.Page
			sample.FastBeforeClick = true
		}
		return run.checkMatchedFrame(f, m)
	}
}

func storagePixelMatcher(m Matcher) *PixelMatcher {
	switch m := m.(type) {
	case *PixelMatcher:
		return m
	case *ScrollEvidenceMatcher:
		if m != nil {
			return m.PixelMatcher
		}
	}
	return nil
}

func (c *storageCandidateMatch) match(f platform.Frame) PageMatch {
	m := c.matcher
	if c.fastBadges {
		fast := *m
		fast.fastCardBadges = true
		m = &fast
	}
	c.detailed = m.storageMatchDetailed(f)
	return m.publicStorageMatch(f, c.detailed)
}

func (c storageCandidateMatch) weak() (PageMatch, *weakSelectionEvidence) {
	return weakStorageMatch(c.detailed)
}

// A Windows request waits until After, then captures native pixels.
// The validated median interval was about 99 ms, including about 30 ms
// of native capture. Pace only after the first fresh frame of this click.
const storageDetailCaptureHold = 69 * time.Millisecond

func pacedStorageDetailRequest(after, previousFresh time.Time) platform.FrameRequest {
	if !previousFresh.IsZero() {
		earliest := previousFresh.Add(storageDetailCaptureHold)
		if earliest.After(after) {
			after = earliest
		}
	}
	return platform.FrameRequest{After: after}
}

func (run *inventoryRun) pacedStorageDetailEnabled(preClick platform.Frame) bool {
	if runtime.GOOS != "windows" || !storageMatchABReuse || preClick.ID == 0 ||
		!run.inv.storagePage() || !run.inv.fullScope() {
		return false
	}
	m := storagePixelMatcher(run.inv.matcher())
	return m != nil && m.pacedFastDetail && !m.fastCardBadges
}

type storageABFrameTiming struct {
	PacedFastDetail       bool                     `json:"paced_fast_detail,omitempty"`
	FastBeforeClick       bool                     `json:"fast_before_click,omitempty"`
	ID                    uint64                   `json:"id"`
	Viewport              int                      `json:"viewport"`
	Visit                 int                      `json:"visit"`
	Geometry              platform.GeometryVersion `json:"geometry"`
	Stage                 string                   `json:"stage"`
	ClickFinishedNS       int64                    `json:"click_finished_ns,omitempty"`
	ArrivedNS             int64                    `json:"arrived_ns"`
	DecisionNS            int64                    `json:"decision_ns,omitempty"`
	FrameRequestNS        int64                    `json:"frame_request_ns,omitempty"`
	NativeCaptureNS       int64                    `json:"native_capture_ns,omitempty"`
	Reused                bool                     `json:"reused"`
	FreshByGate           bool                     `json:"fresh_by_gate"`
	PublicNS              int64                    `json:"public_ns"`
	PrivateNS             int64                    `json:"private_ns,omitempty"`
	PrivateCalled         bool                     `json:"private_called,omitempty"`
	PureMatchReused       bool                     `json:"pure_match_reused,omitempty"`
	StrongTarget          bool                     `json:"strong_target,omitempty"`
	AttributableCandidate bool                     `json:"attributable_candidate,omitempty"`
	StableNotDifferent    bool                     `json:"stable_not_different,omitempty"`
	StableAccepted        bool                     `json:"stable_accepted,omitempty"`
	Page                  PageKind                 `json:"page"`
}
type storageABVisitFailure struct {
	Visit           int    `json:"visit"`
	Viewport        int    `json:"viewport"`
	Stage           string `json:"stage"`
	ClickFinishedNS int64  `json:"click_finished_ns,omitempty"`
	Code            string `json:"code"`
	AtNS            int64  `json:"at_ns"`
}
type storageABTiming struct {
	path            string
	started         time.Time
	stage           string
	requestNS       int64
	requestFrameID  uint64
	requestArrived  time.Time
	clickFinishedNS int64
	frames          []storageABFrameTiming
	failures        []storageABVisitFailure
	dropped         int
}

func startStorageABTiming() *storageABTiming {
	path := os.Getenv("YOUKAI_STORAGE_AB_TIMING_FILE")
	if path == "" {
		return nil
	}
	return &storageABTiming{path: path, started: time.Now(), stage: "prepare", frames: make([]storageABFrameTiming, 0, 16384)}
}

func (run *inventoryRun) storageABFrame(f platform.Frame, stage string) *storageABFrameTiming {
	a := run.abTiming
	if a == nil {
		return nil
	}
	if len(a.frames) == cap(a.frames) {
		a.dropped++
		return nil
	}
	var request int64
	if a.requestFrameID == f.ID && a.requestArrived.Equal(f.ArrivedAt) {
		request = a.requestNS
		a.requestNS = 0
	}
	a.frames = append(a.frames, storageABFrameTiming{ID: f.ID, Visit: run.visitIndex + 1, Viewport: run.viewport, Geometry: f.Geometry,
		Stage: stage, ClickFinishedNS: a.clickFinishedNS, ArrivedNS: f.ArrivedAt.Sub(a.started).Nanoseconds(), FrameRequestNS: request, NativeCaptureNS: f.NativeCaptureDuration.Nanoseconds(),
		Reused: f.Reused, FreshByGate: !f.Reused && f.ID > run.newestFrameID && f.ArrivedAt.After(run.lastArrived)})
	return &a.frames[len(a.frames)-1]
}

func (a *storageABTiming) finish(err error) {
	if a == nil {
		return
	}
	ended := time.Now()
	report := struct {
		StartedUTC         time.Time               `json:"started_utc"`
		CollectorElapsedNS int64                   `json:"collector_elapsed_ns"`
		Reuse              bool                    `json:"reuse"`
		Termination        string                  `json:"collector_termination"`
		Dropped            int                     `json:"dropped"`
		Frames             []storageABFrameTiming  `json:"frames"`
		Rejections         []storageABVisitFailure `json:"rejections"`
	}{a.started.UTC(), ended.Sub(a.started).Nanoseconds(), storageMatchABReuse, "completed", a.dropped, a.frames, a.failures}
	if err != nil {
		report.Termination = storageABFailureCode(err)
	}
	data, err := json.Marshal(report)
	if err == nil {
		var f *os.File
		f, err = os.OpenFile(a.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err == nil {
			_, err = f.Write(data)
			if closeErr := f.Close(); err == nil {
				err = closeErr
			}
		}
	}
	log.Printf("youkai: storage-ab timing frames=%d dropped=%d flush_ms=%d ok=%t", len(a.frames), a.dropped, time.Since(ended).Milliseconds(), err == nil)
}

func (run *inventoryRun) storageABStage() string {
	if run.abTiming == nil {
		return ""
	}
	return run.abTiming.stage
}

func (run *inventoryRun) storageABReadFrame(ctx context.Context, req platform.FrameRequest) (platform.Frame, error) {
	a := run.abTiming
	if a == nil {
		return run.view.Frame(ctx, req)
	}
	started := time.Now()
	f, err := run.view.Frame(ctx, req)
	a.requestNS = time.Since(started).Nanoseconds()
	a.requestFrameID = f.ID
	a.requestArrived = f.ArrivedAt
	return f, err
}

func (run *inventoryRun) timedStoragePublicMatch(f platform.Frame, c *storageCandidateMatch, sample *storageABFrameTiming) PageMatch {
	var began time.Time
	if sample != nil {
		began = time.Now()
	}
	var p PageMatch
	if c != nil && c.matcher != nil {
		p = c.match(f)
	} else {
		p = run.inv.matcher().Match(f)
	}
	if sample != nil {
		sample.PublicNS = time.Since(began).Nanoseconds()
		sample.Page = p.Page
	}
	return p
}

func (run *inventoryRun) timedStorageWeakMatch(f platform.Frame, provider weakSelectionEvidenceProvider, c *storageCandidateMatch, sample *storageABFrameTiming) (PageMatch, *weakSelectionEvidence) {
	var began time.Time
	if sample != nil {
		began = time.Now()
	}
	var p PageMatch
	var weak *weakSelectionEvidence
	shared := c != nil && c.matcher != nil && c.matcher == storagePixelMatcher(run.inv.matcher())
	if shared {
		p, weak = c.weak()
	} else {
		p, weak = provider.weakSelectionEvidence(f)
	}
	if sample != nil {
		sample.PrivateNS = time.Since(began).Nanoseconds()
		sample.PrivateCalled = true
		sample.PureMatchReused = shared
	}
	return p, weak
}

func storageABFailureCode(err error) string {
	if code := faultDiagnosticCode(err); code != "" {
		return code
	}
	if errors.Is(err, context.Canceled) {
		return "contextCancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "contextDeadline"
	}
	return "unclassifiedError"
}

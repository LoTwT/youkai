package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"time"
)

// ToolName identifies this probe in every JSON document it writes.
const ToolName = "winprobe"

// BlackScreenMaxMean is the mean 8-bit RGB value (0..255) at or below which a
// captured frame is reported as black. Alpha is excluded: the platform's
// capture path marks every pixel opaque, so alpha carries no image content.
// The variance is recorded next to the mean because a uniform frame (low
// variance) and a frame with a few bright pixels (higher variance) can both be
// black; the mean remains the blackness verdict, the variance is evidence.
const BlackScreenMaxMean = 1.0

// Win32 style bits used to classify the window form. They are declared here,
// without a build constraint, because the classification rule and its tests are
// platform-neutral even though the values come from Win32.
const (
	wsCaption    = 0x00C00000
	wsThickFrame = 0x00040000
	wsPopup      = 0x80000000
	wsExTopmost  = 0x00000008
	wsExLayered  = 0x00080000
)

// RectFacts is a rectangle in screen physical pixels. Left/Top are inclusive
// and Right/Bottom are exclusive, matching Win32 RECT and image.Rectangle.
type RectFacts struct {
	Left   int32 `json:"left"`
	Top    int32 `json:"top"`
	Right  int32 `json:"right"`
	Bottom int32 `json:"bottom"`
	Width  int32 `json:"width"`
	Height int32 `json:"height"`
}

func rectFacts(left, top, right, bottom int32) RectFacts {
	return RectFacts{
		Left:   left,
		Top:    top,
		Right:  right,
		Bottom: bottom,
		Width:  right - left,
		Height: bottom - top,
	}
}

// SizeFacts is a width/height pair in physical pixels.
type SizeFacts struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// RectContains reports whether inner lies inside outer.
func RectContains(outer, inner RectFacts) bool {
	return outer.Left <= inner.Left && outer.Top <= inner.Top &&
		outer.Right >= inner.Right && outer.Bottom >= inner.Bottom
}

// ClassifyWindowForm distinguishes the three window forms the field round must
// tell apart from window styles and the monitor rectangle:
//
//   - "windowed": WS_CAPTION or WS_THICKFRAME is present;
//   - "fullscreen": neither is present and the window rectangle covers its
//     monitor (borderless fullscreen or an exclusive-fullscreen-like window);
//   - "borderless": neither is present and the window is smaller than its
//     monitor;
//   - "unknown": the caller could not read the facts to decide.
//
// The reason string is recorded as evidence, not parsed by any consumer.
func ClassifyWindowForm(caption, thickFrame bool, windowRect, monitorRect RectFacts) (form, reason string) {
	if windowRect.Width <= 0 || windowRect.Height <= 0 || monitorRect.Width <= 0 || monitorRect.Height <= 0 {
		return "unknown", "empty window or monitor rectangle"
	}
	if caption || thickFrame {
		switch {
		case caption && thickFrame:
			return "windowed", "WS_CAPTION and WS_THICKFRAME present"
		case caption:
			return "windowed", "WS_CAPTION present"
		default:
			return "windowed", "WS_THICKFRAME present"
		}
	}
	if RectContains(windowRect, monitorRect) {
		return "fullscreen", "no WS_CAPTION/WS_THICKFRAME; window rectangle covers its monitor"
	}
	return "borderless", "no WS_CAPTION/WS_THICKFRAME; window rectangle is smaller than its monitor"
}

// WindowStyleFacts records the raw style words plus the flags used for form
// classification. Styles are observations: the probe never sets them.
type WindowStyleFacts struct {
	Style      uint32 `json:"style"`
	StyleHex   string `json:"styleHex"`
	ExStyle    uint32 `json:"exStyle"`
	ExStyleHex string `json:"exStyleHex"`
	Caption    bool   `json:"caption"`
	ThickFrame bool   `json:"thickFrame"`
	Popup      bool   `json:"popup"`
	Topmost    bool   `json:"topmost"`
	Layered    bool   `json:"layered"`
	Form       string `json:"form"`
	FormReason string `json:"formReason"`
	Error      string `json:"error,omitempty"`
}

// IntegrityFacts is the TokenIntegrityLevel observation of one process. It is
// read with query-only access (PROCESS_QUERY_LIMITED_INFORMATION +
// TOKEN_QUERY); a failure is recorded here and never worked around by
// elevation, privilege enabling or token changes.
type IntegrityFacts struct {
	Level string `json:"level"`
	RID   uint32 `json:"rid"`
	Error string `json:"error,omitempty"`
}

// DPIFacts records the target window's DPI awareness (name, DPI_AWARENESS
// enum value and opaque context handle), the two DPI values the platform
// measured, and the platform's virtualization verdict.
type DPIFacts struct {
	WindowAwareness        string `json:"windowAwareness"`
	WindowAwarenessValue   int    `json:"windowAwarenessValue"`
	WindowAwarenessContext string `json:"windowAwarenessContext,omitempty"`
	WindowAwarenessError   string `json:"windowAwarenessError,omitempty"`
	WindowDPI              uint32 `json:"windowDPI"`
	MonitorDPI             uint32 `json:"monitorDPI"`
	Verdict                string `json:"verdict"`
	Detail                 string `json:"detail,omitempty"`
}

// MonitorFacts is one display as observed through EnumDisplayMonitors,
// GetMonitorInfoW and GetDpiForMonitor.
type MonitorFacts struct {
	Handle       uint64    `json:"handle"`
	HandleHex    string    `json:"handleHex"`
	Device       string    `json:"device"`
	Primary      bool      `json:"primary"`
	Rect         RectFacts `json:"rect"`
	WorkArea     RectFacts `json:"workArea"`
	DPI          uint32    `json:"dpi"`
	ScalePercent int       `json:"scalePercent"`
	Error        string    `json:"error,omitempty"`
}

// DisplaysFacts is the full monitor list. Error is set when enumeration
// itself failed; per-monitor failures stay on the monitor entry.
type DisplaysFacts struct {
	Monitors []MonitorFacts `json:"monitors"`
	Error    string         `json:"error,omitempty"`
}

// ProbeInfo describes the probe process itself: its DPI awareness (the fact
// that decides whether screen coordinates are virtualized) and its integrity
// level.
type ProbeInfo struct {
	PID               uint32         `json:"pid"`
	Executable        string         `json:"executable,omitempty"`
	DPIAwareness      string         `json:"dpiAwareness"`
	DPIAwarenessValue int            `json:"dpiAwarenessValue"`
	DPIAwarenessError string         `json:"dpiAwarenessError,omitempty"`
	Integrity         IntegrityFacts `json:"integrity"`
}

// WindowFacts is one candidate top-level window of the target executable.
type WindowFacts struct {
	HWND             uint64           `json:"hwnd"`
	HWNDHex          string           `json:"hwndHex"`
	PID              uint32           `json:"pid"`
	Executable       string           `json:"executable"`
	ProcessStartedAt time.Time        `json:"processStartedAt"`
	Title            string           `json:"title"`
	ClassName        string           `json:"className"`
	Visible          bool             `json:"visible"`
	Minimized        bool             `json:"minimized"`
	Maximized        bool             `json:"maximized"`
	Cloaked          bool             `json:"cloaked"`
	Foreground       bool             `json:"foreground"`
	Style            WindowStyleFacts `json:"style"`
	WindowRect       *RectFacts       `json:"windowRect,omitempty"`
	ClientRect       *RectFacts       `json:"clientRect,omitempty"`
	Monitor          *MonitorFacts    `json:"monitor,omitempty"`
	DPI              DPIFacts         `json:"dpi"`
	Integrity        IntegrityFacts   `json:"integrity"`
	Errors           []string         `json:"errors,omitempty"`
}

// SelectionResult records why the probe did or did not lock a target. Status
// is one of listOnly, locked, noWindow, notReady, ambiguous or error.
type SelectionResult struct {
	Status              string `json:"status"`
	Detail              string `json:"detail,omitempty"`
	LockedHWND          uint64 `json:"lockedHWND,omitempty"`
	RemainingCandidates int    `json:"remainingCandidates,omitempty"`
}

// WindowReport is window.json: identification, geometry, displays, DPI and
// integrity evidence. All rectangles are screen physical pixels under the
// probe's per-monitor-v2 awareness.
type WindowReport struct {
	Tool                string          `json:"tool"`
	Mode                string          `json:"mode"`
	StartedAt           time.Time       `json:"startedAt"`
	GeneratedAt         time.Time       `json:"generatedAt"`
	Executable          string          `json:"executable"`
	OutDirectory        string          `json:"outDirectory"`
	TopLevelWindowCount int             `json:"topLevelWindowCount"`
	CandidateCount      int             `json:"candidateCount"`
	Probe               ProbeInfo       `json:"probe"`
	Displays            DisplaysFacts   `json:"displays"`
	Candidates          []WindowFacts   `json:"candidates"`
	Selection           SelectionResult `json:"selection"`
	Target              *WindowFacts    `json:"target,omitempty"`
	Errors              []string        `json:"errors,omitempty"`
	Notes               []string        `json:"notes,omitempty"`
}

// FrameRecord is one capture attempt. CaptureMS is the platform's GDI call
// duration only; FrameMS is the whole per-frame work (observation, capture,
// statistics, PNG write). Timestamp is the capture request time in UTC.
type FrameRecord struct {
	Index             int       `json:"index"`
	File              string    `json:"file"`
	Timestamp         time.Time `json:"timestamp"`
	CaptureMS         float64   `json:"captureMs"`
	FrameMS           float64   `json:"frameMs"`
	Width             int       `json:"width"`
	Height            int       `json:"height"`
	ClientWidth       int       `json:"clientWidth"`
	ClientHeight      int       `json:"clientHeight"`
	SizeMatchesClient bool      `json:"sizeMatchesClient"`
	GeometryVersion   uint64    `json:"geometryVersion"`
	Mean              float64   `json:"mean"`
	Variance          float64   `json:"variance"`
	Black             bool      `json:"black"`
	SHA256            string    `json:"sha256"`
	DuplicateOf       *int      `json:"duplicateOf,omitempty"`
	ConsecutiveRepeat bool      `json:"consecutiveRepeat"`
	Error             string    `json:"error,omitempty"`
}

// DurationStats is the duration distribution the summary reports.
type DurationStats struct {
	Count int     `json:"count"`
	P50MS float64 `json:"p50Ms"`
	P90MS float64 `json:"p90Ms"`
	P99MS float64 `json:"p99Ms"`
	MaxMS float64 `json:"maxMs"`
}

// FramesReport is frames.json: the per-frame record plus aggregates. GDI on
// this path has no reuse/status flag, so none is invented; every frame is a
// fresh BitBlt of the measured client region.
type FramesReport struct {
	Tool               string        `json:"tool"`
	Executable         string        `json:"executable"`
	StartedAt          time.Time     `json:"startedAt"`
	CompletedAt        time.Time     `json:"completedAt"`
	RequestedFrames    int           `json:"requestedFrames"`
	IntervalMS         int           `json:"intervalMs"`
	CapturedFrames     int           `json:"capturedFrames"`
	BlackFrames        int           `json:"blackFrames"`
	SizeMismatches     int           `json:"sizeMismatches"`
	DuplicateFrames    int           `json:"duplicateFrames"`
	ConsecutiveRepeats int           `json:"consecutiveRepeats"`
	ActualFrameRateHz  float64       `json:"actualFrameRateHz"`
	MeanIntervalMS     float64       `json:"meanStartIntervalMs"`
	InitialClientSize  *SizeFacts    `json:"initialClientSize,omitempty"`
	ClientSizes        []SizeFacts   `json:"clientSizes,omitempty"`
	CaptureMS          DurationStats `json:"captureMs"`
	FrameMS            DurationStats `json:"frameMs"`
	Frames             []FrameRecord `json:"frames"`
	Errors             []string      `json:"errors,omitempty"`
	Notes              []string      `json:"notes,omitempty"`
}

// ImageStats is the black-screen and duplicate-frame evidence of one image.
type ImageStats struct {
	Mean     float64
	Variance float64
	SHA256   string
	Black    bool
}

// AnalyzePixels computes the mean and population variance over the 8-bit R, G
// and B samples of a top-down RGBA image, the SHA-256 of its dimensions and
// row bytes, and the black-screen verdict. Alpha is excluded from the mean and
// variance because the capture path always writes 0xFF there; it stays part of
// the row bytes so the hash covers exactly the stored image content. Stride
// padding, if any, is excluded from both.
func AnalyzePixels(width, height, stride int, pixels []byte) (ImageStats, error) {
	if width <= 0 || height <= 0 {
		return ImageStats{}, fmt.Errorf("invalid image size %dx%d", width, height)
	}
	if stride < width*4 {
		return ImageStats{}, fmt.Errorf("stride %d is smaller than %d bytes per row", stride, width*4)
	}
	if len(pixels) < stride*height {
		return ImageStats{}, fmt.Errorf("pixel buffer of %d bytes does not hold %dx%d at stride %d", len(pixels), width, height, stride)
	}
	h := sha256.New()
	fmt.Fprintf(h, "%dx%d\n", width, height)
	var sum, sumSquares float64
	var samples int
	for y := 0; y < height; y++ {
		row := pixels[y*stride : y*stride+width*4]
		_, _ = h.Write(row)
		for i := 0; i < len(row); i += 4 {
			for c := 0; c < 3; c++ {
				v := float64(row[i+c])
				sum += v
				sumSquares += v * v
				samples++
			}
		}
	}
	mean := sum / float64(samples)
	variance := sumSquares/float64(samples) - mean*mean
	if variance < 0 {
		variance = 0
	}
	return ImageStats{
		Mean:     mean,
		Variance: variance,
		SHA256:   hex.EncodeToString(h.Sum(nil)),
		Black:    mean <= BlackScreenMaxMean,
	}, nil
}

// MarkFrameDuplicates fills DuplicateOf and ConsecutiveRepeat on the frames
// and returns the number of frames whose content already appeared earlier and
// the number of frames equal to their immediate predecessor. Frames without a
// hash (failed captures) are neither duplicates nor repeats. Duplicate content
// is not proof of a stale frame: a static game page and a frozen video both
// produce identical hashes.
func MarkFrameDuplicates(frames []FrameRecord) (duplicateCount, consecutiveRepeats int) {
	first := make(map[string]int, len(frames))
	for i := range frames {
		frames[i].DuplicateOf = nil
		frames[i].ConsecutiveRepeat = false
		hash := frames[i].SHA256
		if hash == "" {
			continue
		}
		if firstIndex, ok := first[hash]; ok {
			index := firstIndex
			frames[i].DuplicateOf = &index
			duplicateCount++
			frames[i].ConsecutiveRepeat = i > 0 && frames[i-1].SHA256 == hash
			if frames[i].ConsecutiveRepeat {
				consecutiveRepeats++
			}
			continue
		}
		first[hash] = i
	}
	return duplicateCount, consecutiveRepeats
}

// Percentile returns the p-th percentile (0..100) of values using linear
// interpolation between the closest ranks: position = p/100 * (n-1). It copies
// and sorts the input, so callers may pass values in any order. ok is false
// for an empty slice or an out-of-range percentile.
func Percentile(values []float64, p float64) (value float64, ok bool) {
	if len(values) == 0 || p < 0 || p > 100 || math.IsNaN(p) {
		return 0, false
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	position := p / 100 * float64(len(sorted)-1)
	low := int(math.Floor(position))
	high := int(math.Ceil(position))
	if low == high {
		return sorted[low], true
	}
	fraction := position - float64(low)
	return sorted[low]*(1-fraction) + sorted[high]*fraction, true
}

// SummarizeDurations builds the p50/p90/p99/max distribution from per-frame
// milliseconds. Empty input yields a zero value with Count 0.
func SummarizeDurations(values []float64) DurationStats {
	stats := DurationStats{Count: len(values)}
	if len(values) == 0 {
		return stats
	}
	stats.P50MS, _ = Percentile(values, 50)
	stats.P90MS, _ = Percentile(values, 90)
	stats.P99MS, _ = Percentile(values, 99)
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	stats.MaxMS = sorted[len(sorted)-1]
	return stats
}

// FrameRate returns the observed start-to-start frame rate and the mean start
// interval in milliseconds. Fewer than two timestamps, or a non-positive
// span, yields zeros rather than a fabricated rate.
func FrameRate(starts []time.Time) (fps, meanIntervalMS float64) {
	if len(starts) < 2 {
		return 0, 0
	}
	span := starts[len(starts)-1].Sub(starts[0]).Seconds()
	if span <= 0 {
		return 0, 0
	}
	return float64(len(starts)-1) / span, span / float64(len(starts)-1) * 1000
}

// Finalize recomputes the aggregates of a frames report from its records.
func (f *FramesReport) Finalize() {
	f.DuplicateFrames, f.ConsecutiveRepeats = MarkFrameDuplicates(f.Frames)
	f.CapturedFrames = 0
	f.BlackFrames = 0
	f.SizeMismatches = 0
	f.ClientSizes = nil
	var starts []time.Time
	var captureMS, frameMS []float64
	seenSizes := make(map[SizeFacts]bool)
	for i := range f.Frames {
		frame := &f.Frames[i]
		if frame.Error != "" {
			continue
		}
		f.CapturedFrames++
		if frame.Black {
			f.BlackFrames++
		}
		if !frame.SizeMatchesClient {
			f.SizeMismatches++
		}
		starts = append(starts, frame.Timestamp)
		captureMS = append(captureMS, frame.CaptureMS)
		frameMS = append(frameMS, frame.FrameMS)
		size := SizeFacts{Width: frame.ClientWidth, Height: frame.ClientHeight}
		if !seenSizes[size] {
			seenSizes[size] = true
			f.ClientSizes = append(f.ClientSizes, size)
		}
	}
	f.ActualFrameRateHz, f.MeanIntervalMS = FrameRate(starts)
	f.CaptureMS = SummarizeDurations(captureMS)
	f.FrameMS = SummarizeDurations(frameMS)
	f.InitialClientSize = nil
	if len(f.ClientSizes) > 0 {
		first := f.ClientSizes[0]
		f.InitialClientSize = &first
	}
}

func marshalJSON(v any) ([]byte, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func writeJSON(path string, v any) error {
	data, err := marshalJSON(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func millis(d time.Duration) float64 {
	return math.Round(float64(d)/float64(time.Millisecond)*1000) / 1000
}

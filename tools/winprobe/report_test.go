package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAnalyzePixels(t *testing.T) {
	t.Run("all black", func(t *testing.T) {
		stats, err := AnalyzePixels(2, 2, 8, make([]byte, 16))
		if err != nil {
			t.Fatalf("AnalyzePixels: %v", err)
		}
		if stats.Mean != 0 || stats.Variance != 0 || !stats.Black {
			t.Fatalf("got mean=%v variance=%v black=%v, want 0/0/true", stats.Mean, stats.Variance, stats.Black)
		}
		if len(stats.SHA256) != 64 {
			t.Fatalf("hash length = %d, want 64", len(stats.SHA256))
		}
	})

	t.Run("black threshold is inclusive", func(t *testing.T) {
		black, err := AnalyzePixels(1, 1, 4, []byte{1, 1, 1, 255})
		if err != nil {
			t.Fatalf("AnalyzePixels: %v", err)
		}
		if !black.Black || black.Mean != 1 {
			t.Fatalf("got mean=%v black=%v, want mean=1 black=true", black.Mean, black.Black)
		}
		notBlack, err := AnalyzePixels(1, 1, 4, []byte{2, 2, 2, 255})
		if err != nil {
			t.Fatalf("AnalyzePixels: %v", err)
		}
		if notBlack.Black || notBlack.Mean != 2 {
			t.Fatalf("got mean=%v black=%v, want mean=2 black=false", notBlack.Mean, notBlack.Black)
		}
	})

	t.Run("mean and variance over rgb only", func(t *testing.T) {
		stats, err := AnalyzePixels(1, 1, 4, []byte{10, 20, 30, 0})
		if err != nil {
			t.Fatalf("AnalyzePixels: %v", err)
		}
		if !almostEqual(stats.Mean, 20, 1e-9) {
			t.Fatalf("mean = %v, want 20", stats.Mean)
		}
		if !almostEqual(stats.Variance, 200.0/3.0, 1e-9) {
			t.Fatalf("variance = %v, want %v", stats.Variance, 200.0/3.0)
		}
		if stats.Black {
			t.Fatal("a mid-gray pixel must not be black")
		}
	})

	t.Run("alpha does not change statistics but is hashed", func(t *testing.T) {
		opaque, err := AnalyzePixels(1, 1, 4, []byte{10, 20, 30, 255})
		if err != nil {
			t.Fatalf("AnalyzePixels: %v", err)
		}
		clear, err := AnalyzePixels(1, 1, 4, []byte{10, 20, 30, 0})
		if err != nil {
			t.Fatalf("AnalyzePixels: %v", err)
		}
		if opaque.Mean != clear.Mean || opaque.Variance != clear.Variance {
			t.Fatalf("alpha changed statistics: %+v vs %+v", opaque, clear)
		}
		if opaque.SHA256 == clear.SHA256 {
			t.Fatal("alpha is part of the stored row bytes and must change the hash")
		}
	})

	t.Run("stride padding is excluded", func(t *testing.T) {
		padded, err := AnalyzePixels(1, 1, 8, []byte{1, 2, 3, 255, 9, 9, 9, 9})
		if err != nil {
			t.Fatalf("AnalyzePixels: %v", err)
		}
		plain, err := AnalyzePixels(1, 1, 4, []byte{1, 2, 3, 255})
		if err != nil {
			t.Fatalf("AnalyzePixels: %v", err)
		}
		if padded.SHA256 != plain.SHA256 {
			t.Fatal("padding changed the content hash")
		}
		if !almostEqual(padded.Mean, 2, 1e-9) {
			t.Fatalf("mean = %v, want 2", padded.Mean)
		}
	})

	t.Run("two pixel variance", func(t *testing.T) {
		stats, err := AnalyzePixels(2, 1, 8, []byte{0, 0, 0, 255, 255, 255, 255, 255})
		if err != nil {
			t.Fatalf("AnalyzePixels: %v", err)
		}
		if !almostEqual(stats.Mean, 127.5, 1e-9) || !almostEqual(stats.Variance, 16256.25, 1e-9) {
			t.Fatalf("got mean=%v variance=%v", stats.Mean, stats.Variance)
		}
	})

	t.Run("invalid inputs", func(t *testing.T) {
		cases := []struct {
			name         string
			w, h, stride int
			pixels       []byte
		}{
			{"zero width", 0, 1, 4, make([]byte, 4)},
			{"zero height", 1, 0, 4, make([]byte, 4)},
			{"short stride", 2, 1, 7, make([]byte, 8)},
			{"short buffer", 2, 1, 8, make([]byte, 7)},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if _, err := AnalyzePixels(tc.w, tc.h, tc.stride, tc.pixels); err == nil {
					t.Fatal("expected an error")
				}
			})
		}
	})
}

func TestMarkFrameDuplicates(t *testing.T) {
	frames := []FrameRecord{
		{SHA256: "a"},
		{SHA256: "a"},
		{SHA256: "b"},
		{SHA256: "a"},
		{SHA256: ""},
		{SHA256: "a"},
	}
	duplicates, consecutive := MarkFrameDuplicates(frames)
	if duplicates != 3 || consecutive != 1 {
		t.Fatalf("duplicates=%d consecutive=%d, want 3/1", duplicates, consecutive)
	}
	if frames[1].DuplicateOf == nil || *frames[1].DuplicateOf != 0 {
		t.Fatalf("frame 1 duplicateOf = %v, want 0", frames[1].DuplicateOf)
	}
	if frames[3].DuplicateOf == nil || *frames[3].DuplicateOf != 0 {
		t.Fatalf("frame 3 duplicateOf = %v, want 0", frames[3].DuplicateOf)
	}
	if frames[5].DuplicateOf == nil || *frames[5].DuplicateOf != 0 {
		t.Fatalf("frame 5 duplicateOf = %v, want 0", frames[5].DuplicateOf)
	}
	if frames[2].DuplicateOf != nil || frames[4].DuplicateOf != nil {
		t.Fatal("first occurrences and failed frames must not be marked duplicates")
	}
	if !frames[1].ConsecutiveRepeat || frames[3].ConsecutiveRepeat {
		t.Fatalf("consecutive flags: %+v", frames)
	}

	allSame := []FrameRecord{{SHA256: "x"}, {SHA256: "x"}, {SHA256: "x"}}
	duplicates, consecutive = MarkFrameDuplicates(allSame)
	if duplicates != 2 || consecutive != 2 {
		t.Fatalf("all same: duplicates=%d consecutive=%d, want 2/2", duplicates, consecutive)
	}

	noDuplicates := []FrameRecord{{SHA256: "x"}, {}, {SHA256: "x"}}
	duplicates, consecutive = MarkFrameDuplicates(noDuplicates)
	if duplicates != 1 || consecutive != 0 {
		t.Fatalf("gapped duplicate: duplicates=%d consecutive=%d, want 1/0", duplicates, consecutive)
	}
}

func TestFramesReportFinalize(t *testing.T) {
	start := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	report := &FramesReport{
		Frames: []FrameRecord{
			{
				Index: 0, Timestamp: start, CaptureMS: 2, FrameMS: 3,
				Width: 100, Height: 50, ClientWidth: 100, ClientHeight: 50,
				SizeMatchesClient: true, GeometryVersion: 1, SHA256: "a",
			},
			{
				Index: 1, Timestamp: start.Add(100 * time.Millisecond), CaptureMS: 4, FrameMS: 5,
				Width: 100, Height: 50, ClientWidth: 100, ClientHeight: 50,
				SizeMatchesClient: true, GeometryVersion: 1, SHA256: "a", Black: true,
			},
			{
				Index: 2, Timestamp: start.Add(200 * time.Millisecond), Error: "capture failed",
			},
		},
	}
	report.Finalize()

	if report.CapturedFrames != 2 || report.BlackFrames != 1 || report.SizeMismatches != 0 {
		t.Fatalf("aggregates: %+v", report)
	}
	if report.DuplicateFrames != 1 || report.ConsecutiveRepeats != 1 {
		t.Fatalf("duplicates: %+v", report)
	}
	if !almostEqual(report.ActualFrameRateHz, 10, 1e-9) || !almostEqual(report.MeanIntervalMS, 100, 1e-9) {
		t.Fatalf("rate: %v fps, interval %v ms", report.ActualFrameRateHz, report.MeanIntervalMS)
	}
	if report.InitialClientSize == nil || *report.InitialClientSize != (SizeFacts{Width: 100, Height: 50}) {
		t.Fatalf("initial client size: %+v", report.InitialClientSize)
	}
	if len(report.ClientSizes) != 1 {
		t.Fatalf("client sizes: %+v", report.ClientSizes)
	}
	if report.CaptureMS.Count != 2 || !almostEqual(report.CaptureMS.P50MS, 3, 1e-9) || !almostEqual(report.CaptureMS.MaxMS, 4, 1e-9) {
		t.Fatalf("capture stats: %+v", report.CaptureMS)
	}
	if report.FrameMS.Count != 2 || !almostEqual(report.FrameMS.P50MS, 4, 1e-9) {
		t.Fatalf("frame stats: %+v", report.FrameMS)
	}
}

func TestPercentile(t *testing.T) {
	values := []float64{4, 1, 3, 2}
	cases := []struct {
		p    float64
		want float64
	}{
		{0, 1},
		{25, 1.75},
		{50, 2.5},
		{90, 3.7},
		{100, 4},
	}
	for _, tc := range cases {
		got, ok := Percentile(values, tc.p)
		if !ok || !almostEqual(got, tc.want, 1e-9) {
			t.Fatalf("p%v = %v (ok=%v), want %v", tc.p, got, ok, tc.want)
		}
	}
	if _, ok := Percentile(nil, 50); ok {
		t.Fatal("empty input must not produce a percentile")
	}
	for _, p := range []float64{-1, 101, math.NaN()} {
		if _, ok := Percentile(values, p); ok {
			t.Fatalf("p=%v must be rejected", p)
		}
	}
	got, ok := Percentile([]float64{7}, 37)
	if !ok || got != 7 {
		t.Fatalf("single value = %v (ok=%v), want 7", got, ok)
	}
}

func TestSummarizeDurations(t *testing.T) {
	if stats := SummarizeDurations(nil); stats.Count != 0 || stats.P50MS != 0 || stats.MaxMS != 0 {
		t.Fatalf("empty stats: %+v", stats)
	}
	stats := SummarizeDurations([]float64{10, 20, 30, 40})
	if stats.Count != 4 || !almostEqual(stats.P50MS, 25, 1e-9) || !almostEqual(stats.P90MS, 37, 1e-9) || stats.MaxMS != 40 {
		t.Fatalf("stats: %+v", stats)
	}
}

func TestFrameRate(t *testing.T) {
	start := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	fps, interval := FrameRate([]time.Time{start, start.Add(100 * time.Millisecond), start.Add(200 * time.Millisecond)})
	if !almostEqual(fps, 10, 1e-9) || !almostEqual(interval, 100, 1e-9) {
		t.Fatalf("fps=%v interval=%v", fps, interval)
	}
	fps, interval = FrameRate([]time.Time{start, start.Add(50 * time.Millisecond)})
	if !almostEqual(fps, 20, 1e-9) || !almostEqual(interval, 50, 1e-9) {
		t.Fatalf("two frames: fps=%v interval=%v", fps, interval)
	}
	for _, starts := range [][]time.Time{nil, {start}, {start, start}} {
		fps, interval = FrameRate(starts)
		if fps != 0 || interval != 0 {
			t.Fatalf("degenerate timestamps %v gave fps=%v interval=%v", starts, fps, interval)
		}
	}
}

func TestClassifyWindowForm(t *testing.T) {
	monitor := rectFacts(0, 0, 1920, 1080)
	same := rectFacts(0, 0, 1920, 1080)
	larger := rectFacts(-8, -8, 1928, 1088)
	smaller := rectFacts(100, 100, 1820, 980)

	cases := []struct {
		name    string
		caption bool
		thick   bool
		window  RectFacts
		want    string
	}{
		{"caption and thick frame", true, true, same, "windowed"},
		{"caption only", true, false, same, "windowed"},
		{"thick frame only", false, true, same, "windowed"},
		{"borderless fullscreen", false, false, same, "fullscreen"},
		{"fullscreen with invisible border", false, false, larger, "fullscreen"},
		{"borderless window", false, false, smaller, "borderless"},
		{"empty rectangle", false, false, RectFacts{}, "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			form, reason := ClassifyWindowForm(tc.caption, tc.thick, tc.window, monitor)
			if form != tc.want {
				t.Fatalf("form = %q (%s), want %q", form, reason, tc.want)
			}
			if reason == "" {
				t.Fatal("reason must not be empty")
			}
		})
	}
}

func TestMarshalWindowReportJSON(t *testing.T) {
	start := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	windowRect := rectFacts(0, 0, 1920, 1080)
	clientRect := rectFacts(100, 200, 2020, 1280)
	monitor := MonitorFacts{
		Handle: 0x10001, HandleHex: "0x10001", Device: `\\.\DISPLAY1`, Primary: true,
		Rect: rectFacts(0, 0, 2560, 1440), WorkArea: rectFacts(0, 0, 2560, 1400),
		DPI: 96, ScalePercent: 100,
	}
	target := &WindowFacts{
		HWND: 0x1234, HWNDHex: "0x1234", PID: 42, Executable: `C:\Games\game.exe`,
		ProcessStartedAt: start, Title: "Game", ClassName: "GameWindow",
		Visible: true, Foreground: true,
		Style:      WindowStyleFacts{Style: wsCaption, StyleHex: "0x00C00000", Form: "windowed", FormReason: "WS_CAPTION present"},
		WindowRect: &windowRect, ClientRect: &clientRect, Monitor: &monitor,
		DPI: DPIFacts{
			WindowAwareness: "perMonitorV2", WindowAwarenessValue: 2, WindowAwarenessContext: "0xFFFFFFFFFFFFFFFC",
			WindowDPI: 96, MonitorDPI: 96, Verdict: "ok",
		},
		Integrity: IntegrityFacts{Level: "medium", RID: 0x2000},
	}
	report := &WindowReport{
		Tool: ToolName, Mode: "capture", StartedAt: start, GeneratedAt: start,
		Executable: `C:\Games\game.exe`, OutDirectory: `C:\evidence`,
		TopLevelWindowCount: 10, CandidateCount: 1,
		Probe: ProbeInfo{
			PID: 7, DPIAwareness: "perMonitorV2", DPIAwarenessValue: 3,
			Integrity: IntegrityFacts{Level: "medium", RID: 0x2000},
		},
		Displays:   DisplaysFacts{Monitors: []MonitorFacts{monitor}},
		Candidates: []WindowFacts{*target},
		Selection:  SelectionResult{Status: "locked", LockedHWND: 0x1234},
		Target:     target,
	}
	data, err := marshalJSON(report)
	if err != nil {
		t.Fatalf("marshalJSON: %v", err)
	}
	if !bytes.HasSuffix(data, []byte("\n")) {
		t.Fatal("JSON must end with a newline")
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	assertField(t, decoded, "tool", ToolName)
	assertField(t, decoded, "topLevelWindowCount", float64(10))
	assertField(t, decoded, "candidateCount", float64(1))
	probe := objectField(t, decoded, "probe")
	assertField(t, probe, "dpiAwareness", "perMonitorV2")
	assertField(t, objectField(t, probe, "integrity"), "level", "medium")
	displays := objectField(t, decoded, "displays")
	monitors := arrayField(t, displays, "monitors")
	first := monitors[0].(map[string]any)
	assertField(t, first, "scalePercent", float64(100))
	assertField(t, first, "device", `\\.\DISPLAY1`)
	targetMap := objectField(t, decoded, "target")
	assertField(t, targetMap, "hwnd", float64(0x1234))
	targetClient := objectField(t, targetMap, "clientRect")
	assertField(t, targetClient, "width", float64(1920))
	targetDPI := objectField(t, targetMap, "dpi")
	assertField(t, targetDPI, "windowAwareness", "perMonitorV2")
	assertField(t, targetDPI, "windowAwarenessValue", float64(2))
	assertField(t, targetDPI, "windowDPI", float64(96))
	assertField(t, objectField(t, targetMap, "style"), "form", "windowed")
}

func TestMarshalFramesReportJSON(t *testing.T) {
	start := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	report := &FramesReport{
		Tool: ToolName, Executable: `C:\Games\game.exe`, StartedAt: start, CompletedAt: start,
		RequestedFrames: 2, IntervalMS: 100,
		Frames: []FrameRecord{
			{
				Index: 0, File: "frame-0000.png", Timestamp: start, CaptureMS: 1.5, FrameMS: 2.5,
				Width: 1920, Height: 1080, ClientWidth: 1920, ClientHeight: 1080,
				SizeMatchesClient: true, GeometryVersion: 3, Mean: 10, Variance: 4,
				SHA256: "abc",
			},
			{
				Index: 1, File: "frame-0001.png", Timestamp: start.Add(100 * time.Millisecond),
				CaptureMS: 2, FrameMS: 3, Width: 1920, Height: 1080, ClientWidth: 1920, ClientHeight: 1080,
				SizeMatchesClient: true, GeometryVersion: 3, Mean: 0, Variance: 0, Black: true,
				SHA256: "abc",
			},
		},
	}
	report.Finalize()
	data, err := marshalJSON(report)
	if err != nil {
		t.Fatalf("marshalJSON: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	assertField(t, decoded, "capturedFrames", float64(2))
	assertField(t, decoded, "blackFrames", float64(1))
	assertField(t, decoded, "duplicateFrames", float64(1))
	assertField(t, decoded, "consecutiveRepeats", float64(1))
	assertField(t, decoded, "sizeMismatches", float64(0))
	rate, ok := decoded["actualFrameRateHz"].(float64)
	if !ok || !almostEqual(rate, 10, 1e-9) {
		t.Fatalf("actualFrameRateHz = %#v, want 10", decoded["actualFrameRateHz"])
	}
	captureStats := objectField(t, decoded, "captureMs")
	if _, ok := captureStats["p50Ms"]; !ok {
		t.Fatal("captureMs must expose p50Ms")
	}
	frames := arrayField(t, decoded, "frames")
	first := frames[0].(map[string]any)
	assertField(t, first, "geometryVersion", float64(3))
	assertField(t, first, "sizeMatchesClient", true)
	assertField(t, first, "sha256", "abc")
	second := frames[1].(map[string]any)
	assertField(t, second, "black", true)
	assertField(t, second, "duplicateOf", float64(0))
	assertField(t, second, "consecutiveRepeat", true)
}

func TestWriteJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "window.json")
	report := &WindowReport{Tool: ToolName, Mode: "list"}
	if err := writeJSON(path, report); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !bytes.HasSuffix(data, []byte("\n")) {
		t.Fatal("file must end with a newline")
	}
	var decoded WindowReport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Tool != ToolName || decoded.Mode != "list" {
		t.Fatalf("round trip: %+v", decoded)
	}
}

func TestRenderSummary(t *testing.T) {
	start := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	windowRect := rectFacts(0, 0, 1920, 1080)
	clientRect := rectFacts(0, 0, 1920, 1080)
	monitor := MonitorFacts{
		Handle: 1, HandleHex: "0x1", Device: `\\.\DISPLAY1`, Primary: true,
		Rect: rectFacts(0, 0, 1920, 1080), WorkArea: rectFacts(0, 0, 1920, 1040),
		DPI: 96, ScalePercent: 100,
	}
	target := &WindowFacts{
		HWND: 0x1234, HWNDHex: "0x1234", PID: 42, Executable: `C:\Games\game.exe`,
		Title: "Game", ClassName: "GameWindow", Visible: true, Foreground: true,
		Style:      WindowStyleFacts{Form: "borderless", FormReason: "no WS_CAPTION/WS_THICKFRAME"},
		WindowRect: &windowRect, ClientRect: &clientRect, Monitor: &monitor,
		DPI: DPIFacts{
			WindowAwareness: "perMonitorV2", WindowAwarenessContext: "0xFFFFFFFFFFFFFFFC",
			WindowDPI: 96, MonitorDPI: 96, Verdict: "ok",
		},
		Integrity: IntegrityFacts{Level: "medium", RID: 0x2000},
	}
	windowReport := &WindowReport{
		Tool: ToolName, Mode: "capture", StartedAt: start, GeneratedAt: start,
		Executable: `C:\Games\game.exe`, OutDirectory: `C:\evidence`,
		TopLevelWindowCount: 12, CandidateCount: 1,
		Probe: ProbeInfo{
			PID: 7, DPIAwareness: "perMonitorV2", DPIAwarenessValue: 3,
			Integrity: IntegrityFacts{Level: "medium", RID: 0x2000},
		},
		Displays:   DisplaysFacts{Monitors: []MonitorFacts{monitor}},
		Candidates: []WindowFacts{*target},
		Selection:  SelectionResult{Status: "locked", LockedHWND: 0x1234},
		Target:     target,
	}
	framesReport := &FramesReport{
		Tool: ToolName, Executable: `C:\Games\game.exe`, StartedAt: start, CompletedAt: start,
		RequestedFrames: 2, IntervalMS: 100,
		Frames: []FrameRecord{
			{
				Index: 0, File: "frame-0000.png", Timestamp: start, CaptureMS: 2, FrameMS: 3,
				Width: 1920, Height: 1080, ClientWidth: 1920, ClientHeight: 1080,
				SizeMatchesClient: true, GeometryVersion: 1, SHA256: "a",
			},
			{
				Index: 1, File: "frame-0001.png", Timestamp: start.Add(100 * time.Millisecond),
				CaptureMS: 4, FrameMS: 5, Width: 1920, Height: 1080,
				ClientWidth: 1920, ClientHeight: 1080, SizeMatchesClient: true,
				GeometryVersion: 1, SHA256: "a", Black: true,
			},
		},
	}
	framesReport.Finalize()

	summary := RenderSummary(windowReport, framesReport)
	for _, want := range []string{
		"result: OK",
		"mode: capture",
		"target: hwnd=0x1234 pid=42",
		"form=borderless",
		"dpi: targetWindow=perMonitorV2 [0xFFFFFFFFFFFFFFFC] windowDPI=96 monitorDPI=96 verdict=ok",
		"frames: requested=2 captured=2 failed=0",
		"client size: 1920x1080",
		"geometry versions: 1",
		"capture size equals client size: 2/2",
		"black frames: 1",
		"frame rate: 10.00 fps",
		"duplicate frames: 1 of 2 captured (consecutive repeats: 1)",
		"\nerrors\n  none\n",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q:\n%s", want, summary)
		}
	}

	windowReport.Errors = append(windowReport.Errors, "boom")
	failed := RenderSummary(windowReport, framesReport)
	if !strings.Contains(failed, "result: FAIL") || !strings.Contains(failed, "- boom") {
		t.Fatalf("failed summary does not carry the error:\n%s", failed)
	}
}

func TestRenderSummaryListMode(t *testing.T) {
	windowReport := &WindowReport{
		Tool: ToolName, Mode: "list",
		Probe:      ProbeInfo{DPIAwareness: "perMonitorV2", DPIAwarenessValue: 3},
		Selection:  SelectionResult{Status: "listOnly"},
		Candidates: []WindowFacts{{HWNDHex: "0x1", PID: 2, Style: WindowStyleFacts{Form: "windowed"}}},
	}
	summary := RenderSummary(windowReport, nil)
	if !strings.Contains(summary, "result: OK") || !strings.Contains(summary, "capture\n  not requested (-list)") {
		t.Fatalf("list summary:\n%s", summary)
	}
	if !strings.Contains(summary, "candidates:") {
		t.Fatalf("list summary omits candidates:\n%s", summary)
	}
}

func TestSummaryResultRequiresLock(t *testing.T) {
	windowReport := &WindowReport{Mode: "capture", Selection: SelectionResult{Status: "noWindow"}}
	framesReport := &FramesReport{}
	if got := summaryResult(windowReport, framesReport); got != "FAIL" {
		t.Fatalf("result = %s, want FAIL", got)
	}
	windowReport.Selection.Status = "locked"
	if got := summaryResult(windowReport, framesReport); got != "OK" {
		t.Fatalf("result = %s, want OK", got)
	}
}

func TestParseOptions(t *testing.T) {
	var stderr bytes.Buffer
	// Build absolute paths from the host temp directory so the test checks
	// the same rule (a full path is required) on macOS and Windows CI.
	exe := filepath.Join(os.TempDir(), "game.exe")
	out := filepath.Join(os.TempDir(), "evidence")
	opts, err := parseOptions([]string{"-exe", exe, "-out", out}, &stderr)
	if err != nil {
		t.Fatalf("parseOptions: %v (%s)", err, stderr.String())
	}
	if opts.exe != filepath.Clean(exe) || opts.out != out || opts.frames != 30 || opts.interval != 100 || opts.list {
		t.Fatalf("options: %+v", opts)
	}

	list, err := parseOptions([]string{"-exe", exe, "-out", out, "-list", "-frames", "0"}, &stderr)
	if err != nil || !list.list || list.frames != 0 {
		t.Fatalf("list options: %+v, err=%v", list, err)
	}

	bad := [][]string{
		{"-out", out},
		{"-exe", exe},
		{"-exe", "relative.exe", "-out", out},
		{"-exe", exe, "-out", out, "-frames", "0"},
		{"-exe", exe, "-out", out, "-interval", "-1"},
		{"-exe", exe, "-out", out, "extra"},
	}
	for _, args := range bad {
		if _, err := parseOptions(args, &stderr); err == nil {
			t.Fatalf("parseOptions(%v) succeeded, want an error", args)
		}
	}

	if _, err := parseOptions([]string{"-h"}, &stderr); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("-h error = %v, want flag.ErrHelp", err)
	}
}

func objectField(t *testing.T, object map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := object[key]
	if !ok {
		t.Fatalf("missing key %q in %v", key, object)
	}
	typed, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("key %q is %T, want object", key, value)
	}
	return typed
}

func arrayField(t *testing.T, object map[string]any, key string) []any {
	t.Helper()
	value, ok := object[key]
	if !ok {
		t.Fatalf("missing key %q in %v", key, object)
	}
	typed, ok := value.([]any)
	if !ok {
		t.Fatalf("key %q is %T, want array", key, value)
	}
	return typed
}

func assertField(t *testing.T, object map[string]any, key string, want any) {
	t.Helper()
	got, ok := object[key]
	if !ok {
		t.Fatalf("missing key %q in %v", key, object)
	}
	if got != want {
		t.Fatalf("%q = %#v, want %#v", key, got, want)
	}
}

func almostEqual(a, b, tolerance float64) bool {
	return math.Abs(a-b) <= tolerance
}

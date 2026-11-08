//go:build windows

package main

import (
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/LoTwT/youkai/internal/platform/windows"
)

// runProbe performs the read-only Windows probe. It deliberately does not use
// the platform's ObserveTarget helper: that helper also takes a ForegroundAPI,
// whose interface includes an activation request. The probe re-verifies the
// target, measures its client area and checks DPI with the same platform
// primitives, without ever acquiring an activation capability.
func runProbe(opts options) (windowReport *WindowReport, framesReport *FramesReport) {
	// Keep every DPI- and coordinate-sensitive call on one OS thread. The
	// process awareness itself is set for the whole process below, but a
	// stable thread makes the observations easier to reason about.
	runtime.LockOSThread()

	started := time.Now().UTC()
	windowReport = &WindowReport{
		Tool:         ToolName,
		Mode:         opts.mode(),
		StartedAt:    started,
		Executable:   opts.exe,
		OutDirectory: opts.out,
		Candidates:   []WindowFacts{},
		Displays:     DisplaysFacts{Monitors: []MonitorFacts{}},
		Selection:    SelectionResult{Status: "notRun"},
	}
	defer func() {
		windowReport.GeneratedAt = time.Now().UTC()
		if framesReport != nil {
			framesReport.CompletedAt = time.Now().UTC()
			framesReport.Finalize()
		}
	}()

	windowReport.Notes = append(windowReport.Notes,
		"This probe never sends input, activates, moves or resizes a window, or registers a hotkey.",
		"All rectangles are screen physical pixels measured under per-monitor-v2 awareness; right and bottom are exclusive.",
		"windowRect is GetWindowRect and may include invisible resize borders; clientRect is GetClientRect mapped by ClientToScreen.",
		"Integrity levels are read with PROCESS_QUERY_LIMITED_INFORMATION and TOKEN_QUERY only; a refusal is recorded, never escalated.",
		"Candidates are top-level windows whose process image path matches -exe case-insensitively. The shared collector omits a window when its process identity cannot be queried (for example access denied for a protected or elevated game process); a running game missing from this list is evidence of that refusal, not proof that it is not running.",
		"Selection follows the shared platform rule: executable identity first, then a single visible non-cloaked candidate; more than one remaining candidate is ambiguous and stops the run.",
		"Window state and rectangles in this file are the pre-capture snapshot; per-frame geometry versions and sizes are in frames.json.",
		"GDI capture is a desktop-region crop: the probe never activates the target, so keep the game window visible and unobstructed when running from a console; minimizing, occluding or moving it changes or blacks out the captured region.",
	)

	windowReport.Probe = ProbeInfo{PID: uint32(os.Getpid()), Integrity: selfIntegrity()}
	if executable, err := os.Executable(); err == nil {
		windowReport.Probe.Executable = executable
	}
	if windowReport.Probe.Integrity.Error != "" {
		windowReport.Errors = append(windowReport.Errors, "probe integrity unavailable: "+windowReport.Probe.Integrity.Error)
	}
	if err := setProcessDpiAwarenessPMv2(); err != nil {
		// The observed awareness decides whether this is fatal: the call
		// fails when the mode was already set, which is fine.
		windowReport.Notes = append(windowReport.Notes,
			"SetProcessDpiAwarenessContext(PerMonitorV2) returned an error: "+err.Error())
	}

	geo := windows.RealGeometryAPI()
	self, selfErr := geo.SelfDpiAwareness()
	if selfErr != nil {
		windowReport.Probe.DPIAwareness = "unknown"
		windowReport.Probe.DPIAwarenessError = selfErr.Error()
		windowReport.Errors = append(windowReport.Errors, "query probe DPI awareness: "+selfErr.Error())
	} else {
		windowReport.Probe.DPIAwareness = dpiAwarenessName(self)
		windowReport.Probe.DPIAwarenessValue = int(self)
		if self != windows.DpiAwarenessPerMonitorV2 {
			windowReport.Errors = append(windowReport.Errors, fmt.Sprintf(
				"probe DPI awareness is %s, not perMonitorV2; client coordinates would be virtualized", dpiAwarenessName(self)))
		}
	}

	monitors, monitorErr := enumerateMonitors()
	if monitors != nil {
		windowReport.Displays.Monitors = monitors
	}
	if monitorErr != nil {
		windowReport.Displays.Error = monitorErr.Error()
		windowReport.Errors = append(windowReport.Errors, "enumerate displays: "+monitorErr.Error())
	}

	winAPI := windows.RealWindowAPI()
	procs := windows.RealProcessAPI()
	probes, err := windows.CollectProbes(winAPI, procs)
	if err != nil {
		windowReport.Errors = append(windowReport.Errors, "enumerate top-level windows: "+err.Error())
		windowReport.Selection = SelectionResult{Status: "error", Detail: "not run: window enumeration failed"}
		if !opts.list {
			framesReport = newFramesReport(opts, started)
			framesReport.Errors = append(framesReport.Errors, "capture skipped: window enumeration failed")
		}
		return windowReport, framesReport
	}
	windowReport.TopLevelWindowCount = len(probes)

	matches := filterProbes(probes, opts.exe)
	for _, probe := range matches {
		windowReport.Candidates = append(windowReport.Candidates, collectWindowFacts(probe, probeTitle(winAPI, probe.HWND), geo, self, selfErr))
	}
	windowReport.CandidateCount = len(windowReport.Candidates)

	if opts.list {
		windowReport.Selection = SelectionResult{
			Status: "listOnly",
			Detail: "-list requested; no target selection and no capture",
		}
		return windowReport, nil
	}

	framesReport = newFramesReport(opts, started)
	if selfErr != nil || self != windows.DpiAwarenessPerMonitorV2 {
		windowReport.Selection = SelectionResult{
			Status: "error",
			Detail: "not run: probe DPI awareness is not verified per-monitor-v2",
		}
		framesReport.Errors = append(framesReport.Errors,
			"capture skipped: probe DPI awareness is not verified per-monitor-v2")
		return windowReport, framesReport
	}

	spec := windows.TargetSpec{ExecutablePath: opts.exe, RequireVisible: true}
	selection, selectionErr := windows.SelectTarget(probes, spec)
	if selectionErr != nil && len(matches) > 0 && isSelectionNoWindow(selectionErr) {
		// The candidate filter normalizes paths; the platform selector compares
		// the raw image path. Fall back to the enumerated path so a harmless
		// spelling difference does not turn a present window into "not found".
		fallback := windows.TargetSpec{ExecutablePath: matches[0].ImagePath, RequireVisible: true}
		if selected, fallbackErr := windows.SelectTarget(probes, fallback); fallbackErr == nil {
			selection = selected
			selectionErr = nil
			windowReport.Notes = append(windowReport.Notes,
				"The -exe path matched candidates after normalization but not byte-for-byte; selection used the process image path from the enumeration.")
		}
	}
	if selectionErr != nil {
		status := "error"
		remaining := 0
		var selectionError *windows.SelectionError
		if errors.As(selectionErr, &selectionError) {
			status = string(selectionError.Kind)
			remaining = len(selectionError.Candidates)
		}
		windowReport.Selection = SelectionResult{
			Status:              status,
			Detail:              selectionErr.Error(),
			RemainingCandidates: remaining,
		}
		windowReport.Errors = append(windowReport.Errors, "target selection: "+selectionErr.Error())
		framesReport.Errors = append(framesReport.Errors, "capture skipped: no unique visible target window")
		return windowReport, framesReport
	}

	windowReport.Selection = SelectionResult{
		Status:     "locked",
		LockedHWND: uint64(selection.HWND),
	}
	target := findCandidate(windowReport.Candidates, selection.HWND)
	if target == nil {
		windowReport.Candidates = append(windowReport.Candidates, collectWindowFacts(selection, probeTitle(winAPI, selection.HWND), geo, self, selfErr))
		windowReport.CandidateCount = len(windowReport.Candidates)
		target = &windowReport.Candidates[len(windowReport.Candidates)-1]
	}
	targetCopy := *target
	windowReport.Target = &targetCopy
	windowReport.Selection.Detail = fmt.Sprintf("pid %d, class %q, title %q", targetCopy.PID, targetCopy.ClassName, targetCopy.Title)

	windowReport.Errors = append(windowReport.Errors, requiredTargetErrors(&targetCopy)...)
	if targetCopy.DPI.Verdict != "ok" {
		detail := targetCopy.DPI.Detail
		if detail == "" {
			detail = "no detail"
		}
		framesReport.Errors = append(framesReport.Errors, fmt.Sprintf(
			"capture skipped: target DPI verdict is %s (%s)", targetCopy.DPI.Verdict, detail))
		return windowReport, framesReport
	}

	captureFrames(winAPI, procs, geo, windows.RealGDI(), windows.LockIdentity(selection), self, opts, framesReport)
	return windowReport, framesReport
}

// captureFrames re-verifies the target before every frame, then captures the
// measured client rectangle with the platform's GDI path. A lost target or a
// failed capture stops the run; the failed frame stays in frames.json with its
// error so the evidence shows where the run ended.
func captureFrames(winAPI windows.WindowAPI, procs windows.ProcessAPI, geo windows.GeometryAPI, gdi windows.GDI, locked windows.WindowIdentity, self windows.DpiAwareness, opts options, framesReport *FramesReport) {
	tracker := &windows.GeometryTracker{}
	interval := time.Duration(opts.interval) * time.Millisecond
	var nextStart time.Time

	for index := 0; index < opts.frames; index++ {
		if index > 0 && interval > 0 {
			if wait := time.Until(nextStart); wait > 0 {
				time.Sleep(wait)
			}
		}
		frameStart := time.Now()
		nextStart = frameStart.Add(interval)

		geometry, fault := observeLocked(winAPI, procs, geo, locked, tracker, self)
		if fault != nil {
			framesReport.Frames = append(framesReport.Frames, FrameRecord{
				Index:     index,
				Timestamp: time.Now().UTC(),
				FrameMS:   millis(time.Since(frameStart)),
				Error:     "observe: " + fault.Error(),
			})
			framesReport.Errors = append(framesReport.Errors, fmt.Sprintf("frame %d observation: %s", index, fault.Error()))
			return
		}

		reference := fmt.Sprintf("frame-%04d", index)
		observation, captureFault := windows.CaptureFrame(gdi, geometry, reference)
		if captureFault != nil {
			framesReport.Frames = append(framesReport.Frames, FrameRecord{
				Index:           index,
				Timestamp:       time.Now().UTC(),
				FrameMS:         millis(time.Since(frameStart)),
				ClientWidth:     geometry.Client.Dx(),
				ClientHeight:    geometry.Client.Dy(),
				GeometryVersion: uint64(geometry.Version),
				Error:           "capture: " + captureFault.Error(),
			})
			framesReport.Errors = append(framesReport.Errors, fmt.Sprintf("frame %d capture: %s", index, captureFault.Error()))
			return
		}

		record := FrameRecord{
			Index:           index,
			File:            reference + ".png",
			Timestamp:       observation.RequestedAt.UTC(),
			CaptureMS:       millis(observation.CompletedAt.Sub(observation.RequestedAt)),
			Width:           observation.Image.Width,
			Height:          observation.Image.Height,
			ClientWidth:     geometry.Client.Dx(),
			ClientHeight:    geometry.Client.Dy(),
			GeometryVersion: uint64(observation.GeometryVersion),
		}
		record.SizeMatchesClient = record.Width == record.ClientWidth && record.Height == record.ClientHeight

		stats, statsErr := AnalyzePixels(observation.Image.Width, observation.Image.Height, observation.Image.Stride, observation.Image.Pixels)
		if statsErr != nil {
			record.Error = "analyze: " + statsErr.Error()
		} else {
			record.Mean = stats.Mean
			record.Variance = stats.Variance
			record.Black = stats.Black
			record.SHA256 = stats.SHA256
			if pngErr := writeCapturedPNG(filepath.Join(opts.out, record.File), observation.Image); pngErr != nil {
				record.Error = "write png: " + pngErr.Error()
			}
		}
		record.FrameMS = millis(time.Since(frameStart))
		framesReport.Frames = append(framesReport.Frames, record)

		if record.Error != "" {
			framesReport.Errors = append(framesReport.Errors, fmt.Sprintf("frame %d: %s", index, record.Error))
			return
		}
		if !record.SizeMatchesClient {
			framesReport.Errors = append(framesReport.Errors, fmt.Sprintf(
				"frame %d: capture %dx%d does not match client %dx%d (geometry version %d)",
				index, record.Width, record.Height, record.ClientWidth, record.ClientHeight, record.GeometryVersion))
			return
		}
	}
}

// observeLocked re-verifies the locked window and process instance and
// refreshes the client geometry. It mirrors the platform's ObserveTarget
// observation order but has no foreground or activation capability: the probe
// never asks for or depends on focus.
func observeLocked(winAPI windows.WindowAPI, procs windows.ProcessAPI, geo windows.GeometryAPI, locked windows.WindowIdentity, tracker *windows.GeometryTracker, self windows.DpiAwareness) (windows.ClientGeometry, *windows.PlatformFault) {
	lastVersion := windows.GeometryVersion(0)
	if current, ok := tracker.Current(); ok {
		lastVersion = current.Version
	}
	fault := func(code, message string, cause error) *windows.PlatformFault {
		return windows.NewFault(code, message, cause, windows.PhaseObserve, time.Now(), lastVersion)
	}

	if !winAPI.IsWindow(locked.HWND) {
		return windows.ClientGeometry{}, fault(windows.CodeTargetLost, "target window gone", nil)
	}
	pid, err := winAPI.ThreadProcessID(locked.HWND)
	if err != nil || pid == 0 {
		return windows.ClientGeometry{}, fault(windows.CodeTargetLost, "target window has no owning process", err)
	}
	path, processStarted, err := procs.Query(pid)
	if err != nil {
		return windows.ClientGeometry{}, fault(windows.CodeTargetLost, "target process identity cannot be verified", err)
	}
	if err := windows.VerifyLocked(locked, windows.WindowProbe{
		HWND:             locked.HWND,
		PID:              pid,
		ImagePath:        path,
		ProcessStartedAt: processStarted,
	}); err != nil {
		return windows.ClientGeometry{}, fault(windows.CodeTargetLost, "target identity mismatch", err)
	}

	measurement, measurementFault := windows.MeasureClient(geo, locked.HWND)
	if measurementFault != nil {
		measurementFault.LastGeometryVersion = lastVersion
		return windows.ClientGeometry{}, measurementFault
	}
	if dpiFault := windows.VerifyDpiSetup(self, measurement.WindowDPI, measurement.MonitorDPI); dpiFault != nil {
		dpiFault.At = time.Now()
		dpiFault.LastGeometryVersion = lastVersion
		return windows.ClientGeometry{}, dpiFault
	}
	current, _ := tracker.Update(measurement, time.Now())
	return current, nil
}

// probeTitle reads one window's caption for the diagnostic report. The
// scanner's probe collection never does this; this developer tool reports
// the title on purpose, and the read stays explicit here.
func probeTitle(win windows.WindowAPI, hwnd windows.HWND) string {
	title, err := win.Title(hwnd)
	if err != nil {
		return ""
	}
	return title
}

func collectWindowFacts(probe windows.WindowProbe, title string, geo windows.GeometryAPI, self windows.DpiAwareness, selfErr error) WindowFacts {
	facts := WindowFacts{
		HWND:             uint64(probe.HWND),
		HWNDHex:          fmt.Sprintf("0x%X", uint64(probe.HWND)),
		PID:              uint32(probe.PID),
		Executable:       probe.ImagePath,
		ProcessStartedAt: probe.ProcessStartedAt.UTC(),
		Title:            title,
		ClassName:        probe.ClassName,
		Visible:          probe.Visible,
		Minimized:        probe.Minimized,
		Maximized:        isZoomed(uintptr(probe.HWND)),
		Cloaked:          probe.Cloaked,
		Foreground:       foregroundWindow() == uintptr(probe.HWND),
		Integrity:        processIntegrity(uint32(probe.PID)),
		Style:            readWindowStyle(uintptr(probe.HWND)),
	}

	if rect, err := windowRect(uintptr(probe.HWND)); err == nil {
		converted := rectFacts(rect.Left, rect.Top, rect.Right, rect.Bottom)
		facts.WindowRect = &converted
	} else {
		facts.Errors = append(facts.Errors, "window rect: "+err.Error())
	}
	if monitor, err := monitorForWindow(uintptr(probe.HWND)); err == nil {
		facts.Monitor = &monitor
	} else {
		facts.Errors = append(facts.Errors, "monitor: "+err.Error())
	}

	measurement, measurementFault := windows.MeasureClient(geo, probe.HWND)
	if measurementFault != nil {
		facts.DPI = DPIFacts{Verdict: "notMeasured", Detail: measurementFault.Error()}
		facts.Errors = append(facts.Errors, "measure client: "+measurementFault.Error())
	} else {
		clientRect := rectFacts(
			int32(measurement.ClientOrigin.X), int32(measurement.ClientOrigin.Y),
			int32(measurement.ClientFarCorner.X), int32(measurement.ClientFarCorner.Y))
		facts.ClientRect = &clientRect
		facts.DPI = DPIFacts{
			WindowDPI:  measurement.WindowDPI,
			MonitorDPI: measurement.MonitorDPI,
			Verdict:    "ok",
		}
		if selfErr != nil {
			facts.DPI.Verdict = "checkUnavailable"
			facts.DPI.Detail = selfErr.Error()
		} else if dpiFault := windows.VerifyDpiSetup(self, measurement.WindowDPI, measurement.MonitorDPI); dpiFault != nil {
			facts.DPI.Verdict = dpiVerdict(dpiFault)
			facts.DPI.Detail = dpiFault.Error()
		}
	}

	awareness, context, awarenessValue, awarenessErr := windowDpiAwareness(uintptr(probe.HWND))
	facts.DPI.WindowAwarenessContext = context
	facts.DPI.WindowAwarenessValue = awarenessValue
	if awarenessErr != nil {
		facts.DPI.WindowAwareness = "unknown"
		facts.DPI.WindowAwarenessError = awarenessErr.Error()
	} else {
		facts.DPI.WindowAwareness = awareness
	}

	if facts.WindowRect != nil && facts.Monitor != nil && facts.Style.Error == "" {
		facts.Style.Form, facts.Style.FormReason = ClassifyWindowForm(
			facts.Style.Caption, facts.Style.ThickFrame, *facts.WindowRect, facts.Monitor.Rect)
	} else {
		facts.Style.Form = "unknown"
		switch {
		case facts.Style.Error != "":
			facts.Style.FormReason = "style query failed"
		case facts.WindowRect == nil:
			facts.Style.FormReason = "window rectangle unavailable"
		default:
			facts.Style.FormReason = "monitor rectangle unavailable"
		}
	}
	return facts
}

func readWindowStyle(hwnd uintptr) WindowStyleFacts {
	styleValue, styleErr := windowLong(hwnd, gwlStyle)
	exStyleValue, exStyleErr := windowLong(hwnd, gwlExStyle)
	style := uint32(styleValue)
	exStyle := uint32(exStyleValue)
	facts := WindowStyleFacts{
		Style:      style,
		StyleHex:   fmt.Sprintf("0x%08X", style),
		ExStyle:    exStyle,
		ExStyleHex: fmt.Sprintf("0x%08X", exStyle),
		Caption:    style&wsCaption != 0,
		ThickFrame: style&wsThickFrame != 0,
		Popup:      style&wsPopup != 0,
		Topmost:    exStyle&wsExTopmost != 0,
		Layered:    exStyle&wsExLayered != 0,
		Form:       "unknown",
	}
	var failures []string
	if styleErr != nil {
		failures = append(failures, styleErr.Error())
	}
	if exStyleErr != nil {
		failures = append(failures, exStyleErr.Error())
	}
	facts.Error = strings.Join(failures, "; ")
	return facts
}

func requiredTargetErrors(target *WindowFacts) []string {
	var errs []string
	if target.Style.Error != "" {
		errs = append(errs, "target window style unavailable: "+target.Style.Error)
	}
	if target.WindowRect == nil {
		errs = append(errs, "target window rect unavailable")
	}
	if target.ClientRect == nil {
		errs = append(errs, "target client rect unavailable")
	}
	if target.Monitor == nil {
		errs = append(errs, "target monitor unavailable")
	} else {
		if target.Monitor.Error != "" {
			errs = append(errs, "target monitor facts incomplete: "+target.Monitor.Error)
		}
		if target.Monitor.DPI == 0 {
			errs = append(errs, "target monitor DPI unavailable")
		}
	}
	if target.DPI.WindowAwarenessError != "" {
		errs = append(errs, "target DPI awareness unavailable: "+target.DPI.WindowAwarenessError)
	}
	if target.DPI.WindowDPI == 0 || target.DPI.MonitorDPI == 0 {
		errs = append(errs, "target DPI values unavailable")
	}
	if target.Integrity.Error != "" {
		errs = append(errs, "target integrity unavailable: "+target.Integrity.Error)
	}
	return errs
}

func dpiVerdict(fault *windows.PlatformFault) string {
	switch fault.Code {
	case windows.CodeDpiAwarenessMismatch:
		return "selfNotPerMonitorV2"
	case windows.CodeDpiVirtualized:
		return "windowVirtualized"
	case windows.CodeDpiCheckUnavailable:
		return "checkUnavailable"
	default:
		return "error"
	}
}

func dpiAwarenessName(awareness windows.DpiAwareness) string {
	switch awareness {
	case windows.DpiAwarenessUnaware:
		return "unaware"
	case windows.DpiAwarenessSystem:
		return "system"
	case windows.DpiAwarenessPerMonitor:
		return "perMonitor"
	case windows.DpiAwarenessPerMonitorV2:
		return "perMonitorV2"
	default:
		return fmt.Sprintf("unknown(%d)", awareness)
	}
}

func filterProbes(probes []windows.WindowProbe, executable string) []windows.WindowProbe {
	matches := make([]windows.WindowProbe, 0, 1)
	for _, probe := range probes {
		if strings.EqualFold(filepath.Clean(probe.ImagePath), executable) {
			matches = append(matches, probe)
		}
	}
	return matches
}

func findCandidate(candidates []WindowFacts, hwnd windows.HWND) *WindowFacts {
	for index := range candidates {
		if candidates[index].HWND == uint64(hwnd) {
			return &candidates[index]
		}
	}
	return nil
}

func isSelectionNoWindow(err error) bool {
	var selectionError *windows.SelectionError
	return errors.As(err, &selectionError) && selectionError.Kind == windows.SelectionNoWindow
}

func newFramesReport(opts options, started time.Time) *FramesReport {
	return &FramesReport{
		Tool:            ToolName,
		Executable:      opts.exe,
		StartedAt:       started,
		RequestedFrames: opts.frames,
		IntervalMS:      opts.interval,
		Frames:          []FrameRecord{},
		Notes: []string{
			"GDI desktop-region capture provides no reuse or frame-status flag; every frame is a new BitBlt of the measured client region.",
			"Frames with the same sha256 have identical content; a static page produces repeats and repeats do not prove a stale frame.",
			"black is mean RGB <= 1.0 of 255; variance is recorded as additional evidence.",
		},
	}
}

func writeCapturedPNG(path string, captured *windows.CapturedImage) (err error) {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
	}()
	rgba := &image.RGBA{
		Pix:    captured.Pixels,
		Stride: captured.Stride,
		Rect:   image.Rect(0, 0, captured.Width, captured.Height),
	}
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	return encoder.Encode(file, rgba)
}

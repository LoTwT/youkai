//go:build windows

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/LoTwT/youkai/internal/platform/windows"
)

const (
	postClickFrames   = 20
	postClickInterval = 100 * time.Millisecond
)

// runInput performs the probe. It keeps every DPI-, input- and last-error-
// sensitive call on one OS thread: the process awareness is set once, and the
// raw GetLastError read after SendInput is meaningful only on the thread that
// made the call.
func runInput(opts options) *RunReport {
	runtime.LockOSThread()

	rep := newRunReport(opts)
	outcome := Outcome{DryRun: opts.dryRun}
	defer func() {
		conclusion, detail := ClassifyConclusion(outcome)
		rep.Conclusion = string(conclusion)
		rep.ConclusionDetail = detail
		rep.GeneratedAt = time.Now().UTC()
	}()

	geoAPI := windows.RealGeometryAPI()
	self, selfErr := prepareProbe(rep, geoAPI)
	countdown(opts.countdown)

	winAPI := windows.RealWindowAPI()
	procs := windows.RealProcessAPI()
	fgAPI := windows.RealForegroundAPI()
	gdi := windows.RealGDI()

	probes, err := windows.CollectProbes(winAPI, procs)
	if err != nil {
		rep.Preflight.Error("targetSelection", "enumerate top-level windows: "+err.Error())
		finishPreflight(rep, &outcome)
		rep.Errors = append(rep.Errors, "enumerate top-level windows: "+err.Error())
		return rep
	}

	selected, ok := selectTarget(rep, probes, opts)
	if !ok {
		finishPreflight(rep, &outcome)
		return rep
	}
	locked := windows.LockIdentity(selected)
	rep.Target = targetFacts(opts.target, selected)
	rep.Target.Integrity = processIntegrity(uint32(selected.PID))
	if rep.Target.Integrity.Error != "" {
		rep.Errors = append(rep.Errors, "target integrity unavailable: "+rep.Target.Integrity.Error)
	}
	if !candidateContains(rep.Candidates, selected.HWND) {
		rep.Candidates = append(rep.Candidates, candidateFacts([]windows.WindowProbe{selected})...)
	}
	rep.Preflight.Pass("targetSelection", fmt.Sprintf("locked hwnd %s pid %d class %q title %q",
		rep.Target.HWNDHex, rep.Target.PID, rep.Target.ClassName, rep.Target.Title))

	// Full identity, state and geometry preflight. A failure here is a
	// deliberate no-click verdict or an observation error; either way the
	// report keeps every fact gathered up to that point.
	if !verifyIdentity(winAPI, procs, locked, rep) {
		finishPreflight(rep, &outcome)
		return rep
	}

	visible := winAPI.IsWindowVisible(locked.HWND)
	rep.Target.Visible = visible
	if visible {
		rep.Preflight.Pass("targetVisible", "IsWindowVisible=true")
	} else {
		rep.Preflight.Refuse("targetVisible", "the target window is not visible")
	}

	cloaked, cloakErr := winAPI.Cloaked(locked.HWND)
	if cloakErr != nil {
		rep.Target.CloakedError = cloakErr.Error()
		rep.Preflight.Refuse("targetNotCloaked", "cloaked state unknown: "+cloakErr.Error())
	} else {
		rep.Target.Cloaked = cloaked
		if cloaked {
			rep.Preflight.Refuse("targetNotCloaked", "the target window is DWM-cloaked")
		} else {
			rep.Preflight.Pass("targetNotCloaked", "DwmGetWindowAttribute(DWMWA_CLOAKED)=0")
		}
	}

	minimized := winAPI.IsIconic(locked.HWND)
	rep.Target.Minimized = minimized
	if minimized {
		rep.Preflight.Refuse("targetNotMinimized", "the target window is minimized")
	} else {
		rep.Preflight.Pass("targetNotMinimized", "IsIconic=0")
	}

	foreground := fgAPI.ForegroundWindow()
	rep.Target.Foreground = foreground == locked.HWND
	if rep.Target.Foreground {
		rep.Preflight.Pass("targetForeground", fmt.Sprintf("GetForegroundWindow=%s", hexHandle(foreground)))
	} else {
		rep.Preflight.Refuse("targetForeground", fmt.Sprintf("foreground window is %s, target is %s; switch to the target during the countdown",
			hexHandle(foreground), rep.Target.HWNDHex))
	}

	measurement, measurementFault := windows.MeasureClient(geoAPI, locked.HWND)
	if measurementFault != nil {
		if measurementFault.Code == windows.CodeDpiCheckUnavailable {
			rep.Preflight.Refuse("clientGeometry", measurementFault.Error())
		} else {
			rep.Preflight.Error("clientGeometry", measurementFault.Error())
		}
		finishPreflight(rep, &outcome)
		rep.Errors = append(rep.Errors, "measure client geometry: "+measurementFault.Error())
		return rep
	}
	tracker := &windows.GeometryTracker{}
	current, _ := tracker.Update(measurement, time.Now())
	rep.Geometry = geometryFacts(current, self, selfErr)
	rep.Preflight.Pass("clientGeometry", fmt.Sprintf("version %d client %s", current.Version, formatRect(current.Client)))
	if current.Monitor.ContainsRect(current.Client) {
		rep.Preflight.Pass("clientOnMonitor", fmt.Sprintf("client %s is fully inside monitor %s",
			formatRect(current.Client), formatRect(current.Monitor)))
	} else {
		rep.Preflight.Refuse("clientOnMonitor", fmt.Sprintf("client %s is not fully inside monitor %s; a partially off-screen window cannot be clicked safely",
			formatRect(current.Client), formatRect(current.Monitor)))
	}

	if selfErr != nil {
		rep.Preflight.Refuse("selfDpiAwareness", "self DPI awareness unavailable: "+selfErr.Error())
		rep.Preflight.NotRun("dpiConsistency", "self DPI awareness is not verified per-monitor-v2")
	} else if self != windows.DpiAwarenessPerMonitorV2 {
		rep.Preflight.Refuse("selfDpiAwareness", fmt.Sprintf("self awareness is %s, not perMonitorV2", dpiAwarenessName(self)))
		rep.Preflight.NotRun("dpiConsistency", "self DPI awareness is not verified per-monitor-v2")
	} else {
		rep.Preflight.Pass("selfDpiAwareness", "GetThreadDpiAwarenessContext reports perMonitorV2")
		if dpiFault := windows.VerifyDpiSetup(self, measurement.WindowDPI, measurement.MonitorDPI); dpiFault != nil {
			rep.Preflight.Refuse("dpiConsistency", dpiFault.Error())
			rep.Geometry.DPIDetail = dpiFault.Error()
		} else {
			rep.Preflight.Pass("dpiConsistency", fmt.Sprintf("windowDPI=%d monitorDPI=%d",
				measurement.WindowDPI, measurement.MonitorDPI))
			rep.Geometry.DPIVerified = true
		}
	}

	var gamePlan *GamePlan
	var notepadPlan *NotepadPlan
	var clickPoint windows.PixelPoint
	planOK := false
	switch opts.target {
	case TargetGame:
		if _, tierErr := MatchStandardTier(current.Client.Dx(), current.Client.Dy()); tierErr != nil {
			rep.Preflight.Refuse("standardTier", tierErr.Error())
			rep.Preflight.NotRun("clickPlan", "no accepted tier")
			rep.Preflight.NotRun("pointInsideClient", "no click point")
			break
		}
		rep.Preflight.Pass("standardTier", fmt.Sprintf("%dx%d scale=%.6f", current.Client.Dx(), current.Client.Dy(),
			float64(current.Client.Dx())/ReferenceCanvasWidth))
		plan, planErr := PlanGameClick(opts.slotRow, opts.slotColumn, current)
		if planErr != nil {
			rep.Preflight.Refuse("clickPlan", planErr.Error())
			rep.Preflight.NotRun("pointInsideClient", "no click point")
			break
		}
		gamePlan = &plan
		clickPoint = plan.ScreenPoint
		rep.Coordinates = gameCoordinates(plan, current)
		rep.Preflight.Pass("clickPlan", fmt.Sprintf("slot %d,%d reference %s client %s screen %s",
			opts.slotRow, opts.slotColumn, formatPoint(plan.CanvasPoint), formatPoint(plan.ClientPoint), formatPoint(plan.ScreenPoint)))
		rep.Preflight.Pass("pointInsideClient", "the scaled point is inside the measured client rect")
		planOK = true
	case TargetNotepad:
		plan, planErr := PlanNotepadClick(current)
		if planErr != nil {
			rep.Preflight.Refuse("editArea", planErr.Error())
			rep.Preflight.NotRun("pointInsideClient", "no click point")
			break
		}
		notepadPlan = &plan
		clickPoint = plan.ScreenPoint
		rep.Coordinates = notepadCoordinates(plan, current)
		rep.Preflight.Pass("editArea", fmt.Sprintf("client %s screen %s inset(h=%d top=%d bottom=%d)",
			formatRect(plan.EditRegion), formatRect(plan.ScreenEditRegion),
			plan.HorizontalInsetPx, plan.TopInsetPx, plan.BottomInsetPx))
		rep.Preflight.Pass("pointInsideClient", "the edit-area center is inside the measured client rect")
		planOK = true
	}

	if planOK {
		hit := fgAPI.HitWindow(clickPoint)
		root := fgAPI.RootOf(hit)
		hitClass, classErr := winAPI.ClassName(hit)
		rep.HitTest = &HitTestFacts{
			Point:        clickPoint,
			Window:       uint64(hit),
			WindowHex:    hexHandle(hit),
			WindowClass:  hitClass,
			Root:         uint64(root),
			RootHex:      hexHandle(root),
			RootIsTarget: root == locked.HWND,
		}
		if classErr != nil {
			rep.HitTest.Error = classErr.Error()
		}
		if hit == 0 || root != locked.HWND {
			rep.Preflight.Refuse("hitTest", fmt.Sprintf("WindowFromPoint(%s) returned %s (class %q), root %s, target %s",
				formatPoint(clickPoint), hexHandle(hit), hitClass, hexHandle(root), rep.Target.HWNDHex))
		} else {
			rep.Preflight.Pass("hitTest", fmt.Sprintf("WindowFromPoint(%s)=%s class=%q root=%s is the target",
				formatPoint(clickPoint), hexHandle(hit), hitClass, hexHandle(root)))
		}
	} else {
		rep.Preflight.NotRun("hitTest", "no click point was derived")
	}

	finishPreflight(rep, &outcome)
	if !rep.Preflight.Passed {
		return rep
	}

	// The pre-click frame is the effect baseline and the page sanity check.
	beforeIndex := len(rep.Frames)
	before, beforeFacts, beforeErr := captureFrame(gdi, current, opts.out, "before", "before")
	beforeFacts.Index = 0
	rep.Frames = append(rep.Frames, beforeFacts)
	if beforeErr != nil {
		reason := "pre-click frame rejected: " + beforeErr.Error()
		rep.Abort = reason
		rep.Errors = append(rep.Errors, reason)
		outcome.Aborted = true
		outcome.AbortReason = reason
		return rep
	}
	outcome.BeforeCaptured = true

	// Re-measure immediately before dispatch, like the production adapter: the
	// precheck alone would let a window move after the baseline. The before
	// frame stays valid only if the geometry version and all target premises
	// are unchanged.
	if _, err := reobserve(winAPI, procs, geoAPI, fgAPI, locked, tracker, self, current.Version); err != nil {
		reason := "pre-dispatch re-observation: " + err.Error()
		rep.Abort = reason
		rep.Errors = append(rep.Errors, reason)
		outcome.Aborted = true
		outcome.AbortReason = reason
		return rep
	}
	actionAt := time.Now().UTC()
	rep.ActionAt = &actionAt
	rep.Frames[beforeIndex].OffsetMS = millis(beforeFacts.Timestamp.Sub(actionAt))

	req := windows.InputRequest{
		GeometryVersion: current.Version,
		FrameRef:        "before",
		Op:              windows.InputOp{Kind: windows.OpClick, Point: clickPoint, Button: windows.MouseButtonLeft},
	}

	precheckBase := windows.InputPrecheck(winAPI, fgAPI, tracker, locked)
	evaluatePrecheck := func(request windows.InputRequest) error {
		precheckErr := precheckBase(request)
		rep.Precheck.Evaluated = true
		rep.Precheck.At = time.Now().UTC()
		rep.Precheck.Passed = precheckErr == nil
		outcome.PrecheckEvaluated = true
		outcome.PrecheckPassed = precheckErr == nil
		if precheckErr == nil {
			rep.Precheck.Code = ""
			rep.Precheck.Detail = "passed"
			return nil
		}
		rep.Precheck.Detail = precheckErr.Error()
		outcome.PrecheckDetail = precheckErr.Error()
		var pe *windows.PrecheckError
		if errors.As(precheckErr, &pe) {
			rep.Precheck.Code = pe.Code
		} else {
			outcome.PrecheckError = true
		}
		return precheckErr
	}

	if opts.dryRun {
		rep.SendInput.State = "dryRun"
		if err := evaluatePrecheck(req); err != nil {
			return rep
		}
		session := &frameSession{
			rep: rep, outcome: &outcome, opts: opts,
			win: winAPI, procs: procs, geo: geoAPI, fg: fgAPI, gdi: gdi,
			locked: locked, tracker: tracker, self: self,
			clickVersion: current.Version, before: before,
			gamePlan: gamePlan, notepadPlan: notepadPlan,
		}
		session.capturePostFrames()
		return rep
	}

	sender := &recordingSender{inner: windows.RealSender(), phase: "dispatch"}
	actor := windows.NewInputActor(sender, evaluatePrecheck)
	defer func() {
		actor.ClosePermission("probe exit")
		sender.setPhase("release")
		appendReleases(rep, "exit", actor.ReleaseHeld())
		rep.SendInput.Calls = sender.snapshot()
	}()
	actor.Permit()

	outcome.DispatchAttempted = true
	result := actor.Dispatch(req)
	outcome.DispatchState = result.State
	outcome.EventsPlanned = result.EventsPlanned
	outcome.EventsInserted = result.EventsInserted
	rep.SendInput.State = string(result.State)
	rep.SendInput.Planned = result.EventsPlanned
	rep.SendInput.Inserted = result.EventsInserted
	rep.SendInput.Aborted = result.Aborted
	rep.SendInput.HeldAfter = heldStrings(result.HeldAfter)
	rep.SendInput.Calls = sender.snapshot()
	if result.Err != nil {
		rep.SendInput.Error = result.Err.Error()
		rep.Errors = append(rep.Errors, "dispatch: "+result.Err.Error())
	}
	if last := actor.LastPostAt(); !last.IsZero() {
		rep.SendInput.LastPostAt = &last
	}

	// One click is the whole run: close permission before observing the
	// effect, so no later code path can dispatch another operation.
	actor.ClosePermission("single click dispatched")
	if len(result.HeldAfter) > 0 {
		sender.setPhase("release")
		appendReleases(rep, "afterDispatch", actor.ReleaseHeld())
	}
	rep.SendInput.Calls = sender.snapshot()
	if held := actor.Held(); len(held) > 0 {
		// The release attempt failed. Do not keep observing with a button
		// still down: a pointer move could turn it into an unintended drag.
		// The exit path retries the release once more and records it.
		reason := fmt.Sprintf("a button pressed by this probe is still held after the release attempt (%s); observation aborted", heldStrings(held))
		rep.Abort = reason
		rep.Errors = append(rep.Errors, reason)
		outcome.Aborted = true
		outcome.AbortReason = reason
		return rep
	}

	if result.EventsInserted > 0 {
		session := &frameSession{
			rep: rep, outcome: &outcome, opts: opts,
			win: winAPI, procs: procs, geo: geoAPI, fg: fgAPI, gdi: gdi,
			locked: locked, tracker: tracker, self: self,
			clickVersion: current.Version, before: before,
			gamePlan: gamePlan, notepadPlan: notepadPlan,
		}
		session.capturePostFrames()
	}
	return rep
}

// prepareProbe records the probe process and configures per-monitor-v2
// awareness before any coordinate is measured. A failure is not fatal here:
// the observed awareness decides, exactly as in the production application.
func prepareProbe(rep *RunReport, geoAPI windows.GeometryAPI) (windows.DpiAwareness, error) {
	probe := ProcessFacts{PID: uint32(os.Getpid())}
	if executable, err := os.Executable(); err == nil {
		probe.Executable = executable
	}
	if err := setProcessDpiAwarenessPMv2(); err != nil {
		rep.Notes = append(rep.Notes, "SetProcessDpiAwarenessContext(PerMonitorV2) returned an error: "+err.Error())
	}
	self, err := geoAPI.SelfDpiAwareness()
	if err != nil {
		probe.DPIAwareness = "unknown"
		probe.DPIAwarenessError = err.Error()
		rep.Errors = append(rep.Errors, "self DPI awareness unavailable: "+err.Error())
	} else {
		probe.DPIAwareness = dpiAwarenessName(self)
		probe.DPIAwarenessValue = int(self)
	}
	probe.Integrity = selfIntegrity()
	if probe.Integrity.Error != "" {
		rep.Errors = append(rep.Errors, "probe integrity unavailable: "+probe.Integrity.Error)
	}
	rep.Probe = probe
	return self, err
}

// selectTarget locks exactly one target window and records the candidate set.
func selectTarget(rep *RunReport, probes []windows.WindowProbe, opts options) (windows.WindowProbe, bool) {
	switch opts.target {
	case TargetGame:
		matches := MatchGameTargets(probes, opts.exe)
		rep.Candidates = candidateFacts(matches)
		spec := windows.TargetSpec{ExecutablePath: opts.exe, RequireVisible: true}
		selected, selectionErr := windows.SelectTarget(probes, spec)
		if selectionErr != nil && len(matches) > 0 && isSelectionNoWindow(selectionErr) {
			// The candidate filter normalizes separators and case; the
			// platform selector compares the raw image path. Fall back to
			// the enumerated path so a harmless spelling difference does
			// not turn a present window into "not found".
			fallback := windows.TargetSpec{ExecutablePath: matches[0].ImagePath, RequireVisible: true}
			if retried, retryErr := windows.SelectTarget(probes, fallback); retryErr == nil {
				selected = retried
				selectionErr = nil
				rep.Notes = append(rep.Notes, "The -exe path matched candidates after normalization but not byte-for-byte; selection used the process image path from the enumeration.")
			}
		}
		if selectionErr != nil {
			rep.Preflight.Refuse("targetSelection", selectionErr.Error())
			return windows.WindowProbe{}, false
		}
		return selected, true
	case TargetNotepad:
		matches := MatchNotepad(probes, opts.notepadTitle)
		rep.Candidates = candidateFacts(matches)
		selected, selectionErr := SelectNotepad(probes, opts.notepadTitle)
		if selectionErr != nil {
			rep.Preflight.Refuse("targetSelection", selectionErr.Error())
			return windows.WindowProbe{}, false
		}
		return selected, true
	default:
		rep.Preflight.Refuse("targetSelection", "unknown target kind "+opts.target)
		return windows.WindowProbe{}, false
	}
}

// verifyIdentity re-verifies the locked process instance. Only a live window
// with the same handle, PID, process creation time and executable may proceed.
func verifyIdentity(winAPI windows.WindowAPI, procs windows.ProcessAPI, locked windows.WindowIdentity, rep *RunReport) bool {
	if !winAPI.IsWindow(locked.HWND) {
		rep.Preflight.Refuse("targetIdentity", fmt.Sprintf("window handle %d no longer names a window", locked.HWND))
		return false
	}
	pid, err := winAPI.ThreadProcessID(locked.HWND)
	if err != nil || pid == 0 {
		detail := "the window has no owning process"
		if err != nil {
			detail = err.Error()
		}
		rep.Preflight.Refuse("targetIdentity", detail)
		return false
	}
	path, started, err := procs.Query(pid)
	if err != nil {
		rep.Preflight.Refuse("targetIdentity", "the owning process identity cannot be verified: "+err.Error())
		return false
	}
	if err := windows.VerifyLocked(locked, windows.WindowProbe{
		HWND:             locked.HWND,
		PID:              pid,
		ImagePath:        path,
		ProcessStartedAt: started,
	}); err != nil {
		rep.Preflight.Refuse("targetIdentity", err.Error())
		return false
	}
	rep.Preflight.Pass("targetIdentity", fmt.Sprintf("hwnd %s pid %d process start %s executable %s",
		hexHandle(locked.HWND), pid, started.UTC().Format(time.RFC3339Nano), path))
	return true
}

// finishPreflight computes the aggregates and copies them into the classifier
// input.
func finishPreflight(rep *RunReport, outcome *Outcome) {
	rep.Preflight.Finalize()
	outcome.PreflightRefused = rep.Preflight.Refused
	outcome.PreflightError = rep.Preflight.Errored
	outcome.PreflightDetail = rep.Preflight.FirstFailure()
}

// reobserve re-verifies identity, foreground, visibility, cloaking, DPI and
// client geometry. It updates the tracker and refuses a changed geometry
// version, so the caller never uses coordinates from an older measurement.
func reobserve(winAPI windows.WindowAPI, procs windows.ProcessAPI, geoAPI windows.GeometryAPI, fgAPI windows.ForegroundAPI, locked windows.WindowIdentity, tracker *windows.GeometryTracker, self windows.DpiAwareness, version windows.GeometryVersion) (windows.ClientGeometry, error) {
	if !winAPI.IsWindow(locked.HWND) {
		return windows.ClientGeometry{}, errors.New("target window gone")
	}
	pid, err := winAPI.ThreadProcessID(locked.HWND)
	if err != nil || pid == 0 {
		return windows.ClientGeometry{}, errors.New("target window has no owning process")
	}
	path, started, err := procs.Query(pid)
	if err != nil {
		return windows.ClientGeometry{}, fmt.Errorf("target process identity unavailable: %w", err)
	}
	if err := windows.VerifyLocked(locked, windows.WindowProbe{
		HWND:             locked.HWND,
		PID:              pid,
		ImagePath:        path,
		ProcessStartedAt: started,
	}); err != nil {
		return windows.ClientGeometry{}, fmt.Errorf("target identity changed: %w", err)
	}
	if fgAPI.ForegroundWindow() != locked.HWND {
		return windows.ClientGeometry{}, errors.New("the target is no longer the foreground window")
	}
	if !winAPI.IsWindowVisible(locked.HWND) {
		return windows.ClientGeometry{}, errors.New("the target window is no longer visible")
	}
	if winAPI.IsIconic(locked.HWND) {
		return windows.ClientGeometry{}, errors.New("the target window is minimized")
	}
	cloaked, err := winAPI.Cloaked(locked.HWND)
	if err != nil {
		return windows.ClientGeometry{}, fmt.Errorf("cloaked state unknown: %w", err)
	}
	if cloaked {
		return windows.ClientGeometry{}, errors.New("the target window is DWM-cloaked")
	}
	measurement, fault := windows.MeasureClient(geoAPI, locked.HWND)
	if fault != nil {
		return windows.ClientGeometry{}, fmt.Errorf("client geometry unavailable: %w", fault)
	}
	if dpiFault := windows.VerifyDpiSetup(self, measurement.WindowDPI, measurement.MonitorDPI); dpiFault != nil {
		return windows.ClientGeometry{}, fmt.Errorf("DPI check failed: %w", dpiFault)
	}
	current, changed := tracker.Update(measurement, time.Now())
	if changed || current.Version != version {
		return windows.ClientGeometry{}, fmt.Errorf("client geometry changed: version %d, click derived from version %d", current.Version, version)
	}
	return current, nil
}

// frameSession captures the two-second post-click series with a safety
// re-observation before every frame.
type frameSession struct {
	rep          *RunReport
	outcome      *Outcome
	opts         options
	win          windows.WindowAPI
	procs        windows.ProcessAPI
	geo          windows.GeometryAPI
	fg           windows.ForegroundAPI
	gdi          windows.GDI
	locked       windows.WindowIdentity
	tracker      *windows.GeometryTracker
	self         windows.DpiAwareness
	clickVersion windows.GeometryVersion
	before       *windows.CapturedImage
	gamePlan     *GamePlan
	notepadPlan  *NotepadPlan
}

func (s *frameSession) capturePostFrames() {
	action := *s.rep.ActionAt
	for i := 0; i < postClickFrames; i++ {
		scheduled := action.Add(time.Duration(i+1) * postClickInterval)
		if wait := time.Until(scheduled); wait > 0 {
			time.Sleep(wait)
		}
		if _, err := reobserve(s.win, s.procs, s.geo, s.fg, s.locked, s.tracker, s.self, s.clickVersion); err != nil {
			s.abort(fmt.Sprintf("after frame %d observation: %v", i, err))
			return
		}
		reference := fmt.Sprintf("after-%03d", i)
		image, facts, err := captureFrame(s.gdi, s.geometry(), s.opts.out, reference, "after")
		facts.Index = i
		facts.OffsetMS = millis(facts.Timestamp.Sub(action))
		if err == nil && image != nil {
			effect, effectErr := s.evaluate(image)
			if effectErr != nil {
				facts.Error = "effect analysis: " + effectErr.Error()
				err = effectErr
			} else {
				facts.Metrics = frameEffectFacts(effect)
				if s.opts.dryRun {
					facts.EffectReason = "dry run: no click was dispatched"
				} else {
					facts.Effective = effect.Effective
					facts.EffectReason = effect.Reason
					if effect.Effective {
						s.outcome.AnyEffectiveFrame = true
					}
				}
			}
		}
		s.rep.Frames = append(s.rep.Frames, facts)
		if err != nil {
			s.abort(fmt.Sprintf("after frame %d: %v", i, err))
			return
		}
		s.outcome.PostFrames++
	}
}

func (s *frameSession) geometry() windows.ClientGeometry {
	current, ok := s.tracker.Current()
	if !ok {
		return windows.ClientGeometry{}
	}
	return current
}

func (s *frameSession) evaluate(after *windows.CapturedImage) (FrameEffect, error) {
	if s.opts.target == TargetGame {
		if s.gamePlan == nil {
			return FrameEffect{}, errors.New("no game click plan")
		}
		return EvaluateGameFrame(s.before, after, *s.gamePlan)
	}
	if s.notepadPlan == nil {
		return FrameEffect{}, errors.New("no Notepad click plan")
	}
	return EvaluateNotepadFrame(s.before, after, *s.notepadPlan)
}

func (s *frameSession) abort(reason string) {
	s.rep.Abort = reason
	s.rep.Errors = append(s.rep.Errors, reason)
	s.outcome.Aborted = true
	s.outcome.AbortReason = reason
}

// captureFrame captures the current client area, writes its PNG, and records
// the evidence. A non-nil error still returns the frame facts (and the image
// when capture succeeded) so a rejected frame stays in the report.
func captureFrame(gdi windows.GDI, geo windows.ClientGeometry, outDir, reference, phase string) (*windows.CapturedImage, FrameFacts, error) {
	facts := FrameFacts{
		Phase:           phase,
		Timestamp:       time.Now().UTC(),
		File:            reference + ".png",
		ClientWidth:     geo.Client.Dx(),
		ClientHeight:    geo.Client.Dy(),
		GeometryVersion: uint64(geo.Version),
	}
	observation, fault := windows.CaptureFrame(gdi, geo, reference)
	if fault != nil {
		facts.Error = fault.Error()
		return nil, facts, fmt.Errorf("capture: %w", fault)
	}
	image := observation.Image
	facts.Timestamp = observation.RequestedAt.UTC()
	facts.GeometryVersion = uint64(observation.GeometryVersion)
	if image == nil {
		facts.Error = "capture returned no image"
		return nil, facts, errors.New("capture returned no image")
	}
	facts.Width, facts.Height = image.Width, image.Height
	facts.SizeMatchesClient = image.Width == geo.Client.Dx() && image.Height == geo.Client.Dy()
	stats, err := AnalyzeImage(image)
	if err != nil {
		facts.Error = err.Error()
		return image, facts, err
	}
	facts.Mean, facts.Variance, facts.Black, facts.PixelSHA256 = stats.Mean, stats.Variance, stats.Black, stats.PixelSHA256

	data, err := encodePNG(image)
	if err != nil {
		facts.Error = "encode png: " + err.Error()
		return image, facts, err
	}
	sum := sha256.Sum256(data)
	facts.PNGSHA256 = hex.EncodeToString(sum[:])
	if err := os.WriteFile(filepath.Join(outDir, facts.File), data, 0o644); err != nil {
		facts.Error = "write png: " + err.Error()
		return image, facts, err
	}

	switch {
	case observation.GeometryVersion != geo.Version:
		err = fmt.Errorf("capture geometry version %d, expected %d", observation.GeometryVersion, geo.Version)
	case !facts.SizeMatchesClient:
		err = fmt.Errorf("capture %dx%d does not match client %dx%d", image.Width, image.Height, geo.Client.Dx(), geo.Client.Dy())
	case facts.Black:
		err = fmt.Errorf("frame is black (mean RGB %.3f <= %.1f)", facts.Mean, BlackScreenMaxMean)
	}
	if err != nil {
		facts.Error = err.Error()
		return image, facts, err
	}
	return image, facts, nil
}

func encodePNG(captured *windows.CapturedImage) ([]byte, error) {
	if captured == nil || captured.Width <= 0 || captured.Height <= 0 || captured.Stride < captured.Width*4 {
		return nil, errors.New("invalid captured image")
	}
	rgba := &image.RGBA{
		Pix:    captured.Pixels,
		Stride: captured.Stride,
		Rect:   image.Rect(0, 0, captured.Width, captured.Height),
	}
	var buf bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := encoder.Encode(&buf, rgba); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func geometryFacts(geo windows.ClientGeometry, self windows.DpiAwareness, selfErr error) *GeometryFacts {
	facts := &GeometryFacts{
		Version:    uint64(geo.Version),
		Client:     geo.Client,
		Monitor:    geo.Monitor,
		WindowDPI:  geo.WindowDPI,
		MonitorDPI: geo.MonitorDPI,
	}
	if selfErr != nil {
		facts.SelfDPIAwareness = "unknown"
		facts.SelfDPIAwarenessError = selfErr.Error()
	} else {
		facts.SelfDPIAwareness = dpiAwarenessName(self)
		facts.SelfDPIAwarenessValue = int(self)
	}
	return facts
}

func gameCoordinates(plan GamePlan, geo windows.ClientGeometry) *CoordinatesFacts {
	return &CoordinatesFacts{
		ReferenceCanvasWidth:  ReferenceCanvasWidth,
		ReferenceCanvasHeight: ReferenceCanvasHeight,
		ReferencePoint:        &plan.CanvasPoint,
		ClientPoint:           plan.ClientPoint,
		ScreenPoint:           plan.ScreenPoint,
		ClientRect:            geo.Client,
		Tier:                  &plan.Tier,
		BorderLeft:            &plan.BorderLeft,
		BorderBottom:          &plan.BorderBottom,
		NameRow:               &plan.NameRow,
		ScreenBorderLeft:      &plan.ScreenLeft,
		ScreenBorderBottom:    &plan.ScreenBottom,
		ScreenNameRow:         &plan.ScreenNameRow,
	}
}

func notepadCoordinates(plan NotepadPlan, geo windows.ClientGeometry) *CoordinatesFacts {
	return &CoordinatesFacts{
		ReferenceCanvasWidth:  ReferenceCanvasWidth,
		ReferenceCanvasHeight: ReferenceCanvasHeight,
		ClientPoint:           plan.ClientPoint,
		ScreenPoint:           plan.ScreenPoint,
		ClientRect:            geo.Client,
		EditRegion:            &plan.EditRegion,
		ScreenEditRegion:      &plan.ScreenEditRegion,
		EditInsets: &EditInsetsFacts{
			Horizontal: plan.HorizontalInsetPx,
			Top:        plan.TopInsetPx,
			Bottom:     plan.BottomInsetPx,
		},
	}
}

// recordingSender wraps the platform Sender without adding an input path: it
// calls the real Sender and records the accepted count, blocked shape, error
// and raw thread GetLastError for each batch.
type recordingSender struct {
	inner windows.Sender
	phase string
	calls []SendInputCallFacts
}

func (s *recordingSender) setPhase(phase string) {
	s.phase = phase
}

func (s *recordingSender) SendInput(events []windows.InputEvent) (int, bool, error) {
	accepted, blocked, err := s.inner.SendInput(events)
	code := lastError()
	call := SendInputCallFacts{
		Index:            len(s.calls),
		At:               time.Now().UTC(),
		Phase:            s.phase,
		Events:           formatInputEvents(events),
		Accepted:         accepted,
		Blocked:          blocked,
		GetLastError:     code,
		GetLastErrorText: lastErrorText(code),
	}
	if err != nil {
		call.Error = err.Error()
	}
	s.calls = append(s.calls, call)
	return accepted, blocked, err
}

func (s *recordingSender) snapshot() []SendInputCallFacts {
	return append([]SendInputCallFacts(nil), s.calls...)
}

func formatInputEvents(events []windows.InputEvent) []string {
	out := make([]string, 0, len(events))
	for _, event := range events {
		switch event.Kind {
		case windows.EventMouseMove:
			out = append(out, fmt.Sprintf("mouseMove(%d,%d)", event.Point.X, event.Point.Y))
		case windows.EventMouseDown:
			out = append(out, "mouseDown("+buttonName(event.Button)+")")
		case windows.EventMouseUp:
			out = append(out, "mouseUp("+buttonName(event.Button)+")")
		case windows.EventWheel:
			out = append(out, fmt.Sprintf("wheel(%d)", event.WheelSteps))
		case windows.EventKeyDown:
			out = append(out, fmt.Sprintf("keyDown(0x%02x)", event.Key))
		case windows.EventKeyUp:
			out = append(out, fmt.Sprintf("keyUp(0x%02x)", event.Key))
		default:
			out = append(out, fmt.Sprintf("unknown(%d)", event.Kind))
		}
	}
	return out
}

func buttonName(button windows.MouseButton) string {
	switch button {
	case windows.MouseButtonLeft:
		return "left"
	case windows.MouseButtonRight:
		return "right"
	case windows.MouseButtonMiddle:
		return "middle"
	default:
		return fmt.Sprintf("button%d", button)
	}
}

func heldStrings(held []windows.HeldKey) []string {
	out := make([]string, 0, len(held))
	for _, key := range held {
		out = append(out, key.String())
	}
	return out
}

func appendReleases(rep *RunReport, phase string, records []windows.ReleaseRecord) {
	for _, record := range records {
		facts := ReleaseFacts{
			Phase:    phase,
			At:       time.Now().UTC(),
			Key:      record.Key.String(),
			Released: record.Released,
		}
		if record.Err != nil {
			facts.Error = record.Err.Error()
			rep.Errors = append(rep.Errors, fmt.Sprintf("release (%s) of %s failed: %s", phase, record.Key.String(), record.Err))
		}
		rep.Release = append(rep.Release, facts)
	}
}

func countdown(seconds int) {
	if seconds <= 0 {
		fmt.Fprintln(os.Stderr, "wininput: starting immediately; the target must already be foreground (this probe never activates a window)")
		return
	}
	for i := seconds; i > 0; i-- {
		fmt.Fprintf(os.Stderr, "wininput: switch to the target window now; checks start in %d s (this probe never activates a window)\n", i)
		time.Sleep(time.Second)
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

func isSelectionNoWindow(err error) bool {
	var selection *windows.SelectionError
	return errors.As(err, &selection) && selection.Kind == windows.SelectionNoWindow
}

func candidateContains(candidates []WindowCandidateFacts, hwnd windows.HWND) bool {
	for _, candidate := range candidates {
		if candidate.HWND == uint64(hwnd) {
			return true
		}
	}
	return false
}

func hexHandle(handle windows.HWND) string {
	return fmt.Sprintf("0x%X", uint64(handle))
}

func millis(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}

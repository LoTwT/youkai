package platform

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LoTwT/youkai/internal/platform/windows"
)

// winReadinessTarget is the window the read-only geometry and integrity
// rows are judged against: the unique discovered candidate when the source
// confirms targets itself, or the configured window of a fixed-target
// source.
type winReadinessTarget struct {
	hwnd windows.HWND
	pid  windows.ProcessID
}

// CheckReadiness implements ReadinessChecker for the Windows adapter. It
// reuses the stage-1 precheck decisions (target selection, tier, work-area
// containment, DPI, integrity) and the PR-05 candidate enumeration, but it
// deliberately never opens a session: no stop hotkey is registered, no GDI
// capture is taken, no input is dispatched, and the scan occupancy is not
// touched. Rows that need the target foreground (occlusion, exclusive
// fullscreen, frame content) are not part of this report; the application
// layer marks them "after start".
func (a *WinAdapter) CheckReadiness(req ReadinessRequest) (ReadinessReport, error) {
	at := a.now()
	report := newWinReadinessReport(at)

	target := a.readinessTarget(req, &report, at)
	report.set(a.readinessAdministrator(target, report.AccessDeniedWindows, at))

	if target == nil {
		report.set(a.readinessDisplayScale(nil, windows.GeometryMeasurement{}, nil, at))
		return report, nil
	}

	measurement, measureFault := windows.MeasureClient(a.natives.Geo, target.hwnd)
	clientKnown := measurement.ClientSize.X > 0 && measurement.ClientSize.Y > 0 &&
		measurement.ClientFarCorner.X-measurement.ClientOrigin.X == measurement.ClientSize.X &&
		measurement.ClientFarCorner.Y-measurement.ClientOrigin.Y == measurement.ClientSize.Y
	var geometry windows.ClientGeometry
	if clientKnown {
		geometry = windows.BuildGeometry(measurement, 0, at)
	}

	report.set(a.readinessResolution(geometry, clientKnown, req.Display, measureFault, at))
	positionKnown := clientKnown && !measurement.Monitor.Empty() && !measurement.WorkArea.Empty()
	report.set(a.readinessPosition(geometry, positionKnown, req.Display, measureFault, at))
	report.set(a.readinessDisplayScale(target, measurement, measureFault, at))
	return report, nil
}

// readinessTarget resolves the window the remaining rows describe. With a
// configured executable it uses the stage-1 probe collection and selection;
// otherwise it enumerates the PR-05 candidates and uses the unique one (or
// the one the caller confirmed). Ambiguity is never resolved by guessing.
func (a *WinAdapter) readinessTarget(req ReadinessRequest, report *ReadinessReport, at time.Time) *winReadinessTarget {
	if req.Target.ExecutablePath != "" {
		return a.readinessFixedTarget(req.Target, report, at)
	}
	found, err := a.DiscoverTargets()
	if err != nil {
		report.set(ReadinessCheck{
			Item:     ReadinessTargetClient,
			Verdict:  ReadinessUnknown,
			Current:  "enumerationFailed=true",
			Required: "confirmed=1",
			Fault: NewFaultWithParams(CodeObservationFailed,
				"game window enumeration failed: "+err.Error(), nil),
			At: at,
		})
		return nil
	}
	report.Candidates = found.Candidates
	report.AccessDeniedWindows = found.AccessDeniedWindows
	report.UnreadableGameWindows = found.UnreadableGameWindows
	current := fmt.Sprintf("candidates=%d denied=%d unreadable=%d",
		len(found.Candidates), found.AccessDeniedWindows, found.UnreadableGameWindows)

	if req.Target.HandleHint != 0 {
		if confirmed, ok := confirmedReadinessCandidate(found.Candidates, req.Target); ok {
			report.set(ReadinessCheck{Item: ReadinessTargetClient, Verdict: ReadinessSatisfied,
				Current: current + " confirmed=" + fmt.Sprint(confirmed.Handle), Required: "confirmed=1", At: at})
			return &winReadinessTarget{hwnd: windows.HWND(confirmed.Handle), pid: windows.ProcessID(confirmed.PID)}
		}
		report.set(ReadinessCheck{
			Item:     ReadinessTargetClient,
			Verdict:  ReadinessUnsatisfied,
			Current:  current,
			Required: "confirmed=1",
			Fault: NewFaultWithParams(CodeTargetNotFound,
				fmt.Sprintf("the confirmed window %d is gone or was replaced", req.Target.HandleHint), nil),
			At: at,
		})
		return nil
	}

	switch {
	case len(found.Candidates) == 1:
		candidate := found.Candidates[0]
		report.set(ReadinessCheck{Item: ReadinessTargetClient, Verdict: ReadinessSatisfied,
			Current: current, Required: "confirmed=1", At: at})
		return &winReadinessTarget{hwnd: windows.HWND(candidate.Handle), pid: windows.ProcessID(candidate.PID)}
	case len(found.Candidates) > 1:
		report.set(ReadinessCheck{
			Item:     ReadinessTargetClient,
			Verdict:  ReadinessUnsatisfied,
			Current:  current,
			Required: "confirmed=1",
			Fault: NewFaultWithParams(CodeTargetAmbiguous,
				fmt.Sprintf("%d game windows are candidates; the user must confirm one", len(found.Candidates)), nil),
			At: at,
		})
		return nil
	case found.AccessDeniedWindows > 0:
		// The administrator row carries the actionable cause; this row
		// cannot honestly claim the game is not running.
		report.set(ReadinessCheck{
			Item:     ReadinessTargetClient,
			Verdict:  ReadinessUnknown,
			Current:  current,
			Required: "confirmed=1",
			At:       at,
		})
		return nil
	case found.UnreadableGameWindows > 0:
		report.set(ReadinessCheck{
			Item:     ReadinessTargetClient,
			Verdict:  ReadinessUnknown,
			Current:  current,
			Required: "confirmed=1",
			Fault: NewFaultWithParams(CodeObservationFailed,
				"a game window is present but its process could not be read", map[string]string{
					"unreadable": fmt.Sprint(found.UnreadableGameWindows),
				}),
			At: at,
		})
		return nil
	default:
		report.set(ReadinessCheck{
			Item:     ReadinessTargetClient,
			Verdict:  ReadinessUnsatisfied,
			Current:  current,
			Required: "confirmed=1",
			Fault: NewFaultWithParams(CodeTargetNotFound,
				"no top-level window of the game client is running", nil),
			At: at,
		})
		return nil
	}
}

// readinessFixedTarget resolves a source configured with one executable,
// using the same probe collection and selector as the session's stage 1.
func (a *WinAdapter) readinessFixedTarget(spec TargetSpec, report *ReadinessReport, at time.Time) *winReadinessTarget {
	probes, err := windows.CollectProbes(a.natives.Win, a.natives.Procs)
	if err != nil {
		report.set(ReadinessCheck{
			Item:     ReadinessTargetClient,
			Verdict:  ReadinessUnknown,
			Current:  "observationFailed=true",
			Required: "visible=1",
			Fault: NewFaultWithParams(CodeObservationFailed,
				"window enumeration failed: "+err.Error(), nil),
			At: at,
		})
		return nil
	}
	probe, err := windows.SelectTarget(probes, windows.TargetSpec{
		ExecutablePath:       spec.ExecutablePath,
		HWNDHint:             windows.HWND(spec.HandleHint),
		PIDHint:              windows.ProcessID(spec.PIDHint),
		ProcessStartedAtHint: spec.ProcessStartedAtHint,
		ClassNameHint:        spec.ClassNameHint,
		RequireVisible:       true,
	})
	if err != nil {
		verdict := ReadinessUnsatisfied
		code := CodeTargetNotFound
		message := err.Error()
		var sel *windows.SelectionError
		if errors.As(err, &sel) {
			switch sel.Kind {
			case windows.SelectionAmbiguous:
				code = CodeTargetAmbiguous
			case windows.SelectionNotReady:
				code = CodeTargetHidden
			case windows.SelectionNoWindow, windows.SelectionHintGone:
				code = CodeTargetNotFound
			}
			message = readinessSelectionMessage(sel)
		} else {
			verdict = ReadinessUnknown
			code = CodeObservationFailed
		}
		report.set(ReadinessCheck{
			Item:     ReadinessTargetClient,
			Verdict:  verdict,
			Current:  "configured=1",
			Required: "visible=1",
			Fault:    NewFaultWithParams(code, message, nil),
			At:       at,
		})
		return nil
	}
	report.set(ReadinessCheck{Item: ReadinessTargetClient, Verdict: ReadinessSatisfied,
		Current: "configured=1", Required: "visible=1", At: at})
	return &winReadinessTarget{hwnd: probe.HWND, pid: probe.PID}
}

// readinessSelectionMessage names the configured target without its
// directory. The message is log-only evidence (the interface localizes the
// fault code), and the configured executable path may contain the user's
// profile name, so quoting the whole path would put it in the diagnostic
// log. Kind detail and candidate count keep their original wording.
func readinessSelectionMessage(err *windows.SelectionError) string {
	named := *err
	named.Executable = executableBase(err.Executable)
	return named.Error()
}

func confirmedReadinessCandidate(candidates []TargetCandidate, spec TargetSpec) (TargetCandidate, bool) {
	for _, candidate := range candidates {
		if candidate.Handle != spec.HandleHint {
			continue
		}
		if spec.PIDHint != 0 && candidate.PID != spec.PIDHint {
			return TargetCandidate{}, false
		}
		if !spec.ProcessStartedAtHint.IsZero() && !candidate.ProcessStartedAt.Equal(spec.ProcessStartedAtHint) {
			return TargetCandidate{}, false
		}
		if spec.ExecutablePath != "" && !strings.EqualFold(candidate.ExecutablePath, spec.ExecutablePath) {
			return TargetCandidate{}, false
		}
		return candidate, true
	}
	return TargetCandidate{}, false
}

func (a *WinAdapter) readinessAdministrator(target *winReadinessTarget, accessDenied int, at time.Time) ReadinessCheck {
	self, selfErr := a.natives.Integrity.SelfIntegrity()
	selfLabel := windows.IntegrityLabel(self, selfErr)
	if target == nil {
		if accessDenied > 0 && selfErr == nil && self < windows.IntegrityHigh {
			return ReadinessCheck{
				Item:     ReadinessAdministrator,
				Verdict:  ReadinessUnsatisfied,
				Current:  "target=unreadable self=" + selfLabel,
				Required: "target<=self",
				Fault: NewFaultWithParams(CodeIntegrityMismatch,
					"the game window cannot be opened because the game runs at higher integrity than youkai",
					map[string]string{"target": "unreadable", "self": selfLabel}),
				At: at,
			}
		}
		if accessDenied > 0 {
			return ReadinessCheck{
				Item:     ReadinessAdministrator,
				Verdict:  ReadinessUnknown,
				Current:  "target=unknown self=" + selfLabel,
				Required: "target<=self",
				Fault: NewFaultWithParams(CodeIntegrityUnverifiable,
					"the game window's integrity level is unverifiable", map[string]string{
						"target": "unknown", "self": selfLabel,
					}),
				At: at,
			}
		}
		return ReadinessCheck{Item: ReadinessAdministrator, Verdict: ReadinessUnknown,
			Current: "target=missing self=" + selfLabel, Required: "target<=self", At: at}
	}
	targetLevel, selfLevel, targetErr, selfErr, fault := windows.IntegrityPair(a.natives.Integrity, target.pid)
	current := fmt.Sprintf("target=%s self=%s",
		windows.IntegrityLabel(targetLevel, targetErr), windows.IntegrityLabel(selfLevel, selfErr))
	if fault == nil {
		return ReadinessCheck{Item: ReadinessAdministrator, Verdict: ReadinessSatisfied,
			Current: current, Required: "target<=self", At: at}
	}
	mapped := mapWinFault(fault)
	verdict := ReadinessUnsatisfied
	if mapped.Code == CodeIntegrityUnverifiable {
		verdict = ReadinessUnknown
	}
	return ReadinessCheck{Item: ReadinessAdministrator, Verdict: verdict,
		Current: current, Required: "target<=self", Fault: mapped, At: at}
}

func (a *WinAdapter) readinessResolution(geometry windows.ClientGeometry, known bool, policy *WindowDisplayPolicy, measureFault *windows.PlatformFault, at time.Time) ReadinessCheck {
	required := ""
	if policy != nil {
		required = tierNames(policy.OpenTiers)
	}
	if !known {
		check := ReadinessCheck{Item: ReadinessGameResolution, Verdict: ReadinessUnknown,
			Required: required, At: at}
		if measureFault != nil {
			check.Fault = mapWinFault(measureFault)
		}
		return check
	}
	current := fmt.Sprintf("%dx%d", geometry.Client.Dx(), geometry.Client.Dy())
	if policy == nil {
		// Without the game adapter's tier contract the size is known but
		// cannot be judged; guessing a supported tier would be a claim.
		return ReadinessCheck{Item: ReadinessGameResolution, Verdict: ReadinessUnknown,
			Current: current, At: at}
	}
	if fault := resolutionFault(geometry, policy); fault != nil {
		return ReadinessCheck{Item: ReadinessGameResolution, Verdict: ReadinessUnsatisfied,
			Current: current, Required: required, Fault: fault, At: at}
	}
	return ReadinessCheck{Item: ReadinessGameResolution, Verdict: ReadinessSatisfied,
		Current: current, Required: required, At: at}
}

func (a *WinAdapter) readinessPosition(geometry windows.ClientGeometry, known bool, policy *WindowDisplayPolicy, measureFault *windows.PlatformFault, at time.Time) ReadinessCheck {
	if !known {
		check := ReadinessCheck{Item: ReadinessWindowPosition, Verdict: ReadinessUnknown, At: at}
		if measureFault != nil {
			check.Fault = mapWinFault(measureFault)
		}
		return check
	}
	current := pixelRectString(geometry.Client)
	required := "workArea=" + pixelRectString(geometry.WorkArea) + " monitor=" + pixelRectString(geometry.Monitor)
	if fault := positionFault(geometry, policy); fault != nil {
		return ReadinessCheck{Item: ReadinessWindowPosition, Verdict: ReadinessUnsatisfied,
			Current: current, Required: required, Fault: fault, At: at}
	}
	return ReadinessCheck{Item: ReadinessWindowPosition, Verdict: ReadinessSatisfied,
		Current: current, Required: required, At: at}
}

func (a *WinAdapter) readinessDisplayScale(target *winReadinessTarget, measurement windows.GeometryMeasurement, measureFault *windows.PlatformFault, at time.Time) ReadinessCheck {
	self, err := a.natives.Geo.SelfDpiAwareness()
	selfLabel := "unknown"
	if err == nil {
		selfLabel = dpiAwarenessLabel(self)
	}
	required := "self=perMonitorV2 window=monitor"
	current := "self=" + selfLabel
	if err != nil {
		return ReadinessCheck{Item: ReadinessDisplayScale, Verdict: ReadinessUnknown,
			Current: current, Required: required,
			Fault: NewFaultWithParams(CodeDpiCheckUnavailable,
				"youkai's DPI awareness is unreadable: "+err.Error(), map[string]string{"self": "unknown"}),
			At: at}
	}
	if self != windows.DpiAwarenessPerMonitorV2 {
		// Reuse the stage-1 verdict so the checklist and the session report
		// the same cause for the same condition.
		return ReadinessCheck{Item: ReadinessDisplayScale, Verdict: ReadinessUnsatisfied,
			Current: current, Required: required,
			Fault: mapWinFault(windows.VerifyDpiSetup(self, 0, 0)),
			At:    at}
	}
	if target == nil {
		return ReadinessCheck{Item: ReadinessDisplayScale, Verdict: ReadinessUnknown,
			Current: current, Required: required, At: at}
	}
	windowDPI, monitorDPI := measurement.WindowDPI, measurement.MonitorDPI
	if measureFault != nil && measureFault.Code != windows.CodeDpiCheckUnavailable {
		windowDPI, monitorDPI = 0, 0
	}
	current = fmt.Sprintf("self=%s window=%d monitor=%d", selfLabel, windowDPI, monitorDPI)
	fault := windows.VerifyDpiSetup(self, windowDPI, monitorDPI)
	if fault == nil {
		return ReadinessCheck{Item: ReadinessDisplayScale, Verdict: ReadinessSatisfied,
			Current: current, Required: required, At: at}
	}
	mapped := mapWinFault(fault)
	verdict := ReadinessUnsatisfied
	if mapped.Code == CodeDpiCheckUnavailable {
		verdict = ReadinessUnknown
	}
	return ReadinessCheck{Item: ReadinessDisplayScale, Verdict: verdict,
		Current: current, Required: required, Fault: mapped, At: at}
}

func dpiAwarenessLabel(awareness windows.DpiAwareness) string {
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
		return fmt.Sprintf("awareness(%d)", int(awareness))
	}
}

func newWinReadinessReport(at time.Time) ReadinessReport {
	report := ReadinessReport{At: at, Checks: make([]ReadinessCheck, 0, 5)}
	for _, item := range []ReadinessItem{
		ReadinessTargetClient,
		ReadinessAdministrator,
		ReadinessGameResolution,
		ReadinessWindowPosition,
		ReadinessDisplayScale,
	} {
		report.Checks = append(report.Checks, ReadinessCheck{Item: item, Verdict: ReadinessUnknown, At: at})
	}
	return report
}

func (r *ReadinessReport) set(check ReadinessCheck) {
	for i := range r.Checks {
		if r.Checks[i].Item == check.Item {
			r.Checks[i] = check
			return
		}
	}
	r.Checks = append(r.Checks, check)
}

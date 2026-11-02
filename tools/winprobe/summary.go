package main

import (
	"fmt"
	"strings"
	"time"
)

// RenderSummary builds summary.txt from the two evidence reports. It is pure
// so the field verdict is testable without Windows; a report that failed for
// any reason says FAIL and names the reason.
func RenderSummary(windowReport *WindowReport, framesReport *FramesReport) string {
	var b strings.Builder
	b.WriteString("winprobe summary\n")
	fmt.Fprintf(&b, "result: %s\n", summaryResult(windowReport, framesReport))
	if windowReport == nil {
		b.WriteString("error: no window report was produced\n")
		return b.String()
	}
	fmt.Fprintf(&b, "mode: %s\n", windowReport.Mode)
	fmt.Fprintf(&b, "executable: %s\n", windowReport.Executable)
	fmt.Fprintf(&b, "out: %s\n", windowReport.OutDirectory)
	fmt.Fprintf(&b, "started: %s\n", formatTime(windowReport.StartedAt))
	fmt.Fprintf(&b, "generated: %s\n", formatTime(windowReport.GeneratedAt))

	b.WriteString("\nprobe\n")
	fmt.Fprintf(&b, "  pid=%d exe=%s\n", windowReport.Probe.PID, windowReport.Probe.Executable)
	if windowReport.Probe.DPIAwarenessError != "" {
		fmt.Fprintf(&b, "  dpiAwareness=unavailable error=%q\n", windowReport.Probe.DPIAwarenessError)
	} else {
		fmt.Fprintf(&b, "  dpiAwareness=%s value=%d\n", windowReport.Probe.DPIAwareness, windowReport.Probe.DPIAwarenessValue)
	}
	fmt.Fprintf(&b, "  integrity=%s\n", formatIntegrity(windowReport.Probe.Integrity))

	b.WriteString("\ndisplays\n")
	if len(windowReport.Displays.Monitors) == 0 {
		b.WriteString("  none\n")
	}
	for i, monitor := range windowReport.Displays.Monitors {
		fmt.Fprintf(&b, "  [%d] %s\n", i, formatMonitor(monitor))
	}
	if windowReport.Displays.Error != "" {
		fmt.Fprintf(&b, "  enumeration error: %s\n", windowReport.Displays.Error)
	}

	b.WriteString("\nidentification\n")
	b.WriteString("  (rectangles are screen physical pixels; right and bottom are exclusive)\n")
	fmt.Fprintf(&b, "  top-level windows enumerated: %d\n", windowReport.TopLevelWindowCount)
	fmt.Fprintf(&b, "  candidates for executable: %d\n", windowReport.CandidateCount)
	fmt.Fprintf(&b, "  selection: %s", windowReport.Selection.Status)
	if windowReport.Selection.Detail != "" {
		fmt.Fprintf(&b, " (%s)", windowReport.Selection.Detail)
	}
	b.WriteString("\n")

	if windowReport.Mode == "list" {
		writeCandidateList(&b, windowReport.Candidates)
	} else if target := windowReport.Target; target != nil {
		writeTarget(&b, target, countCandidateErrors(windowReport.Candidates))
	} else {
		// No locked target: list the candidates so an ambiguity or not-ready
		// verdict in summary.txt can be read without opening window.json.
		writeCandidateList(&b, windowReport.Candidates)
	}

	if framesReport != nil {
		writeCaptureSummary(&b, framesReport)
	} else if windowReport.Mode == "list" {
		b.WriteString("\ncapture\n  not requested (-list)\n")
	}

	writeList(&b, "errors", append(append([]string(nil), windowReport.Errors...), frameErrors(framesReport)...))
	writeList(&b, "notes", append(append([]string(nil), windowReport.Notes...), frameNotes(framesReport)...))
	return b.String()
}

func summaryResult(windowReport *WindowReport, framesReport *FramesReport) string {
	if windowReport == nil || len(windowReport.Errors) > 0 {
		return "FAIL"
	}
	if windowReport.Mode == "capture" {
		if framesReport == nil || len(framesReport.Errors) > 0 || windowReport.Selection.Status != "locked" {
			return "FAIL"
		}
	}
	return "OK"
}

func writeTarget(b *strings.Builder, target *WindowFacts, candidateErrors int) {
	fmt.Fprintf(b, "  target: hwnd=%s pid=%d\n", target.HWNDHex, target.PID)
	fmt.Fprintf(b, "  exe=%s\n", target.Executable)
	fmt.Fprintf(b, "  class=%q title=%q\n", target.ClassName, target.Title)
	fmt.Fprintf(b, "  form=%s reason=%q\n", target.Style.Form, target.Style.FormReason)
	fmt.Fprintf(b, "  state: visible=%t minimized=%t maximized=%t cloaked=%t foreground=%t\n",
		target.Visible, target.Minimized, target.Maximized, target.Cloaked, target.Foreground)

	b.WriteString("\ntarget geometry\n")
	if target.WindowRect != nil {
		fmt.Fprintf(b, "  window rect: %s\n", formatRect(*target.WindowRect))
	} else {
		b.WriteString("  window rect: unavailable\n")
	}
	if target.ClientRect != nil {
		fmt.Fprintf(b, "  client rect: %s\n", formatRect(*target.ClientRect))
	} else {
		b.WriteString("  client rect: unavailable\n")
	}
	if target.Monitor != nil {
		fmt.Fprintf(b, "  monitor: %s\n", formatMonitor(*target.Monitor))
	} else {
		b.WriteString("  monitor: unavailable\n")
	}
	fmt.Fprintf(b, "  dpi: targetWindow=%s windowDPI=%d monitorDPI=%d verdict=%s\n",
		formatWindowAwareness(target.DPI), target.DPI.WindowDPI, target.DPI.MonitorDPI, target.DPI.Verdict)
	if target.DPI.Detail != "" {
		fmt.Fprintf(b, "       detail: %s\n", target.DPI.Detail)
	}
	fmt.Fprintf(b, "  integrity: target=%s\n", formatIntegrity(target.Integrity))
	if candidateErrors > 0 {
		fmt.Fprintf(b, "  candidate errors: %d (see window.json)\n", candidateErrors)
	}
}

func writeCandidateList(b *strings.Builder, candidates []WindowFacts) {
	if len(candidates) == 0 {
		b.WriteString("  no candidate windows\n")
		return
	}
	b.WriteString("  candidates:\n")
	for i, candidate := range candidates {
		fmt.Fprintf(b, "  [%d] hwnd=%s pid=%d form=%s visible=%t minimized=%t maximized=%t cloaked=%t foreground=%t\n",
			i, candidate.HWNDHex, candidate.PID, candidate.Style.Form,
			candidate.Visible, candidate.Minimized, candidate.Maximized, candidate.Cloaked, candidate.Foreground)
		fmt.Fprintf(b, "      exe=%s class=%q title=%q\n", candidate.Executable, candidate.ClassName, candidate.Title)
		if candidate.WindowRect != nil {
			fmt.Fprintf(b, "      window rect: %s\n", formatRect(*candidate.WindowRect))
		} else {
			b.WriteString("      window rect: unavailable\n")
		}
		if candidate.ClientRect != nil {
			fmt.Fprintf(b, "      client rect: %s\n", formatRect(*candidate.ClientRect))
		} else {
			b.WriteString("      client rect: unavailable\n")
		}
		if candidate.Monitor != nil {
			fmt.Fprintf(b, "      monitor: %s\n", formatMonitor(*candidate.Monitor))
		}
		fmt.Fprintf(b, "      dpi: targetWindow=%s windowDPI=%d monitorDPI=%d verdict=%s\n",
			formatWindowAwareness(candidate.DPI), candidate.DPI.WindowDPI, candidate.DPI.MonitorDPI, candidate.DPI.Verdict)
		if candidate.Integrity.Error != "" {
			fmt.Fprintf(b, "      integrity: %s\n", formatIntegrity(candidate.Integrity))
		}
		for _, err := range candidate.Errors {
			fmt.Fprintf(b, "      error: %s\n", err)
		}
	}
}

func writeCaptureSummary(b *strings.Builder, frames *FramesReport) {
	b.WriteString("\ncapture\n")
	if len(frames.Frames) == 0 && len(frames.Errors) > 0 {
		fmt.Fprintf(b, "  frames: requested=%d, none captured (see errors)\n", frames.RequestedFrames)
		return
	}
	failed := len(frames.Frames) - frames.CapturedFrames
	fmt.Fprintf(b, "  frames: requested=%d captured=%d failed=%d\n", frames.RequestedFrames, frames.CapturedFrames, failed)
	b.WriteString("  (GDI desktop-region capture; no reuse/status flag, each frame is a new BitBlt)\n")
	if frames.InitialClientSize != nil {
		fmt.Fprintf(b, "  client size: %dx%d\n", frames.InitialClientSize.Width, frames.InitialClientSize.Height)
	} else {
		b.WriteString("  client size: unavailable\n")
	}
	if len(frames.ClientSizes) > 1 {
		parts := make([]string, 0, len(frames.ClientSizes))
		for _, size := range frames.ClientSizes {
			parts = append(parts, fmt.Sprintf("%dx%d", size.Width, size.Height))
		}
		fmt.Fprintf(b, "  client size changed during capture: %s\n", strings.Join(parts, ", "))
	}
	if versions := geometryVersions(frames.Frames); len(versions) > 0 {
		fmt.Fprintf(b, "  geometry versions: %s\n", strings.Join(versions, ", "))
	}
	matches := frames.CapturedFrames - frames.SizeMismatches
	fmt.Fprintf(b, "  capture size equals client size: %d/%d\n", matches, frames.CapturedFrames)
	if frames.SizeMismatches > 0 {
		fmt.Fprintf(b, "  size mismatches: %d (see frames.json)\n", frames.SizeMismatches)
	}
	fmt.Fprintf(b, "  black frames: %d (mean RGB <= %.1f of 255)\n", frames.BlackFrames, BlackScreenMaxMean)
	fmt.Fprintf(b, "  frame rate: %.2f fps (mean start interval %.2f ms, target interval %d ms)\n",
		frames.ActualFrameRateHz, frames.MeanIntervalMS, frames.IntervalMS)
	fmt.Fprintf(b, "  capture ms: %s\n", formatDurations(frames.CaptureMS))
	fmt.Fprintf(b, "  frame ms:   %s\n", formatDurations(frames.FrameMS))
	fmt.Fprintf(b, "  duplicate frames: %d of %d captured (consecutive repeats: %d)\n",
		frames.DuplicateFrames, frames.CapturedFrames, frames.ConsecutiveRepeats)
}

func geometryVersions(frames []FrameRecord) []string {
	seen := make(map[uint64]bool)
	versions := make([]string, 0, 1)
	for _, frame := range frames {
		if frame.Error != "" || seen[frame.GeometryVersion] {
			continue
		}
		seen[frame.GeometryVersion] = true
		versions = append(versions, fmt.Sprintf("%d", frame.GeometryVersion))
	}
	return versions
}

func writeList(b *strings.Builder, title string, items []string) {
	fmt.Fprintf(b, "\n%s\n", title)
	if len(items) == 0 {
		b.WriteString("  none\n")
		return
	}
	for _, item := range items {
		fmt.Fprintf(b, "  - %s\n", item)
	}
}

func frameErrors(frames *FramesReport) []string {
	if frames == nil {
		return nil
	}
	return frames.Errors
}

func frameNotes(frames *FramesReport) []string {
	if frames == nil {
		return nil
	}
	return frames.Notes
}

func countCandidateErrors(candidates []WindowFacts) int {
	total := 0
	for _, candidate := range candidates {
		total += len(candidate.Errors)
	}
	return total
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func formatRect(r RectFacts) string {
	return fmt.Sprintf("left=%d top=%d right=%d bottom=%d %dx%d", r.Left, r.Top, r.Right, r.Bottom, r.Width, r.Height)
}

func formatMonitor(m MonitorFacts) string {
	line := fmt.Sprintf("handle=%s device=%q primary=%t rect=[%s] work=[%s] dpi=%d scale=%d%%",
		m.HandleHex, m.Device, m.Primary, formatRect(m.Rect), formatRect(m.WorkArea), m.DPI, m.ScalePercent)
	if m.Error != "" {
		line += " error=" + m.Error
	}
	return line
}

func formatIntegrity(i IntegrityFacts) string {
	if i.Error != "" {
		return fmt.Sprintf("unavailable (%s)", i.Error)
	}
	return fmt.Sprintf("%s rid=0x%04X", i.Level, i.RID)
}

func formatWindowAwareness(d DPIFacts) string {
	if d.WindowAwarenessError != "" {
		return "unavailable"
	}
	if d.WindowAwarenessContext != "" {
		return fmt.Sprintf("%s [%s]", d.WindowAwareness, d.WindowAwarenessContext)
	}
	return d.WindowAwareness
}

func formatDurations(d DurationStats) string {
	if d.Count == 0 {
		return "none"
	}
	return fmt.Sprintf("p50=%.3f p90=%.3f p99=%.3f max=%.3f (n=%d)", d.P50MS, d.P90MS, d.P99MS, d.MaxMS, d.Count)
}

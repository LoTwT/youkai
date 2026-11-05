package app

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
)

// DiagnosticsInput is the data the copyable diagnostic summary reports.
// Callers fill what they have; the text names an absent value instead of
// inventing one.
type DiagnosticsInput struct {
	// Version is the running application version, as mygo.App.Version
	// reports it; empty before the version is known.
	Version string
	// Commit is the build's VCS revision, as BuildCommit reports it;
	// empty for an unstamped build.
	Commit string
	// System is the operating-system and display context of this
	// process, as CurrentSystemInfo reports it.
	System SystemInfo
	// Scan is the most recent session, nil when none ever started.
	Scan *DiagnosticsScan
}

// DiagnosticsScan is the most recent session as the copied diagnostic
// summary reports it. The first trigger's message and structured params
// are deliberately not part of this type's output: platform text and
// params can carry external window identity or user paths, while a
// diagnostic copy may not (T2 §3.2).
type DiagnosticsScan struct {
	// End is the session outcome. A live session uses scan.EndKind
	// values (completed, cancelled, failed); a recovered record may
	// also use "interrupted". Empty while the session is still running.
	End string
	// Trigger is the first stop trigger, nil for a normal completion
	// without one. Only its kind, code, phase and time are copied.
	Trigger *scan.Trigger
}

// DiagnosticsScanFromSnapshot summarizes the service's most recent
// session for DiagnosticsText. It returns nil before the first session
// ever started; a started but not yet ended session has an empty End.
func DiagnosticsScanFromSnapshot(snapshot Snapshot) *DiagnosticsScan {
	if snapshot.SessionID == "" {
		return nil
	}
	out := &DiagnosticsScan{End: string(snapshot.Scan.End)}
	if trigger := snapshot.Scan.Trigger; trigger != nil {
		copied := *trigger
		out.Trigger = &copied
	}
	return out
}

// DiagnosticsScanFromTermination summarizes a saved record's first
// termination for DiagnosticsText. A completed session's termination
// carries the verified-end code, not a fault, and a recovered
// interruption carries the settlement's "interrupted" marker rather than
// a first trigger; neither is turned into a trigger, so a normal or
// interrupted end reports no fault code. A saved termination has no
// trigger time, so the copied text reports the time as unknown rather
// than substituting the session's end time.
func DiagnosticsScanFromTermination(term *record.Termination) *DiagnosticsScan {
	if term == nil {
		return nil
	}
	out := &DiagnosticsScan{End: string(term.Kind)}
	if term.Kind == record.TerminationCompleted || term.Kind == record.TerminationInterrupted {
		return out
	}
	trigger := &scan.Trigger{
		Code:  term.Code,
		Phase: scan.Phase(term.Phase),
	}
	switch term.Kind {
	case record.TerminationFailed:
		trigger.Kind = scan.TriggerFault
	case record.TerminationCancelled:
		trigger.Kind = scan.TriggerUser
	}
	out.Trigger = trigger
	return out
}

// SystemInfo is the operating-system and display context a diagnostic
// copy reports. Zero values mean unknown.
type SystemInfo struct {
	// OS is the operating-system name: Windows, macOS, or the
	// runtime.GOOS name on an unsupported platform.
	OS string
	// OSVersion is the operating-system version, empty when unknown.
	OSVersion string
	// DisplayWidth and DisplayHeight are the main display's pixels;
	// zero when the platform could not report them.
	DisplayWidth  int
	DisplayHeight int
	// DisplayScale is the effective scale (100% is 1); zero when
	// unknown.
	DisplayScale float64
}

// DiagnosticsText assembles the copyable diagnostic summary: the same
// text the results summary, settings and exit-failure dialogs share. It
// reports the application version and build commit, the system version,
// the main display's resolution and scale, and the most recent session's
// end type, first-trigger code, kind, phase and time.
//
// The text is privacy-bounded: it never contains an external window
// title, the trigger's free-form message or structured params, or a
// user's home directory. Values still pass through redaction so a caller
// that hands over a path or malformed code cannot leak one.
func DiagnosticsText(in DiagnosticsInput) string {
	var b strings.Builder
	b.WriteString("youkai 诊断信息\n")
	b.WriteString("版本：")
	b.WriteString(versionText(in.Version, in.Commit))
	b.WriteString("\n系统：")
	b.WriteString(systemText(in.System))
	b.WriteString("\n显示：")
	b.WriteString(displayText(in.System))
	b.WriteString("\n")

	if in.Scan == nil {
		b.WriteString("最近一次扫描：无\n")
		return b.String()
	}

	b.WriteString("最近一次扫描：\n")
	b.WriteString("  结束类型：")
	b.WriteString(endText(in.Scan.End))
	b.WriteString("\n")

	trigger := in.Scan.Trigger
	if trigger == nil {
		b.WriteString("  故障码：无\n  首个触发：无\n")
		return b.String()
	}
	b.WriteString("  故障码：")
	b.WriteString(codeText(trigger.Code))
	b.WriteString("\n  首个触发：")
	b.WriteString(triggerKindText(trigger.Kind))
	b.WriteString("\n  阶段：")
	b.WriteString(phaseText(trigger.Phase))
	b.WriteString("\n  发生时间：")
	b.WriteString(timeText(trigger.At))
	b.WriteString("\n")
	return b.String()
}

// DiagnosticsDirectory returns the directory the "open diagnostics
// directory" action reveals. It reuses Paths.Logs, which holds
// youkai.log, its rotated backups and the collector's diagnostics/
// subtrees (development guide, 诊断日志). The directory is returned
// separately rather than embedded in DiagnosticsText because its path
// contains the user's home directory.
func DiagnosticsDirectory(paths Paths) string {
	return paths.Logs
}

func versionText(version, commit string) string {
	v := redactText(version)
	c := commitText(commit)
	switch {
	case v == "" && c == "":
		return "未知"
	case c == "":
		return v
	case v == "":
		return "构建 " + c
	default:
		return v + "（构建 " + c + "）"
	}
}

func systemText(info SystemInfo) string {
	name := redactText(info.OS)
	version := redactText(info.OSVersion)
	switch {
	case name == "" && version == "":
		return "未知"
	case version == "":
		return name
	case name == "":
		return version
	default:
		return name + " " + version
	}
}

func displayText(info SystemInfo) string {
	width, height := info.DisplayWidth, info.DisplayHeight
	scale := scaleText(info.DisplayScale)
	if width <= 0 || height <= 0 {
		if scale == "" {
			return "未知"
		}
		return "分辨率未知（缩放 " + scale + "）"
	}
	text := fmt.Sprintf("%d×%d", width, height)
	if scale != "" {
		text += "（缩放 " + scale + "）"
	}
	return text
}

func scaleText(scale float64) string {
	if scale <= 0 {
		return ""
	}
	return strconv.FormatFloat(scale*100, 'f', -1, 64) + "%"
}

func endText(end string) string {
	switch end = strings.TrimSpace(end); end {
	case "":
		return "进行中"
	case "completed", "cancelled", "failed", "interrupted":
		return end
	default:
		return "unknown"
	}
}

func triggerKindText(kind scan.TriggerKind) string {
	switch kind {
	case scan.TriggerUser, scan.TriggerFault:
		return string(kind)
	case "":
		return "未知"
	default:
		return "unknown"
	}
}

func phaseText(phase scan.Phase) string {
	switch phase {
	case scan.PhasePreparing, scan.PhaseCapturing, scan.PhaseFinalizing:
		return string(phase)
	case "":
		return "未知"
	default:
		return "unknown"
	}
}

func timeText(at time.Time) string {
	if at.IsZero() {
		return "未知"
	}
	return at.Format("2006-01-02 15:04:05 -07:00")
}

// diagnosticCodePattern accepts the stable identifier shape the scan
// contract uses for trigger and fault codes. Anything else — a localized
// sentence, a window title, a path — is reported as unknown rather than
// copied into the diagnostic text.
var diagnosticCodePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]{0,63}$`)

func codeText(code string) string {
	code = strings.TrimSpace(code)
	if code == "" {
		return "无"
	}
	if !diagnosticCodePattern.MatchString(code) {
		return "unknown"
	}
	return code
}

func commitText(commit string) string {
	commit = strings.TrimSpace(commit)
	if commit == "" {
		return ""
	}
	for _, r := range commit {
		if r == '-' || r == '.' || r == '_' ||
			(r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			continue
		}
		return "unknown"
	}
	return commit
}

var (
	unixHomePath = regexp.MustCompile(`(?i)(?:/Users/|/home/)[^/\\\s]+`)
	winHomePath  = regexp.MustCompile(`(?i)[A-Za-z]:\\Users\\[^\\\s]+`)
)

// redactText keeps a one-line diagnostic value free of the user's home
// directory: macOS and Linux home paths and Windows profile paths become
// <用户目录>, and the current user's exact home path is replaced even when
// its parent does not follow those layouts. All other text is preserved;
// window titles have no free-form field here to enter through.
func redactText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	text = strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(text)
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		text = strings.ReplaceAll(text, home, "<用户目录>")
	}
	text = unixHomePath.ReplaceAllString(text, "<用户目录>")
	text = winHomePath.ReplaceAllString(text, "<用户目录>")
	return text
}

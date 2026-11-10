package ui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/LoTwT/youkai/internal/app"
)

// Target discovery (P-01). The installed Windows source starts only the
// game window the user confirmed, so the checklist's target row lists the
// discovered windows and the start stays disabled until one is chosen. Hide
// and replace are handled fail-closed: a refresh keeps the choice only when
// the same window instance is still present (same handle and process
// instance).
//
// The user-facing strings below are the installed UI's copy; raw errors
// stay in the diagnostic log (the service logs discovery failures and
// refused starts).

const (
	// targetUnreadableText: the game window exists and youkai is already
	// elevated (or its own level is unknown), so a denied process query is
	// not an elevation mismatch. The row's value carries the cause; this is
	// its action. The raw failure is in the diagnostic log.
	targetUnreadableText = "请点“重新检查”；仍无法读取时复制诊断信息。"
	// targetDiscoveryFailedAction: enumeration itself failed; the row's value
	// carries the cause and this is its action.
	targetDiscoveryFailedAction = "请点“重新检查”；仍失败时复制诊断信息。"
	// targetDiscoveryFailedText is the past-event cause of a start refused
	// because enumeration failed. The checklist row owns the current action;
	// by the time this line is read that row may already be satisfied again,
	// so this copy must not tell the user to re-check or retry.
	targetDiscoveryFailedText = "查找游戏窗口失败。"
	// targetGoneText: the confirmed window vanished between confirmation
	// and start. Like targetDiscoveryFailedText this is past-event copy only:
	// a recovered window must not leave "re-check and choose again" behind.
	targetGoneText = "确认的游戏窗口已关闭或被替换。"
	// targetStartFailedText is the generic fallback for any other start
	// refusal in the installed UI. The service logs these refusals with
	// their raw cause (the scan lock, recognizer creation, record creation
	// and the source's own preparation); the line sends the user to that
	// log instead of claiming the cause is unknown. Diagnostics copy is an
	// allowlisted summary and does not contain youkai.log, so it must not
	// be named here.
	targetStartFailedText = "详细原因已写入诊断日志；可在“设置 › 诊断”打开诊断目录查看。"
)

// chosenTarget returns the confirmed candidate, nil when none is chosen.
func (m *Model) chosenTarget() *app.TargetCandidate {
	if !m.source.TargetSelection || m.targetChoice < 0 || m.targetChoice >= len(m.targets) {
		return nil
	}
	chosen := m.targets[m.targetChoice]
	return &chosen
}

// targetReady reports whether a start may proceed with the target rules of
// the wired source: a fixed-target source is always ready; a discovery
// source needs one confirmed candidate.
func (m *Model) targetReady() bool {
	if !m.source.TargetSelection {
		return true
	}
	return m.chosenTarget() != nil
}

// startErrorText maps a refused start in the installed UI to the past-event
// line beside the start. It names only what happened: the current state and
// its action belong to the checklist, which may already be satisfied again.
// A fixed-target development source keeps its raw error for field work. The
// service logs most refusals; an update-install refusal is not logged.
func startErrorText(err error) string {
	switch {
	case errors.Is(err, app.ErrTargetGone):
		return targetGoneText
	case errors.Is(err, app.ErrTargetDiscoveryFailed):
		return targetDiscoveryFailedText
	default:
		return targetStartFailedText
	}
}

// sameTarget reports whether two candidates describe the same window of
// the same process instance, the confirmation's identity.
func sameTarget(a, b app.TargetCandidate) bool {
	return a.Handle == b.Handle && a.PID == b.PID &&
		a.ProcessStartedAt.Equal(b.ProcessStartedAt) &&
		strings.EqualFold(a.ExecutablePath, b.ExecutablePath)
}

// targetLabel names one candidate for the radio list. It shows the
// executable path, the owning process, the window class and the handle, so
// two windows of the same process (a launcher or login window beside the
// game) stay distinguishable. Window titles are never read.
func targetLabel(c app.TargetCandidate) string {
	parts := []string{"游戏窗口"}
	if c.ExecutablePath != "" {
		parts[0] = c.ExecutablePath
	}
	parts = append(parts, fmt.Sprintf("PID %d", c.PID))
	if c.ClassName != "" {
		parts = append(parts, c.ClassName)
	}
	parts = append(parts, fmt.Sprintf("窗口 0x%X", c.Handle))
	return strings.Join(parts, " · ")
}

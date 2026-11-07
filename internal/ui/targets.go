package ui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/LoTwT/youkai/internal/app"
)

// Target discovery (P-01). The installed Windows source starts only the
// game window the user confirmed, so the prepare area lists the discovered
// windows and the start stays disabled until one is chosen. Hide and
// replace are handled fail-closed: a refresh keeps the choice only when the
// same window instance is still present (same handle and process instance).
//
// The user-facing strings below are the installed UI's copy; raw errors
// stay in the diagnostic log (the service logs discovery failures and
// refused starts).

const (
	// targetNotFoundText: no window of the game is running.
	targetNotFoundText = "未找到《绝区零》窗口。请启动游戏并保持窗口可见，然后点“重新检查”。"
	// targetUnreadableText: the game window exists and youkai is already
	// elevated (or its own level is unknown), so a denied process query is
	// not an elevation mismatch. The raw failure is in the diagnostic log.
	targetUnreadableText = "无法读取游戏进程信息，请复制诊断信息反馈；详细信息已写入诊断日志。"
	// targetDiscoveryFailedText: enumeration itself failed.
	targetDiscoveryFailedText = "查找游戏窗口失败，请复制诊断信息反馈；详细信息已写入诊断日志。"
	// targetGoneText: the confirmed window vanished between confirmation
	// and start.
	targetGoneText = "确认的游戏窗口已关闭或被替换，请点“重新检查”后重新选择。"
	// targetStartFailedText is the generic fallback for any other start
	// refusal in the installed UI.
	targetStartFailedText = "开始扫描失败，请稍后重试；详细信息已写入诊断日志。"
)

// targetElevatedText is the PR-09 unified integrity-mismatch copy (cause and
// action) for the discovery-time elevation hint. It stays in the fault
// mapping so the prepare area and the session failure never drift apart.
func targetElevatedText() string {
	return failureText("integrityMismatch", nil)
}

// refreshTargets begins a discovery pass. Results land through update; a
// serial discards a pass that a later refresh superseded. The service logs
// the raw failure before returning it.
func (m *Model) refreshTargets() {
	if !m.source.TargetSelection {
		return
	}
	m.targetsSerial++
	serial := m.targetsSerial
	m.targetsLoading = true
	m.targetsErr = ""
	m.goAsync(func() {
		result, err := m.cfg.Service.DiscoverTargets()
		m.update(func() {
			if serial != m.targetsSerial {
				return
			}
			m.targetsLoading = false
			if err != nil {
				m.targetsErr = targetDiscoveryFailedText
				m.targets = nil
				m.targetsAccessDenied = 0
				m.targetsUnreadable = 0
				m.targetChoice = -1
				return
			}
			m.applyTargets(result)
		})
	})
}

// applyTargets replaces the list and keeps the choice only when the same
// window instance is still among the candidates. A vanished or restarted
// window clears the choice so the user has to confirm again.
func (m *Model) applyTargets(result app.TargetDiscoveryResult) {
	chosen := m.chosenTarget()
	m.targets = result.Candidates
	m.targetsAccessDenied = result.AccessDeniedWindows
	m.targetsUnreadable = result.UnreadableGameWindows
	m.targetChoice = -1
	if chosen == nil {
		return
	}
	for i, c := range m.targets {
		if sameTarget(*chosen, c) {
			m.targetChoice = i
			return
		}
	}
}

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
	return !m.targetsLoading && m.targetsErr == "" && m.chosenTarget() != nil
}

// startErrorText maps a refused start in the installed UI to user copy; a
// fixed-target development source keeps its raw error for field work. The
// service's start refusal already logged the original.
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

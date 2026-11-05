package ui

import (
	"fmt"
	"strings"

	"github.com/LoTwT/youkai/internal/app"
)

// Target discovery (P-01). The installed Windows source starts only the
// game window the user confirmed, so the prepare area lists the discovered
// windows and the start stays disabled until one is chosen. Hide and
// replace are handled fail-closed: a refresh keeps the choice only when the
// same window instance is still present.

// refreshTargets begins a discovery pass. Results land through update; a
// serial discards a pass that a later refresh superseded.
func (m *Model) refreshTargets() {
	if !m.source.TargetSelection {
		return
	}
	m.targetsSerial++
	serial := m.targetsSerial
	m.targetsLoading = true
	m.targetsErr = ""
	m.goAsync(func() {
		candidates, err := m.cfg.Service.DiscoverTargets()
		m.update(func() {
			if serial != m.targetsSerial {
				return
			}
			m.targetsLoading = false
			if err != nil {
				m.targetsErr = err.Error()
				m.targets = nil
				m.targetChoice = -1
				return
			}
			m.applyTargets(candidates)
		})
	})
}

// applyTargets replaces the list and keeps the choice only when the same
// window instance is still among the candidates. A vanished or restarted
// window clears the choice so the user has to confirm again.
func (m *Model) applyTargets(candidates []app.TargetCandidate) {
	chosen := m.chosenTarget()
	m.targets = candidates
	m.targetChoice = -1
	if chosen == nil {
		return
	}
	for i, c := range candidates {
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

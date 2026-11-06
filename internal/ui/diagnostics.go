package ui

import "github.com/LoTwT/youkai/internal/app"

// diagnosticsText assembles the copyable summary shared by the record
// summary and settings. The scan input differs by surface: the summary
// reports the opened record's termination, settings the newest live session.
// Everything else — version, system/display context and the app-level field
// allowlist — stays here so the two surfaces cannot drift apart, and privacy
// remains the responsibility of app.DiagnosticsText.
func (m *Model) diagnosticsText(scan *app.DiagnosticsScan) string {
	var system app.SystemInfo
	if m.cfg.SystemInfo != nil {
		system = m.cfg.SystemInfo()
	}
	return app.DiagnosticsText(app.DiagnosticsInput{
		Version: m.cfg.AppVersion,
		Commit:  m.cfg.BuildCommit,
		System:  system,
		Scan:    scan,
	})
}

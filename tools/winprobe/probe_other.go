//go:build !windows

package main

import "time"

// runProbe is the non-Windows stub. It keeps `go test ./tools/winprobe/...`
// and the macOS `go vet ./...` build green while making it explicit that the
// real probe requires Windows; the Windows-only implementation lives in
// probe_windows.go and win32_windows.go.
func runProbe(opts options) (*WindowReport, *FramesReport) {
	now := time.Now().UTC()
	report := &WindowReport{
		Tool:         ToolName,
		Mode:         opts.mode(),
		StartedAt:    now,
		GeneratedAt:  now,
		Executable:   opts.exe,
		OutDirectory: opts.out,
		Selection:    SelectionResult{Status: "error"},
		Notes: []string{
			"This build was not compiled for Windows; no window, geometry, DPI or capture evidence was collected.",
		},
	}
	report.Errors = append(report.Errors,
		"winprobe only runs on Windows; cross-compile with GOOS=windows GOARCH=amd64 CGO_ENABLED=0 and run the resulting executable on the Windows machine")
	return report, nil
}

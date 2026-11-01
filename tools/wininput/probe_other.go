//go:build !windows

package main

import "time"

// runInput is the non-Windows stub. It keeps `go test ./tools/wininput/...`
// and the macOS `go vet ./...` build green while making it explicit that the
// real probe requires Windows. The Windows implementation lives in
// probe_windows.go and win32_windows.go.
func runInput(opts options) *RunReport {
	report := newRunReport(opts)
	report.GeneratedAt = time.Now().UTC()
	report.Conclusion = string(ConclusionError)
	report.ConclusionDetail = "wininput only runs on Windows; cross-compile with GOOS=windows GOARCH=amd64 CGO_ENABLED=0 and run the executable on the Windows machine"
	report.Errors = append(report.Errors,
		"this build was not compiled for Windows; no target was locked and no input or frame evidence was collected")
	return report
}

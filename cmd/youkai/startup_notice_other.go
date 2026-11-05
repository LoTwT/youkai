//go:build !windows

package main

// showStartupFailureDialog is a no-op outside Windows. macOS is the
// development platform and keeps the log line that reportDataRootFailure
// writes; the first release ships the user-visible notice on Windows only
// (08 §10, plan PR-07).
func showStartupFailureDialog(string) {}

//go:build !windows

package main

// prepareReadinessDPI is a no-op outside Windows: the readiness entry is a
// development aid there (the macOS port has no readiness capability yet).
func prepareReadinessDPI() error { return nil }

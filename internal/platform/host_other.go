//go:build !darwin && !windows

package platform

// Host returns the platform's Port on systems the scanner does not
// support: no adapter exists there, and starts must refuse with the
// unsupportedPlatform fault instead of pretending.
func Host() (Port, error) {
	return nil, NewFault(CodeUnsupportedPlatform, "no platform adapter exists for this system")
}

//go:build darwin

package platform

import "github.com/LoTwT/youkai/internal/platform/macos"

// PrepareSystemTermination releases background callers from main-queue
// waits before the irrevocable OS callback synchronously joins them.
func PrepareSystemTermination() { macos.PrepareSystemTermination() }

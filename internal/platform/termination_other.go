//go:build !darwin

package platform

// PrepareSystemTermination is a no-op where cleanup owns a separate
// message-loop thread and does not wait for the application's UI queue.
func PrepareSystemTermination() {}

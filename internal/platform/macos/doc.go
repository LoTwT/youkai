// Package macos implements the macOS side of the platform capture and
// input base: window identity, single-window capture, geometry, short
// input delivery, permission prechecks, foreground verification and the
// global stop hotkey, per [T2](docs/specs/t2-platform-capture-input.md).
//
// The package is a plain package inside the one Go module, as the
// architecture requires: it does not depend on MyGo, the scan kernel or
// any ZZZ game semantics, and it defines no cross-platform interface —
// the integration PR decides what, if anything, the Windows sibling
// package (internal/platform/windows) shares with this one. Everything
// here is macOS-specific and self-contained.
//
// Layout:
//
//   - types.go: the six handoff semantic types of T2 §4.2 — target
//     observation, image observation, short input request, input result,
//     fault & unavailability, and resource teardown;
//   - geometry.go: points versus pixels, the contentRect/contentScale/
//     scaleFactor model of T2 §3.1, image↔screen transforms and geometry
//     versioning;
//   - clock.go: mach-time to comparable Go monotonic time conversion;
//   - target.go / foreground.go / permission.go: window identity
//     assessment, input readiness verification and permission mapping;
//   - frames.go / capture.go: frame status, the bounded latest-frame
//     cache and the capture session lifecycle over an injected native
//     stream runner;
//   - input.go / hotkey.go: the serial short-input dispatcher with its
//     release ledger, and the global stop hotkey manager;
//   - native.go: the narrow native-API interfaces the pure logic drives;
//     the darwin cgo bridge implements them (bridge_darwin.go and the
//     *_darwin.m files, all behind //go:build darwin so the pure logic
//     still compiles on GOOS=windows).
//
// Native behavior is deliberately minimal and honest: the bridge is a
// thin Objective-C/C shim, C holds no unfixed Go pointers (callbacks are
// associated through int64 handles), and the frame callback only copies
// pixels and writes the bounded cache — it never waits for OCR, saving
// or UI.
//
// Everything that needs a real macOS environment — an actual
// ScreenCaptureKit stream, CGEvent delivery into a real app, AX queries
// against a real window, hotkey behavior — is not covered by unit
// tests; those paths are exercised in real-device test round ①
// (实测轮 ①) once the game client is available. The pure logic beneath
// them (geometry, caching, races, permission mapping) is covered by the
// package tests and runs on every platform.
package macos

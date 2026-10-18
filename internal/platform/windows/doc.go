// Package windows is the Windows platform base of the scanner: target
// window identity, client-area geometry with DPI verification, GDI
// desktop-region capture, bounded short input through SendInput,
// foreground and visibility checks, and the global stop hotkey.
//
// Scope and boundaries (T2 §2.1, architecture §3): the package is a
// plain module-internal package. It does not depend on the UI, the scan
// kernel, the game adapter or the recognition stack, and it defines no
// cross-platform interface — the integration milestone decides the
// shared contracts and the macOS side is a separate package of its own.
//
// Structure: files without a //go:build constraint hold the pure logic
// (identity selection and verification, geometry and coordinate math,
// the serial input actor, the capture choreography over an injected GDI
// surface, the hotkey state machine) and their tests run on any platform
// against API fakes. Win32 calls live in //go:build windows files, built
// on golang.org/x/sys/windows system DLL calls plus a few
// user32/gdi32/shcore procedure declarations — no cgo. The SendInput
// INPUT structure layout is pinned to windows/amd64.
//
// Handle lifetimes are explicit: HWND values are borrowed per call and
// never held beyond one verification pass; process handles are opened
// and closed inside a single query; the capture path owns its DCs and
// bitmap within one call and releases them even on failure; the stop
// hotkey is registered, delivered and unregistered on one locked OS
// thread that owns the message loop.
//
// What this code does not establish: a successful GDI capture or a
// non-black frame is not evidence of a new game frame; an inserted input
// event is not game action success; WindowFromPoint ignores hidden and
// disabled windows, so a clear hit test is not proof of no occlusion;
// and the check-to-insert race of user-mode input cannot be closed. All
// real-machine behavior — DPI virtualization in practice, foreground
// restrictions, occlusion and topmost overlays, real hotkey triggering,
// handle lifetimes under load — is left to the Windows field test round
// (T2 §7); this base implements the mechanics and the pure logic only.
package windows

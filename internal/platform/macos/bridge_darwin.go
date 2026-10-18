//go:build darwin

package macos

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework ScreenCaptureKit -framework Foundation -framework CoreVideo -framework CoreMedia
#cgo LDFLAGS: -framework CoreGraphics -framework AppKit -framework ApplicationServices -framework Carbon

#include <stdlib.h>
#include "bridge.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

// The darwin cgo bridge: thin Go wrappers around the C ABI in bridge.h
// (implemented by the *_darwin.m files), implementing the narrow
// native interfaces of native.go, plus the //export callbacks the
// native side invokes. C never holds a Go pointer — callbacks are
// associated through int64 handles owned by the registry below, and
// removing a handle makes every later native callback a counted drop.

// streamBinding is what a native stream handle maps to.
type streamBinding struct {
	sink NativeStreamSink
}

// hotkeyBinding is what a native hotkey id maps to.
type hotkeyBinding struct {
	onTrigger func()
}

var bridge struct {
	mu           sync.Mutex
	nextStream   int64
	streams      map[int64]*streamBinding
	nextHotkey   int64
	hotkeys      map[int64]*hotkeyBinding
	droppedCalls atomic.Int64
}

func init() {
	bridge.streams = make(map[int64]*streamBinding)
	bridge.hotkeys = make(map[int64]*hotkeyBinding)
}

func registryAddStream(sink NativeStreamSink) int64 {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	bridge.nextStream++
	bridge.streams[bridge.nextStream] = &streamBinding{sink: sink}
	return bridge.nextStream
}

func registryRemoveStream(handle int64) {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	delete(bridge.streams, handle)
}

func registryLookupStream(handle int64) *streamBinding {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	return bridge.streams[handle]
}

func registryAddHotkey(onTrigger func()) int64 {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	bridge.nextHotkey++
	bridge.hotkeys[bridge.nextHotkey] = &hotkeyBinding{onTrigger: onTrigger}
	return bridge.nextHotkey
}

func registryRemoveHotkey(id int64) {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	delete(bridge.hotkeys, id)
}

func registryLookupHotkey(id int64) *hotkeyBinding {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	return bridge.hotkeys[id]
}

// youkaiFrameArrived is the native frame callback. It runs on the
// stream's serial queue thread: it converts the C frame into a
// Go-owned NativeFrame (one pixel copy) and hands it to the bound
// sink. Unknown handles (streams already released, or a new session
// that never owned this stream) are counted and dropped — a late
// callback from an old stream never reaches a new session. It never
// waits for OCR, saving or UI.
//
//export youkaiFrameArrived
func youkaiFrameArrived(handle C.int64_t, geo *C.YKFrameGeometry, pixels unsafe.Pointer, pixelLen C.int) {
	binding := registryLookupStream(int64(handle))
	if binding == nil {
		bridge.droppedCalls.Add(1)
		return
	}
	binding.sink.OnNativeFrame(convertNativeFrame(geo, pixels, int32(pixelLen)))
}

// youkaiStreamEvent is the native lifecycle callback (started, stopped
// with an optional error, drained). The message pointer is owned by
// the native caller and only valid during the call, so it is copied
// here.
//
//export youkaiStreamEvent
func youkaiStreamEvent(handle C.int64_t, kind C.int32_t, errCode C.int32_t, message *C.char) {
	binding := registryLookupStream(int64(handle))
	if binding == nil {
		bridge.droppedCalls.Add(1)
		return
	}
	ev := NativeStreamEvent{}
	switch int32(kind) {
	case C.YK_STREAM_EVENT_STARTED:
		ev.Kind = NativeEventStarted
	case C.YK_STREAM_EVENT_STOPPED:
		ev.Kind = NativeEventStopped
	case C.YK_STREAM_EVENT_DRAINED:
		ev.Kind = NativeEventDrained
	default:
		bridge.droppedCalls.Add(1)
		return
	}
	if int32(errCode) != 0 {
		msg := ""
		if message != nil {
			msg = C.GoString(message)
		}
		ev.Err = fmt.Errorf("native stream error %d: %s", int32(errCode), msg)
	}
	binding.sink.OnNativeStreamEvent(ev)
}

// youkaiHotKeyFired is the hotkey trigger callback. It runs on the
// application's main thread; the bound trigger must not block.
//
//export youkaiHotKeyFired
func youkaiHotKeyFired(handle C.int64_t) {
	binding := registryLookupHotkey(int64(handle))
	if binding == nil {
		bridge.droppedCalls.Add(1)
		return
	}
	binding.onTrigger()
}

// convertNativeFrame copies one C frame into a Go-owned NativeFrame.
func convertNativeFrame(geo *C.YKFrameGeometry, pixels unsafe.Pointer, pixelLen int32) NativeFrame {
	nf := NativeFrame{
		Status: NativeFrameStatus(geo.status),
	}
	if geo.status < 0 {
		nf.Status = NativeFrameStatusMissing
	}
	if bool(geo.hasDisplayTime) {
		nf.DisplayTimeMach = uint64(geo.displayTime)
		nf.HasDisplayTime = true
	}
	if bool(geo.hasContentRect) {
		nf.ContentRect = PointRect{X: float64(geo.contentX), Y: float64(geo.contentY), W: float64(geo.contentW), H: float64(geo.contentH)}
		nf.HasContentRect = true
	}
	if bool(geo.hasContentScale) {
		nf.ContentScale = float64(geo.contentScale)
		nf.HasContentScale = true
	}
	if bool(geo.hasScaleFactor) {
		nf.ScaleFactor = float64(geo.scaleFactor)
		nf.HasScaleFactor = true
	}
	if bool(geo.hasScreenRect) {
		nf.ScreenRect = PointRect{X: float64(geo.screenX), Y: float64(geo.screenY), W: float64(geo.screenW), H: float64(geo.screenH)}
		nf.HasScreenRect = true
	}
	nf.SurfaceWidth = int(geo.surfaceWidth)
	nf.SurfaceHeight = int(geo.surfaceHeight)
	nf.BytesPerRow = int(geo.bytesPerRow)
	// Copy the pixel data once, with a sanity bound; a size that
	// contradicts the surface dimensions or exceeds the bound is
	// treated as "no pixel buffer" rather than trusted.
	const maxPixelBytes = 128 << 20
	expected := nf.BytesPerRow * nf.SurfaceHeight
	if pixels != nil && pixelLen > 0 && int(pixelLen) == expected && expected > 0 && expected <= maxPixelBytes {
		nf.Pixels = C.GoBytes(pixels, C.int(pixelLen))
	}
	return nf
}

// errFromBuf reads the UTF-8 message a native call left in buf.
func errFromBuf(buf []byte) string {
	for i, b := range buf {
		if b == 0 {
			return string(buf[:i])
		}
	}
	return string(buf)
}

// newErrBuf allocates the message buffer native calls write into.
func newErrBuf() []byte { return make([]byte, 512) }

// ---- Window and display enumeration ----

type darwinEnumerator struct {
	timeout time.Duration
}

// NewWindowEnumerator returns the SCShareableContent-based enumerator.
// The asynchronous enumeration is bounded by a 5 second timeout
// instead of waiting forever.
func NewWindowEnumerator() WindowEnumerator {
	return darwinEnumerator{timeout: 5 * time.Second}
}

func (e darwinEnumerator) EnumerateWindows() ([]NativeWindow, []NativeDisplay, error) {
	var winPtr *C.YKWindow
	var winCount C.int
	var dispPtr *C.YKDisplay
	var dispCount C.int
	errBuf := newErrBuf()
	rc := C.yk_enumerate_windows(C.double(e.timeout.Seconds()),
		&winPtr, &winCount, &dispPtr, &dispCount,
		(*C.char)(unsafe.Pointer(&errBuf[0])), C.int(len(errBuf)))
	if rc != 0 {
		msg := errFromBuf(errBuf)
		if msg == "" {
			msg = fmt.Sprintf("enumeration failed with code %d", int(rc))
		}
		return nil, nil, errors.New("macos: " + msg)
	}
	defer C.yk_free_enumeration(winPtr, dispPtr)

	windows := make([]NativeWindow, 0, int(winCount))
	if winCount > 0 && winPtr != nil {
		for _, w := range unsafe.Slice(winPtr, int(winCount)) {
			windows = append(windows, NativeWindow{
				WindowID: uint32(w.windowID),
				PID:      int32(w.pid),
				BundleID: C.GoString(&w.bundleID[0]),
				AppName:  C.GoString(&w.appName[0]),
				Title:    C.GoString(&w.title[0]),
				Frame:    PointRect{X: float64(w.x), Y: float64(w.y), W: float64(w.w), H: float64(w.h)},
				OnScreen: bool(w.onScreen),
				Active:   bool(w.active),
				Layer:    int(w.layer),
			})
		}
	}
	displays := make([]NativeDisplay, 0, int(dispCount))
	if dispCount > 0 && dispPtr != nil {
		for _, d := range unsafe.Slice(dispPtr, int(dispCount)) {
			displays = append(displays, NativeDisplay{
				DisplayID: uint32(d.displayID),
				Width:     int(d.width),
				Height:    int(d.height),
				Frame:     PointRect{X: float64(d.x), Y: float64(d.y), W: float64(d.w), H: float64(d.h)},
			})
		}
	}
	return windows, displays, nil
}

// ---- Capture stream runner ----

type darwinStreamRunner struct{}

// NewStreamRunner returns the SCStream-based runner (single window,
// image only).
func NewStreamRunner() NativeStreamRunner { return darwinStreamRunner{} }

// nativeStreamHandle carries both the native token and the registry
// handle so Release can unbind exactly what StartStream bound.
type nativeStreamHandle struct {
	ctx    unsafe.Pointer
	handle int64
}

func (darwinStreamRunner) StartStream(windowID uint32, sink NativeStreamSink) (NativeStreamHandle, error) {
	handle := registryAddStream(sink)
	var ctx unsafe.Pointer
	errBuf := newErrBuf()
	rc := C.yk_stream_start(C.uint32_t(windowID), C.int64_t(handle), &ctx,
		(*C.char)(unsafe.Pointer(&errBuf[0])), C.int(len(errBuf)))
	if rc != 0 {
		registryRemoveStream(handle)
		msg := errFromBuf(errBuf)
		if msg == "" {
			msg = fmt.Sprintf("stream creation failed with code %d", int(rc))
		}
		return nil, errors.New("macos: " + msg)
	}
	return nativeStreamHandle{ctx: ctx, handle: handle}, nil
}

func (r darwinStreamRunner) handleOf(h NativeStreamHandle) (nativeStreamHandle, error) {
	nh, ok := h.(nativeStreamHandle)
	if !ok || nh.ctx == nil {
		return nh, errors.New("macos: unknown native stream handle")
	}
	return nh, nil
}

func (r darwinStreamRunner) RequestStop(h NativeStreamHandle) error {
	nh, err := r.handleOf(h)
	if err != nil {
		return err
	}
	if rc := C.yk_stream_request_stop(nh.ctx); rc != 0 {
		return fmt.Errorf("macos: requesting stream stop failed with code %d", int(rc))
	}
	return nil
}

func (r darwinStreamRunner) Drain(h NativeStreamHandle) error {
	nh, err := r.handleOf(h)
	if err != nil {
		return err
	}
	if rc := C.yk_stream_drain(nh.ctx); rc != 0 {
		return fmt.Errorf("macos: requesting stream drain failed with code %d", int(rc))
	}
	return nil
}

func (r darwinStreamRunner) Release(h NativeStreamHandle) {
	nh, err := r.handleOf(h)
	if err != nil {
		return
	}
	C.yk_stream_release(nh.ctx)
	registryRemoveStream(nh.handle)
}

// ---- Quartz event posting ----

type darwinPoster struct{}

// NewInputPoster returns the CGEvent-based poster. Every method
// reports only creation failures: a nil error means the event was
// posted, and CGEventPost has no confirmation return value.
func NewInputPoster() InputPoster { return darwinPoster{} }

func (darwinPoster) PostMouseMoved(p Point) error {
	if rc := C.yk_post_mouse_moved(C.double(p.X), C.double(p.Y)); rc != 0 {
		return fmt.Errorf("macos: mouse moved event creation failed (%d)", int(rc))
	}
	return nil
}

func (darwinPoster) PostMouseButton(button MouseButton, down bool, at Point) error {
	d := 0
	if down {
		d = 1
	}
	if rc := C.yk_post_mouse_button(C.int32_t(button), C.int(d), C.double(at.X), C.double(at.Y)); rc != 0 {
		return fmt.Errorf("macos: mouse button event creation failed (%d)", int(rc))
	}
	return nil
}

func (darwinPoster) PostKey(code KeyCode, down bool) error {
	d := 0
	if down {
		d = 1
	}
	if rc := C.yk_post_key(C.int32_t(code), C.int(d)); rc != 0 {
		return fmt.Errorf("macos: key event creation failed (%d)", int(rc))
	}
	return nil
}

func (darwinPoster) PostScroll(deltaY float64) error {
	if rc := C.yk_post_scroll(C.double(deltaY)); rc != 0 {
		return fmt.Errorf("macos: scroll event creation failed (%d)", int(rc))
	}
	return nil
}

// ---- Foreground, AX and permissions ----

type darwinForegroundProbe struct {
	timeout time.Duration
}

// NewForegroundProbe returns the NSWorkspace-based foreground probe.
// The AppKit read is routed through the application's main thread with
// a bounded wait, per T2 §2.2 (AppKit 相关操作交由应用已有的主线程
// 调度); without a serving main run loop it fails fast instead of
// hanging.
func NewForegroundProbe() ForegroundProbe {
	return darwinForegroundProbe{timeout: 2 * time.Second}
}

func (p darwinForegroundProbe) FrontmostPID() (int32, error) {
	pid := int32(C.yk_frontmost_pid(C.double(p.timeout.Seconds())))
	switch pid {
	case -1:
		return 0, errors.New("macos: main run loop unavailable for the frontmost query")
	case 0:
		return 0, errors.New("macos: no frontmost application")
	default:
		return pid, nil
	}
}

type darwinAXProbe struct{}

// NewAXProbe returns the AXFocusedWindow probe with bounded messaging
// timeouts per call.
func NewAXProbe() AXFocusProbe { return darwinAXProbe{} }

func (darwinAXProbe) FocusedWindow(timeout time.Duration) (AXFocusReport, error) {
	var out C.YKAXFocus
	rc := C.yk_ax_focused_window(C.double(timeout.Seconds()), &out)
	report := AXFocusReport{
		PID:        int32(out.pid),
		Frame:      PointRect{X: float64(out.x), Y: float64(out.y), W: float64(out.w), H: float64(out.h)},
		Role:       C.GoString(&out.role[0]),
		TimedOut:   bool(out.timedOut),
		ObservedAt: time.Now(),
	}
	if rc != 0 {
		report.Err = fmt.Errorf("macos: ax focused window query failed (AXError %d)", int32(out.axError))
		return report, report.Err
	}
	return report, nil
}

type darwinPermissions struct{}

// NewPermissionProbe returns the accessibility and event-synthesis
// preflight. Neither query prompts; requesting authorization is a user
// action owned by the caller.
func NewPermissionProbe() PermissionProbe { return darwinPermissions{} }

func (darwinPermissions) AccessibilityTrusted() (bool, error) {
	// AXIsProcessTrustedWithOptions only fails by returning false; a
	// true means trusted. There is no separate error channel.
	return bool(C.yk_ax_trusted()), nil
}

func (darwinPermissions) EventPostAllowed() (bool, error) {
	return bool(C.yk_event_post_allowed()), nil
}

// ---- Global hotkey ----

type darwinHotKeyRegistrar struct {
	timeout time.Duration
}

// NewHotKeyRegistrar returns the Carbon-based global hotkey
// registrar. Registration runs on the application's main thread and
// the hotkey is delivered by the host's main run loop; without one the
// registrar fails fast within the timeout instead of hanging. Real
// registration behavior is verified in 实测轮 ①.
func NewHotKeyRegistrar() NativeHotKeyRegistrar {
	return darwinHotKeyRegistrar{timeout: 2 * time.Second}
}

// nativeHotKeyHandle pairs the native EventHotKeyRef with its registry
// id.
type nativeHotKeyHandle struct {
	ref unsafe.Pointer
	id  int64
}

func (r darwinHotKeyRegistrar) Register(spec HotKeySpec, onTrigger func()) (NativeHotKeyHandle, error) {
	id := registryAddHotkey(onTrigger)
	var ref unsafe.Pointer
	rc := C.yk_hotkey_register(C.uint32_t(spec.KeyCode), C.uint32_t(spec.Modifiers),
		C.int64_t(id), C.double(r.timeout.Seconds()), &ref)
	if rc != 0 {
		registryRemoveHotkey(id)
		if rc == -2 {
			return nil, errors.New("macos: main run loop unavailable for hotkey registration")
		}
		return nil, fmt.Errorf("macos: RegisterEventHotKey failed (status %d)", int(rc))
	}
	return nativeHotKeyHandle{ref: ref, id: id}, nil
}

func (r darwinHotKeyRegistrar) Unregister(handle NativeHotKeyHandle) error {
	h, ok := handle.(nativeHotKeyHandle)
	if !ok || h.ref == nil {
		return errors.New("macos: unknown native hotkey handle")
	}
	rc := C.yk_hotkey_unregister(h.ref, C.double(r.timeout.Seconds()))
	registryRemoveHotkey(h.id)
	if rc != 0 {
		if rc == -2 {
			return errors.New("macos: main run loop unavailable for hotkey unregistration")
		}
		return fmt.Errorf("macos: UnregisterEventHotKey failed (status %d)", int(rc))
	}
	return nil
}

// ---- Mach clock source ----

type darwinMachSource struct{}

// NewMachClockSource returns the mach_absolute_time-based clock source.
// The Go runtime's monotonic clock on darwin is the same domain scaled
// by the same timebase, so anchored conversions are comparable while
// the system is awake (see clock.go).
func NewMachClockSource() MachClockSource { return darwinMachSource{} }

func (darwinMachSource) NowTicks() uint64 { return uint64(C.yk_mach_ticks()) }

func (darwinMachSource) Timebase() MachTimebase {
	return MachTimebase{
		Numer: uint32(C.yk_mach_timebase_numer()),
		Denom: uint32(C.yk_mach_timebase_denom()),
	}
}

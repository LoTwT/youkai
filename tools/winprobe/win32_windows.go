//go:build windows

package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Raw Win32 declarations used only by the probe. The platform layer already
// owns identity, geometry, DPI verification and GDI capture; these calls add
// the read-only facts the probe reports that the platform layer does not
// expose: window styles and rectangles, the window's own DPI awareness
// context, the full monitor list with work areas, and token integrity levels.
//
// Every declaration here is a query except SetProcessDpiAwarenessContext,
// which changes only this probe process's DPI mode and must run before any
// window or coordinate measurement, and SetLastError, which only clears the
// calling thread's last-error slot so a zero style word can be told apart from
// a failed GetWindowLongPtrW. No input, activation, window-state or hotkey
// procedure is declared in this file.
var (
	modUser32   = windows.NewLazySystemDLL("user32.dll")
	modShcore   = windows.NewLazySystemDLL("shcore.dll")
	modKernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procSetLastError                  = modKernel32.NewProc("SetLastError")
	procSetProcessDpiAwarenessContext = modUser32.NewProc("SetProcessDpiAwarenessContext")
	procGetWindowDpiAwarenessContext  = modUser32.NewProc("GetWindowDpiAwarenessContext")
	procGetWindowRect                 = modUser32.NewProc("GetWindowRect")
	procGetWindowLongPtrW             = modUser32.NewProc("GetWindowLongPtrW")
	procIsZoomed                      = modUser32.NewProc("IsZoomed")
	procGetForegroundWindow           = modUser32.NewProc("GetForegroundWindow")
	procEnumDisplayMonitors           = modUser32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfoW               = modUser32.NewProc("GetMonitorInfoW")
	procMonitorFromWindow             = modUser32.NewProc("MonitorFromWindow")
	procGetDpiForMonitor              = modShcore.NewProc("GetDpiForMonitor")
)

const (
	// DPI_AWARENESS_CONTEXT handles are the documented negative pseudo-handle
	// values. They are compared by value because the probe only needs to name
	// the context it observes.
	dpiContextUnaware          = ^uintptr(0) // -1
	dpiContextSystem           = ^uintptr(1) // -2
	dpiContextPerMonitor       = ^uintptr(2) // -3
	dpiContextPerMonitorV2     = ^uintptr(3) // -4
	dpiContextUnawareGdiScaled = ^uintptr(4) // -5

	monitorDefaultToNearest = 2
	monitorInfoPrimary      = 1
	mdtEffectiveDPI         = 0

	gwlStyle   = -16
	gwlExStyle = -20
)

func callErr(err error) error {
	var errno syscall.Errno
	if errors.As(err, &errno) && errno != 0 {
		return errno
	}
	return errors.New("the Win32 call failed without a system error")
}

// setProcessDpiAwarenessPMv2 makes this console process per-monitor-v2 aware,
// the same mode MyGo configures for the real application. It must be called
// before any coordinate measurement, otherwise GetClientRect/ClientToScreen
// are DPI-virtualized. A failure is reported; the caller checks the observed
// awareness and refuses capture if it is not PMv2.
func setProcessDpiAwarenessPMv2() error {
	if err := procSetProcessDpiAwarenessContext.Find(); err != nil {
		return fmt.Errorf("SetProcessDpiAwarenessContext unavailable: %w", err)
	}
	ret, _, err := procSetProcessDpiAwarenessContext.Call(dpiContextPerMonitorV2)
	if ret == 0 {
		return fmt.Errorf("SetProcessDpiAwarenessContext(PerMonitorV2): %w", callErr(err))
	}
	return nil
}

func windowDpiAwareness(hwnd uintptr) (name, context string, err error) {
	if findErr := procGetWindowDpiAwarenessContext.Find(); findErr != nil {
		return "", "", fmt.Errorf("GetWindowDpiAwarenessContext unavailable: %w", findErr)
	}
	ctx, _, err := procGetWindowDpiAwarenessContext.Call(uintptr(hwnd))
	if ctx == 0 {
		return "", "", fmt.Errorf("GetWindowDpiAwarenessContext: %w", callErr(err))
	}
	return dpiContextName(ctx), fmt.Sprintf("0x%X", ctx), nil
}

func dpiContextName(ctx uintptr) string {
	switch ctx {
	case dpiContextUnaware:
		return "unaware"
	case dpiContextSystem:
		return "system"
	case dpiContextPerMonitor:
		return "perMonitor"
	case dpiContextPerMonitorV2:
		return "perMonitorV2"
	case dpiContextUnawareGdiScaled:
		return "unawareGdiScaled"
	default:
		return fmt.Sprintf("unknown(0x%X)", ctx)
	}
}

func windowRect(hwnd uintptr) (windows.Rect, error) {
	var rect windows.Rect
	ret, _, err := procGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rect)))
	if ret == 0 {
		return rect, fmt.Errorf("GetWindowRect: %w", callErr(err))
	}
	return rect, nil
}

func windowLong(hwnd uintptr, index int32) (uintptr, error) {
	// GetWindowLongPtrW returns 0 for both a legitimate zero style word and a
	// failure; clear the thread's last error first so the two can be told
	// apart. This only touches the thread's last-error slot.
	procSetLastError.Call(0)
	ret, _, err := procGetWindowLongPtrW.Call(uintptr(hwnd), uintptr(index))
	if ret == 0 {
		if systemErr := callErr(err); systemErr != nil {
			return 0, fmt.Errorf("GetWindowLongPtrW(%d): %w", index, systemErr)
		}
	}
	return ret, nil
}

func isZoomed(hwnd uintptr) bool {
	ret, _, _ := procIsZoomed.Call(uintptr(hwnd))
	return ret != 0
}

func foregroundWindow() uintptr {
	ret, _, _ := procGetForegroundWindow.Call()
	return ret
}

// monitorInfoEx is the Win32 MONITORINFOEXW layout (amd64).
type monitorInfoEx struct {
	cbSize    uint32
	rcMonitor windows.Rect
	rcWork    windows.Rect
	dwFlags   uint32
	szDevice  [32]uint16
}

var (
	monitorMu       sync.Mutex
	monitorHandles  []uintptr
	monitorCallback = syscall.NewCallback(func(hmon uintptr, _ uintptr, _ uintptr, _ uintptr) uintptr {
		monitorHandles = append(monitorHandles, hmon)
		return 1
	})
)

func enumerateMonitors() ([]MonitorFacts, error) {
	monitorMu.Lock()
	defer monitorMu.Unlock()
	monitorHandles = monitorHandles[:0]
	ret, _, err := procEnumDisplayMonitors.Call(0, 0, monitorCallback, 0)
	if ret == 0 {
		return nil, fmt.Errorf("EnumDisplayMonitors: %w", callErr(err))
	}
	monitors := make([]MonitorFacts, 0, len(monitorHandles))
	for _, handle := range monitorHandles {
		facts, factsErr := monitorFacts(handle)
		if factsErr != nil {
			monitors = append(monitors, MonitorFacts{
				Handle:    uint64(handle),
				HandleHex: fmt.Sprintf("0x%X", handle),
				Error:     factsErr.Error(),
			})
			continue
		}
		monitors = append(monitors, facts)
	}
	return monitors, nil
}

func monitorForWindow(hwnd uintptr) (MonitorFacts, error) {
	handle, _, _ := procMonitorFromWindow.Call(uintptr(hwnd), monitorDefaultToNearest)
	if handle == 0 {
		return MonitorFacts{}, errors.New("MonitorFromWindow: no monitor for the window")
	}
	return monitorFacts(handle)
}

func monitorFacts(handle uintptr) (MonitorFacts, error) {
	var info monitorInfoEx
	info.cbSize = uint32(unsafe.Sizeof(info))
	ret, _, err := procGetMonitorInfoW.Call(handle, uintptr(unsafe.Pointer(&info)))
	if ret == 0 {
		return MonitorFacts{}, fmt.Errorf("GetMonitorInfoW: %w", callErr(err))
	}
	facts := MonitorFacts{
		Handle:    uint64(handle),
		HandleHex: fmt.Sprintf("0x%X", handle),
		Device:    windows.UTF16ToString(info.szDevice[:]),
		Primary:   info.dwFlags&monitorInfoPrimary != 0,
		Rect:      rectFacts(info.rcMonitor.Left, info.rcMonitor.Top, info.rcMonitor.Right, info.rcMonitor.Bottom),
		WorkArea:  rectFacts(info.rcWork.Left, info.rcWork.Top, info.rcWork.Right, info.rcWork.Bottom),
	}
	if findErr := procGetDpiForMonitor.Find(); findErr != nil {
		facts.Error = "GetDpiForMonitor unavailable: " + findErr.Error()
		return facts, nil
	}
	var dpiX, dpiY uint32
	hr, _, _ := procGetDpiForMonitor.Call(handle, mdtEffectiveDPI,
		uintptr(unsafe.Pointer(&dpiX)), uintptr(unsafe.Pointer(&dpiY)))
	if hr != 0 || dpiX == 0 {
		facts.Error = fmt.Sprintf("GetDpiForMonitor: HRESULT 0x%08x", int32(hr))
		return facts, nil
	}
	facts.DPI = dpiX
	facts.ScalePercent = dpiScalePercent(dpiX)
	return facts, nil
}

func dpiScalePercent(dpi uint32) int {
	return int((dpi*100 + 48) / 96)
}

// tokenMandatoryLabel is the Win32 TOKEN_MANDATORY_LABEL layout (amd64).
type tokenMandatoryLabel struct {
	Label windows.SIDAndAttributes
}

// processIntegrity reads the integrity level of pid with query-only access.
// It never enables a privilege, changes a token or requests elevation: a
// refusal is returned as an error for the evidence file.
func processIntegrity(pid uint32) IntegrityFacts {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return integrityFailure(fmt.Errorf("OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION): %w", err))
	}
	defer windows.CloseHandle(handle)
	var token windows.Token
	if err := windows.OpenProcessToken(handle, windows.TOKEN_QUERY, &token); err != nil {
		return integrityFailure(fmt.Errorf("OpenProcessToken(TOKEN_QUERY): %w", err))
	}
	defer token.Close()
	return tokenIntegrity(token)
}

// selfIntegrity reads this probe's own integrity level through the
// process-token pseudo handle, which needs no OpenProcess call and no close.
func selfIntegrity() IntegrityFacts {
	return tokenIntegrity(windows.GetCurrentProcessToken())
}

func tokenIntegrity(token windows.Token) IntegrityFacts {
	var needed uint32
	err := windows.GetTokenInformation(token, windows.TokenIntegrityLevel, nil, 0, &needed)
	if err != nil && !errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) {
		return integrityFailure(fmt.Errorf("GetTokenInformation(TokenIntegrityLevel) size query: %w", err))
	}
	if needed == 0 {
		return integrityFailure(errors.New("GetTokenInformation(TokenIntegrityLevel) returned an empty buffer size"))
	}
	// uint64 words keep the buffer 8-byte aligned for the SID pointer in
	// TOKEN_MANDATORY_LABEL; GetTokenInformation writes only `needed` bytes.
	buffer := make([]uint64, (int(needed)+7)/8)
	if err := windows.GetTokenInformation(token, windows.TokenIntegrityLevel,
		(*byte)(unsafe.Pointer(&buffer[0])), needed, &needed); err != nil {
		return integrityFailure(fmt.Errorf("GetTokenInformation(TokenIntegrityLevel): %w", err))
	}
	label := (*tokenMandatoryLabel)(unsafe.Pointer(&buffer[0]))
	if label.Label.Sid == nil {
		return integrityFailure(errors.New("GetTokenInformation(TokenIntegrityLevel) returned a NULL SID"))
	}
	rid, err := sidLastSubAuthority(label.Label.Sid)
	if err != nil {
		return integrityFailure(err)
	}
	return IntegrityFacts{Level: integrityLevelName(rid), RID: rid}
}

func integrityFailure(err error) IntegrityFacts {
	return IntegrityFacts{Level: "unknown", Error: err.Error()}
}

// sidLastSubAuthority reads the last sub-authority of a SID directly from its
// documented layout: revision (1 byte), sub-authority count (1 byte),
// identifier authority (6 bytes), then little-endian uint32 sub-authorities.
// Parsing the bytes avoids converting a Win32-returned uintptr back into an
// unsafe.Pointer; the integrity SID is S-1-16-<rid> with one sub-authority.
func sidLastSubAuthority(sid *windows.SID) (uint32, error) {
	if sid == nil {
		return 0, errors.New("integrity SID is NULL")
	}
	header := unsafe.Slice((*byte)(unsafe.Pointer(sid)), 8)
	count := int(header[1])
	if count == 0 {
		return 0, errors.New("integrity SID has no sub-authority")
	}
	raw := unsafe.Slice((*byte)(unsafe.Pointer(sid)), 8+4*count)
	offset := 8 + 4*(count-1)
	return binary.LittleEndian.Uint32(raw[offset : offset+4]), nil
}

// integrityLevelName maps the mandatory-label RID to the Windows integrity
// level names. Unknown RIDs are reported with their value, never guessed.
func integrityLevelName(rid uint32) string {
	switch rid {
	case 0x0000:
		return "untrusted"
	case 0x1000:
		return "low"
	case 0x2000:
		return "medium"
	case 0x2100:
		return "mediumPlus"
	case 0x3000:
		return "high"
	case 0x4000:
		return "system"
	case 0x5000:
		return "protected"
	default:
		return fmt.Sprintf("unknown(0x%04X)", rid)
	}
}

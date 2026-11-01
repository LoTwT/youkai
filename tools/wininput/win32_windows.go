//go:build windows

package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// This file declares only read-only queries, the process DPI-awareness
// configuration the production application also performs, and GetLastError.
// It declares no input, activation, window-state or hotkey procedure: the
// probe's input path is the platform package's Sender, not a local call.
var (
	modUser32   = windows.NewLazySystemDLL("user32.dll")
	modKernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procSetProcessDpiAwarenessContext = modUser32.NewProc("SetProcessDpiAwarenessContext")
	procGetLastError                  = modKernel32.NewProc("GetLastError")
)

// dpiContextPerMonitorV2 is DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4).
const dpiContextPerMonitorV2 = ^uintptr(3)

func callErr(err error) error {
	var errno syscall.Errno
	if errors.As(err, &errno) && errno != 0 {
		return errno
	}
	return errors.New("the Win32 call failed without a system error")
}

// setProcessDpiAwarenessPMv2 makes this console process per-monitor-v2 aware,
// the same mode the production application configures. It must run before any
// coordinate measurement; a failure is recorded and the observed awareness
// decides whether the run can proceed.
func setProcessDpiAwarenessPMv2() error {
	if err := procSetProcessDpiAwarenessContext.Find(); err != nil {
		return fmt.Errorf("SetProcessDpiAwarenessContext unavailable (needs Windows 10 1703+): %w", err)
	}
	ret, _, err := procSetProcessDpiAwarenessContext.Call(dpiContextPerMonitorV2)
	if ret == 0 {
		return fmt.Errorf("SetProcessDpiAwarenessContext(PerMonitorV2): %w", callErr(err))
	}
	return nil
}

// lastError reads the calling thread's raw last-error value. The caller must
// keep the goroutine on one OS thread; the value is meaningful only there and
// a successful SendInput may leave a stale value behind.
func lastError() uint32 {
	ret, _, _ := procGetLastError.Call()
	return uint32(ret)
}

func lastErrorText(code uint32) string {
	if code == 0 {
		return ""
	}
	return syscall.Errno(code).Error()
}

// tokenMandatoryLabel is the Win32 TOKEN_MANDATORY_LABEL layout (amd64).
type tokenMandatoryLabel struct {
	Label windows.SIDAndAttributes
}

// processIntegrity reads the integrity level of pid with query-only access. It
// never enables a privilege, changes a token or requests elevation: a refusal
// is returned as an error for the evidence file.
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

// selfIntegrity reads this probe's own integrity through the process-token
// pseudo handle, which needs no OpenProcess call and no close.
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

// sidLastSubAuthority reads the last sub-authority of the integrity SID from
// its documented layout: revision (1 byte), sub-authority count (1 byte),
// identifier authority (6 bytes), then little-endian uint32 sub-authorities.
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

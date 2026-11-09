package app

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/platform/windows"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
)

func TestDiagnosticsTextReportsRequiredFields(t *testing.T) {
	at := time.Date(2026, 11, 5, 14, 3, 21, 0, time.FixedZone("CST", 8*60*60))
	text := DiagnosticsText(DiagnosticsInput{
		Version: "0.0.1",
		Commit:  "83ba00c",
		System: SystemInfo{
			OS:            "Windows",
			OSVersion:     "10.0.22631",
			DisplayWidth:  3840,
			DisplayHeight: 2160,
			DisplayScale:  1.25,
		},
		Scan: &DiagnosticsScan{
			End: "failed",
			Trigger: &scan.Trigger{
				Kind:  scan.TriggerFault,
				Code:  "windowOccluded",
				Phase: scan.PhaseCapturing,
				At:    at,
			},
		},
	})

	for _, want := range []string{
		"youkai 诊断信息",
		"版本：0.0.1（构建 83ba00c）",
		"系统：Windows 10.0.22631",
		"显示：3840×2160（缩放 125%）",
		"最近一次扫描：",
		"结束类型：failed",
		"触发码：windowOccluded",
		"首个触发：fault",
		"阶段：capturing",
		"发生时间：2026-11-05 14:03:21 +08:00",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("diagnostic text does not contain %q:\n%s", want, text)
		}
	}
}

func TestDiagnosticsTextNamesAbsentValues(t *testing.T) {
	t.Run("no scan", func(t *testing.T) {
		text := DiagnosticsText(DiagnosticsInput{
			System: SystemInfo{
				OS:            "macOS",
				OSVersion:     "14.6",
				DisplayWidth:  3024,
				DisplayHeight: 1964,
				DisplayScale:  2,
			},
		})
		for _, want := range []string{
			"版本：未知",
			"系统：macOS 14.6",
			"显示：3024×1964（缩放 200%）",
			"最近一次扫描：无",
		} {
			if !strings.Contains(text, want) {
				t.Errorf("diagnostic text does not contain %q:\n%s", want, text)
			}
		}
	})

	t.Run("display without a scale", func(t *testing.T) {
		text := DiagnosticsText(DiagnosticsInput{
			System: SystemInfo{OS: "Windows", DisplayWidth: 1920, DisplayHeight: 1080},
		})
		if want := "显示：1920×1080（缩放未知）"; !strings.Contains(text, want) {
			t.Errorf("diagnostic text does not contain %q:\n%s", want, text)
		}
	})

	t.Run("completed scan", func(t *testing.T) {
		text := DiagnosticsText(DiagnosticsInput{
			Version: "0.0.1",
			System:  SystemInfo{OS: "Windows", OSVersion: "10.0.22631"},
			Scan:    &DiagnosticsScan{End: "completed"},
		})
		for _, want := range []string{
			"结束类型：completed",
			"触发码：无",
			"首个触发：无",
		} {
			if !strings.Contains(text, want) {
				t.Errorf("diagnostic text does not contain %q:\n%s", want, text)
			}
		}
		if strings.Contains(text, "阶段：") {
			t.Errorf("completed scan names a phase without a trigger:\n%s", text)
		}
	})

	t.Run("running scan", func(t *testing.T) {
		text := DiagnosticsText(DiagnosticsInput{
			System: SystemInfo{OS: "Windows"},
			Scan:   &DiagnosticsScan{},
		})
		if !strings.Contains(text, "结束类型：进行中") {
			t.Errorf("running scan does not say it is in progress:\n%s", text)
		}
	})
}

// TestDiagnosticsTextStripsWindowTitlesAndUserPaths pins the privacy
// boundary: a trigger can carry a free-form message and structured
// params from the platform, and callers can hand over user paths in
// otherwise trusted fields. None of it may reach the copied text.
func TestDiagnosticsTextStripsWindowTitlesAndUserPaths(t *testing.T) {
	const (
		title    = "Personal Bank - Google Chrome"
		userName = "alice"
		macPath  = "/Users/alice/Library/Logs/Youkai Dev"
		winPath  = `C:\Users\alice\AppData\Local\Youkai\logs`
	)
	text := DiagnosticsText(DiagnosticsInput{
		Version: "0.0.1 " + macPath,
		Commit:  "83ba00c",
		System: SystemInfo{
			OS:            "Windows 10.0.22631 " + winPath,
			DisplayWidth:  1920,
			DisplayHeight: 1080,
			DisplayScale:  1,
		},
		Scan: &DiagnosticsScan{
			End: "failed",
			Trigger: &scan.Trigger{
				Kind:    scan.TriggerFault,
				Code:    "windowOccluded",
				Message: "blocked by window " + title + " at " + winPath,
				Params: map[string]string{
					"hitTitle":   title,
					"hitProcess": winPath,
					"evidence":   macPath,
				},
				Phase: scan.PhaseCapturing,
			},
		},
	})

	for _, forbidden := range []string{title, userName, "/Users/", `\Users\`, "hitTitle", "hitProcess", "blocked by window"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("diagnostic text leaks %q:\n%s", forbidden, text)
		}
	}
	for _, want := range []string{
		"结束类型：failed",
		"触发码：windowOccluded",
		"首个触发：fault",
		"阶段：capturing",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("diagnostic text does not contain %q:\n%s", want, text)
		}
	}
}

// TestRedactTextHidesHomePathsWithSpaces pins the fallback redaction: a
// profile name may contain spaces, so the user segment must run to the
// next path separator instead of stopping at whitespace.
func TestRedactTextHidesHomePathsWithSpaces(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{
			name: "macOS",
			in:   "/Users/John Smith/Library/Logs/Youkai",
			want: "<用户目录>/Library/Logs/Youkai",
		},
		{
			name: "Windows",
			in:   `C:\Users\John Smith\AppData\Local\Youkai`,
			want: `<用户目录>\AppData\Local\Youkai`,
		},
		{
			name: "Windows spelled in another case",
			in:   `c:\USERS\John Smith\AppData`,
			want: `<用户目录>\AppData`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := redactText(tc.in); got != tc.want {
				t.Fatalf("redactText(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestRedactTextHidesEscapedWindowsProfilePaths covers the text the readiness
// log actually receives: SelectionError.Error quotes the executable with %q,
// so a game under the Windows profile arrives with doubled backslashes. The
// single-backslash table above did not cover that form.
func TestRedactTextHidesEscapedWindowsProfilePaths(t *testing.T) {
	message := (&windows.SelectionError{
		Kind:       windows.SelectionNoWindow,
		Executable: `C:\Users\alice\Games\ZenlessZoneZero.exe`,
	}).Error()
	if !strings.Contains(message, `C:\\Users\\alice`) {
		t.Fatalf("SelectionError no longer spells the quoted path with escaped backslashes; adapt this test to the new text: %q", message)
	}
	got := redactText(message)
	if strings.Contains(got, "alice") {
		t.Fatalf("redactText left the profile name in the quoted path: %q", got)
	}
	if !strings.Contains(got, "<用户目录>") {
		t.Fatalf("redactText did not mark the profile path: %q", got)
	}
}

// TestRedactTextHidesEscapedHomeOutsideUsers pins the fallback for a home
// directory that does not follow the conventional profile root. The readiness
// log receives a %q-quoted executable, so the home path may appear with doubled
// backslashes even though it does not match the generic Users layout.
// os.UserHomeDir reads USERPROFILE on Windows and HOME elsewhere.
func TestRedactTextHidesEscapedHomeOutsideUsers(t *testing.T) {
	const home = `D:\Profiles\alice`
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if got, err := os.UserHomeDir(); err != nil || got != home {
		t.Fatalf("os.UserHomeDir() = %q, %v; cannot inject the home directory", got, err)
	}

	message := (&windows.SelectionError{
		Kind:       windows.SelectionNoWindow,
		Executable: home + `\Games\x.exe`,
	}).Error()
	if !strings.Contains(message, `D:\\Profiles\\alice\\Games`) {
		t.Fatalf("SelectionError no longer spells the quoted path with escaped backslashes; adapt this test to the new text: %q", message)
	}
	got := redactText(message)
	if strings.Contains(got, "alice") {
		t.Fatalf("redactText left the injected home name in the quoted path: %q", got)
	}
	if !strings.Contains(got, "<用户目录>") {
		t.Fatalf("redactText did not mark the injected home path: %q", got)
	}
}

// TestReplaceFoldIgnoresCase covers the os.UserHomeDir replacement:
// Windows may report a home path with different case from the value the
// caller supplied.
func TestReplaceFoldIgnoresCase(t *testing.T) {
	got := replaceFold(`C:\USERS\Alice\AppData`, `C:\Users\Alice`, "<用户目录>")
	if want := `<用户目录>\AppData`; got != want {
		t.Fatalf("replaceFold = %q, want %q", got, want)
	}
}

func TestDiagnosticsScanFromSnapshot(t *testing.T) {
	if got := DiagnosticsScanFromSnapshot(Snapshot{}); got != nil {
		t.Fatalf("empty snapshot = %+v, want nil", got)
	}

	at := time.Date(2026, 11, 5, 14, 3, 21, 0, time.UTC)
	got := DiagnosticsScanFromSnapshot(Snapshot{
		SessionID: "session-1",
		Scan: scan.Snapshot{
			End: scan.EndFailed,
			Trigger: &scan.Trigger{
				Kind:  scan.TriggerFault,
				Code:  "targetLost",
				Phase: scan.PhaseCapturing,
				At:    at,
			},
		},
	})
	if got == nil || got.End != "failed" || got.Trigger == nil {
		t.Fatalf("snapshot summary = %+v, want failed with a trigger", got)
	}
	if got.Trigger.Kind != scan.TriggerFault || got.Trigger.Code != "targetLost" ||
		got.Trigger.Phase != scan.PhaseCapturing || !got.Trigger.At.Equal(at) {
		t.Fatalf("snapshot trigger = %+v, want the kernel's first trigger", got.Trigger)
	}
}

func TestDiagnosticsScanFromTermination(t *testing.T) {
	if got := DiagnosticsScanFromTermination(nil); got != nil {
		t.Fatalf("nil termination = %+v, want nil", got)
	}

	completed := DiagnosticsScanFromTermination(&record.Termination{
		Kind:  record.TerminationCompleted,
		Code:  scan.CodeVerifiedEnd,
		Phase: record.PhaseCapturing,
	})
	if completed == nil || completed.End != "completed" || completed.Trigger != nil {
		t.Fatalf("completed termination = %+v, want completed without a fault trigger", completed)
	}
	text := DiagnosticsText(DiagnosticsInput{Version: "0.0.1", System: SystemInfo{OS: "Windows"}, Scan: completed})
	if strings.Contains(text, scan.CodeVerifiedEnd) {
		t.Errorf("completed termination reported the verified-end code as a fault:\n%s", text)
	}
	if !strings.Contains(text, "触发码：无") {
		t.Errorf("completed termination does not report an absent fault code:\n%s", text)
	}

	failed := DiagnosticsScanFromTermination(&record.Termination{
		Kind:  record.TerminationFailed,
		Code:  "windowOccluded",
		Phase: record.PhasePreparing,
	})
	if failed == nil || failed.Trigger == nil ||
		failed.Trigger.Kind != scan.TriggerFault || failed.Trigger.Code != "windowOccluded" ||
		failed.Trigger.Phase != scan.PhasePreparing {
		t.Fatalf("failed termination = %+v, want a fault trigger", failed)
	}

	cancelled := DiagnosticsScanFromTermination(&record.Termination{
		Kind:  record.TerminationCancelled,
		Code:  "hotkey",
		Phase: record.PhaseCapturing,
	})
	if cancelled == nil || cancelled.Trigger == nil || cancelled.Trigger.Kind != scan.TriggerUser {
		t.Fatalf("cancelled termination = %+v, want a user trigger", cancelled)
	}

	interrupted := DiagnosticsScanFromTermination(&record.Termination{
		Kind:  record.TerminationInterrupted,
		Code:  "interrupted",
		Phase: record.PhaseCapturing,
	})
	if interrupted == nil || interrupted.End != "interrupted" || interrupted.Trigger != nil {
		t.Fatalf("interrupted termination = %+v, want no synthesized trigger", interrupted)
	}
}

func TestDiagnosticsTextRejectsNonCodeTriggerText(t *testing.T) {
	const title = "Personal Bank - Google Chrome"
	text := DiagnosticsText(DiagnosticsInput{
		Version: "0.0.1",
		System:  SystemInfo{OS: "Windows"},
		Scan: &DiagnosticsScan{
			End:     "failed",
			Trigger: &scan.Trigger{Kind: scan.TriggerFault, Code: title, Phase: scan.PhasePreparing},
		},
	})
	if !strings.Contains(text, "触发码：unknown") {
		t.Errorf("malformed trigger code was not replaced:\n%s", text)
	}
	if strings.Contains(text, title) {
		t.Errorf("malformed trigger code reached the text:\n%s", text)
	}
}

func TestDiagnosticsDirectoryIsTheLogDirectory(t *testing.T) {
	paths := Paths{Logs: "/logs/youkai"}
	if got, want := DiagnosticsDirectory(paths), paths.Logs; got != want {
		t.Fatalf("DiagnosticsDirectory = %q, want %q", got, want)
	}
	text := DiagnosticsText(DiagnosticsInput{System: SystemInfo{OS: "macOS"}})
	if strings.Contains(text, paths.Logs) {
		t.Errorf("copied diagnostic text contains the log directory %q:\n%s", paths.Logs, text)
	}
}

func TestCurrentSystemInfoNamesTheOS(t *testing.T) {
	info := CurrentSystemInfo()
	if info.OS == "" {
		t.Fatal("CurrentSystemInfo reports no operating system")
	}
	text := DiagnosticsText(DiagnosticsInput{Version: "test", System: info})
	if !strings.Contains(text, "系统："+info.OS) {
		t.Errorf("diagnostic text does not name the system %q:\n%s", info.OS, text)
	}
}

// TestCurrentSystemInfoReportsDisplayWhenAvailable exercises the
// platform reader on the OS that runs the test. A headless CI host may
// legitimately have no main display; skip there instead of inventing
// numbers.
func TestCurrentSystemInfoReportsDisplayWhenAvailable(t *testing.T) {
	info := CurrentSystemInfo()
	if info.DisplayWidth <= 0 || info.DisplayHeight <= 0 {
		t.Skip("the platform reported no main display")
	}
	if info.DisplayScale <= 0 {
		t.Fatalf("display %d×%d has no scale", info.DisplayWidth, info.DisplayHeight)
	}
	want := fmt.Sprintf("%d×%d", info.DisplayWidth, info.DisplayHeight)
	text := DiagnosticsText(DiagnosticsInput{Version: "test", System: info})
	if !strings.Contains(text, want) {
		t.Errorf("diagnostic text does not contain the display %q:\n%s", want, text)
	}
}

// Command wininput is the single-click Windows input probe. It answers the
// next field question after the read-only capture probe: can this machine's
// SendInput path deliver one harmless left click to the target, and does the
// target visibly react?
//
// HARD CONSTRAINTS:
//
//   - At most one left-button click per run. The tool never drags, never sends
//     wheel or keyboard events, and never retries a click.
//   - The click goes through internal/platform/windows' existing InputActor,
//     RealSender and InputPrecheck. This tool adds no input path of its own.
//   - It never calls SetForegroundWindow, SetWindowPos, ShowWindow, MoveWindow,
//     SetCursorPos, RegisterHotKey, keybd_event or mouse_event, never
//     activates, moves, resizes, restores or focuses a window, and registers
//     no hotkey. The operator must make the target foreground during the
//     countdown; the probe itself waits and then refuses if it is not.
//   - Before clicking, the target must be uniquely locked, foreground, visible,
//     not minimized and not DWM-cloaked, DPI-consistent, in an accepted 16:9
//     client tier (game) and hit-tested inside the target window. Any failure
//     refuses the click and exits non-zero.
//   - A UIPI privilege mismatch is recorded, never bypassed. A medium-integrity
//     probe sending to a high-integrity game is expected to show SendInput's
//     zero-delivery/access-denied shape; run the probe elevated for the
//     comparison. There is no force or bypass switch.
//   - After the click, foreground, identity, visibility and geometry are
//     re-checked before every frame; any change, a black frame, or a frame that
//     does not match the client size aborts the observation. At exit the actor
//     releases only buttons its ledger says this probe pressed.
//   - A successful SendInput return count is not evidence of game action. The
//     verdict comes from before/after frames.
//
// Usage:
//
//	wininput -target notepad -out <dir> [options]
//	wininput -target game -exe <full path> -slot r,c -out <dir> [options]
//
// See docs/development.md for the field usage note. This tool exists only on
// the probe branch; it is not part of the product build.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const usageText = `wininput - one left click through the production Windows input path

usage:
  wininput -target notepad -out <dir> [options]
  wininput -target game -exe <full path> -slot r,c -out <dir> [options]

targets:
  notepad   lock the single manually opened Notepad window (by notepad.exe
            base name, or by -notepad-title) and click its edit-area center
  game      click the center of warehouse cell (r,c) at the 1920x1080
            reference layout (first center 139,185; pitches 130 and 170)

required:
  -target <name>   notepad or game
  -out <dir>       evidence directory (created if missing)
  -exe <path>      full path of the game executable (game only)
  -slot <r,c>      1-based row and column, 1..5 and 1..4 (game only)

options:
  -dry-run             run every check and capture, but do not call SendInput
  -countdown <sec>     seconds to switch to the target before checks (default 5)
  -notepad-title <s>   lock Notepad by title substring instead of notepad.exe

The probe never activates, moves, resizes or focuses a window and registers no
hotkey. It sends at most one left click per run, never a drag, wheel or key
event, and releases only buttons it pressed itself. A UIPI privilege mismatch
is recorded, not bypassed; run elevated for the comparison.

Accepted game client sizes are exact 16:9 tiers:
  1280x720, 1600x900, 1920x1080, 2400x1350, 2560x1440, 2880x1620,
  3200x1800, 3840x2160
Any other client size is refused, not scaled.

conclusions:
  delivered-and-effective   the full click was inserted and a frame showed the effect
  delivered-no-effect       the full click was inserted but no frame met the effect test
  refused-by-precheck       a pre-click premise or the platform precheck refused the click
  dry-run-ready             every check passed; SendInput was suppressed by -dry-run
  error                     setup, capture, observation abort, UIPI block or partial dispatch

exit codes: 0 delivered-and-effective or dry-run-ready; 1 other conclusions;
2 usage error.
`

const (
	TargetNotepad = "notepad"
	TargetGame    = "game"
)

type options struct {
	target       string
	exe          string
	slot         string
	slotRow      int
	slotColumn   int
	out          string
	dryRun       bool
	countdown    int
	notepadTitle string
}

func (o options) mode() string {
	if o.dryRun {
		return "dryRun"
	}
	return "click"
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is the testable entry point. Exit codes: 0 a successful delivery or a
// ready dry run, 1 every other probe conclusion, 2 usage error.
func run(args []string, stdout, stderr io.Writer) int {
	opts, err := parseOptions(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(stderr, "wininput:", err)
		return 2
	}
	if err := os.MkdirAll(opts.out, 0o755); err != nil {
		fmt.Fprintln(stderr, "wininput: create output directory:", err)
		return 1
	}
	report := runInput(opts)
	if report == nil {
		fmt.Fprintln(stderr, "wininput: internal error: no run report")
		return 1
	}
	if err := writeJSON(filepath.Join(opts.out, "run.json"), report); err != nil {
		fmt.Fprintln(stderr, "wininput: write run.json:", err)
		return 1
	}
	summary := RenderSummary(report)
	if err := os.WriteFile(filepath.Join(opts.out, "summary.txt"), []byte(summary), 0o644); err != nil {
		fmt.Fprintln(stderr, "wininput: write summary.txt:", err)
		fmt.Fprint(stdout, summary)
		return 1
	}
	fmt.Fprint(stdout, summary)
	return exitCode(report.Conclusion)
}

func exitCode(conclusion string) int {
	switch Conclusion(conclusion) {
	case ConclusionDeliveredEffective, ConclusionDryRunReady:
		return 0
	default:
		return 1
	}
}

func parseOptions(args []string, stderr io.Writer) (options, error) {
	var opts options
	fs := flag.NewFlagSet("wininput", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, usageText) }
	fs.StringVar(&opts.target, "target", "", "target kind: notepad or game")
	fs.StringVar(&opts.exe, "exe", "", "full path of the game executable")
	fs.StringVar(&opts.slot, "slot", "", "1-based row,column of the warehouse cell")
	fs.StringVar(&opts.out, "out", "", "evidence output directory")
	fs.BoolVar(&opts.dryRun, "dry-run", false, "run all checks and captures without calling SendInput")
	fs.IntVar(&opts.countdown, "countdown", 5, "seconds to switch to the target before checks")
	fs.StringVar(&opts.notepadTitle, "notepad-title", "", "lock Notepad by title substring instead of notepad.exe")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if fs.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if opts.target != TargetNotepad && opts.target != TargetGame {
		return options{}, fmt.Errorf("-target must be %q or %q, got %q", TargetNotepad, TargetGame, opts.target)
	}
	if opts.out == "" {
		return options{}, errors.New("-out is required")
	}
	if opts.countdown < 0 || opts.countdown > 120 {
		return options{}, fmt.Errorf("-countdown must be within 0..120 seconds, got %d", opts.countdown)
	}
	switch opts.target {
	case TargetGame:
		if opts.exe == "" {
			return options{}, errors.New("-exe is required for -target game")
		}
		if !isAbsoluteWindowsPath(opts.exe) {
			return options{}, fmt.Errorf("-exe must be an absolute path, got %q", opts.exe)
		}
		if opts.slot == "" {
			return options{}, errors.New("-slot is required for -target game")
		}
		row, column, err := ParseSlotPar(opts.slot)
		if err != nil {
			return options{}, err
		}
		opts.slotRow, opts.slotColumn = row, column
		if opts.notepadTitle != "" {
			return options{}, errors.New("-notepad-title is only valid for -target notepad")
		}
	case TargetNotepad:
		if opts.exe != "" {
			return options{}, errors.New("-exe is only valid for -target game")
		}
		if opts.slot != "" {
			return options{}, errors.New("-slot is only valid for -target game")
		}
	}
	return opts, nil
}

// isAbsoluteWindowsPath accepts drive-qualified, UNC and host-native absolute
// paths. It is deliberately independent of the host running the unit tests, so
// the Windows examples can be parsed on macOS.
func isAbsoluteWindowsPath(p string) bool {
	if len(p) >= 3 {
		c := p[0]
		if (c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z') && p[1] == ':' && (p[2] == '\\' || p[2] == '/') {
			return true
		}
	}
	if strings.HasPrefix(p, `\\`) || strings.HasPrefix(p, "//") {
		return true
	}
	return filepath.IsAbs(p)
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

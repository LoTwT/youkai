// Command winprobe is the read-only Windows field probe for the local game
// client. It answers the first field questions: can the game window be
// identified uniquely, are its client geometry and DPI facts consistent, and
// does the platform's GDI client-area capture produce usable screenshots?
//
// HARD CONSTRAINT: this tool never sends input and never changes the state of
// the target window. It does not call SendInput, SetForegroundWindow,
// SetWindowPos, ShowWindow, MoveWindow, RegisterHotKey, SetCursorPos or any
// platform-layer wrapper around them. It does not activate, move, resize,
// restore or focus the game window, and it registers no hotkey. Its only
// window-related calls are reads: enumeration, geometry, styles, DPI
// awareness, foreground observation (GetForegroundWindow, never Set...), and
// GDI BitBlt of the desktop. Do not add input, activation, window-state or
// hotkey capability to this tool.
//
// Usage:
//
//	winprobe -exe <full path to the game exe> -out <evidence dir> [-frames N] [-interval MS] [-list]
//
// The evidence directory receives window.json, frames.json, summary.txt and
// frame-NNNN.png files. See docs/development.md for the field usage note.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const usageText = `winprobe - read-only Windows capture probe (never sends input)

usage:
  winprobe -exe <full path to game exe> -out <evidence dir> [options]

required:
  -exe <path>      full path of the game executable whose window is probed
  -out <dir>       evidence directory (created if missing)

options:
  -frames <n>      number of client-area frames to capture (default 30)
  -interval <ms>   target interval between frame starts in milliseconds (default 100)
  -list            list candidate windows and measured facts; do not select or capture

The probe locks exactly one visible, non-cloaked top-level window whose image
path matches -exe (case-insensitive). It never guesses among several windows:
no window, several candidates or a hidden target is an error and a non-zero
exit. It never sends input, activates, moves or resizes a window, or registers
a hotkey.

outputs in -out:
  window.json      candidates, locked target, geometry, displays, DPI, integrity
  frames.json      per-frame timing, size, mean/variance, content hash, geometry version
  frame-NNNN.png   client-area captures (capture mode only)
  summary.txt      human-readable verdict, black/repeat counts, errors
`

type options struct {
	exe      string
	out      string
	frames   int
	interval int // milliseconds
	list     bool
}

func (o options) mode() string {
	if o.list {
		return "list"
	}
	return "capture"
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is the testable entry point. Exit codes: 0 success, 1 probe or evidence
// failure, 2 usage error.
func run(args []string, stdout, stderr io.Writer) int {
	opts, err := parseOptions(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(stderr, "winprobe:", err)
		return 2
	}
	if err := os.MkdirAll(opts.out, 0o755); err != nil {
		fmt.Fprintln(stderr, "winprobe: create output directory:", err)
		return 1
	}

	windowReport, framesReport := runProbe(opts)
	if windowReport == nil {
		// runProbe always returns a report; keep the guard so a future bug
		// cannot panic after the evidence directory was created.
		fmt.Fprintln(stderr, "winprobe: internal error: no window report")
		return 1
	}
	writeErrs := writeReports(opts.out, windowReport, framesReport)
	windowReport.Errors = append(windowReport.Errors, writeErrs...)

	summary := RenderSummary(windowReport, framesReport)
	fmt.Fprint(stdout, summary)
	if err := os.WriteFile(filepath.Join(opts.out, "summary.txt"), []byte(summary), 0o644); err != nil {
		fmt.Fprintln(stderr, "winprobe: write summary.txt:", err)
		return 1
	}
	if hasErrors(windowReport, framesReport) {
		return 1
	}
	return 0
}

func parseOptions(args []string, stderr io.Writer) (options, error) {
	var opts options
	fs := flag.NewFlagSet("winprobe", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, usageText) }
	fs.StringVar(&opts.exe, "exe", "", "full path of the game executable")
	fs.StringVar(&opts.out, "out", "", "evidence output directory")
	fs.IntVar(&opts.frames, "frames", 30, "number of frames to capture")
	fs.IntVar(&opts.interval, "interval", 100, "target interval between frame starts in milliseconds")
	fs.BoolVar(&opts.list, "list", false, "list candidates only; do not select or capture")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if fs.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if opts.exe == "" {
		return options{}, errors.New("-exe is required")
	}
	if opts.out == "" {
		return options{}, errors.New("-out is required")
	}
	if !filepath.IsAbs(opts.exe) {
		return options{}, fmt.Errorf("-exe must be a full path, got %q", opts.exe)
	}
	if opts.frames < 0 {
		return options{}, errors.New("-frames must not be negative")
	}
	if !opts.list && opts.frames == 0 {
		return options{}, errors.New("-frames must be at least 1 in capture mode")
	}
	if opts.interval < 0 {
		return options{}, errors.New("-interval must not be negative")
	}
	opts.exe = filepath.Clean(opts.exe)
	return opts, nil
}

func writeReports(out string, windowReport *WindowReport, framesReport *FramesReport) []string {
	var errs []string
	if err := writeJSON(filepath.Join(out, "window.json"), windowReport); err != nil {
		errs = append(errs, "write window.json: "+err.Error())
	}
	if framesReport != nil {
		if err := writeJSON(filepath.Join(out, "frames.json"), framesReport); err != nil {
			errs = append(errs, "write frames.json: "+err.Error())
		}
	}
	return errs
}

func hasErrors(windowReport *WindowReport, framesReport *FramesReport) bool {
	if windowReport != nil && len(windowReport.Errors) > 0 {
		return true
	}
	if framesReport != nil && len(framesReport.Errors) > 0 {
		return true
	}
	return false
}

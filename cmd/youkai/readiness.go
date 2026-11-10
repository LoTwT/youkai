package main

import (
	"log"
	"path/filepath"
	"time"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/ui"
	"github.com/egoist/mygo"
)

// readinessCheckEnv is the developer-only switch that runs the read-only
// preparation check (R-01) and exits. It exists so a real-machine round can
// inspect the checklist before PR-17 wires the scanning page: no session,
// scan occupancy, hotkey or input is involved.
const readinessCheckEnv = "YOUKAI_READINESS_CHECK"

// runReadinessCheck executes the checklist. A value that parses as a
// positive duration (for example "60s") repeats the check once per second
// for that long; any other non-empty value runs it once.
//
// The process deliberately does not start MyGo's event loop and does not
// create a window: the check is defined to leave the game untouched, and a
// foreground youkai window would take the game's focus. Logs go to stderr,
// so on Windows redirect them to a file.
func runReadinessCheck(value string) {
	if err := prepareReadinessDPI(); err != nil {
		// The packaged manifest may already set PMv2; a failed call then
		// is expected, and the check below reports the actual awareness.
		log.Printf("youkai: readiness: set PMv2 DPI awareness: %v", err)
	}
	paths, err := app.Resolve(app.Options{
		Name:      mygo.App.Name(),
		Resources: resourcesRoot(),
	})
	if err != nil {
		log.Fatalf("youkai: readiness: %v", err)
	}
	source, _ := scanSource(filepath.Join(paths.Logs, "diagnostics"))
	checker := app.NewReadiness(paths, source)

	interval, total := readinessSchedule(value)
	deadline := time.Now().Add(total)
	for {
		logReadiness(checker.Check(nil))
		if total <= 0 || !time.Now().Before(deadline) {
			return
		}
		time.Sleep(interval)
	}
}

// readinessSchedule parses the switch: a positive duration repeats every
// second until it elapses; anything else runs once.
func readinessSchedule(value string) (interval, total time.Duration) {
	if d, err := time.ParseDuration(value); err == nil && d > 0 {
		return time.Second, d
	}
	return 0, 0
}

func logReadiness(report app.ReadinessReport) {
	log.Printf("youkai: readiness checked at %s", report.CheckedAt.Format(time.RFC3339Nano))
	for _, check := range report.Checks {
		log.Printf("youkai: readiness: item=%s verdict=%s current=%q required=%q code=%q at=%s",
			check.Item, check.Verdict, check.Current, check.Required, check.FaultCode,
			check.CheckedAt.Format(time.RFC3339Nano))
		if copy := ui.ReadinessFailureText(check.FaultCode, check.FaultParams); copy != "" {
			log.Printf("youkai: readiness: item=%s copy=%q", check.Item, copy)
		}
	}
	for _, candidate := range report.Candidates {
		log.Printf("youkai: readiness: candidate hwnd=%d pid=%d class=%q exe=%q",
			candidate.Handle, candidate.PID, candidate.ClassName, candidate.ExecutablePath)
	}
	if report.AccessDeniedWindows > 0 || report.UnreadableGameWindows > 0 {
		log.Printf("youkai: readiness: unreadable game windows: elevation-hint=%d generic=%d",
			report.AccessDeniedWindows, report.UnreadableGameWindows)
	}
}

package app

import (
	"bytes"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/record"
)

// readinessFakeSource serves one platform report and records whether any
// start or recognizer work happened.
type readinessFakeSource struct {
	report    platform.ReadinessReport
	err       error
	gotTarget *TargetCandidate
	starts    int
}

func (s *readinessFakeSource) NewScan() (ScanSpec, error) {
	s.starts++
	return ScanSpec{}, os.ErrInvalid
}

func (s *readinessFakeSource) CheckReadiness(target *TargetCandidate) (platform.ReadinessReport, error) {
	s.gotTarget = target
	return s.report, s.err
}

func writeRecognitionResources(t *testing.T, paths Paths) {
	t.Helper()
	res, err := OCRResources(paths.Resources)
	if err != nil {
		t.Fatalf("OCRResources: %v", err)
	}
	for _, path := range []string{res.LibraryPath, res.ModelPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("artifact"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func checkByItem(t *testing.T, report ReadinessReport, item ReadinessItem) ReadinessCheck {
	t.Helper()
	for _, check := range report.Checks {
		if check.Item == item {
			return check
		}
	}
	t.Fatalf("readiness row %s missing in %+v", item, report.Checks)
	return ReadinessCheck{}
}

func TestServiceCheckReadinessAssemblesEveryRowWithoutStarting(t *testing.T) {
	paths := newTestPaths(t)
	writeRecognitionResources(t, paths)
	checkedAt := time.Now().UTC().Add(-time.Second)
	source := &readinessFakeSource{report: platform.ReadinessReport{
		At: checkedAt,
		Candidates: []platform.TargetCandidate{{
			Handle: 7, PID: 42, ExecutablePath: `C:\game\ZenlessZoneZero.exe`, ClassName: "UnityWndClass",
		}},
		AccessDeniedWindows:   1,
		UnreadableGameWindows: 2,
		Checks: []platform.ReadinessCheck{
			{Item: platform.ReadinessTargetClient, Verdict: platform.ReadinessSatisfied,
				Current: "candidates=1", Required: "confirmed=1", At: checkedAt},
			{Item: platform.ReadinessAdministrator, Verdict: platform.ReadinessUnsatisfied,
				Current: "target=high self=medium", Required: "target<=self",
				Fault: &platform.Fault{Code: platform.CodeIntegrityMismatch, Params: map[string]string{"target": "high", "self": "medium"}}, At: checkedAt},
			{Item: platform.ReadinessGameResolution, Verdict: platform.ReadinessUnknown,
				Current: "1920x1080", Required: "1920x1080",
				Fault: &platform.Fault{Code: platform.CodeClientTierNotOpened}, At: checkedAt},
			{Item: platform.ReadinessWindowPosition, Verdict: platform.ReadinessSatisfied,
				Current: "10,20 1920x1080", Required: "workArea=0,0 1920x1040", At: checkedAt},
			{Item: platform.ReadinessDisplayScale, Verdict: platform.ReadinessSatisfied,
				Current: "self=perMonitorV2 window=96 monitor=96", Required: "self=perMonitorV2 window=monitor", At: checkedAt},
			// Rows this layer never probes must stay "after start" even if a
			// source claims a verdict for the same name, and unknown-only
			// rows are ignored rather than leaking into the checklist.
			{Item: platform.ReadinessItem("stopHotkey"), Verdict: platform.ReadinessSatisfied},
			{Item: platform.ReadinessItem("gamePage"), Verdict: platform.ReadinessSatisfied},
			{Item: "unknownPlatformRow", Verdict: platform.ReadinessSatisfied},
		},
	}}
	recognizerCalled := false
	svc, dataLock := newTestService(t, paths, source, func() (Recognizer, error) {
		recognizerCalled = true
		return newFakeRecognizer(), nil
	})

	// Holding the scan slot must not prevent the read-only check: it never
	// competes for that occupancy.
	held, err := dataLock.AcquireScanLock()
	if err != nil {
		t.Fatalf("acquire the scan lock before the check: %v", err)
	}
	report := svc.CheckReadiness(nil)
	if err := held.Close(); err != nil {
		t.Fatalf("release the scan lock: %v", err)
	}

	if source.starts != 0 {
		t.Fatalf("the readiness check started %d scans", source.starts)
	}
	if recognizerCalled {
		t.Fatal("the readiness check created the recognizer")
	}
	if len(report.Checks) != 9 {
		t.Fatalf("checks = %+v, want the nine 08 §1.1 rows", report.Checks)
	}
	if len(report.Candidates) != 1 || report.Candidates[0].Handle != 7 {
		t.Fatalf("candidates = %+v", report.Candidates)
	}
	if report.AccessDeniedWindows != 1 || report.UnreadableGameWindows != 2 {
		t.Fatalf("discovery counts = %d/%d", report.AccessDeniedWindows, report.UnreadableGameWindows)
	}
	if report.CheckedAt.IsZero() {
		t.Fatal("the report has no check time")
	}

	target := checkByItem(t, report, ReadinessTargetClient)
	if target.Verdict != ReadinessSatisfied || target.Current != "candidates=1" {
		t.Fatalf("target row = %+v", target)
	}
	admin := checkByItem(t, report, ReadinessAdministrator)
	if admin.Verdict != ReadinessUnsatisfied || admin.FaultCode != platform.CodeIntegrityMismatch ||
		admin.FaultParams["target"] != "high" || admin.FaultParams["self"] != "medium" {
		t.Fatalf("administrator row = %+v", admin)
	}
	resolution := checkByItem(t, report, ReadinessGameResolution)
	if resolution.Verdict != ReadinessUnknown || resolution.FaultCode != platform.CodeClientTierNotOpened {
		t.Fatalf("resolution row = %+v", resolution)
	}
	position := checkByItem(t, report, ReadinessWindowPosition)
	if position.Verdict != ReadinessSatisfied {
		t.Fatalf("position row = %+v", position)
	}
	dpi := checkByItem(t, report, ReadinessDisplayScale)
	if dpi.Verdict != ReadinessSatisfied {
		t.Fatalf("display-scale row = %+v", dpi)
	}
	// The two rows the platform never probes stay "after start" even when a
	// source claims a verdict for the same name.
	hotkey := checkByItem(t, report, ReadinessStopHotkey)
	if hotkey.Verdict != ReadinessCheckedAfterStart {
		t.Fatalf("stop hotkey = %s, want checkedAfterStart", hotkey.Verdict)
	}
	page := checkByItem(t, report, ReadinessGamePage)
	if page.Verdict != ReadinessCheckedAfterStart {
		t.Fatalf("game page = %s, want checkedAfterStart", page.Verdict)
	}
	disk := checkByItem(t, report, ReadinessDiskSpace)
	if disk.Required != "1073741824" || disk.CheckedAt.IsZero() {
		t.Fatalf("disk row = %+v", disk)
	}
	resources := checkByItem(t, report, ReadinessRecognitionResources)
	if resources.Verdict != ReadinessSatisfied || resources.Current != "present" {
		t.Fatalf("resource row = %+v", resources)
	}
	for _, check := range report.Checks {
		if check.CheckedAt.IsZero() {
			t.Fatalf("row %s has no check time", check.Item)
		}
	}

	// The scan occupancy is free again: the check only borrowed nothing.
	free, err := dataLock.AcquireScanLock()
	if err != nil {
		t.Fatalf("the readiness check left the scan occupancy held: %v", err)
	}
	if err := free.Close(); err != nil {
		t.Fatalf("release the scan lock: %v", err)
	}
}

func TestReadinessWithoutASourceLeavesPlatformRowsUnknown(t *testing.T) {
	paths := newTestPaths(t)
	writeRecognitionResources(t, paths)
	svc, _ := newTestService(t, paths, nil, func() (Recognizer, error) { return newFakeRecognizer(), nil })
	report := svc.CheckReadiness(nil)
	for _, item := range []ReadinessItem{
		ReadinessTargetClient,
		ReadinessAdministrator,
		ReadinessGameResolution,
		ReadinessWindowPosition,
		ReadinessDisplayScale,
	} {
		if got := checkByItem(t, report, item).Verdict; got != ReadinessUnknown {
			t.Fatalf("%s without a source = %s, want unknown", item, got)
		}
	}
	if got := checkByItem(t, report, ReadinessStopHotkey).Verdict; got != ReadinessCheckedAfterStart {
		t.Fatalf("stop hotkey = %s", got)
	}
	if got := checkByItem(t, report, ReadinessGamePage).Verdict; got != ReadinessCheckedAfterStart {
		t.Fatalf("game page = %s", got)
	}
}

func TestReadinessDiskSpaceVerdicts(t *testing.T) {
	at := time.Now()
	cases := []struct {
		name      string
		free      int64
		known     bool
		verdict   ReadinessVerdict
		wantValue string
	}{
		{"enough", ReadinessMinFreeBytes, true, ReadinessSatisfied, "1073741824"},
		{"too little", ReadinessMinFreeBytes - 1, true, ReadinessUnsatisfied, "1073741823"},
		{"unreadable volume", 0, false, ReadinessUnknown, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			check := readinessDiskSpaceVerdict(tc.free, tc.known, at)
			if check.Verdict != tc.verdict || check.Current != tc.wantValue {
				t.Fatalf("check = %+v, want %s current %q", check, tc.verdict, tc.wantValue)
			}
			if check.Required != "1073741824" {
				t.Fatalf("required = %q", check.Required)
			}
		})
	}
}

func TestReadinessRecognitionResourcesVerdicts(t *testing.T) {
	paths := newTestPaths(t)
	writeRecognitionResources(t, paths)
	if check := readinessRecognitionResources(paths.Resources, time.Now()); check.Verdict != ReadinessSatisfied {
		t.Fatalf("complete resources = %+v", check)
	}
	res, err := OCRResources(paths.Resources)
	if err != nil {
		t.Fatalf("OCRResources: %v", err)
	}
	if err := os.Remove(res.ModelPath); err != nil {
		t.Fatal(err)
	}
	if check := readinessRecognitionResources(paths.Resources, time.Now()); check.Verdict != ReadinessUnsatisfied ||
		check.Current != "missing=model" {
		t.Fatalf("missing model = %+v", check)
	}
	if check := readinessRecognitionResources("", time.Now()); check.Verdict != ReadinessUnknown {
		t.Fatalf("unresolved resources = %+v", check)
	}
}

// readinessFakePort satisfies the optional platform capability without any
// native surface.
type readinessFakePort struct {
	platform.Port
	got platform.ReadinessRequest
}

func (p *readinessFakePort) CheckReadiness(req platform.ReadinessRequest) (platform.ReadinessReport, error) {
	p.got = req
	return platform.ReadinessReport{}, nil
}

func TestGameScanSourceReadinessPassesTheConfirmedTarget(t *testing.T) {
	port := &readinessFakePort{}
	src := &GameScanSource{Port: port, Client: record.ClientWindowsLocal}
	target := &TargetCandidate{Handle: 7, PID: 42, ProcessStartedAt: time.Unix(100, 0)}
	if _, err := src.CheckReadiness(target); err != nil {
		t.Fatalf("source readiness: %v", err)
	}
	if port.got.Target.HandleHint != 7 || port.got.Target.PIDHint != 42 ||
		!port.got.Target.ProcessStartedAtHint.Equal(time.Unix(100, 0)) {
		t.Fatalf("confirmed target was not passed through: %+v", port.got.Target)
	}
	if port.got.Display == nil || len(port.got.Display.OpenTiers) == 0 {
		t.Fatalf("the Windows local source did not pass its display policy: %+v", port.got.Display)
	}

	fixed := &GameScanSource{
		Port:   port,
		Client: record.ClientWindowsLocal,
		Target: platform.TargetSpec{ExecutablePath: `C:\game\ZenlessZoneZero.exe`},
	}
	if _, err := fixed.CheckReadiness(target); err != nil {
		t.Fatalf("fixed-target source readiness: %v", err)
	}
	if port.got.Target.HandleHint != 0 || port.got.Target.ExecutablePath != `C:\game\ZenlessZoneZero.exe` {
		t.Fatalf("a fixed-target source must keep its configured target: %+v", port.got.Target)
	}
}

// captureReadinessLog redirects the standard logger for one test. The
// readiness page re-checks every ten seconds, so these tests inspect how many
// lines one repeated condition produces.
func captureReadinessLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	logs := &bytes.Buffer{}
	previous := log.Writer()
	log.SetOutput(logs)
	t.Cleanup(func() { log.SetOutput(previous) })
	return logs
}

func readinessTargetFault(code, message string, verdict platform.ReadinessVerdict) platform.ReadinessReport {
	return platform.ReadinessReport{Checks: []platform.ReadinessCheck{{
		Item:     platform.ReadinessTargetClient,
		Verdict:  verdict,
		Current:  "candidates=0",
		Required: "confirmed=1",
		Fault:    &platform.Fault{Code: code, Message: message},
	}}}
}

func readinessTargetSatisfied() platform.ReadinessReport {
	return platform.ReadinessReport{Checks: []platform.ReadinessCheck{{
		Item:     platform.ReadinessTargetClient,
		Verdict:  platform.ReadinessSatisfied,
		Current:  "candidates=1",
		Required: "confirmed=1",
	}}}
}

// TestServiceCheckReadinessLogsFaultChangesOnly pins the log's state-change
// rule: repeated passes with the same fault stay quiet, a different fault and
// the recovery each produce exactly one line, and the line carries the
// platform message that used to be discarded.
func TestServiceCheckReadinessLogsFaultChangesOnly(t *testing.T) {
	paths := newTestPaths(t)
	source := &readinessFakeSource{report: readinessTargetFault(
		platform.CodeTargetNotFound, "first cause", platform.ReadinessUnsatisfied)}
	svc, _ := newTestService(t, paths, source, func() (Recognizer, error) { return newFakeRecognizer(), nil })
	logs := captureReadinessLog(t)

	svc.CheckReadiness(nil)
	svc.CheckReadiness(nil)
	if got := strings.Count(logs.String(), "fault item=targetClient"); got != 1 {
		t.Fatalf("the unchanged fault wrote %d lines, want 1:\n%s", got, logs)
	}
	if !strings.Contains(logs.String(), `message="first cause"`) {
		t.Fatalf("the fault message is missing from the log:\n%s", logs)
	}

	source.report = readinessTargetFault(
		platform.CodeTargetAmbiguous, "second cause", platform.ReadinessUnsatisfied)
	svc.CheckReadiness(nil)
	svc.CheckReadiness(nil)
	if got := strings.Count(logs.String(), "code=targetAmbiguous"); got != 1 {
		t.Fatalf("the changed fault wrote %d lines, want 1:\n%s", got, logs)
	}
	if got := strings.Count(logs.String(), "fault item=targetClient"); got != 2 {
		t.Fatalf("total fault lines = %d, want 2:\n%s", got, logs)
	}
	if !strings.Contains(logs.String(), `message="second cause"`) {
		t.Fatalf("the changed fault message is missing from the log:\n%s", logs)
	}

	source.report = readinessTargetSatisfied()
	svc.CheckReadiness(nil)
	svc.CheckReadiness(nil)
	if got := strings.Count(logs.String(), "fault cleared item=targetClient"); got != 1 {
		t.Fatalf("the recovery wrote %d lines, want 1:\n%s", got, logs)
	}
	if got := strings.Count(logs.String(), "fault item=targetClient"); got != 2 {
		t.Fatalf("the satisfied pass wrote a fault line:\n%s", logs)
	}
}

// TestServiceCheckReadinessRedactsHomePaths covers both free-text fields the
// readiness log carries: a row's platform message and the platform error.
// The target-selection message no longer includes a directory, but redaction
// remains the fallback for any other message that carries a home path.
func TestServiceCheckReadinessRedactsHomePaths(t *testing.T) {
	quoted := `no top-level window of "C:\Users\alice\Games\ZenlessZoneZero.exe"`
	assertRedacted := func(t *testing.T, logs string) {
		t.Helper()
		if strings.Contains(logs, "alice") {
			t.Fatalf("the readiness log leaks the profile name:\n%s", logs)
		}
		if !strings.Contains(logs, "<用户目录>") {
			t.Fatalf("the readiness log does not mark the profile path:\n%s", logs)
		}
	}

	t.Run("fault message", func(t *testing.T) {
		paths := newTestPaths(t)
		source := &readinessFakeSource{report: readinessTargetFault(
			platform.CodeTargetNotFound, quoted, platform.ReadinessUnsatisfied)}
		svc, _ := newTestService(t, paths, source, func() (Recognizer, error) { return newFakeRecognizer(), nil })
		logs := captureReadinessLog(t)
		svc.CheckReadiness(nil)
		assertRedacted(t, logs.String())
	})

	t.Run("platform error", func(t *testing.T) {
		paths := newTestPaths(t)
		source := &readinessFakeSource{err: errors.New("display policy: " + quoted)}
		svc, _ := newTestService(t, paths, source, func() (Recognizer, error) { return newFakeRecognizer(), nil })
		logs := captureReadinessLog(t)
		svc.CheckReadiness(nil)
		assertRedacted(t, logs.String())
	})
}

// TestServiceCheckReadinessLogsResourceRowWithoutFaultCode pins the blocking
// row that has no fault code: it still writes its failure, keeps the first
// line's current value as evidence, stays quiet while unchanged, and reports
// its clearing.
func TestServiceCheckReadinessLogsResourceRowWithoutFaultCode(t *testing.T) {
	paths := newTestPaths(t)
	source := &readinessFakeSource{report: readinessTargetSatisfied()}
	svc, _ := newTestService(t, paths, source, func() (Recognizer, error) { return newFakeRecognizer(), nil })
	logs := captureReadinessLog(t)

	report := svc.CheckReadiness(nil)
	resources := checkByItem(t, report, ReadinessRecognitionResources)
	if resources.Verdict != ReadinessUnsatisfied || resources.FaultCode != "" {
		t.Fatalf("resource row = %+v, want unsatisfied without a fault code", resources)
	}
	if got := strings.Count(logs.String(), "fault item=recognitionResources"); got != 1 {
		t.Fatalf("the unsatisfied resource row wrote %d fault lines, want 1:\n%s", got, logs)
	}
	if !strings.Contains(logs.String(), `current="missing=runtime,model"`) {
		t.Fatalf("the first resource line does not carry the current value:\n%s", logs)
	}

	svc.CheckReadiness(nil)
	if got := strings.Count(logs.String(), "fault item=recognitionResources"); got != 1 {
		t.Fatalf("the unchanged resource row wrote %d fault lines, want 1:\n%s", got, logs)
	}

	writeRecognitionResources(t, paths)
	svc.CheckReadiness(nil)
	if got := strings.Count(logs.String(), "fault cleared item=recognitionResources"); got != 1 {
		t.Fatalf("the recovered resource row wrote %d cleared lines, want 1:\n%s", got, logs)
	}
}

// TestReadinessLogDiskSpaceIgnoresChangingFreeBytes pins the comparison rule
// for the other code-less blocker: free bytes change on every pass, so the
// key must omit current while the first line still carries it.
func TestReadinessLogDiskSpaceIgnoresChangingFreeBytes(t *testing.T) {
	logs := captureReadinessLog(t)
	state := &readinessLogState{}
	disk := ReadinessCheck{Item: ReadinessDiskSpace, Verdict: ReadinessUnsatisfied,
		Current: "1073741823", Required: "1073741824"}

	state.observe(ReadinessReport{Checks: []ReadinessCheck{disk}}, nil)
	if got := strings.Count(logs.String(), "fault item=diskSpace"); got != 1 {
		t.Fatalf("the unsatisfied disk row wrote %d fault lines, want 1:\n%s", got, logs)
	}
	if !strings.Contains(logs.String(), `fault item=diskSpace verdict=unsatisfied code= current="1073741823"`) {
		t.Fatalf("the first disk line does not carry the current value:\n%s", logs)
	}

	disk.Current = "1073741822"
	state.observe(ReadinessReport{Checks: []ReadinessCheck{disk}}, nil)
	if got := strings.Count(logs.String(), "fault item=diskSpace"); got != 1 {
		t.Fatalf("a changed free-byte count wrote %d fault lines, want 1:\n%s", got, logs)
	}
}

// TestServiceCheckReadinessLogsChangedCauseUnderSameCode pins the message as
// part of the state key: the same verdict and code with a different measured
// cause must write a new line, while an unchanged report stays quiet.
func TestServiceCheckReadinessLogsChangedCauseUnderSameCode(t *testing.T) {
	paths := newTestPaths(t)
	source := &readinessFakeSource{report: platform.ReadinessReport{Checks: []platform.ReadinessCheck{{
		Item:     platform.ReadinessGameResolution,
		Verdict:  platform.ReadinessUnsatisfied,
		Current:  "1280x720",
		Required: "1920x1080",
		Fault: &platform.Fault{Code: platform.CodeClientTierNotOpened,
			Message: "client area 1280x720 is a known tier but no profile for it has field acceptance"},
	}}}}
	svc, _ := newTestService(t, paths, source, func() (Recognizer, error) { return newFakeRecognizer(), nil })
	logs := captureReadinessLog(t)

	svc.CheckReadiness(nil)
	svc.CheckReadiness(nil)
	if got := strings.Count(logs.String(), "fault item=gameResolution"); got != 1 {
		t.Fatalf("the unchanged resolution fault wrote %d lines, want 1:\n%s", got, logs)
	}

	source.report = platform.ReadinessReport{Checks: []platform.ReadinessCheck{{
		Item:     platform.ReadinessGameResolution,
		Verdict:  platform.ReadinessUnsatisfied,
		Current:  "2560x1440",
		Required: "1920x1080",
		Fault: &platform.Fault{Code: platform.CodeClientTierNotOpened,
			Message: "client area 2560x1440 is a known tier but no profile for it has field acceptance"},
	}}}
	svc.CheckReadiness(nil)
	if got := strings.Count(logs.String(), "fault item=gameResolution"); got != 2 {
		t.Fatalf("a changed cause under the same code wrote %d lines, want 2:\n%s", got, logs)
	}
	if !strings.Contains(logs.String(), `message="client area 2560x1440`) {
		t.Fatalf("the changed resolution cause is missing:\n%s", logs)
	}
}

// TestServiceCheckReadinessPlatformFailureKeepsRowState pins that a failed
// platform check is not a recovery: the row fault stays remembered, so the
// same fault is not logged again after the platform returns, and no cleared
// line appears until the row itself is satisfied.
func TestServiceCheckReadinessPlatformFailureKeepsRowState(t *testing.T) {
	paths := newTestPaths(t)
	source := &readinessFakeSource{report: readinessTargetFault(
		platform.CodeTargetNotFound, "first cause", platform.ReadinessUnsatisfied)}
	svc, _ := newTestService(t, paths, source, func() (Recognizer, error) { return newFakeRecognizer(), nil })
	logs := captureReadinessLog(t)

	svc.CheckReadiness(nil)
	if got := strings.Count(logs.String(), "fault item=targetClient"); got != 1 {
		t.Fatalf("the first fault wrote %d lines, want 1:\n%s", got, logs)
	}

	source.err = errors.New("platform check boom")
	svc.CheckReadiness(nil)
	if got := strings.Count(logs.String(), "fault cleared item=targetClient"); got != 0 {
		t.Fatalf("a platform failure cleared the row:\n%s", logs)
	}

	source.err = nil
	svc.CheckReadiness(nil)
	if got := strings.Count(logs.String(), "fault item=targetClient"); got != 1 {
		t.Fatalf("the unchanged fault repeated after a platform failure:\n%s", logs)
	}
	if got := strings.Count(logs.String(), "fault cleared item=targetClient"); got != 0 {
		t.Fatalf("the platform recovery cleared the row:\n%s", logs)
	}

	source.err = errors.New("platform check boom")
	svc.CheckReadiness(nil)
	if got := strings.Count(logs.String(), "fault cleared item=targetClient"); got != 0 {
		t.Fatalf("a second platform failure cleared the row:\n%s", logs)
	}

	source.err = nil
	source.report = readinessTargetSatisfied()
	svc.CheckReadiness(nil)
	if got := strings.Count(logs.String(), "fault cleared item=targetClient"); got != 1 {
		t.Fatalf("the satisfied row wrote %d cleared lines, want 1:\n%s", got, logs)
	}
}

// TestServiceCheckReadinessLogsPlatformFailureOnce covers the other line the
// ten-second re-check used to repeat: a source that cannot produce a platform
// report. The same error stays quiet; a different error and the recovery are
// each written once.
func TestServiceCheckReadinessLogsPlatformFailureOnce(t *testing.T) {
	paths := newTestPaths(t)
	source := &readinessFakeSource{err: errors.New("display policy: first failure")}
	svc, _ := newTestService(t, paths, source, func() (Recognizer, error) { return newFakeRecognizer(), nil })
	logs := captureReadinessLog(t)

	svc.CheckReadiness(nil)
	svc.CheckReadiness(nil)
	if got := strings.Count(logs.String(), "platform check failed"); got != 1 {
		t.Fatalf("the unchanged platform error wrote %d lines, want 1:\n%s", got, logs)
	}

	source.err = errors.New("display policy: second failure")
	svc.CheckReadiness(nil)
	svc.CheckReadiness(nil)
	if got := strings.Count(logs.String(), "platform check failed"); got != 2 {
		t.Fatalf("the changed platform error left %d lines, want 2:\n%s", got, logs)
	}

	source.err = nil
	source.report = readinessTargetSatisfied()
	svc.CheckReadiness(nil)
	svc.CheckReadiness(nil)
	if got := strings.Count(logs.String(), "platform check recovered"); got != 1 {
		t.Fatalf("the platform recovery wrote %d lines, want 1:\n%s", got, logs)
	}
}

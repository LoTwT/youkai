package app

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/LoTwT/youkai/internal/game/zzz/collect"
	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/record"
)

// ReadinessMinFreeBytes is the free space the preparation checklist
// requires before a scan may start. It is a conservative bound for the
// evidence and checkpoint writes of a full scan; 08 §1.1 registers 1 GB
// as provisional until the W-3 end-to-end acceptance fixes it from the
// actual scan data volume.
const ReadinessMinFreeBytes int64 = 1 << 30

// ReadinessVerdict is one checklist row's conclusion (R-01). The values
// are stable identifiers, never user-facing text.
type ReadinessVerdict string

const (
	ReadinessSatisfied   = ReadinessVerdict(platform.ReadinessSatisfied)
	ReadinessUnsatisfied = ReadinessVerdict(platform.ReadinessUnsatisfied)
	ReadinessUnknown     = ReadinessVerdict(platform.ReadinessUnknown)
	// ReadinessCheckedAfterStart is the conclusion for rows that need the
	// target foreground or the session's own registration. They are never
	// probed by the read-only check (P-03, Q-B).
	ReadinessCheckedAfterStart ReadinessVerdict = "checkedAfterStart"
)

// ReadinessItem names one row of the 08 §1.1 preparation checklist. The
// names are stable identifiers shared with the interface layer.
type ReadinessItem string

const (
	// The five platform-owned rows keep the platform's identifiers as the
	// single source of truth; the rest are application-level rows.
	ReadinessTargetClient                       = ReadinessItem(platform.ReadinessTargetClient)
	ReadinessAdministrator                      = ReadinessItem(platform.ReadinessAdministrator)
	ReadinessGameResolution                     = ReadinessItem(platform.ReadinessGameResolution)
	ReadinessWindowPosition                     = ReadinessItem(platform.ReadinessWindowPosition)
	ReadinessDisplayScale                       = ReadinessItem(platform.ReadinessDisplayScale)
	ReadinessStopHotkey           ReadinessItem = "stopHotkey"
	ReadinessDiskSpace            ReadinessItem = "diskSpace"
	ReadinessRecognitionResources ReadinessItem = "recognitionResources"
	ReadinessGamePage             ReadinessItem = "gamePage"
)

// ReadinessCheck is one checklist row: its conclusion, the current value,
// the needed value, the diagnostic fault code and when it was checked.
// Current and Required are stable diagnostic strings; the interface layer
// localizes them (sizes use "x", levels use the platform labels, disk
// values are decimal byte counts).
type ReadinessCheck struct {
	Item        ReadinessItem
	Verdict     ReadinessVerdict
	Current     string
	Required    string
	FaultCode   string
	FaultParams map[string]string
	// FaultMessage is the platform's English diagnostic reason for
	// FaultCode. It is evidence for the diagnostic log only: the interface
	// localizes FaultCode/FaultParams and must not display this text. The
	// readiness path never reads another window's title, so the message
	// carries no external window title.
	FaultMessage string
	CheckedAt    time.Time
}

// ReadinessReport is one read-only preparation check. It is assembled
// without creating a session, taking the scan occupancy, registering the
// stop hotkey or dispatching input.
type ReadinessReport struct {
	CheckedAt             time.Time
	Candidates            []TargetCandidate
	AccessDeniedWindows   int
	UnreadableGameWindows int
	Checks                []ReadinessCheck
}

// ReadinessSource is the optional ScanSource capability that performs the
// platform-owned read-only checks. A source without it, or a platform
// without the capability, leaves those rows unknown.
type ReadinessSource interface {
	CheckReadiness(target *TargetCandidate) (platform.ReadinessReport, error)
}

// Readiness assembles the full checklist from a scan source and the
// application's own resource facts.
type Readiness struct {
	Paths  Paths
	Source ScanSource
	// log deduplicates the diagnostic log. NewReadiness supplies it; the
	// zero value keeps no history and therefore logs every pass.
	log *readinessLogState
}

// NewReadiness returns a checker whose diagnostic log writes a fault when it
// appears or changes and writes its clearing when it disappears. Reusing the
// returned value keeps that history, which is what a repeatedly checked page
// needs; the check interval belongs to the caller.
func NewReadiness(paths Paths, source ScanSource) Readiness {
	return Readiness{Paths: paths, Source: source, log: &readinessLogState{}}
}

// Check runs one read-only check. Every row carries its own check time so
// the interface can apply its focus/10-second freshness rule; the service
// itself never decides when to re-check.
func (r Readiness) Check(target *TargetCandidate) ReadinessReport {
	at := nowUTC()
	report := ReadinessReport{CheckedAt: at, Checks: defaultReadinessChecks(at)}

	var platformErr error
	if source, ok := r.Source.(ReadinessSource); ok {
		platformReport, err := source.CheckReadiness(target)
		if err != nil {
			platformErr = err
		} else {
			mergePlatformReadiness(&report, platformReport, at)
		}
	}

	report.set(readinessDiskSpace(r.Paths.Data, at))
	report.set(readinessRecognitionResources(r.Paths.Resources, at))

	logState := r.log
	if logState == nil {
		logState = &readinessLogState{}
	}
	logState.observe(report, platformErr)
	return report
}

// CheckReadiness implements the service's read-only preparation check
// (R-01). It never starts a session or takes the scan occupancy.
func (s *Service) CheckReadiness(target *TargetCandidate) ReadinessReport {
	return Readiness{Paths: s.cfg.Paths, Source: s.cfg.Source, log: &s.readinessLog}.Check(target)
}

// readinessLogState remembers the failure each checklist row reported last,
// so the ten-second re-check writes a line only when a failure appears,
// changes or clears instead of once per pass.
type readinessLogState struct {
	mu sync.Mutex
	// faults is keyed by row. The verdict, code and redacted message identify
	// the failure. current is deliberately excluded: the disk's free-byte
	// count changes on every pass and would otherwise write a line each time.
	faults      map[ReadinessItem]readinessFault
	platformErr string
}

type readinessFault struct {
	verdict ReadinessVerdict
	code    string
	message string
}

// readinessFaultOf reports whether the row is a failure worth logging and
// the value the log compares. An unsatisfied row without a fault code (disk
// space, recognition resources) still blocks start, so it is a failure too;
// its measured current value stays out of the comparison.
func readinessFaultOf(check ReadinessCheck) (readinessFault, bool) {
	if check.FaultCode == "" && check.Verdict != ReadinessUnsatisfied {
		return readinessFault{}, false
	}
	return readinessFault{
		verdict: check.Verdict,
		code:    check.FaultCode,
		message: redactText(check.FaultMessage),
	}, true
}

// observe compares this pass with the previous one. A platform failure is
// itself a state: it is logged once per distinct error and its recovery is
// logged once. While the platform report is unavailable, row failures are
// left untouched so a failed platform check does not fabricate recoveries.
func (s *readinessLogState) observe(report ReadinessReport, platformErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if platformErr != nil {
		message := redactText(platformErr.Error())
		if message != s.platformErr {
			s.platformErr = message
			log.Printf("youkai: readiness: platform check failed: %s", message)
		}
		return
	}
	if s.platformErr != "" {
		s.platformErr = ""
		log.Printf("youkai: readiness: platform check recovered")
	}

	current := make(map[ReadinessItem]readinessFault, len(report.Checks))
	seen := make(map[ReadinessItem]bool, len(report.Checks))
	for _, check := range report.Checks {
		seen[check.Item] = true
		fault, failed := readinessFaultOf(check)
		if !failed {
			continue
		}
		current[check.Item] = fault
		previous, existed := s.faults[check.Item]
		if existed && previous == fault {
			continue
		}
		log.Printf("youkai: readiness: fault item=%s verdict=%s code=%s current=%q required=%q message=%q",
			check.Item, check.Verdict, check.FaultCode, check.Current, check.Required, fault.message)
	}
	for item, previous := range s.faults {
		if seen[item] {
			continue
		}
		log.Printf("youkai: readiness: fault cleared item=%s verdict=unknown code=%s", item, previous.code)
	}
	for _, check := range report.Checks {
		previous, existed := s.faults[check.Item]
		if !existed {
			continue
		}
		if _, stillFaulted := current[check.Item]; stillFaulted {
			continue
		}
		log.Printf("youkai: readiness: fault cleared item=%s verdict=%s code=%s",
			check.Item, check.Verdict, previous.code)
	}
	s.faults = current
}

// CheckReadiness implements ReadinessSource for the game source. The
// installed Windows source discovers candidates; a source configured with
// a fixed target uses the platform's fixed-target selection. Ports without
// the capability (macOS) return an empty report and leave the rows unknown.
func (g *GameScanSource) CheckReadiness(target *TargetCandidate) (platform.ReadinessReport, error) {
	if g.Port == nil {
		return platform.ReadinessReport{}, fmt.Errorf("app: readiness: the game source has no platform port")
	}
	checker, ok := g.Port.(platform.ReadinessChecker)
	if !ok {
		return platform.ReadinessReport{}, nil
	}
	req := platform.ReadinessRequest{Target: g.Target}
	if g.Client == record.ClientWindowsLocal {
		policy, err := collect.WindowsLocalDisplayPolicy()
		if err != nil {
			return platform.ReadinessReport{}, fmt.Errorf("app: readiness: display policy: %w", err)
		}
		req.Display = &policy
	}
	if target != nil && g.RequiresTargetConfirmation() {
		// Only the discovery source has a user-confirmed candidate; a
		// fixed-target source keeps its configured identity (NewScanForTarget
		// does the same).
		req.Target.HandleHint = target.Handle
		req.Target.PIDHint = target.PID
		req.Target.ProcessStartedAtHint = target.ProcessStartedAt
	}
	return checker.CheckReadiness(req)
}

func defaultReadinessChecks(at time.Time) []ReadinessCheck {
	items := []ReadinessItem{
		ReadinessTargetClient,
		ReadinessAdministrator,
		ReadinessGameResolution,
		ReadinessWindowPosition,
		ReadinessDisplayScale,
		ReadinessStopHotkey,
		ReadinessDiskSpace,
		ReadinessRecognitionResources,
		ReadinessGamePage,
	}
	checks := make([]ReadinessCheck, 0, len(items))
	for _, item := range items {
		verdict := ReadinessUnknown
		if item == ReadinessStopHotkey || item == ReadinessGamePage {
			// Q-B: the hotkey is only registered by Open and released by
			// Close; the platform deliberately offers no side-effect-free
			// registrability probe. Occlusion, exclusive fullscreen and the
			// game page need the target foreground. Both stay unprobed.
			verdict = ReadinessCheckedAfterStart
		}
		checks = append(checks, ReadinessCheck{Item: item, Verdict: verdict, CheckedAt: at})
	}
	return checks
}

func (r *ReadinessReport) set(check ReadinessCheck) {
	for i := range r.Checks {
		if r.Checks[i].Item == check.Item {
			r.Checks[i] = check
			return
		}
	}
	r.Checks = append(r.Checks, check)
}

func mergePlatformReadiness(report *ReadinessReport, source platform.ReadinessReport, at time.Time) {
	report.Candidates = source.Candidates
	report.AccessDeniedWindows = source.AccessDeniedWindows
	report.UnreadableGameWindows = source.UnreadableGameWindows
	items := map[platform.ReadinessItem]ReadinessItem{
		platform.ReadinessTargetClient:   ReadinessTargetClient,
		platform.ReadinessAdministrator:  ReadinessAdministrator,
		platform.ReadinessGameResolution: ReadinessGameResolution,
		platform.ReadinessWindowPosition: ReadinessWindowPosition,
		platform.ReadinessDisplayScale:   ReadinessDisplayScale,
	}
	for _, check := range source.Checks {
		item, ok := items[check.Item]
		if !ok {
			continue
		}
		verdict := ReadinessUnknown
		switch check.Verdict {
		case platform.ReadinessSatisfied:
			verdict = ReadinessSatisfied
		case platform.ReadinessUnsatisfied:
			verdict = ReadinessUnsatisfied
		}
		out := ReadinessCheck{
			Item:      item,
			Verdict:   verdict,
			Current:   check.Current,
			Required:  check.Required,
			CheckedAt: check.At,
		}
		if out.CheckedAt.IsZero() {
			out.CheckedAt = at
		}
		if check.Fault != nil {
			out.FaultCode = check.Fault.Code
			out.FaultParams = copyReadinessParams(check.Fault.Params)
			out.FaultMessage = check.Fault.Message
		}
		report.set(out)
	}
}

func copyReadinessParams(params map[string]string) map[string]string {
	if len(params) == 0 {
		return nil
	}
	out := make(map[string]string, len(params))
	for k, v := range params {
		out[k] = v
	}
	return out
}

// readinessDiskSpace judges the data volume's free space. The disk query
// itself is platform-specific (diskspace_windows.go / diskspace_unix.go);
// an unreadable volume is unknown, never "0 bytes free".
func readinessDiskSpace(dataRoot string, at time.Time) ReadinessCheck {
	free, _, ok := diskSpace(dataRoot)
	return readinessDiskSpaceVerdict(free, ok, at)
}

// readinessDiskSpaceVerdict is the disk row's judgment over the facts the
// platform volume query returned; tests drive it without a real volume.
func readinessDiskSpaceVerdict(free int64, known bool, at time.Time) ReadinessCheck {
	check := ReadinessCheck{Item: ReadinessDiskSpace, CheckedAt: at,
		Required: strconv.FormatInt(ReadinessMinFreeBytes, 10)}
	if !known {
		check.Verdict = ReadinessUnknown
		return check
	}
	check.Current = strconv.FormatInt(free, 10)
	if free >= ReadinessMinFreeBytes {
		check.Verdict = ReadinessSatisfied
	} else {
		check.Verdict = ReadinessUnsatisfied
	}
	return check
}

// readinessRecognitionResources checks that the shipped OCR runtime library
// and model files are present and non-empty. It deliberately never loads
// them: a missing or damaged artifact is F-05's checklist row, and loading
// the model belongs to the first accepted start, not to a page that may be
// visible for hours.
func readinessRecognitionResources(resourcesRoot string, at time.Time) ReadinessCheck {
	check := ReadinessCheck{Item: ReadinessRecognitionResources, CheckedAt: at,
		Required: "files=runtime,model"}
	res, err := OCRResources(resourcesRoot)
	if err != nil {
		check.Verdict = ReadinessUnknown
		check.Current = "unresolved"
		return check
	}
	missing := ""
	for _, artifact := range []struct {
		name string
		path string
	}{
		{"runtime", res.LibraryPath},
		{"model", res.ModelPath},
	} {
		info, err := os.Stat(artifact.path)
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			if missing != "" {
				missing += ","
			}
			missing += artifact.name
		}
	}
	if missing != "" {
		check.Verdict = ReadinessUnsatisfied
		check.Current = "missing=" + missing
		return check
	}
	check.Verdict = ReadinessSatisfied
	check.Current = "present"
	return check
}

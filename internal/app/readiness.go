package app

import (
	"fmt"
	"log"
	"os"
	"strconv"
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
	CheckedAt   time.Time
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
}

// Check runs one read-only check. Every row carries its own check time so
// the interface can apply its focus/10-second freshness rule; the service
// itself never decides when to re-check.
func (r Readiness) Check(target *TargetCandidate) ReadinessReport {
	at := nowUTC()
	report := ReadinessReport{CheckedAt: at, Checks: defaultReadinessChecks(at)}

	if source, ok := r.Source.(ReadinessSource); ok {
		platformReport, err := source.CheckReadiness(target)
		if err != nil {
			log.Printf("youkai: readiness: platform check failed: %v", err)
		} else {
			mergePlatformReadiness(&report, platformReport, at)
		}
	}

	report.set(readinessDiskSpace(r.Paths.Data, at))
	report.set(readinessRecognitionResources(r.Paths.Resources, at))
	return report
}

// CheckReadiness implements the service's read-only preparation check
// (R-01). It never starts a session or takes the scan occupancy.
func (s *Service) CheckReadiness(target *TargetCandidate) ReadinessReport {
	return Readiness{Paths: s.cfg.Paths, Source: s.cfg.Source}.Check(target)
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

package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/LoTwT/youkai/internal/platform/windows"
)

// ToolName identifies this probe in every evidence document.
const ToolName = "wininput"

// Evidence option and identity facts.

// OptionsFacts is the invocation recorded with the run.
type OptionsFacts struct {
	Target           string `json:"target"`
	Executable       string `json:"executable,omitempty"`
	NotepadTitleHint string `json:"notepadTitleHint,omitempty"`
	Slot             string `json:"slot,omitempty"`
	DryRun           bool   `json:"dryRun"`
	CountdownSeconds int    `json:"countdownSeconds"`
	OutDirectory     string `json:"outDirectory"`
}

// IntegrityFacts is one process's TokenIntegrityLevel, read with query-only
// access. A failure is recorded, never worked around by elevation.
type IntegrityFacts struct {
	Level string `json:"level"`
	RID   uint32 `json:"rid"`
	Error string `json:"error,omitempty"`
}

// ProcessFacts describes the probe process: its DPI awareness and integrity.
type ProcessFacts struct {
	PID               uint32         `json:"pid"`
	Executable        string         `json:"executable,omitempty"`
	DPIAwareness      string         `json:"dpiAwareness"`
	DPIAwarenessValue int            `json:"dpiAwarenessValue"`
	DPIAwarenessError string         `json:"dpiAwarenessError,omitempty"`
	Integrity         IntegrityFacts `json:"integrity"`
}

// TargetFacts is the locked target at preflight time.
type TargetFacts struct {
	Kind             string         `json:"kind"`
	HWND             uint64         `json:"hwnd"`
	HWNDHex          string         `json:"hwndHex"`
	PID              uint32         `json:"pid"`
	Executable       string         `json:"executable"`
	ProcessStartedAt time.Time      `json:"processStartedAt"`
	ClassName        string         `json:"className"`
	Title            string         `json:"title"`
	Visible          bool           `json:"visible"`
	Minimized        bool           `json:"minimized"`
	Cloaked          bool           `json:"cloaked"`
	Foreground       bool           `json:"foreground"`
	CloakedError     string         `json:"cloakedError,omitempty"`
	Integrity        IntegrityFacts `json:"integrity"`
}

// WindowCandidateFacts is one candidate window shown when selection fails.
type WindowCandidateFacts struct {
	HWND       uint64 `json:"hwnd"`
	HWNDHex    string `json:"hwndHex"`
	PID        uint32 `json:"pid"`
	Executable string `json:"executable"`
	ClassName  string `json:"className"`
	Title      string `json:"title"`
	Visible    bool   `json:"visible"`
	Minimized  bool   `json:"minimized"`
	Cloaked    bool   `json:"cloaked"`
}

// GeometryFacts is the measured client geometry and its DPI evidence.
type GeometryFacts struct {
	Version               uint64            `json:"version"`
	Client                windows.PixelRect `json:"client"`
	Monitor               windows.PixelRect `json:"monitor"`
	WindowDPI             uint32            `json:"windowDPI"`
	MonitorDPI            uint32            `json:"monitorDPI"`
	SelfDPIAwareness      string            `json:"selfDpiAwareness"`
	SelfDPIAwarenessValue int               `json:"selfDpiAwarenessValue"`
	SelfDPIAwarenessError string            `json:"selfDpiAwarenessError,omitempty"`
	DPIVerified           bool              `json:"dpiVerified"`
	DPIDetail             string            `json:"dpiDetail,omitempty"`
}

// Preflight check statuses.
const (
	CheckPass   = "pass"
	CheckRefuse = "refuse"
	CheckError  = "error"
	CheckNotRun = "notRun"
)

// CheckFacts is one pre-click check.
type CheckFacts struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// PreflightFacts is the ordered set of probe-side checks performed before a
// click. A refusal is a deliberate no-click verdict; an error means the check
// could not be completed.
type PreflightFacts struct {
	Checks  []CheckFacts `json:"checks"`
	Passed  bool         `json:"passed"`
	Refused bool         `json:"refused"`
	Errored bool         `json:"errored"`
}

func (p *PreflightFacts) add(name, status, detail string) {
	p.Checks = append(p.Checks, CheckFacts{Name: name, Status: status, Detail: detail})
}

// Pass records a satisfied check.
func (p *PreflightFacts) Pass(name, detail string) { p.add(name, CheckPass, detail) }

// Refuse records a premise that is not satisfied; the click must not be sent.
func (p *PreflightFacts) Refuse(name, detail string) { p.add(name, CheckRefuse, detail) }

// Error records a check that could not be completed.
func (p *PreflightFacts) Error(name, detail string) { p.add(name, CheckError, detail) }

// NotRun records a check that was skipped because an earlier check failed.
func (p *PreflightFacts) NotRun(name, detail string) { p.add(name, CheckNotRun, detail) }

// Finalize computes the aggregate verdicts.
func (p *PreflightFacts) Finalize() {
	p.Passed = len(p.Checks) > 0
	p.Refused = false
	p.Errored = false
	for _, check := range p.Checks {
		switch check.Status {
		case CheckRefuse:
			p.Refused = true
		case CheckError:
			p.Errored = true
		}
		if check.Status != CheckPass {
			p.Passed = false
		}
	}
}

// FirstFailure returns a compact description of the governing failure.
func (p *PreflightFacts) FirstFailure() string {
	for _, status := range []string{CheckError, CheckRefuse, CheckNotRun} {
		for _, check := range p.Checks {
			if check.Status == status {
				if check.Detail == "" {
					return check.Name
				}
				return check.Name + ": " + check.Detail
			}
		}
	}
	return ""
}

// PrecheckFacts is the platform InputPrecheck verdict. It is evaluated inside
// the actor's serial section for a real click and directly for a dry run.
type PrecheckFacts struct {
	Evaluated bool      `json:"evaluated"`
	Passed    bool      `json:"passed"`
	Code      string    `json:"code,omitempty"`
	Detail    string    `json:"detail,omitempty"`
	At        time.Time `json:"at,omitempty"`
}

// HitTestFacts is the WindowFromPoint evidence at the click point.
type HitTestFacts struct {
	Point        windows.PixelPoint `json:"point"`
	Window       uint64             `json:"window"`
	WindowHex    string             `json:"windowHex"`
	WindowClass  string             `json:"windowClass,omitempty"`
	Root         uint64             `json:"root"`
	RootHex      string             `json:"rootHex"`
	RootIsTarget bool               `json:"rootIsTarget"`
	Error        string             `json:"error,omitempty"`
}

// EditInsetsFacts records the Notepad edit-area approximation.
type EditInsetsFacts struct {
	Horizontal int `json:"horizontal"`
	Top        int `json:"top"`
	Bottom     int `json:"bottom"`
}

// CoordinatesFacts is the complete coordinate derivation: reference canvas,
// tier, client-relative point, screen point, and evidence regions in both
// client-relative and screen terms.
type CoordinatesFacts struct {
	ReferenceCanvasWidth  int                 `json:"referenceCanvasWidth"`
	ReferenceCanvasHeight int                 `json:"referenceCanvasHeight"`
	ReferencePoint        *windows.PixelPoint `json:"referencePoint,omitempty"`
	ClientPoint           windows.PixelPoint  `json:"clientPoint"`
	ScreenPoint           windows.PixelPoint  `json:"screenPoint"`
	ClientRect            windows.PixelRect   `json:"clientRect"`
	Tier                  *Tier               `json:"tier,omitempty"`
	BorderLeft            *windows.PixelRect  `json:"borderLeft,omitempty"`
	BorderBottom          *windows.PixelRect  `json:"borderBottom,omitempty"`
	NameRow               *windows.PixelRect  `json:"nameRow,omitempty"`
	ScreenBorderLeft      *windows.PixelRect  `json:"screenBorderLeft,omitempty"`
	ScreenBorderBottom    *windows.PixelRect  `json:"screenBorderBottom,omitempty"`
	ScreenNameRow         *windows.PixelRect  `json:"screenNameRow,omitempty"`
	EditRegion            *windows.PixelRect  `json:"editRegion,omitempty"`
	ScreenEditRegion      *windows.PixelRect  `json:"screenEditRegion,omitempty"`
	EditInsets            *EditInsetsFacts    `json:"editInsets,omitempty"`
}

// SendInputCallFacts is one call into the platform Sender. A click is two
// batches: [move, down] and [up].
type SendInputCallFacts struct {
	Index            int       `json:"index"`
	At               time.Time `json:"at"`
	Phase            string    `json:"phase"`
	Events           []string  `json:"events"`
	Accepted         int       `json:"accepted"`
	Blocked          bool      `json:"blocked"`
	Error            string    `json:"error,omitempty"`
	GetLastError     uint32    `json:"getLastError"`
	GetLastErrorText string    `json:"getLastErrorText,omitempty"`
}

// SendInputFacts is the dispatch accounting.
type SendInputFacts struct {
	// State is the platform InputState for a real attempt, "dryRun" when the
	// click was deliberately suppressed, or "notAttempted" before dispatch.
	State      string               `json:"state"`
	Planned    int                  `json:"planned"`
	Inserted   int                  `json:"inserted"`
	Aborted    bool                 `json:"aborted"`
	Error      string               `json:"error,omitempty"`
	HeldAfter  []string             `json:"heldAfter"`
	LastPostAt *time.Time           `json:"lastPostAt,omitempty"`
	Calls      []SendInputCallFacts `json:"calls"`
}

// ReleaseFacts is one stop-time release attempt for a button this probe
// pressed. The actor never releases user keys.
type ReleaseFacts struct {
	Phase    string    `json:"phase"`
	At       time.Time `json:"at"`
	Key      string    `json:"key"`
	Released bool      `json:"released"`
	Error    string    `json:"error,omitempty"`
}

// RegionDiffFacts is the JSON form of one region comparison.
type RegionDiffFacts struct {
	Region          windows.PixelRect `json:"region"`
	Pixels          int               `json:"pixels"`
	MeanAbsDiff     float64           `json:"meanAbsDiff"`
	ChangedPixels   int               `json:"changedPixels"`
	ChangedFraction float64           `json:"changedFraction"`
	MaxDelta        int               `json:"maxDelta"`
	LumaBefore      float64           `json:"lumaBefore"`
	LumaAfter       float64           `json:"lumaAfter"`
	LumaDelta       float64           `json:"lumaDelta"`
}

// FrameMetricsFacts is the per-frame effect evidence. Exactly one of Edit or
// the game regions is set, depending on the target.
type FrameMetricsFacts struct {
	Edit         *RegionDiffFacts `json:"edit,omitempty"`
	BorderLeft   *RegionDiffFacts `json:"borderLeft,omitempty"`
	BorderBottom *RegionDiffFacts `json:"borderBottom,omitempty"`
	Detail       *RegionDiffFacts `json:"detail,omitempty"`
}

// FrameFacts is one before/after capture with hashes and effect verdict.
type FrameFacts struct {
	Phase             string             `json:"phase"`
	Index             int                `json:"index"`
	Timestamp         time.Time          `json:"timestamp"`
	OffsetMS          float64            `json:"offsetMs"`
	File              string             `json:"file"`
	Width             int                `json:"width"`
	Height            int                `json:"height"`
	ClientWidth       int                `json:"clientWidth"`
	ClientHeight      int                `json:"clientHeight"`
	SizeMatchesClient bool               `json:"sizeMatchesClient"`
	GeometryVersion   uint64             `json:"geometryVersion"`
	Mean              float64            `json:"mean"`
	Variance          float64            `json:"variance"`
	Black             bool               `json:"black"`
	PNGSHA256         string             `json:"pngSha256"`
	PixelSHA256       string             `json:"pixelSha256"`
	Error             string             `json:"error,omitempty"`
	Metrics           *FrameMetricsFacts `json:"metrics,omitempty"`
	Effective         bool               `json:"effective"`
	EffectReason      string             `json:"effectReason,omitempty"`
}

// RunReport is run.json: the complete evidence of one probe run.
type RunReport struct {
	Tool             string                 `json:"tool"`
	Mode             string                 `json:"mode"`
	TargetKind       string                 `json:"targetKind"`
	StartedAt        time.Time              `json:"startedAt"`
	GeneratedAt      time.Time              `json:"generatedAt"`
	OutDirectory     string                 `json:"outDirectory"`
	Options          OptionsFacts           `json:"options"`
	Probe            ProcessFacts           `json:"probe"`
	Target           *TargetFacts           `json:"target,omitempty"`
	Candidates       []WindowCandidateFacts `json:"candidates"`
	Geometry         *GeometryFacts         `json:"geometry,omitempty"`
	Coordinates      *CoordinatesFacts      `json:"coordinates,omitempty"`
	Preflight        PreflightFacts         `json:"preflight"`
	Precheck         PrecheckFacts          `json:"precheck"`
	HitTest          *HitTestFacts          `json:"hitTest,omitempty"`
	SendInput        SendInputFacts         `json:"sendInput"`
	Release          []ReleaseFacts         `json:"release"`
	Frames           []FrameFacts           `json:"frames"`
	ActionAt         *time.Time             `json:"actionAt,omitempty"`
	Abort            string                 `json:"abort,omitempty"`
	Conclusion       string                 `json:"conclusion"`
	ConclusionDetail string                 `json:"conclusionDetail"`
	Errors           []string               `json:"errors"`
	Notes            []string               `json:"notes"`
}

func newRunReport(opts options) *RunReport {
	return &RunReport{
		Tool:         ToolName,
		Mode:         opts.mode(),
		TargetKind:   opts.target,
		StartedAt:    time.Now().UTC(),
		OutDirectory: opts.out,
		Options:      optionsFacts(opts),
		Candidates:   []WindowCandidateFacts{},
		Preflight:    PreflightFacts{Checks: []CheckFacts{}},
		Precheck:     PrecheckFacts{Detail: "not reached"},
		SendInput:    SendInputFacts{State: "notAttempted", HeldAfter: []string{}, Calls: []SendInputCallFacts{}},
		Release:      []ReleaseFacts{},
		Frames:       []FrameFacts{},
		Errors:       []string{},
		Notes:        defaultNotes(),
	}
}

func optionsFacts(opts options) OptionsFacts {
	return OptionsFacts{
		Target:           opts.target,
		Executable:       opts.exe,
		NotepadTitleHint: opts.notepadTitle,
		Slot:             opts.slot,
		DryRun:           opts.dryRun,
		CountdownSeconds: opts.countdown,
		OutDirectory:     opts.out,
	}
}

func defaultNotes() []string {
	return []string{
		"wininput sends at most one left-button click. It never drags, sends wheel or keyboard events, activates, moves, resizes or focuses a window, or registers a hotkey.",
		"Input goes through internal/platform/windows InputActor + RealSender + InputPrecheck; this tool adds no input path of its own.",
		"A UIPI privilege mismatch is not bypassed: SendInput's zero-delivery/access-denied shape is recorded as a dispatch failure, and only a new run with matching privileges can differ.",
		"Integrity levels are read with PROCESS_QUERY_LIMITED_INFORMATION and TOKEN_QUERY only; a refusal is recorded, never escalated.",
		"All rectangles are screen physical pixels under per-monitor-v2 awareness unless marked client-relative; right and bottom are exclusive.",
		"Reference cell centers are the approximate 1920x1080 field values: first (139,185), column pitch 130, row pitch 170; -slot is 1-based within 5 rows and 4 columns.",
		"Only exact 16:9 client sizes in the accepted tier list are scaled; any other client size is refused instead of approximated.",
		"Game effect requires both the target cell's border band to change and the detail name-row region's MAD to change in the same post-click frame.",
		"Notepad effect requires changed pixels in the edit-area inset; a blinking caret can make one frame miss, so any post-click frame counts.",
		"GetLastError is read immediately after each SendInput call on the same locked OS thread. A successful call may leave a stale thread last-error value, so the accepted count and the sender's classification are the primary facts.",
		"Page sanity is only 'not black and client-sized'; a different non-black page is not detected by this probe.",
		"Candidates are top-level windows whose process identity can be queried; a running game missing from the candidate list is evidence of that refusal, not proof that it is not running.",
		"The reference name-row band and cell border bands are probe approximations, not a calibrated production layout.",
		"Only buttons recorded in the actor ledger as pressed by this probe are released at exit; user keys are never touched.",
		"GDI capture is a desktop-region crop; the target must be visible and unobstructed, and any foreground, geometry or page-sanity change aborts the observation.",
	}
}

func candidateFacts(probes []windows.WindowProbe) []WindowCandidateFacts {
	out := make([]WindowCandidateFacts, 0, len(probes))
	for _, probe := range probes {
		out = append(out, WindowCandidateFacts{
			HWND:       uint64(probe.HWND),
			HWNDHex:    fmt.Sprintf("0x%X", uint64(probe.HWND)),
			PID:        uint32(probe.PID),
			Executable: probe.ImagePath,
			ClassName:  probe.ClassName,
			Title:      probe.Title,
			Visible:    probe.Visible,
			Minimized:  probe.Minimized,
			Cloaked:    probe.Cloaked,
		})
	}
	return out
}

func targetFacts(kind string, probe windows.WindowProbe) *TargetFacts {
	return &TargetFacts{
		Kind:             kind,
		HWND:             uint64(probe.HWND),
		HWNDHex:          fmt.Sprintf("0x%X", uint64(probe.HWND)),
		PID:              uint32(probe.PID),
		Executable:       probe.ImagePath,
		ProcessStartedAt: probe.ProcessStartedAt.UTC(),
		ClassName:        probe.ClassName,
		Title:            probe.Title,
		Visible:          probe.Visible,
		Minimized:        probe.Minimized,
		Cloaked:          probe.Cloaked,
	}
}

func regionDiffFacts(diff RegionDiff) *RegionDiffFacts {
	return &RegionDiffFacts{
		Region:          diff.Region,
		Pixels:          diff.Pixels,
		MeanAbsDiff:     diff.MeanAbsDiff,
		ChangedPixels:   diff.ChangedPixels,
		ChangedFraction: diff.ChangedFraction,
		MaxDelta:        diff.MaxDelta,
		LumaBefore:      diff.LumaBefore,
		LumaAfter:       diff.LumaAfter,
		LumaDelta:       diff.LumaDelta,
	}
}

func frameEffectFacts(effect FrameEffect) *FrameMetricsFacts {
	metrics := &FrameMetricsFacts{}
	if effect.Edit != nil {
		metrics.Edit = regionDiffFacts(*effect.Edit)
	}
	if effect.BorderLeft != nil {
		metrics.BorderLeft = regionDiffFacts(*effect.BorderLeft)
	}
	if effect.BorderBottom != nil {
		metrics.BorderBottom = regionDiffFacts(*effect.BorderBottom)
	}
	if effect.Detail != nil {
		metrics.Detail = regionDiffFacts(*effect.Detail)
	}
	return metrics
}

// RenderSummary builds summary.txt. It is pure so the evidence can be read
// without opening the JSON.
func RenderSummary(rep *RunReport) string {
	var b strings.Builder
	b.WriteString("wininput summary\n")
	if rep == nil {
		b.WriteString("result: error\n")
		b.WriteString("detail: no report was produced\n")
		return b.String()
	}
	fmt.Fprintf(&b, "result: %s\n", rep.Conclusion)
	fmt.Fprintf(&b, "detail: %s\n", rep.ConclusionDetail)
	fmt.Fprintf(&b, "mode: %s\n", rep.Mode)
	fmt.Fprintf(&b, "target: %s\n", rep.TargetKind)
	fmt.Fprintf(&b, "out: %s\n", rep.OutDirectory)
	fmt.Fprintf(&b, "started: %s\n", formatTime(rep.StartedAt))
	fmt.Fprintf(&b, "generated: %s\n", formatTime(rep.GeneratedAt))

	b.WriteString("\nprobe\n")
	fmt.Fprintf(&b, "  pid=%d exe=%s\n", rep.Probe.PID, rep.Probe.Executable)
	fmt.Fprintf(&b, "  dpiAwareness=%s value=%d\n", rep.Probe.DPIAwareness, rep.Probe.DPIAwarenessValue)
	if rep.Probe.DPIAwarenessError != "" {
		fmt.Fprintf(&b, "  dpiAwarenessError=%s\n", rep.Probe.DPIAwarenessError)
	}
	fmt.Fprintf(&b, "  integrity=%s\n", formatIntegrity(rep.Probe.Integrity))

	b.WriteString("\ntarget\n")
	if rep.Target == nil {
		b.WriteString("  not locked\n")
	} else {
		target := rep.Target
		fmt.Fprintf(&b, "  kind=%s hwnd=%s pid=%d\n", target.Kind, target.HWNDHex, target.PID)
		fmt.Fprintf(&b, "  exe=%s\n", target.Executable)
		fmt.Fprintf(&b, "  class=%q title=%q\n", target.ClassName, target.Title)
		fmt.Fprintf(&b, "  state: visible=%t minimized=%t cloaked=%t foreground=%t\n",
			target.Visible, target.Minimized, target.Cloaked, target.Foreground)
		if target.CloakedError != "" {
			fmt.Fprintf(&b, "  cloaked query error: %s\n", target.CloakedError)
		}
		fmt.Fprintf(&b, "  integrity=%s\n", formatIntegrity(target.Integrity))
	}
	if len(rep.Candidates) > 0 {
		fmt.Fprintf(&b, "  candidates: %d\n", len(rep.Candidates))
		for i, candidate := range rep.Candidates {
			fmt.Fprintf(&b, "  [%d] hwnd=%s pid=%d visible=%t minimized=%t cloaked=%t class=%q title=%q\n",
				i, candidate.HWNDHex, candidate.PID, candidate.Visible, candidate.Minimized, candidate.Cloaked,
				candidate.ClassName, candidate.Title)
		}
	}

	if rep.Geometry != nil {
		g := rep.Geometry
		b.WriteString("\ngeometry\n")
		fmt.Fprintf(&b, "  version=%d client=%s monitor=%s\n", g.Version, formatRect(g.Client), formatRect(g.Monitor))
		fmt.Fprintf(&b, "  windowDPI=%d monitorDPI=%d selfDPI=%s value=%d verified=%t\n",
			g.WindowDPI, g.MonitorDPI, g.SelfDPIAwareness, g.SelfDPIAwarenessValue, g.DPIVerified)
		if g.SelfDPIAwarenessError != "" {
			fmt.Fprintf(&b, "  self DPI error: %s\n", g.SelfDPIAwarenessError)
		}
		if g.DPIDetail != "" {
			fmt.Fprintf(&b, "  dpi detail: %s\n", g.DPIDetail)
		}
	}

	if rep.Coordinates != nil {
		c := rep.Coordinates
		b.WriteString("\ncoordinates\n")
		if c.Tier != nil {
			fmt.Fprintf(&b, "  tier=%s scale=%.6f reference=%s\n", c.Tier.Name(), c.Tier.Scale, formatPointPtr(c.ReferencePoint))
		}
		fmt.Fprintf(&b, "  client=%s screen=%s clientRect=%s\n",
			formatPoint(c.ClientPoint), formatPoint(c.ScreenPoint), formatRect(c.ClientRect))
		if c.BorderLeft != nil {
			fmt.Fprintf(&b, "  border left (client)=%s screen=%s\n", formatRect(*c.BorderLeft), formatRectPtr(c.ScreenBorderLeft))
			fmt.Fprintf(&b, "  border bottom (client)=%s screen=%s\n", formatRect(*c.BorderBottom), formatRectPtr(c.ScreenBorderBottom))
			fmt.Fprintf(&b, "  name row (client)=%s screen=%s\n", formatRect(*c.NameRow), formatRectPtr(c.ScreenNameRow))
		}
		if c.EditRegion != nil {
			fmt.Fprintf(&b, "  edit area (client)=%s screen=%s\n", formatRect(*c.EditRegion), formatRectPtr(c.ScreenEditRegion))
			if c.EditInsets != nil {
				fmt.Fprintf(&b, "  edit insets: horizontal=%d top=%d bottom=%d\n",
					c.EditInsets.Horizontal, c.EditInsets.Top, c.EditInsets.Bottom)
			}
		}
	}

	b.WriteString("\npreflight\n")
	if rep.Preflight.Passed {
		b.WriteString("  passed\n")
	} else {
		fmt.Fprintf(&b, "  passed=false refused=%t errored=%t\n", rep.Preflight.Refused, rep.Preflight.Errored)
	}
	for _, check := range rep.Preflight.Checks {
		fmt.Fprintf(&b, "  - %s: %s", check.Name, check.Status)
		if check.Detail != "" {
			fmt.Fprintf(&b, " (%s)", check.Detail)
		}
		b.WriteString("\n")
	}

	b.WriteString("\nplatform precheck\n")
	if !rep.Precheck.Evaluated {
		fmt.Fprintf(&b, "  not evaluated (%s)\n", rep.Precheck.Detail)
	} else if rep.Precheck.Passed {
		fmt.Fprintf(&b, "  passed at %s\n", formatTime(rep.Precheck.At))
	} else {
		fmt.Fprintf(&b, "  refused at %s code=%s detail=%s\n",
			formatTime(rep.Precheck.At), rep.Precheck.Code, rep.Precheck.Detail)
	}

	if rep.HitTest != nil {
		h := rep.HitTest
		fmt.Fprintf(&b, "\nhit test\n  point=%s window=%s class=%q root=%s rootIsTarget=%t\n",
			formatPoint(h.Point), h.WindowHex, h.WindowClass, h.RootHex, h.RootIsTarget)
		if h.Error != "" {
			fmt.Fprintf(&b, "  error: %s\n", h.Error)
		}
	}

	b.WriteString("\ndispatch\n")
	fmt.Fprintf(&b, "  state=%s planned=%d inserted=%d\n", rep.SendInput.State, rep.SendInput.Planned, rep.SendInput.Inserted)
	if rep.SendInput.Error != "" {
		fmt.Fprintf(&b, "  error: %s\n", rep.SendInput.Error)
	}
	if len(rep.SendInput.HeldAfter) > 0 {
		fmt.Fprintf(&b, "  buttons still held after dispatch: %s\n", strings.Join(rep.SendInput.HeldAfter, ", "))
	}
	for _, call := range rep.SendInput.Calls {
		lastErrorText := call.GetLastErrorText
		if lastErrorText == "" {
			lastErrorText = "none"
		}
		fmt.Fprintf(&b, "  call %d [%s]: events=[%s] accepted=%d blocked=%t getLastError=%d (%s)\n",
			call.Index, call.Phase, strings.Join(call.Events, " "), call.Accepted, call.Blocked,
			call.GetLastError, lastErrorText)
		if call.Error != "" {
			fmt.Fprintf(&b, "          error: %s\n", call.Error)
		}
	}
	if rep.ActionAt != nil {
		fmt.Fprintf(&b, "  actionAt=%s\n", formatTime(*rep.ActionAt))
	}

	b.WriteString("\nframes\n")
	effective := 0
	for _, frame := range rep.Frames {
		if frame.Effective {
			effective++
		}
	}
	fmt.Fprintf(&b, "  captured=%d effective=%d\n", len(rep.Frames), effective)
	for _, frame := range rep.Frames {
		fmt.Fprintf(&b, "  %s", formatFrame(frame))
	}

	b.WriteString("\nrelease\n")
	if len(rep.Release) == 0 {
		b.WriteString("  none\n")
	} else {
		for _, release := range rep.Release {
			fmt.Fprintf(&b, "  %s %s released=%t", formatTime(release.At), release.Phase, release.Released)
			if release.Key != "" {
				fmt.Fprintf(&b, " key=%s", release.Key)
			}
			if release.Error != "" {
				fmt.Fprintf(&b, " error=%s", release.Error)
			}
			b.WriteString("\n")
		}
	}

	if rep.Abort != "" {
		fmt.Fprintf(&b, "\nabort\n  %s\n", rep.Abort)
	}

	b.WriteString("\nerrors\n")
	if len(rep.Errors) == 0 {
		b.WriteString("  none\n")
	} else {
		for _, err := range rep.Errors {
			fmt.Fprintf(&b, "  - %s\n", err)
		}
	}
	b.WriteString("\nnotes\n")
	for _, note := range rep.Notes {
		fmt.Fprintf(&b, "  - %s\n", note)
	}
	return b.String()
}

func formatFrame(frame FrameFacts) string {
	line := fmt.Sprintf("[%s %d] offset=%.1fms size=%dx%d client=%dx%d matches=%t black=%t",
		frame.Phase, frame.Index, frame.OffsetMS, frame.Width, frame.Height,
		frame.ClientWidth, frame.ClientHeight, frame.SizeMatchesClient, frame.Black)
	if frame.Metrics != nil {
		if frame.Metrics.Edit != nil {
			d := frame.Metrics.Edit
			line += fmt.Sprintf(" edit[px=%d mad=%.3f max=%d lumaDelta=%.2f]",
				d.ChangedPixels, d.MeanAbsDiff, d.MaxDelta, d.LumaDelta)
		}
		if frame.Metrics.BorderLeft != nil && frame.Metrics.BorderBottom != nil {
			left, bottom := frame.Metrics.BorderLeft, frame.Metrics.BorderBottom
			line += fmt.Sprintf(" borderL[px=%d mad=%.3f lumaDelta=%.2f] borderB[px=%d mad=%.3f lumaDelta=%.2f]",
				left.ChangedPixels, left.MeanAbsDiff, left.LumaDelta,
				bottom.ChangedPixels, bottom.MeanAbsDiff, bottom.LumaDelta)
		}
		if frame.Metrics.Detail != nil {
			d := frame.Metrics.Detail
			line += fmt.Sprintf(" detail[mad=%.3f frac=%.4f px=%d]", d.MeanAbsDiff, d.ChangedFraction, d.ChangedPixels)
		}
	}
	if frame.Effective {
		line += " effective=true"
	}
	if frame.EffectReason != "" {
		line += " reason=" + frame.EffectReason
	}
	if frame.Error != "" {
		line += " error=" + frame.Error
	}
	return line + "\n"
}

func formatPoint(p windows.PixelPoint) string {
	return fmt.Sprintf("(%d,%d)", p.X, p.Y)
}

func formatPointPtr(p *windows.PixelPoint) string {
	if p == nil {
		return "unknown"
	}
	return formatPoint(*p)
}

func formatRect(r windows.PixelRect) string {
	return fmt.Sprintf("(%d,%d)-(%d,%d) %dx%d", r.Min.X, r.Min.Y, r.Max.X, r.Max.Y, r.Dx(), r.Dy())
}

func formatRectPtr(r *windows.PixelRect) string {
	if r == nil {
		return "unknown"
	}
	return formatRect(*r)
}

func formatIntegrity(i IntegrityFacts) string {
	if i.Error != "" {
		return fmt.Sprintf("unavailable (%s)", i.Error)
	}
	return fmt.Sprintf("%s rid=0x%04X", i.Level, i.RID)
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	return t.UTC().Format(time.RFC3339Nano)
}

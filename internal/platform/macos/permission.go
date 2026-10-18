package macos

import "errors"

// Permission prechecks (T2 §2.2): screen recording, accessibility and
// event synthesis are three separate capabilities and no one of them
// substitutes for another. Accessibility is queried without prompting
// (AXIsProcessTrustedWithOptions with the prompt option off; asking for
// authorization is a user action the caller initiates). Event
// synthesis is queried via CGPreflightPostEventAccess. Screen recording
// has no preflight here by design: its assessment comes from what the
// actual ScreenCaptureKit calls returned — enumeration, stream start
// and the first valid frame — because that is the only honest evidence
// the platform has (a permission granted earlier can still fail live).

// CapabilityState is the common coarse state of one capability.
type CapabilityState string

const (
	// CapabilityGranted: the preflight query returned a positive
	// result. For screen recording this state is never claimed from a
	// preflight; only actual capture success reports availability.
	CapabilityGranted CapabilityState = "granted"
	// CapabilityNotGranted: the preflight query returned a negative
	// result.
	CapabilityNotGranted CapabilityState = "notGranted"
	// CapabilityUnknown: the query itself failed; nothing is claimed.
	CapabilityUnknown CapabilityState = "unknown"
)

// AccessibilityTrust is the result of one AXIsProcessTrustedWithOptions
// query with prompting disabled. Trusted reports whether the process
// currently passes the accessibility trust check; it says nothing
// about screen recording or event synthesis.
type AccessibilityTrust struct {
	Trusted bool
	// Err is the query failure; when set, Trusted is void.
	Err error
}

// State maps the query result to the coarse capability state.
func (t AccessibilityTrust) State() CapabilityState {
	switch {
	case t.Err != nil:
		return CapabilityUnknown
	case t.Trusted:
		return CapabilityGranted
	default:
		return CapabilityNotGranted
	}
}

// EventPostAccess is the result of one CGPreflightPostEventAccess
// query: whether the process may synthesize and post events. It is
// separate from accessibility trust (T2 §2.2: 分别检查 AX 信任与事件
// 合成能力；录屏已获准不能代替输入授权).
type EventPostAccess struct {
	Allowed bool
	// Err is the query failure; when set, Allowed is void.
	Err error
}

// State maps the query result to the coarse capability state.
func (a EventPostAccess) State() CapabilityState {
	switch {
	case a.Err != nil:
		return CapabilityUnknown
	case a.Allowed:
		return CapabilityGranted
	default:
		return CapabilityNotGranted
	}
}

// ScreenRecordingStage names one stage of the actual-capture evidence.
type ScreenRecordingStage string

const (
	// StageEnumeration: the SCShareableContent enumeration.
	StageEnumeration ScreenRecordingStage = "enumeration"
	// StageStreamStart: creating and starting the SCStream.
	StageStreamStart ScreenRecordingStage = "streamStart"
	// StageFirstFrame: the first complete frame with pixel data.
	StageFirstFrame ScreenRecordingStage = "firstFrame"
)

// ScreenRecordingState is the availability state derived from the
// actual-capture evidence. It deliberately has no "granted" value: the
// platform cannot observe the TCC database, so the strongest claim is
// that capture actually worked when tried — a permission can be
// revoked afterwards.
type ScreenRecordingState string

const (
	// ScreenRecordingAvailable: enumeration returned windows of other
	// processes, the stream started and the first complete frame with
	// pixels arrived. Everything the scan needs from screen recording
	// worked at the time it was tried.
	ScreenRecordingAvailable ScreenRecordingState = "available"
	// ScreenRecordingUnavailable: one stage failed. Stage and Err
	// preserve which one and how it failed; the caller decides whether
	// that means permission is missing or something else broke — the
	// platform does not infer a TCC state from an error code (T2 §4.3:
	// 无错误码不能反推“权限正常”，反之亦然).
	ScreenRecordingUnavailable ScreenRecordingState = "unavailable"
	// ScreenRecordingUnknown: the evidence is incomplete (for example
	// enumeration worked but no stream was started yet), so nothing is
	// claimed either way.
	ScreenRecordingUnknown ScreenRecordingState = "unknown"
)

// ScreenRecordingEvidence collects the outcomes of the actual capture
// attempts. Each stage is optional at observation time; the assessment
// derives the state from whatever is present. FirstFrameComplete and
// FirstFrameHasPixels describe the first frame callback observed after
// the stream started.
type ScreenRecordingEvidence struct {
	// EnumerationErr is the enumeration failure, if any.
	EnumerationErr error
	// OtherProcessWindows counts enumerated windows owned by processes
	// other than the caller. Without screen recording permission the
	// enumeration tends to return only the caller's own windows, so a
	// positive count is the meaningful positive signal.
	OtherProcessWindows int
	// StreamStartErr is the stream start failure, if any; nil once the
	// start completed. Before any attempt it is nil and StreamStarted
	// is false.
	StreamStartErr error
	// StreamStarted reports that the stream start completed.
	StreamStarted bool
	// FirstFrameComplete reports that a complete frame arrived.
	FirstFrameComplete bool
	// FirstFrameHasPixels reports that the first complete frame carried
	// pixel data.
	FirstFrameHasPixels bool
}

// Assessment derives the screen recording availability state from the
// evidence.
func (e ScreenRecordingEvidence) Assessment() ScreenRecordingAssessment {
	if e.EnumerationErr != nil {
		return ScreenRecordingAssessment{State: ScreenRecordingUnavailable, FailedStage: StageEnumeration, Err: e.EnumerationErr}
	}
	if e.OtherProcessWindows <= 0 {
		// Enumeration did not fail but showed no other-process windows:
		// either permission is missing or there is nothing else on
		// screen. Either way screen recording has not demonstrated the
		// capability the scan needs, so it is unavailable with the
		// observation attached rather than a permission claim.
		return ScreenRecordingAssessment{
			State:       ScreenRecordingUnavailable,
			FailedStage: StageEnumeration,
			Err:         errors.New("enumeration listed no windows of other processes (screen recording permission likely missing)"),
		}
	}
	if e.StreamStartErr != nil {
		return ScreenRecordingAssessment{State: ScreenRecordingUnavailable, FailedStage: StageStreamStart, Err: e.StreamStartErr}
	}
	if !e.StreamStarted {
		return ScreenRecordingAssessment{State: ScreenRecordingUnknown}
	}
	if !e.FirstFrameComplete || !e.FirstFrameHasPixels {
		return ScreenRecordingAssessment{State: ScreenRecordingUnknown}
	}
	return ScreenRecordingAssessment{State: ScreenRecordingAvailable}
}

// ScreenRecordingAssessment is the derived availability of screen
// recording, with the failing stage and raw error when unavailable.
type ScreenRecordingAssessment struct {
	State ScreenRecordingState
	// FailedStage and Err are set when State is unavailable.
	FailedStage ScreenRecordingStage
	Err         error
}

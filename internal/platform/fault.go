// Package platform is the unified platform port of the scan session:
// the game-independent boundary between the scan kernel's controller and
// the per-platform bases (internal/platform/macos,
// internal/platform/windows). The port owns the session vocabulary —
// preflight, target resolution, observation, frames, bounded input,
// faults and the stop sequence — while the platform bases own the native
// mechanics underneath it.
//
// Dependency direction: platform imports the platform bases, never the
// other way around; the game adapter (game/zzz/collect) and the
// application import platform only. The two adapters in this package
// carry no build tags: they depend only on the bases' exported native
// interfaces and pure logic, so both compile — and their fake-driven
// tests run — on every platform the CI builds.
package platform

import "time"

// FaultClass classifies a platform fault by what it means for the
// session, per the fault contract of the platform port:
//
//   - Setup faults make the session impossible to start; the controller
//     fails preparation immediately.
//   - Premise faults say a precondition of input is currently not met
//     (focus lost, target hidden or ambiguous, stale geometry, the point
//     outside the content region). Preparation keeps waiting for them
//     within its budget; during collection they stop the session —
//     stopping is one-way, a regained focus is just another observation.
//   - Broken faults are unrecoverable within the session (target lost,
//     geometry changed after the baseline, capture stopped, permissions
//     revoked, input blocked). They fail preparation immediately and
//     stop collection.
//   - Observation faults say the observation itself failed — the
//     enumeration errored, a foreground query timed out. They carry no
//     verdict about the target; preparation retries them within its
//     budget and collection stops on them.
type FaultClass string

const (
	ClassSetup       FaultClass = "setup"
	ClassPremise     FaultClass = "premise"
	ClassBroken      FaultClass = "broken"
	ClassObservation FaultClass = "observation"
)

// Diagnostic codes of the platform port. They follow the scan JSON
// contract's suggested codes where it has them (focusLost,
// geometryChanged, permissionRevoked, …) and are stable identifiers, not
// user-facing text. Codes outside the four classes' tables below
// (invalidOperation, permitClosed) are request-validation refusals of a
// single Dispatch, not session faults; they are classified as premise by
// default so a controller that reports one verbatim does not invent a
// new outcome kind.
const (
	// Setup.
	CodePermissionMissing          = "permissionMissing"
	CodeHotkeyUnavailable          = "hotkeyUnavailable"
	CodeTargetNotFound             = "targetNotFound"
	CodeScreenRecordingUnavailable = "screenRecordingUnavailable"
	CodeUnsupportedPlatform        = "unsupportedPlatform"
	// CodeTargetOffscreen: macOS found windows of the target bundle, but
	// every resolvable one straddles or lies outside the displays. It is
	// distinct from targetNotFound so the UI can ask the user to move
	// the cloud window fully onto one screen.
	CodeTargetOffscreen = "targetOffscreen"
	// CodeWindowSizeUnsupported: the resolved window's point size is not
	// the client profile's required size. The platform refuses before
	// capture rather than scaling an unaccepted configuration.
	CodeWindowSizeUnsupported = "windowSizeUnsupported"
	// CodeFrameConfigMismatch: a captured frame's image size or content
	// region is not the calibrated layout's configuration (for example
	// the window or stream scale changed).
	CodeFrameConfigMismatch = "frameConfigMismatch"
	// CodeClientTierNotOpened: the Windows client area is a known 16:9
	// tier, but no profile for it has field acceptance.
	CodeClientTierNotOpened = "clientTierNotOpened"
	// CodeClientTierUnsupported: the Windows client area is not one of
	// the known 16:9 tiers at all.
	CodeClientTierUnsupported = "clientTierUnsupported"
	// CodeWindowOutsideWorkArea: the client area is neither fully inside
	// the monitor's work area nor exactly the whole monitor (borderless
	// full screen).
	CodeWindowOutsideWorkArea = "windowOutsideWorkArea"
	// CodeExclusiveFullscreen: the foreground target is an exclusive
	// Direct3D full-screen application.
	CodeExclusiveFullscreen = "exclusiveFullscreen"
	// CodeIntegrityMismatch: the target process runs at a higher
	// mandatory integrity level than youkai, so UIPI will silently drop
	// SendInput events.
	CodeIntegrityMismatch = "integrityMismatch"
	// CodeIntegrityUnverifiable: a mandatory integrity level could not be
	// read. The check is fail-closed.
	CodeIntegrityUnverifiable = "integrityUnverifiable"

	// Premise.
	CodeFocusLost         = "focusLost"
	CodeTargetHidden      = "targetHidden"
	CodeTargetAmbiguous   = "targetAmbiguous"
	CodePositionOutside   = "positionOutside"
	CodeStaleGeometry     = "staleGeometry"
	CodeWindowOccluded    = "windowOccluded"
	CodeFrameContentBlank = "frameContentBlank"

	// Broken.
	CodeTargetLost        = "targetLost"
	CodeGeometryChanged   = "geometryChanged"
	CodeGeometryInvalid   = "geometryInvalid"
	CodeCaptureStopped    = "captureStopped"
	CodePermissionRevoked = "permissionRevoked"
	CodeInputBlocked      = "inputBlocked"
	CodeInputFailed       = "inputFailed"

	// Observation.
	CodeObservationFailed = "observationFailed"

	// Request-validation refusals of one Dispatch (not session faults).
	CodeInvalidOperation = "invalidOperation"
	CodePermitClosed     = "permitClosed"
)

// codeClasses is the authoritative code → class table. Every Fault the
// port and its adapters produce carries the class this table assigns.
var codeClasses = map[string]FaultClass{
	CodePermissionMissing:          ClassSetup,
	CodeHotkeyUnavailable:          ClassSetup,
	CodeTargetNotFound:             ClassSetup,
	CodeScreenRecordingUnavailable: ClassSetup,
	CodeUnsupportedPlatform:        ClassSetup,
	CodeTargetOffscreen:            ClassSetup,
	CodeWindowSizeUnsupported:      ClassSetup,
	CodeClientTierNotOpened:        ClassSetup,
	CodeClientTierUnsupported:      ClassSetup,
	CodeWindowOutsideWorkArea:      ClassSetup,
	CodeExclusiveFullscreen:        ClassSetup,
	CodeIntegrityMismatch:          ClassSetup,
	CodeIntegrityUnverifiable:      ClassSetup,
	CodeFocusLost:                  ClassPremise,
	CodeTargetHidden:               ClassPremise,
	CodeTargetAmbiguous:            ClassPremise,
	CodePositionOutside:            ClassPremise,
	CodeStaleGeometry:              ClassPremise,
	CodeWindowOccluded:             ClassPremise,
	CodeFrameContentBlank:          ClassPremise,
	CodeFrameConfigMismatch:        ClassBroken,
	CodeTargetLost:                 ClassBroken,
	CodeGeometryChanged:            ClassBroken,
	CodeGeometryInvalid:            ClassBroken,
	CodeCaptureStopped:             ClassBroken,
	CodePermissionRevoked:          ClassBroken,
	CodeInputBlocked:               ClassBroken,
	CodeInputFailed:                ClassBroken,
	CodeObservationFailed:          ClassObservation,
	CodeInvalidOperation:           ClassPremise,
	CodePermitClosed:               ClassPremise,
}

// ClassOf returns the class of a diagnostic code; unknown codes are
// classified as premise (the conservative wait-and-see default).
func ClassOf(code string) FaultClass {
	if class, ok := codeClasses[code]; ok {
		return class
	}
	return ClassPremise
}

// Fault is one platform fault or refusal: a diagnostic code, its class,
// a human-readable message and the observation time. It implements
// error so it can travel through the kernel's FaultError unchanged.
type Fault struct {
	Code    string
	Class   FaultClass
	Message string
	// Params are structured diagnostic facts (sizes, rectangles, window
	// identification, integrity levels) the UI maps to a localized
	// explanation. The platform never builds user-facing prose from
	// them; the message stays a stable English diagnostic.
	Params map[string]string
	At     time.Time
}

// NewFault builds a fault with its class derived from the code table.
func NewFault(code, message string) *Fault {
	return &Fault{Code: code, Class: ClassOf(code), Message: message, At: time.Now()}
}

// NewFaultWithParams builds a fault carrying structured diagnostic
// parameters. The map is copied; later mutation cannot change the fault.
func NewFaultWithParams(code, message string, params map[string]string) *Fault {
	return &Fault{Code: code, Class: ClassOf(code), Message: message, Params: copyParams(params), At: time.Now()}
}

func copyParams(params map[string]string) map[string]string {
	if len(params) == 0 {
		return nil
	}
	out := make(map[string]string, len(params))
	for k, v := range params {
		out[k] = v
	}
	return out
}

// Error implements error with the code first, so logs and triggers keep
// the stable identifier even when the message is trimmed.
func (f *Fault) Error() string {
	if f.Message == "" {
		return f.Code
	}
	return f.Code + ": " + f.Message
}

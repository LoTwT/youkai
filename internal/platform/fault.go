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

	// Premise.
	CodeFocusLost       = "focusLost"
	CodeTargetHidden    = "targetHidden"
	CodeTargetAmbiguous = "targetAmbiguous"
	CodePositionOutside = "positionOutside"
	CodeStaleGeometry   = "staleGeometry"

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
	CodeFocusLost:                  ClassPremise,
	CodeTargetHidden:               ClassPremise,
	CodeTargetAmbiguous:            ClassPremise,
	CodePositionOutside:            ClassPremise,
	CodeStaleGeometry:              ClassPremise,
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
	At      time.Time
}

// NewFault builds a fault with its class derived from the code table.
func NewFault(code, message string) *Fault {
	return &Fault{Code: code, Class: ClassOf(code), Message: message, At: time.Now()}
}

// Error implements error with the code first, so logs and triggers keep
// the stable identifier even when the message is trimmed.
func (f *Fault) Error() string {
	if f.Message == "" {
		return f.Code
	}
	return f.Code + ": " + f.Message
}

package windows

import (
	"fmt"
	"time"
)

// Mandatory integrity levels (Windows integrity mechanism). The values are
// the RIDs Windows defines for SECURITY_MANDATORY_*; the numeric order is
// the privilege order UIPI enforces.
type IntegrityLevel uint32

const (
	IntegrityUntrusted  IntegrityLevel = 0x0000
	IntegrityLow        IntegrityLevel = 0x1000
	IntegrityMedium     IntegrityLevel = 0x2000
	IntegrityMediumPlus IntegrityLevel = 0x2100
	IntegrityHigh       IntegrityLevel = 0x3000
	IntegritySystem     IntegrityLevel = 0x4000
	IntegrityProtected  IntegrityLevel = 0x5000
)

// String returns a stable diagnostic label, never a user-facing sentence.
func (l IntegrityLevel) String() string {
	switch l {
	case IntegrityUntrusted:
		return "untrusted"
	case IntegrityLow:
		return "low"
	case IntegrityMedium:
		return "medium"
	case IntegrityMediumPlus:
		return "mediumPlus"
	case IntegrityHigh:
		return "high"
	case IntegritySystem:
		return "system"
	case IntegrityProtected:
		return "protected"
	default:
		return fmt.Sprintf("0x%x", uint32(l))
	}
}

// IntegrityAPI reads mandatory integrity levels. The real implementation
// opens the target with PROCESS_QUERY_LIMITED_INFORMATION only and asks
// its token for TOKEN_QUERY; it never reads memory, injects code or
// changes the target. A failed query is an error, not a level: callers
// fail closed.
type IntegrityAPI interface {
	ProcessIntegrity(pid ProcessID) (IntegrityLevel, error)
	SelfIntegrity() (IntegrityLevel, error)
}

// CodeIntegrityMismatch: the target's integrity level is higher than
// youkai's; UIPI will silently drop SendInput events.
const CodeIntegrityMismatch = "integrityMismatch"

// CodeIntegrityUnverifiable: at least one integrity level could not be
// read; the premise cannot be claimed.
const CodeIntegrityUnverifiable = "integrityUnverifiable"

// VerifyIntegrity refuses a target that runs at a higher mandatory
// integrity level than the scanner, and refuses fail-closed when either
// level is unknown. Equal or lower target levels pass.
func VerifyIntegrity(target, self IntegrityLevel, targetErr, selfErr error) *PlatformFault {
	switch {
	case targetErr != nil:
		return NewFault(CodeIntegrityUnverifiable,
			"the target process integrity level is unverifiable", targetErr, PhasePrecheck, time.Now(), 0).
			WithParams(map[string]string{"self": self.String(), "detail": targetErr.Error()})
	case selfErr != nil:
		return NewFault(CodeIntegrityUnverifiable,
			"the scanner process integrity level is unverifiable", selfErr, PhasePrecheck, time.Now(), 0).
			WithParams(map[string]string{"target": target.String(), "detail": selfErr.Error()})
	case target > self:
		return NewFault(CodeIntegrityMismatch,
			fmt.Sprintf("the target process integrity level %s is higher than youkai's %s; UIPI would drop SendInput events", target, self),
			nil, PhasePrecheck, time.Now(), 0).
			WithParams(map[string]string{"target": target.String(), "self": self.String()})
	}
	return nil
}

// IntegrityPair runs both queries and applies VerifyIntegrity.
func IntegrityPair(api IntegrityAPI, pid ProcessID) (target, self IntegrityLevel, fault *PlatformFault) {
	target, targetErr := api.ProcessIntegrity(pid)
	self, selfErr := api.SelfIntegrity()
	return target, self, VerifyIntegrity(target, self, targetErr, selfErr)
}

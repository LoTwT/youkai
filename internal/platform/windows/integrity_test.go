package windows

import (
	"errors"
	"testing"
)

func TestVerifyIntegrity(t *testing.T) {
	cases := []struct {
		name      string
		target    IntegrityLevel
		self      IntegrityLevel
		targetErr error
		selfErr   error
		wantCode  string
		wantParam map[string]string
	}{
		{name: "equal medium", target: IntegrityMedium, self: IntegrityMedium},
		{name: "target lower", target: IntegrityMedium, self: IntegrityHigh},
		{name: "target high, self medium", target: IntegrityHigh, self: IntegrityMedium,
			wantCode: CodeIntegrityMismatch, wantParam: map[string]string{"target": "high", "self": "medium"}},
		{name: "target system, self high", target: IntegritySystem, self: IntegrityHigh,
			wantCode: CodeIntegrityMismatch, wantParam: map[string]string{"target": "system", "self": "high"}},
		{name: "target query failed", target: 0, self: IntegrityMedium, targetErr: errors.New("OpenProcess: access denied"),
			wantCode: CodeIntegrityUnverifiable, wantParam: map[string]string{"self": "medium"}},
		{name: "self query failed", target: IntegrityHigh, targetErr: nil, selfErr: errors.New("OpenProcessToken: access denied"),
			wantCode: CodeIntegrityUnverifiable, wantParam: map[string]string{"target": "high"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := VerifyIntegrity(tc.target, tc.self, tc.targetErr, tc.selfErr)
			if tc.wantCode == "" {
				if f != nil {
					t.Fatalf("want pass, got %v", f)
				}
				return
			}
			if f == nil || f.Code != tc.wantCode {
				t.Fatalf("want %s, got %v", tc.wantCode, f)
			}
			for k, v := range tc.wantParam {
				if f.Params[k] != v {
					t.Fatalf("param %s = %q, want %q (%v)", k, f.Params[k], v, f.Params)
				}
			}
		})
	}
}

func TestIntegrityLevelString(t *testing.T) {
	cases := map[IntegrityLevel]string{
		IntegrityUntrusted:     "untrusted",
		IntegrityLow:           "low",
		IntegrityMedium:        "medium",
		IntegrityHigh:          "high",
		IntegritySystem:        "system",
		IntegrityProtected:     "protected",
		IntegrityLevel(0x1234): "0x1234",
	}
	for level, want := range cases {
		if got := level.String(); got != want {
			t.Errorf("%#x.String() = %q, want %q", uint32(level), got, want)
		}
	}
}

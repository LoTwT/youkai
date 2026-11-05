package record

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestPositionConclusionsPreferStoredLedger pins the new contract: a
// checkpoint carrying the typed ledger reports it without re-reading the
// observed scope, even when the two differ.
func TestPositionConclusionsPreferStoredLedger(t *testing.T) {
	sh := testShell()
	sh.PositionLedger = &PositionLedger{
		Sort: SortVerificationVerified,
		Positions: []PositionConclusion{
			{Position: 1, Complete: true},
			{Position: 2, Stage: "filter", ReasonCode: "navigationMismatch"},
		},
	}
	sh.ObservedScope = json.RawMessage(`{"sort":"unverified","positions":[{"position":6,"complete":true}]}`)
	got, known := sh.PositionConclusions()
	if !known {
		t.Fatal("a stored ledger is reported as unknown")
	}
	want := []PositionConclusion{
		{Position: 1, Complete: true},
		{Position: 2, Stage: "filter", ReasonCode: "navigationMismatch"},
	}
	if !reflect.DeepEqual(got.Positions, want) {
		t.Fatalf("positions = %+v, want %+v", got.Positions, want)
	}
	if got.Sort != SortVerificationVerified {
		t.Fatalf("sort = %q, want verified", got.Sort)
	}
}

// TestPositionConclusionsReadLegacyObservedScope covers checkpoints written
// before the ledger existed: the positions and sort verification are read
// once from the observed scope the current UI also parses.
func TestPositionConclusionsReadLegacyObservedScope(t *testing.T) {
	sh := testShell()
	sh.ObservedScope = json.RawMessage(`{"kind":"zzzInventory","sort":"verified","coverage":"partial","positions":[{"position":1,"viewports":2,"visits":4,"topVerified":true,"bottomVerified":true,"complete":true},{"position":3,"complete":false,"stage":"top","reasonCode":"navigationMismatch","reason":"the top never verified"}]}`)
	got, known := sh.PositionConclusions()
	if !known {
		t.Fatal("a legacy scope with positions is reported as unknown")
	}
	if got.Sort != SortVerificationVerified {
		t.Fatalf("sort = %q, want verified", got.Sort)
	}
	want := []PositionConclusion{
		{Position: 1, Complete: true},
		{Position: 3, Stage: "top", ReasonCode: "navigationMismatch"},
	}
	if !reflect.DeepEqual(got.Positions, want) {
		t.Fatalf("positions = %+v, want %+v", got.Positions, want)
	}
}

// TestPositionConclusionsWithoutPositions checks the "cannot list" answer
// for records that have no scope at all and for scopes that carry no
// positions member. Neither may masquerade as a complete ledger.
func TestPositionConclusionsWithoutPositions(t *testing.T) {
	cases := []struct {
		name   string
		scope  json.RawMessage
		known  bool
		reason string
	}{
		{name: "no scope", reason: "a record without scope detail cannot list positions"},
		{name: "empty scope", scope: json.RawMessage(`{}`), reason: "a scope without positions cannot list them"},
		{name: "null positions", scope: json.RawMessage(`{"positions":null}`), reason: "null positions are not a list"},
		{name: "malformed positions", scope: json.RawMessage(`{"positions":"nope"}`), reason: "a malformed positions member is unreadable"},
		{name: "out of range", scope: json.RawMessage(`{"positions":[{"position":7,"complete":false}]}`), reason: "an invalid ledger is unreadable"},
		{name: "empty list", scope: json.RawMessage(`{"sort":"verified","positions":[]}`), known: true, reason: "an explicit empty list is a known empty ledger"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sh := testShell()
			sh.ObservedScope = tc.scope
			got, known := sh.PositionConclusions()
			if known != tc.known {
				t.Fatalf("known = %v, want %v (%s)", known, tc.known, tc.reason)
			}
			if tc.known && len(got.Positions) != 0 {
				t.Fatalf("positions = %+v, want empty", got.Positions)
			}
		})
	}
}

// TestDecodePositionLedgerValidatesStructure keeps malformed legacy
// position lists from becoming a stable ledger: duplicates, wrong order,
// out-of-range numbers and complete entries with a cause are unreadable.
func TestDecodePositionLedgerValidatesStructure(t *testing.T) {
	cases := []struct {
		name  string
		scope string
		ok    bool
	}{
		{
			name:  "duplicate position",
			scope: `{"positions":[{"position":1,"complete":true},{"position":1,"complete":false}]}`,
		},
		{
			name:  "descending positions",
			scope: `{"positions":[{"position":2,"complete":true},{"position":1,"complete":true}]}`,
		},
		{
			name:  "complete with a cause",
			scope: `{"positions":[{"position":1,"complete":true,"stage":"filter"}]}`,
		},
		{
			name:  "invalid sort",
			scope: `{"sort":"sideways","positions":[{"position":1,"complete":true}]}`,
		},
		{
			name:  "degraded position",
			scope: `{"positions":[{"position":2,"complete":false,"stage":"scroll","reasonCode":"positionLost"}]}`,
			ok:    true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := DecodePositionLedger(json.RawMessage(tc.scope))
			if ok != tc.ok {
				t.Fatalf("decoded = %v, want %v", ok, tc.ok)
			}
		})
	}
}

// TestPositionConclusionsLegacySortUnknown keeps a legacy scope without a
// sort member readable: the positions are known, but the sort state is not
// guessed as unverified.
func TestPositionConclusionsLegacySortUnknown(t *testing.T) {
	sh := testShell()
	sh.ObservedScope = json.RawMessage(`{"positions":[{"position":1,"complete":true}]}`)
	got, known := sh.PositionConclusions()
	if !known {
		t.Fatal("positions without a sort member are reported as unknown")
	}
	if got.Sort != SortVerificationUnknown {
		t.Fatalf("sort = %q, want unknown", got.Sort)
	}
}

// TestSortVerificationReadsScopeWithoutPositions keeps the sort verdict
// independent of the position list: a legacy scope can state the sort
// before any position is reached.
func TestSortVerificationReadsScopeWithoutPositions(t *testing.T) {
	sh := testShell()
	sh.ObservedScope = json.RawMessage(`{"kind":"zzzInventory","sort":"unverified"}`)
	if _, known := sh.PositionConclusions(); known {
		t.Fatal("a scope without positions is reported as a position ledger")
	}
	sort, known := sh.SortVerification()
	if !known || sort != SortVerificationUnverified {
		t.Fatalf("sort = %q/%v, want unverified/true", sort, known)
	}
}

// TestPositionConclusionDegraded pins the shared predicate: a position
// with its own stage or code is degraded, while a complete one or a
// session-fatal cut is not.
func TestPositionConclusionDegraded(t *testing.T) {
	cases := []struct {
		name string
		p    PositionConclusion
		want bool
	}{
		{name: "complete", p: PositionConclusion{Position: 1, Complete: true}},
		{name: "cut by session fault", p: PositionConclusion{Position: 1}},
		{name: "stage", p: PositionConclusion{Position: 1, Stage: "filter"}, want: true},
		{name: "code without stage", p: PositionConclusion{Position: 1, ReasonCode: "navigationMismatch"}, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.p.Degraded(); got != tc.want {
				t.Fatalf("Degraded = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestCompleteCoverageRequiresCompleteLedger guards the invariant the
// summary relies on: a new checkpoint may claim complete coverage only
// when the ledger lists all six positions complete and the sort verified.
func TestCompleteCoverageRequiresCompleteLedger(t *testing.T) {
	full := func() []PositionConclusion {
		positions := make([]PositionConclusion, 0, PositionCount)
		for i := 1; i <= PositionCount; i++ {
			positions = append(positions, PositionConclusion{Position: i, Complete: true})
		}
		return positions
	}
	ledgerWith := func(sort SortVerification, positions []PositionConclusion) *PositionLedger {
		return &PositionLedger{Sort: sort, Positions: positions}
	}
	cases := []struct {
		name   string
		status CoverageStatus
		ledger *PositionLedger
		ok     bool
	}{
		{name: "complete coverage with full verified ledger", status: CoverageComplete, ledger: ledgerWith(SortVerificationVerified, full()), ok: true},
		{name: "partial coverage with full verified ledger", status: CoveragePartial, ledger: ledgerWith(SortVerificationVerified, full()), ok: true},
		{name: "complete coverage with an unverified sort", status: CoverageComplete, ledger: ledgerWith(SortVerificationUnverified, full())},
		{name: "complete coverage with an unknown sort", status: CoverageComplete, ledger: ledgerWith(SortVerificationUnknown, full())},
		{name: "complete coverage with five positions", status: CoverageComplete, ledger: ledgerWith(SortVerificationVerified, full()[:PositionCount-1])},
		{name: "complete coverage with a degraded position", status: CoverageComplete, ledger: ledgerWith(SortVerificationVerified, append(full()[:PositionCount-1],
			PositionConclusion{Position: PositionCount, Stage: "bottom", ReasonCode: "navigationMismatch"}))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cp := validDoc()
			cp.Session.Coverage = Coverage{Status: tc.status}
			cp.Session.PositionLedger = tc.ledger
			err := cp.validate()
			if (err == nil) != tc.ok {
				t.Fatalf("validate = %v, want ok=%v", err, tc.ok)
			}
		})
	}
}

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/LoTwT/youkai/internal/record"
)

// P-12's first slice: a saved record's incomplete positions can be scanned
// again as a new, independent record. The source record is never rewritten,
// merged or migrated; its id only records where the requested range came
// from.
var (
	// ErrRescanUnsupported: the wired scan source cannot scan a selected
	// subset of positions.
	ErrRescanUnsupported = errors.New("app: the wired scan source cannot rescan selected positions")
	// ErrRescanRequest: the request's source id or position list is
	// malformed (empty, duplicate, unsorted or outside 1..6).
	ErrRescanRequest = errors.New("app: rescan request is invalid")
	// ErrRescanSource: the source record cannot be proven from its newest
	// saved generation, is not a Windows local record, or has no readable
	// position ledger.
	ErrRescanSource = errors.New("app: rescan source record cannot be proven")
	// ErrRescanScope: a requested position was not planned by the source or
	// is already complete.
	ErrRescanScope = errors.New("app: rescan positions are not available")
)

// RescanRequest selects the source record and wheel positions of one
// partial rescan. Positions are strictly ascending and unique; the service
// rejects anything else instead of normalizing it.
type RescanRequest struct {
	SourceRecordID string `json:"sourceRecordId"`
	Positions      []int  `json:"positions"`
}

// Validate checks the request shape without touching the store.
func (r RescanRequest) Validate() error {
	if strings.TrimSpace(r.SourceRecordID) == "" {
		return fmt.Errorf("%w: source record id is empty", ErrRescanRequest)
	}
	if err := validateScanPositions(r.Positions); err != nil {
		return fmt.Errorf("%w: %v", ErrRescanRequest, err)
	}
	return nil
}

// ScanPlan is the declared wheel-position range of one zzz inventory
// session. An empty Positions means the historical full range 1..6; a
// non-empty plan is a partial rescan and always names its source record.
type ScanPlan struct {
	Positions []int
	RescanOf  string
}

// Full reports whether the session set out to scan the historical full
// range. Only a full session may support a complete coverage conclusion.
func (p ScanPlan) Full() bool { return len(p.Positions) == 0 }

// Includes reports whether the plan covers one wheel position.
func (p ScanPlan) Includes(position int) bool {
	if p.Full() {
		return position >= 1 && position <= record.PositionCount
	}
	for _, planned := range p.Positions {
		if planned == position {
			return true
		}
	}
	return false
}

// decodeScanScopeFields decodes a scope object without letting a duplicate
// range member overwrite an earlier one. encoding/json's map/struct decode
// keeps the last duplicate silently, which could turn "positions: [4]" plus
// a later full list into a different range than the file declared.
func decodeScanScopeFields(raw json.RawMessage) (map[string]json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("declared scope: %w", err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, errors.New("declared scope is not a JSON object")
	}
	fields := make(map[string]json.RawMessage)
	seen := make(map[string]bool, 2)
	for dec.More() {
		keyToken, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("declared scope: %w", err)
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, errors.New("declared scope has a non-string key")
		}
		canonical := ""
		switch {
		case key == "positions" || key == "rescanOf":
			canonical = key
		case strings.EqualFold(key, "positions") || strings.EqualFold(key, "rescanOf"):
			return nil, fmt.Errorf("declared scope: unexpected case for %q", key)
		}
		if canonical != "" {
			if seen[canonical] {
				return nil, fmt.Errorf("declared scope: duplicate %q", canonical)
			}
			seen[canonical] = true
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, fmt.Errorf("declared scope: %w", err)
		}
		fields[key] = value
	}
	if _, err := dec.Token(); err != nil {
		return nil, fmt.Errorf("declared scope: %w", err)
	}
	return fields, nil
}

// ParseScanPlan reads the position members of a session's declared scope
// and distinguishes the historical full scan (neither member present) from
// damaged partial-rescan data. The scope stays otherwise opaque; only the
// two optional members the partial-rescan contract adds are interpreted.
// Both members must be present and valid together; a present null, an empty
// source, a missing source or an invalid position list is an error, never a
// silent fallback to the full range.
func ParseScanPlan(raw json.RawMessage) (ScanPlan, error) {
	if len(raw) == 0 || !json.Valid(raw) {
		return ScanPlan{}, errors.New("declared scope is not a JSON document")
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return ScanPlan{}, errors.New("declared scope is not a JSON object")
	}
	scope, err := decodeScanScopeFields(raw)
	if err != nil {
		return ScanPlan{}, err
	}
	positions, hasPositions := scope["positions"]
	rescanOf, hasSource := scope["rescanOf"]
	if !hasPositions {
		if hasSource {
			return ScanPlan{}, errors.New("a source record is declared without positions")
		}
		return ScanPlan{}, nil
	}
	if len(positions) == 0 || bytes.Equal(bytes.TrimSpace(positions), []byte("null")) {
		return ScanPlan{}, errors.New("positions is null")
	}
	var selected []int
	if err := json.Unmarshal(positions, &selected); err != nil {
		return ScanPlan{}, fmt.Errorf("positions: %w", err)
	}
	if err := validateScanPositions(selected); err != nil {
		return ScanPlan{}, err
	}
	if !hasSource {
		return ScanPlan{}, errors.New("positions are declared without a source record")
	}
	var source string
	if err := json.Unmarshal(rescanOf, &source); err != nil || strings.TrimSpace(source) == "" {
		return ScanPlan{}, errors.New("the source record id is empty or not a string")
	}
	return ScanPlan{Positions: append([]int(nil), selected...), RescanOf: source}, nil
}

// DeclaredScanPlan is the non-failing view used by display paths: it reports
// false for damaged partial-rescan data so the caller can say "cannot tell"
// instead of inventing a full or partial plan.
func DeclaredScanPlan(raw json.RawMessage) (ScanPlan, bool) {
	plan, err := ParseScanPlan(raw)
	return plan, err == nil
}

// ScanScopeDamaged reports valid JSON that is not a scope object or that
// actually declares one of the partial-rescan members but cannot form a
// valid plan. A missing scope or a scope with neither member is not
// damaged; real records always carry valid JSON, and the display paths must
// not turn a fixture's omitted scope into a data-damage warning.
func ScanScopeDamaged(raw json.RawMessage) bool {
	if len(raw) == 0 || !json.Valid(raw) {
		return false
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return true
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return true
	}
	for key := range fields {
		if key != "positions" && strings.EqualFold(key, "positions") {
			return true
		}
		if key != "rescanOf" && strings.EqualFold(key, "rescanOf") {
			return true
		}
	}
	_, hasPositions := fields["positions"]
	_, hasSource := fields["rescanOf"]
	if !hasPositions && !hasSource {
		return false
	}
	_, err := ParseScanPlan(raw)
	return err != nil
}

// RescanCandidatesOf lists the source record's planned positions that did
// not complete: a degraded position, one cut off by the session ending, or
// one never reached. Positions the source never planned are absent, so a
// partial-rescan record can be rescanned again without offering unrelated
// positions. ok is false when the record cannot list positions at all.
func RescanCandidatesOf(shell record.SessionShell) ([]PositionOutcome, bool) {
	if ScanScopeDamaged(shell.Scope) {
		return nil, false
	}
	plan, planKnown := DeclaredScanPlan(shell.Scope)
	ledger, ledgerKnown := shell.PositionConclusions()
	if !planKnown || !ledgerKnown {
		return nil, false
	}
	var candidates []PositionOutcome
	for _, outcome := range PositionOutcomes(ledger.Positions) {
		if !plan.Includes(outcome.Position) {
			continue
		}
		if outcome.State == PositionOutcomeComplete {
			continue
		}
		candidates = append(candidates, outcome)
	}
	return candidates, true
}

// validateScanPositions enforces the one canonical order: 1..6, strictly
// ascending, no duplicates. The traversal records its ledger in the same
// order, so a request cannot reorder positions or repeat one.
func validateScanPositions(positions []int) error {
	if len(positions) == 0 {
		return errors.New("no positions selected")
	}
	previous := 0
	for _, position := range positions {
		if position < 1 || position > record.PositionCount {
			return fmt.Errorf("position %d is outside 1..%d", position, record.PositionCount)
		}
		if position <= previous {
			return fmt.Errorf("position %d is not after position %d", position, previous)
		}
		previous = position
	}
	return nil
}

// validateRescanSource proves the request against the source record's
// newest saved generation. It is deliberately independent of the UI: the
// newest generation must decode, match the record directory's identity and
// carry readable, digest-valid evidence; the record must be a Windows local
// drive-disc scan with a readable ledger, and every requested position must
// be planned and still incomplete. An older valid generation is never
// accepted as the source.
func (s *Service) validateRescanSource(ctx context.Context, req RescanRequest) (record.SessionShell, error) {
	rec, err := s.store.LoadLatest(ctx, req.SourceRecordID)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return record.SessionShell{}, ctxErr
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return record.SessionShell{}, err
		}
		return record.SessionShell{}, fmt.Errorf("%w: %v", ErrRescanSource, err)
	}
	if err := ctx.Err(); err != nil {
		return record.SessionShell{}, err
	}
	shell := rec.Checkpoint.Session
	if shell.SessionID != req.SourceRecordID {
		return record.SessionShell{}, fmt.Errorf("%w: checkpoint session %q does not match record %q",
			ErrRescanSource, shell.SessionID, req.SourceRecordID)
	}
	if shell.Client != record.ClientWindowsLocal {
		return record.SessionShell{}, fmt.Errorf("%w: client %q is not the supported Windows local client",
			ErrRescanSource, shell.Client)
	}
	if shell.Game != "zzz" || shell.Entity != "driveDisc" {
		return record.SessionShell{}, fmt.Errorf("%w: game %q entity %q is not the drive-disc inventory",
			ErrRescanSource, shell.Game, shell.Entity)
	}
	if ScanScopeDamaged(shell.Scope) {
		return record.SessionShell{}, fmt.Errorf("%w: the declared position range is damaged", ErrRescanSource)
	}
	plan, ok := DeclaredScanPlan(shell.Scope)
	if !ok {
		return record.SessionShell{}, fmt.Errorf("%w: the declared position range is unreadable", ErrRescanSource)
	}
	ledger, ok := shell.PositionConclusions()
	if !ok {
		return record.SessionShell{}, fmt.Errorf("%w: the record has no readable position ledger", ErrRescanSource)
	}
	outcomes := make(map[int]PositionOutcome, record.PositionCount)
	for _, outcome := range PositionOutcomes(ledger.Positions) {
		outcomes[outcome.Position] = outcome
	}
	for _, position := range req.Positions {
		if !plan.Includes(position) {
			return record.SessionShell{}, fmt.Errorf("%w: position %d was not planned by the source record",
				ErrRescanScope, position)
		}
		if outcome, reached := outcomes[position]; reached && outcome.State == PositionOutcomeComplete {
			return record.SessionShell{}, fmt.Errorf("%w: position %d is already complete",
				ErrRescanScope, position)
		}
	}
	return shell, nil
}

// validateRescanSpec re-checks the source's answer after it built the spec:
// the declared scope must carry exactly the requested positions and source
// relation, and the new session must describe the same game, entity and
// client as the record it rescans. A source that ignored the range is
// refused rather than allowed to scan the whole warehouse.
func validateRescanSpec(spec ScanSpec, req RescanRequest, source record.SessionShell) error {
	if spec.Game != source.Game || spec.Entity != source.Entity || spec.Client != source.Client {
		return fmt.Errorf("%w: the adapter's provenance (%s/%s/%s) differs from the source record's (%s/%s/%s)",
			ErrRescanScope, spec.Game, spec.Entity, spec.Client, source.Game, source.Entity, source.Client)
	}
	plan, ok := DeclaredScanPlan(spec.Scope)
	if !ok || plan.Full() {
		return fmt.Errorf("%w: the adapter did not declare a partial position range", ErrRescanScope)
	}
	if plan.RescanOf != req.SourceRecordID {
		return fmt.Errorf("%w: the adapter declared source %q, want %q",
			ErrRescanScope, plan.RescanOf, req.SourceRecordID)
	}
	if !slices.Equal(plan.Positions, req.Positions) {
		return fmt.Errorf("%w: the adapter declared positions %v, want %v",
			ErrRescanScope, plan.Positions, req.Positions)
	}
	return nil
}

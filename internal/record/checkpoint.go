package record

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// The checkpoint envelope identifies the internal persistence format. It
// is deliberately not the public scan.json contract: checkpoints carry
// running state such as queued and running entries that public files never
// contain, and the public format stays frozen until export is implemented.
const (
	// FormatName identifies internal checkpoint documents on disk.
	FormatName = "youkai.checkpoint"
	// FormatVersion is the current internal checkpoint format version.
	// Readers reject an unknown version instead of guessing at a
	// compatible interpretation. Adding an optional field (omitempty,
	// tolerated as absent by readers) does not bump the version; only a
	// change to an existing field's shape or semantics does. Version
	// history: 2 added ItemRecord.Failure, the recognition failure text
	// of failed items. SessionShell.FailureParams, SessionShell.PositionLedger
	// and its position conclusions are such optional additions and keep
	// version 2.
	//
	// Cost of keeping 2: an older binary reads a new generation with
	// DisallowUnknownFields and fails on failureParams or positionLedger;
	// that downgrade is accepted because the application does not support
	// downgrades. Every finished traversal now writes positionLedger, so
	// the incompatibility applies to all records that ran a traversal, not
	// only to sessions that recorded structured failure params.
	FormatVersion = 2
)

// Client identifies which of the three confirmed entry points produced a
// session (scan JSON v1, section 2).
type Client string

const (
	ClientWindowsLocal Client = "windowsLocal"
	ClientWindowsCloud Client = "windowsCloud"
	ClientMacOSCloud   Client = "macosCloud"
)

func (c Client) valid() bool {
	switch c {
	case ClientWindowsLocal, ClientWindowsCloud, ClientMacOSCloud:
		return true
	}
	return false
}

// TerminationKind is how a session ended. An interrupted recovery is its
// own kind, distinct from a clean end, a user cancel and a failure
// (architecture section 5); the first trigger is the one that counts.
type TerminationKind string

const (
	TerminationCompleted   TerminationKind = "completed"
	TerminationCancelled   TerminationKind = "cancelled"
	TerminationFailed      TerminationKind = "failed"
	TerminationInterrupted TerminationKind = "interrupted"
)

func (k TerminationKind) valid() bool {
	switch k {
	case TerminationCompleted, TerminationCancelled, TerminationFailed, TerminationInterrupted:
		return true
	}
	return false
}

// Phase names the stage the first termination trigger occurred in, not the
// stage the process happened to be in when the file was written.
type Phase string

const (
	PhasePreparing  Phase = "preparing"
	PhaseCapturing  Phase = "capturing"
	PhaseFinalizing Phase = "finalizing"
)

func (p Phase) valid() bool {
	switch p {
	case PhasePreparing, PhaseCapturing, PhaseFinalizing:
		return true
	}
	return false
}

// CoverageStatus is the session's coverage conclusion. Complete is only
// justified by verified traversal evidence, never inferred from OCR
// success counts or an expected total alone.
type CoverageStatus string

const (
	CoverageComplete CoverageStatus = "complete"
	CoveragePartial  CoverageStatus = "partial"
	CoverageUnknown  CoverageStatus = "unknown"
)

func (s CoverageStatus) valid() bool {
	switch s {
	case CoverageComplete, CoveragePartial, CoverageUnknown:
		return true
	}
	return false
}

// ResourceRole is the role a provenance resource played in a scan.
type ResourceRole string

const (
	ResourceAdapter    ResourceRole = "adapter"
	ResourceLayout     ResourceRole = "layout"
	ResourceModel      ResourceRole = "model"
	ResourceDictionary ResourceRole = "dictionary"
)

func (r ResourceRole) valid() bool {
	switch r {
	case ResourceAdapter, ResourceLayout, ResourceModel, ResourceDictionary:
		return true
	}
	return false
}

// Status is the raw processing outcome of one captured observation: the
// five classes of the architecture, with queued and running existing only
// while a session (and its checkpoints) is live.
type Status string

const (
	StatusQueued      Status = "queued"
	StatusRunning     Status = "running"
	StatusProduced    Status = "produced"
	StatusFailed      Status = "failed"
	StatusUnprocessed Status = "unprocessed"
)

func (s Status) valid() bool {
	switch s {
	case StatusQueued, StatusRunning, StatusProduced, StatusFailed, StatusUnprocessed:
		return true
	}
	return false
}

// UnprocessedReason records why an observation ended unprocessed: the user
// cancelled before it ran, or the process died around it and recovery
// marked it interrupted.
type UnprocessedReason string

const (
	ReasonCancelled   UnprocessedReason = "cancelled"
	ReasonInterrupted UnprocessedReason = "interrupted"
)

func (r UnprocessedReason) valid() bool {
	switch r {
	case ReasonCancelled, ReasonInterrupted:
		return true
	}
	return false
}

// Quality is the field-quality state of an item's current revision,
// independent of its processing outcome: it says how trustworthy the
// current fields are, not whether the inventory was covered.
type Quality string

const (
	QualityAutoAccepted   Quality = "autoAccepted"
	QualityNeedsReview    Quality = "needsReview"
	QualityHumanConfirmed Quality = "humanConfirmed"
	QualityNotEvaluated   Quality = "notEvaluated"
)

func (q Quality) valid() bool {
	switch q {
	case QualityAutoAccepted, QualityNeedsReview, QualityHumanConfirmed, QualityNotEvaluated:
		return true
	}
	return false
}

// Termination records how a session ended: the first trigger only, with a
// diagnostic code; later faults are separate session events.
type Termination struct {
	Kind  TerminationKind `json:"kind"`
	Code  string          `json:"code"`
	Phase Phase           `json:"phase"`
}

func (t *Termination) validate() error {
	if !t.Kind.valid() {
		return fmt.Errorf("termination kind %q", t.Kind)
	}
	if t.Code == "" {
		return errors.New("termination code is empty")
	}
	if !t.Phase.valid() {
		return fmt.Errorf("termination phase %q", t.Phase)
	}
	return nil
}

// Coverage is the session's coverage conclusion and, when a trusted
// same-scope total exists, that total. ExpectedCount stays nil when the
// total is unknown instead of being faked with zero.
type Coverage struct {
	Status        CoverageStatus `json:"status"`
	ExpectedCount *int64         `json:"expectedCount,omitempty"`
}

// PositionCount is the number of wheel positions a complete drive-disc
// inventory scan covers. A position conclusion outside 1..PositionCount is
// invalid rather than tolerated as an extension.
const PositionCount = 6

// SortVerification is whether the traversal verified the sort label at
// every opened panel. Unknown is distinct from unverified: a checkpoint
// written before the observed scope carried a sort field proved neither.
type SortVerification string

const (
	SortVerificationUnknown    SortVerification = "unknown"
	SortVerificationUnverified SortVerification = "unverified"
	SortVerificationVerified   SortVerification = "verified"
)

func (s SortVerification) valid() bool {
	switch s {
	case SortVerificationUnknown, SortVerificationUnverified, SortVerificationVerified:
		return true
	}
	return false
}

// PositionConclusion is one wheel position's traversal conclusion: whether
// the walk completed it and, when it did not, the failed preparation or
// proof stage and the underlying diagnostic code. A position cut by a
// session-fatal fault keeps Stage and ReasonCode empty because its cause is
// the session termination.
type PositionConclusion struct {
	Position   int    `json:"position"`
	Complete   bool   `json:"complete"`
	Stage      string `json:"stage,omitempty"`
	ReasonCode string `json:"reasonCode,omitempty"`
}

// Degraded reports whether the position failed on its own preparation or
// proof, with a position-level cause, rather than being cut by a
// session-fatal fault (which leaves Stage and ReasonCode empty). The app
// summary and the results view share this predicate so they always count
// the same positions as incomplete.
func (p PositionConclusion) Degraded() bool {
	return !p.Complete && (p.Stage != "" || p.ReasonCode != "")
}

// PositionLedger is the stable per-position conclusion the summary and
// notification surfaces read instead of decoding the opaque observed
// scope. Sort carries the sort verification the coverage conclusion
// depends on, with unknown kept separate from unverified.
type PositionLedger struct {
	Sort      SortVerification     `json:"sort"`
	Positions []PositionConclusion `json:"positions"`
}

// validate checks the ledger's structure. Coverage consistency is checked
// against the shell's conclusion separately: the ledger may legitimately
// stop at fewer than six positions when a session ends early.
func (l *PositionLedger) validate() error {
	if !l.Sort.valid() {
		return fmt.Errorf("sort verification %q", l.Sort)
	}
	previous := 0
	for i := range l.Positions {
		p := &l.Positions[i]
		if p.Position < 1 || p.Position > PositionCount {
			return fmt.Errorf("position %d is outside 1..%d", p.Position, PositionCount)
		}
		if p.Position <= previous {
			return fmt.Errorf("position %d is not after position %d", p.Position, previous)
		}
		previous = p.Position
		if p.Complete && (p.Stage != "" || p.ReasonCode != "") {
			return fmt.Errorf("position %d is complete but carries stage %q and code %q",
				p.Position, p.Stage, p.ReasonCode)
		}
	}
	return nil
}

// validateAgainst rejects a ledger that cannot support the session's
// coverage conclusion. A complete conclusion needs all six positions
// complete and a verified sort; anything shorter or unproven is partial.
func (l *PositionLedger) validateAgainst(status CoverageStatus) error {
	if err := l.validate(); err != nil {
		return err
	}
	if status != CoverageComplete {
		return nil
	}
	if l.Sort != SortVerificationVerified {
		return fmt.Errorf("complete coverage with sort verification %q", l.Sort)
	}
	if len(l.Positions) != PositionCount {
		return fmt.Errorf("complete coverage with %d position conclusions", len(l.Positions))
	}
	for i := range l.Positions {
		if !l.Positions[i].Complete {
			return fmt.Errorf("complete coverage with incomplete position %d", l.Positions[i].Position)
		}
	}
	return nil
}

// DecodePositionLedger reads the traversal's observed-scope report into the
// stable position ledger. It is the compatibility path for checkpoints
// written before SessionShell.PositionLedger existed, and it stays the
// source the record deliverer copies from because the traversal reports
// positions only inside ObservedScope. It reports false when the scope
// cannot be read or carries no positions member, so callers record "cannot
// list" instead of inventing an empty, complete-looking ledger.
func DecodePositionLedger(raw json.RawMessage) (PositionLedger, bool) {
	if len(raw) == 0 || !json.Valid(raw) {
		return PositionLedger{}, false
	}
	var scope struct {
		Sort      *string         `json:"sort"`
		Positions json.RawMessage `json:"positions"`
	}
	if err := json.Unmarshal(raw, &scope); err != nil || len(scope.Positions) == 0 {
		return PositionLedger{}, false
	}
	var positions []PositionConclusion
	if err := json.Unmarshal(scope.Positions, &positions); err != nil || positions == nil {
		return PositionLedger{}, false
	}
	sort := SortVerificationUnknown
	if scope.Sort != nil {
		sort = SortVerification(*scope.Sort)
	}
	ledger := PositionLedger{Sort: sort, Positions: positions}
	if err := ledger.validate(); err != nil {
		return PositionLedger{}, false
	}
	return ledger, true
}

// decodeObservedSort reads the sort verdict from an observed-scope report.
// It reports false when the scope is unreadable or has no sort member, so a
// caller can distinguish "unverified" from "the record does not say". The
// sort member is independent of the positions: the precheck can record it
// before any position is reached.
func decodeObservedSort(raw json.RawMessage) (SortVerification, bool) {
	if len(raw) == 0 || !json.Valid(raw) {
		return SortVerificationUnknown, false
	}
	var scope struct {
		Sort json.RawMessage `json:"sort"`
	}
	if err := json.Unmarshal(raw, &scope); err != nil || len(scope.Sort) == 0 {
		return SortVerificationUnknown, false
	}
	var sort SortVerification
	if err := json.Unmarshal(scope.Sort, &sort); err != nil || !sort.valid() {
		return SortVerificationUnknown, false
	}
	return sort, true
}

// ResourceRef records one resource a scan actually used — adapter, layout,
// model or dictionary — with its version and digest, so a saved record
// states what produced it. The id is logical, not a file name: the
// resources/ directory holds recheck material, this list holds provenance.
type ResourceRef struct {
	Role    ResourceRole `json:"role"`
	ID      string       `json:"id"`
	Version string       `json:"version"`
	SHA256  string       `json:"sha256"`
}

func (r *ResourceRef) validate() error {
	if !r.Role.valid() {
		return fmt.Errorf("role %q", r.Role)
	}
	if r.ID == "" {
		return errors.New("id is empty")
	}
	if r.Version == "" {
		return errors.New("version is empty")
	}
	if !isSHA256Hex(r.SHA256) {
		return fmt.Errorf("sha256 %q is not 64 lowercase hex digits", r.SHA256)
	}
	return nil
}

// EvidenceFile is one evidence image as the checkpoint index declares it:
// the file below evidence/ must carry exactly this size and digest before
// a generation may reference it, and recovery re-checks both.
type EvidenceFile struct {
	ID         string `json:"id"`
	ByteLength int64  `json:"byteLength"`
	SHA256     string `json:"sha256"`
}

// SessionShell is the session's bookkeeping as every checkpoint carries
// it. Fields with stable semantics are explicit types; the game-specific
// scan scope stays an opaque JSON payload until M2's internal/game/zzz
// owns the drive-disc types.
type SessionShell struct {
	SessionID string `json:"sessionId"`
	Game      string `json:"game"`
	Entity    string `json:"entity"`
	Client    Client `json:"client"`
	Locale    string `json:"locale"`
	// Scope is what the session set out to scan, as the game adapter
	// defines it. Opaque to this package on purpose; only checked to be
	// a JSON document.
	Scope json.RawMessage `json:"scope"`
	// ObservedScope is the range the traversal's precheck actually
	// observed on the page (T5 §3's 筛选、排序、数量 snapshot) — the
	// declared Scope stays what the session set out to scan. Opaque the
	// same way; absent when no traversal reported one (the development
	// source verifies no page, and a failed precheck observes nothing).
	// It remains the compatibility source for PositionLedger on records
	// written before that field existed.
	ObservedScope json.RawMessage `json:"observedScope,omitempty"`
	StartedAt     time.Time       `json:"startedAt"`
	EndedAt       *time.Time      `json:"endedAt,omitempty"`
	// Termination is nil while the session is still running; a
	// checkpoint without it is exactly what recovery marks interrupted.
	Termination *Termination `json:"termination,omitempty"`
	// FailureParams carries the first trigger's structured diagnostic
	// facts (sizes, rectangles, window identification, integrity levels).
	// The UI maps the code plus these values to a localized explanation;
	// params never contain user-facing prose. It stays out of the public
	// scan JSON, which keeps its three-field termination shape.
	FailureParams map[string]string `json:"failureParams,omitempty"`
	Coverage      Coverage          `json:"coverage"`
	// PositionLedger is the traversal's per-position conclusion and the
	// sort verification behind it. It is optional because checkpoints
	// written before the field existed carry only ObservedScope;
	// PositionConclusions reads that fallback once. A missing ledger means
	// the positions cannot be listed, not that all six were complete.
	PositionLedger *PositionLedger `json:"positionLedger,omitempty"`
	// CapturedCount is the number of accepted observations, including
	// those that later failed or stayed unprocessed; it must equal the
	// item records, which is the accounting check that makes an
	// inconsistent checkpoint invalid.
	CapturedCount int64 `json:"capturedCount"`
	// Resources lists what the scan actually used; unrun preflight
	// resources are not invented.
	Resources []ResourceRef `json:"resources,omitempty"`
	// CoverageEvidenceIDs references the traversal, stitching and end
	// evidence behind the coverage conclusion; it verifies like item
	// evidence.
	CoverageEvidenceIDs []string `json:"coverageEvidenceIds,omitempty"`
}

func (sh *SessionShell) validate(itemCount int) error {
	if sh.SessionID == "" {
		return errors.New("session id is empty")
	}
	if sh.Game == "" {
		return errors.New("game is empty")
	}
	if sh.Entity == "" {
		return errors.New("entity is empty")
	}
	if !sh.Client.valid() {
		return fmt.Errorf("client %q", sh.Client)
	}
	if sh.Locale == "" {
		return errors.New("locale is empty")
	}
	if len(sh.Scope) == 0 || !json.Valid(sh.Scope) {
		return errors.New("scope is not a JSON document")
	}
	if bytes.Equal(bytes.TrimSpace(sh.Scope), []byte("null")) {
		return errors.New("scope is null")
	}
	if len(sh.ObservedScope) > 0 {
		if !json.Valid(sh.ObservedScope) {
			return errors.New("observedScope is not a JSON document")
		}
		if bytes.Equal(bytes.TrimSpace(sh.ObservedScope), []byte("null")) {
			return errors.New("observedScope is null")
		}
	}
	if sh.StartedAt.IsZero() {
		return errors.New("startedAt is zero")
	}
	if sh.EndedAt != nil && sh.EndedAt.IsZero() {
		return errors.New("endedAt is zero")
	}
	if sh.Termination != nil {
		if err := sh.Termination.validate(); err != nil {
			return err
		}
	}
	if !sh.Coverage.Status.valid() {
		return fmt.Errorf("coverage status %q", sh.Coverage.Status)
	}
	if sh.Coverage.ExpectedCount != nil && *sh.Coverage.ExpectedCount < 0 {
		return fmt.Errorf("expected count %d is negative", *sh.Coverage.ExpectedCount)
	}
	if sh.PositionLedger != nil {
		if err := sh.PositionLedger.validateAgainst(sh.Coverage.Status); err != nil {
			return fmt.Errorf("position ledger: %w", err)
		}
	}
	if sh.CapturedCount != int64(itemCount) {
		return fmt.Errorf("captured count %d does not match %d item records", sh.CapturedCount, itemCount)
	}
	for i := range sh.Resources {
		if err := sh.Resources[i].validate(); err != nil {
			return fmt.Errorf("resource %d: %w", i+1, err)
		}
	}
	seen := make(map[string]struct{}, len(sh.CoverageEvidenceIDs))
	for _, id := range sh.CoverageEvidenceIDs {
		if !validID(id) {
			return fmt.Errorf("coverage evidence id %q: %w", id, ErrInvalidID)
		}
		if _, dup := seen[id]; dup {
			return fmt.Errorf("coverage evidence id %q listed twice", id)
		}
		seen[id] = struct{}{}
	}
	return nil
}

// PositionConclusions returns the session's per-position conclusions and
// whether the checkpoint can list them. New checkpoints carry the typed
// ledger; older ones only have the observed scope, so this reads its
// positions member once. A checkpoint with neither, or with an unreadable
// scope, reports false — "cannot list" — rather than a zero that would read
// as "all six complete".
func (sh *SessionShell) PositionConclusions() (PositionLedger, bool) {
	if sh.PositionLedger != nil {
		ledger := *sh.PositionLedger
		ledger.Positions = append([]PositionConclusion(nil), sh.PositionLedger.Positions...)
		return ledger, true
	}
	return DecodePositionLedger(sh.ObservedScope)
}

// SortVerification returns the sort verdict and whether the checkpoint
// states one. The persisted ledger is authoritative for new records; an
// older record carries the verdict only in its observed scope. It is
// independent of PositionConclusions: a scope can state the sort before any
// position is reached.
func (sh *SessionShell) SortVerification() (SortVerification, bool) {
	if sh.PositionLedger != nil {
		s := sh.PositionLedger.Sort
		return s, s != SortVerificationUnknown && s.valid()
	}
	return decodeObservedSort(sh.ObservedScope)
}

// FieldChange is one field edit inside a manual revision, kept as audit
// history. Field names and values belong to the item payload, so both stay
// opaque here; M2's game package owns their types and the before-matches-
// previous check that needs them.
type FieldChange struct {
	Field  string          `json:"field"`
	Before json.RawMessage `json:"before"`
	After  json.RawMessage `json:"after"`
}

// ItemRevision is one persisted manual revision of an item: what changed,
// when it was saved, and whether the user confirmed the whole item.
type ItemRevision struct {
	Revision  int64         `json:"revision"`
	SavedAt   time.Time     `json:"savedAt"`
	Changes   []FieldChange `json:"changes"`
	Confirmed bool          `json:"confirmed"`
}

func (rev *ItemRevision) validate() error {
	if rev.SavedAt.IsZero() {
		return errors.New("savedAt is zero")
	}
	seen := make(map[string]struct{}, len(rev.Changes))
	for _, ch := range rev.Changes {
		if ch.Field == "" {
			return errors.New("change with an empty field name")
		}
		if _, dup := seen[ch.Field]; dup {
			return fmt.Errorf("change field %q appears twice", ch.Field)
		}
		seen[ch.Field] = struct{}{}
		if err := checkJSONValue("change before", ch.Before); err != nil {
			return err
		}
		if err := checkJSONValue("change after", ch.After); err != nil {
			return err
		}
	}
	return nil
}

// ItemRecord is one captured observation: its capture identity and
// bookkeeping in explicit types, its field payload opaque. The original
// observation (status, reason, observed payload, evidence) is sealed once
// accepted; manual edits only append revisions and move the current
// payload forward.
type ItemRecord struct {
	// Sequence is the session's capture sequence, 1..n in order.
	Sequence int64  `json:"sequence"`
	Status   Status `json:"status"`
	// Reason is required exactly when Status is unprocessed: cancelled
	// by the user or interrupted by a crash.
	Reason UnprocessedReason `json:"unprocessedReason,omitempty"`
	// Failure carries why recognition ended without a payload when
	// Status is failed; the public scan.json contract exposes the same
	// text as the item's processing reason.
	Failure string  `json:"failure,omitempty"`
	Quality Quality `json:"quality"`
	// Revision is 0 for the raw result and grows by one per persisted
	// manual revision; it always equals the number of recorded
	// revisions.
	Revision int64 `json:"revision"`
	// Observed is the original normalized payload, immutable after the
	// observation was accepted. The application owns its shape; this
	// package only checks that it is a JSON document.
	Observed json.RawMessage `json:"observed,omitempty"`
	// Data is the item's current payload; unset until a normalized
	// result exists. A produced drive disc carries the raw recognized
	// lines plus the structured interpretation; older checkpoints carry
	// only the raw lines and remain readable because the payload stays
	// opaque here.
	Data json.RawMessage `json:"data,omitempty"`
	// EvidenceIDs are the images backing this observation; every one
	// must be indexed in the checkpoint and on disk.
	EvidenceIDs []string `json:"evidenceIds,omitempty"`
	// Revisions is the manual revision history, ascending; empty while
	// nobody edited the item.
	Revisions []ItemRevision `json:"revisions,omitempty"`
}

func (it *ItemRecord) validate() error {
	if !it.Status.valid() {
		return fmt.Errorf("processing status %q", it.Status)
	}
	switch it.Status {
	case StatusUnprocessed:
		if !it.Reason.valid() {
			return fmt.Errorf("unprocessed reason %q", it.Reason)
		}
	default:
		if it.Reason != "" {
			return fmt.Errorf("status %q must not carry an unprocessed reason", it.Status)
		}
	}
	switch it.Status {
	case StatusFailed:
		if it.Failure == "" {
			return fmt.Errorf("status %q carries no failure text", it.Status)
		}
	default:
		if it.Failure != "" {
			return fmt.Errorf("status %q must not carry a failure text", it.Status)
		}
	}
	if !it.Quality.valid() {
		return fmt.Errorf("quality %q", it.Quality)
	}
	if it.Revision < 0 {
		return fmt.Errorf("revision %d is negative", it.Revision)
	}
	if it.Revision != int64(len(it.Revisions)) {
		return fmt.Errorf("revision %d does not match %d recorded revisions", it.Revision, len(it.Revisions))
	}
	for j := range it.Revisions {
		if err := it.Revisions[j].validate(); err != nil {
			return fmt.Errorf("revision entry %d: %w", j+1, err)
		}
		if it.Revisions[j].Revision != int64(j+1) {
			return fmt.Errorf("revision entry %d has number %d", j+1, it.Revisions[j].Revision)
		}
	}
	if len(it.Revisions) == 0 {
		// A raw result cannot already carry a human confirmation;
		// confirmation is itself a revision.
		if it.Quality == QualityHumanConfirmed {
			return fmt.Errorf("quality %q without any recorded revision", it.Quality)
		}
	} else {
		last := it.Revisions[len(it.Revisions)-1]
		if last.Confirmed != (it.Quality == QualityHumanConfirmed) {
			return fmt.Errorf("quality %q disagrees with the last revision's confirmed=%v", it.Quality, last.Confirmed)
		}
		if !last.Confirmed && it.Quality != QualityNeedsReview {
			return fmt.Errorf("quality %q after an unconfirmed revision", it.Quality)
		}
	}
	if err := checkPayload("observed payload", it.Observed); err != nil {
		return err
	}
	if err := checkPayload("current payload", it.Data); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(it.EvidenceIDs))
	for _, id := range it.EvidenceIDs {
		if !validID(id) {
			return fmt.Errorf("evidence id %q: %w", id, ErrInvalidID)
		}
		if _, dup := seen[id]; dup {
			return fmt.Errorf("evidence id %q listed twice", id)
		}
		seen[id] = struct{}{}
	}
	return nil
}

// Checkpoint is the internal persistence document of one scan record: the
// session shell, all item records and the evidence index, at one result
// revision. The store stamps Format, FormatVersion and SavedRevision;
// everything else is the caller's snapshot. Every save and every load goes
// through the same validation, so a corrupted file and an impossible
// document fail identically.
type Checkpoint struct {
	Format        string `json:"format"`
	FormatVersion int    `json:"formatVersion"`
	// SavedRevision is the persisted result revision this generation
	// represents; generations are named after it. The item revision and
	// the UI snapshot version are separate counters.
	SavedRevision int64          `json:"savedRevision"`
	Session       SessionShell   `json:"session"`
	Items         []ItemRecord   `json:"items"`
	Evidence      []EvidenceFile `json:"evidence"`
}

// validate checks the document's structural consistency: format identity,
// enum validity, the item/coverage accounting and the evidence index
// bijection. It sees no disk state; whether the referenced evidence files
// actually exist is a separate, deliberately cheaper check at save time
// and a full re-read at recovery time.
func (cp *Checkpoint) validate() error {
	if cp.Format != FormatName {
		return fmt.Errorf("format %q is not %q", cp.Format, FormatName)
	}
	if cp.FormatVersion != FormatVersion {
		return fmt.Errorf("format version %d is not %d", cp.FormatVersion, FormatVersion)
	}
	if cp.SavedRevision < 1 {
		return fmt.Errorf("saved revision %d is below 1", cp.SavedRevision)
	}
	if err := cp.Session.validate(len(cp.Items)); err != nil {
		return fmt.Errorf("session: %w", err)
	}
	for i := range cp.Items {
		if err := cp.Items[i].validate(); err != nil {
			return fmt.Errorf("item %d: %w", i+1, err)
		}
		if cp.Items[i].Sequence != int64(i+1) {
			return fmt.Errorf("item %d has sequence %d; sequences are 1..n in order", i+1, cp.Items[i].Sequence)
		}
	}
	return cp.validateEvidenceIndex()
}

// validateEvidenceIndex requires the evidence index and the referenced
// evidence to describe exactly the same set: an unreferenced entry wastes
// verification, a missing one would make the document unrecoverable.
func (cp *Checkpoint) validateEvidenceIndex() error {
	index := make(map[string]struct{}, len(cp.Evidence))
	for _, ev := range cp.Evidence {
		if !validID(ev.ID) {
			return fmt.Errorf("evidence entry %q: %w", ev.ID, ErrInvalidID)
		}
		if _, dup := index[ev.ID]; dup {
			return fmt.Errorf("evidence entry %q listed twice", ev.ID)
		}
		if ev.ByteLength < 1 {
			return fmt.Errorf("evidence entry %q declares %d bytes", ev.ID, ev.ByteLength)
		}
		if !isSHA256Hex(ev.SHA256) {
			return fmt.Errorf("evidence entry %q has a malformed digest", ev.ID)
		}
		index[ev.ID] = struct{}{}
	}
	referenced := cp.referencedEvidence()
	for id := range referenced {
		if _, ok := index[id]; !ok {
			return fmt.Errorf("evidence %q is referenced but not indexed", id)
		}
	}
	for id := range index {
		if _, ok := referenced[id]; !ok {
			return fmt.Errorf("evidence %q is indexed but nothing references it", id)
		}
	}
	return nil
}

// referencedEvidence returns the evidence ids the document cites: every
// item's evidence plus the coverage evidence of the shell.
func (cp *Checkpoint) referencedEvidence() map[string]struct{} {
	ids := make(map[string]struct{})
	for _, it := range cp.Items {
		for _, id := range it.EvidenceIDs {
			ids[id] = struct{}{}
		}
	}
	for _, id := range cp.Session.CoverageEvidenceIDs {
		ids[id] = struct{}{}
	}
	return ids
}

// checkPayload accepts an unset payload or a JSON document, but not a
// literal null: payloads use absence to mean "no result yet" and must not
// fake one with null, the same distinction the public contract keeps.
func checkPayload(what string, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	if err := checkJSONValue(what, raw); err != nil {
		return err
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("%s is null; omit it instead", what)
	}
	return nil
}

// checkJSONValue accepts an unset value or any valid JSON, null included:
// revision history values legitimately record unknown as null.
func checkJSONValue(what string, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	if !json.Valid(raw) {
		return fmt.Errorf("%s is not valid JSON", what)
	}
	return nil
}

// isSHA256Hex reports whether s is exactly 64 lowercase hexadecimal
// digits, the canonical SHA-256 text form used by every digest here.
func isSHA256Hex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= '0' && c <= '9' || c >= 'a' && c <= 'f' {
			continue
		}
		return false
	}
	return true
}

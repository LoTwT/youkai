package app

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/LoTwT/youkai/internal/game/zzz"
	"github.com/LoTwT/youkai/internal/record"
)

// ExportFormat is one of the two public usable-data files. The first
// version deliberately offers no other container: no ZIP, no archive
// view and no evidence.
type ExportFormat string

const (
	// ExportJSON is the *.youkai.json usable-data document.
	ExportJSON ExportFormat = "json"
	// ExportCSV is the *.youkai.csv wide table derived from the same data.
	ExportCSV ExportFormat = "csv"
)

// Extension is the file extension of a format, without the leading dot.
func (f ExportFormat) Extension() string { return string(f) }

// Public identity of the usable-data document (scan JSON v1).
const (
	exportFormatName        = "youkai.scan"
	exportFormatVersion     = 1
	exportProducerName      = "youkai"
	exportCatalogNamespace  = "youkai.zzz"
	exportValidationRuleID  = "zzzDriveDisc"
	exportTimeLayout        = "2006-01-02T15:04:05.000Z"
	exportDefaultTimeLayout = "2006-01-02"
)

// ExportSummary is what the UI shows before an export: how many captured
// items are usable, how many are excluded and the coverage conclusion
// that will be written. It never upgrades coverage.
type ExportSummary struct {
	// Captured is session.capturedCount, including failed, unprocessed and
	// not-yet-usable observations.
	Captured int64
	// Included is the number of items written to the files.
	Included int
	// Excluded is Captured minus Included; it cannot be negative.
	Excluded int
	// Coverage is the conclusion actually written after the export-side
	// guard against unsupported complete claims.
	Coverage record.CoverageStatus
	// ExpectedCount is the trusted same-scope total, when the record has
	// one; nil when unknown.
	ExpectedCount *int64
}

// ExportItem is one usable drive disc in the public shape. Issues are the
// persisted recognition/manual issues of this revision; Validation is the
// current revalidation bound to the same data and the shipped catalog.
type ExportItem struct {
	Sequence   int64          `json:"sequence"`
	Revision   int64          `json:"revision"`
	Quality    record.Quality `json:"quality"`
	Issues     []zzz.Issue    `json:"issues"`
	Validation zzz.Validation `json:"validation"`
	Data       zzz.DiscData   `json:"data"`
}

// UsableExport is one immutable, analysed snapshot of a saved record: the
// exact items, catalog subset and coverage both output formats render
// from. Building it never reads a newer revision and never writes the
// checkpoint.
type UsableExport struct {
	Summary ExportSummary

	session         exportSession
	catalog         exportCatalog
	validationRules exportValidationRules
	items           []ExportItem
}

type exportDocument struct {
	Format          string                `json:"format"`
	FormatVersion   int                   `json:"formatVersion"`
	ExportedAt      string                `json:"exportedAt"`
	Producer        exportProducer        `json:"producer"`
	Session         exportSession         `json:"session"`
	Catalog         exportCatalog         `json:"catalog"`
	FieldPolicy     exportFieldPolicy     `json:"fieldPolicy"`
	ValidationRules exportValidationRules `json:"validationRules"`
	ExcludedCount   int                   `json:"excludedCount"`
	Items           []ExportItem          `json:"items"`
}

type exportProducer struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type exportSession struct {
	ID            string             `json:"id"`
	SavedRevision int64              `json:"savedRevision"`
	Game          string             `json:"game"`
	Entity        string             `json:"entity"`
	Client        record.Client      `json:"client"`
	Locale        string             `json:"locale"`
	StartedAt     string             `json:"startedAt"`
	EndedAt       *string            `json:"endedAt"`
	Termination   record.Termination `json:"termination"`
	Scope         exportScope        `json:"scope"`
	Coverage      exportCoverage     `json:"coverage"`
	CapturedCount int64              `json:"capturedCount"`
}

type exportScope struct {
	Rarity    string `json:"rarity"`
	Slots     []int  `json:"slots"`
	MinLevel  int    `json:"minLevel"`
	MaxLevel  int    `json:"maxLevel"`
	Sets      string `json:"sets"`
	Locked    string `json:"locked"`
	Discarded string `json:"discarded"`
	Equipped  string `json:"equipped"`
}

type exportCoverage struct {
	Status        record.CoverageStatus `json:"status"`
	ExpectedCount *int64                `json:"expectedCount"`
}

// exportCatalog carries only the entries this file's items reference.
type exportCatalog struct {
	Namespace string            `json:"namespace"`
	Revision  string            `json:"revision"`
	Sets      map[string]string `json:"sets"`
	Agents    map[string]string `json:"agents"`
}

type exportFieldPolicy struct {
	Management string `json:"management"`
	Equipped   string `json:"equipped"`
}

type exportValidationRules struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

// v1Scope is the declared scan range of the first version: all S drive
// discs, every set, slot and level, with no exclusion by lock, discard or
// equipment state. A session that narrows this range needs a contract
// change; the opaque scope stored in the checkpoint describes the
// collector mode, not these field-policy semantics.
func v1Scope() exportScope {
	return exportScope{
		Rarity:    zzz.RarityS,
		Slots:     []int{1, 2, 3, 4, 5, 6},
		MinLevel:  0,
		MaxLevel:  15,
		Sets:      "all",
		Locked:    "all",
		Discarded: "all",
		Equipped:  "all",
	}
}

// BuildUsableExport analyses one saved record and freezes the data the
// two formats share. Only records the contract calls usable enter Items:
// produced structured data, quality autoAccepted or humanConfirmed, a
// passed current validation, no error in either issue array, and every
// required field present. Everything else is counted as excluded. The
// function revalidates with the shipped catalog rather than trusting a
// stale persisted conclusion, and never changes the checkpoint.
func BuildUsableExport(rec *record.Recovery) (*UsableExport, error) {
	if rec == nil {
		return nil, errors.New("app: export: record is nil")
	}
	cp := &rec.Checkpoint
	shell := &cp.Session
	if shell.SessionID == "" {
		return nil, errors.New("app: export: session id is empty")
	}
	if shell.Game != "zzz" || shell.Entity != "driveDisc" {
		return nil, fmt.Errorf("app: export: game %q entity %q is not the v1 zzz drive-disc contract", shell.Game, shell.Entity)
	}
	if shell.Locale != "zh-CN" {
		return nil, fmt.Errorf("app: export: locale %q is not the v1 zh-CN contract", shell.Locale)
	}
	if shell.StartedAt.IsZero() {
		return nil, errors.New("app: export: session start time is missing")
	}
	if shell.CapturedCount != int64(len(cp.Items)) {
		return nil, fmt.Errorf("app: export: captured count %d does not match %d item records",
			shell.CapturedCount, len(cp.Items))
	}

	export := &UsableExport{
		session: exportSession{
			ID:            shell.SessionID,
			SavedRevision: cp.SavedRevision,
			Game:          shell.Game,
			Entity:        shell.Entity,
			Client:        shell.Client,
			Locale:        shell.Locale,
			StartedAt:     formatExportTime(shell.StartedAt),
			Termination:   exportTermination(rec),
			Scope:         v1Scope(),
			Coverage:      exportCoverageOf(shell),
			CapturedCount: shell.CapturedCount,
		},
		catalog: exportCatalog{
			Namespace: exportCatalogNamespace,
			Revision:  zzz.CatalogRevision,
			Sets:      make(map[string]string),
			Agents:    make(map[string]string),
		},
		validationRules: exportValidationRules{ID: exportValidationRuleID, Version: zzz.CatalogRevision},
		items:           []ExportItem{},
	}
	if shell.EndedAt != nil {
		ended := formatExportTime(*shell.EndedAt)
		export.session.EndedAt = &ended
	}

	for i := range cp.Items {
		item := &cp.Items[i]
		exported, ok := usableItem(item)
		if !ok {
			continue
		}
		export.items = append(export.items, exported)
		set, _ := zzz.LookupSetByID(*exported.Data.SetID)
		export.catalog.Sets[strconv.Itoa(set.ID)] = set.Name
		if exported.Data.Equipped != nil && exported.Data.Equipped.AgentID != nil {
			agent, _ := zzz.LookupAgentByID(*exported.Data.Equipped.AgentID)
			export.catalog.Agents[strconv.Itoa(agent.ID)] = agent.Name
		}
	}

	export.Summary = ExportSummary{
		Captured:      shell.CapturedCount,
		Included:      len(export.items),
		Excluded:      int(shell.CapturedCount) - len(export.items),
		Coverage:      export.session.Coverage.Status,
		ExpectedCount: shell.Coverage.ExpectedCount,
	}
	return export, nil
}

// usableItem decides one record and converts it to the public item
// shape. The persisted issue list and the current validation must both be
// error-free, so a previous failed check is never upgraded by export.
func usableItem(item *record.ItemRecord) (ExportItem, bool) {
	if item.Status != record.StatusProduced {
		return ExportItem{}, false
	}
	switch item.Quality {
	case record.QualityAutoAccepted, record.QualityHumanConfirmed:
	default:
		return ExportItem{}, false
	}
	structured, ok := structuredDiscPayload(item.Data)
	if !ok {
		return ExportItem{}, false
	}
	if hasIssueError(structured.Issues) {
		return ExportItem{}, false
	}
	if structured.Validation.Status != zzz.ValidationPassed {
		return ExportItem{}, false
	}
	check := CheckDiscData(structured.Data)
	if check.Validation.Status != zzz.ValidationPassed || hasIssueError(check.Validation.Issues) {
		return ExportItem{}, false
	}
	// Passing validation implies every required field is known; the tests
	// keep this check explicit because the export must never rely on a
	// merely plausible payload.
	if structured.Data.SetID == nil || structured.Data.Rarity == nil ||
		*structured.Data.Rarity != zzz.RarityS || structured.Data.Slot == nil ||
		structured.Data.Level == nil || structured.Data.MainStat == nil ||
		structured.Data.Substats == nil || structured.Data.ManagementState == nil ||
		structured.Data.Equipped == nil {
		return ExportItem{}, false
	}
	if _, ok := zzz.LookupSetByID(*structured.Data.SetID); !ok {
		return ExportItem{}, false
	}
	if structured.Data.Equipped.State == zzz.EquippedStateEquipped {
		if structured.Data.Equipped.AgentID == nil {
			return ExportItem{}, false
		}
		if _, ok := zzz.LookupAgentByID(*structured.Data.Equipped.AgentID); !ok {
			return ExportItem{}, false
		}
	}
	issues := append([]zzz.Issue(nil), structured.Issues...)
	if issues == nil {
		issues = []zzz.Issue{}
	}
	return ExportItem{
		Sequence:   item.Sequence,
		Revision:   item.Revision,
		Quality:    item.Quality,
		Issues:     issues,
		Validation: check.Validation,
		Data:       structured.Data,
	}, true
}

// structuredDisc reads the persisted structured payload of a produced
// item, including its recognition issues and saved validation. A payload
// without a structured interpretation is not usable data.
func structuredDiscPayload(payload json.RawMessage) (zzz.StructuredDisc, bool) {
	var doc struct {
		Structured *zzz.StructuredDisc `json:"structured"`
	}
	if len(payload) == 0 {
		return zzz.StructuredDisc{}, false
	}
	if err := json.Unmarshal(payload, &doc); err != nil || doc.Structured == nil {
		return zzz.StructuredDisc{}, false
	}
	return *doc.Structured, true
}

func hasIssueError(issues []zzz.Issue) bool {
	for _, issue := range issues {
		if issue.Severity == zzz.SeverityError {
			return true
		}
	}
	return false
}

// exportTermination returns the stored first trigger. A checkpoint with
// no termination is an unfinished session: recovery marks it interrupted.
// The exact stage of the trigger is not persisted, so the export reports
// the earliest stage the record proves — preparing without any admitted
// capture, capturing once an observation exists — rather than inventing a
// later phase.
func exportTermination(rec *record.Recovery) record.Termination {
	if rec.Checkpoint.Session.Termination != nil {
		return *rec.Checkpoint.Session.Termination
	}
	phase := record.PhasePreparing
	if rec.Checkpoint.Session.CapturedCount > 0 || len(rec.Checkpoint.Items) > 0 {
		phase = record.PhaseCapturing
	}
	return record.Termination{
		Kind:  record.TerminationInterrupted,
		Code:  "interrupted",
		Phase: phase,
	}
}

// exportCoverageOf copies the saved conclusion but refuses complete when
// the observed scope never verified the sort order. Complete also needs a
// full traversal ledger, which the current record does not carry; with
// an unverified or absent sort the honest conclusion is partial.
func exportCoverageOf(shell *record.SessionShell) exportCoverage {
	status := shell.Coverage.Status
	if status == record.CoverageComplete && !sortVerified(shell.ObservedScope) {
		status = record.CoveragePartial
	}
	return exportCoverage{Status: status, ExpectedCount: shell.Coverage.ExpectedCount}
}

// sortVerified reports whether the observed scope explicitly records a
// verified sort label. The calibrated traversal writes "unverified"; an
// absent scope proves nothing.
func sortVerified(observed json.RawMessage) bool {
	if len(observed) == 0 {
		return false
	}
	var scope struct {
		Sort string `json:"sort"`
	}
	if err := json.Unmarshal(observed, &scope); err != nil {
		return false
	}
	return scope.Sort == "verified"
}

// JSON renders the usable-data document at one export time. Items and
// catalog entries come from this snapshot, not from a newer disk read.
func (e *UsableExport) JSON(exportedAt time.Time, producerVersion string) ([]byte, error) {
	if producerVersion == "" {
		return nil, errors.New("app: export: producer version is empty")
	}
	if exportedAt.IsZero() {
		return nil, errors.New("app: export: export time is missing")
	}
	doc := exportDocument{
		Format:          exportFormatName,
		FormatVersion:   exportFormatVersion,
		ExportedAt:      formatExportTime(exportedAt),
		Producer:        exportProducer{Name: exportProducerName, Version: producerVersion},
		Session:         e.session,
		Catalog:         e.catalog,
		FieldPolicy:     exportFieldPolicy{Management: "required", Equipped: "required"},
		ValidationRules: e.validationRules,
		ExcludedCount:   e.Summary.Excluded,
		Items:           e.items,
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("app: export: encode JSON: %w", err)
	}
	return buf.Bytes(), nil
}

// CSV renders the same items and the same order as JSON as one UTF-8
// wide table. A BOM makes the Chinese names show correctly in spreadsheet
// applications that otherwise assume the local code page. The item rows
// carry no evidence, issue arrays or nested session object.
func (e *UsableExport) CSV() ([]byte, error) {
	var buf bytes.Buffer
	buf.Write([]byte{0xEF, 0xBB, 0xBF}) // UTF-8 BOM for Excel-compatible opening
	w := csv.NewWriter(&buf)
	if err := w.Write(csvHeader()); err != nil {
		return nil, fmt.Errorf("app: export: write CSV header: %w", err)
	}
	for i := range e.items {
		item := &e.items[i]
		setName := ""
		if item.Data.SetID != nil {
			if set, ok := zzz.LookupSetByID(*item.Data.SetID); ok {
				setName = set.Name
			}
		}
		agentName := ""
		if item.Data.Equipped != nil && item.Data.Equipped.AgentID != nil {
			if agent, ok := zzz.LookupAgentByID(*item.Data.Equipped.AgentID); ok {
				agentName = agent.Name
			}
		}
		row := []string{
			strconv.FormatInt(item.Sequence, 10),
			e.session.ID,
			intString(item.Data.SetID),
			setName,
			intString(item.Data.Slot),
			intString(item.Data.Level),
			stringString(item.Data.Rarity),
			stringString(item.Data.ManagementState),
			equippedState(item.Data.Equipped),
			agentID(item.Data.Equipped),
			agentName,
			statPart(item.Data.MainStat, func(s *zzz.StatData) *string { return s.Attribute }),
			statPart(item.Data.MainStat, func(s *zzz.StatData) *string { return s.Value }),
			statPart(item.Data.MainStat, func(s *zzz.StatData) *string { return s.Unit }),
		}
		for sub := 0; sub < 4; sub++ {
			var line *zzz.SubstatData
			if sub < len(item.Data.Substats) {
				line = &item.Data.Substats[sub]
			}
			row = append(row,
				subPart(line, func(s *zzz.SubstatData) *string { return s.Attribute }),
				subPart(line, func(s *zzz.SubstatData) *string { return s.Value }),
				subPart(line, func(s *zzz.SubstatData) *string { return s.Unit }),
				subRolls(line),
			)
		}
		row = append(row,
			string(item.Quality),
			string(item.Validation.Status),
			strconv.FormatInt(item.Revision, 10),
		)
		if err := w.Write(row); err != nil {
			return nil, fmt.Errorf("app: export: write CSV row %d: %w", item.Sequence, err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, fmt.Errorf("app: export: write CSV: %w", err)
	}
	return buf.Bytes(), nil
}

// csvHeader is the contract's column order. The first version also names
// the session, set and agent beside their numeric identities so the file
// is readable without the JSON catalog; those three columns are additions
// requested for the usability export and do not replace any contract
// column.
func csvHeader() []string {
	header := []string{
		"sequence", "sessionId",
		"setId", "setName",
		"slot", "level", "rarity",
		"managementState", "equippedState", "agentId", "agentName",
		"mainAttribute", "mainValue", "mainUnit",
	}
	for i := 1; i <= 4; i++ {
		prefix := "sub" + strconv.Itoa(i)
		header = append(header, prefix+"Attribute", prefix+"Value", prefix+"Unit", prefix+"Rolls")
	}
	return append(header, "quality", "validationStatus", "revision")
}

// DefaultExportName suggests a file name carrying the scan date and a
// short session identity, as the workflow contract asks.
func DefaultExportName(shell record.SessionShell, format ExportFormat) string {
	date := "unknown-date"
	if !shell.StartedAt.IsZero() {
		date = shell.StartedAt.Local().Format(exportDefaultTimeLayout)
	}
	short := shell.SessionID
	if len(short) > 8 {
		short = short[:8]
	}
	if short == "" {
		short = "session"
	}
	return fmt.Sprintf("youkai-%s-%s.youkai.%s", date, short, format.Extension())
}

// formatExportTime renders the contract's UTC millisecond timestamp.
func formatExportTime(t time.Time) string {
	return t.UTC().Format(exportTimeLayout)
}

func stringString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func intString(value *int) string {
	if value == nil {
		return ""
	}
	return strconv.Itoa(*value)
}

func equippedState(equipped *zzz.Equipped) string {
	if equipped == nil {
		return ""
	}
	return equipped.State
}

func agentID(equipped *zzz.Equipped) string {
	if equipped == nil || equipped.AgentID == nil {
		return ""
	}
	return strconv.Itoa(*equipped.AgentID)
}

func statPart(stat *zzz.StatData, part func(*zzz.StatData) *string) string {
	if stat == nil {
		return ""
	}
	return stringString(part(stat))
}

func subPart(sub *zzz.SubstatData, part func(*zzz.SubstatData) *string) string {
	if sub == nil {
		return ""
	}
	return stringString(part(sub))
}

func subRolls(sub *zzz.SubstatData) string {
	if sub == nil || sub.EnhancementRolls == nil {
		return ""
	}
	return strconv.Itoa(*sub.EnhancementRolls)
}

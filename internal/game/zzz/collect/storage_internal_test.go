package collect

import (
	"errors"
	"image"
	"image/color"
	"testing"

	"github.com/LoTwT/youkai/internal/scan"
)

func TestStorageLayoutAndProfile(t *testing.T) {
	layout := StorageLayout()
	if layout.Page != PageDriveStorage || layout.Name != "winlocal-storage-1920x1080" {
		t.Fatalf("storage layout = page %s name %q", layout.Page, layout.Name)
	}
	if err := layout.Validate(); err != nil {
		t.Fatalf("StorageLayout().Validate(): %v", err)
	}
	if layout.CardPositionBadge.Size() != image.Pt(34, 26) {
		t.Fatalf("card badge window = %v, want 34x26", layout.CardPositionBadge.Size())
	}
	if layout.DetailPositionBadge.Size() != image.Pt(28, 20) {
		t.Fatalf("detail badge window = %v, want 28x20", layout.DetailPositionBadge.Size())
	}
	for i, set := range layout.CardPositions {
		if !set.valid() {
			t.Fatalf("card glyph set %d invalid", i+1)
		}
	}
	for i, set := range layout.DetailPositions {
		if !set.valid() {
			t.Fatalf("detail glyph set %d invalid", i+1)
		}
	}

	profile, err := WindowsStorageProfileFor(image.Pt(1920, 1080))
	if err != nil {
		t.Fatalf("WindowsStorageProfileFor(1920x1080): %v", err)
	}
	if profile.Layout.Page != PageDriveStorage || profile.Calibration.ScrollMaxRows != storageRows-1 {
		t.Fatalf("storage profile = %+v", profile)
	}
	inv := profile.Inventory(false)
	if !inv.RequirePanelRangeProof {
		t.Fatal("storage inventory does not require the panel range proof")
	}
	if _, ok := inv.Matcher.(*ScrollEvidenceMatcher); !ok {
		t.Fatalf("storage matcher = %T, want ScrollEvidenceMatcher", inv.Matcher)
	}
	if _, err := WindowsStorageProfileFor(image.Pt(1280, 720)); err == nil {
		t.Fatal("a 1280x720 storage profile was accepted")
	}

	cases := map[string]func(*Layout){
		"empty card window":   func(l *Layout) { l.CardPositionBadge = image.Rectangle{} },
		"empty detail window": func(l *Layout) { l.DetailPositionBadge = image.Rectangle{} },
		"empty card set":      func(l *Layout) { l.CardPositions[0] = glyphSet{} },
		"empty detail set":    func(l *Layout) { l.DetailPositions[0] = glyphSet{} },
		"oversized mask": func(l *Layout) {
			l.CardPositions[0].masks = append([]glyphMask(nil), l.CardPositions[0].masks...)
			l.CardPositions[0].masks[0] = glyphMask{rows: []string{
				"################################################################",
			}}
		},
	}
	for name, mutate := range cases {
		bad := StorageLayout()
		mutate(&bad)
		if err := bad.Validate(); err == nil {
			t.Fatalf("%s: layout validation accepted the corrupted layout", name)
		}
	}
}

// TestPositionVerdictUnknownAndConflict pins the conservative badge
// verdicts without a private frame: a unique foreground match names the
// slot, a blank window stays unknown, and two equally good masks are a
// conflict rather than a first-template guess.
func TestPositionVerdictUnknownAndConflict(t *testing.T) {
	const (
		minBrightness = 100
		minScore      = 0.9
		minMargin     = 0.2
	)
	set := func(rows ...string) glyphSet {
		return glyphSet{
			minBrightness: minBrightness,
			minScore:      minScore,
			minMargin:     minMargin,
			masks:         []glyphMask{{rows: rows}},
		}
	}
	one := set("#..", "...", "...")
	two := set("..#", "...", "...")
	window := image.Rect(0, 0, 3, 3)
	img := image.NewRGBA(image.Rect(0, 0, 3, 3))
	img.SetRGBA(0, 0, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	m := &PixelMatcher{}

	if got := m.positionVerdict(img, window, [6]glyphSet{one, two}); got != (PositionVerdict{Position: 1}) {
		t.Fatalf("unique badge = %+v, want position 1", got)
	}
	if got := m.positionVerdict(image.NewRGBA(image.Rect(0, 0, 3, 3)), window, [6]glyphSet{one, two}); got != (PositionVerdict{}) {
		t.Fatalf("blank badge = %+v, want unknown", got)
	}
	conflict := set("#..", "...", "...")
	if got := m.positionVerdict(img, window, [6]glyphSet{one, conflict}); !got.Conflict {
		t.Fatalf("equal badges = %+v, want conflict", got)
	}
	if got := m.positionVerdict(img, window, [6]glyphSet{one, two, set("...", "...", "...")}); !got.known() {
		t.Fatalf("badge with an empty third set = %+v, want position 1", got)
	}
}

func TestStorageSlotLedgerMixedPositions(t *testing.T) {
	const cells = storageRows * storageColumns
	occupied := make([]bool, cells)
	positions := make([]int, cells)
	for i := range occupied {
		occupied[i] = true
		positions[i] = i%6 + 1
	}
	ledger := newSlotLedgerWithPositions(storageColumns, occupied, positions, 1)
	if !ledger.positionsAgree(occupied, positions) {
		t.Fatal("a ledger built from the mixed positions does not agree with them")
	}
	wrong := append([]int(nil), positions...)
	wrong[5] = 2
	if ledger.positionsAgree(occupied, wrong) {
		t.Fatal("the ledger accepted a different card badge at one cell")
	}

	next := append(append([]int(nil), positions[storageColumns:]...), 1, 2, 3, 4, 5, 6, 1, 2, 3)
	ledger.advanceWithPositions(1, occupied, next, 2)
	if ledger.rowBase != 1 || len(ledger.cells) != cells+storageColumns {
		t.Fatalf("one-row advance = rowBase %d cells %d", ledger.rowBase, len(ledger.cells))
	}
	if !ledger.positionsAgree(occupied, next) {
		t.Fatal("the one-row ledger does not agree with the after positions")
	}

	// The legacy warehouse calls advance with no per-cell positions; that
	// must not slice a nil positions array.
	ledger.advance(1, occupied, 3)
	if got := len(ledger.visible()); got != cells {
		t.Fatalf("legacy advance visible cells = %d, want %d", got, cells)
	}
	if !ledger.positionsAgree(occupied, nil) {
		t.Fatal("legacy advance lost the vacuous position agreement")
	}
}

func TestStorageBottomRowKeepsEmptyCells(t *testing.T) {
	occupied := make([]bool, storageRows*storageColumns)
	positions := make([]int, len(occupied))
	last := storageColumns * (storageRows - 1)
	occupied[last] = true
	positions[last] = 6
	ledger := newSlotLedgerWithPositions(storageColumns, occupied, positions, 1)
	if !ledger.agreesWith(occupied) {
		t.Fatal("the bottom ledger does not account for the eight empty cells")
	}
	report, visits := ledger.report()
	if report == nil || len(report.Slots) != 1 || report.Slots[0].Position != 6 || visits != 0 {
		t.Fatalf("bottom report = %+v visits %d, want one item at position 6", report, visits)
	}
	changed := append([]bool(nil), occupied...)
	changed[last+1] = true
	if ledger.agreesWith(changed) {
		t.Fatal("the ledger accepted an empty cell becoming occupied without a new viewport")
	}
}

func TestStoragePositionLedgerScoped(t *testing.T) {
	positions := make([]int, storageRows*storageColumns)
	for i := range positions {
		positions[i] = i%6 + 1
	}
	occupied := make([]bool, len(positions))
	for i := range occupied {
		occupied[i] = true
	}
	ledger := newSlotLedgerWithPositions(storageColumns, occupied, positions, 1)
	run := &inventoryRun{
		ledger: ledger,
		inv: &Inventory{
			Layout:    StorageLayout(),
			positions: []int{3},
		},
	}
	report, visits := run.storagePositionLedger(3)
	if report == nil || visits != 0 {
		t.Fatalf("scoped report = %+v visits %d", report, visits)
	}
	want := 0
	for _, p := range positions {
		if p == 3 {
			want++
		}
	}
	if len(report.Slots) != want {
		t.Fatalf("position-3 ledger has %d cells, want %d", len(report.Slots), want)
	}
	for _, cell := range report.Slots {
		if cell.Position != 3 {
			t.Fatalf("scoped ledger leaked position %d", cell.Position)
		}
	}
}

func TestRecordStoragePositionsRangeAndScope(t *testing.T) {
	newRun := func(positions []int, rangeVerified bool) (*Inventory, *inventoryRun) {
		cells := make([]bool, storageRows*storageColumns)
		badges := make([]int, len(cells))
		for i := range cells {
			cells[i] = true
			badges[i] = i%6 + 1
		}
		inv := &Inventory{Layout: StorageLayout(), positions: positions}
		inv.scope.Sort = scopeSortVerified
		run := &inventoryRun{
			inv:                   inv,
			ledger:                newSlotLedgerWithPositions(storageColumns, cells, badges, 1),
			storageTopVerified:    true,
			storageBottomVerified: true,
			storageComplete:       true,
			rangeProof:            positionRange{Verified: rangeVerified},
		}
		return inv, run
	}

	inv, run := newRun(nil, true)
	run.recordStoragePositions(nil)
	if len(inv.scope.Positions) != scopePositionCount {
		t.Fatalf("full pass recorded %d positions", len(inv.scope.Positions))
	}
	for _, entry := range inv.scope.Positions {
		if !entry.Complete || !entry.Top || !entry.Bottom || !entry.Range.Verified || entry.Stage != "" {
			t.Fatalf("full pass entry = %+v, want complete with range proof", entry)
		}
	}
	if inv.scope.Coverage != scopeCoverageComplete {
		t.Fatalf("full pass coverage = %q, want complete", inv.scope.Coverage)
	}

	inv, run = newRun(nil, false)
	run.recordStoragePositions(nil)
	for _, entry := range inv.scope.Positions {
		if entry.Complete {
			t.Fatalf("entry without range proof claims complete: %+v", entry)
		}
	}
	if inv.scope.Coverage != scopeCoveragePartial {
		t.Fatalf("range-less coverage = %q, want partial", inv.scope.Coverage)
	}

	inv, run = newRun([]int{3}, true)
	run.recordStoragePositions(nil)
	if len(inv.scope.Positions) != 1 || inv.scope.Positions[0].Position != 3 ||
		!inv.scope.Positions[0].Complete {
		t.Fatalf("scoped pass positions = %+v", inv.scope.Positions)
	}
	if inv.scope.Coverage != scopeCoveragePartial {
		t.Fatalf("scoped coverage = %q, want partial", inv.scope.Coverage)
	}

	inv, run = newRun(nil, true)
	run.storageComplete = false
	run.recordStoragePositions(scan.NewFault(CodePositionUnreadable, errors.New("badge unreadable")))
	for _, entry := range inv.scope.Positions {
		if entry.Complete || entry.Stage != "storage" || entry.ReasonCode != CodePositionUnreadable {
			t.Fatalf("failed pass entry = %+v", entry)
		}
	}
	if inv.scope.Coverage != scopeCoveragePartial {
		t.Fatalf("failed coverage = %q, want partial", inv.scope.Coverage)
	}
}

func TestStoragePositionGuards(t *testing.T) {
	run := &inventoryRun{position: 3, inv: &Inventory{Layout: StorageLayout()}}
	match := PageMatch{
		List: &ListEvidence{
			Positions: []PositionVerdict{{Position: 3}},
		},
		Detail: &DetailEvidence{Position: PositionVerdict{Position: 3}},
	}
	if err := run.requireCardPosition(match, 0, "target"); err != nil {
		t.Fatalf("matching card position: %v", err)
	}
	if err := run.requireDetailPosition(match, 0, "target"); err != nil {
		t.Fatalf("matching detail position: %v", err)
	}

	match.List.Positions[0] = PositionVerdict{Position: 4}
	if err := run.requireCardPosition(match, 0, "target"); faultDiagnosticCode(err) != CodePositionConflict {
		t.Fatalf("card mismatch code = %q, want %q", faultDiagnosticCode(err), CodePositionConflict)
	}
	match.List.Positions[0] = PositionVerdict{}
	if err := run.requireCardPosition(match, 0, "target"); faultDiagnosticCode(err) != CodePositionUnreadable {
		t.Fatalf("unreadable card code = %q", faultDiagnosticCode(err))
	}
	match.List.Positions[0] = PositionVerdict{Conflict: true}
	if err := run.requireCardPosition(match, 0, "target"); faultDiagnosticCode(err) != CodePositionConflict {
		t.Fatalf("conflicting card code = %q", faultDiagnosticCode(err))
	}

	match.List.Positions[0] = PositionVerdict{Position: 3}
	match.Detail.Position = PositionVerdict{Position: 4}
	if err := run.requireDetailPosition(match, 0, "target"); faultDiagnosticCode(err) != CodePositionConflict {
		t.Fatalf("detail mismatch code = %q", faultDiagnosticCode(err))
	}
	match.Detail.Position = PositionVerdict{Conflict: true}
	if err := run.requireDetailPosition(match, 0, "target"); faultDiagnosticCode(err) != CodePositionConflict {
		t.Fatalf("conflicting detail code = %q", faultDiagnosticCode(err))
	}
}

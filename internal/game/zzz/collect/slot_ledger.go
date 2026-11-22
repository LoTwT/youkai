package collect

import "github.com/LoTwT/youkai/internal/scan"

type slotState string

const (
	slotUnresolved slotState = "unresolved"
	slotAdmitted   slotState = "admitted"
	slotNotS       slotState = "notS"
	// slotOutOfScope is a drive-storage card whose slot is not in a scoped
	// rescan's declared range. It remains in the ledger as evidence that
	// the card was seen and identified, but it is never clicked, admitted
	// or counted as a visit.
	slotOutOfScope slotState = "outOfScope"
	// slotEmpty is report-only: the drive-storage ledger keeps empty grid
	// cells so a saved report can show which positions were genuinely
	// empty. The live ledger uses the zero state for the same cells so
	// occupancy agreement stays unchanged.
	slotEmpty slotState = "empty"
)

// A slotIdentity is valid only within this position's continuous walk in
// this session. A lost position or a return to the top cannot reuse it
// without re-establishing correspondence.
type slotIdentity struct {
	AbsoluteRow int `json:"absoluteRow"`
	Column      int `json:"column"`
}

type slotEntry struct {
	slotIdentity
	// Position is the drive-storage card badge's slot (1..6). It is zero on
	// the old warehouse page, where the wheel position applies to the
	// whole list.
	Position           int       `json:"position,omitempty"`
	State              slotState `json:"state"`
	FirstSeenFrameID   uint64    `json:"firstSeenFrameId"`
	ResolvedFrameID    uint64    `json:"resolvedFrameId,omitempty"`
	Sequence           scan.Seq  `json:"sequence,omitempty"`
	RevisitFromFrameID uint64    `json:"revisitFromFrameId,omitempty"`
	RevisitReason      string    `json:"revisitReason,omitempty"`
}

// The current viewport is the suffix of cells starting at rowBase. Empty
// cells have a zero state and are kept only to check viewport occupancy.
// A scroll keeps overlap entries in place and appends only newly seen cells.
type slotLedger struct {
	rowBase int
	columns int
	cells   []slotEntry
}

func newSlotLedger(columns int, occupied []bool, frameID uint64) *slotLedger {
	return newSlotLedgerWithPositions(columns, occupied, nil, frameID)
}

func newSlotLedgerWithPositions(columns int, occupied []bool, positions []int, frameID uint64) *slotLedger {
	l := &slotLedger{columns: columns}
	l.appendCells(occupied, positions, frameID)
	return l
}

func (l *slotLedger) appendCells(occupied []bool, positions []int, frameID uint64) {
	base := len(l.cells)
	for i, present := range occupied {
		var entry slotEntry
		if present {
			index := base + i
			entry = slotEntry{
				slotIdentity:     slotIdentity{AbsoluteRow: index / l.columns, Column: index % l.columns},
				State:            slotUnresolved,
				FirstSeenFrameID: frameID,
			}
			if i < len(positions) {
				entry.Position = positions[i]
			}
		}
		l.cells = append(l.cells, entry)
	}
}

func (l *slotLedger) visible() []slotEntry { return l.cells[l.rowBase*l.columns:] }

func (l *slotLedger) agreesWith(occupied []bool) bool {
	cells := l.visible()
	if len(cells) != len(occupied) {
		return false
	}
	for i, cell := range cells {
		if occupied[i] != (cell.State != "") {
			return false
		}
	}
	return true
}

func (l *slotLedger) advance(rows int, occupied []bool, frameID uint64) {
	l.advanceWithPositions(rows, occupied, nil, frameID)
}

func (l *slotLedger) advanceWithPositions(rows int, occupied []bool, positions []int, frameID uint64) {
	start := len(occupied) - rows*l.columns
	if start < 0 {
		start = 0
	}
	tail := make([]int, len(occupied)-start)
	if len(positions) >= len(occupied) {
		copy(tail, positions[start:])
	}
	l.appendCells(occupied[start:], tail, frameID)
	l.rowBase += rows
}

// positionsAgree checks the visible viewport's card badges against the
// ledger. The old warehouse page has no per-card positions, so it agrees
// vacuously.
func (l *slotLedger) positionsAgree(occupied []bool, positions []int) bool {
	cells := l.visible()
	if len(cells) != len(occupied) {
		return false
	}
	for i, cell := range cells {
		if !occupied[i] {
			if cell.State != "" {
				return false
			}
			continue
		}
		if cell.State == "" {
			return false
		}
		if len(positions) == 0 {
			continue
		}
		if i >= len(positions) || positions[i] == 0 || positions[i] != cell.Position {
			return false
		}
	}
	return true
}

type slotLedgerReport struct {
	RowBase int         `json:"rowBase"`
	Slots   []slotEntry `json:"slots"`
}

func (l *slotLedger) report() (*slotLedgerReport, int) {
	if l == nil {
		return nil, 0
	}
	report := &slotLedgerReport{RowBase: l.rowBase, Slots: []slotEntry{}}
	visits := 0
	for _, cell := range l.cells {
		if cell.State == "" {
			continue
		}
		report.Slots = append(report.Slots, cell)
		if cell.State == slotAdmitted || cell.State == slotNotS {
			visits++
		}
	}
	return report, visits
}

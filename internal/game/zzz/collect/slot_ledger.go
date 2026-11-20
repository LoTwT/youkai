package collect

import "github.com/LoTwT/youkai/internal/scan"

type slotState string

const (
	slotUnresolved slotState = "unresolved"
	slotAdmitted   slotState = "admitted"
	slotNotS       slotState = "notS"
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
	State            slotState `json:"state"`
	FirstSeenFrameID uint64    `json:"firstSeenFrameId"`
	ResolvedFrameID  uint64    `json:"resolvedFrameId,omitempty"`
	Sequence         scan.Seq  `json:"sequence,omitempty"`
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
	l := &slotLedger{columns: columns}
	l.appendCells(occupied, frameID)
	return l
}

func (l *slotLedger) appendCells(occupied []bool, frameID uint64) {
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
	l.appendCells(occupied[len(occupied)-rows*l.columns:], frameID)
	l.rowBase += rows
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

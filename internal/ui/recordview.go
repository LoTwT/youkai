package ui

import (
	"encoding/json"
	"fmt"

	"github.com/LoTwT/youkai/internal/record"
)

// RecordView is one scan record as the results area shows it: the record
// identity, the session's shell bookkeeping and one row per captured item,
// in capture order. The live record comes from the current (or last)
// session's saved checkpoint generations; a reopened one from the history
// list, with the recovery's interruption marks.
type RecordView struct {
	// ID is the record identifier (the session's), also the directory
	// name below sessions/ its evidence lives in.
	ID string
	// Live: the record of the session the service currently reports (or
	// last reported), as opposed to one reopened from history.
	Live bool
	// Interrupted: the recovery marked items interrupted — the process
	// died around a scan that never reached a normal end.
	Interrupted bool
	// InterruptedCount is the number of items so marked.
	InterruptedCount int
	// Shell is the session's bookkeeping (client, times, termination,
	// coverage).
	Shell record.SessionShell
	// Rows are the captured items in capture order; no item disappears
	// for its status.
	Rows []Row
}

// Row is one captured item of a record: its processing outcome, its field
// quality, its observed lines and the evidence images backing them.
type Row struct {
	// Seq is the capture sequence number, 1..n in capture order.
	Seq int64
	// Status is the raw processing state: queued, running, produced,
	// failed or unprocessed.
	Status record.Status
	// Reason is why an unprocessed item never ran (cancelled or
	// interrupted).
	Reason record.UnprocessedReason
	// Failure is why recognition ended without a payload.
	Failure string
	// Quality is the field-quality state of the current revision.
	Quality record.Quality
	// Lines are the observed text lines, in observation order.
	Lines []Line
	// EvidenceIDs are the evidence images backing the observation; line i
	// pairs with EvidenceIDs[i] when it exists.
	EvidenceIDs []string
}

// Line is one observed text line of an item: its role and provenance from
// the sealed observed payload, and the recognized text with the raw model
// score from the result payload when the item produced one. The score is
// the model's confidence, not the probability the field is right.
type Line struct {
	Role   string
	Source string
	Text   string
	Score  float64
	Scored bool
}

// observedDoc is the persisted shape of an app.Observation: the line
// metadata in observation order, the pixels living in the evidence files.
type observedDoc struct {
	Lines []struct {
		Role   string `json:"role"`
		Source string `json:"source,omitempty"`
	} `json:"lines"`
}

// resultDoc is the persisted shape of an app.RecognitionResult.
type resultDoc struct {
	Lines []struct {
		Role  string  `json:"role"`
		Text  string  `json:"text"`
		Score float64 `json:"score"`
	} `json:"lines"`
}

// BuildRecordView flattens one loaded record into the results area's view.
// Payload shapes the application service authored are read defensively: a
// line that fails to parse simply keeps what did, never blocking the item
// from showing.
func BuildRecordView(id string, rec *record.Recovery, live bool) *RecordView {
	cp := &rec.Checkpoint
	view := &RecordView{
		ID:               id,
		Live:             live,
		Interrupted:      len(rec.Interrupted) > 0,
		InterruptedCount: len(rec.Interrupted),
		Shell:            cp.Session,
	}
	for i := range cp.Items {
		it := &cp.Items[i]
		row := Row{
			Seq:         it.Sequence,
			Status:      it.Status,
			Reason:      it.Reason,
			Failure:     it.Failure,
			Quality:     it.Quality,
			EvidenceIDs: append([]string(nil), it.EvidenceIDs...),
		}
		row.Lines = mergeLines(it.Observed, it.Data)
		view.Rows = append(view.Rows, row)
	}
	return view
}

// mergeLines zips an item's observed lines with its recognized ones by
// observation order.
func mergeLines(observed, data json.RawMessage) []Line {
	var obs observedDoc
	if len(observed) > 0 {
		_ = json.Unmarshal(observed, &obs) // a bad shape leaves the lines it has
	}
	var res resultDoc
	if len(data) > 0 {
		_ = json.Unmarshal(data, &res)
	}
	n := len(obs.Lines)
	if len(res.Lines) > n {
		n = len(res.Lines)
	}
	if n == 0 {
		return nil
	}
	lines := make([]Line, n)
	for i := range lines {
		if i < len(obs.Lines) {
			lines[i].Role = obs.Lines[i].Role
			lines[i].Source = obs.Lines[i].Source
		}
		if i < len(res.Lines) {
			lines[i].Text = res.Lines[i].Text
			lines[i].Score = res.Lines[i].Score
			lines[i].Scored = true
		}
	}
	return lines
}

// RowOf returns the row of the capture sequence seq, or -1.
func (v *RecordView) RowOf(seq int64) int {
	if seq == 0 {
		return -1
	}
	for i := range v.Rows {
		if v.Rows[i].Seq == seq {
			return i
		}
	}
	return -1
}

// StatusText renders a processing state in the interface's language.
func (r Row) StatusText() string {
	switch r.Status {
	case record.StatusProduced:
		return "已产出"
	case record.StatusFailed:
		return "失败"
	case record.StatusUnprocessed:
		switch r.Reason {
		case record.ReasonCancelled:
			return "未处理（用户取消）"
		case record.ReasonInterrupted:
			return "未处理（中断）"
		default:
			return "未处理"
		}
	case record.StatusQueued:
		return "排队"
	case record.StatusRunning:
		return "识别中"
	default:
		return string(r.Status)
	}
}

// QualityText renders a field-quality state.
func (r Row) QualityText() string {
	switch r.Quality {
	case record.QualityAutoAccepted:
		return "自动接受"
	case record.QualityNeedsReview:
		return "待校对"
	case record.QualityHumanConfirmed:
		return "人工确认"
	case record.QualityNotEvaluated:
		return "未评价"
	default:
		return string(r.Quality)
	}
}

// Summary is the one-line content of the results table: the first
// recognized text of a produced item, the failure of a failed one, the
// reason of an unprocessed one. Pending items show nothing rather than a
// guess.
func (r Row) Summary() string {
	switch r.Status {
	case record.StatusProduced:
		for _, l := range r.Lines {
			if l.Text != "" {
				return l.Text
			}
		}
		return ""
	case record.StatusFailed:
		return "识别失败：" + r.Failure
	case record.StatusUnprocessed:
		return r.StatusText()
	default:
		return ""
	}
}

// Label is the results table's row text for assistive technology.
func (r Row) Label() string {
	return fmt.Sprintf("%d %s %s", r.Seq, r.StatusText(), r.Summary())
}

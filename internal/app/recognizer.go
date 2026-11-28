package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/LoTwT/youkai/internal/game/zzz"
	"github.com/LoTwT/youkai/internal/game/zzz/collect"
	"github.com/LoTwT/youkai/internal/ocrlayout"
	"github.com/LoTwT/youkai/internal/recognition"
	"github.com/LoTwT/youkai/internal/scan"
)

// The OCR artifacts and their names live in internal/ocrlayout, shared
// with the fetchdeps packaging tool. MyGo's packaging merges the
// per-platform resource directories into the root of the shipped resources
// (see the framework's resources documentation), so a packaged app — and
// the development app mygo dev stages — finds the runtime library at
// onnxruntime/ and the shared model at models/, independently of the
// platform directory the fetchdeps tool downloads into
// (docs/specs/t3-ocr-runtime-model-samples.md 固定组合与制品静态核查).

// OCRResources resolves the OCR runtime and model paths below a resources
// root, as the bundled layout ships them. It backs both the application
// service's recognizer and the entry point's install self-check (see
// recognition.SelfCheck), so the two always load the same packaged copies.
// Paths stay absolute and rooted at resourcesRoot so loading never depends on
// the working directory or the system library search path
// (docs/specs/scanner-architecture.md §4).
func OCRResources(resourcesRoot string) (recognition.Resources, error) {
	lib := ocrlayout.RuntimeLibName(runtime.GOOS)
	if lib == "" {
		return recognition.Resources{}, fmt.Errorf("app: no pinned ONNX runtime artifact for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	if resourcesRoot == "" {
		return recognition.Resources{}, errors.New("app: resources root is empty")
	}
	return recognition.Resources{
		LibraryPath: filepath.Join(resourcesRoot, "onnxruntime", lib),
		ModelPath:   filepath.Join(resourcesRoot, "models", ocrlayout.ModelName),
	}, nil
}

// newOCRRecognizer creates the real recognizer from a resources root:
// one model session over the bundled ONNX Runtime environment. The
// process-wide environment outlives the session (see recognition.Shutdown)
// and the returned adapter is closed only by the explicit-exit wrap-up.
func newOCRRecognizer(resourcesRoot string) (Recognizer, error) {
	res, err := OCRResources(resourcesRoot)
	if err != nil {
		return nil, err
	}
	rec, err := recognition.New(res)
	if err != nil {
		return nil, err
	}
	return &lineRecognizer{rec: rec}, nil
}

// lineRecognizer adapts the OCR recognizer to the scan kernel's
// Recognizer: one observation — the text-line crops of one item — is
// recognized line by line, and the per-line texts and scores travel on as
// the task's result payload. After recognition the lines are assembled by
// role into the drive-disc parser's input, and the parsed structured
// payload rides beside the untouched raw lines. The parser judges field
// values; recognition itself still does not guess at a decoded line.
//
// The wrapped *recognition.Recognizer belongs to the application's
// lifetime: it is created once for the first scan and closed once by the
// explicit-exit wrap-up, never per scan (docs/specs/scanner-architecture.md
// §4 释放顺序).
type lineRecognizer struct {
	rec *recognition.Recognizer
}

// Recognize implements scan.Recognizer. A hard recognition error that
// belongs to this one observation is reported as the task's failure; a
// broken recognizer or runtime is returned as an error and stops the
// session; a stop's cancellation returns without an outcome, leaving the
// entry unprocessed to the kernel.
func (r *lineRecognizer) Recognize(ctx context.Context, task scan.Task, submit scan.Submit) error {
	obs, ok := task.Observation.(Observation)
	if !ok {
		return fmt.Errorf("app: observation of task %d is %T, not app.Observation", task.Seq, task.Observation)
	}
	if ctx.Err() != nil {
		return nil
	}
	result := RecognitionResult{Lines: make([]RecognizedLine, 0, len(obs.Lines))}
	// Context (the rarity badge and the selected cell border) is admission
	// evidence only: it is excluded from recognition on purpose.
	for _, crop := range obs.Lines {
		if ctx.Err() != nil {
			return nil
		}
		line, err := r.rec.Recognize(ctx, crop.Image)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			if errors.Is(err, recognition.ErrInvalidImage) {
				// One unusable crop fails this one observation, not the
				// session.
				return submit(task.SessionID, task.Seq, scan.Recognition{Failed: err})
			}
			// Anything else — inference failure, a closed recognizer, a
			// broken runtime — stops the whole session.
			return err
		}
		result.Lines = append(result.Lines, RecognizedLine{Role: crop.Role, Text: line.Text, Score: line.Score})
	}
	result.Structured = structuredDisc(obs, result.Lines)
	log.Printf("youkai: scan %s item %d recognized: %s", task.SessionID, task.Seq, formatLines(result.Lines))
	return submit(task.SessionID, task.Seq, scan.Recognition{Result: result})
}

// structuredDisc interprets the recognized lines of one observation as a
// drive-disc detail page. The detail-line parser consumes the name, level,
// main and sub1..sub4 roles; rarity comes from the observation's verified
// capture evidence, never from the text. The equipper identity comes from
// the avatar matcher's accepted id (zero when unreliable) and is passed
// through unchanged. A result without any detail-line role (for example a
// context-only observation) carries no structured payload rather than a
// fabricated empty disc.
func structuredDisc(obs Observation, lines []RecognizedLine) *zzz.StructuredDisc {
	detail, ok := discDetailLines(lines)
	if !ok {
		return nil
	}
	// The Windows card and detail badges independently confirm this slot.
	// Repair a dropped closing bracket only when the one visible digit
	// agrees and the remaining text names a known set. Keep the raw OCR
	// line in RecognitionResult and leave ambiguous suffixes to ParseDetail.
	if obs.Position >= 1 && obs.Position <= 6 {
		name, suffix, found := strings.Cut(strings.TrimSpace(detail.Set), "[")
		// The drive-storage OCR can omit 岿 in this catalog name.
		// Require the title's slot to agree with the two capture badges.
		if name == "云如我" && len(suffix) == 2 && suffix[0] == '0'+byte(obs.Position) && suffix[1] == ']' {
			detail.Set = "云岿如我[" + suffix
		}
		// The matching field crops show a closing bracket, read as 1.
		if name == "拂晓行纪" && suffix == "41" && obs.Position == 4 {
			detail.Set = "拂晓行纪[4]"
		}
		if found && len(suffix) == 1 && suffix[0] == '0'+byte(obs.Position) {
			if _, known := zzz.LookupSetByName(strings.TrimSpace(name)); known {
				detail.Set = strings.TrimSpace(detail.Set) + "]"
			}
		}
	}
	rec, issues := zzz.ParseDetail(detail)
	// The traversal already compared the card badge with the selected card
	// and the visible detail badge. The name line's [N] suffix is the OCR
	// side of the same fact; if it disagrees, the slot is unknown rather
	// than choosing one of the two readings.
	if obs.Position >= 1 && obs.Position <= 6 && rec.Slot != 0 && rec.Slot != obs.Position {
		issues = append(issues, zzz.Issue{
			Field:    "slot",
			Code:     zzz.CodeSlotPositionMismatch,
			Severity: zzz.SeverityError,
			Message: fmt.Sprintf("详情标题槽位 [%d] 与遍历确认的卡片槽位 %d 不一致；槽位记为未知",
				rec.Slot, obs.Position),
		})
		rec.Slot = 0
	}
	structured := zzz.ToStructuredWithState(rec, obs.Rarity, zzz.CaptureState{
		ManagementState: obs.ManagementState,
		EquippedState:   obs.EquippedState,
		AgentID:         obs.AgentID,
	}, issues)
	return &structured
}

// discDetailLines assembles the fixed detail-line roles in observation
// order. The second result is false when no drive-disc role is present.
func discDetailLines(lines []RecognizedLine) (zzz.DetailLines, bool) {
	var detail zzz.DetailLines
	found := false
	for _, line := range lines {
		switch line.Role {
		case collect.LineName:
			detail.Set = line.Text
			found = true
		case collect.LineLevel:
			detail.Level = line.Text
			found = true
		case collect.LineMain:
			detail.Main = line.Text
			found = true
		case collect.LineSub1, collect.LineSub2, collect.LineSub3, collect.LineSub4:
			detail.Subs = append(detail.Subs, line.Text)
			found = true
		}
	}
	return detail, found
}

// Close releases the model session; the process-wide environment stays
// for recognition.Shutdown.
func (r *lineRecognizer) Close() error {
	return r.rec.Close()
}

// formatLines renders recognized lines compactly for the development
// log: role=text(score) joined by spaces.
func formatLines(lines []RecognizedLine) string {
	out := ""
	for i, line := range lines {
		if i > 0 {
			out += " "
		}
		out += fmt.Sprintf("%s=%s(%.3f)", line.Role, line.Text, line.Score)
	}
	return out
}

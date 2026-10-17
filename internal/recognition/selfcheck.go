package recognition

import (
	"bytes"
	"context"
	"fmt"
	"image/png"

	_ "embed"
)

// The install-time recognition self-check: one embedded text-line crop with a
// known transcription, recognized through the full pipeline against the
// artifacts an installed app actually bundles. The entry point (cmd/youkai)
// runs it only behind the explicit YOUKAI_SELFTEST environment switch —
// before the application service, locks and user data are touched — to prove
// a packaged build loads its own copies of the native library and the model
// and decodes a fixed sample correctly. It is not a user-facing feature and
// no arbitrary image import.

// selfCheckImage is the fixed crop testdata/disc1_level.png of this package.
// The go tool ignores testdata when matching package files, but go:embed
// embeds from it like from any other subdirectory, so the self-check reuses
// the exact file the integration test asserts instead of a second copy.
//
//go:embed testdata/disc1_level.png
var selfCheckImage []byte

// selfCheckWant is the expected transcription of selfCheckImage as recorded
// in testdata/lines.tsv (disc1_level.png); the integration test asserts the
// same exact match, so a mismatch here means the pipeline or the bundled
// artifacts differ from the tested combination, not that OCR is imperfect.
const selfCheckWant = "等级15/15"

// SelfCheck loads the ONNX Runtime library and the recognition model from res
// and recognizes the embedded fixed sample end to end (preprocessing,
// inference, CTC decoding). It returns the recognized result; the check fails
// when any step errors or the decoded text differs from the recorded
// transcription.
//
// The check opens and closes its own recognizer; the process-wide ONNX
// Runtime environment it initialized stays loaded, exactly as the application
// service's first scan leaves it — call [Shutdown] when the process is done
// with recognition, as the entry point's self-test mode does.
func SelfCheck(res Resources) (Result, error) {
	img, err := png.Decode(bytes.NewReader(selfCheckImage))
	if err != nil {
		return Result{}, fmt.Errorf("self-check: decoding embedded sample: %w", err)
	}
	rec, err := New(res)
	if err != nil {
		return Result{}, err
	}
	got, err := rec.Recognize(context.Background(), img)
	if closeErr := rec.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return Result{}, fmt.Errorf("self-check: %w", err)
	}
	if got.Text != selfCheckWant {
		return got, fmt.Errorf("self-check: decoded %q, want %q", got.Text, selfCheckWant)
	}
	return got, nil
}

package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// TestBuiltInControlsUseSimplifiedChinese guards the zh-Hans catalog link
// in main.go: SetLanguages alone leaves MyGo's built-in controls in
// English, so the package must be imported (R-15). A built-in calendar's
// month buttons carry the catalog's labels, proving the catalog is
// registered in this binary.
func TestBuiltInControlsUseSimplifiedChinese(t *testing.T) {
	date := time.Date(2026, 11, 5, 21, 40, 0, 0, time.Local)
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Calendar(c, &date)
	}, 360, 420)
	tt.SetLocale("zh-Hans")
	if _, ok := tt.Find("下个月"); !ok {
		t.Fatalf("the built-in calendar is not Chinese: %q", tt.Texts())
	}
}

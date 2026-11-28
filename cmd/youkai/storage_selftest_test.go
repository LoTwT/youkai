package main

import (
	"encoding/json"
	"testing"

	"github.com/LoTwT/youkai/internal/storageconfig"
)

func TestWindowsStorageSelfTestReportsVerifiedDefaults(t *testing.T) {
	// Normal app startup must not pick up stale performance overrides.
	env := envOf(map[string]string{
		"YOUKAI_STORAGE_MATCH_AB":     "A",
		"YOUKAI_STORAGE_FAST_BADGES":  "1",
		"YOUKAI_DEFER_WRAP_LIVE_READ": "0",
	})
	line, err := verifyWindowsStorageSelfTest(env)
	if err != nil {
		t.Fatal(err)
	}
	var actual struct {
		Source string
		Model  struct{ Version, SHA256 string }
		Config storageconfig.Config
	}
	if err := json.Unmarshal([]byte(line), &actual); err != nil {
		t.Fatal(err)
	}
	want := storageconfig.Config{
		ReuseFrameMatch:  true,
		FastBeforeClick:  true,
		PacedDetail:      true,
		BatchEvidence:    true,
		SavedSnapshot:    true,
		DeferWrapRead:    true,
		ReuseScrollPages: true,
	}
	if actual.Source != "drive-storage" || actual.Config != want ||
		len(actual.Model.Version) != 64 || len(actual.Model.SHA256) != 64 {
		t.Fatalf("unexpected Windows startup profile: %s", line)
	}
	if mac := storageconfig.ForApp("darwin", envOf(nil)); mac != (storageconfig.Config{}) {
		t.Fatalf("unexpected macOS defaults: %+v", mac)
	}
}

func TestWindowsStorageSelfTestRejectsDifferentSettings(t *testing.T) {
	env := envOf(map[string]string{"YOUKAI_GAME_SOURCE": "drive-storage"})
	if _, err := verifyWindowsStorageSelfTest(env); err == nil {
		t.Fatal("expected the self-test to reject an explicit drive-storage source without the verified settings")
	}
}

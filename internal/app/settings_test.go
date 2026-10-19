package app

import (
	"os"
	"path/filepath"
	"testing"
)

// A fresh data root has the documented default: closing hides to the tray.
func TestLoadSettingsMissingFileUsesDefault(t *testing.T) {
	settings, err := LoadSettings(t.TempDir())
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if settings.CloseBehavior != CloseBehaviorHide {
		t.Fatalf("close behavior = %q, want %q", settings.CloseBehavior, CloseBehaviorHide)
	}
}

// The preference round-trips through the settings file.
func TestSaveSettingsRoundTrip(t *testing.T) {
	root := t.TempDir()
	if err := SaveSettings(root, Settings{CloseBehavior: CloseBehaviorExit}); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	settings, err := LoadSettings(root)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if settings.CloseBehavior != CloseBehaviorExit {
		t.Fatalf("close behavior = %q, want %q", settings.CloseBehavior, CloseBehaviorExit)
	}

	// A second save replaces the first value.
	if err := SaveSettings(root, Settings{CloseBehavior: CloseBehaviorHide}); err != nil {
		t.Fatalf("SaveSettings (replace): %v", err)
	}
	settings, err = LoadSettings(root)
	if err != nil {
		t.Fatalf("LoadSettings (replace): %v", err)
	}
	if settings.CloseBehavior != CloseBehaviorHide {
		t.Fatalf("close behavior = %q, want %q", settings.CloseBehavior, CloseBehaviorHide)
	}
}

// A file without the field (written by an older build) loads the default.
func TestLoadSettingsWithoutCloseBehaviorUsesDefault(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, SettingsFileName), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err := LoadSettings(root)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if settings.CloseBehavior != CloseBehaviorHide {
		t.Fatalf("close behavior = %q, want %q", settings.CloseBehavior, CloseBehaviorHide)
	}
}

// Unknown values and malformed documents fall back to the default with an
// error, so the entry point can log the problem and still start.
func TestLoadSettingsInvalidFallsBackToDefault(t *testing.T) {
	cases := map[string]string{
		"unknown behavior": `{"closeBehavior":"minimize"}`,
		"malformed json":   `{"closeBehavior":`,
	}
	for name, contents := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, SettingsFileName), []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			settings, err := LoadSettings(root)
			if err == nil {
				t.Fatal("LoadSettings accepted an invalid document")
			}
			if settings.CloseBehavior != CloseBehaviorHide {
				t.Fatalf("close behavior = %q, want the default %q", settings.CloseBehavior, CloseBehaviorHide)
			}
		})
	}
}

// SaveSettings refuses unknown values instead of persisting a preference the
// next launch would reject, and creates the data root when needed.
func TestSaveSettingsValidatesAndCreatesRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nested", "Youkai")
	if err := SaveSettings(root, Settings{CloseBehavior: "invalid"}); err == nil {
		t.Fatal("SaveSettings accepted an unknown close behavior")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("an invalid save touched the data root: %v", err)
	}
	if err := SaveSettings(root, Settings{}); err != nil {
		t.Fatalf("SaveSettings with the zero value: %v", err)
	}
	settings, err := LoadSettings(root)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if settings.CloseBehavior != CloseBehaviorHide {
		t.Fatalf("zero close behavior = %q, want %q", settings.CloseBehavior, CloseBehaviorHide)
	}
	// The temporary file is gone, so the directory holds only the settings
	// file and repeated saves do not accumulate debris.
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != SettingsFileName {
		names := make([]string, len(entries))
		for i, entry := range entries {
			names[i] = entry.Name()
		}
		t.Fatalf("settings directory holds %v, want only %q", names, SettingsFileName)
	}
}

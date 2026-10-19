package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// SettingsFileName is the file under Paths.Data that keeps the user's
// preferences. It is a small JSON document: preferences here are a few
// switches, not application state, so they do not share the record store or
// any database.
const SettingsFileName = "settings.json"

// CloseBehavior is what clicking the main window's close button does.
type CloseBehavior string

const (
	// CloseBehaviorHide hides the window to the tray/menu bar and keeps the
	// process and any scan session running. It is the default, matching the
	// platform convention for a tray application.
	CloseBehaviorHide CloseBehavior = "hide"
	// CloseBehaviorExit runs the explicit-exit wrap-up: stop input, finish
	// the session, save, release resources, then quit.
	CloseBehaviorExit CloseBehavior = "exit"
)

// valid reports whether b names a supported close behavior.
func (b CloseBehavior) valid() bool {
	return b == CloseBehaviorHide || b == CloseBehaviorExit
}

// normalize replaces the zero value with the default. A settings file
// written before the preference existed has no closeBehavior field, and
// must load as the documented default rather than an error.
func (b CloseBehavior) normalize() CloseBehavior {
	if b == "" {
		return CloseBehaviorHide
	}
	return b
}

// Settings are the persisted user preferences.
type Settings struct {
	// CloseBehavior is what closing the window does; empty loads as
	// CloseBehaviorHide.
	CloseBehavior CloseBehavior `json:"closeBehavior"`
}

// DefaultSettings is the behavior of a fresh installation.
func DefaultSettings() Settings {
	return Settings{CloseBehavior: CloseBehaviorHide}
}

// LoadSettings reads the preferences under dataRoot. A missing file is the
// fresh-install default without an error; a malformed file or an unknown
// close behavior returns the defaults with the error, so the caller can log
// the problem and run with documented behavior instead of failing to start.
func LoadSettings(dataRoot string) (Settings, error) {
	path := filepath.Join(dataRoot, SettingsFileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return DefaultSettings(), nil
	}
	if err != nil {
		return DefaultSettings(), fmt.Errorf("app: read settings %q: %w", path, err)
	}
	var settings Settings
	if err := json.Unmarshal(data, &settings); err != nil {
		return DefaultSettings(), fmt.Errorf("app: parse settings %q: %w", path, err)
	}
	settings.CloseBehavior = settings.CloseBehavior.normalize()
	if !settings.CloseBehavior.valid() {
		return DefaultSettings(), fmt.Errorf("app: settings %q: unknown close behavior %q", path, settings.CloseBehavior)
	}
	return settings, nil
}

// SaveSettings atomically replaces the preferences under dataRoot: a
// temporary file in the same directory is written and renamed over the
// old one, so a crash mid-write cannot leave a truncated settings file.
func SaveSettings(dataRoot string, settings Settings) error {
	settings.CloseBehavior = settings.CloseBehavior.normalize()
	if !settings.CloseBehavior.valid() {
		return fmt.Errorf("app: save settings: unknown close behavior %q", settings.CloseBehavior)
	}
	if err := os.MkdirAll(dataRoot, 0o700); err != nil {
		return fmt.Errorf("app: save settings: create data root: %w", err)
	}
	file, err := os.CreateTemp(dataRoot, ".settings-*.json")
	if err != nil {
		return fmt.Errorf("app: save settings: %w", err)
	}
	tempPath := file.Name()
	// Remove the temporary file on every failure path; after a successful
	// rename it no longer exists and Remove is a no-op error we discard.
	defer os.Remove(tempPath)

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(settings); err != nil {
		file.Close()
		return fmt.Errorf("app: save settings: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("app: save settings: %w", err)
	}
	if err := os.Rename(tempPath, filepath.Join(dataRoot, SettingsFileName)); err != nil {
		return fmt.Errorf("app: save settings: %w", err)
	}
	return nil
}

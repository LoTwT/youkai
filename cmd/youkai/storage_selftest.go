package main

import (
	"encoding/json"
	"errors"
	"image"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/game/zzz/collect"
	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/storageconfig"
)

func windowsStorageDescription(settings storageconfig.Config) (string, error) {
	model, err := collect.VerifiedStorageCardGlyphModel()
	if err != nil {
		return "", err
	}
	line, err := json.Marshal(struct {
		Source string                       `json:"source"`
		Model  collect.StorageGlyphIdentity `json:"model"`
		Config storageconfig.Config         `json:"config"`
		Trace  bool                         `json:"pageUnknownTrace"`
	}{"drive-storage", model, settings, collect.StoragePageTraceBuilt})
	return string(line), err
}

func verifyWindowsStorageSelfTest(getenv func(string) string) (string, error) {
	if err := collect.SelfCheckStorageCardGlyphModel(); err != nil {
		return "", err
	}
	settings := storageconfig.ForApp("windows", getenv)
	if settings != storageconfig.WindowsSettings() {
		return "", errors.New("the effective Windows drive storage configuration differs from the verified settings")
	}
	// Exercise the usual source and traversal factories without opening a game.
	source, _, err := scanSourceFrom(getenv, "windows", func() (platform.Port, error) { return nil, nil })
	if err != nil {
		return "", err
	}
	game, ok := source.(*app.GameScanSource)
	if !ok || game.TraversalForClient == nil {
		return "", errors.New("the Windows drive storage entry is unavailable")
	}
	th, err := game.TraversalForClient(image.Pt(1920, 1080))
	if err != nil {
		return "", err
	}
	inv, ok := th.(*collect.Inventory)
	if !ok || inv.Layout.Page != collect.PageDriveStorage {
		return "", errors.New("the executable does not select the Windows drive storage entry")
	}
	return windowsStorageDescription(settings)
}

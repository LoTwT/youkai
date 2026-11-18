package main

import _ "embed"

// These platform assets are exported from the selected original A artwork by
// go run ./tools/iconassets (from the repository root).
// appIconPNG is also the build-time icon configured in mygo.json.
//
//go:embed appicon.png
var appIconPNG []byte

// trayIconPNG keeps the dark tile, ivory eye and magenta sweep on Windows.
//
//go:embed trayicon.png
var trayIconPNG []byte

// trayTemplateIconPNG contains only the mark's alpha, without the tile, so
// macOS can tint the menu bar icon for its current appearance.
//
//go:embed trayicon-template.png
var trayTemplateIconPNG []byte

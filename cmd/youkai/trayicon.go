package main

import _ "embed"

// trayIconPNG is the 32x32 tray/menu bar icon. It is a template image on
// macOS (the system tints its alpha channel), and keeps its blue color on
// Windows; both a light and a dark taskbar show it.
//
//go:embed trayicon.png
var trayIconPNG []byte

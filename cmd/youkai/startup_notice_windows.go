//go:build windows

package main

import (
	"log"

	"golang.org/x/sys/windows"
)

// startupFailureDialogTitle names the failing launch in the message box's
// title bar; 08 §10 fixes the body text, not the title.
const startupFailureDialogTitle = "youkai 无法启动"

// showStartupFailureDialog shows a native Windows message box. It runs
// before App.Run and therefore cannot use MyGo's Dialog.Error, which
// panics before the framework is ready (08 §10). MessageBoxW needs no
// application storage, so it also works when the data root itself is
// unusable. MB_SETFOREGROUND keeps the box in front of whatever the app was
// launched from; a failure left behind other windows would reproduce the
// "double-click does nothing" report.
func showStartupFailureDialog(message string) {
	text, err := windows.UTF16PtrFromString(message)
	if err != nil {
		log.Printf("youkai: startup failure dialog: text: %v", err)
		return
	}
	title, err := windows.UTF16PtrFromString(startupFailureDialogTitle)
	if err != nil {
		log.Printf("youkai: startup failure dialog: title: %v", err)
		return
	}
	if _, err := windows.MessageBox(0, text, title,
		windows.MB_OK|windows.MB_ICONERROR|windows.MB_SETFOREGROUND); err != nil {
		log.Printf("youkai: startup failure dialog: %v", err)
	}
}

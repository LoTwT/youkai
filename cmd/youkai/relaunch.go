package main

import (
	"log"
	"os"
)

// relaunchHandoff releases the data-root occupancy for the installed
// update's restart.
//
// The wrap-up's teardown releases the occupancy when its cleanup
// succeeded; a failed teardown keeps it held for the writes the failed
// exit still allows, and the restart would then start the new process
// against a held root — it would quit with the misleading second-instance
// notice. MyGo's quit sequence orders the steps: saveWindowStates, then
// the OnQuit listeners, then relaunchNow, which starts the new process
// without waiting; the entry point's deferred release runs only after Run
// returned. OnQuit therefore runs after the framework's last write and
// before the new process exists.
//
// Only the installed update's restart releases here: a cancelled relaunch
// never reaches OnQuit, and the plain explicit exit keeps its release in
// the entry point's deferred Close at process exit, so the occupancy
// stays held while a failed exit lets the user keep viewing and saving
// results.
type relaunchHandoff struct {
	// updateInstalled reports a successful install. OnUpdateInstalled
	// raises it when it requests the relaunch that the exit interception
	// cancels.
	updateInstalled bool
	// settled reports that an explicit exit reached OnExitSettled.
	settled bool
	// detachLog stops the standard logger from writing into the diagnostic
	// log under the data root. On the restart path the occupancy is gone
	// after the release, while this process still logs until it exits —
	// the wrap-up's cached failure, the shutdown report — and those lines
	// must not land in a file the new process may already be appending to.
	detachLog func()
	// release releases the data-root occupancy. DataRootLock.Close is
	// idempotent, so the deferred release stays harmless, and tests
	// observe the handoff through it.
	release func() error
}

// detachDiagnosticLog redirects the standard logger to stderr. The
// diagnostic sink writes through on every line, so the lines logged
// before this call are already in the file and none are lost; the sink's
// handle stays open until the entry point's deferred Close, and Close is
// safe to call after the logger moved on.
func detachDiagnosticLog() {
	log.SetOutput(os.Stderr)
}

// settle records a settled explicit exit. OnExitSettled calls it on every
// exit; only the installed update's restart releases the occupancy.
func (h *relaunchHandoff) settle() {
	h.settled = true
}

// onQuit is the MyGo OnQuit listener.
func (h *relaunchHandoff) onQuit() {
	if !h.updateInstalled || !h.settled {
		return
	}
	// Detach the log before the release: the lines below and the ones the
	// entry point logs after Run returns go to stderr, so nothing this
	// process writes can reach the data root once the new process owns it.
	h.detachLog()
	if err := h.release(); err != nil {
		log.Printf("youkai: release the data-root occupancy for the restart: %v", err)
		return
	}
	log.Printf("youkai: data-root occupancy released for the restart")
}

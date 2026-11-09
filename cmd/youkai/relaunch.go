package main

import (
	"log"
	"os"

	"github.com/LoTwT/youkai/internal/app"
)

// relaunchHandoff hands the data root over to the installed update's
// restart.
//
// The wrap-up's teardown releases the occupancy when its cleanup
// succeeded; a failed teardown keeps it held for the writes the failed
// exit still allows, and the restart would then start the new process
// against a held root — it would quit with the misleading second-instance
// notice. MyGo's quit sequence orders the steps: saveWindowStates, then
// the OnQuit listeners, then relaunchNow, which starts the new process
// without waiting; the entry point's deferred release runs only after Run
// returned. OnQuit therefore runs after the framework's last write and
// before the new process exists: it detaches the diagnostic log and
// releases whatever the teardown left held (DataRootLock.Close is
// idempotent, so the release is a no-op after a successful teardown).
//
// Only the installed update's restart hands over here: a cancelled
// relaunch never reaches OnQuit. A plain explicit exit whose teardown
// succeeded already released the occupancy there; after a failed teardown
// it stays held until the entry point's deferred Close at process exit,
// while the failed exit lets the user keep viewing and saving results.
type relaunchHandoff struct {
	// updateInstalled reports a successful install. onUpdateInstalled
	// raises it when it requests the relaunch that the exit interception
	// cancels.
	updateInstalled bool
	// settled reports that an explicit exit reached onExitSettled.
	settled bool
	// detachLog stops the standard logger from writing into the diagnostic
	// log under the data root. On the restart path a new process may
	// already be appending to that file while this one still logs — a
	// failed wrap-up's cached report, a background update check — and those
	// lines must go to stderr only. The success path detaches as well: the
	// teardown already released the occupancy, but the background lines
	// must not interleave with the new process's.
	detachLog func()
	// release releases the data-root occupancy. DataRootLock.Close is
	// idempotent, so the deferred release stays harmless, and tests
	// observe the handoff through it.
	release func() error
}

// newRelaunchHandoff wires the entry point's handoff: the diagnostic log
// detaches to stderr and release frees the held data-root occupancy.
func newRelaunchHandoff(dataLock *app.DataRootLock) *relaunchHandoff {
	return &relaunchHandoff{
		detachLog: detachDiagnosticLog,
		release:   dataLock.Close,
	}
}

// detachDiagnosticLog redirects the standard logger to stderr. The
// diagnostic sink writes through on every line, so the lines logged
// before this call are already in the file and none are lost; the sink's
// handle stays open until the entry point's deferred Close, and Close is
// safe to call after the logger moved on.
func detachDiagnosticLog() {
	log.SetOutput(os.Stderr)
}

// settle records a settled explicit exit. onExitSettled calls it on every
// exit; only the installed update's restart releases the occupancy.
func (h *relaunchHandoff) settle() {
	h.settled = true
}

// onUpdateInstalled records the successful install and requests the
// restart. The close interception cancels this quit, so onExitSettled
// re-issues the restart once the wrap-up settled.
func (h *relaunchHandoff) onUpdateInstalled(relaunch func()) {
	h.updateInstalled = true
	log.Printf("youkai: update installed, restarting the application")
	relaunch()
}

// onExitSettled records the settled explicit exit and finishes it: the
// installed update's restart re-issues the relaunch, every other exit
// quits. A failed final save never reaches here.
func (h *relaunchHandoff) onExitSettled(relaunch, quit func()) {
	h.settle()
	if h.updateInstalled {
		log.Printf("youkai: exit settled, relaunching into the installed update")
		relaunch()
		return
	}
	quit()
}

// onQuit is the MyGo OnQuit listener.
func (h *relaunchHandoff) onQuit() {
	if !h.updateInstalled || !h.settled {
		return
	}
	// Detach the log before the release: from here the lines this process
	// still writes go to stderr, so they cannot land in the file the new
	// process may already be appending to. The crash output file under the
	// data root is the deliberate exception — it stays open because a
	// crash in this window is exactly the evidence it exists to keep
	// (debug.SetCrashOutput(nil, debug.CrashOptions{}) could close it, but
	// keeping the evidence is the choice).
	h.detachLog()
	if err := h.release(); err != nil {
		log.Printf("youkai: release the data-root occupancy for the restart: %v", err)
	}
}

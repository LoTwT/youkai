package main

import (
	"errors"
	"fmt"
	"log"

	"github.com/LoTwT/youkai/internal/app"
)

// dataRootFailureKind separates the two ways a launch can fail to take its
// data root. 08 §10 gives them different user-visible texts because the
// remedies differ: another youkai process (one whose single-instance
// identity differs, so the launch got past the single-instance lock) can be
// exited from the notification area, while an unreadable or uncreatable
// root needs a disk and permission check.
type dataRootFailureKind int

const (
	// dataRootUnavailable is the fallback: the root could not be created,
	// opened or locked for any reason other than another holder.
	dataRootUnavailable dataRootFailureKind = iota
	// dataRootHeldElsewhere is another process's exclusive occupancy. The
	// app package reports it as ErrLockHeld under any path alias.
	dataRootHeldElsewhere
)

func (k dataRootFailureKind) String() string {
	if k == dataRootHeldElsewhere {
		return "held by another youkai process"
	}
	return "unavailable"
}

// classifyDataRootFailure maps an AcquireDataRootLock error to the two
// cases 08 §10 distinguishes. ErrLockHeld is the package's contract for a
// foreign holder; everything else — invalid path, uncreatable directory,
// unopenable or unlockable lock file — belongs to the generic case.
func classifyDataRootFailure(err error) dataRootFailureKind {
	if errors.Is(err, app.ErrLockHeld) {
		return dataRootHeldElsewhere
	}
	return dataRootUnavailable
}

// dataRootFailureMessage returns the 08 §10 text for the classified
// failure. The unavailable case fills the "<原因>" placeholder with the
// wrapped error, which names the failed operation and path — the detail a
// user needs to check the disk and permissions.
func dataRootFailureMessage(kind dataRootFailureKind, err error) string {
	if kind == dataRootHeldElsewhere {
		return "另一个 youkai 正在使用数据目录（可能是另一个版本）。请先从通知区域退出它，再重新打开。"
	}
	return fmt.Sprintf("youkai 无法打开它的数据目录：%v。请检查磁盘与权限后重新打开。", err)
}

// reportDataRootFailure records the full error and shows the platform's
// startup notice; the caller exits afterwards. The diagnostic log file is
// opened only after a successful acquire, so this line reaches stderr
// (development builds) rather than the log file. On Windows the native
// message box is the channel an installed GUI build has; see
// showStartupFailureDialog.
func reportDataRootFailure(err error) {
	kind := classifyDataRootFailure(err)
	log.Printf("youkai: data root %s: %v", kind, err)
	showStartupFailureDialog(dataRootFailureMessage(kind, err))
}

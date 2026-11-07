package ui

import (
	"context"
	"fmt"
	"time"

	"github.com/LoTwT/youkai/internal/scan"
	"github.com/egoist/mygo/ui"
)

// UpdateSource checks for and installs application updates; the entry
// point wires MyGo's updater behind it (cmd/youkai), tests drive fakes.
// The seam keeps this package free of the updater's implementation, like
// Service keeps the application service abstract.
type UpdateSource interface {
	// Enabled reports whether this build can update itself: it was built
	// with an update feed and public key embedded, and it can write where
	// it is installed. Development runs and builds whose configuration
	// carried no updates answer false — a state to explain, not an error.
	Enabled() bool
	// Check returns the newest published version when it is newer than the
	// running one, or nil when this build is up to date. An error is shown
	// as it is; the caller never treats it as "disabled".
	Check(ctx context.Context) (UpdateHandle, error)
}

// UpdateHandle is one found update: its version and notes for display, and
// Install, which follows MyGo's download, signature verification and
// replacement flow (the running app is not affected until the restart).
type UpdateHandle interface {
	Version() string
	Notes() string
	Install(ctx context.Context, progress func(downloaded, total int64)) error
}

// checkTimeout bounds one update check. MyGo's HTTP client has no timeout
// of its own; without this bound a dead connection would leave the area at
// "checking" forever. Installs stay unbounded — archives are large and the
// progress callback keeps the app responsive.
const checkTimeout = time.Minute

// updateCheckInterval is how long a running application waits between
// background update checks (2026-10-17 决定：每 24 小时检查一次): the
// startup check and the area's button run outside this schedule.
const updateCheckInterval = 24 * time.Hour

// updateState is where the update flow stands. The model allows only the
// transitions the actions guard: check from anything but checking,
// installing or installed; install from available while nothing blocks it.
type updateState int

const (
	updateIdle updateState = iota
	updateChecking
	updateUpToDate
	updateAvailable
	updateInstalling
	updateInstalled
)

// StartUpdateChecks runs the update-check schedule: one check at once (the
// startup check the entry point used to trigger on its own), then one every
// updateCheckInterval for as long as the application runs (2026-10-17 决
// 定：启动后的首次检查保留，应用持续运行时每 24 小时再检查一次). Checks
// never activate the window — their results land through update, which only
// redraws — and builds without updates start nothing: the area explains
// that state instead.
//
// The waiting goroutine is deliberately outside the model's WaitGroup, whose
// waits are what Idle and the view tests settle: a days-long wait would
// never settle. It ticks for the rest of the process and is never started
// twice.
func (m *Model) StartUpdateChecks() {
	if m.updates == nil || !m.updatesEnabled {
		return
	}
	m.CheckUpdates()
	wait := m.cfg.UpdateCheckWait
	if wait == nil {
		wait = func(interval time.Duration) <-chan time.Time { return time.After(interval) }
	}
	go func() {
		for {
			<-wait(updateCheckInterval)
			// The tick lands off the UI thread and the model's state is
			// confined to it, so the check is scheduled like any other
			// background event instead of running here.
			m.update(func() { m.CheckUpdates() })
		}
	}()
}

// CheckUpdates checks for a newer version in the background — from the
// schedule's startup check, from a periodic tick and from the area's
// button. It never blocks the caller: the result lands through update,
// which only redraws the window.
func (m *Model) CheckUpdates() {
	if m.updates == nil || !m.updatesEnabled || m.updatesState == updateChecking ||
		m.updatesState == updateInstalling || m.updatesState == updateInstalled {
		return
	}
	m.updatesState = updateChecking
	m.updatesCheckErr = ""
	m.goAsync(func() {
		ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
		defer cancel()
		handle, err := m.updates.Check(ctx)
		m.update(func() {
			switch {
			case err != nil:
				m.updatesState = updateIdle
				m.updatesCheckErr = err.Error()
			case handle == nil:
				m.updatesState = updateUpToDate
			default:
				m.updatesState = updateAvailable
				m.updatesVersion = handle.Version()
				m.updatesNotes = handle.Notes()
				m.updatesHandle = handle
				// The rehearsal switch installs what a check found on its
				// own; the normal flow waits for the install button. The
				// guards inside InstallUpdate still apply, so a scan that
				// holds the slot or a pending save keeps the update waiting.
				if m.devAutoInstall {
					m.InstallUpdate()
				}
			}
		})
	})
}

// InstallUpdate installs the update a check found. A scan session that
// still holds the active slot — running, wrapping up, or waiting for a
// save retry — forbids the install (2026-10-17 决定：扫描进行中不安装，收尾
// 保存后再装); the view explains why next to the disabled button, and this
// guard repeats the rule so nothing reaches Install behind the UI's back.
// The click only schedules the download; the app keeps running until the
// entry point relaunches it after a successful install.
func (m *Model) InstallUpdate() {
	if m.updatesState != updateAvailable || m.updatesHandle == nil {
		return
	}
	if m.updateBlockedReason() != "" {
		return
	}
	m.updatesState = updateInstalling
	m.updatesInstallErr = ""
	m.updatesProgress = 0
	// Publish the install to the service before any download work: new
	// starts are refused from this moment, and the restart at the end is
	// the only way out. A failed or cancelled install must clear it again.
	m.cfg.Service.SetUpdateInstalling(true)
	handle := m.updatesHandle
	m.goAsync(func() {
		err := handle.Install(context.Background(), func(downloaded, total int64) {
			// Progress arrives on this goroutine; only the fraction moves,
			// through the queued updater like every background result.
			frac := 0.0
			if total > 0 {
				frac = float64(downloaded) / float64(total)
			}
			m.update(func() { m.updatesProgress = frac })
		})
		m.update(func() {
			if err != nil {
				// The app keeps running the current version; the update
				// stays found, so the button offers the install again. The
				// service must accept starts again too.
				m.cfg.Service.SetUpdateInstalling(false)
				m.updatesState = updateAvailable
				m.updatesInstallErr = err.Error()
				return
			}
			m.updatesState = updateInstalled
			if m.cfg.OnUpdateInstalled != nil {
				m.cfg.OnUpdateInstalled()
			}
		})
	})
}

// updateBlockedReason explains why the found update may not install now,
// empty when it may: an active scan session (its wrap-up, and a failed
// save awaiting retry, keep the slot busy), content still to be written,
// or the explicit exit in progress. A scan that starts while a download is
// already running is not blocked here — the restart runs the normal quit
// sequence, which stops and saves it like any other quit.
func (m *Model) updateBlockedReason() string {
	switch {
	case m.exitState == exitRunning:
		return "正在退出应用，不能安装更新。"
	case m.snap.SaveError != "":
		return "有保存失败的扫描结果待重试保存，先完成保存再安装更新。"
	case m.snap.Busy:
		if m.snap.Scan.Phase == scan.PhaseFinalizing {
			return "扫描正在收尾保存，等保存完成后再安装更新。"
		}
		return "扫描会话进行中，等其结束并保存完成后再安装更新。"
	case m.snap.SavePending:
		return "有扫描内容待写入，等保存完成后再安装更新。"
	default:
		return ""
	}
}

// updateStatusText is the update area's one-line state, in the same voice
// as the prepare area's notes.
func (m *Model) updateStatusText() string {
	switch m.updatesState {
	case updateIdle:
		return "尚未检查更新。"
	case updateChecking:
		return "正在检查更新……"
	case updateUpToDate:
		return "已是最新版本。"
	case updateAvailable:
		return fmt.Sprintf("发现新版本 %s。", m.updatesVersion)
	case updateInstalling:
		if m.updatesProgress > 0 {
			return fmt.Sprintf("正在下载并安装更新…… %.0f%%", m.updatesProgress*100)
		}
		return "正在下载并安装更新……"
	default: // updateInstalled
		return "更新已安装，正在重启应用……"
	}
}

// updateDotVisible reports the design's one update cue: a dot on the
// settings rail entry and on the in-page directory's 更新 entry while a
// newer version is waiting to be installed (08 §6). There is no page-top
// banner; the update section is the only place the news is written.
func (m *Model) updateDotVisible() bool {
	return m.updatesState == updateAvailable
}

// updateDot is the small accent circle both entries carry. Its label keeps
// the indicator discoverable in tests and assistive technology.
func updateDot(c *ui.Context, t *ui.Theme, tok *Tokens) {
	ui.Box(c).Size(t.Space(0.75), t.Space(0.75)).Radius(t.Space(0.375)).
		Background(tok.AccentFG).Shrink(0).Label("有新版本")
}

// updateArea is the settings update section: the running version, the state
// of the last check, the found version with its notes, the one primary
// action (安装并重启 while a version waits, 检查更新 otherwise) and every
// honest failure with its retry. Builds without updates say so once.
func (m *Model) updateArea(c *ui.Context, t *ui.Theme, tok *Tokens) {
	if m.appVersion != "" {
		ui.Text(c, "当前版本 "+m.appVersion).TextColor(tok.TextMuted)
	}
	if m.updates == nil || !m.updatesEnabled {
		ui.Text(c, "这个版本不支持自动更新").TextColor(tok.TextMuted)
		return
	}
	ui.Column(c).Gap(t.Space(1.5)).Children(func() {
		switch m.updatesState {
		case updateAvailable:
			m.updateAvailableArea(c, t, tok)
		case updateInstalling:
			ui.Text(c, m.updateStatusText()).TextColor(tok.TextMuted)
			ui.Progress(c, m.updatesProgress)
		case updateInstalled:
			ui.Text(c, m.updateStatusText()).TextColor(tok.TextMuted)
		default:
			ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
				ui.Text(c, m.updateStatusText()).TextColor(tok.TextMuted)
				ui.Box(c).Grow(1)
				if m.updatesState != updateChecking {
					label := "检查更新"
					if m.updatesCheckErr != "" {
						label = "重试检查"
					}
					if ui.Button(c, label).Clicked() {
						m.CheckUpdates()
					}
				}
			})
		}
		if m.updatesState == updateAvailable && m.updatesNotes != "" {
			ui.Text(c, m.updatesNotes).TextColor(tok.TextMuted).FontSize(t.FontSize * 0.9)
		}
		if m.updatesCheckErr != "" {
			ui.Text(c, "检查更新失败："+m.updatesCheckErr).TextColor(t.Danger)
		}
		if m.updatesInstallErr != "" {
			ui.Text(c, "安装更新失败："+m.updatesInstallErr+"。应用继续运行当前版本，可重试安装。").TextColor(t.Danger)
		}
	})
}

// updateAvailableArea is the found-version row. The install is the page's
// only primary action; a scan, wrap-up, failed save or exit disables it with
// its reason beside it, and the model's InstallUpdate repeats the guard.
func (m *Model) updateAvailableArea(c *ui.Context, t *ui.Theme, tok *Tokens) {
	blocked := m.updateBlockedReason()
	label := "安装并重启"
	if m.updatesInstallErr != "" {
		label = "重试安装"
	}
	ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
		ui.Text(c, m.updateStatusText()).TextColor(tok.TextMuted)
		ui.Box(c).Grow(1)
		button := ui.PrimaryButton(c, label).Disabled(blocked != "")
		if button.Clicked() {
			m.InstallUpdate()
		}
	})
	if blocked != "" {
		ui.Text(c, blocked).TextColor(t.TextMuted).FontSize(t.FontSize * 0.9)
	}
}

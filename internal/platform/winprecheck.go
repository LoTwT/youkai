package platform

import (
	"fmt"
	"image"
	"strings"
	"time"

	"github.com/LoTwT/youkai/internal/platform/windows"
)

// Windows local-client precheck. Stage 1 (tier, screen containment,
// DPI, integrity) runs before the user switches focus; stage 2
// (foreground, occlusion samples, frame-content fallback) runs once the
// target is foreground, inside the preparation wait. This file holds the
// stage-1/stage-2 policy checks; the native queries live in the
// internal/platform/windows base.

// displayFault applies the game adapter's display policy to one measured
// geometry: the exact client size must be a known tier and an opened one,
// and the client must lie inside the monitor's work area or be exactly the
// whole monitor (borderless full screen).
func displayFault(geo windows.ClientGeometry, policy *WindowDisplayPolicy) *Fault {
	if fault := resolutionFault(geo, policy); fault != nil {
		return fault
	}
	return positionFault(geo, policy)
}

// resolutionFault applies the tier half of displayFault. It is a separate
// function because the read-only preparation check reports the tier and the
// position as different checklist rows while reusing exactly this decision.
func resolutionFault(geo windows.ClientGeometry, policy *WindowDisplayPolicy) *Fault {
	if policy == nil {
		return NewFaultWithParams(CodeInvalidOperation,
			"the windows session has no display policy; the game adapter must supply the reference tiers and probes",
			nil)
	}
	client := image.Pt(geo.Client.Dx(), geo.Client.Dy())
	tier, known, open := policy.tier(client)
	current := fmt.Sprintf("%dx%d", client.X, client.Y)
	switch {
	case !known:
		return NewFaultWithParams(CodeClientTierUnsupported,
			fmt.Sprintf("client area %s is not a known 16:9 display tier", current),
			map[string]string{
				"current": current,
				"known":   tierNames(policy.KnownTiers),
				"allowed": tierNames(policy.OpenTiers),
			})
	case !open:
		return NewFaultWithParams(CodeClientTierNotOpened,
			fmt.Sprintf("client area %s is a known tier (%s) but no profile for it has field acceptance", current, tier.Name),
			map[string]string{
				"current": current,
				"tier":    tier.Name,
				"known":   tierNames(policy.KnownTiers),
				"allowed": tierNames(policy.OpenTiers),
			})
	}
	return nil
}

// positionFault applies the work-area half of displayFault: the client must
// be fully inside the monitor's work area or exactly the whole monitor
// (borderless full screen). A nil policy is allowed here because window
// containment does not depend on the tier contract; the fault then omits
// the allowed-tier hint.
func positionFault(geo windows.ClientGeometry, policy *WindowDisplayPolicy) *Fault {
	inWorkArea := geo.WorkArea.ContainsRect(geo.Client)
	fillsMonitor := geo.Client == geo.Monitor
	if inWorkArea || fillsMonitor {
		return nil
	}
	params := map[string]string{
		"client":   pixelRectString(geo.Client),
		"monitor":  pixelRectString(geo.Monitor),
		"workArea": pixelRectString(geo.WorkArea),
	}
	if policy != nil {
		params["allowed"] = tierNames(policy.OpenTiers)
	}
	return NewFaultWithParams(CodeWindowOutsideWorkArea,
		fmt.Sprintf("client area %s is neither inside the monitor work area %s nor exactly the monitor %s",
			pixelRectString(geo.Client), pixelRectString(geo.WorkArea), pixelRectString(geo.Monitor)),
		params)
}

// occlusionFault hit-tests every configured sample point of the client's
// open tier. Any point whose WindowFromPoint root is not the locked
// target refuses the precheck; the fault names the point and the window
// that won it, so a taskbar or overlay is identifiable from the record.
func occlusionFault(fg windows.ForegroundAPI, win windows.WindowAPI, procs windows.ProcessAPI, geo windows.ClientGeometry, target windows.HWND, probes WindowProbes) *Fault {
	if len(probes.Occlusion) == 0 {
		return nil
	}
	for _, probe := range probes.Occlusion {
		at := windows.PixelPoint{X: geo.Client.Min.X + probe.At.X, Y: geo.Client.Min.Y + probe.At.Y}
		if !geo.Client.Contains(at) {
			return NewFaultWithParams(CodeWindowOccluded,
				fmt.Sprintf("occlusion probe %q at %v is outside the client rect", probe.Name, at),
				map[string]string{
					"probe":  probe.Name,
					"point":  fmt.Sprintf("%d,%d", at.X, at.Y),
					"reason": "probeOutsideClient",
				})
		}
		hit := fg.HitWindow(at)
		root := windows.HWND(0)
		if hit != 0 {
			root = fg.RootOf(hit)
		}
		if hit != 0 && root == target {
			continue
		}
		cover := root
		if cover == 0 {
			cover = hit
		}
		className, processName := "", ""
		if cover != 0 {
			if c, err := win.ClassName(cover); err == nil {
				className = c
			}
			if pid, err := win.ThreadProcessID(cover); err == nil && pid != 0 {
				if path, _, err := procs.Query(pid); err == nil {
					processName = executableBase(path)
				}
			}
		}
		// The hit window is an arbitrary external program: its title is
		// user data and never reaches params, diagnostics or the record.
		return NewFaultWithParams(CodeWindowOccluded,
			fmt.Sprintf("occlusion probe %q at %v hit window %d (root %d, class %q, process %q), not the target %d",
				probe.Name, at, hit, root, className, processName, target),
			map[string]string{
				"probe":      probe.Name,
				"point":      fmt.Sprintf("%d,%d", at.X, at.Y),
				"hit":        fmt.Sprintf("%d", hit),
				"hitClass":   className,
				"hitProcess": processName,
				"target":     fmt.Sprintf("%d", target),
				"reason":     "covered",
			})
	}
	return nil
}

// executableBase returns the executable's base name from a Windows path
// without depending on the host OS's path separator.
func executableBase(path string) string {
	if i := strings.LastIndexAny(path, `\/`); i >= 0 {
		return path[i+1:]
	}
	return path
}

// exclusiveFullscreenFault applies the shell verdict while the target is
// foreground. An unknown verdict is not guessed: the caller logs it and
// leaves the frame-content fallback to catch the black capture shape.
func exclusiveFullscreenFault(state windows.FullscreenState, err error) *Fault {
	if err != nil || state == windows.FullscreenUnknown {
		return nil
	}
	if state != windows.FullscreenExclusive {
		return nil
	}
	return NewFaultWithParams(CodeExclusiveFullscreen,
		"the foreground target is an exclusive-mode Direct3D full-screen application",
		map[string]string{"state": string(state)})
}

// frameContentFault applies the frame fallback: from every client edge,
// content must appear within the calibrated all-black margin, and the
// optional content probe must contain content.
func frameContentFault(capture *windows.CapturedImage, probes WindowProbes) *Fault {
	if capture == nil {
		return NewFaultWithParams(CodeFrameContentBlank, "the precheck frame is missing", map[string]string{
			"reason": "missingFrame",
		})
	}
	content := windows.PixelRect{}
	if !probes.Content.Empty() {
		content = windows.PixelRect{
			Min: windows.PixelPoint{X: probes.Content.Min.X, Y: probes.Content.Min.Y},
			Max: windows.PixelPoint{X: probes.Content.Max.X, Y: probes.Content.Max.Y},
		}
	}
	fault := windows.CheckFrameContent(capture, content, windows.EdgeMargins{
		Top:    probes.EdgeMargins.Top,
		Right:  probes.EdgeMargins.Right,
		Bottom: probes.EdgeMargins.Bottom,
		Left:   probes.EdgeMargins.Left,
	})
	if fault == nil {
		return nil
	}
	params := map[string]string{}
	for k, v := range fault.Params {
		params[k] = v
	}
	params["client"] = fmt.Sprintf("%dx%d", capture.Width, capture.Height)
	return NewFaultWithParams(fault.Code, fault.Message, params).at(fault.At)
}

func tierNames(tiers []ClientTier) string {
	out := ""
	for i, tier := range tiers {
		if i > 0 {
			out += ", "
		}
		out += tier.Name
	}
	return out
}

func pixelRectString(r windows.PixelRect) string {
	return fmt.Sprintf("%d,%d %dx%d", r.Min.X, r.Min.Y, r.Dx(), r.Dy())
}

// at overrides the fault time with the platform check's time.
func (f *Fault) at(t time.Time) *Fault {
	if !t.IsZero() {
		f.At = t
	}
	return f
}

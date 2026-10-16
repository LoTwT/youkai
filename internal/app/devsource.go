package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
)

// devPace is the default delay between two admitted observations, slow
// enough to watch a development run progress and fast enough not to be
// the point of the run. Tests and tighter development loops set Pace
// directly.
const devPace = 250 * time.Millisecond

// DevScanSource is the development and testing stand-in for the game
// adapters of M2: it replays the fixed text-line crops a directory holds
// as observations, one item per crop group, at a controlled pace. It
// exists because M1 has no real game controller; it is enabled only
// through the explicit environment switch of the entry point
// (YOUKAI_DEV_SOURCE), and it is deliberately not a user-facing
// "import arbitrary images" feature — the only directories it ever reads
// are the ones a developer points it at.
//
// The directory layout is the recognition test fixture: lines.tsv rows
// of file, source-screenshot sha256, crop rectangle and expected text
// (the columns beyond the first are that fixture's own provenance and
// are ignored here), with the listed PNG files beside it. File names
// group the lines: disc1_name.png and disc1_level.png belong to one
// observation, "disc1"; the rows' order within a group follows the file.
type DevScanSource struct {
	// Pace is the delay between two admitted observations; zero admits
	// as fast as recognition keeps up.
	Pace time.Duration

	dir     string
	groups  []string
	filesOf map[string][]string
}

// LoadDevScanSource reads the fixture directory dir: lines.tsv and the
// crop files it lists. Every listed file must exist; a missing or
// malformed fixture fails here, at wiring time, rather than mid-scan.
func LoadDevScanSource(dir string) (*DevScanSource, error) {
	if dir == "" {
		return nil, errors.New("app: dev scan source: directory is empty")
	}
	rows, err := readDevLines(dir)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("app: dev scan source: %s lists no lines", filepath.Join(dir, devLinesFile))
	}
	src := &DevScanSource{
		Pace:    devPace,
		dir:     dir,
		filesOf: make(map[string][]string),
	}
	for _, row := range rows {
		name := strings.TrimSuffix(row.file, ".png")
		obs, role, ok := strings.Cut(name, "_")
		if !ok || obs == "" || role == "" {
			return nil, fmt.Errorf("app: dev scan source: %s: file name %q is not <observation>_<role>.png", devLinesFile, row.file)
		}
		if _, seen := src.filesOf[obs]; !seen {
			src.groups = append(src.groups, obs)
		}
		src.filesOf[obs] = append(src.filesOf[obs], row.file)
	}
	for _, group := range src.groups {
		for _, file := range src.filesOf[group] {
			path := filepath.Join(dir, file)
			if _, err := os.Stat(path); err != nil {
				return nil, fmt.Errorf("app: dev scan source: crop %s: %w", path, err)
			}
		}
	}
	return src, nil
}

// devLinesFile names the fixture's manifest inside its directory.
const devLinesFile = "lines.tsv"

// devRow is one parsed row of the manifest: the crop file name is all
// this source consumes.
type devRow struct {
	file string
}

// readDevLines parses lines.tsv. The seven-column shape (file, source
// sha256, x0, y0, x1, y1, expected text) is the recognition fixture's
// documented format; rows are tab-separated, blank rows and # comments
// are skipped, and a stray carriage return from a Windows edit is
// tolerated.
func readDevLines(dir string) ([]devRow, error) {
	raw, err := os.ReadFile(filepath.Join(dir, devLinesFile))
	if err != nil {
		return nil, fmt.Errorf("app: dev scan source: %w", err)
	}
	var rows []devRow
	for i, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 7 {
			return nil, fmt.Errorf("app: dev scan source: %s row %d has %d fields, want 7", devLinesFile, i+1, len(fields))
		}
		if !strings.HasSuffix(fields[0], ".png") {
			return nil, fmt.Errorf("app: dev scan source: %s row %d: %q is not a PNG file name", devLinesFile, i+1, fields[0])
		}
		rows = append(rows, devRow{file: fields[0]})
	}
	return rows, nil
}

// NewScan implements ScanSource. Every start gets a fresh controller
// over the same fixed crops; the descriptor identifies the scan as the
// development source of this directory.
func (d *DevScanSource) NewScan() (ScanSpec, error) {
	scope, err := json.Marshal(struct {
		Kind      string `json:"kind"`
		Directory string `json:"directory"`
		Items     int    `json:"items"`
	}{Kind: "devDirectory", Directory: d.dir, Items: len(d.groups)})
	if err != nil {
		return ScanSpec{}, fmt.Errorf("app: dev scan source: marshal scope: %w", err)
	}
	return ScanSpec{
		// The fixture is drive-disc detail crops taken from the Windows
		// full client (see the recognition testdata README); the
		// descriptor says so instead of inventing a scan the source
		// cannot back up.
		Game:       "zzz",
		Entity:     "driveDisc",
		Client:     record.ClientWindowsLocal,
		Locale:     "zh-CN",
		Scope:      scope,
		Controller: &devScanController{src: d},
	}, nil
}

// devScanController drives one session over the development fixture: it
// decodes every crop during Prepare — a broken image is a precheck
// failure, not a mid-scan fault — and admits one observation per group
// during Collect, pacing itself between groups.
type devScanController struct {
	src   *DevScanSource
	crops map[string][]LineCrop
}

// Prepare implements scan.Controller: decode all crops now, so the
// collection loop only ever hands over known-good images.
func (c *devScanController) Prepare(ctx context.Context, h *scan.ControllerHandle) error {
	c.crops = make(map[string][]LineCrop, len(c.src.groups))
	for _, group := range c.src.groups {
		lines := make([]LineCrop, 0, len(c.src.filesOf[group]))
		for _, file := range c.src.filesOf[group] {
			if err := ctx.Err(); err != nil {
				return err
			}
			crop, err := loadDevCrop(c.src.dir, file)
			if err != nil {
				return err
			}
			name := strings.TrimSuffix(file, ".png")
			_, role, _ := strings.Cut(name, "_")
			lines = append(lines, LineCrop{Role: role, Source: file, Image: crop})
		}
		c.crops[group] = lines
	}
	return nil
}

// loadDevCrop decodes one fixture crop.
func loadDevCrop(dir, file string) (image.Image, error) {
	f, err := os.Open(filepath.Join(dir, file))
	if err != nil {
		return nil, fmt.Errorf("app: dev scan source: open crop: %w", err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("app: dev scan source: decode %s: %w", file, err)
	}
	return img, nil
}

// Collect implements scan.Controller: admit the groups one by one, in
// manifest order, waiting Pace between them. It honours the input
// permission before every step and returns with the context error once a
// stop closed it, leaving the rest of the fixture unadmitted.
func (c *devScanController) Collect(ctx context.Context, h *scan.ControllerHandle) error {
	for _, group := range c.src.groups {
		if !h.InputPermitted() {
			return ctx.Err()
		}
		slot := h.Acquire(ctx)
		if slot == nil {
			return ctx.Err()
		}
		lines := c.crops[group]
		if _, err := slot.Admit(Observation{Lines: lines}); err != nil {
			slot.Discard()
			return ctx.Err()
		}
		if c.src.Pace <= 0 {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(c.src.Pace):
		}
	}
	return nil
}

package collect

import (
	"fmt"
	"image"
	"image/color"
	"time"
)

// The 2026-11-22 drive-storage page (游戏内「驱动仓库」, Windows local
// client 3.3.0) is a second, independent inventory entry. It reuses the
// calibrated geometry, page/scroll evidence and admission rules, but its
// list mixes all six disc slots in one 9×4 grid and has no wheel: each
// card's top-left badge carries its slot, and the detail pane repeats the
// slot in its title suffix and in the independent badge under the title.
// The whole storage is one continuous top-to-bottom walk; per-slot
// ledgers are derived from the card badges only after the single pass has
// complete top/bottom and all-card evidence (T5 §3/§5).
//
// The layout below is measured from Jane's 2026-11-22 1920×1080 native
// crops. It is not a scale of the old warehouse layout: the grid, detail
// pane and funnel are different. The card/detail slot glyph sets are binary
// masks derived from the private handoff; the public repository only
// carries the masks, not the source crops.

// glyphStencil is one small calibrated UI glyph: a binary mask over a
// region and the grayscale threshold that separates its ink. A match is
// allowed to search a few pixels inside the declared window because the
// list can shift by 1–2 px between scroll states; the source window is
// part of Layout.ReadRegions, so the matcher still reads only declared
// pixels.
type glyphStencil struct {
	minBrightness int
	maxMismatch   int
	margin        int
	rows          []string
}

func (g glyphStencil) size() image.Point {
	if len(g.rows) == 0 {
		return image.Point{}
	}
	return image.Pt(len(g.rows[0]), len(g.rows))
}

func (g glyphStencil) valid() bool {
	if g.minBrightness < 0 || g.maxMismatch < 0 || g.margin < 0 || len(g.rows) == 0 {
		return false
	}
	w := len(g.rows[0])
	if w == 0 {
		return false
	}
	for _, row := range g.rows {
		if len(row) != w {
			return false
		}
	}
	return true
}

// bestMismatch finds the least number of mask mismatches for a template
// placed anywhere inside window. It returns false when the template cannot
// fit or the image is unusable.
func (g glyphStencil) bestMismatch(img *image.RGBA, window image.Rectangle) (int, bool) {
	if img == nil || window.Empty() || !g.valid() {
		return 0, false
	}
	tw, th := g.size().X, g.size().Y
	if window.Dx() < tw || window.Dy() < th || !window.In(img.Bounds()) {
		return 0, false
	}
	best := -1
	for oy := 0; oy <= window.Dy()-th; oy++ {
		for ox := 0; ox <= window.Dx()-tw; ox++ {
			m := g.mismatchAt(img, window.Min.Add(image.Pt(ox, oy)))
			if best < 0 || m < best {
				best = m
			}
		}
	}
	return best, best >= 0
}

func (g glyphStencil) mismatchAt(img *image.RGBA, at image.Point) int {
	mismatches := 0
	for y, row := range g.rows {
		for x := range len(row) {
			i := img.PixOffset(at.X+x, at.Y+y)
			bright := (int(img.Pix[i])+int(img.Pix[i+1])+int(img.Pix[i+2]))/3 >= g.minBrightness
			if bright != (row[x] == '#') {
				mismatches++
			}
		}
	}
	return mismatches
}

// matchesExact compares the stencil at window's origin and requires the
// window to be exactly the stencil's size. It is used for fixed header and
// control glyphs that cannot move with the list.
func (g glyphStencil) matchesExact(img *image.RGBA, window image.Rectangle) bool {
	if img == nil || window.Empty() || window.Size() != g.size() || !window.In(img.Bounds()) || !g.valid() {
		return false
	}
	return g.mismatchAt(img, window.Min) <= g.maxMismatch
}

// glyphMask is one binary slot-glyph template. Unlike the fixed header and
// control stencils above, a list badge can shift by a pixel or two between
// scroll states, so the matcher searches the mask inside the layout's
// declared window. Only the shape is stored; the source crops stay private.
type glyphMask struct {
	rows []string
}

func (g glyphMask) size() image.Point {
	if len(g.rows) == 0 {
		return image.Point{}
	}
	return image.Pt(len(g.rows[0]), len(g.rows))
}

func (g glyphMask) valid() bool {
	if len(g.rows) == 0 {
		return false
	}
	w := len(g.rows[0])
	if w == 0 {
		return false
	}
	for _, row := range g.rows {
		if len(row) != w {
			return false
		}
	}
	ink := 0
	for _, row := range g.rows {
		for x := range len(row) {
			if row[x] == '#' {
				ink++
			}
		}
	}
	return ink > 0
}

// bestF1 finds the best foreground F1 of a mask over window. The foreground
// is the bright ink of the badge, not the whole card: comparing the mask's
// ink against the window's ink makes the score insensitive to the card art
// behind the badge while still counting bright card art that falls inside
// the mask as a false positive. A raw mismatch count was not usable here:
// the selected-card templates were clipped and the unselected S badge for
// slot 5 exceeds the old exact-mask bound by more than 30 pixels.
func (g glyphMask) bestF1(img *image.RGBA, window image.Rectangle, minBrightness int) (float64, bool) {
	if img == nil || window.Empty() || !g.valid() || minBrightness < 0 {
		return 0, false
	}
	tw, th := g.size().X, g.size().Y
	if window.Dx() < tw || window.Dy() < th || !window.In(img.Bounds()) {
		return 0, false
	}
	best := -1.0
	for oy := 0; oy <= window.Dy()-th; oy++ {
		for ox := 0; ox <= window.Dx()-tw; ox++ {
			score := g.f1At(img, window.Min.Add(image.Pt(ox, oy)), minBrightness)
			if score > best {
				best = score
			}
		}
	}
	return best, best >= 0
}

func (g glyphMask) f1At(img *image.RGBA, at image.Point, minBrightness int) float64 {
	tp, fp, fn := 0, 0, 0
	for y, row := range g.rows {
		for x := range len(row) {
			i := img.PixOffset(at.X+x, at.Y+y)
			bright := (int(img.Pix[i])+int(img.Pix[i+1])+int(img.Pix[i+2]))/3 >= minBrightness
			if row[x] == '#' {
				if bright {
					tp++
				} else {
					fn++
				}
			} else if bright {
				fp++
			}
		}
	}
	if tp+fn == 0 {
		return -1
	}
	return 2 * float64(tp) / float64(2*tp+fp+fn)
}

// glyphSet is one slot's calibrated shape evidence. Several masks cover the
// same digit at different grid columns; the best foreground F1 wins. The
// acceptance floor and the runner-up margin are both required, so an
// unreadable or ambiguous badge stays unknown instead of being guessed.
type glyphSet struct {
	minBrightness int
	minScore      float64
	minMargin     float64
	masks         []glyphMask
}

func (g glyphSet) valid() bool {
	if g.minBrightness < 0 || g.minScore <= 0 || g.minScore > 1 || g.minMargin < 0 || len(g.masks) == 0 {
		return false
	}
	for _, mask := range g.masks {
		if !mask.valid() {
			return false
		}
	}
	return true
}

func (g glyphSet) bestScore(img *image.RGBA, window image.Rectangle) (float64, bool) {
	best := -1.0
	for _, mask := range g.masks {
		score, ok := mask.bestF1(img, window, g.minBrightness)
		if ok && score > best {
			best = score
		}
	}
	return best, best >= 0
}

// bestMatch is bestScore plus the winning mask's top-left point. It is used
// when a conflicting runner-up must be re-scored at the winning set's offset
// instead of being allowed to choose a different offset.
func (g glyphSet) bestMatch(img *image.RGBA, window image.Rectangle) (float64, image.Point, bool) {
	best := -1.0
	var at image.Point
	for _, mask := range g.masks {
		tw, th := mask.size().X, mask.size().Y
		if tw == 0 || th == 0 || window.Dx() < tw || window.Dy() < th {
			continue
		}
		for oy := 0; oy <= window.Dy()-th; oy++ {
			for ox := 0; ox <= window.Dx()-tw; ox++ {
				point := window.Min.Add(image.Pt(ox, oy))
				score := mask.f1At(img, point, g.minBrightness)
				if score > best {
					best, at = score, point
				}
			}
		}
	}
	return best, at, best >= 0
}

// bestOffsets returns every top-left point where g reaches its best score.
// Equal-quality placements of one glyph are not interchangeable when a
// conflict is rechecked: each can expose a different runner-up margin.
func (g glyphSet) bestOffsets(img *image.RGBA, window image.Rectangle) ([]image.Point, bool) {
	best := -1.0
	seen := map[image.Point]bool{}
	var offsets []image.Point
	for _, mask := range g.masks {
		tw, th := mask.size().X, mask.size().Y
		if tw == 0 || th == 0 || window.Dx() < tw || window.Dy() < th {
			continue
		}
		for oy := 0; oy <= window.Dy()-th; oy++ {
			for ox := 0; ox <= window.Dx()-tw; ox++ {
				point := window.Min.Add(image.Pt(ox, oy))
				score := mask.f1At(img, point, g.minBrightness)
				switch {
				case score > best:
					best = score
					offsets = offsets[:0]
					for k := range seen {
						delete(seen, k)
					}
					seen[point] = true
					offsets = append(offsets, point)
				case score == best && !seen[point]:
					seen[point] = true
					offsets = append(offsets, point)
				}
			}
		}
	}
	return offsets, best >= 0
}

// bestAt scores the best mask of g at one fixed top-left point.
func (g glyphSet) bestAt(img *image.RGBA, at image.Point) (float64, bool) {
	if img == nil || !at.In(img.Bounds()) {
		return 0, false
	}
	best := -1.0
	for _, mask := range g.masks {
		tw, th := mask.size().X, mask.size().Y
		if tw == 0 || th == 0 || at.X+tw > img.Bounds().Max.X || at.Y+th > img.Bounds().Max.Y {
			continue
		}
		score := mask.f1At(img, at, g.minBrightness)
		if score > best {
			best = score
		}
	}
	return best, best >= 0
}

// PositionVerdict is one card's or detail pane's slot evidence. Position 0
// is unreadable and Conflict means two glyph sets were close enough that the
// slot cannot be named; neither is guessed.
type PositionVerdict struct {
	Position int
	Conflict bool
}

func (v PositionVerdict) known() bool { return v.Position >= 1 && v.Position <= 6 && !v.Conflict }

// storageLayout constants measured on the 2026-11-22 handoff. The card
// origins and row pitch come from the manifest crops; the 9×4 grid is
// row-major.
const (
	storageCardWidth  = 139
	storageCardHeight = 163
	storageRowPitch   = 174
	storageColumns    = 9
	storageRows       = 4
)

var storageCardX = [storageColumns]int{95, 232, 367, 503, 639, 775, 910, 1046, 1181}
var storageCardY = [storageRows]int{197, 372, 546, 720}

// StorageLayout returns the drive-storage page's calibrated anchors on the
// native 1920×1080 Windows client. The 9×4 cards, detail pane, filter
// panel, funnel and scrollbar were measured from the 2026-11-22 private
// handoff; the sort template is the already-calibrated Windows "稀有度"
// crop, which matched the storage panel's sort region pixel-for-pixel.
func StorageLayout() Layout {
	slots := make([]image.Rectangle, 0, storageRows*storageColumns)
	for _, y := range storageCardY {
		for _, x := range storageCardX {
			slots = append(slots, image.Rect(x, y, x+storageCardWidth, y+storageCardHeight))
		}
	}
	return Layout{
		Page:    PageDriveStorage,
		Name:    "winlocal-storage-1920x1080",
		Image:   image.Pt(ReferenceCanvasWidth, ReferenceCanvasHeight),
		Content: image.Rect(0, 0, ReferenceCanvasWidth, ReferenceCanvasHeight),

		Rows:    storageRows,
		Columns: storageColumns,
		Slots:   slots,
		Cell: CellProbes{
			// The selected card's yellow frame is inset at x 100–107 and
			// runs through the card's middle; the probe is the same
			// left-edge band the old warehouse calibration used.
			Border: image.Rect(-63, -35, -58, 35),
			// The rarity bar spans x 112–220, y 317–329 for the first row
			// (row pitch 174); the probe reads its middle.
			Bar: image.Rect(-35, 39, 35, 51),
			// The management lock sits at the card's bottom-right (about
			// 110,115 in the 139×163 crop), inside the S badge's right
			// neighbor. The 26×26 probe is centered on the lock.
			Management: image.Rect(28, 21, 54, 47),
			// The equipped-agent avatar is the pink circle at the card's
			// top-right; the crop is evidence even though the presence
			// classifier stays unknown for this entry (equipped is optional).
			Avatar: image.Rect(38, -74, 64, -48),
		},
		// The card badge search window is x 10–44, y 22–48 relative to the
		// 139×163 card; the 26×18 masks sit at x 14–40, y 26–44. The extra
		// margin is the observed one-row scroll's sub-pixel shift.
		CardPositionBadge: image.Rect(-59, -59, -25, -33),
		CardPositions: [6]glyphSet{
			storageCardPosition1, storageCardPosition2, storageCardPosition3,
			storageCardPosition4, storageCardPosition5, storageCardPosition6,
		},
		// The card pitch is 174 px (bar rows 317, 491, 666, 840).
		RowPitch: storageRowPitch,
		// The list viewport excludes the detail pane and the scrollbar; the
		// scroll matcher compares only the 9-column card area.
		ListViewport: image.Rect(70, 184, 1320, 956),
		// The thumb is a 1-px column at x=1365; the top sample is y 238–261,
		// the bottom sample y 842–865. The track includes the end caps, which
		// are shorter than the thumb and therefore never win the longest-run
		// selection.
		ScrollTrack: image.Rect(1365, 224, 1366, 880),
		// Wheel input is used for this page; the drag endpoints remain
		// defined so the shared layout validation and alternate profiles
		// stay structurally complete. They are not dispatched by the
		// drive-storage traversal.
		ScrollFrom:     image.Pt(680, 700),
		ScrollTo:       image.Pt(680, 526),
		ScrollDuration: 1200 * time.Millisecond,
		TopFrom:        image.Pt(680, 300),
		TopTo:          image.Pt(680, 900),
		TopDuration:    150 * time.Millisecond,

		// The funnel is the bottom-left control at (148,929); its blue fill
		// is the active tier measured in the handoff.
		FilterButton: image.Pt(148, 929),
		Funnel: ColorProbe{
			At: image.Rect(133, 926, 138, 931),
			Colors: map[string]color.RGBA{
				"clear":  {R: 16, G: 16, B: 16, A: 255},
				"active": {R: 13, G: 142, B: 254, A: 255},
			},
			Tol: 20,
		},
		// The panel covers the right-side tabs. In the closed page those
		// two fixed tab icons are white; the open panel paints the same
		// coordinates nearly black. Two independent probes prevent a
		// single changed icon from claiming the panel page.
		PanelOpen: []ColorProbe{
			{
				At: image.Rect(1752, 185, 1755, 188),
				Colors: map[string]color.RGBA{
					"open":   {R: 0, G: 0, B: 0, A: 255},
					"closed": {R: 255, G: 255, B: 255, A: 255},
				},
				Tol: 30,
			},
			{
				At: image.Rect(1452, 189, 1455, 192),
				Colors: map[string]color.RGBA{
					"open":   {R: 17, G: 17, B: 17, A: 255},
					"closed": {R: 255, G: 255, B: 255, A: 255},
				},
				Tol: 30,
			},
		},
		PanelReset: image.Pt(1431, 1028),
		// The funnel toggles the panel on this page; closing it leaves the
		// S selection active (the handoff's 16-s-top frame).
		PanelClose: image.Pt(148, 929),
		PanelS:     image.Pt(1425, 441),
		panelRange: storagePanelRangeCalibration(),
		// The existing Windows native sort crop is byte-identical in the
		// storage panel's (1285,124)-(1467,201) region.
		SortLabel:    image.Rect(1285, 124, 1467, 201),
		SortTemplate: windowsLocalSortLabelTemplate,

		// Detail pane: the 450×620 crop starts at (1401,255). The title,
		// independent slot badge, S badge, four substat bands and bottom
		// lock button are measured from the handoff.
		DetailLines: []LineRegion{
			{Role: LineName, Rect: image.Rect(1405, 272, 1835, 305)},
			// Stop at x=1645: x=1700 is the measured start of the
			// equipped-slot "EMPTY" label, which must not enter level OCR.
			{Role: LineLevel, Rect: image.Rect(1448, 405, 1645, 435)},
			{Role: LineMain, Rect: image.Rect(1405, 495, 1835, 525)},
			{Role: LineSub1, Rect: image.Rect(1405, 578, 1835, 600)},
			{Role: LineSub2, Rect: image.Rect(1405, 628, 1835, 650)},
			{Role: LineSub3, Rect: image.Rect(1405, 678, 1835, 700)},
			{Role: LineSub4, Rect: image.Rect(1405, 728, 1835, 750)},
		},
		QualityBadge:  image.Rect(1414, 405, 1448, 435),
		DetailAvatar:  image.Rect(1450, 329, 1482, 361),
		TopManagement: image.Rect(1509, 820, 1537, 855),
		Substat4Probe: image.Rect(1731, 729, 1831, 750),
		// The detail's independent badge is only 16 px tall; its search
		// window excludes the equipped-agent avatar to its right.
		DetailPositionBadge: image.Rect(1419, 325, 1447, 345),
		DetailPositions: [6]glyphSet{
			storageDetailPosition1, storageDetailPosition2, storageDetailPosition3,
			storageDetailPosition4, storageDetailPosition5, storageDetailPosition6,
		},
		PageTitle:        image.Rect(78, 128, 159, 151),
		PageTitleStencil: &storagePageTitle,
	}
}

// storagePanelRangeCalibration is the drive-storage panel's reset/S
// evidence. The S glyph moved from the old page's (1412,593) to
// (1419,434); the fill and neighbouring A/B fills are measured on the
// same four panel states. Reset is the bottom-left orange control.
func storagePanelRangeCalibration() *panelRangeProbes {
	neutral := map[string]color.RGBA{"clear": {R: 11, G: 11, B: 11, A: 255}}
	return &panelRangeProbes{
		funnel: ColorProbe{
			At: image.Rect(133, 926, 138, 931), Tol: 20,
			Colors: map[string]color.RGBA{
				"clear":  {R: 12, G: 12, B: 12, A: 255},
				"active": {R: 13, G: 92, B: 167, A: 255},
			},
		},
		sFill: ColorProbe{
			At: image.Rect(1398, 438, 1402, 442), Tol: 10,
			Colors: map[string]color.RGBA{
				"clear":  {R: 28, G: 28, B: 28, A: 255},
				"active": {R: 0, G: 139, B: 255, A: 255},
			},
		},
		otherFills: [2]ColorProbe{
			{At: image.Rect(1598, 438, 1602, 442), Colors: neutral, Tol: 8},
			{At: image.Rect(1398, 498, 1402, 502), Colors: neutral, Tol: 8},
		},
		sLabel:    image.Rect(1412, 430, 1438, 452),
		sLabelSet: &storagePanelSLabelSet,
	}
}

// StorageCalibration returns the drive-storage page's measured threshold
// set. It reuses the normalized scroll/detail thresholds and replaces the
// probe-count and scrollbar-end values that depend on this page's regions.
// The 3-row and 4-row substat tiers include the 2026-11-23 field control: a
// short fourth value ("9") measured 174 ink pixels in the value column, a
// three-row set-effect row measured 0, and a normal four-row row measured
// 572. Unmarked/discarded cards, pink equipped avatars and long-list
// scrollbar margins are not covered by the handoff; those thresholds remain
// conservative or unverified and are called out in T5.
func StorageCalibration() Calibration {
	c := ReferenceCalibration()
	c.BarEmptyMax = 40
	c.BarOccupiedMin = 120
	c.SelectionHitMin = 100
	c.SelectionClearMax = 10
	c.DetailSameMax = 2.0
	c.DetailDifferentMin = 4.0
	c.InkThreshold = 60
	// Keep the 3/4 boundary below the 174-pixel short fourth value while
	// leaving 61–99 as unknown so a partial/animation frame still stops.
	c.Substat4InkMin = 100
	c.Substat3InkMax = 60
	c.DetailPopulatedInk = 2000
	c.QualityGoldMin = 250
	c.SortLabelMADMax = 5.0

	c.ManagementLockRingMin = 50
	c.ManagementLockCoreMax = 45
	c.ManagementDiscardRedMin = 40
	c.ManagementClearRingMax = 30
	c.ManagementClearRedMax = 10

	c.TopLockedDimMin = 80
	c.TopUnlockedBrightMin = 50
	c.TopUnlockedDimMax = 60

	c.ScrollStepX = 6
	c.ScrollStepY = 4
	c.ScrollSearchRadius = 720
	c.ScrollMinSamples = 5000
	c.ScrollStationaryMax = 4.0
	c.ScrollDisplacedMax = 10.0
	c.ScrollDisplacedMargin = 3.0
	c.ScrollShiftMargin = 2.0
	c.ScrollRowTolerance = 3
	c.ScrollMaxRows = storageRows - 1
	c.ScrollLostMin = 10.0
	c.ScrollbarBrightMin = 110
	c.ScrollbarTopStartMax = 240
	c.ScrollbarBottomEndMin = 863
	c.ScrollbarMoveMin = 2
	c.ScrollbarTravelTolerance = 0.20
	return c
}

// WindowsStorageProfile is the Windows local client's drive-storage
// profile. Only the native 1920×1080 tier is opened for this entry; the
// page has no scaled calibration, so another tier is refused rather than
// stretched.
type WindowsStorageProfile struct {
	Name         string
	Tier         DisplayTier
	Layout       Layout
	Calibration  Calibration
	ScrollTiming ScrollTiming
}

// WindowsStorageProfileFor builds the drive-storage profile for one
// measured client area. The caller passes the size the platform precheck
// accepted and froze.
func WindowsStorageProfileFor(client image.Point) (WindowsStorageProfile, error) {
	tier, err := openClientTier(client)
	if err != nil {
		return WindowsStorageProfile{}, err
	}
	if tier.Name != "1920x1080" {
		return WindowsStorageProfile{}, fmt.Errorf(
			"collect: windows storage: tier %s is not calibrated for the drive-storage page; only 1920x1080 is open", tier.Name)
	}
	return WindowsStorageProfile{
		Name:         "windowsStorage-" + tier.Name,
		Tier:         tier,
		Layout:       StorageLayout(),
		Calibration:  StorageCalibration(),
		ScrollTiming: MacCloudScrollTiming(),
	}, nil
}

// Inventory builds one drive-storage traversal. The old warehouse's
// legacy pixel matcher is not a safe ablation for the 9×4 repeated-card
// grid: its bare MAD Stationary can accept a moving scrollbar at the
// bottom as stillness. Drive storage therefore always uses the evidence
// matcher regardless of the legacy switch; the switch remains meaningful
// only for the old warehouse entry.
func (p WindowsStorageProfile) Inventory(legacy bool) *Inventory {
	_ = legacy
	pixel := NewPixelMatcher(p.Layout, p.Calibration)
	timing := p.ScrollTiming
	return &Inventory{
		Layout:                 p.Layout,
		Matcher:                NewScrollEvidenceMatcher(pixel),
		RequirePanelRangeProof: true,
		ScrollTiming:           &timing,
	}
}

func (p WindowsStorageProfile) Diagnostic() string {
	return fmt.Sprintf("profile=%s tier=%s layout=%s panel_range_required=true timing=%s",
		p.Name, p.Tier.Name, p.Layout.Name, WindowsLocalTimingProvenance)
}

func (inv *Inventory) storagePage() bool { return inv.Layout.Page == PageDriveStorage }

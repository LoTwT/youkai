package collect

import (
	"image"
	"image/color"
	"time"
)

// maccloudWidth/Height are the raw-stream buffer size of the macOS cloud
// client (report "制品与环境": 1920×1080, game content in the left
// 1650×1080).
const (
	maccloudWidth  = 1920
	maccloudHeight = 1080
)

// slotCentersY are the measured row centers: 255 + 146.25r rounded to the
// frame pixels the click points were verified against (report §1).
var maccloudSlotCentersY = [5]int{255, 401, 548, 694, 840}

// maccloudWheelCenters are the six wheel positions, numbered from the
// top-left counter-clockwise 1..6 (report §1, screen points converted to
// frame pixels).
var maccloudWheelCenters = [6]image.Point{
	image.Pt(1065, 371), // 1 top-left
	image.Pt(978, 566),  // 2 left
	image.Pt(1065, 757), // 3 bottom-left
	image.Pt(1340, 757), // 4 bottom-right
	image.Pt(1430, 566), // 5 right
	image.Pt(1340, 371), // 6 top-right
}

// maccloudWheelArcs are the narrowed selection-color probes around each
// wheel center, relative to the center. The draft's ±95 ring was measured
// as a whole; the four arc blocks reduce the masked fixture while keeping
// a wide separation: across 280 sampled frames the selected position
// counted 285–723 selection-colored pixels and every other position
// counted 0 (design §2's "4b-1 收窄成几个弧段小块").
var maccloudWheelArcs = []image.Rectangle{
	image.Rect(-15, -95, 15, -60),
	image.Rect(-15, 60, 15, 95),
	image.Rect(-95, -15, -60, 15),
	image.Rect(60, -15, 95, 15),
}

// maccloudDetailLines are the detail pane's calibrated line regions
// (report §6 and testdata/macos-cloud/README.md). The fourth row is
// calibrated even though only four-substat discs have a value there: a
// three-substat disc shows the set-effect label in the same band, and the
// dedicated probe below tells the two forms apart.
var maccloudDetailLines = []LineRegion{
	{Role: LineName, Rect: image.Rect(526, 205, 706, 237)},
	{Role: LineLevel, Rect: image.Rect(600, 258, 706, 289)},
	{Role: LineMain, Rect: image.Rect(540, 342, 826, 362)},
	{Role: LineSub1, Rect: image.Rect(540, 424, 826, 444)},
	{Role: LineSub2, Rect: image.Rect(540, 469, 826, 488)},
	{Role: LineSub3, Rect: image.Rect(540, 514, 826, 533)},
	{Role: LineSub4, Rect: image.Rect(540, 558, 826, 578)},
}

// MacCloudLayout returns the calibrated anchor set for the macOS cloud
// client at the 1920×1080 raw-stream size measured in the M2 sample round
// (T5 §1/§3, report §1–§6). Every value is a frame pixel; the calibration
// is one display configuration, recognized by the exact image size and
// content region.
func MacCloudLayout() Layout {
	slots := make([]image.Rectangle, 0, 5*4)
	for r := 0; r < 5; r++ {
		for c := 0; c < 4; c++ {
			cx, cy := 119+112*c, maccloudSlotCentersY[r]
			slots = append(slots, image.Rect(cx-50, cy-60, cx+50, cy+60))
		}
	}
	return Layout{
		Name:    "maccloud-1920x1080",
		Image:   image.Pt(maccloudWidth, maccloudHeight),
		Content: image.Rect(0, 0, 1650, maccloudHeight),

		Rows:    5,
		Columns: 4,
		Slots:   slots,
		// The selection border sits immediately left of the cell and the
		// rarity bar along its bottom edge (report §1's sel tool, verified
		// on all 120 sampled viewports).
		Cell: CellProbes{
			Border: image.Rect(-55, -30, -50, 30),
			Bar:    image.Rect(-30, 38, 30, 46),
			// The management badge sits in the cell's bottom-right
			// corner: gray lock, red trash or nothing. The region is
			// centered on the badge (measured center +35,+19 from the
			// slot center).
			Management: image.Rect(20, 4, 50, 34),
			// The equipped-agent avatar sits in the top-right corner;
			// the probe is centered on it (measured center +36,-38).
			Avatar: image.Rect(20, -54, 52, -22),
		},
		RowPitch: 147, // report §5: 588 frame pixels per four-row step.

		ListViewport: image.Rect(60, 195, 512, 930), // shift tool's region.
		// The thumb column; the two end caps (215–218 and 909–911) are
		// outside the rect on purpose.
		ScrollTrack:    image.Rect(47, 220, 48, 905),
		ScrollFrom:     image.Pt(286, 848),
		ScrollTo:       image.Pt(286, 262),
		ScrollDuration: time.Second, // platform drag bound; 1000ms = 4 rows.
		TopFrom:        image.Pt(286, 295),
		TopTo:          image.Pt(286, 862),
		TopDuration:    150 * time.Millisecond,

		// The funnel icon's center pixel: dark (7,10,11) with no filter,
		// blue (91,142,231) with one (report §4).
		FilterButton: image.Pt(270, 143),
		Funnel: ColorProbe{
			At: image.Rect(162, 143, 163, 144),
			Colors: map[string]color.RGBA{
				"clear":  {R: 7, G: 10, B: 11, A: 255},
				"active": {R: 91, G: 142, B: 231, A: 255},
			},
			Tol: 25,
		},
		// The panel's red header mark and white title pixel, against the
		// same points' list-page dark values (report §4, three panel and
		// two list frames).
		PanelOpen: []ColorProbe{
			{
				At: image.Rect(1530, 130, 1531, 131),
				Colors: map[string]color.RGBA{
					"open":   {R: 183, G: 56, B: 31, A: 255},
					"closed": {R: 15, G: 14, B: 15, A: 255},
				},
				Tol: 25,
			},
			{
				At: image.Rect(1108, 152, 1109, 153),
				Colors: map[string]color.RGBA{
					"open":   {R: 255, G: 255, B: 255, A: 255},
					"closed": {R: 9, G: 11, B: 14, A: 255},
				},
				Tol: 25,
			},
		},
		PanelReset: image.Pt(1230, 985),
		PanelClose: image.Pt(1550, 143),
		// The S rarity chip: the white S tile at about (1225,615) in
		// /tmp/m2-samples/s2-02-panel-slot1-after-reset/frame-0001.png
		// (T5 §3). Only S is wired; A/B classification is not implemented.
		PanelS: image.Pt(1225, 615),

		Wheel:     maccloudWheelCenters,
		WheelArcs: append([]image.Rectangle(nil), maccloudWheelArcs...),

		DetailLines:  append([]LineRegion(nil), maccloudDetailLines...),
		QualityBadge: image.Rect(532, 256, 564, 286),
		// The equipped-agent avatar immediately right of the level line
		// (measured circle at about x804–843, y255–286 in the M2 frames).
		DetailAvatar: image.Rect(800, 250, 844, 294),
		// The lock/state indicator immediately left of the detail page's
		// dismantle control; the filled circle means locked (measured at
		// about 1104–1148, 123–161).
		TopManagement: image.Rect(1102, 118, 1152, 166),
		// The value column of the fourth row: a four-substat disc's value
		// reaches here, a three-substat disc's set-effect label does not.
		Substat4Probe: image.Rect(781, 558, 826, 578),
	}
}

// Calibration is the pixel matcher's threshold set. Every threshold is a
// frame-pixel or grayscale value measured on the macOS cloud client's
// samples; the comments name the evidence. The struct is separate from
// Layout because thresholds and anchors change for different reasons.
type Calibration struct {
	// Occupancy bar: the mean of the bar probe's largest RGB channel.
	BarEmptyMax    int // ≤: empty (measured empty bars 6–14)
	BarOccupiedMin int // ≥: occupied (measured occupied bars 240–245)

	// Selection border: the number of selection-colored pixels in the
	// border probe.
	SelectionHitMin   int // ≥: the slot carries the border (measured 60–305; 60 is a clipped edge)
	SelectionClearMax int // <: no border (measured 0); between is undecidable

	// Wheel arcs: the number of selection-colored pixels in the four arc
	// probes. Measured across 280 frames: selected 285–723, others 0.
	WheelSelectedMin int // ≥: the position is selected
	WheelOtherMax    int // ≤: the position is not selected

	// Detail comparison: mean of the seven per-line grayscale MADs.
	// Measured same-content pairs (frame-0000/0001, n=236): max 1.01,
	// p95 0.88. Adjacent cell pairs in the six full viewports (n=186):
	// 183 above 4.0 (min 4.07), two below 2.0 (0.42, 0.62), one in the band
	// (2.38); the 3.72 pair is diagonal, not adjacent. No same-content pair
	// exceeds 2.0, so the bands do not falsely admit; low-MAD adjacent
	// pairs only stop safely (detailUnattributed).
	DetailSameMax      float64
	DetailDifferentMin float64

	// Ink thresholds for the detail probes (grayscale > threshold).
	InkThreshold       int // 60, as in the line-crop calibration
	Substat4InkMin     int // ≥: four substats (measured 132–426)
	Substat3InkMax     int // ≤: three substats (measured 0–21)
	DetailPopulatedInk int // name line ink of a populated pane (measured 2400+)

	// Management badge (cell bottom-right). The lock is a bright neutral
	// annulus around a mostly dark center; the trash is a red glyph; an
	// unmarked cell has neither. Counts are over the badge probe's
	// 30×30 region. Measured across the labelled M2 samples: lock ring
	// 226–260 with only 15–37 bright center pixels, discarded red
	// 279–282, no badge ring ≤29 and red 0. The lock's flash (dark
	// phase) is not in the recorded samples; callers union multiple
	// frames so one dim frame cannot erase a lock.
	ManagementLockRingMin   int // ≥: gray lock annulus
	ManagementLockCoreMax   int // ≤: hollow lock center (rejects a solid bright block)
	ManagementDiscardRedMin int // ≥: red trash glyph
	ManagementClearRingMax  int // ≤: no management icon
	ManagementClearRedMax   int // ≤: no red trash pixels

	// Avatar (cell top-right and detail level-right). A bright neutral
	// ring covers most directions around a portrait core whose mean
	// brightness and range distinguish it from the empty placeholder.
	// Measured: present ring 168–423 (dim portraits ~137–170 after
	// fixture quantization), 7–8 covered sectors, core mean 105–233 and
	// range ≥162; absent ring ≤116 with an empty core (mean 19–22); a
	// selected-cell corner glow reaches ring 173 but only 5 sectors, so it
	// stays unreadable instead of becoming "present".
	AvatarRingMin           int // ≥: sanity floor for the annulus
	AvatarRingSectorsMin    int // ≥: covered 45° sectors (of 8)
	AvatarCoreMeanMin       int // ≥: portrait core mean brightness
	AvatarCoreRangeMin      int // ≥: portrait core brightness range
	AvatarAbsentRingMax     int // ≤: no avatar ring
	AvatarAbsentCoreMeanMax int // ≤: empty placeholder core

	// Top management indicator (detail page, left of the dismantle
	// control). Counts are over the indicator's disk. Locked is the
	// filled (dim) circle; unlocked is the open circle with a bright
	// glyph. Measured: locked dim 760–763 and bright 6–9; unlocked dim
	// ~327 and bright ~300.
	TopLockedDimMin      int // ≥: filled locked indicator
	TopUnlockedBrightMin int // ≥: open indicator's bright glyph
	TopUnlockedDimMax    int // ≤: open indicator's dim background

	// Quality: gold pixels in the badge probe; every S sample counted
	// 251–256, dimmed panel frames 0.
	QualityGoldMin int

	// Scroll matching on ListViewport (x step 3, y step 2).
	ScrollStepX, ScrollStepY int
	ScrollSearchRadius       int     // dy ∈ [-720,720]
	ScrollMinSamples         int     // minimum overlap samples
	ScrollStationaryMax      float64 // err0 < this: stationary (1.35–1.88)
	ScrollDisplacedMax       float64 // best err ≤ this (6.78–9.67)
	ScrollDisplacedMargin    float64 // and err0 − err ≥ this (≥3)
	ScrollRowTolerance       int     // |dy − k·RowPitch| ≤; also the retained-overlap margin
	ScrollMaxRows            int     // k = 1..Rows-1
	ScrollLostMin            float64 // err0 ≥ this with no alignment → Lost (13.3)

	// Scrollbar thumb detection on ScrollTrack.
	ScrollbarBrightMin    int // grayscale >: thumb pixels (measured 110+)
	ScrollbarTopStartMax  int // segment start ≤: top (measured 226–227)
	ScrollbarBottomEndMin int // segment end ≥: bottom (measured 900)
}

// MacCloudCalibration returns the threshold set measured on the macOS
// cloud client's samples. The values are the ones the full-sample replay
// (TestReplayFieldSamples) printed.
func MacCloudCalibration() Calibration {
	return Calibration{
		BarEmptyMax:        40,
		BarOccupiedMin:     120,
		SelectionHitMin:    40,
		SelectionClearMax:  10,
		WheelSelectedMin:   150,
		WheelOtherMax:      60,
		DetailSameMax:      2.0,
		DetailDifferentMin: 4.0,
		InkThreshold:       60,
		Substat4InkMin:     80,
		Substat3InkMax:     50,
		DetailPopulatedInk: 500,
		QualityGoldMin:     150,

		ManagementLockRingMin:   120,
		ManagementLockCoreMax:   70,
		ManagementDiscardRedMin: 100,
		ManagementClearRingMax:  60,
		ManagementClearRedMax:   30,

		AvatarRingMin:           40,
		AvatarRingSectorsMin:    7,
		AvatarCoreMeanMin:       60,
		AvatarCoreRangeMin:      40,
		AvatarAbsentRingMax:     120,
		AvatarAbsentCoreMeanMax: 45,

		TopLockedDimMin:      650,
		TopUnlockedBrightMin: 200,
		TopUnlockedDimMax:    450,

		ScrollStepX:           3,
		ScrollStepY:           2,
		ScrollSearchRadius:    720,
		ScrollMinSamples:      2000,
		ScrollStationaryMax:   4.0,
		ScrollDisplacedMax:    10.0,
		ScrollDisplacedMargin: 3.0,
		ScrollRowTolerance:    3,
		ScrollMaxRows:         4,
		ScrollLostMin:         10.0,
		ScrollbarBrightMin:    110,
		ScrollbarTopStartMax:  229,
		ScrollbarBottomEndMin: 898,
	}
}

// validate refuses a threshold set the matcher could only use by guessing.
func (c Calibration) validate() error {
	switch {
	case c.BarEmptyMax < 0 || c.BarOccupiedMin <= c.BarEmptyMax:
		return errCalibration("occupancy bar")
	case c.SelectionClearMax < 0 || c.SelectionHitMin <= c.SelectionClearMax:
		return errCalibration("selection border")
	case c.WheelOtherMax < 0 || c.WheelSelectedMin <= c.WheelOtherMax:
		return errCalibration("wheel arcs")
	case c.DetailSameMax <= 0 || c.DetailDifferentMin <= c.DetailSameMax:
		return errCalibration("detail comparison")
	case c.DetailPopulatedInk <= 0 || c.InkThreshold <= 0:
		return errCalibration("detail ink")
	case c.Substat3InkMax < 0 || c.Substat4InkMin <= c.Substat3InkMax:
		return errCalibration("substat row count")
	case c.QualityGoldMin <= 0:
		return errCalibration("quality badge")
	case c.ManagementClearRingMax < 0 || c.ManagementLockRingMin <= c.ManagementClearRingMax ||
		c.ManagementLockCoreMax <= 0 || c.ManagementClearRedMax < 0 ||
		c.ManagementDiscardRedMin <= c.ManagementClearRedMax:
		return errCalibration("management badge")
	case c.AvatarAbsentRingMax < 0 || c.AvatarRingMin <= 0 ||
		c.AvatarRingSectorsMin < 1 || c.AvatarRingSectorsMin > 8 ||
		c.AvatarAbsentCoreMeanMax < 0 || c.AvatarCoreMeanMin <= c.AvatarAbsentCoreMeanMax ||
		c.AvatarCoreRangeMin <= 0:
		return errCalibration("equipped avatar")
	case c.TopLockedDimMin <= 0 || c.TopUnlockedBrightMin <= 0 ||
		c.TopUnlockedDimMax < 0 || c.TopLockedDimMin <= c.TopUnlockedDimMax:
		return errCalibration("top management indicator")
	case c.ScrollStepX <= 0 || c.ScrollStepY <= 0 || c.ScrollSearchRadius <= 0 || c.ScrollMinSamples <= 0:
		return errCalibration("scroll sampling")
	case c.ScrollStationaryMax <= 0 || c.ScrollDisplacedMax <= c.ScrollStationaryMax || c.ScrollDisplacedMargin < 0:
		return errCalibration("scroll thresholds")
	case c.ScrollRowTolerance < 0 || c.ScrollMaxRows < 1 || c.ScrollLostMin < c.ScrollDisplacedMax:
		return errCalibration("scroll rows")
	case c.ScrollbarBrightMin < 0 || c.ScrollbarTopStartMax < 0 || c.ScrollbarBottomEndMin < 0:
		return errCalibration("scrollbar")
	}
	return nil
}

type calibrationError string

func (e calibrationError) Error() string {
	return "collect: calibration: " + string(e) + " thresholds are not ordered"
}

func errCalibration(what string) error { return calibrationError(what) }

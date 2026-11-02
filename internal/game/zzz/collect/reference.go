package collect

import (
	"fmt"
	"image"
	"image/color"
	"time"
)

// The 1920×1080 reference canvas
//
// The Windows local client renders the warehouse at the native client size:
// there is no video encoding or resampling between the game's pixels and the
// captured frame. The 1920×1080 native render therefore defines the reference
// canvas every other 16:9 configuration is expressed against. The macOS cloud
// client's layout is not a pure scale of this canvas (its list panel and
// toolbar are laid out slightly differently), so MacCloudLayout keeps its own
// measured constants and is never replaced by a scaled reference layout.
const (
	ReferenceCanvasWidth  = 1920
	ReferenceCanvasHeight = 1080
)

// DisplayTier is one supported 16:9 client-area tier. Only the tiers listed
// here can become a clientRect transform; a capture whose client area is not
// exactly one of them is a different display configuration and must be
// rejected rather than scaled approximately. The tier names are diagnostic
// labels.
type DisplayTier struct {
	Name   string
	Client image.Point
}

// referenceClientTiers is the closed set of 16:9 client-area sizes the model
// is defined for. 2276×1280 is the game's rounded 16:9 tier at height 1280
// (16:9 × 1280 is 2275.6, rendered as 2276). The tiers being listed here does
// not open any of them: each tier still needs its own field acceptance before
// a profile may be enabled (architecture §6's 不按比例换算宣布支持).
var referenceClientTiers = [...]DisplayTier{
	{Name: "3840x2160", Client: image.Pt(3840, 2160)},
	{Name: "2560x1440", Client: image.Pt(2560, 1440)},
	{Name: "2276x1280", Client: image.Pt(2276, 1280)},
	{Name: "1920x1080", Client: image.Pt(1920, 1080)},
	{Name: "1600x900", Client: image.Pt(1600, 900)},
	{Name: "1280x720", Client: image.Pt(1280, 720)},
}

// openClientTiers is the subset of referenceClientTiers the Windows local
// profile may start. D-2 (2026-11-02) opens 1920×1080 only; the other
// listed tiers are known but not opened and must be refused with a request
// to switch to 1920×1080.
//
// Opening another tier requires its own field acceptance on a Windows
// local client at that native client size: the game display setting and
// measured client pixels (including border offsets at the user's DPI),
// GDI capture fidelity, every ReferenceLayout anchor after the clientRect
// transform, the filter/scroll thresholds (including a long list for the
// scrollbar), input timing, and a complete scan acceptance. Listing a tier
// in referenceClientTiers is not evidence; a scale factor never opens one.
var openClientTiers = [...]DisplayTier{
	{Name: "1920x1080", Client: image.Pt(1920, 1080)},
}

// OpenClientTiers returns a copy of the opened Windows local-client tiers.
func OpenClientTiers() []DisplayTier {
	out := make([]DisplayTier, len(openClientTiers))
	copy(out, openClientTiers[:])
	return out
}

// ReferenceClientTiers returns the closed table of client-area tiers. The
// slice is a copy; mutating it cannot change the accepted table.
func ReferenceClientTiers() []DisplayTier {
	out := make([]DisplayTier, len(referenceClientTiers))
	copy(out, referenceClientTiers[:])
	return out
}

// TransformSource names where a canvas transform's scale and offset came
// from. A clientRect transform is defined by the Windows client area's exact
// size; a measuredVideoRect transform is defined by a cloud client's measured
// video rectangle and may carry an offset. Measured-video-rect measurement is
// not implemented in this model: only the enum and the interface exist so a
// later profile can supply the measured rectangle without changing the type.
type TransformSource string

const (
	// TransformClientRect: the Windows local client, where the game's render
	// fills the client area 1:1. The offset is zero and the scale is the
	// client width divided by the reference canvas width.
	TransformClientRect TransformSource = "clientRect"
	// TransformMeasuredVideoRect: a cloud client's game video rectangle,
	// measured against the frame image. The transform is not derived from the
	// client area and may be offset inside the frame (title bar, letterbox).
	TransformMeasuredVideoRect TransformSource = "measuredVideoRect"
)

// CanvasTransform maps reference-canvas pixels to a display configuration's
// frame pixels: frame = round(reference × Scale) + Offset. Scale and Offset
// are canvas metadata, not a capture profile: a cloud client's full-frame
// identity (title bar, letterbox) is still supplied by that client's profile.
// ScaledLayout therefore produces the canvas's required frame extent in Image
// and the transformed content rectangle in Content; a cloud profile must
// replace both with the measured captured frame and video rectangle when it
// wires the layout.
type CanvasTransform struct {
	Scale  float64
	Offset image.Point
	Source TransformSource
}

// ClientRectTransform returns the transform for a Windows local client whose
// client area is exactly one of the 16:9 tiers in ReferenceClientTiers. Any
// other size is refused: the model never scales an unlisted display
// configuration into support.
func ClientRectTransform(client image.Point) (CanvasTransform, error) {
	for _, tier := range referenceClientTiers {
		if tier.Client != client {
			continue
		}
		return CanvasTransform{
			Scale:  float64(client.X) / float64(ReferenceCanvasWidth),
			Offset: image.Point{},
			Source: TransformClientRect,
		}, nil
	}
	return CanvasTransform{}, fmt.Errorf(
		"collect: canvas: client area %dx%d is not one of the 16:9 tiers (%s); scaling an unlisted display configuration would claim support without evidence",
		client.X, client.Y, tierNames())
}

func tierNames() string {
	out := ""
	for i, tier := range referenceClientTiers {
		if i > 0 {
			out += ", "
		}
		out += tier.Name
	}
	return out
}

// Validate checks that the transform can be used to scale anchors. It is a
// structural check only; the actual per-tier calibration is still an
// acceptance decision, not something a scale factor can establish.
func (t CanvasTransform) Validate() error {
	if t.Scale <= 0 {
		return fmt.Errorf("collect: canvas: scale %v is not positive", t.Scale)
	}
	switch t.Source {
	case TransformClientRect:
		if t.Offset != (image.Point{}) {
			return fmt.Errorf("collect: canvas: a clientRect transform has offset %v; the client area starts at the frame origin", t.Offset)
		}
		// Require the exact tier ratio, not a rounded round-trip: a scale a
		// few ulps off could still round to a tier's pixel size and silently
		// claim a display configuration that was never accepted.
		for _, tier := range referenceClientTiers {
			if t.Scale == float64(tier.Client.X)/float64(ReferenceCanvasWidth) {
				return nil
			}
		}
		return fmt.Errorf("collect: canvas: clientRect scale %v is not exactly a tier's width/%d ratio (%s)", t.Scale, ReferenceCanvasWidth, tierNames())
	case TransformMeasuredVideoRect:
		// The measured rectangle is supplied by a later cloud profile; only
		// the scale's validity is checked here.
	default:
		return fmt.Errorf("collect: canvas: unknown transform source %q", t.Source)
	}
	return nil
}

// ReferenceLayout returns the complete anchor set on the 1920×1080 reference
// canvas. Each value was checked against the Windows local client at
// 1920×1080 (native pixels, no video compression), using the macOS cloud
// calibration transformed by x_mac = 0.859·x_ref, y_mac = 0.859·y_ref + 96
// only as the initial hypothesis. Values that are not directly observable in
// the recorded frames (the management badge, the list avatar and the top
// indicator) are carried from the cloud calibration and marked unverified in
// their comments; the comments name the measured evidence for every other
// group, including the 2026-11-02 W3a filter-panel, sort-label and long-list
// samples.
//
// The layout is a model, not an enabled profile: no tier is opened by this
// value, and a Windows profile still needs its own field acceptance.
func ReferenceLayout() Layout {
	slots := make([]image.Rectangle, 0, 5*4)
	for row := 0; row < 5; row++ {
		for column := 0; column < 4; column++ {
			cx := 139 + 130*column
			cy := 181 + 171*row
			// The click region is the derived 116×140 card region (the cloud
			// calibration's ±50×60 at this scale). The measured card's visible
			// footprint is narrower vertically (x 82–196, y 121–241, so
			// 114×120); the click region keeps the cloud-sized hit area because
			// the exact clickable bounds are not observable from a screenshot.
			slots = append(slots, image.Rect(cx-58, cy-70, cx+58, cy+70))
		}
	}
	return Layout{
		Name:    "winlocal-1920x1080",
		Image:   image.Pt(ReferenceCanvasWidth, ReferenceCanvasHeight),
		Content: image.Rect(0, 0, ReferenceCanvasWidth, ReferenceCanvasHeight),

		Rows:    5,
		Columns: 4,
		Slots:   slots,
		Cell: CellProbes{
			// The selection border is a 3-pixel-wide yellow line at
			// x 78–80, y 132–230 (measured: RGB 255,222,0) immediately left
			// of the card. The probe keeps the narrower mac height so it
			// samples the middle of the border and the same verdict survives
			// the cloud transform.
			Border: image.Rect(-63, -35, -58, 35),
			// The rarity bar spans the card's lower edge at y 222–234 for the
			// first row (pitch 171). The probe samples the bar's middle 70
			// pixels horizontally across its full height.
			Bar: image.Rect(-35, 41, 35, 53),
			// The management badge and equipped-agent avatar are inherited
			// from the mac calibration: the Windows fixture has no labelled
			// badge sample, so these two probes are structurally placed at the
			// card's bottom-right and top-right corners but their verdicts are
			// unverified on Windows.
			Management: image.Rect(23, 5, 58, 40),
			Avatar:     image.Rect(23, -63, 60, -26),
		},
		// Measured card pitch: rarity bars start at y 222, 393, 563 and 734
		// (pitch 170–172); the row centers are 181+171r.
		RowPitch: 171,

		// The measured textured list panel spans x 70–596 and y 115–971 on the
		// 1920×1080 capture; the black bottom bar starts at y≈976 and the detail
		// pane starts to the right of the panel. The mac-derived viewport
		// (x 70–596, y 115–971 after the canvas transform) matches this measured
		// range, so the reference uses the measured panel bounds directly.
		ListViewport: image.Rect(70, 115, 596, 971),
		// The scrollbar is a 5-pixel column at x 54–59. The W2a captures
		// (11–13 items) only showed a filled thumb; the W3a long-list sample
		// (about 28 items, 1.4 screens) shows both resting ends and a moving
		// thumb at the probe's 1-pixel column x=55. The bright run is
		// y 148–711 at the top, 368–931 at the bottom and 361–924 mid-drag;
		// the thumb is 564 pixels long and its darkest pixel is 116, while
		// the groove is 0 (one anti-aliased edge pixel is 92–93). This
		// verifies both premises ReferenceCalibration depends on: a resting
		// thumb reaches the measured ends, and the groove stays below
		// ScrollbarBrightMin=110. The end thresholds keep the mac 2–3 px
		// slack (151/929); the darkest-thumb margin is only 6 levels and the
		// sample covers one list length, so a longer list is still untested.
		// The separate y 941–945 run is the bottom cap, not the track.
		ScrollTrack: image.Rect(55, 145, 56, 941),
		// Drag endpoints are provisional canvas positions derived from the
		// mac profile (the Windows drag/timing profile is out of scope in this
		// model). The traversal must not use them until the Windows profile is
		// calibrated.
		ScrollFrom:     image.Pt(333, 875),
		ScrollTo:       image.Pt(333, 193),
		ScrollDuration: 1200 * time.Millisecond,
		TopFrom:        image.Pt(333, 232),
		TopTo:          image.Pt(333, 892),
		TopDuration:    150 * time.Millisecond,

		// The funnel button occupies the toolbar's text control around
		// x 292–340; the click point is its center. The icon itself spans
		// x 175–220 and the probe reads the solid blue fill at (189,55).
		FilterButton: image.Pt(314, 55),
		Funnel: ColorProbe{
			At: image.Rect(189, 55, 190, 56),
			// Active: the measured Windows fill (53,140,252). Clear: carried
			// from the mac calibration (7,10,11); no Windows clear-state
			// sample was recorded, so the clear tier is unverified.
			Colors: map[string]color.RGBA{
				"clear":  {R: 7, G: 10, B: 11, A: 255},
				"active": {R: 53, G: 140, B: 252, A: 255},
			},
			Tol: 25,
		},
		// The filter panel was sampled on Windows in the 2026-11-02 W3a
		// round. The first probe's open tier is the measured S2 fill
		// (196,41,10); the old cloud-derived (183,56,31) was only 4 levels
		// inside the blue channel's tolerance. Its closed tier keeps the
		// earlier measured (15,14,15); S3 reads (16,16,16), within tolerance.
		// The second probe's open tier is (255,255,255) on S2, matching the
		// cloud value; its closed tier keeps the cloud-derived (9,11,14),
		// and S3 reads (0,0,0), within tolerance. Both probes report open on
		// all three S2 frames and closed on all three S3 frames.
		PanelOpen: []ColorProbe{
			{
				// The cloud probe's canvas position rounds onto the bright
				// toolbar edge at y=40; the adjacent dark pixel at y=39 is the
				// closed-state sample on the measured frame.
				At: image.Rect(1781, 39, 1782, 40),
				Colors: map[string]color.RGBA{
					"open":   {R: 196, G: 41, B: 10, A: 255},
					"closed": {R: 15, G: 14, B: 15, A: 255},
				},
				Tol: 25,
			},
			{
				At: image.Rect(1290, 65, 1291, 66),
				Colors: map[string]color.RGBA{
					"open":   {R: 255, G: 255, B: 255, A: 255},
					"closed": {R: 9, G: 11, B: 14, A: 255},
				},
				Tol: 25,
			},
		},
		// W3a checked the reset and close click points against the S2 panel
		// frame; both agree with these positions. PanelS is not calibrated:
		// with two set filters selected, the S control was observed near
		// y≈704 rather than y=604, because the set-filter rows push the
		// controls down. Re-measure it after a reset; until then the value
		// stays the cloud-derived placeholder.
		PanelReset: image.Pt(1432, 1035),
		PanelClose: image.Pt(1804, 55),
		PanelS:     image.Pt(1426, 604),
		// The Windows local client renders "稀有度" at native pixels. The
		// W3a template is the crop (1285,124)-(1467,201) of S2/frame-0000.png
		// (182×77); see sortlabel.go for its provenance. The template is
		// reference-canvas pixels, so only the 1:1 1920×1080 layout declares
		// it; ScaledLayout drops it for every other tier rather than
		// resampling the template. The mac cloud client keeps its own
		// template.
		SortLabel:    image.Rect(1285, 124, 1467, 201),
		SortTemplate: windowsLocalSortLabelTemplate,

		// The six wheel centers are the cloud calibration carried through the
		// canvas transform; none of the six click centers was independently
		// measured on Windows. Position 3's selection ring was checked on the
		// Windows frame: the ring is centered at about (1239,763) with a
		// measured radius of 85–105 pixels, about 7 px above the mac-derived
		// wheel center (1240,770). The arc blocks still capture the ring, but
		// the click center remains the derived value and is unverified.
		Wheel: [6]image.Point{
			image.Pt(1240, 320), // 1 top-left
			image.Pt(1139, 547), // 2 left
			image.Pt(1240, 770), // 3 bottom-left
			image.Pt(1560, 770), // 4 bottom-right
			image.Pt(1665, 547), // 5 right
			image.Pt(1560, 320), // 6 top-right
		},
		// The four arc blocks are the cloud probe blocks carried through the
		// canvas transform (inner radius 70, outer 111). They contain the
		// measured Windows selection ring (radius 85–105) and produce no hits
		// on the five unselected positions.
		WheelArcs: []image.Rectangle{
			image.Rect(-17, -111, 17, -70),
			image.Rect(-17, 70, 17, 111),
			image.Rect(-111, -17, -70, 17),
			image.Rect(70, -17, 111, 17),
		},

		// Detail line bands were checked against the measured text rows:
		// name y 128–159, level 185–211, main 285–311, substats 378–405,
		// 432–449, 484–509 and 534–561. The x ranges keep the cloud crop's
		// canvas position, which covers both the label and value columns
		// (labels start at x 632, values at x 922–959).
		DetailLines: []LineRegion{
			{Role: LineName, Rect: image.Rect(612, 127, 822, 164)},
			{Role: LineLevel, Rect: image.Rect(699, 186, 822, 225)},
			{Role: LineMain, Rect: image.Rect(629, 284, 962, 311)},
			{Role: LineSub1, Rect: image.Rect(629, 379, 962, 406)},
			{Role: LineSub2, Rect: image.Rect(629, 431, 962, 455)},
			{Role: LineSub3, Rect: image.Rect(629, 484, 962, 509)},
			{Role: LineSub4, Rect: image.Rect(629, 535, 962, 561)},
		},
		// The S badge is a gold tile at x 623–651, y 184–212 (measured gold
		// pixels 362 in this crop).
		QualityBadge: image.Rect(619, 186, 657, 221),
		// The equipped-agent avatar is the bright circle at x 935–972,
		// y 181–218. The cloud crop is larger; it still contains the whole
		// circle, so the inherited bounds are kept for cloud compatibility.
		DetailAvatar: image.Rect(931, 179, 982, 230),
		// The lock/state indicator is the third toolbar circle, measured
		// centered at (1305,51) with a radius of about 28. The cloud crop is
		// kept: it contains most of the circle and the same open-ring shape.
		TopManagement: image.Rect(1283, 26, 1341, 81),
		// The fourth substat row's value column: measured value text at
		// x 921–960, y 537–555; this is the same band the three-substat
		// set-effect label does not reach.
		Substat4Probe: image.Rect(909, 537, 962, 561),
	}
}

// ScaledLayout returns ref scaled by t into the target display
// configuration's frame pixels. Positions and lengths are rounded; every
// rectangle is rasterized from its center (c' = round(c·s)+offset, then a
// rounded width/height, minimum 1 pixel) so a 1×1 probe cannot drift by a
// corner-rounding half pixel.
//
// Image and Content are the canvas's required frame extent and transformed
// content rectangle. For a clientRect transform they are exactly the client
// area; for a measuredVideoRect transform the image includes the offset (the
// canvas's bounding box), while the cloud profile still supplies the captured
// frame and content (title bar, letterbox) when it wires the layout. A sort
// template is never scaled: the convention is to resample the live sort
// region back to the reference canvas and compare it there. A scaled layout
// therefore drops the sort claim instead of shrinking the template; only the
// 1:1 1920×1080 reference layout carries the Windows native template.
//
// The returned layout is a structural model. Its pixel thresholds still need
// their own per-tier acceptance; see ScaledCalibration and WithOverrides.
func ScaledLayout(ref Layout, t CanvasTransform) (Layout, error) {
	if err := t.Validate(); err != nil {
		return Layout{}, err
	}
	identity := t.Scale == 1 && t.Offset == (image.Point{})

	out := ref
	out.Image = image.Pt(
		maxInt(1, int(roundFloat(float64(ref.Image.X)*t.Scale))+t.Offset.X),
		maxInt(1, int(roundFloat(float64(ref.Image.Y)*t.Scale))+t.Offset.Y),
	)
	out.Content = scaleRect(ref.Content, t)
	out.Name = fmt.Sprintf("%s@%dx%d", ref.Name, out.Image.X, out.Image.Y)
	if identity {
		out.Name = ref.Name
	}

	out.Slots = make([]image.Rectangle, len(ref.Slots))
	for i, slot := range ref.Slots {
		out.Slots[i] = scaleRect(slot, t)
	}
	out.Cell = CellProbes{
		Border:     scaleRelativeRect(ref.Cell.Border, t.Scale),
		Bar:        scaleRelativeRect(ref.Cell.Bar, t.Scale),
		Management: scaleRelativeRect(ref.Cell.Management, t.Scale),
		Avatar:     scaleRelativeRect(ref.Cell.Avatar, t.Scale),
	}
	out.RowPitch = maxInt(1, int(roundFloat(float64(ref.RowPitch)*t.Scale)))

	out.ListViewport = scaleRect(ref.ListViewport, t)
	out.ScrollTrack = scaleRect(ref.ScrollTrack, t)
	out.ScrollFrom = scalePoint(ref.ScrollFrom, t)
	out.ScrollTo = scalePoint(ref.ScrollTo, t)
	out.TopFrom = scalePoint(ref.TopFrom, t)
	out.TopTo = scalePoint(ref.TopTo, t)
	// ScrollDuration and TopDuration are client-profile timing, not canvas
	// geometry; they are copied unchanged and must be calibrated separately.

	out.FilterButton = scalePoint(ref.FilterButton, t)
	out.Funnel = scaleColorProbe(ref.Funnel, t)
	out.PanelOpen = make([]ColorProbe, len(ref.PanelOpen))
	for i, probe := range ref.PanelOpen {
		out.PanelOpen[i] = scaleColorProbe(probe, t)
	}
	out.PanelReset = scalePoint(ref.PanelReset, t)
	out.PanelClose = scalePoint(ref.PanelClose, t)
	out.PanelS = scalePoint(ref.PanelS, t)
	if identity {
		out.SortLabel = ref.SortLabel
		out.SortTemplate = ref.SortTemplate
	} else {
		// Templates are reference-canvas pixels. The scaled profile must
		// resample the live region to the reference canvas before comparing
		// it; shrinking the template here would compare two resamplings and
		// silently change the calibrated MAD.
		out.SortLabel = image.Rectangle{}
		out.SortTemplate = nil
	}

	for i, center := range ref.Wheel {
		out.Wheel[i] = scalePoint(center, t)
	}
	out.WheelArcs = make([]image.Rectangle, len(ref.WheelArcs))
	for i, arc := range ref.WheelArcs {
		out.WheelArcs[i] = scaleRelativeRect(arc, t.Scale)
	}
	out.DetailLines = make([]LineRegion, len(ref.DetailLines))
	for i, line := range ref.DetailLines {
		out.DetailLines[i] = LineRegion{Role: line.Role, Rect: scaleRect(line.Rect, t)}
	}
	out.QualityBadge = scaleRect(ref.QualityBadge, t)
	out.DetailAvatar = scaleRect(ref.DetailAvatar, t)
	out.TopManagement = scaleRect(ref.TopManagement, t)
	out.Substat4Probe = scaleRect(ref.Substat4Probe, t)
	return out, nil
}

func scaleRect(r image.Rectangle, t CanvasTransform) image.Rectangle {
	if r.Empty() {
		return image.Rectangle{}
	}
	center := rectCenter(r)
	scaledCenter := scalePoint(center, t)
	return rasterRect(scaledCenter, r.Dx(), r.Dy(), t.Scale)
}

// scaleRelativeRect scales a cell- or center-relative rectangle. The transform
// offset belongs to absolute canvas coordinates and is deliberately not
// applied.
func scaleRelativeRect(r image.Rectangle, s float64) image.Rectangle {
	if r.Empty() {
		return image.Rectangle{}
	}
	center := rectCenter(r)
	scaledCenter := image.Pt(int(roundFloat(float64(center.X)*s)), int(roundFloat(float64(center.Y)*s)))
	return rasterRect(scaledCenter, r.Dx(), r.Dy(), s)
}

func rasterRect(center image.Point, width, height int, s float64) image.Rectangle {
	w := maxInt(1, int(roundFloat(float64(width)*s)))
	h := maxInt(1, int(roundFloat(float64(height)*s)))
	min := image.Pt(center.X-w/2, center.Y-h/2)
	return image.Rectangle{Min: min, Max: min.Add(image.Pt(w, h))}
}

func scalePoint(p image.Point, t CanvasTransform) image.Point {
	return image.Pt(
		int(roundFloat(float64(p.X)*t.Scale))+t.Offset.X,
		int(roundFloat(float64(p.Y)*t.Scale))+t.Offset.Y,
	)
}

func scaleColorProbe(p ColorProbe, t CanvasTransform) ColorProbe {
	p.At = scaleRect(p.At, t)
	return p
}

func roundFloat(v float64) float64 {
	if v < 0 {
		return -roundFloat(-v)
	}
	return float64(int(v + 0.5))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

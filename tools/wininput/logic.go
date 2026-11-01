package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/LoTwT/youkai/internal/platform/windows"
)

// The 1920×1080 reference canvas of the warehouse page. The cell anchors are
// the approximate field values the operator measured on the Windows local
// client (first column center x≈139, column pitch ≈130; first row center
// y≈185, row pitch ≈170). They are probe inputs, not a calibrated production
// layout: a few pixels of error are expected, and the evidence report records
// the exact values used so a later round can refine them.
const (
	ReferenceCanvasWidth  = 1920
	ReferenceCanvasHeight = 1080

	ReferenceFirstCenterX = 139
	ReferenceFirstCenterY = 185
	ReferenceColumnPitch  = 130
	ReferenceRowPitch     = 170

	// ReferenceRows and ReferenceColumns bound the visible warehouse grid at
	// the reference canvas. They match the 5×4 grid the macOS cloud client
	// calibration recorded and the given Windows pitches; a request outside
	// them is refused instead of clicking an arbitrary point on the page.
	ReferenceRows    = 5
	ReferenceColumns = 4
)

// Reference regions of the game target. They are approximations chosen for
// this probe and are reported verbatim so a field round can compare them with
// the real page:
//
//   - the cell border bands straddle the likely left and bottom edges of a
//     cell whose pitch is 130×170 (half extents 60×75 reference pixels);
//   - the detail name-row band is a wide strip in the upper right pane. It is
//     deliberately wider than one text line so a small vertical calibration
//     error still lands inside it.
var (
	referenceBorderLeft = windows.PixelRect{
		Min: windows.PixelPoint{X: -68, Y: -60},
		Max: windows.PixelPoint{X: -52, Y: 60},
	}
	referenceBorderBottom = windows.PixelRect{
		Min: windows.PixelPoint{X: -50, Y: 67},
		Max: windows.PixelPoint{X: 50, Y: 83},
	}
	referenceNameRow = windows.PixelRect{
		Min: windows.PixelPoint{X: 720, Y: 120},
		Max: windows.PixelPoint{X: 1560, Y: 270},
	}
)

// Tier is one accepted client-area size. The probe only scales the reference
// canvas by an explicitly accepted 16:9 tier; an arbitrary window size is
// refused rather than approximated, because the layout scaling is only
// validated at these sizes.
type Tier struct {
	Width  int     `json:"width"`
	Height int     `json:"height"`
	Scale  float64 `json:"scale"`
}

// Name returns the size label used in evidence and errors.
func (t Tier) Name() string {
	return fmt.Sprintf("%dx%d", t.Width, t.Height)
}

// StandardTiers is the explicit acceptance list: the common 16:9 client sizes
// plus the physical-pixel sizes a per-monitor-v2 window produces for
// 1920×1080 at 125%, 150% and 200% scaling. 1366×768 (not exact 16:9),
// 1920×1200 and arbitrary dragged window sizes are not accepted.
var StandardTiers = []Tier{
	{Width: 1280, Height: 720, Scale: 1280.0 / ReferenceCanvasWidth},
	{Width: 1600, Height: 900, Scale: 1600.0 / ReferenceCanvasWidth},
	{Width: 1920, Height: 1080, Scale: 1920.0 / ReferenceCanvasWidth},
	{Width: 2400, Height: 1350, Scale: 2400.0 / ReferenceCanvasWidth},
	{Width: 2560, Height: 1440, Scale: 2560.0 / ReferenceCanvasWidth},
	{Width: 2880, Height: 1620, Scale: 2880.0 / ReferenceCanvasWidth},
	{Width: 3200, Height: 1800, Scale: 3200.0 / ReferenceCanvasWidth},
	{Width: 3840, Height: 2160, Scale: 3840.0 / ReferenceCanvasWidth},
}

// TierList returns the accepted sizes for usage and error text.
func TierList() string {
	names := make([]string, 0, len(StandardTiers))
	for _, tier := range StandardTiers {
		names = append(names, tier.Name())
	}
	return strings.Join(names, ", ")
}

// MatchStandardTier accepts only an exact entry of StandardTiers.
func MatchStandardTier(width, height int) (Tier, error) {
	for _, tier := range StandardTiers {
		if tier.Width == width && tier.Height == height {
			return tier, nil
		}
	}
	return Tier{}, fmt.Errorf("client size %dx%d is not an accepted 16:9 tier (%s); resize the client to one of these exact sizes", width, height, TierList())
}

// ParseSlotPar parses the 1-based "-slot r,c" value. Row 1, column 1 is the
// first visible warehouse cell, consistent with the user-facing position
// numbers; the internal canvas anchors are zero-based arithmetic.
func ParseSlotPar(s string) (row, column int, err error) {
	parts := strings.Split(s, ",")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("slot %q must be \"row,column\", for example 1,1", s)
	}
	row, err = strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, fmt.Errorf("slot row %q is not an integer", strings.TrimSpace(parts[0]))
	}
	column, err = strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, fmt.Errorf("slot column %q is not an integer", strings.TrimSpace(parts[1]))
	}
	if row < 1 || row > ReferenceRows {
		return 0, 0, fmt.Errorf("slot row %d is outside 1..%d", row, ReferenceRows)
	}
	if column < 1 || column > ReferenceColumns {
		return 0, 0, fmt.Errorf("slot column %d is outside 1..%d", column, ReferenceColumns)
	}
	return row, column, nil
}

// CanvasPoint returns the 1920×1080 reference center of a 1-based slot.
func CanvasPoint(row, column int) (windows.PixelPoint, error) {
	if row < 1 || row > ReferenceRows {
		return windows.PixelPoint{}, fmt.Errorf("row %d is outside 1..%d", row, ReferenceRows)
	}
	if column < 1 || column > ReferenceColumns {
		return windows.PixelPoint{}, fmt.Errorf("column %d is outside 1..%d", column, ReferenceColumns)
	}
	return windows.PixelPoint{
		X: ReferenceFirstCenterX + (column-1)*ReferenceColumnPitch,
		Y: ReferenceFirstCenterY + (row-1)*ReferenceRowPitch,
	}, nil
}

// ScaleCanvasPoint maps a reference-canvas point to client-area pixels. The
// scale is the client width divided by 1920, per the page's uniform scaling.
// Rounding is to nearest: the reference anchor is a continuous position, and
// the nearest physical pixel is the best available representation.
func ScaleCanvasPoint(p windows.PixelPoint, tier Tier) windows.PixelPoint {
	return windows.PixelPoint{
		X: int(math.Round(float64(p.X) * tier.Scale)),
		Y: int(math.Round(float64(p.Y) * tier.Scale)),
	}
}

// ScaleCanvasRect maps a reference-canvas rectangle to client-area pixels.
// Min and Max are scaled independently so the rectangle's edges stay aligned
// with the scaled anchors rather than its center.
func ScaleCanvasRect(r windows.PixelRect, tier Tier) windows.PixelRect {
	return windows.PixelRect{
		Min: windows.PixelPoint{
			X: int(math.Round(float64(r.Min.X) * tier.Scale)),
			Y: int(math.Round(float64(r.Min.Y) * tier.Scale)),
		},
		Max: windows.PixelPoint{
			X: int(math.Round(float64(r.Max.X) * tier.Scale)),
			Y: int(math.Round(float64(r.Max.Y) * tier.Scale)),
		},
	}
}

// GamePlan is the complete client-relative game click plan. The click point
// and all evidence regions are in captured-image pixels; ScreenPoint and the
// Screen* rectangles add the client origin.
type GamePlan struct {
	Tier          Tier
	CanvasPoint   windows.PixelPoint
	ClientPoint   windows.PixelPoint
	ScreenPoint   windows.PixelPoint
	BorderLeft    windows.PixelRect
	BorderBottom  windows.PixelRect
	NameRow       windows.PixelRect
	ScreenLeft    windows.PixelRect
	ScreenBottom  windows.PixelRect
	ScreenNameRow windows.PixelRect
}

// PlanGameClick validates the client tier and derives the click point and
// effect regions. All regions must fit inside the captured client image; a
// reference band that does not fit is a refusal, not a silent crop.
func PlanGameClick(row, column int, geo windows.ClientGeometry) (GamePlan, error) {
	tier, err := MatchStandardTier(geo.Client.Dx(), geo.Client.Dy())
	if err != nil {
		return GamePlan{}, err
	}
	canvas, err := CanvasPoint(row, column)
	if err != nil {
		return GamePlan{}, err
	}
	clientPoint := ScaleCanvasPoint(canvas, tier)
	screenPoint := windows.PixelPoint{
		X: geo.Client.Min.X + clientPoint.X,
		Y: geo.Client.Min.Y + clientPoint.Y,
	}
	if !geo.Client.Contains(screenPoint) {
		return GamePlan{}, fmt.Errorf("scaled click point %v is outside the client rect %v", screenPoint, geo.Client)
	}
	image := windows.PixelRect{
		Min: windows.PixelPoint{},
		Max: windows.PixelPoint{X: geo.Client.Dx(), Y: geo.Client.Dy()},
	}
	borderLeft := ScaleCanvasRect(offsetRect(referenceBorderLeft, canvas), tier)
	borderBottom := ScaleCanvasRect(offsetRect(referenceBorderBottom, canvas), tier)
	nameRow := ScaleCanvasRect(referenceNameRow, tier)
	for name, rect := range map[string]windows.PixelRect{
		"cell border left":   borderLeft,
		"cell border bottom": borderBottom,
		"detail name row":    nameRow,
	} {
		if rect.Empty() || !image.ContainsRect(rect) {
			return GamePlan{}, fmt.Errorf("%s region %v is empty or outside the %dx%d client image", name, rect, image.Dx(), image.Dy())
		}
	}
	if !image.Contains(clientPoint) {
		return GamePlan{}, fmt.Errorf("scaled click point %v is outside the %dx%d client image", clientPoint, image.Dx(), image.Dy())
	}
	return GamePlan{
		Tier:          tier,
		CanvasPoint:   canvas,
		ClientPoint:   clientPoint,
		ScreenPoint:   screenPoint,
		BorderLeft:    borderLeft,
		BorderBottom:  borderBottom,
		NameRow:       nameRow,
		ScreenLeft:    screenRect(geo.Client.Min, borderLeft),
		ScreenBottom:  screenRect(geo.Client.Min, borderBottom),
		ScreenNameRow: screenRect(geo.Client.Min, nameRow),
	}, nil
}

// NotepadPlan is the client-relative Notepad click plan. The edit area is
// approximated as the client area inset from the tab/menu band at the top and
// the status band at the bottom; the click lands at its center.
type NotepadPlan struct {
	EditRegion        windows.PixelRect
	ScreenEditRegion  windows.PixelRect
	ClientPoint       windows.PixelPoint
	ScreenPoint       windows.PixelPoint
	HorizontalInsetPx int
	TopInsetPx        int
	BottomInsetPx     int
}

// PlanNotepadClick derives the Notepad edit-area center. The client center
// alone would be sufficient for a normal Notepad window, but the insets keep
// the point clear of the tab strip and status bar when the window is short.
func PlanNotepadClick(geo windows.ClientGeometry) (NotepadPlan, error) {
	w, h := geo.Client.Dx(), geo.Client.Dy()
	if w < 200 || h < 120 {
		return NotepadPlan{}, fmt.Errorf("notepad client area %dx%d is too small to define an edit-area center", w, h)
	}
	horizontal := max(1, w*2/100)
	top := max(1, h*12/100)
	bottom := max(1, h*5/100)
	region := windows.PixelRect{
		Min: windows.PixelPoint{X: horizontal, Y: top},
		Max: windows.PixelPoint{X: w - horizontal, Y: h - bottom},
	}
	if region.Empty() || region.Dx() < 50 || region.Dy() < 30 {
		return NotepadPlan{}, fmt.Errorf("notepad edit-area inset leaves an unusable region %v", region)
	}
	clientPoint := rectCenter(region)
	screenPoint := windows.PixelPoint{
		X: geo.Client.Min.X + clientPoint.X,
		Y: geo.Client.Min.Y + clientPoint.Y,
	}
	if !geo.Client.Contains(screenPoint) {
		return NotepadPlan{}, fmt.Errorf("notepad click point %v is outside the client rect %v", screenPoint, geo.Client)
	}
	return NotepadPlan{
		EditRegion:        region,
		ScreenEditRegion:  screenRect(geo.Client.Min, region),
		ClientPoint:       clientPoint,
		ScreenPoint:       screenPoint,
		HorizontalInsetPx: horizontal,
		TopInsetPx:        top,
		BottomInsetPx:     bottom,
	}, nil
}

func offsetRect(r windows.PixelRect, by windows.PixelPoint) windows.PixelRect {
	return windows.PixelRect{
		Min: windows.PixelPoint{X: r.Min.X + by.X, Y: r.Min.Y + by.Y},
		Max: windows.PixelPoint{X: r.Max.X + by.X, Y: r.Max.Y + by.Y},
	}
}

func screenRect(origin windows.PixelPoint, r windows.PixelRect) windows.PixelRect {
	return offsetRect(r, origin)
}

func rectCenter(r windows.PixelRect) windows.PixelPoint {
	return windows.PixelPoint{
		X: r.Min.X + r.Dx()/2,
		Y: r.Min.Y + r.Dy()/2,
	}
}

// ImageStats is the black-screen, variance and content-hash evidence of one
// frame. Alpha is excluded from the statistics but included in the pixel
// hash, exactly like the platform's captured image (always 0xFF).
type ImageStats struct {
	Width       int
	Height      int
	Mean        float64
	Variance    float64
	Black       bool
	PixelSHA256 string
}

// BlackScreenMaxMean is the mean 8-bit RGB value at or below which a frame is
// reported as black. The check is deliberately minimal: the task only needs
// "not a pure black frame" as the page sanity gate.
const BlackScreenMaxMean = 1.0

// AnalyzeImage computes the mean/variance and content hash of a captured
// image. The hash covers the dimensions and every stored row byte (including
// alpha and stride padding).
func AnalyzeImage(im *windows.CapturedImage) (ImageStats, error) {
	if im == nil {
		return ImageStats{}, fmt.Errorf("nil image")
	}
	if im.Width <= 0 || im.Height <= 0 {
		return ImageStats{}, fmt.Errorf("invalid image size %dx%d", im.Width, im.Height)
	}
	if im.Stride < im.Width*4 {
		return ImageStats{}, fmt.Errorf("stride %d is smaller than %d bytes per row", im.Stride, im.Width*4)
	}
	if len(im.Pixels) < im.Stride*im.Height {
		return ImageStats{}, fmt.Errorf("pixel buffer of %d bytes does not hold %dx%d at stride %d", len(im.Pixels), im.Width, im.Height, im.Stride)
	}
	h := sha256.New()
	fmt.Fprintf(h, "%dx%d\n", im.Width, im.Height)
	var sum, sumSquares float64
	var samples int
	for y := 0; y < im.Height; y++ {
		row := im.Pixels[y*im.Stride : y*im.Stride+im.Width*4]
		_, _ = h.Write(row)
		for i := 0; i < len(row); i += 4 {
			for c := 0; c < 3; c++ {
				v := float64(row[i+c])
				sum += v
				sumSquares += v * v
				samples++
			}
		}
	}
	mean := sum / float64(samples)
	variance := sumSquares/float64(samples) - mean*mean
	if variance < 0 {
		variance = 0
	}
	return ImageStats{
		Width:       im.Width,
		Height:      im.Height,
		Mean:        mean,
		Variance:    variance,
		Black:       mean <= BlackScreenMaxMean,
		PixelSHA256: hex.EncodeToString(h.Sum(nil)),
	}, nil
}

// RegionDiff is the pre/post comparison of one evidence region.
type RegionDiff struct {
	Region          windows.PixelRect
	Pixels          int
	MeanAbsDiff     float64
	ChangedPixels   int
	ChangedFraction float64
	MaxDelta        int
	LumaBefore      float64
	LumaAfter       float64
	LumaDelta       float64
}

// DiffRegion compares the same client-relative region of two captured frames.
// A pixel counts as changed when its largest RGB channel delta reaches
// pixelDelta. MeanAbsDiff is the mean absolute RGB difference over the whole
// region; LumaBefore/After are the mean Rec.601 luminance values. The function
// never reads outside the region or the images.
func DiffRegion(before, after *windows.CapturedImage, region windows.PixelRect, pixelDelta int) (RegionDiff, error) {
	if before == nil || after == nil {
		return RegionDiff{}, fmt.Errorf("nil frame")
	}
	if before.Width != after.Width || before.Height != after.Height {
		return RegionDiff{}, fmt.Errorf("frame sizes differ: %dx%d vs %dx%d", before.Width, before.Height, after.Width, after.Height)
	}
	if before.Stride < before.Width*4 || after.Stride < after.Width*4 {
		return RegionDiff{}, fmt.Errorf("invalid stride %d/%d for width %d", before.Stride, after.Stride, before.Width)
	}
	if len(before.Pixels) < before.Stride*before.Height || len(after.Pixels) < after.Stride*after.Height {
		return RegionDiff{}, fmt.Errorf("pixel buffers do not hold the declared images")
	}
	image := windows.PixelRect{
		Min: windows.PixelPoint{},
		Max: windows.PixelPoint{X: before.Width, Y: before.Height},
	}
	if region.Empty() || !image.ContainsRect(region) {
		return RegionDiff{}, fmt.Errorf("region %v is empty or outside the %dx%d image", region, before.Width, before.Height)
	}
	if pixelDelta < 1 {
		return RegionDiff{}, fmt.Errorf("pixel delta %d must be positive", pixelDelta)
	}
	diff := RegionDiff{Region: region, Pixels: region.Dx() * region.Dy()}
	var sumAbs float64
	var lumaBefore, lumaAfter float64
	for y := region.Min.Y; y < region.Max.Y; y++ {
		for x := region.Min.X; x < region.Max.X; x++ {
			bi := y*before.Stride + 4*x
			ai := y*after.Stride + 4*x
			br, bg, bb := float64(before.Pixels[bi]), float64(before.Pixels[bi+1]), float64(before.Pixels[bi+2])
			ar, ag, ab := float64(after.Pixels[ai]), float64(after.Pixels[ai+1]), float64(after.Pixels[ai+2])
			dr := math.Abs(ar - br)
			dg := math.Abs(ag - bg)
			db := math.Abs(ab - bb)
			sumAbs += (dr + dg + db) / 3
			if d := maxInt3(int(dr), int(dg), int(db)); d > diff.MaxDelta {
				diff.MaxDelta = d
			}
			if maxFloat3(dr, dg, db) >= float64(pixelDelta) {
				diff.ChangedPixels++
			}
			lumaBefore += 0.299*br + 0.587*bg + 0.114*bb
			lumaAfter += 0.299*ar + 0.587*ag + 0.114*ab
		}
	}
	pixels := float64(diff.Pixels)
	diff.MeanAbsDiff = sumAbs / pixels
	diff.ChangedFraction = float64(diff.ChangedPixels) / pixels
	diff.LumaBefore = lumaBefore / pixels
	diff.LumaAfter = lumaAfter / pixels
	diff.LumaDelta = diff.LumaAfter - diff.LumaBefore
	return diff, nil
}

func maxInt3(a, b, c int) int {
	if b > a {
		a = b
	}
	if c > a {
		a = c
	}
	return a
}

func maxFloat3(a, b, c float64) float64 {
	if b > a {
		a = b
	}
	if c > a {
		a = c
	}
	return a
}

// Effect thresholds. They are fixed for this probe and are recorded in the
// evidence notes; the field round decides whether they need calibration.
const (
	// Notepad: a caret is at least a few pixels at any accepted scale. Eight
	// changed pixels in the edit area is well above capture noise and well
	// below a caret's footprint.
	NotepadPixelDelta       = 20
	NotepadMinChangedPixels = 8

	// Game cell border: a selection outline changes either enough pixels in
	// the border band or the band's mean luminance. Both numbers are lower
	// bounds chosen to tolerate a few pixels of anchor error.
	BorderPixelDelta       = 16
	BorderMinChangedPixels = 20
	BorderMinLumaDelta     = 6.0

	// Game detail name row: the task requires the region's MAD to change
	// clearly. The changed-fraction floor keeps a few isolated pixels from
	// satisfying the MAD alone.
	DetailPixelDelta         = 16
	DetailMADThreshold       = 2.0
	DetailMinChangedFraction = 0.005
)

// FrameEffect is the per-frame effect verdict. Only a real click run may use
// Effective; a dry run records the metrics with a "no click" reason.
type FrameEffect struct {
	Edit         *RegionDiff
	BorderLeft   *RegionDiff
	BorderBottom *RegionDiff
	Detail       *RegionDiff
	Effective    bool
	Reason       string
}

// EvaluateNotepadFrame decides whether the edit area changed after the click.
// A blinking caret can be off in one frame, so the caller evaluates every
// post-click frame and accepts any frame that shows a change.
func EvaluateNotepadFrame(before, after *windows.CapturedImage, plan NotepadPlan) (FrameEffect, error) {
	edit, err := DiffRegion(before, after, plan.EditRegion, NotepadPixelDelta)
	if err != nil {
		return FrameEffect{}, err
	}
	effect := FrameEffect{Edit: &edit}
	if edit.ChangedPixels >= NotepadMinChangedPixels {
		effect.Effective = true
		effect.Reason = fmt.Sprintf("edit area changed: %d pixels with a channel delta >= %d (threshold %d)",
			edit.ChangedPixels, NotepadPixelDelta, NotepadMinChangedPixels)
		return effect, nil
	}
	effect.Reason = fmt.Sprintf("edit area unchanged: %d changed pixels with a channel delta >= %d (threshold %d)",
		edit.ChangedPixels, NotepadPixelDelta, NotepadMinChangedPixels)
	return effect, nil
}

// EvaluateGameFrame decides whether the click moved the selection to the
// target cell and updated the detail name row in the same frame. Both
// conditions are required: a cell border change alone can be an animation, and
// a detail change alone can be unrelated content motion.
func EvaluateGameFrame(before, after *windows.CapturedImage, plan GamePlan) (FrameEffect, error) {
	left, err := DiffRegion(before, after, plan.BorderLeft, BorderPixelDelta)
	if err != nil {
		return FrameEffect{}, err
	}
	bottom, err := DiffRegion(before, after, plan.BorderBottom, BorderPixelDelta)
	if err != nil {
		return FrameEffect{}, err
	}
	detail, err := DiffRegion(before, after, plan.NameRow, DetailPixelDelta)
	if err != nil {
		return FrameEffect{}, err
	}
	borderChanged := borderBandChanged(left) || borderBandChanged(bottom)
	detailChanged := detail.MeanAbsDiff >= DetailMADThreshold && detail.ChangedFraction >= DetailMinChangedFraction
	effect := FrameEffect{
		BorderLeft:   &left,
		BorderBottom: &bottom,
		Detail:       &detail,
		Effective:    borderChanged && detailChanged,
	}
	switch {
	case borderChanged && detailChanged:
		effect.Reason = fmt.Sprintf("cell border changed and name-row MAD %.3f >= %.3f with changed fraction %.4f >= %.4f",
			detail.MeanAbsDiff, DetailMADThreshold, detail.ChangedFraction, DetailMinChangedFraction)
	case !borderChanged && !detailChanged:
		effect.Reason = fmt.Sprintf("cell border unchanged and name-row MAD %.3f < %.3f or changed fraction %.4f < %.4f",
			detail.MeanAbsDiff, DetailMADThreshold, detail.ChangedFraction, DetailMinChangedFraction)
	case !borderChanged:
		effect.Reason = fmt.Sprintf("cell border unchanged (left %d/%d px, bottom %d/%d px, luma deltas %.2f/%.2f)",
			left.ChangedPixels, BorderMinChangedPixels, bottom.ChangedPixels, BorderMinChangedPixels, left.LumaDelta, bottom.LumaDelta)
	default:
		effect.Reason = fmt.Sprintf("name-row MAD %.3f < %.3f or changed fraction %.4f < %.4f",
			detail.MeanAbsDiff, DetailMADThreshold, detail.ChangedFraction, DetailMinChangedFraction)
	}
	return effect, nil
}

func borderBandChanged(d RegionDiff) bool {
	return d.ChangedPixels >= BorderMinChangedPixels || math.Abs(d.LumaDelta) >= BorderMinLumaDelta
}

// Conclusion is the final verdict of one run.
type Conclusion string

const (
	ConclusionDeliveredEffective Conclusion = "delivered-and-effective"
	ConclusionDeliveredNoEffect  Conclusion = "delivered-no-effect"
	ConclusionRefusedByPrecheck  Conclusion = "refused-by-precheck"
	ConclusionError              Conclusion = "error"
	// ConclusionDryRunReady is the dry-run counterpart: every check passed
	// and the click was deliberately not sent. It is not a delivery verdict.
	ConclusionDryRunReady Conclusion = "dry-run-ready"
)

// Outcome is the classifier input: the facts of the run, without any claim
// about whether the game acted on the click.
type Outcome struct {
	DryRun            bool
	PreflightRefused  bool
	PreflightError    bool
	PreflightDetail   string
	PrecheckEvaluated bool
	PrecheckPassed    bool
	PrecheckError     bool
	PrecheckDetail    string

	DispatchAttempted bool
	DispatchState     windows.InputState
	EventsPlanned     int
	EventsInserted    int

	BeforeCaptured    bool
	PostFrames        int
	AnyEffectiveFrame bool
	Aborted           bool
	AbortReason       string
}

// ClassifyConclusion maps the run facts to the required conclusion labels.
// Delivery and effect are separate: a full SendInput insertion only reaches
// delivered-and-effective when a captured frame meets the effect test.
//
// A full click that was followed by an aborted observation is error, not
// delivered-no-effect, because the two-second window was not observed. If an
// effective frame was already captured before the abort, the delivery verdict
// stands and the abort is recorded separately.
func ClassifyConclusion(o Outcome) (Conclusion, string) {
	switch {
	case o.PreflightError:
		return ConclusionError, "preflight could not be completed: " + o.PreflightDetail
	case o.PreflightRefused:
		return ConclusionRefusedByPrecheck, "preflight refused the click: " + o.PreflightDetail
	case o.PrecheckError:
		return ConclusionError, "the platform precheck failed: " + o.PrecheckDetail
	case o.PrecheckEvaluated && !o.PrecheckPassed:
		return ConclusionRefusedByPrecheck, "the platform precheck refused the click: " + o.PrecheckDetail
	case !o.BeforeCaptured:
		return ConclusionError, "no valid pre-click frame was captured"
	case o.DryRun && o.Aborted:
		return ConclusionError, "dry run aborted: " + o.AbortReason
	case o.DryRun && !o.PrecheckEvaluated:
		return ConclusionError, "dry run did not evaluate the platform precheck"
	case o.DryRun:
		return ConclusionDryRunReady, "all preflight and platform precheck checks passed; SendInput was suppressed by -dry-run"
	case !o.DispatchAttempted:
		return ConclusionError, "no dispatch was attempted"
	}
	switch o.DispatchState {
	case windows.StateDelivered:
		if o.EventsPlanned <= 0 || o.EventsInserted < o.EventsPlanned {
			return ConclusionError, fmt.Sprintf("delivered state is inconsistent: %d of %d events inserted", o.EventsInserted, o.EventsPlanned)
		}
		if o.AnyEffectiveFrame {
			detail := "SendInput inserted the full click and at least one post-click frame met the effect test"
			if o.Aborted {
				detail += "; the observation later aborted: " + o.AbortReason
			}
			return ConclusionDeliveredEffective, detail
		}
		if o.Aborted {
			return ConclusionError, "the click was inserted but frame observation stopped early: " + o.AbortReason
		}
		if o.PostFrames == 0 {
			return ConclusionError, "the click was inserted but no post-click frame was captured"
		}
		return ConclusionDeliveredNoEffect, fmt.Sprintf("SendInput inserted the full click but none of %d post-click frames met the effect test", o.PostFrames)
	case windows.StateBlocked:
		return ConclusionError, "SendInput inserted no events and the system reported access denial (likely UIPI); the probe did not bypass it"
	case windows.StateFailed:
		return ConclusionError, "SendInput failed before inserting the click"
	case windows.StatePartialDelivered:
		return ConclusionError, fmt.Sprintf("only %d of %d click events were inserted", o.EventsInserted, o.EventsPlanned)
	case windows.StateNotDispatched:
		return ConclusionError, "the click was refused after the preflight without a recorded platform precheck refusal"
	default:
		return ConclusionError, "unknown dispatch state " + string(o.DispatchState)
	}
}

// NotepadSelectionError reports why the Notepad target could not be locked.
type NotepadSelectionError struct {
	Kind       string // "noWindow" or "ambiguous"
	Hint       string
	Candidates []windows.WindowProbe
}

func (e *NotepadSelectionError) Error() string {
	if e.Kind == "ambiguous" {
		return fmt.Sprintf("%d windows match Notepad (%s); close all but one or pass -notepad-title to narrow the title", len(e.Candidates), e.Hint)
	}
	return fmt.Sprintf("no Notepad window matches %s; open Notepad manually (this probe never starts it)", e.Hint)
}

// NotepadMatch is one candidate criterion description used in evidence.
func NotepadMatch(hint string) string {
	if strings.TrimSpace(hint) != "" {
		return fmt.Sprintf("title contains %q", hint)
	}
	return "executable base name is notepad.exe"
}

// SelectNotepad locks exactly one Notepad window. With a title hint it matches
// titles; otherwise it matches the executable base name case-insensitively.
// Every match counts, including hidden or minimized windows: a second Notepad
// window makes the target ambiguous instead of silently choosing the visible
// one.
func SelectNotepad(probes []windows.WindowProbe, titleHint string) (windows.WindowProbe, error) {
	matches := MatchNotepad(probes, titleHint)
	hint := NotepadMatch(titleHint)
	switch len(matches) {
	case 0:
		return windows.WindowProbe{}, &NotepadSelectionError{Kind: "noWindow", Hint: hint}
	case 1:
		return matches[0], nil
	default:
		return windows.WindowProbe{}, &NotepadSelectionError{Kind: "ambiguous", Hint: hint, Candidates: matches}
	}
}

// MatchNotepad returns every probe matching the Notepad criterion.
func MatchNotepad(probes []windows.WindowProbe, titleHint string) []windows.WindowProbe {
	hint := strings.TrimSpace(titleHint)
	matches := make([]windows.WindowProbe, 0, 1)
	for _, probe := range probes {
		if hint != "" {
			if strings.Contains(strings.ToLower(probe.Title), strings.ToLower(hint)) {
				matches = append(matches, probe)
			}
			continue
		}
		if strings.EqualFold(executableBaseName(probe.ImagePath), "notepad.exe") {
			matches = append(matches, probe)
		}
	}
	return matches
}

// executableBaseName splits on both separators so the pure logic behaves the
// same in macOS unit tests and on Windows.
func executableBaseName(path string) string {
	normalized := strings.ReplaceAll(path, "\\", "/")
	if i := strings.LastIndex(normalized, "/"); i >= 0 {
		return normalized[i+1:]
	}
	return normalized
}

// sameWindowsPath compares Windows paths case-insensitively with either
// separator, independent of the host running the unit tests.
func sameWindowsPath(a, b string) bool {
	normalize := func(p string) string {
		p = strings.ReplaceAll(p, "\\", "/")
		return strings.ToLower(strings.TrimRight(p, "/"))
	}
	return normalize(a) == normalize(b)
}

// MatchGameTargets returns the top-level probes whose image path is the game
// executable, for evidence and for the path-normalization fallback.
func MatchGameTargets(probes []windows.WindowProbe, executable string) []windows.WindowProbe {
	matches := make([]windows.WindowProbe, 0, 1)
	for _, probe := range probes {
		if sameWindowsPath(probe.ImagePath, executable) {
			matches = append(matches, probe)
		}
	}
	return matches
}

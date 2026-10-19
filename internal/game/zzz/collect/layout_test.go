package collect_test

import (
	"image"
	"testing"

	"github.com/LoTwT/youkai/internal/game/zzz/collect"
	"github.com/LoTwT/youkai/internal/platform"
)

// Layout validation and the pixel matcher's contract: the macOS cloud
// layout is structurally complete and exactly identifies its display
// configuration; an incomplete layout or an uncalibrated matcher is
// refused, never guessed around.

func TestMacCloudLayoutValidatesAndFitsContent(t *testing.T) {
	l := collect.MacCloudLayout()
	if err := l.Validate(); err != nil {
		t.Fatalf("the calibrated layout must validate: %v", err)
	}
	if !l.FitsContent(l.Content) {
		t.Fatalf("every calibrated region must fit the content region")
	}
	if l.Image != image.Pt(1920, 1080) {
		t.Fatalf("image size %v, want 1920x1080", l.Image)
	}
	if l.Content != image.Rect(0, 0, 1650, 1080) {
		t.Fatalf("content region %v, want the left 1650x1080", l.Content)
	}
	if l.Rows != 5 || l.Columns != 4 || len(l.Slots) != 20 {
		t.Fatalf("grid %d×%d with %d slots, want 5×4 with 20", l.Rows, l.Columns, len(l.Slots))
	}
	if l.PanelS != image.Pt(1225, 615) {
		t.Fatalf("panel S control %v, want the sample frame's (1225,615)", l.PanelS)
	}
	if l.Cell.Management.Empty() || l.Cell.Avatar.Empty() ||
		l.DetailAvatar.Empty() || l.TopManagement.Empty() {
		t.Fatalf("management/avatar probes must be calibrated: %+v", l)
	}
	if m := collect.NewPixelMatcher(l, collect.MacCloudCalibration()); m == nil {
		t.Fatal("the matcher must construct")
	}
}

func TestMacCloudLayoutMatchesFrameExactly(t *testing.T) {
	l := collect.MacCloudLayout()
	if !l.MatchesFrame(image.Pt(1920, 1080), image.Rect(0, 0, 1650, 1080)) {
		t.Fatal("the calibrated size and content must match")
	}
	if l.MatchesFrame(image.Pt(1920, 1081), image.Rect(0, 0, 1650, 1080)) {
		t.Fatal("a different image size is a different display configuration")
	}
	if l.MatchesFrame(image.Pt(1920, 1080), image.Rect(0, 0, 1600, 1080)) {
		t.Fatal("a different content region is a different display configuration")
	}
}

func TestLayoutValidateRefusesBroken(t *testing.T) {
	if err := (collect.Layout{}).Validate(); err == nil {
		t.Fatal("the zero-value layout must be refused")
	}
	cases := []struct {
		name   string
		mutate func(*collect.Layout)
	}{
		{"no image", func(l *collect.Layout) { l.Image = image.Point{} }},
		{"content outside image", func(l *collect.Layout) { l.Content = image.Rect(0, 0, 2000, 1080) }},
		{"no grid", func(l *collect.Layout) { l.Rows, l.Columns = 0, 0 }},
		{"slots do not add up", func(l *collect.Layout) { l.Slots = l.Slots[:19] }},
		{"empty slot", func(l *collect.Layout) { l.Slots[0] = image.Rect(0, 0, 0, 0) }},
		{"empty border probe", func(l *collect.Layout) { l.Cell.Border = image.Rectangle{} }},
		{"empty bar probe", func(l *collect.Layout) { l.Cell.Bar = image.Rectangle{} }},
		{"empty management probe", func(l *collect.Layout) { l.Cell.Management = image.Rectangle{} }},
		{"empty avatar probe", func(l *collect.Layout) { l.Cell.Avatar = image.Rectangle{} }},
		{"zero row pitch", func(l *collect.Layout) { l.RowPitch = 0 }},
		{"empty viewport", func(l *collect.Layout) { l.ListViewport = image.Rectangle{} }},
		{"empty scroll track", func(l *collect.Layout) { l.ScrollTrack = image.Rectangle{} }},
		{"scroll drag goes nowhere", func(l *collect.Layout) { l.ScrollTo = l.ScrollFrom }},
		{"zero scroll duration", func(l *collect.Layout) { l.ScrollDuration = 0 }},
		{"top drag goes nowhere", func(l *collect.Layout) { l.TopTo = l.TopFrom }},
		{"zero top duration", func(l *collect.Layout) { l.TopDuration = 0 }},
		{"funnel without colors", func(l *collect.Layout) { l.Funnel.Colors = nil }},
		{"one panel probe", func(l *collect.Layout) { l.PanelOpen = l.PanelOpen[:1] }},
		{"panel probe without colors", func(l *collect.Layout) { l.PanelOpen[0].Colors = nil }},
		{"no wheel positions", func(l *collect.Layout) { l.Wheel = [6]image.Point{} }},
		{"no wheel probes", func(l *collect.Layout) { l.WheelArcs = nil }},
		{"empty wheel probe", func(l *collect.Layout) { l.WheelArcs[0] = image.Rectangle{} }},
		{"no detail lines", func(l *collect.Layout) { l.DetailLines = nil }},
		{"detail line without role", func(l *collect.Layout) { l.DetailLines[0].Role = "" }},
		{"empty detail line", func(l *collect.Layout) { l.DetailLines[0].Rect = image.Rectangle{} }},
		{"empty quality badge", func(l *collect.Layout) { l.QualityBadge = image.Rectangle{} }},
		{"empty detail avatar", func(l *collect.Layout) { l.DetailAvatar = image.Rectangle{} }},
		{"empty top management", func(l *collect.Layout) { l.TopManagement = image.Rectangle{} }},
		{"empty substat-4 probe", func(l *collect.Layout) { l.Substat4Probe = image.Rectangle{} }},
		{"missing S control", func(l *collect.Layout) { l.PanelS = image.Point{} }},
		{"S control outside content", func(l *collect.Layout) { l.PanelS = image.Pt(1700, 615) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := collect.MacCloudLayout()
			tc.mutate(&l)
			if err := l.Validate(); err == nil {
				t.Fatalf("the mutated layout must be refused")
			}
		})
	}
}

func TestLayoutFitsContent(t *testing.T) {
	l := collect.MacCloudLayout()
	content := l.Content

	outside := l
	outside.Slots = append([]image.Rectangle(nil), l.Slots...)
	outside.Slots[0] = image.Rect(1600, 0, 1700, 60)
	if outside.FitsContent(content) {
		t.Fatal("a slot outside the content region must not fit")
	}

	wide := l
	wide.WheelArcs = append([]image.Rectangle(nil), l.WheelArcs...)
	wide.WheelArcs[0] = image.Rect(-1000, 0, -900, 10)
	if wide.FitsContent(content) {
		t.Fatal("a wheel probe outside the content region must not fit")
	}

	below := l
	below.DetailLines = append([]collect.LineRegion(nil), l.DetailLines...)
	below.DetailLines[0].Rect = image.Rect(526, 1000, 706, 1100)
	if below.FitsContent(content) {
		t.Fatalf("a detail line below the content region must not fit")
	}

	outsideDetailAvatar := l
	outsideDetailAvatar.DetailAvatar = image.Rect(1600, 250, 1700, 294)
	if outsideDetailAvatar.FitsContent(content) {
		t.Fatal("a detail avatar outside the content region must not fit")
	}

	outsideTop := l
	outsideTop.TopManagement = image.Rect(1600, 118, 1700, 166)
	if outsideTop.FitsContent(content) {
		t.Fatal("a top management indicator outside the content region must not fit")
	}

	outsideCell := l
	outsideCell.Cell.Management = image.Rect(1200, 4, 1230, 34)
	if outsideCell.FitsContent(content) {
		t.Fatal("a per-cell badge outside the content region must not fit")
	}

	outsideS := l
	outsideS.PanelS = image.Pt(1700, 615)
	if outsideS.FitsContent(content) {
		t.Fatal("an S control outside the content region must not fit")
	}
}

func TestMacCloudCalibrationRejectsUnorderedTiers(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*collect.Calibration)
	}{
		{"bars overlap", func(c *collect.Calibration) { c.BarOccupiedMin = c.BarEmptyMax }},
		{"selection overlap", func(c *collect.Calibration) { c.SelectionHitMin = c.SelectionClearMax }},
		{"wheel overlap", func(c *collect.Calibration) { c.WheelSelectedMin = c.WheelOtherMax }},
		{"detail bands overlap", func(c *collect.Calibration) { c.DetailDifferentMin = c.DetailSameMax }},
		{"substat bands overlap", func(c *collect.Calibration) { c.Substat4InkMin = c.Substat3InkMax }},
		{"management bands overlap", func(c *collect.Calibration) { c.ManagementLockRingMin = c.ManagementClearRingMax }},
		{"management red bands overlap", func(c *collect.Calibration) { c.ManagementDiscardRedMin = c.ManagementClearRedMax }},
		{"avatar ring missing", func(c *collect.Calibration) { c.AvatarRingMin = 0 }},
		{"avatar sectors out of range", func(c *collect.Calibration) { c.AvatarRingSectorsMin = 9 }},
		{"avatar core bands overlap", func(c *collect.Calibration) { c.AvatarCoreMeanMin = c.AvatarAbsentCoreMeanMax }},
		{"top management bands overlap", func(c *collect.Calibration) { c.TopLockedDimMin = c.TopUnlockedDimMax }},
		{"scroll thresholds unordered", func(c *collect.Calibration) { c.ScrollDisplacedMax = c.ScrollStationaryMax }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := collect.MacCloudCalibration()
			tc.mutate(&c)
			l := collect.MacCloudLayout()
			m := collect.NewPixelMatcher(l, c)
			f := platform.Frame{
				ID:      1,
				Image:   image.NewRGBA(image.Rect(0, 0, 1920, 1080)),
				Content: l.Content,
			}
			if pm := m.Match(f); pm.Page != collect.PageUnknown {
				t.Fatalf("an unordered threshold set must make every verdict unknown, got %s", pm.Page)
			}
			if sm := m.MatchScroll(f, f); sm.Displaced || sm.Stationary || sm.Lost {
				t.Fatalf("an unordered threshold set must claim no scroll verdict: %+v", sm)
			}
		})
	}
}

func TestUncalibratedMatcherReportsUndecidable(t *testing.T) {
	m := collect.Uncalibrated{}
	frame := platform.Frame{ID: 7, Geometry: 1}
	pm := m.Match(frame)
	if pm.Page != collect.PageUnknown || pm.List != nil || pm.Detail != nil {
		t.Fatalf("an uncalibrated match must be unknown without evidence: %+v", pm)
	}
	sm := m.MatchScroll(frame, platform.Frame{ID: 8, Geometry: 1})
	if sm.Displaced || sm.Stationary || sm.Lost || len(sm.Matches) != 0 {
		t.Fatalf("an uncalibrated scroll verdict must claim nothing: %+v", sm)
	}
	if c := m.CompareDetail(frame, frame); c != collect.DetailUndecided {
		t.Fatalf("an uncalibrated detail comparison must be undecided, got %v", c)
	}
}

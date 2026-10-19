// Package agentavatars identifies the equipper of a drive disc from the
// game's circular agent avatars. The fairy catalog provides 57
// IconRoleCircle images for its 60 agents (1381, 1531 and 1551 have no
// partnerInfo.iconPath); those images are embedded here so matching never
// touches the network.
//
// The matcher is deliberately conservative. It compares the detail-pane
// avatar (the primary evidence) and the list cell's top-right avatar (the
// confirmation) against every embedded image, accepts only a clear
// top-1 with a margin over the runner-up, and reports no id otherwise.
// Two upstream images (1581 Remielle and 1621 Roxy) are byte-identical;
// they can therefore never be told apart and always resolve to unknown.
//
// Real-frame feasibility is documented in README.md: on the one frame
// with an independently known equipper (s2-70-unlocked-equipped, Yixuan
// 1371) no embedded image reaches the acceptance threshold, so production
// scans keep agentId null until the asset gap is resolved. The synthetic
// tests below prove the matching machinery itself is sound.
package agentavatars

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/binary"
	"fmt"
	"image"
	"math"
	"sort"
	"sync"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/webp"

	"github.com/LoTwT/youkai/internal/game/zzz"
)

//go:embed assets/*.webp
var embedded embed.FS

// Matching normalization and acceptance thresholds. Both avatars are
// reduced to a 32x32 grayscale/RGB sample; a circular mask of radius 13
// excludes the game's outer ring and the transparent corners of the
// source image. A ±1-pixel alignment search absorbs the one-pixel jitter
// of the crop's brightness-based circle detection.
const (
	// MatchSize is the square both avatars and every embedded image are
	// normalized to.
	MatchSize = 32
	// MaskRadius is the radius of the compared disc inside MatchSize.
	MaskRadius = 13.0
	// MinScore is the lowest combined detail/list correlation accepted as
	// a reliable identity. Measured synthetic matches (asset scaled into
	// the real crop geometry with ±3-level noise) score at least 0.979;
	// the known real frame's best candidate scores 0.457.
	MinScore = 0.90
	// MinMargin is the required gap between the best and second-best
	// combined scores. Synthetic matches keep a gap of at least 0.19;
	// the known real frame's gap is 0.013.
	MinMargin = 0.08
	// MinSideScore requires both the detail and the list avatar to carry
	// their own evidence, not just the average.
	MinSideScore = 0.88
	// alignRadius is the integer pixel search around the detected circle.
	alignRadius = 1
	// assetDir is the embedded resource directory.
	assetDir = "assets"
	// assetSize is the native size of every fairy IconRoleCircle image.
	assetSize = 142
)

// Result is one deterministic identity verdict. AgentID is 0 whenever the
// evidence is absent, ambiguous or below threshold; callers must write a
// null agentId in that case and never substitute a runner-up.
type Result struct {
	AgentID     int
	Score       float64
	DetailScore float64
	ListScore   float64
	Second      float64
	Margin      float64
	Evidence    string
}

// Matcher holds the normalized embedded assets. It is immutable after
// construction and safe for concurrent use.
type Matcher struct {
	assets     []asset
	duplicates map[int]int
}

// asset is one embedded avatar reduced to the comparison grid.
type asset struct {
	id   int
	name string
	gray []float32
	rgb  [3][]float32
	// mean and norm are precomputed over the circular mask.
	mean [4]float64
	norm [4]float64
}

// scored is one candidate's correlation evidence.
type scored struct {
	id       int
	detail   float64
	list     float64
	combined float64
}

// New loads and normalizes every embedded avatar. It fails when an
// embedded file is missing or cannot be decoded, because that would make
// the matcher silently incomplete.
func New() (*Matcher, error) {
	m := &Matcher{duplicates: make(map[int]int)}
	seen := make(map[[32]byte]int)
	for _, agent := range zzz.Agents() {
		if agent.Avatar == "" {
			continue
		}
		data, err := embedded.ReadFile(assetDir + "/" + agent.Avatar + ".webp")
		if err != nil {
			return nil, fmt.Errorf("agentavatars: read %s for agent %d: %w", agent.Avatar, agent.ID, err)
		}
		src, err := webp.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("agentavatars: decode %s for agent %d: %w", agent.Avatar, agent.ID, err)
		}
		if src.Bounds().Dx() != assetSize || src.Bounds().Dy() != assetSize {
			return nil, fmt.Errorf("agentavatars: %s is %v, want %dx%d", agent.Avatar, src.Bounds(), assetSize, assetSize)
		}
		a, err := normalizeAsset(agent.ID, agent.Avatar, src)
		if err != nil {
			return nil, err
		}
		m.assets = append(m.assets, a)
		sum := sha256.Sum256(a.bytes())
		if previous, ok := seen[sum]; ok {
			m.duplicates[agent.ID] = previous
			m.duplicates[previous] = agent.ID
			continue
		}
		seen[sum] = agent.ID
	}
	if len(m.assets) != 57 {
		return nil, fmt.Errorf("agentavatars: %d embedded avatars, want 57", len(m.assets))
	}
	return m, nil
}

// Match identifies the equipper from the detail and list avatar crops.
// Both crops are required: the detail avatar is primary evidence and the
// list avatar confirms it. A nil, unreadable or ambiguous pair returns
// AgentID 0 with the evidence in Result.Evidence.
func (m *Matcher) Match(detail, list image.Image) Result {
	if m == nil || len(m.assets) == 0 {
		return Result{Evidence: "agent avatar match unavailable: the matcher carries no assets"}
	}
	ds, ok := normalizeCrop(detail)
	if !ok {
		return Result{Evidence: "agent avatar match unavailable: the detail avatar crop is unusable"}
	}
	ls, ok := normalizeCrop(list)
	if !ok {
		return Result{Evidence: "agent avatar match unavailable: the list avatar crop is unusable"}
	}

	scores := make([]scored, 0, len(m.assets))
	for _, a := range m.assets {
		dsScore := a.score(ds)
		lsScore := a.score(ls)
		scores = append(scores, scored{
			id:       a.id,
			detail:   dsScore,
			list:     lsScore,
			combined: (dsScore + lsScore) / 2,
		})
	}
	sort.Slice(scores, func(i, j int) bool {
		if scores[i].combined != scores[j].combined {
			return scores[i].combined > scores[j].combined
		}
		return scores[i].id < scores[j].id
	})
	top := scores[0]
	second := scored{}
	if len(scores) > 1 {
		second = scores[1]
	}
	res := Result{
		Score:       top.combined,
		DetailScore: top.detail,
		ListScore:   top.list,
		Second:      second.combined,
		Margin:      top.combined - second.combined,
	}
	if pair, ok := m.duplicates[top.id]; ok {
		res.Evidence = fmt.Sprintf(
			"agent avatar match unavailable: agents %d and %d embed identical portraits (both score %.3f), so the identity is ambiguous",
			top.id, pair, top.combined)
		return res
	}
	detailBest := bestID(scores, func(s scored) float64 { return s.detail })
	listBest := bestID(scores, func(s scored) float64 { return s.list })
	reason := ""
	switch {
	case top.combined < MinScore:
		reason = fmt.Sprintf("the best candidate %d scores %.3f < %.3f", top.id, top.combined, MinScore)
	case top.detail < MinSideScore || top.list < MinSideScore:
		reason = fmt.Sprintf("candidate %d's detail/list scores %.3f/%.3f are below %.3f",
			top.id, top.detail, top.list, MinSideScore)
	case detailBest != top.id || listBest != top.id:
		reason = fmt.Sprintf("the detail avatar prefers %d and the list avatar prefers %d", detailBest, listBest)
	case res.Margin < MinMargin:
		reason = fmt.Sprintf("the top two candidates %d and %d differ by %.3f < %.3f",
			top.id, second.id, res.Margin, MinMargin)
	}
	if reason != "" {
		res.Evidence = fmt.Sprintf(
			"agent avatar match unavailable: %s (best %d, detail %.3f, list %.3f, second %d %.3f)",
			reason, top.id, top.detail, top.list, second.id, second.combined)
		return res
	}
	res.AgentID = top.id
	res.Evidence = fmt.Sprintf(
		"agent avatar match: agent %d, detail %.3f, list %.3f, combined %.3f, second %d %.3f, margin %.3f",
		top.id, top.detail, top.list, top.combined, second.id, second.combined, res.Margin)
	return res
}

// defaultMatcher is the process-wide matcher. The assets are immutable, so
// one decode is enough; a load failure stays a reported unknown rather
// than stopping a scan.
var (
	defaultOnce sync.Once
	defaultM    *Matcher
	defaultErr  error
)

// Match identifies the equipper using the process-wide embedded matcher.
func Match(detail, list image.Image) Result {
	defaultOnce.Do(func() {
		defaultM, defaultErr = New()
	})
	if defaultErr != nil {
		return Result{Evidence: "agent avatar match unavailable: " + defaultErr.Error()}
	}
	return defaultM.Match(detail, list)
}

// HasAvatar reports whether the catalog associates an embedded avatar with
// the fairy numeric agent id.
func HasAvatar(id int) bool {
	agent, ok := zzz.LookupAgentByID(id)
	return ok && agent.Avatar != ""
}

// AvatarImage decodes one embedded avatar at its native size. It exists
// for tests and diagnostics; matching itself uses the normalized samples.
func AvatarImage(id int) (image.Image, bool) {
	agent, ok := zzz.LookupAgentByID(id)
	if !ok || agent.Avatar == "" {
		return nil, false
	}
	data, err := embedded.ReadFile(assetDir + "/" + agent.Avatar + ".webp")
	if err != nil {
		return nil, false
	}
	img, err := webp.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, false
	}
	return img, true
}

// EmbeddedAssetCount reports how many avatar files are embedded.
func EmbeddedAssetCount() int {
	entries, err := embedded.ReadDir(assetDir)
	if err != nil {
		return 0
	}
	return len(entries)
}

// normalizeAsset reduces one native avatar to the comparison grid.
func normalizeAsset(id int, name string, src image.Image) (asset, error) {
	rgba := image.NewRGBA(image.Rect(0, 0, MatchSize, MatchSize))
	xdraw.CatmullRom.Scale(rgba, rgba.Bounds(), src, src.Bounds(), xdraw.Src, nil)
	a := asset{id: id, name: name}
	a.gray, a.rgb = channels(rgba)
	a.mean, a.norm = moments(a)
	return a, nil
}

// normalizeCrop detects the game avatar inside its crop, scales the
// detected square to MatchSize and pads it by alignRadius so the comparison
// can search ±alignRadius pixels.
func normalizeCrop(img image.Image) (*sample, bool) {
	if img == nil || img.Bounds().Empty() {
		return nil, false
	}
	rgba := toRGBA(img)
	sq, ok := circleBounds(rgba)
	if !ok {
		return nil, false
	}
	scaled := image.NewRGBA(image.Rect(0, 0, MatchSize, MatchSize))
	xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), rgba, sq, xdraw.Src, nil)
	s := &sample{size: MatchSize + 2*alignRadius}
	s.gray, s.rgb = channelsPadded(scaled, alignRadius)
	return s, true
}

// circleBounds finds the avatar's bright circular sprite inside a crop and
// returns the smallest square around it. The crop background is the dark
// UI around the avatar; a threshold relative to the border's median
// brightness keeps the detection stable on both bright and dark portraits.
func circleBounds(img *image.RGBA) (image.Rectangle, bool) {
	b := img.Bounds()
	if b.Dx() < 8 || b.Dy() < 8 {
		return image.Rectangle{}, false
	}
	gray := make([]float64, 0, b.Dx()*b.Dy())
	border := make([]float64, 0, 2*(b.Dx()+b.Dy()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			v := (float64(r) + float64(g) + float64(bl)) / 3 / 257
			gray = append(gray, v)
			if x == b.Min.X || x == b.Max.X-1 || y == b.Min.Y || y == b.Max.Y-1 {
				border = append(border, v)
			}
		}
	}
	sort.Float64s(border)
	threshold := math.Max(25, math.Min(90, border[len(border)/2]+30))
	minX, minY, maxX, maxY := b.Max.X, b.Max.Y, b.Min.X-1, b.Min.Y-1
	bright := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if gray[(y-b.Min.Y)*b.Dx()+(x-b.Min.X)] <= threshold {
				continue
			}
			bright++
			if x < minX {
				minX = x
			}
			if x > maxX {
				maxX = x
			}
			if y < minY {
				minY = y
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	if bright < (b.Dx()*b.Dy())/10 || maxX < minX || maxY < minY {
		return b, true
	}
	w, h := maxX-minX+1, maxY-minY+1
	if w < 4 || h < 4 {
		return b, true
	}
	short, long := w, h
	if short > long {
		short, long = long, short
	}
	if float64(short) < 0.4*float64(minInt(b.Dx(), b.Dy())) || float64(long) > 1.5*float64(short) {
		return b, true
	}
	side := long
	cx := (minX + maxX + 1) / 2
	cy := (minY + maxY + 1) / 2
	x0 := cx - side/2
	y0 := cy - side/2
	if x0 < b.Min.X {
		x0 = b.Min.X
	}
	if y0 < b.Min.Y {
		y0 = b.Min.Y
	}
	if x0+side > b.Max.X {
		x0 = b.Max.X - side
	}
	if y0+side > b.Max.Y {
		y0 = b.Max.Y - side
	}
	sq := image.Rect(x0, y0, x0+side, y0+side)
	if sq.Dx() < b.Dx()/2 || sq.Dy() < b.Dy()/2 {
		return b, true
	}
	return sq, true
}

// sample is one normalized crop padded by alignRadius on every side.
type sample struct {
	size int
	gray []float32
	rgb  [3][]float32
}

// score compares the asset against the crop's ±alignRadius alignments and
// returns the best normalized cross-correlation. The score averages the
// grayscale and per-channel correlations so a brightness-only grade
// change cannot swing the verdict, while color still separates similar
// silhouettes.
func (a asset) score(s *sample) float64 {
	best := -2.0
	for dy := -alignRadius; dy <= alignRadius; dy++ {
		for dx := -alignRadius; dx <= alignRadius; dx++ {
			g := correlate(a.gray, a.mean[0], a.norm[0], s.gray, s.size, dx, dy)
			v := g
			for c := 0; c < 3; c++ {
				v += correlate(a.rgb[c], a.mean[c+1], a.norm[c+1], s.rgb[c], s.size, dx, dy)
			}
			v /= 4
			if v > best {
				best = v
			}
		}
	}
	return best
}

// correlate computes NCC between a 32x32 asset channel and the 32x32 window
// of a padded sample. The asset's mean/norm are precomputed over the mask;
// the window's are computed here.
func correlate(asset []float32, meanA, stdA float64, padded []float32, size int, dx, dy int) float64 {
	offX, offY := alignRadius+dx, alignRadius+dy
	var sum, sum2, dot float64
	n := 0
	for y := 0; y < MatchSize; y++ {
		for x := 0; x < MatchSize; x++ {
			if !maskPixel(x, y) {
				continue
			}
			b := float64(padded[(y+offY)*size+offX+x])
			sum += b
			sum2 += b * b
			dot += float64(asset[y*MatchSize+x]) * b
			n++
		}
	}
	if n == 0 || stdA <= 0 {
		return 0
	}
	meanB := sum / float64(n)
	varB := sum2/float64(n) - meanB*meanB
	if varB <= 0 {
		return 0
	}
	cov := dot/float64(n) - meanA*meanB
	return cov / (stdA * math.Sqrt(varB))
}

// maskPixel reports whether (x,y) lies inside the comparison disc.
func maskPixel(x, y int) bool {
	dx, dy := float64(x)-15.5, float64(y)-15.5
	return dx*dx+dy*dy <= MaskRadius*MaskRadius
}

// moments computes the masked mean and standard deviation of each channel.
func moments(a asset) ([4]float64, [4]float64) {
	var mean, norm [4]float64
	var n int
	var sums [4]float64
	for y := 0; y < MatchSize; y++ {
		for x := 0; x < MatchSize; x++ {
			if !maskPixel(x, y) {
				continue
			}
			i := y*MatchSize + x
			sums[0] += float64(a.gray[i])
			for c := 0; c < 3; c++ {
				sums[c+1] += float64(a.rgb[c][i])
			}
			n++
		}
	}
	if n == 0 {
		return mean, norm
	}
	for c := range mean {
		mean[c] = sums[c] / float64(n)
	}
	var vars [4]float64
	for y := 0; y < MatchSize; y++ {
		for x := 0; x < MatchSize; x++ {
			if !maskPixel(x, y) {
				continue
			}
			i := y*MatchSize + x
			d := float64(a.gray[i]) - mean[0]
			vars[0] += d * d
			for c := 0; c < 3; c++ {
				d := float64(a.rgb[c][i]) - mean[c+1]
				vars[c+1] += d * d
			}
		}
	}
	for c := range norm {
		norm[c] = math.Sqrt(vars[c] / float64(n))
	}
	return mean, norm
}

// channels reduces an RGBA image of MatchSize to the grayscale and RGB
// comparison samples.
func channels(img *image.RGBA) ([]float32, [3][]float32) {
	var rgb [3][]float32
	for c := 0; c < 3; c++ {
		rgb[c] = make([]float32, MatchSize*MatchSize)
	}
	gray := make([]float32, MatchSize*MatchSize)
	for y := 0; y < MatchSize; y++ {
		for x := 0; x < MatchSize; x++ {
			i := img.PixOffset(x, y)
			r, g, b := img.Pix[i], img.Pix[i+1], img.Pix[i+2]
			gray[y*MatchSize+x] = float32(int(r)+int(g)+int(b)) / 3
			rgb[0][y*MatchSize+x] = float32(r)
			rgb[1][y*MatchSize+x] = float32(g)
			rgb[2][y*MatchSize+x] = float32(b)
		}
	}
	return gray, rgb
}

// channelsPadded builds the padded comparison sample used by the alignment
// search.
func channelsPadded(img *image.RGBA, pad int) ([]float32, [3][]float32) {
	size := MatchSize + 2*pad
	gray := make([]float32, size*size)
	var rgb [3][]float32
	for c := 0; c < 3; c++ {
		rgb[c] = make([]float32, size*size)
	}
	for y := 0; y < MatchSize; y++ {
		for x := 0; x < MatchSize; x++ {
			i := img.PixOffset(x, y)
			r, g, b := img.Pix[i], img.Pix[i+1], img.Pix[i+2]
			j := (y+pad)*size + x + pad
			gray[j] = float32(int(r)+int(g)+int(b)) / 3
			rgb[0][j] = float32(r)
			rgb[1][j] = float32(g)
			rgb[2][j] = float32(b)
		}
	}
	return gray, rgb
}

// bytes serializes the normalized samples for duplicate detection.
func (a asset) bytes() []byte {
	out := make([]byte, 0, 4*MatchSize*MatchSize*4)
	var buf [4]byte
	appendValue := func(v float32) {
		binary.LittleEndian.PutUint32(buf[:], math.Float32bits(v))
		out = append(out, buf[:]...)
	}
	for _, v := range a.gray {
		appendValue(v)
	}
	for c := 0; c < 3; c++ {
		for _, v := range a.rgb[c] {
			appendValue(v)
		}
	}
	return out
}

func toRGBA(img image.Image) *image.RGBA {
	if r, ok := img.(*image.RGBA); ok {
		return r
	}
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	xdraw.Copy(dst, dst.Bounds().Min, img, b, xdraw.Src, nil)
	return dst
}

func bestID(scores []scored, value func(scored) float64) int {
	best := scores[0].id
	bestScore := value(scores[0])
	for _, s := range scores[1:] {
		if v := value(s); v > bestScore {
			best, bestScore = s.id, v
		}
	}
	return best
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

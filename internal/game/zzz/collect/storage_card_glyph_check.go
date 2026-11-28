package collect

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
)

const (
	storageGlyphVersion = "9317230e47c7103a49849489278dd6a87016f417ab2803540d93ff9b8eb44110"
	storageGlyphDigest  = "d9f501ab861d1552f7bb08eecbd3ffc8ee635bb6d52f782f06040cdf1095f72c"
)

type StorageGlyphIdentity struct {
	Version string `json:"version"`
	Digest  string `json:"sha256"`
}

// VerifiedStorageCardGlyphModel checks the parameters linked into this build.
func VerifiedStorageCardGlyphModel() (StorageGlyphIdentity, error) {
	return verifyStorageCardGlyphModel(storageCardGlyphLDA)
}

func verifyStorageCardGlyphModel(model cardGlyphLDA) (StorageGlyphIdentity, error) {
	if !model.valid() || model.Version != storageGlyphVersion {
		return StorageGlyphIdentity{}, errors.New("collect: the Windows 5/6 badge model is missing, invalid, or has an unexpected version")
	}
	var data [(cardGlyphFeatureLen + 2) * 8]byte
	for i, weight := range model.Weights {
		binary.LittleEndian.PutUint64(data[i*8:], math.Float64bits(weight))
	}
	binary.LittleEndian.PutUint64(data[cardGlyphFeatureLen*8:], math.Float64bits(model.Bias))
	binary.LittleEndian.PutUint64(data[(cardGlyphFeatureLen+1)*8:], math.Float64bits(model.MinScore))
	sum := sha256.Sum256(data[:])
	digest := hex.EncodeToString(sum[:])
	if digest != storageGlyphDigest {
		return StorageGlyphIdentity{}, errors.New("collect: the Windows 5/6 badge model does not match the verified parameter digest")
	}
	return StorageGlyphIdentity{Version: model.Version, Digest: digest}, nil
}

// SelfCheckStorageCardGlyphModel exercises the linked classifier on two
// deterministic glyph rasters. Windows field replay covers real captures.
func SelfCheckStorageCardGlyphModel() error {
	for _, fixture := range []struct {
		slot int
		mask glyphMask
		size image.Point
	}{
		{5, storageCardPosition5A, storageCardPosition5A.size()},
		{6, storageCardPosition6A, image.Pt(12, 18)},
	} {
		source := fixture.mask.size()
		img := image.NewRGBA(image.Rect(0, 0, 34, 26))
		for y := range fixture.size.Y {
			for x := range fixture.size.X {
				if fixture.mask.rows[y*source.Y/fixture.size.Y][x*source.X/fixture.size.X] == '#' {
					img.SetRGBA(6+x, 5+y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
				}
			}
		}
		m := &PixelMatcher{}
		if !m.confirmCardGlyph(img, img.Bounds(), fixture.slot) || m.confirmCardGlyph(img, img.Bounds(), 11-fixture.slot) {
			return fmt.Errorf("collect: the linked badge model did not distinguish the synthetic %d badge", fixture.slot)
		}
	}
	return nil
}

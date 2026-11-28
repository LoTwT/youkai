package collect

import (
	"slices"
	"testing"
)

func TestVerifiedStorageCardGlyphModel(t *testing.T) {
	if _, err := VerifiedStorageCardGlyphModel(); err != nil {
		t.Fatal(err)
	}
	changed := storageCardGlyphLDA
	changed.Weights = slices.Clone(changed.Weights)
	changed.Weights[0] += 0.01
	if _, err := verifyStorageCardGlyphModel(changed); err == nil {
		t.Fatal("model checksum accepted different weights")
	}
	if _, err := verifyStorageCardGlyphModel(cardGlyphLDA{Version: "unset"}); err == nil {
		t.Fatal("model check accepted the placeholder")
	}
}

func TestVerifiedCardGlyphModelDistinguishesFiveAndSix(t *testing.T) {
	if err := SelfCheckStorageCardGlyphModel(); err != nil {
		t.Fatal(err)
	}
}

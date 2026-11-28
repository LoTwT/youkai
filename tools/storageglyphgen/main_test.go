package main

import (
	"go/format"
	"strings"
	"testing"
)

func testExport() export {
	w := make([]*float64, cardFeatureLen)
	for i := range w {
		v := float64(i) / 1000
		w[i] = &v
	}
	bias := 1.5
	return export{
		Schema: "badge-lda-export-v1", Core: []int{6, 5, 21, 16}, Anchor: "component",
		MinLDAScore: 0.5, TrainManifestSHA: strings.Repeat("a", 64), TrainSamples: 48, TrainObjects: 24,
		Weights: w, Bias: &bias,
	}
}

func TestRenderModelCanonicalAndFormatted(t *testing.T) {
	e := testExport()
	e.Feature = "line one\nline two"
	out, err := renderModel(e)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "line one") {
		t.Fatal("source feature description must not be emitted")
	}
	if !strings.Contains(string(out), "336 row-major z-normalized luma") {
		t.Fatal("canonical feature comment missing")
	}
	if _, err := format.Source(out); err != nil {
		t.Fatalf("generated Go is not parseable: %v", err)
	}
}

func TestValidateExportRejectsNullWeights(t *testing.T) {
	for _, i := range []int{0, cardFeatureLen / 2, cardFeatureLen - 1} {
		e := testExport()
		e.Weights[i] = nil
		if err := validateExport(e); err == nil || !strings.Contains(err.Error(), "null") {
			t.Fatalf("null weight at %d: err=%v, want null rejection", i, err)
		}
	}
}

func TestValidateExportRejectsContractMismatches(t *testing.T) {
	tests := []struct {
		name string
		edit func(*export)
	}{
		{"missing anchor", func(e *export) { e.Anchor = "" }},
		{"fixed anchor", func(e *export) { e.Anchor = "fixed" }},
		{"bad sha", func(e *export) { e.TrainManifestSHA = "not-a-sha" }},
		{"null bias", func(e *export) { e.Bias = nil }},
		{"wrong length", func(e *export) { e.Weights = e.Weights[:len(e.Weights)-1] }},
		{"wrong core", func(e *export) { e.Core = []int{6, 4, 21, 16} }},
	}
	for _, tc := range tests {
		e := testExport()
		tc.edit(&e)
		if err := validateExport(e); err == nil {
			t.Errorf("%s: validation unexpectedly passed", tc.name)
		}
	}
}

//go:build race

package collect_test

// testRace reports whether the race detector is active. The traversal
// tests keep the production matcher thresholds, but the race detector
// makes the full-list correspondence search orders of magnitude slower;
// the harness subsamples coarser there so a bounded scroll wait still
// observes its settle frames.
const testRace = true

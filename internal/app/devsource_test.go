package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/scan"
)

// recognitionFixtureDir is the real recognition test fixture: the six
// drive-disc detail crops of internal/recognition/testdata, the canonical
// input of the development data source.
const recognitionFixtureDir = "../recognition/testdata"

func TestDevScanSourceLoadsRecognitionFixture(t *testing.T) {
	src, err := LoadDevScanSource(recognitionFixtureDir)
	if err != nil {
		t.Fatalf("LoadDevScanSource: %v", err)
	}
	if src.Pace != devPace {
		t.Fatalf("default pace = %v, want %v", src.Pace, devPace)
	}
	want := []string{"disc1", "disc2", "disc3", "disc4", "disc5", "disc6"}
	if len(src.groups) != len(want) {
		t.Fatalf("groups = %v, want %v", src.groups, want)
	}
	for i, group := range want {
		if src.groups[i] != group {
			t.Fatalf("group %d = %q, want %q (groups %v)", i, src.groups[i], group, src.groups)
		}
	}
	// Each disc carries its seven crops in manifest order: name, level,
	// main, sub1..sub4 (testdata/README.md).
	wantRoles := "name level main sub1 sub2 sub3 sub4"
	for _, group := range want {
		var roles []string
		for _, file := range src.filesOf[group] {
			_, role, _ := strings.Cut(strings.TrimSuffix(file, ".png"), "_")
			roles = append(roles, role)
		}
		if got := strings.Join(roles, " "); got != wantRoles {
			t.Fatalf("%s roles = %q, want %q", group, got, wantRoles)
		}
	}

	spec, err := src.NewScan()
	if err != nil {
		t.Fatalf("NewScan: %v", err)
	}
	if err := spec.validate(); err != nil {
		t.Fatalf("spec: %v", err)
	}
	if spec.Game != "zzz" || spec.Entity != "driveDisc" {
		t.Fatalf("descriptor = %s/%s, want zzz/driveDisc", spec.Game, spec.Entity)
	}
	if !strings.Contains(string(spec.Scope), `"kind":"devDirectory"`) || !strings.Contains(string(spec.Scope), `"items":6`) {
		t.Fatalf("scope = %s, want the dev directory description with 6 items", spec.Scope)
	}
}

func TestDevScanSourceRejectsBrokenFixtures(t *testing.T) {
	cases := []struct {
		name  string
		build func(t *testing.T, dir string)
		want  string
	}{
		{
			name:  "missing manifest",
			build: func(t *testing.T, dir string) {},
			want:  "lines.tsv",
		},
		{
			name: "row with wrong field count",
			build: func(t *testing.T, dir string) {
				writeFixtureFile(t, dir, "disc1_main.png\t00ab\t0\t0\n")
			},
			want: "want 7",
		},
		{
			name: "non-png file name",
			build: func(t *testing.T, dir string) {
				writeFixtureFile(t, dir, "disc1_main.jpg\t00ab\t0\t0\t4\t2\tfixture\n")
			},
			want: "not a PNG",
		},
		{
			name: "name without role",
			build: func(t *testing.T, dir string) {
				writeFixtureFile(t, dir, "disc1.png\t00ab\t0\t0\t4\t2\tfixture\n")
			},
			want: "not <observation>_<role>.png",
		},
		{
			name: "listed crop missing",
			build: func(t *testing.T, dir string) {
				writeFixtureFile(t, dir, "disc1_main.png\t00ab\t0\t0\t4\t2\tfixture\n")
			},
			want: "crop",
		},
		{
			name: "empty manifest",
			build: func(t *testing.T, dir string) {
				writeFixtureFile(t, dir, "# only a comment\n")
			},
			want: "lists no lines",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tc.build(t, dir)
			_, err := LoadDevScanSource(dir)
			if err == nil {
				t.Fatalf("LoadDevScanSource succeeded, want an error naming %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to name %q", err, tc.want)
			}
		})
	}
}

// writeFixtureFile writes one fixture file below dir, failing the test on
// error.
func writeFixtureFile(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "lines.tsv"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDevScanControllerAdmitsAllGroupsThroughKernel(t *testing.T) {
	src, err := LoadDevScanSource(recognitionFixtureDir)
	if err != nil {
		t.Fatalf("LoadDevScanSource: %v", err)
	}
	src.Pace = 0
	sink := &collectSink{}
	session, err := scan.Start(scan.Config{
		ID:         "dev-kernel",
		Capacity:   2,
		Controller: mustSpec(t, src),
		Recognizer: newFakeRecognizer(),
		Deliverer:  sink,
	})
	if err != nil {
		t.Fatalf("scan.Start: %v", err)
	}
	<-session.Done()
	snap := session.Snapshot()
	if snap.End != scan.EndCompleted {
		t.Fatalf("end = %s with %d faults, want completed", snap.End, len(snap.Faults))
	}
	if snap.Counts.Captured != 6 || snap.Counts.Produced != 6 {
		t.Fatalf("counts = %+v, want 6 captured and produced", snap.Counts)
	}
	if len(sink.observations) != 6 {
		t.Fatalf("admitted %d observations, want 6", len(sink.observations))
	}
	first := sink.observations[0]
	if len(first.Lines) != 7 {
		t.Fatalf("first observation has %d lines, want 7", len(first.Lines))
	}
	if first.Lines[0].Role != "name" || first.Lines[0].Source != "disc1_name.png" || first.Lines[0].Image == nil {
		t.Fatalf("first observation starts with %+v, want the decoded name line of disc1", first.Lines[0])
	}
}

// TestDevScanControllerPacesAdmission proves the controlled pace: with a
// 40ms pace, six admissions span at least 5×40ms (the last one admits
// without waiting afterwards).
func TestDevScanControllerPacesAdmission(t *testing.T) {
	src, err := LoadDevScanSource(recognitionFixtureDir)
	if err != nil {
		t.Fatalf("LoadDevScanSource: %v", err)
	}
	src.Pace = 40 * time.Millisecond
	sink := &timingSink{}
	session, err := scan.Start(scan.Config{
		ID:         "dev-paced",
		Capacity:   2,
		Controller: mustSpec(t, src),
		Recognizer: newFakeRecognizer(),
		Deliverer:  sink,
	})
	if err != nil {
		t.Fatalf("scan.Start: %v", err)
	}
	<-session.Done()
	if len(sink.arrivals) != 6 {
		t.Fatalf("admitted %d observations, want 6", len(sink.arrivals))
	}
	span := sink.arrivals[len(sink.arrivals)-1].Sub(sink.arrivals[0])
	if span < 200*time.Millisecond {
		t.Fatalf("six admissions spanned %v with a 40ms pace, want at least 200ms", span)
	}
}

// mustSpec builds the scan spec of a dev source, failing on error.
func mustSpec(t *testing.T, src *DevScanSource) scan.Controller {
	t.Helper()
	spec, err := src.NewScan()
	if err != nil {
		t.Fatalf("NewScan: %v", err)
	}
	return spec.Controller
}

// collectSink records every delivered observation.
type collectSink struct {
	observations []Observation
}

func (s *collectSink) Deliver(rec scan.Record) error {
	if obs, ok := rec.Observation.(Observation); ok {
		s.observations = append(s.observations, obs)
	}
	return nil
}

// timingSink records the arrival times of the delivered records.
type timingSink struct {
	arrivals []time.Time
}

func (s *timingSink) Deliver(rec scan.Record) error {
	s.arrivals = append(s.arrivals, time.Now())
	return nil
}

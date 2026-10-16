package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"path"
	"testing"
)

func TestPinnedDeps(t *testing.T) {
	// Offline self-checks of the fixed manifest: every download comes from
	// an https URL with a well-formed digest, every destination stays a
	// clean relative path under resources/, and no two dependencies install
	// the same file.
	dests := map[string]string{}
	for _, d := range deps {
		if path.Base(d.name) != d.name {
			t.Errorf("%s: name must be a plain file name", d.name)
		}
		u, err := url.Parse(d.url)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			t.Errorf("%s: url %q is not a valid https URL", d.name, d.url)
		}
		if err := checkDigest(d.sha256); err != nil {
			t.Errorf("%s: %v", d.name, err)
		}
		switch d.platform {
		case "", "windows/amd64", "darwin/arm64":
		default:
			t.Errorf("%s: platform %q is not one this project ships", d.name, d.platform)
		}
		targets := d.targets()
		if len(targets) == 0 {
			t.Errorf("%s: installs no files", d.name)
		}
		for _, tgt := range targets {
			if err := safeRelPath(tgt.dest); err != nil {
				t.Errorf("%s: destination %q: %v", d.name, tgt.dest, err)
			}
			if tgt.member != "" {
				if err := safeRelPath(tgt.member); err != nil {
					t.Errorf("%s: member %q: %v", d.name, tgt.member, err)
				}
			}
			if err := checkDigest(tgt.sha256); err != nil {
				t.Errorf("%s: %s: %v", d.name, tgt.dest, err)
			}
			if other, ok := dests[tgt.dest]; ok {
				t.Errorf("destination %s is installed by both %s and %s", tgt.dest, other, d.name)
			}
			dests[tgt.dest] = d.name
		}
	}
}

// checkDigest verifies that s is a lowercase hex SHA-256.
func checkDigest(s string) error {
	if len(s) != sha256.Size*2 {
		return fmt.Errorf("digest %q is not %d hex characters", s, sha256.Size*2)
	}
	if _, err := hex.DecodeString(s); err != nil {
		return fmt.Errorf("digest %q: %v", s, err)
	}
	return nil
}

func TestSelectDeps(t *testing.T) {
	const host = "darwin/arm64"
	for _, tc := range []struct {
		want    string
		wantErr bool
	}{
		{want: "all"},
		{want: "current"},
		{want: "darwin/arm64"},
		{want: "windows/amd64"},
		{want: "linux/amd64", wantErr: true},
		{want: "windows-amd64", wantErr: true},
		{want: "bogus", wantErr: true},
	} {
		selected, err := selectDeps(deps, tc.want, host)
		if tc.wantErr {
			if err == nil {
				t.Errorf("selectDeps(%q) = %v, want an error", tc.want, selected)
			}
			continue
		}
		if err != nil {
			t.Errorf("selectDeps(%q): %v", tc.want, err)
			continue
		}
		wantPlatform := tc.want
		if tc.want == "current" {
			wantPlatform = host
		}
		if tc.want == "all" {
			if len(selected) != len(deps) {
				t.Errorf("selectDeps(all) selected %d dependencies, want %d", len(selected), len(deps))
			}
			continue
		}
		var platformDeps int
		for _, d := range selected {
			if d.platform != "" && d.platform != wantPlatform {
				t.Errorf("selectDeps(%q) selected %s for platform %q", tc.want, d.name, d.platform)
			}
			if d.platform == wantPlatform {
				platformDeps++
			}
		}
		if platformDeps == 0 {
			t.Errorf("selectDeps(%q) selected no dependency of that platform", tc.want)
		}
	}
}

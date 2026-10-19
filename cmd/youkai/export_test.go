package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRefuseManagedTarget(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name    string
		path    string
		refused bool
	}{
		{"root itself", root, true},
		{"below root", filepath.Join(root, "sessions", "out.youkai.json"), true},
		{"sibling with a shared prefix", root + "-exports" + string(os.PathSeparator) + "out.youkai.json", false},
		{"outside", filepath.Join(t.TempDir(), "out.youkai.json"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := refuseManagedTarget(root, tc.path)
			if tc.refused && err == nil {
				t.Fatalf("%q was not refused", tc.path)
			}
			if !tc.refused && err != nil {
				t.Fatalf("%q was refused: %v", tc.path, err)
			}
		})
	}
}

func TestWriteExportFileReplacesOnlyOnSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.youkai.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeExportFile(path, []byte("new")); err != nil {
		t.Fatalf("writeExportFile: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("content = %q, want new", got)
	}
	// A missing directory fails before touching the previous file.
	missing := filepath.Join(dir, "missing", "out.youkai.json")
	if err := writeExportFile(missing, []byte("x")); err == nil {
		t.Fatal("writing into a missing directory succeeded")
	}
	got, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("a failed write changed the previous file: %q", got)
	}
}

//go:build windows

package windows

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// The regression this test guards against: a LazyProc naming a function that
// no DLL exports fails only when the path is first used on a real machine. The
// Windows field round found both a nonexistent GetCurrentDpiAwarenessContext
// and GetDC/ReleaseDC declared under gdi32 instead of user32. This test reads
// every NewLazySystemDLL/NewProc declaration in the package and resolves each
// function on the running Windows machine, so a future typo or wrong DLL fails
// CI instead of a field run.
func TestDeclaredLazyProcsExist(t *testing.T) {
	dllPattern := regexp.MustCompile(`^\s*(\w+)\s*=\s*windows\.NewLazySystemDLL\("([^"]+)"\)`)
	procPattern := regexp.MustCompile(`^\s*(\w+)\s*=\s*(\w+)\.NewProc\("([^"]+)"\)`)

	dlls := make(map[string]string)
	type procDecl struct {
		file     string
		line     int
		dllVar   string
		function string
	}
	var procs []procDecl

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		dllMentions, procMentions := 0, 0
		dllMatches, procMatches := 0, 0
		for lineNumber, line := range strings.Split(string(source), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			if strings.Contains(line, "windows.NewLazySystemDLL(") {
				dllMentions++
			}
			if strings.Contains(line, ".NewProc(") {
				procMentions++
			}
			if match := dllPattern.FindStringSubmatch(line); match != nil {
				dlls[match[1]] = match[2]
				dllMatches++
				continue
			}
			if match := procPattern.FindStringSubmatch(line); match != nil {
				procs = append(procs, procDecl{
					file:     name,
					line:     lineNumber + 1,
					dllVar:   match[2],
					function: match[3],
				})
				procMatches++
			}
		}
		if dllMatches != dllMentions {
			t.Errorf("%s: %d NewLazySystemDLL declarations but %d matched the expected form", name, dllMentions, dllMatches)
		}
		if procMatches != procMentions {
			t.Errorf("%s: %d NewProc declarations but %d matched the expected form", name, procMentions, procMatches)
		}
	}
	if len(dlls) == 0 {
		t.Fatal("found no NewLazySystemDLL declarations; the test is not scanning the package")
	}
	if len(procs) == 0 {
		t.Fatal("found no NewProc declarations; the test is not scanning the package")
	}

	sort.Slice(procs, func(i, j int) bool {
		if procs[i].dllVar != procs[j].dllVar {
			return procs[i].dllVar < procs[j].dllVar
		}
		return procs[i].function < procs[j].function
	})
	for _, proc := range procs {
		dll, ok := dlls[proc.dllVar]
		if !ok {
			t.Errorf("%s:%d: %s has no NewLazySystemDLL declaration", proc.file, proc.line, proc.dllVar)
			continue
		}
		if err := windows.NewLazySystemDLL(dll).NewProc(proc.function).Find(); err != nil {
			t.Errorf("%s:%d: %s!%s does not resolve: %v", proc.file, proc.line, dll, proc.function, err)
		}
	}
}

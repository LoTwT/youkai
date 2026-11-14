package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/upstream." + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// This paired oracle fails against each real upstream template (the Windows
// fixture red also executes those original force-kill/catch paths), then
// passes only after the complete production conversion.
func unsafeControl(s string) bool {
	return strings.Contains(s, "Stop-Process") || strings.Contains(s, "CloseMainWindow") || strings.Contains(s, "SilentlyContinue | Where-Object")
}

func TestCompleteTemplateConversion(t *testing.T) {
	up := fixture(t, "nsi")
	if !unsafeControl(up) {
		t.Fatal("red oracle did not detect original full unsafe template")
	}
	got, err := convertNSIS(up)
	if err != nil {
		t.Fatal(err)
	}
	if unsafeControl(got) {
		t.Fatal("unsafe process control remains")
	}
	for _, want := range []string{"nsExec::ExecToStack /TIMEOUT=15000", `WriteUninstaller "$INSTDIR\Uninstall.exe"`, `  RMDir /r "$INSTDIR"`, `  StrCmp $2 "0" done`, "!define YOUKAI_SAFE_INSTALLER 1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s", want)
		}
	}
	if strings.Count(got, "nsExec::ExecToStack /TIMEOUT=15000") != 2 {
		t.Fatal("both complete install/uninstall checks are required")
	}
	install := strings.Index(got, "Section\n  Push \"$INSTDIR\"\n  Call CloseApp\n")
	write := strings.Index(got, "  File ")
	uninstall := strings.Index(got, "Section \"Uninstall\"\n  Push \"$INSTDIR\"\n  Call un.CloseApp\n")
	remove := strings.Index(got, `  RMDir /r "$INSTDIR"`)
	if !(install >= 0 && install < write && uninstall >= 0 && uninstall < remove) {
		t.Fatal("final-directory checks must precede ordinary file changes")
	}
	for _, mutation := range []string{
		strings.Replace(up, "Function un.CloseApp", "Function un.CloseApp2", 1),
		strings.Replace(up, "  Call CloseApp", "  ; changed\n  Call CloseApp", 1),
		strings.Replace(up, `  RMDir /r "$INSTDIR"`, `  Delete "$INSTDIR\Youkai.exe"`, 1),
		up + "\nSection\nSectionEnd\n",
	} {
		if _, err := convertNSIS(mutation); err == nil {
			t.Fatal("template drift accepted")
		}
	}
	if _, err := convertNSIS(got); err == nil {
		t.Fatal("double conversion accepted")
	}
}

func TestPS1FullControlAndFailure(t *testing.T) {
	up := fixture(t, "ps1")
	if !unsafeControl(up) {
		t.Fatal("red oracle missed original PS1")
	}
	got, err := convertPS1(up)
	if err != nil {
		t.Fatal(err)
	}
	if unsafeControl(got) || strings.Contains(got, "function Wait-Exit") || strings.Contains(got, "function Get-AppProcess") {
		t.Fatal("old independent quit path remains")
	}
	if !strings.Contains(got, "-ErrorAction Stop).InstallLocation") || !strings.Contains(got, "\tthrow\n}\n}") {
		t.Fatal("PS1 discovery/outer failure must propagate")
	}
	if !strings.Contains(got, `Start-Process -FilePath $setup -ArgumentList "/S /D=$folder" -Wait -PassThru`) || !strings.Contains(got, "$process.ExitCode -ne 0") {
		t.Fatal("handoff must wait and propagate NSIS refusal")
	}
	if _, err := convertPS1(strings.Replace(up, "$process.ExitCode -ne 0", "$process.ExitCode -eq 0", 1)); err == nil {
		t.Fatal("exit propagation drift accepted")
	}
	if _, err := convertPS1(up + "\n# drift\n"); err == nil {
		t.Fatal("PS1 full-template drift accepted")
	}
	channel := strings.Replace(up, "$tagPrefix = ''", "$tagPrefix = 'test-v'", 1)
	if _, err := convertPS1(channel); err != nil {
		t.Fatal("supported channel slot:", err)
	}
}

func TestFinalChecksumBoundary(t *testing.T) {
	dir := t.TempDir()
	setup := []byte("signed installer fixture")
	archive := []byte("signed update archive fixture")
	ps := []byte("adapted script")
	for name, b := range map[string][]byte{"setup.exe": setup, "app.tar.gz": archive, "install.ps1": ps} {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	old := digest(setup) + "  setup.exe\n" + digest(archive) + "  app.tar.gz\n" + digest([]byte("original script")) + "  install.ps1\n"
	path := filepath.Join(dir, "SHA256SUMS")
	if err := os.WriteFile(path, []byte(old), 0600); err != nil {
		t.Fatal(err)
	}
	if err := finishChecksums(dir, path, true); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), digest(ps)+"  install.ps1") {
		t.Fatal("PS1 checksum not finalized")
	}
	if err := os.WriteFile(filepath.Join(dir, "app.tar.gz"), []byte("changed signed bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := finishChecksums(dir, path, true); err == nil {
		t.Fatal("changed archive accepted with old signature")
	}
	if err := os.Remove(filepath.Join(dir, "setup.exe")); err != nil {
		t.Fatal(err)
	}
	if err := finishChecksums(dir, path, true); err == nil {
		t.Fatal("missing required checksum member accepted")
	}
}

func TestCompilerAndSigningContract(t *testing.T) {
	up := fixture(t, "nsi")
	sign := `!uninstfinalize '"$MYGO_SIGNER" sign-uninstaller "%1"' = 0` + "\n"
	signed := strings.Replace(up, "InstallDir ", sign+"InstallDir ", 1)
	converted, err := convertNSIS(signed)
	if err != nil || !strings.Contains(converted, sign) {
		t.Fatal("uninstaller signing was not preserved", err)
	}
	if _, err := convertNSIS(strings.Replace(signed, sign, sign+sign, 1)); err == nil {
		t.Fatal("duplicate signer accepted")
	}
	if _, err := convertNSIS(strings.Replace(signed, "sign-uninstaller", "other-command", 1)); err == nil {
		t.Fatal("signer drift accepted")
	}
	if runtime.GOOS == "windows" {
		return
	} // actual Windows compiler is verified by the NSIS fixture packet
	dir := t.TempDir()
	fake := filepath.Join(dir, "compiler")
	// Explicit absolute local fake, with shell builtins only; no PATH upload,
	// signing utility, network client or persistent environment mutation.
	script := "#!/bin/sh\nif [ \"$1\" = -VERSION ]; then printf 'v3.13\\n'; exit 0; fi\n[ \"$MYGO_SIGNER\" = DEP01-LOCAL ] || exit 91\n[ \"$MYGO_SIGN_SETTINGS\" = fixture-settings ] || exit 92\nexit 13\n"
	if err := os.WriteFile(fake, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(dir, "input.nsi")
	if err := os.WriteFile(input, []byte(signed), 0600); err != nil {
		t.Fatal(err)
	}
	state := buildState{Compiler: fake, Record: filepath.Join(dir, "record.json")}
	b, _ := json.Marshal(state)
	statePath := filepath.Join(dir, "state.json")
	if err := os.WriteFile(statePath, b, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YOUKAI_MYGO_BUILD_STATE", statePath)
	t.Setenv("MYGO_SIGNER", "DEP01-LOCAL")
	t.Setenv("MYGO_SIGN_SETTINGS", "fixture-settings")
	if err := compile([]string{"-VERSION"}); err != nil {
		t.Fatal(err)
	}
	err = compile([]string{"-V2", input})
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 13 {
		t.Fatalf("compiler/signer failure not propagated: %v", err)
	}
	if _, err := os.Stat(state.Record); !os.IsNotExist(err) {
		t.Fatal("failed compiler emitted success record")
	}
	b, _ = os.ReadFile(input)
	if unsafeControl(string(b)) || !strings.Contains(string(b), "nsExec::ExecToStack /TIMEOUT=15000") {
		t.Fatal("real compiler boundary saw unconverted input")
	}
}

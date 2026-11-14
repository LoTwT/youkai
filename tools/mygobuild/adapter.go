package main

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

//go:embed gate.ps1
var gateScript string

const nsisTemplateSHA = "321886c1ed4ba2968d4a1ccba213246db665137b7a606b175a3a8d10d2539773"
const ps1TemplateSHA = "aa9779e9e52c16ce47005d02667b95c4b3590c2d181ddf72843acd9e97a70d45"

func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func once(s, old, replacement string) (string, error) {
	if strings.Count(s, old) != 1 {
		return "", fmt.Errorf("template drift: expected one %q", old)
	}
	return strings.Replace(s, old, replacement, 1), nil
}

// Normalize only the generator's documented variable slots. Every other
// byte, including complete functions, section structure and hooks, is bound.
func normalizeNSIS(s string) string {
	s = strings.TrimPrefix(s, "\ufeff")
	for _, pair := range [][2]string{
		{`(?m)^OutFile "[^\r\n]+"$`, `OutFile "@OUT@"`},
		{`(?m)^  File(?: /r)? "[^\r\n]+"\n`, "@FILES@\n"},
		{`(?m)^!include "[^\r\n]*require-safe-installer\.nsh"$`, `!include "@SENTINEL@"`},
		{`(?m)^(  WriteRegStr HKCU "Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\com\.lotwt\.youkai" "DisplayVersion") "[^"\r\n]+"$`, `$1 "@VERSION@"`},
		{`(?m)^!uninstfinalize '[^\r\n]+' = 0\n`, ""},
	} {
		s = regexp.MustCompile(pair[0]).ReplaceAllString(s, pair[1])
	}
	// The installed resource set varies with app-local CRT and arch. Only
	// consecutive actual File directives can occupy this single slot.
	s = regexp.MustCompile(`(?:@FILES@\n)+`).ReplaceAllString(s, "@FILES@\n")
	return s
}

func normalizePS1(s string) string {
	for _, name := range []string{"releases", "api", "tagPrefix", "assetsApi"} {
		re := regexp.MustCompile(`(?m)^\$` + name + ` = '[^'\r\n]*'$`)
		s = re.ReplaceAllString(s, "$"+"$"+name+" = '@SOURCE@'")
	}
	return s
}

func closeFunction(prefix string) string {
	var write strings.Builder
	for _, line := range strings.SplitAfter(gateScript, "\n") {
		if line != "" {
			write.WriteString("  FileWrite $2 " + nsisQuote(strings.TrimSuffix(line, "\n")+"\r\n") + "\n")
		}
	}
	return `Function ` + prefix + `CloseApp
  Exch $0
  Push $1
  Push $2
  Push $3
  StrCpy $1 "$WINDIR\Sysnative\WindowsPowerShell\v1.0\powershell.exe"
  IfFileExists $1 have_tool
  StrCpy $1 "$SYSDIR\WindowsPowerShell\v1.0\powershell.exe"
  IfFileExists $1 have_tool failed
  have_tool:
  InitPluginsDir
  ClearErrors
  FileOpen $2 "$PLUGINSDIR\youkai-install-check.ps1" w
  IfErrors failed
` + write.String() + `  FileClose $2
  IfErrors failed
  System::Call 'kernel32::SetEnvironmentVariable(t "MYGO_APP_DIR", t r0) i.r2'
  StrCmp $2 1 0 failed
  System::Call 'kernel32::SetEnvironmentVariable(t "MYGO_APP_SCRIPT", t "$PLUGINSDIR\youkai-install-check.ps1") i.r2'
  StrCmp $2 1 0 failed
  nsExec::ExecToStack /TIMEOUT=15000 '"$1" -NoProfile -NonInteractive -ExecutionPolicy Bypass -Command "& ([scriptblock]::Create([IO.File]::ReadAllText($$env:MYGO_APP_SCRIPT)))"'
  Pop $2
  Pop $3
  StrCmp $2 "0" done
  DetailPrint "$3"
  failed:
  MessageBox MB_OK|MB_ICONSTOP "Youkai is running, or its state could not be checked. Quit Youkai from its menu, resolve unsaved work or save errors, then retry. No app was forced to exit." /SD IDOK
  SetErrorLevel 2
  Quit
  done:
  Pop $3
  Pop $2
  Pop $1
  Pop $0
FunctionEnd

`
}

func convertNSIS(s string) (string, error) {
	// These are the two exact host variants emitted by uninstallerSigning.
	// Signing is a single optional slot, never an arbitrary command list.
	signatures := regexp.MustCompile(`(?m)^!uninstfinalize[^\r\n]*$`).FindAllString(s, -1)
	if len(signatures) > 1 {
		return "", errors.New("uninstaller signing command count drift")
	}
	if len(signatures) == 1 && signatures[0] != `!uninstfinalize '"$MYGO_SIGNER" sign-uninstaller "%1"' = 0` && signatures[0] != `!uninstfinalize '"%MYGO_SIGNER%" sign-uninstaller "%1"' = 0` {
		return "", errors.New("uninstaller signing command drift")
	}
	if digest([]byte(normalizeNSIS(s))) != nsisTemplateSHA {
		return "", errors.New("MyGo 0.9 complete NSIS template drift; refusing unsafe compiler input")
	}
	s = strings.TrimPrefix(s, "\ufeff")
	for _, prefix := range []string{"", "un."} {
		start := strings.Index(s, "; Quits the app running from the folder on the stack.\nFunction "+prefix+"CloseApp\n")
		if start < 0 {
			return "", errors.New("missing complete CloseApp")
		}
		end := strings.Index(s[start:], "FunctionEnd\n")
		if end < 0 {
			return "", errors.New("missing CloseApp boundary")
		}
		s = s[:start] + closeFunction(prefix) + s[start+end+len("FunctionEnd\n"):]
	}
	// Keep MyGo's ordinary file, registration and uninstall operations intact.
	// The final-directory check precedes every section's first target mutation.
	return "\ufeff!define YOUKAI_SAFE_INSTALLER 1\n" + s, nil
}

func nsisQuote(s string) string {
	s = strings.NewReplacer("$", "$$", `"`, `$\"`, "\r", `$\r`, "\n", `$\n`, "\t", `$\t`).Replace(s)
	return `"` + s + `"`
}

func convertPS1(s string) (string, error) {
	if digest([]byte(normalizePS1(s))) != ps1TemplateSHA {
		return "", errors.New("MyGo 0.9 complete install.ps1 template drift")
	}
	start := strings.Index(s, "# Get-AppProcess returns")
	end := strings.Index(s, "function Install-App {")
	if start < 0 || end < start {
		return "", errors.New("PS1 helper boundaries")
	}
	s = s[:start] + "# The adapted NSIS checks the final destination and refuses a running app.\n" + s[end:]
	start = strings.Index(s, "\t\t# Quit the app:")
	end = strings.Index(s, "\t\tWrite-Host \"Installing")
	if start < 0 || end < start {
		return "", errors.New("PS1 quit boundaries")
	}
	s = s[:start] + "\t\t# NSIS checks the actual /D destination before changing installed files.\n" + s[end:]
	old := "\t\t$location = (Get-ItemProperty -Path $uninstallKey -ErrorAction SilentlyContinue).InstallLocation\n\t\tif ($location) { $folder = $location.TrimEnd('\\') }"
	new := `        if (Test-Path -LiteralPath $uninstallKey -ErrorAction Stop) {
            $location = (Get-ItemProperty -LiteralPath $uninstallKey -ErrorAction Stop).InstallLocation
            if ($null -ne $location) {
                if ($location -isnot [string] -or -not [IO.Path]::IsPathRooted($location)) { throw 'Invalid InstallLocation; repair the installation path before retrying' }
                $folder = $location.TrimEnd('\')
            }
        }`
	var err error
	s, err = once(s, old, new)
	if err != nil {
		return "", err
	}
	s, err = once(s, "\tWrite-Host \"install.ps1: $_\" -ForegroundColor Red\n}", "\tWrite-Host \"install.ps1: $_\" -ForegroundColor Red\n\tthrow\n}")
	return s, err
}

func rewritePS1(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s, err := convertPS1(string(b))
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(s), 0644)
}

// Preserve every original checksum entry, verifying unchanged artifacts
// before replacing the PS1 digest. Archive/delta/manifest bytes are untouched.
func finishChecksums(dir, path string, hasPS1 bool) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var out strings.Builder
	found := false
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if len(line) < 67 || line[64:66] != "  " {
			return errors.New("malformed SHA256SUMS")
		}
		name := line[66:]
		if seen[name] {
			return fmt.Errorf("duplicate checksum member: %s", name)
		}
		seen[name] = true
		if filepath.Base(name) != name || strings.ContainsAny(name, "/\\") {
			return errors.New("invalid checksum filename")
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		actual := digest(data)
		if name == "install.ps1" && hasPS1 {
			found = true
		} else if actual != line[:64] {
			return fmt.Errorf("artifact changed after MyGo signing: %s", name)
		}
		fmt.Fprintf(&out, "%s  %s\n", actual, name)
	}
	if hasPS1 && !found {
		return errors.New("install.ps1 missing from checksums")
	}
	return os.WriteFile(path, []byte(out.String()), 0644)
}

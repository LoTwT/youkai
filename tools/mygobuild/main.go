// mygobuild is the version-bound Windows safety adapter around the unchanged
// MyGo generator. It never uploads. The actual compiler sees only converted
// complete templates, before MyGo signs the installer and builds updates.
package main

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type buildState struct {
	Compiler string
	Record   string
}
type compileRecord struct{ OriginalSHA256, ConvertedSHA256, Compiler, Input string }

var sourcePins = map[string]string{
	"nsis.go":       "1a666c780024f42d80b5659e251e519aa21a7727bfdff809c019add37b4b1adb",
	"build.go":      "eeae6bf873395f5de91872fde6eddf5a3792fef8754f4b13ac6a80a30235ceb2",
	"installps1.go": "bdc05e3ea90cf3b5e666b84cacf47292f726530803a7d886a39f705648746c23",
	"checksums.go":  "9e1f2f39db8205af498e4f2236ee90ebc6bee1b4dcadef6436378c5ad39001f0",
	"updates.go":    "06e9113735002fbac0e5d208b3ea531230fe5ceb24257d66ecec9601c48cc44a",
	"publish.go":    "87ca12217e125f4ccd6fc3b34a596f323b6460cfb1c722c9af1ea7628ed07caa",
}

func main() {
	var err error
	if strings.EqualFold(strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe"), "makensis") {
		err = compile(os.Args[1:])
	} else {
		err = build(os.Args[1:])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "mygobuild:", err)
		var exitError *exec.ExitError
		if errors.As(err, &exitError) && exitError.ExitCode() > 0 {
			os.Exit(exitError.ExitCode())
		}
		os.Exit(1)
	}
}

func run(program string, args []string, env []string) error {
	cmd := exec.Command(program, args...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func bindMyGo() error {
	data, err := exec.Command("go", "list", "-mod=readonly", "-m", "-json", "github.com/egoist/mygo").Output()
	if err != nil {
		return err
	}
	var m struct {
		Version, Dir string
		Replace      *json.RawMessage
	}
	if err = json.Unmarshal(data, &m); err != nil {
		return err
	}
	if m.Version != "v0.9.0" || m.Replace != nil {
		return errors.New("requires the original MyGo v0.9.0, without replace")
	}
	for name, want := range sourcePins {
		b, err := os.ReadFile(filepath.Join(m.Dir, "cmd", "mygo", name))
		if err != nil {
			return err
		}
		if digest(b) != want {
			return fmt.Errorf("MyGo source drift: %s", name)
		}
	}
	return nil
}

func build(args []string) error {
	flags := flag.NewFlagSet("mygobuild", flag.ContinueOnError)
	platform := flags.String("platform", "windows/amd64", "Windows packaging target")
	out := flags.String("o", "", "output directory (mygo.json out by default)")
	debug := flags.Bool("debug", false, "keep MyGo development features")
	skipCommand := flags.Bool("skip-build-command", false, "skip MyGo buildCommand")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *platform != "windows/amd64" || flags.NArg() != 0 {
		return errors.New("this adapter packages windows/amd64 from the current project directory")
	}
	// There is deliberately no upload or skip-installer switch.
	var config struct {
		Name, Identifier, Out string
		Windows               struct{ InstallerInclude string }
		Updates               *json.RawMessage
	}
	b, err := os.ReadFile("mygo.json")
	if err != nil {
		return err
	}
	if err = json.Unmarshal(b, &config); err != nil {
		return err
	}
	if config.Name != "Youkai" || config.Identifier != "com.lotwt.youkai" || config.Windows.InstallerInclude != "packaging/require-safe-installer.nsh" {
		return errors.New("project identity or safety sentinel drift")
	}
	if err = bindMyGo(); err != nil {
		return err
	}
	work, err := os.MkdirTemp("", "youkai-safe-build-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	real, err := resolveNSIS(work)
	if err != nil {
		return err
	}
	wrapper := filepath.Join(work, "bin")
	if err = os.Mkdir(wrapper, 0755); err != nil {
		return err
	}
	name := "makensis"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	// Build a native wrapper; app cross-compilation settings must not leak in.
	env := append(os.Environ(), "GOOS="+runtime.GOOS, "GOARCH="+runtime.GOARCH, "CGO_ENABLED=0")
	if err = run("go", []string{"build", "-o", filepath.Join(wrapper, name), "./tools/mygobuild"}, env); err != nil {
		return err
	}
	state := buildState{Compiler: real, Record: filepath.Join(work, "compiled.json")}
	data, _ := json.Marshal(state)
	statePath := filepath.Join(work, "state.json")
	if err = os.WriteFile(statePath, data, 0600); err != nil {
		return err
	}
	mygoArgs := []string{"tool", "mygo", "build", "-platform", *platform}
	if *out != "" {
		mygoArgs = append(mygoArgs, "-o", *out)
	} else {
		*out = config.Out
		if *out == "" {
			*out = "dist"
		}
	}
	if *debug {
		mygoArgs = append(mygoArgs, "-debug")
	}
	if *skipCommand {
		mygoArgs = append(mygoArgs, "-skip-build-command")
	}
	env = append(os.Environ(), "PATH="+wrapper+string(os.PathListSeparator)+os.Getenv("PATH"), "YOUKAI_MYGO_BUILD_STATE="+statePath)
	if err = run("go", mygoArgs, env); err != nil {
		return err
	}
	// An upstream warning/skip, missing adapter or failed compiler is fatal.
	compiled, err := os.ReadFile(state.Record)
	if err != nil {
		return fmt.Errorf("no converted installer was compiled: %w", err)
	}
	dir := filepath.Join(*out, "windows-amd64")
	setups, err := filepath.Glob(filepath.Join(dir, "youkai-setup-*-amd64.exe"))
	if err != nil || len(setups) != 1 {
		return errors.New("expected exactly one compiled installer")
	}
	if config.Updates != nil {
		if err = rewritePS1(filepath.Join(dir, "install.ps1")); err != nil {
			return err
		}
	}
	sumsPath := filepath.Join(*out, "SHA256SUMS")
	if config.Updates == nil {
		// MyGo writes SHA256SUMS only with updates. Package still binds its
		// signed installer, and cannot accidentally reuse an old release list.
		setup, readErr := os.ReadFile(setups[0])
		if readErr != nil {
			return readErr
		}
		if err = os.WriteFile(sumsPath, []byte(digest(setup)+"  "+filepath.Base(setups[0])+"\n"), 0644); err != nil {
			return err
		}
	}
	if err = finishChecksums(dir, sumsPath, config.Updates != nil); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(dir, "installer-safety.json"), compiled, 0644); err != nil {
		return err
	}
	fmt.Println("Windows installer safety adaptation and final checksums verified")
	return nil
}

func compile(args []string) error {
	path := os.Getenv("YOUKAI_MYGO_BUILD_STATE")
	if path == "" {
		return errors.New("compiler wrapper requires its private build state")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var state buildState
	if err = json.Unmarshal(b, &state); err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(state.Compiler)
	if err != nil {
		return err
	}
	self, _ = filepath.EvalSymlinks(self)
	if !filepath.IsAbs(real) || strings.EqualFold(real, self) {
		return errors.New("real NSIS compiler must be absolute and distinct from wrapper")
	}
	// Preserve the real version probe, signer environment and exact exit.
	if len(args) == 1 && args[0] == "-VERSION" {
		return run(real, args, os.Environ())
	}
	if len(args) != 2 || args[0] != "-V2" || filepath.Ext(args[1]) != ".nsi" {
		return errors.New("unexpected NSIS invocation")
	}
	original, err := os.ReadFile(args[1])
	if err != nil {
		return err
	}
	converted, err := convertNSIS(string(original))
	if evidence := os.Getenv("YOUKAI_BUILD_EVIDENCE"); evidence != "" {
		if writeErr := os.WriteFile(filepath.Join(evidence, "installer-upstream.nsi"), original, 0600); writeErr != nil {
			return writeErr
		}
		if err == nil {
			if writeErr := os.WriteFile(filepath.Join(evidence, "installer-adapted.nsi"), []byte(converted), 0600); writeErr != nil {
				return writeErr
			}
		}
	}
	if err != nil {
		return err
	}
	if err = os.WriteFile(args[1], []byte(converted), 0600); err != nil {
		return err
	}
	if err = run(real, args, os.Environ()); err != nil {
		return err
	}
	record := compileRecord{digest(original), digest([]byte(converted)), real, args[1]}
	b, _ = json.MarshalIndent(record, "", "  ")
	return os.WriteFile(state.Record, append(b, '\n'), 0600)
}

func resolveNSIS(work string) (string, error) {
	if value := os.Getenv("YOUKAI_MAKENSIS"); value != "" {
		return filepath.Abs(value)
	}
	if p, err := exec.LookPath("makensis"); err == nil {
		return filepath.Abs(p)
	}
	if runtime.GOOS != "windows" {
		return "", errors.New("install NSIS 3.13 (makensis) for Windows packaging")
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	for _, p := range []string{filepath.Join(os.Getenv("ProgramFiles(x86)"), "NSIS", "makensis.exe"), filepath.Join(os.Getenv("ProgramFiles"), "NSIS", "makensis.exe"), filepath.Join(cache, "mygo", "nsis-3.13", "Bin", "makensis.exe"), filepath.Join(cache, "mygo", "nsis-3.13", "makensis.exe")} {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return filepath.Abs(p)
		}
	}
	// Same NSIS release, mirrors and SHA as MyGo 0.9. Download into private
	// build TEMP, never the shared Go module cache or an existing install.
	const want = "ba63dffc4410ee89193e1cb5a41989991bd77c61068da17e3156d136b7b0b3d8"
	client := http.Client{Timeout: 10 * time.Minute}
	var last error
	for _, url := range []string{"https://github.com/egoist/mygo/releases/download/nsis-3.13/nsis-3.13.zip", "https://downloads.sourceforge.net/project/nsis/NSIS%203/3.13/nsis-3.13.zip"} {
		resp, err := client.Get(url)
		if err != nil {
			last = err
			continue
		}
		b, readErr := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		resp.Body.Close()
		if readErr != nil || resp.StatusCode != 200 || digest(b) != want {
			last = errors.New("NSIS download/status/SHA256 check failed")
			continue
		}
		archive := filepath.Join(work, "nsis.zip")
		if err = os.WriteFile(archive, b, 0600); err != nil {
			return "", err
		}
		return unpackNSIS(archive, filepath.Join(work, "nsis"))
	}
	return "", fmt.Errorf("NSIS 3.13 unavailable: %w", last)
}

func unpackNSIS(archive, dir string) (string, error) {
	z, err := zip.OpenReader(archive)
	if err != nil {
		return "", err
	}
	defer z.Close()
	for _, f := range z.File {
		_, name, ok := strings.Cut(f.Name, "/")
		if !ok || name == "" {
			continue
		}
		if !filepath.IsLocal(filepath.FromSlash(name)) || strings.Contains(name, `\`) || f.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("unsafe NSIS ZIP member")
		}
		path := filepath.Join(dir, filepath.FromSlash(name))
		if f.FileInfo().IsDir() {
			if err = os.MkdirAll(path, 0755); err != nil {
				return "", err
			}
			continue
		}
		if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return "", err
		}
		in, err := f.Open()
		if err != nil {
			return "", err
		}
		out, err := os.Create(path)
		if err != nil {
			in.Close()
			return "", err
		}
		_, err = io.Copy(out, in)
		in.Close()
		cerr := out.Close()
		if err != nil {
			return "", err
		}
		if cerr != nil {
			return "", cerr
		}
	}
	for _, p := range []string{filepath.Join(dir, "Bin", "makensis.exe"), filepath.Join(dir, "makensis.exe")} {
		if _, err = os.Stat(p); err == nil {
			return filepath.Abs(p)
		}
	}
	return "", errors.New("NSIS ZIP has no compiler")
}

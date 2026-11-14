package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/LoTwT/youkai/internal/ocrlayout"
)

// kind tells how a dependency's downloaded bytes become files under
// resources/.
type kind int

const (
	kindFile kind = iota // the download itself is the installed file
	kindZip              // selected members of a zip archive
	kindTgz              // selected members of a gzipped tar archive
)

// member is one file to unpack from a downloaded archive.
type member struct {
	name   string // path inside the archive, slash-separated
	dest   string // path under resources/, slash-separated
	sha256 string // digest of the extracted file
}

// dependency is one pinned download. platform is the "goos/goarch" of the
// apps that need it, or "" when every platform does. Destinations follow
// MyGo's resources layout (cmd/mygo/resources.go of MyGo v0.9.0): the
// contents of platform directories such as resources/windows-amd64/ ship
// only with that platform's app, merged into the resource root, and
// everything else ships with every app.
type dependency struct {
	name     string // file name; also the cache file name and log label
	url      string // fixed source of the bytes
	sha256   string // digest of the downloaded bytes
	platform string
	kind     kind
	dest     string   // kindFile: path under resources/
	members  []member // archives: files to unpack
}

// target is one file a dependency must produce under resources/.
type target struct {
	member string // path inside the archive; "" for a plain download
	dest   string // path under resources/, slash-separated
	sha256 string
}

// targets lists the files dep installs, in manifest order.
func (d dependency) targets() []target {
	if d.kind == kindFile {
		return []target{{dest: d.dest, sha256: d.sha256}}
	}
	targets := make([]target, 0, len(d.members))
	for _, m := range d.members {
		targets = append(targets, target{member: m.name, dest: m.dest, sha256: m.sha256})
	}
	return targets
}

// deps is the fixed list of third-party resources of the first release.
// The two ORT archive digests and the libonnxruntime.1.32.0.dylib digest are
// recorded in docs/specs/t3-ocr-runtime-model-samples.md; the other member
// digests were computed from those digest-verified official archives, so
// every unpacked file is checked against what was pinned, not against
// whatever its source serves today.
var deps = []dependency{
	{
		name:     "onnxruntime-win-x64-1.32.0.zip",
		url:      "https://github.com/microsoft/onnxruntime/releases/download/v1.32.0/onnxruntime-win-x64-1.32.0.zip",
		sha256:   "aaa1d04fcf86bd31e84b90bc0cbde80e632bd0720dcc5d8ba05c4fe45c09d127",
		platform: "windows/amd64",
		kind:     kindZip,
		members: []member{
			{
				name:   "onnxruntime-win-x64-1.32.0/lib/onnxruntime.dll",
				dest:   ocrlayout.RuntimeDest("windows", "amd64"),
				sha256: "348c3d29d6995eee37e3451825eaddb7ea7400be3afda6ca2286b12bc775f042",
			},
			{
				name:   "onnxruntime-win-x64-1.32.0/lib/onnxruntime_providers_shared.dll",
				dest:   "windows-amd64/onnxruntime/onnxruntime_providers_shared.dll",
				sha256: "6caf57ac07c9cf72fb4e73c5bb0fe156f02dd60bbbd7385530c9a5654f10f950",
			},
			{
				name:   "onnxruntime-win-x64-1.32.0/LICENSE",
				dest:   "windows-amd64/onnxruntime/LICENSE",
				sha256: "c250d6278f0b47a6439fb7592b08b58a55eb9f535aa49a1db63211c3f982b674",
			},
			{
				name:   "onnxruntime-win-x64-1.32.0/ThirdPartyNotices.txt",
				dest:   "windows-amd64/onnxruntime/ThirdPartyNotices.txt",
				sha256: "bf8073fd6a112ac2a656bb60799652127e881689e51d37ad20df446cd5188b2d",
			},
		},
	},
	{
		name:     "onnxruntime-osx-arm64-1.32.0.tgz",
		url:      "https://github.com/microsoft/onnxruntime/releases/download/v1.32.0/onnxruntime-osx-arm64-1.32.0.tgz",
		sha256:   "8ddca23f532b9d4fbb279ba9b08dab6c0ac94fdc9aec2eb44a1db7049216c043",
		platform: "darwin/arm64",
		kind:     kindTgz,
		members: []member{
			{
				// The versioned dylib is the real file; lib/libonnxruntime.dylib
				// in the same archive is a same-sized duplicate, which the
				// digest below rules out unpacking by mistake.
				name:   "onnxruntime-osx-arm64-1.32.0/lib/libonnxruntime.1.32.0.dylib",
				dest:   ocrlayout.RuntimeDest("darwin", "arm64"),
				sha256: "51c5a25a3dfafd74f62f403e6e0dc7881d97c0e627c703a4d9548c5677abc8fd",
			},
			{
				name:   "onnxruntime-osx-arm64-1.32.0/LICENSE",
				dest:   "darwin-arm64/onnxruntime/LICENSE",
				sha256: "2f07c72751aed99790b8a4869cf2311df85a860b22ded05fa22803587a48922c",
			},
			{
				name:   "onnxruntime-osx-arm64-1.32.0/ThirdPartyNotices.txt",
				dest:   "darwin-arm64/onnxruntime/ThirdPartyNotices.txt",
				sha256: "d8696258afcf9df7827a5e01d6b6d55ee9b461b4e82020f5f48242deb9cc2270",
			},
		},
	},
	{
		// The recognition model every platform ships, from the ModelScope
		// URL and digest pinned in RapidOCR's default_models.yaml at commit
		// 7cfd3d9ef786a2e538ca30df700325e72e85b5f5 (tag v3.10.0).
		name:     "PP-OCRv6_rec_small.onnx",
		url:      "https://www.modelscope.cn/models/RapidAI/RapidOCR/resolve/v3.10.0/onnx/PP-OCRv6/rec/PP-OCRv6_rec_small.onnx",
		sha256:   "6f327246b50388f3c176ae304bd95767ea6dc0c9ae92153ef8cbe210b3c14884",
		platform: "",
		kind:     kindFile,
		dest:     ocrlayout.ModelDest,
	},
}

// selectDeps returns the dependencies that want selects: "all", "current"
// (resolved to host, the running machine's "goos/goarch") or one platform
// deps publishes. Common dependencies (platform "") are always included, so
// fetching a single platform still gets the model it loads.
func selectDeps(list []dependency, want, host string) ([]dependency, error) {
	if want == "current" {
		want = host
	}
	var supported []string
	for _, d := range list {
		if d.platform != "" && !slices.Contains(supported, d.platform) {
			supported = append(supported, d.platform)
		}
	}
	if want != "all" && !slices.Contains(supported, want) {
		return nil, fmt.Errorf("no dependencies are published for %q; use -platform all or one of: %s", want, strings.Join(supported, ", "))
	}
	var selected []dependency
	for _, d := range list {
		if want == "all" || d.platform == "" || d.platform == want {
			selected = append(selected, d)
		}
	}
	return selected, nil
}

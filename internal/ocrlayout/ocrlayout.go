// Package ocrlayout owns the names and repository paths of the bundled OCR
// artifacts. The packaging tool and the application both use it so a rename
// cannot silently diverge between fetchdeps and the runtime loader.
package ocrlayout

const (
	// ONNXRuntimeVersion is the pinned ONNX Runtime release.
	ONNXRuntimeVersion = "1.29.0"
	// ModelName is the single recognition model shared by both platforms.
	ModelName = "PP-OCRv6_rec_small.onnx"
	// ModelDest is the model's path under the repository resources root.
	ModelDest = "models/" + ModelName
)

// RuntimeLibName returns the pinned shared-library file name for goos, or
// an empty string when the platform has no pinned artifact.
func RuntimeLibName(goos string) string {
	switch goos {
	case "darwin":
		return "libonnxruntime." + ONNXRuntimeVersion + ".dylib"
	case "windows":
		return "onnxruntime.dll"
	default:
		return ""
	}
}

// RuntimeDest returns the runtime library's path under the repository
// resources root: the platform directory fetchdeps downloads into, which
// MyGo later merges into the packaged resource root. It returns an empty
// string for a platform without a pinned artifact.
func RuntimeDest(goos, goarch string) string {
	lib := RuntimeLibName(goos)
	if lib == "" {
		return ""
	}
	return goos + "-" + goarch + "/onnxruntime/" + lib
}

package storageconfig

// Config controls the matching, capture pacing, and persistence options
// for the Windows drive warehouse.
type Config struct {
	ReuseFrameMatch  bool `json:"reuseFrameMatch"`
	FastBadges       bool `json:"fastBadges"`
	FastBeforeClick  bool `json:"fastBeforeClick"`
	PacedDetail      bool `json:"pacedDetail"`
	BatchEvidence    bool `json:"batchEvidence"`
	SavedSnapshot    bool `json:"savedSnapshot"`
	DeferWrapRead    bool `json:"deferWrapRead"`
	ReuseScrollPages bool `json:"reuseScrollPages"`
}

// WindowsDefault reports when the normal Windows entry selects the
// drive warehouse. Explicit developer scan sources retain their own options.
func WindowsDefault(goos string, getenv func(string) string) bool {
	return goos == "windows" && getenv("YOUKAI_DEV_SOURCE") == "" && getenv("YOUKAI_GAME_SOURCE") == ""
}

// WindowsSettings is the configuration verified by the 13m01 field run.
func WindowsSettings() Config {
	return Config{
		ReuseFrameMatch:  true,
		FastBeforeClick:  true,
		PacedDetail:      true,
		BatchEvidence:    true,
		SavedSnapshot:    true,
		DeferWrapRead:    true,
		ReuseScrollPages: true,
	}
}

func ForApp(goos string, getenv func(string) string) Config {
	if WindowsDefault(goos, getenv) {
		return WindowsSettings()
	}
	return FromEnv(getenv)
}

// FromEnv keeps explicit QA sources configurable for replay and diagnostics.
func FromEnv(getenv func(string) string) Config {
	return Config{
		ReuseFrameMatch:  getenv("YOUKAI_STORAGE_MATCH_AB") == "B",
		FastBadges:       getenv("YOUKAI_STORAGE_FAST_BADGES") == "1",
		FastBeforeClick:  getenv("YOUKAI_STORAGE_FAST_BEFORE_CLICK") == "1",
		PacedDetail:      getenv("YOUKAI_STORAGE_PACED_FAST_DETAIL") == "1",
		BatchEvidence:    getenv("YOUKAI_STORAGE_BATCH_EVIDENCE") == "1",
		SavedSnapshot:    getenv("YOUKAI_ACTIVE_CHECKPOINT_SNAPSHOT") == "1",
		DeferWrapRead:    getenv("YOUKAI_DEFER_WRAP_LIVE_READ") == "1",
		ReuseScrollPages: getenv("YOUKAI_STORAGE_REUSE_SCROLL_PAGES") == "1",
	}
}

package config

import "time"

// Defaults returns the compiled-in default configuration. Every key has a
// default; AccountConfig fields are required and have empty defaults so
// validation flags missing values explicitly.
func Defaults() *Config {
	return &Config{
		Account: AccountConfig{},
		Cache: CacheConfig{
			BodyCacheMaxCount:    500,
			BodyCacheMaxBytes:    200 * 1024 * 1024,
			VacuumInterval:       7 * 24 * time.Hour,
			DoneActionsRetention: 7 * 24 * time.Hour,
			MmapSizeBytes:        256 * 1024 * 1024,
			CacheSizeKB:          64 * 1024,
		},
		Sync: SyncConfig{
			MaxConcurrent:      4,
			ForegroundInterval: 30 * time.Second,
			BackgroundInterval: 5 * time.Minute,
			BackfillDays:       90,
			MaxRetries:         5,
		},
		UI: UIConfig{
			FoldersWidth:        25,
			ListWidth:           40,
			RelativeDatesWithin: 24 * time.Hour,
			Timezone:            "Local",
		},
		Bindings: BindingsConfig{
			Quit:            "q",
			Help:            "?",
			Cmd:             ":",
			Search:          "/",
			Refresh:         "ctrl+r",
			FocusFolders:    "1",
			FocusList:       "2",
			FocusViewer:     "3",
			NextPane:        "tab",
			PrevPane:        "shift+tab",
			Up:              "k",
			Down:            "j",
			Left:            "h",
			Right:           "l",
			PageUp:          "ctrl+u",
			PageDown:        "ctrl+d",
			Home:            "g",
			End:             "G",
			Open:            "enter",
			MarkRead:        "r",
			MarkUnread:      "R",
			ToggleFlag:      "f",
			Delete:          "d",
			PermanentDelete: "D",
			Archive:         "a",
			Move:            "m",
			AddCategory:     "c",
			RemoveCategory:  "C",
			Undo:            "u",
			UndoStack:       "U",
			Filter:          "F",
			ClearFilter:     "esc",
			ApplyToFiltered: ";",
		},
		Rendering: RenderingConfig{
			ShowFullHeaders: false,
			OpenBrowserCmd:  "open",
			HTMLMaxBytes:    1024 * 1024,
		},
		Logging: LoggingConfig{
			Level:   "info",
			Path:    "",
			MaxSize: 10,
		},
	}
}

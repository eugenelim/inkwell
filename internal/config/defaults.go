package config

import "time"

// Defaults returns the compiled-in default configuration. The [account]
// section ships with the locked Microsoft-Graph-CLI-Tools first-party
// client and the multi-tenant /common authority (PRD §4). The user's
// UPN is left empty until first sign-in resolves it.
func Defaults() *Config {
	return &Config{
		Account: AccountConfig{
			TenantID:   "common",
			ClientID:   "14d82eec-204b-4c2f-b7e8-296a70dab67e",
			SignInMode: "auto",
		},
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
			Theme:               "default",
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
			Filter:          "F",
			ClearFilter:     "esc",
			ApplyToFiltered: ";",
			Unsubscribe:     "U",
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

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
			Quit:         "q",
			Help:         "?",
			Cmd:          ":",
			Search:       "/",
			Refresh:      "ctrl+r",
			FocusFolders: "1",
			FocusList:    "2",
			FocusViewer:  "3",
			NextPane:     "tab",
			PrevPane:     "shift+tab",
			// Movement keys ship with both vi-style + arrow alternates
			// (and PageUp/PageDown with both ctrl- + paging variants)
			// so non-vim users aren't forced into hjkl. Comma-
			// separated values are parsed by ApplyBindingOverrides.
			Up:              "k,up",
			Down:            "j,down",
			Left:            "h,left",
			Right:           "l,right",
			PageUp:          "ctrl+u,pgup",
			PageDown:        "ctrl+d,pgdown",
			Home:            "g,home",
			End:             "G,end",
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
		Triage: TriageConfig{
			ConfirmPermanentDelete: true,
			UndoStackSize:          0, // 0 = unlimited; v1 doesn't enforce
			RecentFoldersCount:     5,
		},
		Bulk: BulkConfig{
			ProgressThreshold: 50,
			PreviewSampleSize: 20,
			SizeWarnThreshold: 1000,
			SizeHardMax:       5000,
			DryRunDefault:     false,
		},
		Calendar: CalendarConfig{
			LookaheadDays: 1,
			LookbackDays:  0,
			ShowDeclined:  false,
			CacheTTL:      15 * time.Minute,
		},
	}
}

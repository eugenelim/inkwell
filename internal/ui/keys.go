package ui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
)

// BindingOverrides is the consumer-site shape of config.BindingsConfig.
// Defined here so the UI doesn't import internal/config (CLAUDE.md
// §2). Each field is the override key string ("d", "ctrl+d", etc.);
// empty means "leave default".
type BindingOverrides struct {
	Quit            string
	Help            string
	Cmd             string
	Search          string
	Refresh         string
	FocusFolders    string
	FocusList       string
	FocusViewer     string
	NextPane        string
	PrevPane        string
	Up              string
	Down            string
	Left            string
	Right           string
	PageUp          string
	PageDown        string
	Home            string
	End             string
	Open            string
	MarkRead        string
	MarkUnread      string
	ToggleFlag      string
	Delete          string
	PermanentDelete string
	Archive         string
	Move            string
	AddCategory     string
	RemoveCategory  string
	Undo            string
	Filter          string
	ClearFilter     string
	ApplyToFiltered string
	Unsubscribe     string
}

// KeyMap is the application-wide keyboard contract. The UI's Update
// dispatches in Mode order; pane-scoped meanings (e.g. `r` = reply in
// viewer, `r` = mark-read in list) are resolved by the focused pane,
// not by separate bindings.
type KeyMap struct {
	// Global
	Quit    key.Binding
	Help    key.Binding
	Cmd     key.Binding
	Search  key.Binding
	Refresh key.Binding

	// Pane focus
	FocusFolders key.Binding
	FocusList    key.Binding
	FocusViewer  key.Binding
	NextPane     key.Binding
	PrevPane     key.Binding

	// Movement
	Up       key.Binding
	Down     key.Binding
	Left     key.Binding
	Right    key.Binding
	PageUp   key.Binding
	PageDown key.Binding
	Home     key.Binding
	End      key.Binding
	Open     key.Binding
	// Toggle expansion of a folder with children (folders pane only).
	Expand key.Binding

	// List actions (pane-scoped — list, viewer)
	MarkRead        key.Binding
	MarkUnread      key.Binding
	ToggleFlag      key.Binding
	Delete          key.Binding
	PermanentDelete key.Binding
	Archive         key.Binding
	Move            key.Binding
	AddCategory     key.Binding
	RemoveCategory  key.Binding

	// Undo
	Undo key.Binding

	// Filter / bulk
	Filter          key.Binding
	ClearFilter     key.Binding
	ApplyToFiltered key.Binding

	// Unsubscribe (spec 16). Capital U so it never collides with j/k
	// movement or single-letter triage keys.
	Unsubscribe key.Binding
}

// DefaultKeyMap returns the spec §5 default bindings. Tests use this;
// production wires user overrides from `[bindings]` config.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c")),
		Help:    key.NewBinding(key.WithKeys("?")),
		Cmd:     key.NewBinding(key.WithKeys(":")),
		Search:  key.NewBinding(key.WithKeys("/")),
		Refresh: key.NewBinding(key.WithKeys("ctrl+r")),

		FocusFolders: key.NewBinding(key.WithKeys("1")),
		FocusList:    key.NewBinding(key.WithKeys("2")),
		FocusViewer:  key.NewBinding(key.WithKeys("3")),
		NextPane:     key.NewBinding(key.WithKeys("tab")),
		PrevPane:     key.NewBinding(key.WithKeys("shift+tab")),

		Up:       key.NewBinding(key.WithKeys("k", "up")),
		Down:     key.NewBinding(key.WithKeys("j", "down")),
		Left:     key.NewBinding(key.WithKeys("h", "left")),
		Right:    key.NewBinding(key.WithKeys("l", "right")),
		PageUp:   key.NewBinding(key.WithKeys("ctrl+u", "pgup")),
		PageDown: key.NewBinding(key.WithKeys("ctrl+d", "pgdown")),
		Home:     key.NewBinding(key.WithKeys("home", "g")),
		End:      key.NewBinding(key.WithKeys("end", "G")),
		Open:     key.NewBinding(key.WithKeys("enter")),
		Expand:   key.NewBinding(key.WithKeys("o", " ")),

		MarkRead:        key.NewBinding(key.WithKeys("r")),
		MarkUnread:      key.NewBinding(key.WithKeys("R")),
		ToggleFlag:      key.NewBinding(key.WithKeys("f")),
		Delete:          key.NewBinding(key.WithKeys("d")),
		PermanentDelete: key.NewBinding(key.WithKeys("D")),
		Archive:         key.NewBinding(key.WithKeys("a")),
		Move:            key.NewBinding(key.WithKeys("m")),
		AddCategory:     key.NewBinding(key.WithKeys("c")),
		RemoveCategory:  key.NewBinding(key.WithKeys("C")),

		Undo: key.NewBinding(key.WithKeys("u")),

		Filter:          key.NewBinding(key.WithKeys("F")),
		ClearFilter:     key.NewBinding(key.WithKeys("esc")),
		ApplyToFiltered: key.NewBinding(key.WithKeys(";")),

		Unsubscribe: key.NewBinding(key.WithKeys("U")),
	}
}

// ApplyBindingOverrides folds user TOML overrides into a base KeyMap.
// Each non-empty field replaces the default keys for that binding;
// empty fields leave the default in place. Returns a typed error if
// any field has an invalid form (Bubble Tea's `key.NewBinding` is
// permissive, so the only structural failure mode is an empty
// override string for a field meant to be set — captured upstream
// by the config decode step).
//
// Spec 04 §17 invariant: a typo in `[bindings]` (decoded into a
// non-existent struct field) is rejected at config Load time via
// the BurntSushi/toml MetaData.Undecoded() check; this function
// translates the strings → bindings without any further name-level
// validation. The two layers compose into "unknown name = config
// load error; valid name + invalid key string = unreachable from a
// happy TOML path".
func ApplyBindingOverrides(km KeyMap, o BindingOverrides) (KeyMap, error) {
	apply := func(target *key.Binding, override string) {
		if override == "" {
			return
		}
		*target = key.NewBinding(key.WithKeys(override))
	}
	apply(&km.Quit, o.Quit)
	apply(&km.Help, o.Help)
	apply(&km.Cmd, o.Cmd)
	apply(&km.Search, o.Search)
	apply(&km.Refresh, o.Refresh)
	apply(&km.FocusFolders, o.FocusFolders)
	apply(&km.FocusList, o.FocusList)
	apply(&km.FocusViewer, o.FocusViewer)
	apply(&km.NextPane, o.NextPane)
	apply(&km.PrevPane, o.PrevPane)
	apply(&km.Up, o.Up)
	apply(&km.Down, o.Down)
	apply(&km.Left, o.Left)
	apply(&km.Right, o.Right)
	apply(&km.PageUp, o.PageUp)
	apply(&km.PageDown, o.PageDown)
	apply(&km.Home, o.Home)
	apply(&km.End, o.End)
	apply(&km.Open, o.Open)
	apply(&km.MarkRead, o.MarkRead)
	apply(&km.MarkUnread, o.MarkUnread)
	apply(&km.ToggleFlag, o.ToggleFlag)
	apply(&km.Delete, o.Delete)
	apply(&km.PermanentDelete, o.PermanentDelete)
	apply(&km.Archive, o.Archive)
	apply(&km.Move, o.Move)
	apply(&km.AddCategory, o.AddCategory)
	apply(&km.RemoveCategory, o.RemoveCategory)
	apply(&km.Undo, o.Undo)
	apply(&km.Filter, o.Filter)
	apply(&km.ClearFilter, o.ClearFilter)
	apply(&km.ApplyToFiltered, o.ApplyToFiltered)
	apply(&km.Unsubscribe, o.Unsubscribe)
	// Reject duplicate bindings — two actions on the same key would
	// silently lose one. Common typo: copy-paste the same value
	// across two fields.
	if dup := findDuplicateBinding(km); dup != "" {
		return km, fmt.Errorf("bindings: key %q bound to multiple actions", dup)
	}
	return km, nil
}

// findDuplicateBinding scans the KeyMap for any single key string
// bound to >1 action and returns the offender, or "". Keys-on-
// different-panes are NOT duplicates here (e.g. `r` is reply in the
// viewer and mark-read in the list — same KeyMap field MarkRead,
// different runtime dispatch); this checks for distinct fields
// sharing the same key string.
func findDuplicateBinding(km KeyMap) string {
	seen := make(map[string]string)
	check := func(name string, b key.Binding) string {
		for _, k := range b.Keys() {
			if other, hit := seen[k]; hit && other != name {
				return k
			}
			seen[k] = name
		}
		return ""
	}
	checks := []struct {
		name string
		b    key.Binding
	}{
		{"quit", km.Quit}, {"help", km.Help}, {"cmd", km.Cmd},
		{"search", km.Search}, {"refresh", km.Refresh},
		{"focus_folders", km.FocusFolders}, {"focus_list", km.FocusList}, {"focus_viewer", km.FocusViewer},
		{"next_pane", km.NextPane}, {"prev_pane", km.PrevPane},
		{"page_up", km.PageUp}, {"page_down", km.PageDown},
		{"home", km.Home}, {"end", km.End}, {"open", km.Open},
		// Movement (j/k/h/l) intentionally NOT in this set — they
		// pane-scope to different actions and re-binding them is
		// unusual but legitimate.
		// MarkRead vs Reply share `r` legitimately (pane-scoped); same for ToggleFlag.
		{"delete", km.Delete}, {"permanent_delete", km.PermanentDelete},
		{"archive", km.Archive}, {"move", km.Move},
		{"add_category", km.AddCategory}, {"remove_category", km.RemoveCategory},
		{"undo", km.Undo},
		{"filter", km.Filter}, {"apply_to_filtered", km.ApplyToFiltered},
		{"unsubscribe", km.Unsubscribe},
	}
	for _, c := range checks {
		if dup := check(c.name, c.b); dup != "" {
			return dup
		}
	}
	return ""
}

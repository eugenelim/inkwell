package ui

import "github.com/charmbracelet/bubbles/key"

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

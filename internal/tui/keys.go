package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap holds every binding the TUI reacts to.
type KeyMap struct {
	Up        key.Binding
	Down      key.Binding
	Top       key.Binding
	Bottom    key.Binding
	Filter    key.Binding
	Escape    key.Binding
	Toggle    key.Binding
	SelectAll key.Binding
	ClearAll  key.Binding
	Update    key.Binding
	Outdated  key.Binding
	Reset     key.Binding
	Refresh   key.Binding
	Help      key.Binding
	Quit      key.Binding
	Confirm   key.Binding
	Deny      key.Binding
}

// DefaultKeyMap returns the bindings described in the README.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up:        key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:      key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Top:       key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g", "top")),
		Bottom:    key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("G", "bottom")),
		Filter:    key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		Escape:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Toggle:    key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "select")),
		SelectAll: key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "select all")),
		ClearAll:  key.NewBinding(key.WithKeys("A"), key.WithHelp("A", "clear selection")),
		Update:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "update")),
		Outdated:  key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "check outdated")),
		Reset:     key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "remove pin")),
		Refresh:   key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "reload")),
		Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:      key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Confirm:   key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "confirm")),
		Deny:      key.NewBinding(key.WithKeys("n", "esc"), key.WithHelp("n", "cancel")),
	}
}

// ShortHelp implements help.KeyMap.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Filter, k.Toggle, k.Update, k.Outdated, k.Help, k.Quit}
}

// FullHelp implements help.KeyMap.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Top, k.Bottom},
		{k.Filter, k.Escape, k.Toggle, k.SelectAll, k.ClearAll},
		{k.Update, k.Outdated, k.Reset, k.Refresh},
		{k.Help, k.Quit},
	}
}

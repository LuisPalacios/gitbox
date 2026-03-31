package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap defines the shared keybindings for the TUI.
type KeyMap struct {
	Up      key.Binding
	Down    key.Binding
	Enter   key.Binding
	Back    key.Binding
	Quit    key.Binding
	Help    key.Binding
	Tab     key.Binding
	Refresh key.Binding
	Reload  key.Binding
	Delete  key.Binding
	Add     key.Binding
	Theme   key.Binding
}

// Keys is the global key map.
var Keys = KeyMap{
	Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Enter:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
	Back:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	Quit:    key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
	Help:    key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Tab:     key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "section")),
	Refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	Reload:  key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "reload config")),
	Delete:  key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
	Add:     key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add")),
	Theme:   key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "theme")),
}

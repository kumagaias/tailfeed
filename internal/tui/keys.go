package tui

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Up         key.Binding
	Down       key.Binding
	Left       key.Binding
	Right      key.Binding
	PrevGroup  key.Binding // [
	NextGroup  key.Binding // ]
	GotoBottom key.Binding // Shift+G
	GotoTopG   key.Binding // first g (gg detection)
	PageDown   key.Binding // Ctrl+F
	PageUp     key.Binding // Ctrl+B
	Command    key.Binding
	Open       key.Binding
	MarkRead   key.Binding
	Quit       key.Binding
	Confirm    key.Binding
	Cancel     key.Binding
	ViewDetail key.Binding // v — toggle detail pane
	Delete     key.Binding // d — delete selected feed in list overlay
}

var keys = keyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	Left: key.NewBinding(
		key.WithKeys("left", "h"),
		key.WithHelp("←/h", "prev article"),
	),
	Right: key.NewBinding(
		key.WithKeys("right", "l"),
		key.WithHelp("→/l", "next article"),
	),
	PrevGroup: key.NewBinding(
		key.WithKeys("["),
		key.WithHelp("[", "prev group"),
	),
	NextGroup: key.NewBinding(
		key.WithKeys("]"),
		key.WithHelp("]", "next group"),
	),
	GotoBottom: key.NewBinding(
		key.WithKeys("G"),
		key.WithHelp("G", "newest"),
	),
	GotoTopG: key.NewBinding(
		key.WithKeys("g"),
		key.WithHelp("gg", "oldest"),
	),
	PageDown: key.NewBinding(
		key.WithKeys("ctrl+f"),
		key.WithHelp("^F", "page down"),
	),
	PageUp: key.NewBinding(
		key.WithKeys("ctrl+b"),
		key.WithHelp("^B", "page up"),
	),
	Command: key.NewBinding(
		key.WithKeys("/"),
		key.WithHelp("/", "command"),
	),
	Open: key.NewBinding(
		key.WithKeys("enter", "o"),
		key.WithHelp("enter", "open link"),
	),
	MarkRead: key.NewBinding(
		key.WithKeys("m"),
		key.WithHelp("m", "mark read"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q"),
		key.WithHelp("q", "quit"),
	),
	Confirm: key.NewBinding(
		key.WithKeys("enter"),
	),
	Cancel: key.NewBinding(
		key.WithKeys("esc", "ctrl+c"),
		key.WithHelp("esc/^C", "cancel"),
	),
	ViewDetail: key.NewBinding(
		key.WithKeys("v"),
		key.WithHelp("v", "detail"),
	),
	Delete: key.NewBinding(
		key.WithKeys("d"),
		key.WithHelp("d", "delete feed"),
	),
}

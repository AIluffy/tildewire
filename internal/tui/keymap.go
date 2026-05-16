package tui

import "charm.land/bubbles/v2/key"

type keyMap struct {
	Up           key.Binding
	Down         key.Binding
	FocusLeft    key.Binding
	FocusRight   key.Binding
	SourceUp     key.Binding
	SourceDown   key.Binding
	PreviewUp    key.Binding
	PreviewDown  key.Binding
	OpenDetail   key.Binding
	Back         key.Binding
	Quit         key.Binding
	All          key.Binding
	GitHub       key.Binding
	HackerNews   key.Binding
	HuggingFace  key.Binding
	Lobsters     key.Binding
	ProductHunt  key.Binding
	Scope        key.Binding
	Search       key.Binding
	Filter       key.Binding
	Palette      key.Binding
	Settings     key.Binding
	Refresh      key.Binding
	Save         key.Binding
	MarkRead     key.Binding
	MarkUnread   key.Binding
	Hide         key.Binding
	OpenURL      key.Binding
	OpenSource   key.Binding
	CopyURL      key.Binding
	CopyMarkdown key.Binding
	Health       key.Binding
	Help         key.Binding
}

func defaultKeyMap() keyMap {
	return keyMap{
		Up:           key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k/up", "up")),
		Down:         key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/down", "down")),
		FocusLeft:    key.NewBinding(key.WithKeys("left"), key.WithHelp("left", "focus left")),
		FocusRight:   key.NewBinding(key.WithKeys("right"), key.WithHelp("right", "focus right")),
		SourceUp:     key.NewBinding(key.WithKeys("K", "shift+up"), key.WithHelp("K", "source up")),
		SourceDown:   key.NewBinding(key.WithKeys("J", "shift+down"), key.WithHelp("J", "source down")),
		PreviewUp:    key.NewBinding(key.WithKeys("pgup", "ctrl+u"), key.WithHelp("pgup", "preview up")),
		PreviewDown:  key.NewBinding(key.WithKeys("pgdown", "ctrl+d"), key.WithHelp("pgdn", "preview down")),
		OpenDetail:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "detail")),
		Back:         key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Quit:         key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		All:          key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "all")),
		GitHub:       key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "github")),
		HackerNews:   key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "hn")),
		HuggingFace:  key.NewBinding(key.WithKeys("3"), key.WithHelp("3", "hf")),
		Lobsters:     key.NewBinding(key.WithKeys("4"), key.WithHelp("4", "lobsters")),
		ProductHunt:  key.NewBinding(key.WithKeys("5"), key.WithHelp("5", "product hunt")),
		Scope:        key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "scope")),
		Search:       key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
		Filter:       key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "filter")),
		Palette:      key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "palette")),
		Settings:     key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "config")),
		Refresh:      key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Save:         key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "save")),
		MarkRead:     key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "read")),
		MarkUnread:   key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "unread")),
		Hide:         key.NewBinding(key.WithKeys("h"), key.WithHelp("h", "hide")),
		OpenURL:      key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open")),
		OpenSource:   key.NewBinding(key.WithKeys("O"), key.WithHelp("O", "source")),
		CopyURL:      key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "copy")),
		CopyMarkdown: key.NewBinding(key.WithKeys("Y"), key.WithHelp("Y", "md link")),
		Health:       key.NewBinding(key.WithKeys("!"), key.WithHelp("!", "health")),
		Help:         key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.FocusLeft, k.FocusRight, k.Up, k.Down, k.SourceUp, k.SourceDown, k.PreviewUp, k.PreviewDown, k.OpenDetail, k.Scope, k.Search, k.Filter, k.Palette, k.Refresh, k.Save, k.Hide, k.Health, k.Quit, k.Help}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.FocusLeft, k.FocusRight, k.Up, k.Down, k.SourceUp, k.SourceDown, k.PreviewUp, k.PreviewDown, k.OpenDetail, k.Back},
		{k.All, k.GitHub, k.HackerNews, k.HuggingFace, k.Lobsters, k.ProductHunt, k.Scope, k.Search, k.Filter, k.Palette, k.Settings, k.Refresh},
		{k.Save, k.MarkRead, k.MarkUnread, k.Hide},
		{k.OpenURL, k.OpenSource, k.CopyURL, k.CopyMarkdown, k.Health, k.Quit},
	}
}

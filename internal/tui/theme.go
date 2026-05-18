package tui

import (
	"strings"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/AIluffy/tildewire/internal/config"
	"github.com/AIluffy/tildewire/internal/domain"
	md "github.com/AIluffy/tildewire/internal/tui/markdown"
)

type themeSpec struct {
	value       string
	label       string
	glamour     string
	text        string
	muted       string
	subtle      string
	accent      string
	accentAlt   string
	active      string
	search      string
	ok          string
	warn        string
	err         string
	toastText   string
	toastBg     string
	codeText    string
	github      string
	hackerNews  string
	huggingFace string
	lobsters    string
	productHunt string
}

type themeStyles struct {
	spec      themeSpec
	header    lipgloss.Style
	title     lipgloss.Style
	muted     lipgloss.Style
	searchHit lipgloss.Style
	ok        lipgloss.Style
	warn      lipgloss.Style
	err       lipgloss.Style
	toast     lipgloss.Style
	panel     lipgloss.Style
	focus     lipgloss.Style
	active    lipgloss.Style
	control   lipgloss.Style
	sources   map[domain.SourceID]lipgloss.Style
	markdown  md.Theme
}

var themeSpecs = []themeSpec{
	{value: config.ThemeCatppuccin, label: "Catppuccin", glamour: "dark", text: "#CDD6F4", muted: "#6C7086", subtle: "#9399B2", accent: "#CBA6F7", accentAlt: "#89B4FA", active: "#89B4FA", search: "#F9E2AF", ok: "#A6E3A1", warn: "#FAB387", err: "#F38BA8", toastText: "#11111B", toastBg: "#89B4FA", codeText: "#CDD6F4", github: "#CBA6F7", hackerNews: "#FAB387", huggingFace: "#F9E2AF", lobsters: "#A6E3A1", productHunt: "#F5C2E7"},
	{value: config.ThemeDracula, label: "Dracula", glamour: "dracula", text: "#F8F8F2", muted: "#6272A4", subtle: "#BD93F9", accent: "#BD93F9", accentAlt: "#8BE9FD", active: "#8BE9FD", search: "#F1FA8C", ok: "#50FA7B", warn: "#FFB86C", err: "#FF5555", toastText: "#282A36", toastBg: "#8BE9FD", codeText: "#F8F8F2", github: "#BD93F9", hackerNews: "#FFB86C", huggingFace: "#F1FA8C", lobsters: "#50FA7B", productHunt: "#FF79C6"},
	{value: config.ThemeGruvbox, label: "Gruvbox", glamour: "dark", text: "#EBDBB2", muted: "#928374", subtle: "#A89984", accent: "#D3869B", accentAlt: "#83A598", active: "#83A598", search: "#FABD2F", ok: "#B8BB26", warn: "#FE8019", err: "#FB4934", toastText: "#282828", toastBg: "#83A598", codeText: "#EBDBB2", github: "#D3869B", hackerNews: "#FE8019", huggingFace: "#FABD2F", lobsters: "#B8BB26", productHunt: "#FB4934"},
	{value: config.ThemeNord, label: "Nord", glamour: "dark", text: "#D8DEE9", muted: "#4C566A", subtle: "#81A1C1", accent: "#88C0D0", accentAlt: "#81A1C1", active: "#81A1C1", search: "#EBCB8B", ok: "#A3BE8C", warn: "#D08770", err: "#BF616A", toastText: "#2E3440", toastBg: "#88C0D0", codeText: "#D8DEE9", github: "#B48EAD", hackerNews: "#D08770", huggingFace: "#EBCB8B", lobsters: "#A3BE8C", productHunt: "#BF616A"},
	{value: config.ThemeTokyoNight, label: "Tokyo Night", glamour: "tokyo-night", text: "#A9B1D6", muted: "#565F89", subtle: "#7AA2F7", accent: "#BB9AF7", accentAlt: "#7AA2F7", active: "#7AA2F7", search: "#E0AF68", ok: "#9ECE6A", warn: "#FF9E64", err: "#F7768E", toastText: "#1A1B26", toastBg: "#7AA2F7", codeText: "#A9B1D6", github: "#BB9AF7", hackerNews: "#FF9E64", huggingFace: "#E0AF68", lobsters: "#9ECE6A", productHunt: "#F7768E"},
	{value: config.ThemeSolarizedDark, label: "Solarized Dark", glamour: "dark", text: "#93A1A1", muted: "#586E75", subtle: "#839496", accent: "#268BD2", accentAlt: "#2AA198", active: "#2AA198", search: "#B58900", ok: "#859900", warn: "#CB4B16", err: "#DC322F", toastText: "#002B36", toastBg: "#2AA198", codeText: "#93A1A1", github: "#6C71C4", hackerNews: "#CB4B16", huggingFace: "#B58900", lobsters: "#859900", productHunt: "#D33682"},
	{value: config.ThemeOneDark, label: "One Dark", glamour: "dark", text: "#ABB2BF", muted: "#5C6370", subtle: "#61AFEF", accent: "#C678DD", accentAlt: "#61AFEF", active: "#61AFEF", search: "#E5C07B", ok: "#98C379", warn: "#D19A66", err: "#E06C75", toastText: "#282C34", toastBg: "#61AFEF", codeText: "#ABB2BF", github: "#C678DD", hackerNews: "#D19A66", huggingFace: "#E5C07B", lobsters: "#98C379", productHunt: "#E06C75"},
	{value: config.ThemeEverforest, label: "Everforest", glamour: "dark", text: "#D3C6AA", muted: "#859289", subtle: "#A7C080", accent: "#A7C080", accentAlt: "#7FBBB3", active: "#7FBBB3", search: "#DBBC7F", ok: "#A7C080", warn: "#E69875", err: "#E67E80", toastText: "#2D353B", toastBg: "#7FBBB3", codeText: "#D3C6AA", github: "#D699B6", hackerNews: "#E69875", huggingFace: "#DBBC7F", lobsters: "#A7C080", productHunt: "#E67E80"},
	{value: config.ThemeRosePine, label: "Rose Pine", glamour: "dark", text: "#E0DEF4", muted: "#6E6A86", subtle: "#9CCFD8", accent: "#C4A7E7", accentAlt: "#9CCFD8", active: "#9CCFD8", search: "#F6C177", ok: "#31748F", warn: "#EBBCBA", err: "#EB6F92", toastText: "#191724", toastBg: "#9CCFD8", codeText: "#E0DEF4", github: "#C4A7E7", hackerNews: "#F6C177", huggingFace: "#EBBCBA", lobsters: "#31748F", productHunt: "#EB6F92"},
	{value: config.ThemeMonokai, label: "Monokai", glamour: "dark", text: "#F8F8F2", muted: "#75715E", subtle: "#66D9EF", accent: "#AE81FF", accentAlt: "#66D9EF", active: "#66D9EF", search: "#E6DB74", ok: "#A6E22E", warn: "#FD971F", err: "#F92672", toastText: "#272822", toastBg: "#66D9EF", codeText: "#F8F8F2", github: "#AE81FF", hackerNews: "#FD971F", huggingFace: "#E6DB74", lobsters: "#A6E22E", productHunt: "#F92672"},
}

func themeStylesFor(value string) themeStyles {
	spec := themeSpecFor(value)
	panel := lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color(spec.subtle)).Padding(0, 1)
	return themeStyles{
		spec:      spec,
		header:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(spec.accent)),
		title:     lipgloss.NewStyle().Bold(true),
		muted:     lipgloss.NewStyle().Foreground(lipgloss.Color(spec.muted)),
		searchHit: lipgloss.NewStyle().Foreground(lipgloss.Color(spec.search)).Bold(true),
		ok:        lipgloss.NewStyle().Foreground(lipgloss.Color(spec.ok)),
		warn:      lipgloss.NewStyle().Foreground(lipgloss.Color(spec.warn)),
		err:       lipgloss.NewStyle().Foreground(lipgloss.Color(spec.err)),
		toast:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(spec.toastText)).Background(lipgloss.Color(spec.toastBg)).Padding(0, 1),
		panel:     panel,
		focus:     panel.BorderForeground(lipgloss.Color(spec.active)),
		active:    lipgloss.NewStyle().Foreground(lipgloss.Color(spec.active)).Bold(true),
		control:   lipgloss.NewStyle().Foreground(lipgloss.Color(spec.text)).Bold(true),
		sources: map[domain.SourceID]lipgloss.Style{
			domain.SourceGitHub:      lipgloss.NewStyle().Foreground(lipgloss.Color(spec.github)).Bold(true),
			domain.SourceHackerNews:  lipgloss.NewStyle().Foreground(lipgloss.Color(spec.hackerNews)).Bold(true),
			domain.SourceAILabs:      lipgloss.NewStyle().Foreground(lipgloss.Color(spec.accent)).Bold(true),
			domain.SourceHuggingFace: lipgloss.NewStyle().Foreground(lipgloss.Color(spec.huggingFace)).Bold(true),
			domain.SourceLobsters:    lipgloss.NewStyle().Foreground(lipgloss.Color(spec.lobsters)).Bold(true),
			domain.SourceProductHunt: lipgloss.NewStyle().Foreground(lipgloss.Color(spec.productHunt)).Bold(true),
		},
		markdown: md.Theme{
			Accent:   spec.accent,
			Muted:    spec.muted,
			Text:     spec.text,
			CodeText: spec.codeText,
		},
	}
}

func themeSpecFor(value string) themeSpec {
	value = strings.TrimSpace(value)
	for _, spec := range themeSpecs {
		if spec.value == value {
			return spec
		}
	}
	return themeSpecs[0]
}

func themeSettingsOptions() []settingsOption {
	options := make([]settingsOption, 0, len(themeSpecs))
	for _, spec := range themeSpecs {
		options = append(options, settingsOption{Label: spec.label, Value: spec.value})
	}
	return options
}

func themePaletteCommands() []paletteCommand {
	commands := make([]paletteCommand, 0, len(themeSpecs))
	for _, spec := range themeSpecs {
		spec := spec
		commands = append(commands, paletteCommand{
			label:    "Theme: " + spec.label,
			keywords: "theme color palette " + spec.value + " " + strings.ToLower(spec.label),
			run: func(m Model) (Model, tea.Cmd) {
				m.applyTheme(spec.value)
				m.message = "theme: " + spec.label
				return m, m.saveSettingsCmd(m.config)
			},
		})
	}
	return commands
}

func (m *Model) applyTheme(value string) {
	styles := themeStylesFor(value)
	m.config.Theme = styles.spec.value
	m.config.GlamourStyle = styles.spec.glamour
	m.styles = styles
	m.loadingSpinner = spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(styles.active))
	m.clearDetailContentCache()
	m.refreshDetailContentCache()
}

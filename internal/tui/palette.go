package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/AIluffy/tildewire/internal/app"
	"github.com/AIluffy/tildewire/internal/domain"
)

type paletteCommand struct {
	label    string
	keywords string
	run      func(Model) (Model, tea.Cmd)
}

func (m *Model) openPalette() {
	m.paletteOpen = true
	m.paletteFilter = ""
	m.paletteCursor = 0
	m.paletteOffset = 0
	m.filterOpen = false
	m.health = false
	m.rulesOpen = false
	m.dedupeOpen = false
	m.message = "command palette"
}

func (m Model) handlePaletteKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	keyText := msg.String()
	if msg.Text != "" {
		keyText = msg.Text
	}
	switch keyText {
	case "enter":
		commands := m.filteredPaletteCommands()
		if len(commands) == 0 {
			return m, nil
		}
		command := commands[clamp(m.paletteCursor, 0, len(commands)-1)]
		m.paletteOpen = false
		m.paletteFilter = ""
		m.paletteCursor = 0
		return command.run(m)
	case "esc":
		m.paletteOpen = false
		m.message = "palette cancelled"
		return m, nil
	case "j", "down":
		m.movePaletteCursor(1)
	case "k", "up":
		m.movePaletteCursor(-1)
	case "backspace", "ctrl+h":
		runes := []rune(m.paletteFilter)
		if len(runes) > 0 {
			m.paletteFilter = string(runes[:len(runes)-1])
			m.clampPaletteCursor()
		}
	case "ctrl+u":
		m.paletteFilter = ""
		m.clampPaletteCursor()
	default:
		if msg.Text != "" {
			m.paletteFilter += msg.Text
			m.clampPaletteCursor()
		}
	}
	m.message = "palette: " + m.paletteFilter
	return m, nil
}

func (m Model) paletteCommands() []paletteCommand {
	hiddenActionLabel := "Hide item"
	if entry, ok := m.selected(); ok && entry.State.Hidden {
		hiddenActionLabel = "Restore item"
	}
	commands := []paletteCommand{
		{label: "Export saved Markdown", keywords: "export saved markdown md", run: func(m Model) (Model, tea.Cmd) {
			return m, m.exportSavedCmd(app.ExportMarkdown)
		}},
		{label: "Export saved JSON", keywords: "export saved json", run: func(m Model) (Model, tea.Cmd) {
			return m, m.exportSavedCmd(app.ExportJSON)
		}},
		{label: "Export saved CSV", keywords: "export saved csv", run: func(m Model) (Model, tea.Cmd) {
			return m, m.exportSavedCmd(app.ExportCSV)
		}},
		{label: "Refresh sources", keywords: "refresh reload sources", run: func(m Model) (Model, tea.Cmd) {
			return m.startRefresh(true, app.RefreshModeVisible)
		}},
		{label: "Clear cache", keywords: "clear cache purge reset cached data", run: func(m Model) (Model, tea.Cmd) {
			return m, m.clearCacheCmd()
		}},
		{label: "Show hidden items", keywords: "show hidden include hidden restore unhide all items", run: func(m Model) (Model, tea.Cmd) {
			m.filter.IncludeHidden = true
			m.message = "showing hidden items"
			return m, m.loadWithMessageCmd(m.message)
		}},
		{label: "Open item URL", keywords: "open item url", run: func(m Model) (Model, tea.Cmd) {
			entry, ok := m.selected()
			if !ok {
				return m, nil
			}
			return m, m.openItemURLCmd(entry)
		}},
		{label: "Copy item URL", keywords: "copy item url", run: func(m Model) (Model, tea.Cmd) {
			entry, ok := m.selected()
			if !ok {
				return m, nil
			}
			return m, m.copyItemURLCmd(entry)
		}},
		{label: "Save or unsave item", keywords: "save unsave item", run: func(m Model) (Model, tea.Cmd) {
			entry, ok := m.selected()
			if !ok {
				return m, nil
			}
			return m, m.setSavedCmd(entry.Item.ID, !entry.State.Saved)
		}},
		{label: "Mark item read", keywords: "read item", run: func(m Model) (Model, tea.Cmd) {
			entry, ok := m.selected()
			if !ok {
				return m, nil
			}
			return m, m.setReadCmd(entry.Item.ID, true)
		}},
		{label: hiddenActionLabel, keywords: "hide restore item unhide", run: func(m Model) (Model, tea.Cmd) {
			entry, ok := m.selected()
			if !ok {
				return m, nil
			}
			return m, m.setHiddenCmd(entry.Item.ID, !entry.State.Hidden)
		}},
		{label: "Boost from current item", keywords: "boost personalize rule current item", run: func(m Model) (Model, tea.Cmd) {
			rule, ok := m.ruleFromCurrentItem(domain.RuleEffectBoost)
			if !ok {
				m.message = "no rule target"
				return m, nil
			}
			return m, m.createRuleCmd(rule)
		}},
		{label: "Mute from current item", keywords: "mute personalize rule current item", run: func(m Model) (Model, tea.Cmd) {
			rule, ok := m.ruleFromCurrentItem(domain.RuleEffectMute)
			if !ok {
				m.message = "no rule target"
				return m, nil
			}
			return m, m.createRuleCmd(rule)
		}},
		{label: "Hide similar items", keywords: "hide personalize rule current item", run: func(m Model) (Model, tea.Cmd) {
			rule, ok := m.ruleFromCurrentItem(domain.RuleEffectHide)
			if !ok {
				m.message = "no rule target"
				return m, nil
			}
			return m, m.createRuleCmd(rule)
		}},
		{label: "Personalization rules", keywords: "rules personalization boost mute hide", run: func(m Model) (Model, tea.Cmd) {
			m.openRulesPanel()
			return m, nil
		}},
		{label: "Add personalization rule", keywords: "add personalization rule boost mute hide", run: func(m Model) (Model, tea.Cmd) {
			m.openRuleForm(0, domain.PersonalizationRule{})
			return m, m.ruleForm.Init()
		}},
		{label: "Dedupe candidates", keywords: "dedupe duplicate candidates simhash", run: func(m Model) (Model, tea.Cmd) {
			m.openDedupePanel()
			return m, nil
		}},
		paletteSourceCommand("All view", "all view source", domain.SourceAll),
	}
	for _, entry := range m.sourceCatalog() {
		commands = append(commands, paletteSourceCommand(entry.Label+" view", sourceKeywords(entry.Source), entry.Source))
	}
	commands = append(commands, themePaletteCommands()...)
	commands = append(commands,
		paletteCommand{label: "Filter", keywords: "filter", run: func(m Model) (Model, tea.Cmd) {
			m.openFilter()
			return m, nil
		}},
		paletteCommand{label: "Settings", keywords: "settings config", run: func(m Model) (Model, tea.Cmd) {
			m.openSettingsForm()
			m.detail = false
			m.message = "settings"
			return m, nil
		}},
		paletteCommand{label: "Source health", keywords: "health status sources", run: func(m Model) (Model, tea.Cmd) {
			m.health = true
			m.detail = false
			return m, nil
		}},
	)
	return commands
}

func paletteSourceCommand(label, keywords string, source domain.SourceID) paletteCommand {
	return paletteCommand{label: label, keywords: keywords, run: func(m Model) (Model, tea.Cmd) {
		return m.switchSource(source)
	}}
}

func sourceKeywords(source domain.SourceID) string {
	switch source {
	case domain.SourceGitHub:
		return "github trending source"
	case domain.SourceHackerNews:
		return "hacker news hn source"
	case domain.SourceAILabs:
		return "ai labs openai anthropic deepmind meta source"
	case domain.SourceHuggingFace:
		return "hugging face hf papers source"
	case domain.SourceLobsters:
		return "lobsters source"
	case domain.SourceProductHunt:
		return "product hunt ph products source"
	default:
		return string(source) + " source"
	}
}

func (m Model) filteredPaletteCommands() []paletteCommand {
	filter := strings.ToLower(strings.TrimSpace(m.paletteFilter))
	commands := m.paletteCommands()
	if filter == "" {
		return commands
	}
	var filtered []paletteCommand
	for _, command := range commands {
		haystack := strings.ToLower(command.label + " " + command.keywords)
		if strings.Contains(haystack, filter) {
			filtered = append(filtered, command)
		}
	}
	return filtered
}

func (m *Model) movePaletteCursor(delta int) {
	commands := m.filteredPaletteCommands()
	if len(commands) == 0 {
		m.paletteCursor = 0
		return
	}
	m.paletteCursor = clamp(m.paletteCursor+delta, 0, len(commands)-1)
	m.ensurePaletteCursorVisible()
}

func (m *Model) clampPaletteCursor() {
	commands := m.filteredPaletteCommands()
	if len(commands) == 0 {
		m.paletteCursor = 0
		m.paletteOffset = 0
		return
	}
	m.paletteCursor = clamp(m.paletteCursor, 0, len(commands)-1)
	m.ensurePaletteCursorVisible()
}

func (m *Model) ensurePaletteCursorVisible() {
	visible := m.paletteVisibleRows()
	if visible <= 0 {
		return
	}
	if m.paletteCursor < m.paletteOffset {
		m.paletteOffset = m.paletteCursor
	}
	if m.paletteCursor >= m.paletteOffset+visible {
		m.paletteOffset = m.paletteCursor - visible + 1
	}
}

func (m Model) paletteVisibleRows() int {
	return max(3, min(12, m.height-7))
}

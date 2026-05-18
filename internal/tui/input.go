package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/AIluffy/tildewire/internal/app"
	"github.com/AIluffy/tildewire/internal/domain"
)

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.settingsOpen {
		return m.handleSettingsKey(msg)
	}
	if m.ruleForm != nil {
		return m.handleRuleFormKey(msg)
	}
	if m.paletteOpen {
		return m.handlePaletteKey(msg)
	}
	if m.filterOpen {
		return m.handleFilterKey(msg)
	}
	if m.rulesOpen {
		return m.handleRulesKey(msg)
	}
	if m.dedupeOpen {
		return m.handleDedupeKey(msg)
	}
	if m.health {
		return m.handleHealthKey(msg)
	}
	if m.mode == inputModeSearch {
		return m.handleSearchKey(msg)
	}
	if m.detail {
		return m.handleDetailKey(msg)
	}
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Help):
		m.help.ShowAll = !m.help.ShowAll
		return m, nil
	case key.Matches(msg, m.keys.Back):
		m.detail = false
		m.health = false
		m.rulesOpen = false
		m.dedupeOpen = false
		m.paletteOpen = false
		return m, nil
	case key.Matches(msg, m.keys.OpenDetail):
		if entry, ok := m.selected(); ok {
			m.openDetail(entry)
			return m, batchCommands(m.loadDetailCmd(entry), m.loadingSpinner.Tick)
		}
		return m, nil
	case key.Matches(msg, m.keys.FocusLeft):
		m.moveFocus(-1)
		return m, nil
	case key.Matches(msg, m.keys.FocusRight):
		m.moveFocus(1)
		return m, nil
	case key.Matches(msg, m.keys.SourceUp):
		if m.activePanel == panelSources {
			return m.moveFocusedSourceOrder(-1)
		}
		return m, nil
	case key.Matches(msg, m.keys.SourceDown):
		if m.activePanel == panelSources {
			return m.moveFocusedSourceOrder(1)
		}
		return m, nil
	case key.Matches(msg, m.keys.Up):
		return m.moveFocusedPanel(-1)
	case key.Matches(msg, m.keys.Down):
		return m.moveFocusedPanel(1)
	case key.Matches(msg, m.keys.PreviewUp):
		m.scrollPreview(-m.previewPageSize())
		return m, nil
	case key.Matches(msg, m.keys.PreviewDown):
		m.scrollPreview(m.previewPageSize())
		return m, nil
	case key.Matches(msg, m.keys.All):
		return m.switchSource(domain.SourceAll)
	case key.Matches(msg, m.keys.GitHub):
		return m.switchSource(domain.SourceGitHub)
	case key.Matches(msg, m.keys.HackerNews):
		return m.switchSource(domain.SourceHackerNews)
	case key.Matches(msg, m.keys.AILabs):
		return m.switchSource(domain.SourceAILabs)
	case key.Matches(msg, m.keys.HuggingFace):
		return m.switchSource(domain.SourceHuggingFace)
	case key.Matches(msg, m.keys.Lobsters):
		return m.switchSource(domain.SourceLobsters)
	case key.Matches(msg, m.keys.ProductHunt):
		return m.switchSource(domain.SourceProductHunt)
	case key.Matches(msg, m.keys.Scope):
		next, ok := nextSourceView(m.view, m.filter.SourceView)
		if !ok {
			return m, nil
		}
		m.rememberFeedSelection()
		m.filter.SourceView = next
		m.detail = false
		m.restoreFeedSelection()
		m.resetPreviewScroll()
		m.message = "scope: " + sourceViewLabel(m.view, next)
		return m, m.loadThenRefreshCmd()
	case key.Matches(msg, m.keys.Search):
		m.mode = inputModeSearch
		m.searchDraft = m.filter.Search
		m.searchBase = m.filter.Search
		m.message = "search: " + m.searchDraft
		return m, nil
	case key.Matches(msg, m.keys.Filter):
		m.openFilter()
		return m, nil
	case key.Matches(msg, m.keys.Palette):
		m.openPalette()
		return m, nil
	case key.Matches(msg, m.keys.Settings):
		m.openSettingsForm()
		m.detail = false
		m.health = false
		m.rulesOpen = false
		m.dedupeOpen = false
		m.filterOpen = false
		m.paletteOpen = false
		m.message = "settings"
		return m, nil
	case key.Matches(msg, m.keys.Refresh):
		return m.startRefresh(true, app.RefreshModeVisible)
	case key.Matches(msg, m.keys.Save):
		entry, ok := m.selected()
		if !ok {
			return m, nil
		}
		return m, m.setSavedCmd(entry.Item.ID, !entry.State.Saved)
	case key.Matches(msg, m.keys.MarkRead):
		entry, ok := m.selected()
		if !ok {
			return m, nil
		}
		return m, m.setReadCmd(entry.Item.ID, true)
	case key.Matches(msg, m.keys.MarkUnread):
		entry, ok := m.selected()
		if !ok {
			return m, nil
		}
		return m, m.setReadCmd(entry.Item.ID, false)
	case key.Matches(msg, m.keys.Hide):
		entry, ok := m.selected()
		if !ok {
			return m, nil
		}
		return m, m.setHiddenCmd(entry.Item.ID, !entry.State.Hidden)
	case key.Matches(msg, m.keys.OpenURL):
		entry, ok := m.selected()
		if !ok {
			return m, nil
		}
		return m, openURLCmd(entry.Item.URL)
	case key.Matches(msg, m.keys.OpenSource):
		entry, ok := m.selected()
		if !ok {
			return m, nil
		}
		return m, openURLCmd(entry.Item.CommentsURL)
	case key.Matches(msg, m.keys.CopyURL):
		entry, ok := m.selected()
		if !ok {
			return m, nil
		}
		return m, copyCmd(entry.Item.URL, "copied url")
	case key.Matches(msg, m.keys.CopyMarkdown):
		entry, ok := m.selected()
		if !ok {
			return m, nil
		}
		return m, copyCmd(fmt.Sprintf("[%s](%s)", entry.Item.Title, entry.Item.URL), "copied markdown link")
	case key.Matches(msg, m.keys.Health):
		m.health = true
		m.detail = false
		m.filterOpen = false
		m.paletteOpen = false
		m.rulesOpen = false
		m.dedupeOpen = false
		return m, nil
	}
	return m, nil
}

func (m Model) startRefresh(force bool, mode app.RefreshMode) (Model, tea.Cmd) {
	if m.refreshing {
		m.message = "refresh already running"
		return m, nil
	}
	m.refreshing = true
	m.refreshID++
	m.message = "refreshing sources"
	return m, m.refreshWithProgressCmd(m.refreshID, force, mode)
}

func (m Model) handleHealthKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Help):
		m.help.ShowAll = !m.help.ShowAll
		return m, nil
	case key.Matches(msg, m.keys.Refresh):
		next, cmd := m.startRefresh(true, app.RefreshModeVisible)
		if cmd != nil && m.lastError != "" {
			next.message = "retrying failed refresh"
		}
		return next, cmd
	case key.Matches(msg, m.keys.Back):
		m.health = false
		return m, nil
	}
	return m, nil
}

func (m Model) handleDetailKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Help):
		m.help.ShowAll = !m.help.ShowAll
		return m, nil
	case key.Matches(msg, m.keys.Back):
		m.detail = false
		m.detailOffset = 0
		return m, m.clearDetailRawImagesCmd()
	case key.Matches(msg, m.keys.PreviewUp):
		return m, m.scrollDetailRawImagesCmd(-m.detailPageSize())
	case key.Matches(msg, m.keys.PreviewDown):
		return m, m.scrollDetailRawImagesCmd(m.detailPageSize())
	case key.Matches(msg, m.keys.Save):
		entry, ok := m.selected()
		if !ok {
			return m, nil
		}
		return m, m.setSavedCmd(entry.Item.ID, !entry.State.Saved)
	case key.Matches(msg, m.keys.MarkRead):
		entry, ok := m.selected()
		if !ok {
			return m, nil
		}
		return m, m.setReadCmd(entry.Item.ID, true)
	case key.Matches(msg, m.keys.MarkUnread):
		entry, ok := m.selected()
		if !ok {
			return m, nil
		}
		return m, m.setReadCmd(entry.Item.ID, false)
	case key.Matches(msg, m.keys.Hide):
		entry, ok := m.selected()
		if !ok {
			return m, nil
		}
		return m, m.setHiddenCmd(entry.Item.ID, !entry.State.Hidden)
	case key.Matches(msg, m.keys.OpenURL):
		entry, ok := m.selected()
		if !ok {
			return m, nil
		}
		return m, openURLCmd(entry.Item.URL)
	case key.Matches(msg, m.keys.OpenSource):
		entry, ok := m.selected()
		if !ok {
			return m, nil
		}
		return m, openURLCmd(entry.Item.CommentsURL)
	case key.Matches(msg, m.keys.CopyURL):
		entry, ok := m.selected()
		if !ok {
			return m, nil
		}
		return m, copyCmd(entry.Item.URL, "copied url")
	case key.Matches(msg, m.keys.CopyMarkdown):
		entry, ok := m.selected()
		if !ok {
			return m, nil
		}
		return m, copyCmd(fmt.Sprintf("[%s](%s)", entry.Item.Title, entry.Item.URL), "copied markdown link")
	}
	switch msg.String() {
	case "j", "down":
		return m, m.scrollDetailRawImagesCmd(1)
	case "k", "up":
		return m, m.scrollDetailRawImagesCmd(-1)
	case "space":
		return m, m.scrollDetailRawImagesCmd(m.detailPageSize())
	default:
		return m, nil
	}
}

func (m Model) handleSearchKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.mode = inputModeNormal
		m.filter.Search = strings.TrimSpace(m.searchDraft)
		m.detail = false
		m.feedOffset = 0
		m.rememberFeedSelection()
		m.resetPreviewScroll()
		if m.filter.Search == "" {
			m.message = "search cleared"
		} else {
			m.message = "search: " + m.filter.Search
		}
		m.searchLoadID++
		return m, m.searchLoadCmd(m.searchLoadID, m.message)
	case "esc":
		m.mode = inputModeNormal
		m.searchDraft = ""
		m.message = "search cancelled"
		return m.restoreCancelledSearch()
	case "backspace", "ctrl+h":
		runes := []rune(m.searchDraft)
		if len(runes) > 0 {
			m.searchDraft = string(runes[:len(runes)-1])
			return m.liveSearch()
		}
	case "ctrl+u":
		if m.searchDraft != "" {
			m.searchDraft = ""
			return m.liveSearch()
		}
	default:
		if msg.Text != "" {
			m.searchDraft += msg.Text
			return m.liveSearch()
		}
	}
	m.message = "search: " + m.searchDraft
	return m, nil
}

func (m Model) liveSearch() (tea.Model, tea.Cmd) {
	search := strings.TrimSpace(m.searchDraft)
	m.message = "search: " + m.searchDraft
	if search == m.filter.Search {
		return m, nil
	}
	m.filter.Search = search
	m.detail = false
	m.feedOffset = 0
	m.rememberFeedSelection()
	m.resetPreviewScroll()
	m.searchLoadID++
	return m, m.searchLoadCmd(m.searchLoadID, m.message)
}

func (m Model) restoreCancelledSearch() (tea.Model, tea.Cmd) {
	search := strings.TrimSpace(m.searchBase)
	m.searchBase = ""
	if search == m.filter.Search {
		return m, nil
	}
	m.filter.Search = search
	m.detail = false
	m.feedOffset = 0
	m.rememberFeedSelection()
	m.resetPreviewScroll()
	m.searchLoadID++
	return m, m.searchLoadCmd(m.searchLoadID, m.message)
}

func (m Model) handleFilterKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	keyText := msg.String()
	if msg.Text != "" {
		keyText = msg.Text
	}
	switch keyText {
	case "enter":
		previousView := m.view
		previousSourceView := m.filter.SourceView
		previousKey := m.currentFeedSelectionKey()
		m.rememberFeedSelection()
		m.filterOpen = false
		m.view = m.normalizeFilterDraftView(m.filterDraftView)
		m.filter = m.normalizeFilterDraftForView(m.view, m.filterDraft)
		m.detail = false
		if m.currentFeedSelectionKey() == previousKey {
			m.feedOffset = 0
			m.rememberFeedSelection()
		} else {
			m.restoreFeedSelection()
		}
		m.resetPreviewScroll()
		m.message = "filter: " + filterLabel(m.filter)
		if m.view != previousView || m.filter.SourceView != previousSourceView {
			return m, m.loadThenRefreshCmd()
		}
		return m, m.loadCmd()
	case "esc":
		m.filterOpen = false
		m.message = "filter cancelled"
		return m, nil
	case "j", "down", "tab":
		m.filterCursor = (m.filterCursor + 1) % filterFieldCount
	case "k", "up", "shift+tab":
		m.filterCursor = (m.filterCursor + filterFieldCount - 1) % filterFieldCount
	case "l", "right", "space":
		m.cycleFilterDraftField(1)
	case "h", "left":
		m.cycleFilterDraftField(-1)
	case "c":
		m.filterDraft = clearFilterFacets(m.filterDraft)
	case "backspace", "ctrl+h":
		if m.filterCursor == filterFieldTag {
			runes := []rune(m.filterDraft.Tag)
			if len(runes) > 0 {
				m.filterDraft.Tag = string(runes[:len(runes)-1])
			}
		}
	case "ctrl+u":
		if m.filterCursor == filterFieldTag {
			m.filterDraft.Tag = ""
		} else {
			m.filterDraft = clearFilterFacets(m.filterDraft)
		}
	default:
		if m.filterCursor == filterFieldTag && msg.Text != "" {
			m.filterDraft.Tag += msg.Text
		}
	}
	m.message = "filter: " + filterLabel(m.normalizeFilterDraftForView(m.filterDraftView, m.filterDraft))
	return m, nil
}

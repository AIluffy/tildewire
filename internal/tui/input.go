package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/AIluffy/tildewire/internal/app"
	"github.com/AIluffy/tildewire/internal/domain"
)

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if overlay := m.activeOverlay(); overlay != overlayNone {
		return m.handleOverlayKey(overlay, msg)
	}
	if next, cmd, ok := m.switchSourceByKey(msg); ok {
		return next, cmd
	}
	if next, cmd, ok := m.handleCommonKey(msg, closeOverlayBack); ok {
		return next, cmd
	}
	switch {
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
	case key.Matches(msg, m.keys.Scope):
		next, ok := nextSourceView(m.view, m.filter.SourceView)
		if !ok {
			return m, nil
		}
		m.rememberFeedSelection()
		m.filter.SourceView = next
		m.closeOverlay()
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
		m.message = "settings"
		return m, nil
	case key.Matches(msg, m.keys.Refresh):
		return m.startRefresh(true, app.RefreshModeVisible)
	case key.Matches(msg, m.keys.Health):
		m.openOverlay(overlayHealth)
		return m, nil
	}
	if next, cmd, ok := m.handleSelectedEntryKey(msg); ok {
		return next, cmd
	}
	return m, nil
}

func (m Model) handleCommonKey(msg tea.KeyPressMsg, back func(Model) (Model, tea.Cmd)) (Model, tea.Cmd, bool) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit, true
	case key.Matches(msg, m.keys.Help):
		m.help.ShowAll = !m.help.ShowAll
		return m, nil, true
	case key.Matches(msg, m.keys.Back) && back != nil:
		next, cmd := back(m)
		return next, cmd, true
	default:
		return m, nil, false
	}
}

func closeOverlayBack(m Model) (Model, tea.Cmd) {
	m.closeOverlay()
	return m, nil
}

func detailBack(m Model) (Model, tea.Cmd) {
	m.closeOverlay()
	m.detailOffset = 0
	return m, m.clearDetailRawImagesCmd()
}

func (m Model) handleSelectedEntryKey(msg tea.KeyPressMsg) (Model, tea.Cmd, bool) {
	entry, ok := m.selected()
	switch {
	case key.Matches(msg, m.keys.Save):
		if !ok {
			return m, nil, true
		}
		return m, m.setSavedCmd(entry.Item.ID, !entry.State.Saved), true
	case key.Matches(msg, m.keys.MarkRead):
		if !ok {
			return m, nil, true
		}
		return m, m.setReadCmd(entry.Item.ID, true), true
	case key.Matches(msg, m.keys.MarkUnread):
		if !ok {
			return m, nil, true
		}
		return m, m.setReadCmd(entry.Item.ID, false), true
	case key.Matches(msg, m.keys.Hide):
		if !ok {
			return m, nil, true
		}
		return m, m.setHiddenCmd(entry.Item.ID, !entry.State.Hidden), true
	case key.Matches(msg, m.keys.OpenURL):
		if !ok {
			return m, nil, true
		}
		return m, m.openItemURLCmd(entry), true
	case key.Matches(msg, m.keys.OpenSource):
		if !ok {
			return m, nil, true
		}
		return m, m.openSourceURLCmd(entry), true
	case key.Matches(msg, m.keys.CopyURL):
		if !ok {
			return m, nil, true
		}
		return m, m.copyItemURLCmd(entry), true
	case key.Matches(msg, m.keys.CopyMarkdown):
		if !ok {
			return m, nil, true
		}
		return m, m.copyMarkdownLinkCmd(entry), true
	default:
		return m, nil, false
	}
}

func (m Model) switchSourceByKey(msg tea.KeyPressMsg) (Model, tea.Cmd, bool) {
	sources := []struct {
		key    key.Binding
		source domain.SourceID
	}{
		{key: m.keys.All, source: domain.SourceAll},
		{key: m.keys.Recommend, source: domain.SourceRecommend},
		{key: m.keys.GitHub, source: domain.SourceGitHub},
		{key: m.keys.HackerNews, source: domain.SourceHackerNews},
		{key: m.keys.AILabs, source: domain.SourceAILabs},
		{key: m.keys.HuggingFace, source: domain.SourceHuggingFace},
		{key: m.keys.Lobsters, source: domain.SourceLobsters},
		{key: m.keys.ProductHunt, source: domain.SourceProductHunt},
	}
	for _, candidate := range sources {
		if key.Matches(msg, candidate.key) {
			next, cmd := m.switchSource(candidate.source)
			return next, cmd, true
		}
	}
	return m, nil, false
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
	if next, cmd, ok := m.handleCommonKey(msg, closeOverlayBack); ok {
		return next, cmd
	}
	switch {
	case key.Matches(msg, m.keys.Refresh):
		next, cmd := m.startRefresh(true, app.RefreshModeVisible)
		if cmd != nil && m.lastError != "" {
			next.message = "retrying failed refresh"
		}
		return next, cmd
	}
	return m, nil
}

func (m Model) handleRecommendDiagnosticsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if next, cmd, ok := m.handleCommonKey(msg, closeOverlayBack); ok {
		return next, cmd
	}
	return m, nil
}

func (m Model) handleDetailKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if next, cmd, ok := m.handleCommonKey(msg, detailBack); ok {
		return next, cmd
	}
	switch {
	case key.Matches(msg, m.keys.PreviewUp):
		return m, m.scrollDetailRawImagesCmd(-m.detailPageSize())
	case key.Matches(msg, m.keys.PreviewDown):
		return m, m.scrollDetailRawImagesCmd(m.detailPageSize())
	}
	if next, cmd, ok := m.handleSelectedEntryKey(msg); ok {
		return next, cmd
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
		m.closeOverlay()
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
	m.closeOverlay()
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
	m.closeOverlay()
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
		m.closeOverlay()
		m.view = m.normalizeFilterDraftView(m.filterDraftView)
		m.filter = m.normalizeFilterDraftForView(m.view, m.filterDraft)
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
		m.closeOverlay()
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

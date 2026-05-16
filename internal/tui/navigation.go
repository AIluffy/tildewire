package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/zhangxueai/tildewire/internal/app"
	"github.com/zhangxueai/tildewire/internal/domain"
)

func feedSelectionKeyFor(view domain.SourceID, filter app.FeedFilter) feedSelectionKey {
	if view == "" {
		view = domain.SourceAll
	}
	sourceView := ""
	if view != domain.SourceAll {
		sourceView = strings.TrimSpace(filter.SourceView)
		if sourceView == "" {
			sourceView = defaultSourceView(view)
		}
	}
	return feedSelectionKey{view: view, sourceView: sourceView}
}

func (m Model) currentFeedSelectionKey() feedSelectionKey {
	return feedSelectionKeyFor(m.view, m.filter)
}

func (m *Model) rememberFeedSelection() {
	if m.feedSelections == nil {
		m.feedSelections = make(map[feedSelectionKey]feedSelection)
	}
	m.feedSelections[m.currentFeedSelectionKey()] = feedSelection{
		cursor:     m.cursor,
		feedOffset: m.feedOffset,
	}
}

func (m *Model) restoreFeedSelection() {
	if m.feedSelections == nil {
		m.feedSelections = make(map[feedSelectionKey]feedSelection)
	}
	selection, ok := m.feedSelections[m.currentFeedSelectionKey()]
	if !ok {
		selection = feedSelection{}
	}
	m.cursor = selection.cursor
	m.feedOffset = selection.feedOffset
	m.clampFeedSelection()
}

func (m *Model) clampFeedSelection() {
	if len(m.entries) == 0 {
		m.cursor = 0
		m.feedOffset = 0
		return
	}
	if m.cursor >= len(m.entries) {
		m.cursor = len(m.entries) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.clampFeedOffset()
}

func (m *Model) openDetail(entry domain.FeedEntry) {
	m.clearDetailContentCache()
	m.detail = true
	m.filterOpen = false
	m.health = false
	m.rulesOpen = false
	m.dedupeOpen = false
	m.paletteOpen = false
	m.detailLoading = true
	m.detailEntryID = entry.Item.ID
	m.itemDetail = domain.ItemDetail{ItemID: entry.Item.ID, Title: entry.Item.Title, URL: entry.Item.URL}
	m.detailError = ""
	m.detailOffset = 0
	m.message = "loading detail"
	m.refreshDetailContentCache()
}

func (m *Model) moveFocus(delta int) {
	panels := []mainPanel{panelSources, panelFeed, panelPreview}
	current := 1
	for idx, panel := range panels {
		if m.activePanel == panel {
			current = idx
			break
		}
	}
	next := (current + delta) % len(panels)
	if next < 0 {
		next += len(panels)
	}
	m.activePanel = panels[next]
}

func (m Model) moveFocusedPanel(delta int) (tea.Model, tea.Cmd) {
	switch m.activePanel {
	case panelSources:
		return m.moveFocusedSource(delta)
	case panelPreview:
		m.scrollPreview(delta)
		return m, nil
	default:
		return m.moveFeedCursor(delta), nil
	}
}

func (m Model) moveFocusedSource(delta int) (tea.Model, tea.Cmd) {
	rows := m.sourceRows()
	current := 1
	for idx, row := range rows {
		if row.source == m.view {
			current = idx
			break
		}
	}
	next := clamp(current+delta, 1, len(rows)-1)
	if next == current {
		return m, nil
	}
	m.setSource(rows[next].source)
	return m, m.loadThenRefreshCmd()
}

func (m Model) moveFocusedSourceOrder(delta int) (tea.Model, tea.Cmd) {
	if delta == 0 || m.view == "" || m.view == domain.SourceAll {
		return m, nil
	}
	catalog := m.sourceCatalog()
	current := -1
	for idx, entry := range catalog {
		if entry.Source == m.view {
			current = idx
			break
		}
	}
	if current < 0 {
		return m, nil
	}
	next := current + delta
	if next < 0 || next >= len(catalog) {
		return m, nil
	}
	catalog[current], catalog[next] = catalog[next], catalog[current]
	m.config.SourceOrder = sourceOrderFromCatalog(catalog)
	m.clampSourcesOffset()
	m.message = "source order saved"
	return m, m.saveSourceOrderCmd(m.config)
}

func (m Model) moveFeedCursor(delta int) Model {
	moved := false
	if delta < 0 && m.cursor > 0 {
		m.cursor--
		m.ensureFeedCursorVisible()
		m.resetPreviewScroll()
		moved = true
	}
	if delta > 0 && m.cursor < len(m.entries)-1 {
		m.cursor++
		m.ensureFeedCursorVisible()
		m.resetPreviewScroll()
		moved = true
	}
	if moved {
		m.rememberFeedSelection()
	}
	return m
}

func (m Model) handleMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	mouse := msg.Mouse()
	box, ok := m.panelAt(mouse.X, mouse.Y)
	if !ok {
		return m, nil
	}
	m.activePanel = box.panel
	switch box.panel {
	case panelSources:
		source, ok := m.sourceAtContentRow(mouse.Y - box.contentY)
		if !ok {
			return m, nil
		}
		m.setSource(source)
		return m, m.loadThenRefreshCmd()
	case panelFeed:
		idx, ok := m.feedIndexAtContentRow(mouse.Y - box.contentY)
		if !ok {
			return m, nil
		}
		if idx != m.cursor {
			m.cursor = idx
			m.rememberFeedSelection()
			m.resetPreviewScroll()
		}
	case panelPreview:
		return m, nil
	}
	return m, nil
}

func (m Model) handleDetailMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	mouse := msg.Mouse()
	if mouse.Button != tea.MouseLeft {
		return m, nil
	}
	block, ok := m.detailCodeBlockAt(mouse.X, mouse.Y)
	if !ok {
		return m, nil
	}
	return m, copyToastCmd(block.Text(), "copied code block")
}

func (m Model) handleMouseWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	mouse := msg.Mouse()
	box, ok := m.panelAt(mouse.X, mouse.Y)
	if !ok {
		return m, nil
	}
	m.activePanel = box.panel
	delta := 0
	switch mouse.Button {
	case tea.MouseWheelUp:
		delta = -1
	case tea.MouseWheelDown:
		delta = 1
	default:
		return m, nil
	}
	switch box.panel {
	case panelSources:
		m.scrollSources(delta)
	case panelFeed:
		m.scrollFeed(delta)
	case panelPreview:
		m.scrollPreview(delta)
	}
	return m, nil
}

func (m Model) handleDetailMouseWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	mouse := msg.Mouse()
	switch mouse.Button {
	case tea.MouseWheelUp:
		return m, m.scrollDetailRawImagesCmd(-1)
	case tea.MouseWheelDown:
		return m, m.scrollDetailRawImagesCmd(1)
	}
	return m, nil
}

func (m Model) panelAt(x, y int) (panelBox, bool) {
	layout := m.mainLayout()
	for _, box := range []panelBox{layout.sources, layout.feed, layout.preview} {
		if x >= box.x && x < box.x+box.width && y >= box.y && y < box.y+box.height {
			return box, true
		}
	}
	return panelBox{}, false
}

func (m Model) sourceAtContentRow(row int) (domain.SourceID, bool) {
	if row <= 0 {
		return "", false
	}
	layout := m.mainLayout()
	rows := m.sourceRows()
	visibleRows := max(0, layout.sources.contentHeight-1)
	start := clamp(m.sourcesOffset, 0, maxSourceOffset(len(rows), visibleRows))
	idx := start + row - 1
	if idx < 0 || idx >= len(rows) || rows[idx].source == "" {
		return "", false
	}
	return rows[idx].source, true
}

func (m Model) feedIndexAtContentRow(row int) (int, bool) {
	topRows := m.feedTopRows()
	if row < topRows {
		return 0, false
	}
	layout := m.mainLayout()
	visibleRows := feedVisibleRowsFor(layout.feed.contentHeight, topRows)
	start := clamp(m.feedOffset, 0, maxFeedOffset(len(m.entries), visibleRows))
	itemRow := row - topRows
	if itemRow%feedItemBlockRows >= feedItemContentRows {
		return 0, false
	}
	idx := start + itemRow/feedItemBlockRows
	if idx < 0 || idx >= len(m.entries) {
		return 0, false
	}
	return idx, true
}

func (m *Model) setSource(source domain.SourceID) {
	m.rememberFeedSelection()
	m.view = source
	if source == domain.SourceAll {
		m.filter.SourceView = ""
	} else {
		m.filter.SourceView = defaultSourceView(source)
	}
	m.detail = false
	m.detailLoading = false
	m.detailEntryID = ""
	m.itemDetail = domain.ItemDetail{}
	m.detailError = ""
	m.detailOffset = 0
	m.paletteOpen = false
	m.activePanel = panelSources
	m.restoreFeedSelection()
	m.resetPreviewScroll()
}

func (m Model) switchSource(source domain.SourceID) (Model, tea.Cmd) {
	if !m.sourceEnabled(source) {
		m.message = "source disabled: " + app.SourceLabel(source)
		return m, nil
	}
	m.setSource(source)
	return m, m.loadThenRefreshCmd()
}

func (m Model) feedTopRows() int {
	return 1 + feedTitleMarginRows
}

func (m Model) selected() (domain.FeedEntry, bool) {
	if m.cursor < 0 || m.cursor >= len(m.entries) {
		return domain.FeedEntry{}, false
	}
	return m.entries[m.cursor], true
}

func (m Model) previewPageSize() int {
	_, visibleHeight := m.previewViewportSize()
	return max(1, visibleHeight)
}

func (m Model) previewViewportSize() (int, int) {
	preview := m.mainLayout().preview
	return preview.contentWidth, max(0, preview.contentHeight-1)
}

func (m Model) feedVisibleRows() int {
	return feedVisibleRowsFor(m.mainLayout().feed.contentHeight, m.feedTopRows())
}

func (m *Model) scrollPreview(delta int) {
	if delta == 0 {
		return
	}
	width, visibleHeight := m.previewViewportSize()
	limit := maxPreviewOffset(len(m.previewContentLines(width)), visibleHeight)
	m.previewOffset = clamp(m.previewOffset+delta, 0, limit)
}

func (m *Model) resetPreviewScroll() {
	m.previewOffset = 0
}

func (m *Model) clampPreviewOffset() {
	width, visibleHeight := m.previewViewportSize()
	limit := maxPreviewOffset(len(m.previewContentLines(width)), visibleHeight)
	m.previewOffset = clamp(m.previewOffset, 0, limit)
}

func (m Model) detailVisibleHeight() int {
	return max(1, m.height-1)
}

func (m Model) detailPageSize() int {
	return max(1, m.detailVisibleHeight()-1)
}

func (m *Model) scrollDetail(delta int) bool {
	if delta == 0 {
		return false
	}
	width := m.detailContentWidth()
	limit := maxDetailOffset(len(m.detailContentLines(width)), m.detailVisibleHeight())
	previous := m.detailOffset
	m.detailOffset = clamp(m.detailOffset+delta, 0, limit)
	return m.detailOffset != previous
}

func (m *Model) clampDetailOffset() {
	width := m.detailContentWidth()
	limit := maxDetailOffset(len(m.detailContentLines(width)), m.detailVisibleHeight())
	m.detailOffset = clamp(m.detailOffset, 0, limit)
}

func (m *Model) scrollFeed(delta int) {
	if delta == 0 {
		return
	}
	visibleRows := m.feedVisibleRows()
	limit := maxFeedOffset(len(m.entries), visibleRows)
	m.feedOffset = clamp(m.feedOffset+delta, 0, limit)
	m.rememberFeedSelection()
}

func (m *Model) ensureFeedCursorVisible() {
	visibleRows := m.feedVisibleRows()
	if visibleRows <= 0 {
		m.feedOffset = 0
		return
	}
	if m.cursor < m.feedOffset {
		m.feedOffset = m.cursor
	}
	if m.cursor >= m.feedOffset+visibleRows {
		m.feedOffset = m.cursor - visibleRows + 1
	}
	m.clampFeedOffset()
}

func (m *Model) clampFeedOffset() {
	visibleRows := m.feedVisibleRows()
	limit := maxFeedOffset(len(m.entries), visibleRows)
	m.feedOffset = clamp(m.feedOffset, 0, limit)
}

func (m *Model) scrollSources(delta int) {
	if delta == 0 {
		return
	}
	layout := m.mainLayout()
	rows := m.sourceRows()
	visibleRows := max(0, layout.sources.contentHeight-1)
	limit := maxSourceOffset(len(rows), visibleRows)
	m.sourcesOffset = clamp(m.sourcesOffset+delta, 0, limit)
}

func (m *Model) clampSourcesOffset() {
	layout := m.mainLayout()
	rows := m.sourceRows()
	visibleRows := max(0, layout.sources.contentHeight-1)
	limit := maxSourceOffset(len(rows), visibleRows)
	m.sourcesOffset = clamp(m.sourcesOffset, 0, limit)
}

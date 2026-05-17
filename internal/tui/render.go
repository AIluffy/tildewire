package tui

import (
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"

	"github.com/AIluffy/tildewire/internal/domain"
)

type mainPanel int

const (
	panelNone mainPanel = iota
	panelSources
	panelFeed
	panelPreview
)

type mainLayout struct {
	sources panelBox
	feed    panelBox
	preview panelBox
}

const (
	feedItemContentRows  = 2
	feedItemBlockRows    = 3
	feedTitleMarginRows  = 1
	maxSourcesPanelWidth = 32
)

type panelBox struct {
	panel         mainPanel
	x             int
	y             int
	width         int
	height        int
	contentX      int
	contentY      int
	contentWidth  int
	contentHeight int
}

func (m Model) render() string {
	if m.width < 40 {
		m.width = 40
	}
	if m.height < 12 {
		m.height = 12
	}
	if m.settingsOpen {
		return m.renderSettings()
	}
	if m.ruleForm != nil {
		return m.renderRuleForm()
	}
	if m.filterOpen {
		return m.renderFilter()
	}
	if m.paletteOpen {
		return m.renderPalette()
	}
	if m.rulesOpen {
		return m.renderRules()
	}
	if m.dedupeOpen {
		return m.renderDedupeCandidates()
	}
	if m.health {
		return m.renderHealth()
	}
	if m.detail {
		return m.renderDetail()
	}
	layout := m.mainLayout()
	header := clip(m.renderHeader(), m.width)
	sources := m.renderPanel(layout.sources, m.renderSources(layout.sources.contentWidth, layout.sources.contentHeight), m.activePanel == panelSources)
	feed := m.renderPanel(layout.feed, m.renderFeed(layout.feed.contentWidth, layout.feed.contentHeight), m.activePanel == panelFeed)
	preview := m.renderPanel(layout.preview, m.renderPreview(layout.preview.contentWidth, layout.preview.contentHeight), m.activePanel == panelPreview)
	body := lipgloss.JoinHorizontal(lipgloss.Top, sources, feed, preview)
	status := m.renderStatus()
	help := m.renderMainHelp()
	return strings.Join([]string{header, body, status, help}, "\n")
}

func (m Model) mainLayout() mainLayout {
	renderWidth := max(40, m.width)
	renderHeight := max(12, m.height)
	frameWidth, _ := m.styles.panel.GetFrameSize()
	minPanelWidth := frameWidth + 6
	minFeedWidth := frameWidth + 16
	bodyHeight := max(1, renderHeight-2-renderedBlockHeight(m.renderMainHelp()))

	sourcesWidth := clamp(renderWidth/5, minPanelWidth, maxSourcesPanelWidth)
	previousPreviewWidth := clamp(renderWidth/3, minPanelWidth, 48)
	previewWidth := clamp(renderWidth*2/5, minPanelWidth, 64)
	feedWidth := renderWidth - sourcesWidth - previewWidth
	preferredFeedWidth := max(minFeedWidth, frameWidth+41)
	if feedWidth < preferredFeedWidth {
		deficit := preferredFeedWidth - feedWidth
		shrinkPreview := min(deficit, max(0, previewWidth-previousPreviewWidth))
		previewWidth -= shrinkPreview
		feedWidth = renderWidth - sourcesWidth - previewWidth
	}
	if feedWidth < minFeedWidth {
		deficit := minFeedWidth - feedWidth
		shrinkPreview := min(deficit, previewWidth-minPanelWidth)
		previewWidth -= shrinkPreview
		deficit -= shrinkPreview
		shrinkSources := min(deficit, sourcesWidth-minPanelWidth)
		sourcesWidth -= shrinkSources
		feedWidth = renderWidth - sourcesWidth - previewWidth
	}
	if feedWidth < minPanelWidth {
		sourcesWidth = minPanelWidth
		previewWidth = minPanelWidth
		feedWidth = max(minPanelWidth, renderWidth-sourcesWidth-previewWidth)
	}

	bodyY := 1
	sources := m.newPanelBox(panelSources, 0, bodyY, sourcesWidth, bodyHeight)
	feed := m.newPanelBox(panelFeed, sourcesWidth, bodyY, feedWidth, bodyHeight)
	preview := m.newPanelBox(panelPreview, sourcesWidth+feedWidth, bodyY, previewWidth, bodyHeight)
	return mainLayout{sources: sources, feed: feed, preview: preview}
}

func (m Model) renderMainHelp() string {
	width := max(40, m.width)
	helpModel := m.help
	helpModel.SetWidth(width)
	return clipBlock(helpModel.View(m.keys), width)
}

func (m Model) newPanelBox(panel mainPanel, x, y, width, height int) panelBox {
	left := m.styles.panel.GetBorderLeftSize() + m.styles.panel.GetPaddingLeft()
	top := m.styles.panel.GetBorderTopSize() + m.styles.panel.GetPaddingTop()
	frameWidth, frameHeight := m.styles.panel.GetFrameSize()
	return panelBox{
		panel:         panel,
		x:             x,
		y:             y,
		width:         width,
		height:        height,
		contentX:      x + left,
		contentY:      y + top,
		contentWidth:  max(0, width-frameWidth),
		contentHeight: max(0, height-frameHeight),
	}
}

func (m Model) renderPanel(box panelBox, content string, focused bool) string {
	style := m.styles.panel
	if focused {
		style = m.styles.focus
	}
	return style.Width(box.width).Height(box.height).Render(content)
}

func (m Model) renderHeader() string {
	view := "All"
	switch m.view {
	case domain.SourceGitHub:
		view = "Trending"
	case domain.SourceHackerNews:
		view = "Hacker News"
	case domain.SourceHuggingFace:
		view = "HF Papers"
	case domain.SourceLobsters:
		view = "Lobsters"
	case domain.SourceProductHunt:
		view = "Product Hunt"
	}
	refresh := ""
	if m.refreshing {
		refresh = "  refreshing"
	}
	parts := []string{"View: " + view, "Sort: Hot", "Filter: " + filterLabel(m.filter)}
	if m.view != domain.SourceAll && m.filter.SourceView != "" {
		parts = append(parts, "Scope: "+sourceViewLabel(m.view, m.filter.SourceView))
	}
	search := m.filter.Search
	if m.mode == inputModeSearch {
		search = m.searchDraft
	}
	if search != "" {
		parts = append(parts, "Search: "+search)
	}
	if refresh != "" {
		parts = append(parts, strings.TrimSpace(refresh))
	}
	return strings.Join([]string{m.renderAppTitle(), m.styles.header.Render(strings.Join(parts, "  "))}, "  ")
}

func (m Model) renderAppTitle() string {
	const title = "Tildewire"
	runes := []rune(title)
	gradient := lipgloss.Blend1D(
		len(runes),
		lipgloss.Color(m.styles.spec.accentAlt),
		lipgloss.Color(m.styles.spec.active),
		lipgloss.Color(m.styles.spec.accent),
		lipgloss.Color(m.styles.spec.productHunt),
	)

	var b strings.Builder
	for i, r := range runes {
		style := m.styles.title
		if i < len(gradient) {
			style = style.Foreground(gradient[i])
		}
		b.WriteString(style.Render(string(r)))
	}
	return b.String()
}

func (m Model) renderSources(width, height int) string {
	lines := []string{"SOURCES"}
	rows := m.sourceRows()
	visibleRows := max(0, height-1)
	start := clamp(m.sourcesOffset, 0, maxSourceOffset(len(rows), visibleRows))
	end := min(len(rows), start+visibleRows)
	for _, row := range rows[start:end] {
		lines = append(lines, clip(row.render(m, width), width))
	}
	return fillLines(lines, height)
}

func (m Model) renderFeed(width, height int) string {
	lines := []string{"FEED", ""}
	if len(m.entries) == 0 {
		if m.refreshing {
			lines = append(lines, m.loadingLines(width)...)
			return fillLines(lines, height)
		}
		lines = append(lines, m.styles.muted.Render("No cached items yet."), m.styles.muted.Render("Press r to refresh."))
		return fillLines(lines, height)
	}
	visibleRows := feedVisibleRowsFor(height, m.feedTopRows())
	start := clamp(m.feedOffset, 0, maxFeedOffset(len(m.entries), visibleRows))
	end := min(len(m.entries), start+visibleRows)
	indexWidth := feedIndexWidth(len(m.entries))
	for idx := start; idx < end; idx++ {
		entry := m.entries[idx]
		lines = append(lines, m.renderFeedItemLine(idx, entry, idx == m.cursor, width, m.filter.Search, indexWidth))
		lines = append(lines, m.renderFeedItemSubtitle(entry, width))
		lines = append(lines, "")
	}
	return fillLines(lines, height)
}

func (m Model) renderFeedItemSubtitle(entry domain.FeedEntry, width int) string {
	context, ok := searchMatchContext(entry, m.filter.Search)
	if ok {
		line := m.styles.muted.Render("   match: ") + m.renderSearchHighlightedText(context, searchQueryTokens(m.filter.Search))
		return clip(line, width)
	}
	return m.styles.muted.Render(clip("   "+entry.Item.Subtitle, width))
}

func (m Model) renderSearchHighlightedText(value string, tokens []string) string {
	return m.renderSearchHighlightedTextWithStyle(value, tokens, m.styles.muted)
}

func (m Model) renderSearchHighlightedTextWithStyle(value string, tokens []string, baseStyle lipgloss.Style) string {
	if value == "" {
		return ""
	}
	var builder strings.Builder
	for value != "" {
		start, length := firstSearchTokenMatch(value, tokens)
		if start < 0 {
			builder.WriteString(baseStyle.Render(value))
			break
		}
		if start > 0 {
			builder.WriteString(baseStyle.Render(value[:start]))
		}
		builder.WriteString(m.styles.searchHit.Render(value[start : start+length]))
		value = value[start+length:]
	}
	return builder.String()
}

func firstSearchTokenMatch(value string, tokens []string) (int, int) {
	lower := strings.ToLower(value)
	matchStart := -1
	matchLength := 0
	for _, token := range tokens {
		if token == "" {
			continue
		}
		idx := strings.Index(lower, token)
		if idx < 0 {
			continue
		}
		if matchStart < 0 || idx < matchStart || idx == matchStart && len(token) > matchLength {
			matchStart = idx
			matchLength = len(token)
		}
	}
	return matchStart, matchLength
}

func searchMatchContext(entry domain.FeedEntry, query string) (string, bool) {
	tokens := searchQueryTokens(query)
	if len(tokens) == 0 {
		return "", false
	}
	item := entry.Item
	if textContainsSearchToken(item.Title, tokens) || textContainsSearchToken(item.Subtitle, tokens) {
		return "", false
	}
	candidates := []string{
		item.Summary,
		item.Author,
		item.Organization,
		item.Language,
		item.Refs.Repo,
		item.Refs.ArxivID,
		item.Refs.PaperID,
		strings.Join(item.Tags, ", "),
	}
	for _, candidate := range candidates {
		if snippet, ok := searchContextSnippet(candidate, tokens); ok {
			return snippet, true
		}
	}
	return "", false
}

func searchQueryTokens(query string) []string {
	var tokens []string
	var current strings.Builder
	flush := func() {
		if current.Len() == 0 {
			return
		}
		tokens = append(tokens, strings.ToLower(current.String()))
		current.Reset()
	}
	for _, r := range query {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return tokens
}

func textContainsSearchToken(value string, tokens []string) bool {
	value = strings.ToLower(value)
	for _, token := range tokens {
		if strings.Contains(value, token) {
			return true
		}
	}
	return false
}

func searchContextSnippet(value string, tokens []string) (string, bool) {
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		return "", false
	}
	lower := strings.ToLower(value)
	matchByte := -1
	for _, token := range tokens {
		idx := strings.Index(lower, token)
		if idx >= 0 && (matchByte < 0 || idx < matchByte) {
			matchByte = idx
		}
	}
	if matchByte < 0 {
		return "", false
	}
	runes := []rune(value)
	matchRune := byteIndexToRuneIndex(value, matchByte)
	start := max(0, matchRune-48)
	end := min(len(runes), matchRune+64)
	snippet := strings.TrimSpace(string(runes[start:end]))
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(runes) {
		snippet += "..."
	}
	return snippet, true
}

func byteIndexToRuneIndex(value string, byteIndex int) int {
	runeIndex := 0
	for idx := range value {
		if idx >= byteIndex {
			return runeIndex
		}
		runeIndex++
	}
	return runeIndex
}

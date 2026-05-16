package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/AIluffy/tildewire/internal/app"
	"github.com/AIluffy/tildewire/internal/domain"
	"github.com/AIluffy/tildewire/internal/tui/githubreadme"
	md "github.com/AIluffy/tildewire/internal/tui/markdown"
)

const (
	maxDetailContentWidth    = 100
	detailBottomSpacingLines = 3
)

type detailLineCache struct {
	key        detailLineCacheKey
	lines      []string
	codeBlocks []md.RenderedCodeBlock
	ok         bool
}

type detailLineCacheKey struct {
	entryID        string
	width          int
	loading        bool
	loadingLabel   string
	detailError    string
	glamourStyle   string
	imageMode      string
	imageVersion   int
	darkBackground bool
}

func (m Model) renderDetail() string {
	width := m.detailContentWidth()
	_, ok := m.selected()
	if !ok {
		body := fillLines([]string{mutedStyle.Render("No item selected.")}, m.detailVisibleHeight())
		lines := strings.Split(body, "\n")
		lines = append(lines, mutedStyle.Render("esc back"))
		return strings.Join(m.centerDetailLines(lines, width), "\n")
	}
	lines := m.detailContentLines(width)
	visibleHeight := m.detailVisibleHeight()
	offset := clamp(m.detailOffset, 0, maxDetailOffset(len(lines), visibleHeight))
	end := min(len(lines), offset+visibleHeight)
	bodyLines := []string{}
	if len(lines) > 0 {
		bodyLines = lines[offset:end]
	}
	body := fillLines(bodyLines, visibleHeight)
	footer := "esc back  j/k scroll  s save  m read  u unread  o open  q quit"
	if limit := maxDetailOffset(len(lines), visibleHeight); limit > 0 {
		footer = fmt.Sprintf("%s  %d/%d", footer, offset+1, limit+1)
	}
	renderedLines := strings.Split(body, "\n")
	renderedLines = m.renderDetailToast(renderedLines, width)
	renderedLines = append(renderedLines, mutedStyle.Render(footer))
	return strings.Join(m.centerDetailLines(renderedLines, width), "\n")
}

func (m Model) renderDetailToast(lines []string, width int) []string {
	if m.toast == "" || len(lines) == 0 {
		return lines
	}
	lines[len(lines)-1] = renderToastLine(m.toast, width)
	return lines
}

func renderToastLine(message string, width int) string {
	maxMessageWidth := max(1, width-6)
	toast := toastStyle.Render(clip(message, maxMessageWidth))
	toastWidth := ansi.StringWidth(toast)
	leftPad := strings.Repeat(" ", max(0, (width-toastWidth)/2))
	return clip(leftPad+toast, width)
}

func (m Model) detailContentWidth() int {
	return clamp(m.width-4, 40, maxDetailContentWidth)
}

func (m Model) centerDetailLines(lines []string, contentWidth int) []string {
	renderWidth := max(40, m.width)
	indent := max(0, (renderWidth-contentWidth)/2)
	prefix := strings.Repeat(" ", indent)
	centered := make([]string, len(lines))
	for i, line := range lines {
		if _, ok := detailRawLine(line); ok {
			centered[i] = ""
			continue
		}
		if line == "" {
			centered[i] = ""
			continue
		}
		centered[i] = clip(prefix+clip(line, contentWidth), renderWidth)
	}
	return centered
}

func (m *Model) refreshDetailContentCache() {
	if !m.detail {
		return
	}
	_ = m.detailContentLines(m.detailContentWidth())
}

func (m *Model) clearDetailContentCache() {
	m.detailLineCache = detailLineCache{}
}

func (m *Model) detailContentLines(width int) []string {
	entry, ok := m.selected()
	if !ok {
		return nil
	}
	loadingLabel := m.detailLoadingLabel()
	key := detailLineCacheKey{
		entryID:        entry.Item.ID,
		width:          width,
		loading:        m.detailLoading,
		loadingLabel:   loadingLabel,
		detailError:    m.detailError,
		glamourStyle:   strings.TrimSpace(m.config.GlamourStyle),
		imageMode:      normalizeMarkdownImagePreviewMode(m.config.MarkdownImagePreview),
		imageVersion:   m.detailImageVersion,
		darkBackground: m.darkBackground,
	}
	if m.detailLineCache.ok && m.detailLineCache.key == key {
		return m.detailLineCache.lines
	}
	document := m.renderDetailDocument(entry, width, loadingLabel)
	body := document.Content
	body = strings.TrimRight(clipBlock(body, width), "\n")
	if body == "" {
		m.detailLineCache = detailLineCache{key: key, ok: true}
		return nil
	}
	lines := strings.Split(body, "\n")
	lines = appendDetailBottomSpacing(lines)
	m.detailLineCache = detailLineCache{key: key, lines: lines, codeBlocks: document.CodeBlocks, ok: true}
	return lines
}

func appendDetailBottomSpacing(lines []string) []string {
	return append(lines, make([]string, detailBottomSpacingLines)...)
}

func (m Model) detailLoadingLabel() string {
	if !m.detailLoading {
		return ""
	}
	frame := strings.TrimSpace(ansi.Strip(m.loadingSpinner.View()))
	if frame == "" {
		return "Loading detail..."
	}
	return "Loading detail... " + frame
}

func (m Model) renderDetailDocument(entry domain.FeedEntry, width int, loadingLabel string) md.RenderedDocument {
	if section, ok := githubreadme.Section(m.itemDetail); ok {
		markdown := githubDetailMarkdown(entry, m.itemDetail, section, loadingLabel, m.detailError)
		if document, err := m.renderGitHubMarkdownDocument(markdown, width, section.URL); err == nil && strings.TrimSpace(document.Content) != "" {
			return document
		}
	}
	markdown := detailMarkdown(entry, m.itemDetail, loadingLabel, m.detailError)
	if document, err := m.renderDetailMarkdownDocument(markdown, width); err == nil && strings.TrimSpace(document.Content) != "" {
		return document
	}
	lines := []string{
		activeStyle.Render(wrapFirst(entry.Item.Title, width)),
	}
	if entry.Item.Subtitle != "" {
		lines = append(lines, mutedStyle.Render(entry.Item.Subtitle))
	}
	lines = append(lines, "", "URL: "+entry.Item.URL)
	if entry.Item.CommentsURL != "" {
		lines = append(lines, "Source: "+entry.Item.CommentsURL)
	}
	if entry.Item.Summary != "" {
		lines = append(lines, "")
		lines = append(lines, wrap(entry.Item.Summary, width)...)
	}
	if loadingLabel != "" {
		lines = append(lines, "", loadingLabel)
	}
	if m.detailError != "" {
		lines = append(lines, "", "Detail error: "+m.detailError)
	}
	return md.RenderedDocument{Content: strings.Join(lines, "\n")}
}

func (m Model) detailCodeBlockAt(x, y int) (md.CodeBlock, bool) {
	if y < 0 || y >= m.detailVisibleHeight() {
		return md.CodeBlock{}, false
	}
	contentWidth := m.detailContentWidth()
	indent := max(0, (max(40, m.width)-contentWidth)/2)
	relativeX := x - indent
	if relativeX < 0 || relativeX >= contentWidth {
		return md.CodeBlock{}, false
	}
	line := m.detailOffset + y
	for _, block := range m.detailRenderedCodeBlocks() {
		hit := block.CopyHit
		if line == hit.Line && relativeX >= hit.StartX && relativeX < hit.EndX {
			return block.Block, true
		}
	}
	return md.CodeBlock{}, false
}

func (m Model) detailRenderedCodeBlocks() []md.RenderedCodeBlock {
	width := m.detailContentWidth()
	_ = m.detailContentLines(width)
	return m.detailLineCache.codeBlocks
}

func (m Model) renderMarkdown(markdown string, width int) (string, error) {
	return m.renderMarkdownWithOptions(markdown, width, "", true, false, false)
}

func (m Model) renderGitHubMarkdown(markdown string, width int, baseURL string) (string, error) {
	return m.renderMarkdownWithOptions(markdown, min(width, 96), baseURL, false, true, true)
}

func (m Model) renderMarkdownDocument(markdown string, width int) (md.RenderedDocument, error) {
	return m.renderMarkdownDocumentWithOptions(markdown, width, "", true, false, false)
}

func (m Model) renderDetailMarkdownDocument(markdown string, width int) (md.RenderedDocument, error) {
	return m.renderMarkdownDocumentWithOptions(markdown, width, "", true, true, false)
}

func (m Model) renderGitHubMarkdownDocument(markdown string, width int, baseURL string) (md.RenderedDocument, error) {
	return m.renderMarkdownDocumentWithOptions(markdown, min(width, 96), baseURL, false, true, true)
}

func (m Model) renderMarkdownWithOptions(markdown string, width int, baseURL string, tableWrap bool, cleanHeadingPrefixes bool, imageSegments bool) (string, error) {
	document, err := m.renderMarkdownDocumentWithOptions(markdown, width, baseURL, tableWrap, cleanHeadingPrefixes, imageSegments)
	if err != nil {
		return "", err
	}
	return document.Content, nil
}

func (m Model) renderMarkdownDocumentWithOptions(markdown string, width int, baseURL string, tableWrap bool, cleanHeadingPrefixes bool, imageSegments bool) (md.RenderedDocument, error) {
	options := md.Options{
		Style:                strings.TrimSpace(m.config.GlamourStyle),
		Width:                width,
		BaseURL:              baseURL,
		TableWrap:            tableWrap,
		CleanHeadingPrefixes: cleanHeadingPrefixes,
		ImageSegments:        imageSegments,
		ImageMode:            m.config.MarkdownImagePreview,
		Images:               m.imagePreviews,
		DarkBackground:       m.darkBackground,
		TerminalBackground:   m.terminalBackground,
	}
	return md.RenderDocument(markdown, options)
}

func githubDetailMarkdown(entry domain.FeedEntry, detail domain.ItemDetail, section domain.DetailSection, loadingLabel string, detailErr string) string {
	item := entry.Item
	title := strings.TrimSpace(item.Title)
	if title == "" {
		title = strings.TrimSpace(item.Refs.Repo)
	}
	if title == "" {
		title = "GitHub repository"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", title)
	if meta := githubDetailMetaLine(entry); meta != "" {
		b.WriteString(meta)
		b.WriteString("\n\n")
	}
	if item.URL != "" {
		fmt.Fprintf(&b, "**Repo:** %s\n\n", item.URL)
	}
	if section.URL != "" {
		fmt.Fprintf(&b, "**README:** %s\n\n", section.URL)
	}
	if len(detail.Providers) > 0 {
		fmt.Fprintf(&b, "**Detail providers:** %s\n\n", detailProviderLabels(detail.Providers))
		b.WriteString("---\n\n")
	}
	if loadingLabel != "" {
		b.WriteString(loadingLabel)
		b.WriteString("\n\n")
	}
	if detailErr != "" {
		fmt.Fprintf(&b, "_Detail loaded with errors: %s_\n\n", detailErr)
	}
	if cleaned := githubreadme.Clean(section.Body); cleaned != "" {
		b.WriteString("## README\n\n")
		b.WriteString(cleaned)
		b.WriteString("\n\n")
	}
	writeDetailComments(&b, detail.Comments)
	return b.String()
}

func githubDetailMetaLine(entry domain.FeedEntry) string {
	metrics := mergePreviewMetrics(entry.PrimarySource().Metrics, entry.Item.Metrics)
	parts := make([]string, 0, 4)
	if entry.Item.Language != "" {
		parts = append(parts, entry.Item.Language)
	}
	if metrics.Stars != nil {
		parts = append(parts, fmt.Sprintf("%d stars", *metrics.Stars))
	}
	if metrics.Forks != nil {
		parts = append(parts, fmt.Sprintf("%d forks", *metrics.Forks))
	}
	if metrics.StarsToday != nil {
		parts = append(parts, fmt.Sprintf("%d stars today", *metrics.StarsToday))
	}
	return strings.Join(parts, " | ")
}

func detailMarkdown(entry domain.FeedEntry, detail domain.ItemDetail, loadingLabel string, detailErr string) string {
	item := entry.Item
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", item.Title)
	if item.Subtitle != "" {
		fmt.Fprintf(&b, "%s\n\n", item.Subtitle)
	}
	if item.URL != "" {
		fmt.Fprintf(&b, "[Open item](%s)\n\n", item.URL)
	}
	if item.CommentsURL != "" {
		fmt.Fprintf(&b, "[Source discussion](%s)\n\n", item.CommentsURL)
	}
	if item.Summary != "" {
		b.WriteString(item.Summary)
		b.WriteString("\n\n")
	}
	if item.Author != "" {
		fmt.Fprintf(&b, "**Author:** %s\n\n", item.Author)
	}
	if item.Language != "" {
		fmt.Fprintf(&b, "**Language:** %s\n\n", item.Language)
	}
	if len(item.Tags) > 0 {
		fmt.Fprintf(&b, "**Tags:** %s\n\n", strings.Join(item.Tags, ", "))
	}
	if len(detail.Providers) > 0 {
		fmt.Fprintf(&b, "**Detail providers:** %s\n\n", detailProviderLabels(detail.Providers))
	}
	if loadingLabel != "" {
		b.WriteString(loadingLabel)
		b.WriteString("\n\n")
	}
	if detailErr != "" {
		fmt.Fprintf(&b, "_Detail loaded with errors: %s_\n\n", detailErr)
	}
	for _, section := range detail.Sections {
		if strings.TrimSpace(section.Body) == "" {
			continue
		}
		fmt.Fprintf(&b, "## %s\n\n", section.Title)
		if section.URL != "" {
			fmt.Fprintf(&b, "[Open section](%s)\n\n", section.URL)
		}
		b.WriteString(section.Body)
		b.WriteString("\n\n")
	}
	writeDetailComments(&b, detail.Comments)
	return b.String()
}

func writeDetailComments(b *strings.Builder, comments []domain.DetailComment) {
	if len(comments) > 0 {
		b.WriteString("## Top Comments\n\n")
		for _, comment := range comments {
			if strings.TrimSpace(comment.Body) == "" {
				continue
			}
			author := comment.Author
			if author == "" {
				author = "unknown"
			}
			fmt.Fprintf(b, "### %s\n\n", author)
			if meta := detailCommentMeta(comment); meta != "" {
				fmt.Fprintf(b, "_%s_\n\n", meta)
			}
			if comment.URL != "" {
				fmt.Fprintf(b, "[Open comment](%s)\n\n", comment.URL)
			}
			b.WriteString(comment.Body)
			b.WriteString("\n\n")
		}
	}
}

func detailProviderLabels(providers []domain.SourceID) string {
	labels := make([]string, 0, len(providers))
	seen := make(map[domain.SourceID]bool, len(providers))
	for _, provider := range providers {
		if provider == "" || seen[provider] {
			continue
		}
		seen[provider] = true
		labels = append(labels, app.SourceLabel(provider))
	}
	return strings.Join(labels, ", ")
}

func detailCommentMeta(comment domain.DetailComment) string {
	var parts []string
	if comment.Source != "" {
		parts = append(parts, app.SourceLabel(comment.Source))
	}
	if comment.Score != nil {
		parts = append(parts, fmt.Sprintf("%d points", *comment.Score))
	}
	if comment.PublishedAt != nil {
		parts = append(parts, timeLabel(comment.PublishedAt))
	}
	return strings.Join(parts, " | ")
}

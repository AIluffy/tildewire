package tui

import (
	"fmt"
	"strings"

	"github.com/zhangxueai/tildewire/internal/app"
	"github.com/zhangxueai/tildewire/internal/domain"
)

func (m Model) renderPreview(width, height int) string {
	if height <= 0 {
		return ""
	}
	content := m.previewContentLines(width)
	visibleHeight := max(0, height-1)
	limit := maxPreviewOffset(len(content), visibleHeight)
	offset := clamp(m.previewOffset, 0, limit)
	header := m.previewHeader(offset, limit, width)
	if height == 1 {
		return header
	}
	end := min(len(content), offset+visibleHeight)
	lines := append([]string{header}, content[offset:end]...)
	return fillLines(lines, height)
}

func (m Model) previewHeader(offset, limit, width int) string {
	header := "PREVIEW"
	if entry, ok := m.selected(); ok {
		if source := entry.PrimarySource().Source; source != "" {
			header = fmt.Sprintf("PREVIEW [%s]", app.SourceBadge(source))
		}
	}
	if limit > 0 {
		header = fmt.Sprintf("%s %d/%d", header, offset+1, limit+1)
	}
	return clip(header, width)
}

func (m Model) previewContentLines(width int) []string {
	lines := []string{}
	entry, ok := m.selected()
	if !ok {
		lines = append(lines, "", mutedStyle.Render("Select an item."))
		return lines
	}
	lines = append(lines, "", activeStyle.Render(clip(entry.Item.Title, width)))
	if metrics := previewMetricLine(entry); metrics != "" {
		lines = append(lines, mutedStyle.Render(clip(metrics, width)))
	}
	if entry.Item.Summary != "" {
		lines = append(lines, "")
		lines = append(lines, wrap(entry.Item.Summary, width)...)
	}
	if cues := previewContentCueLines(entry, width); len(cues) > 0 {
		lines = append(lines, "")
		lines = append(lines, cues...)
	}
	if links := previewLinkLines(entry, width); len(links) > 0 {
		lines = append(lines, "")
		lines = append(lines, links...)
	}
	return lines
}

func previewMetricLine(entry domain.FeedEntry) string {
	source := entry.PrimarySource()
	metrics := mergePreviewMetrics(source.Metrics, entry.Item.Metrics)
	parts := previewRankParts(source)
	switch source.Source {
	case domain.SourceGitHub:
		parts = append(parts, githubPreviewMetricParts(metrics)...)
	case domain.SourceHackerNews:
		parts = append(parts, hackerNewsPreviewMetricParts(metrics)...)
	case domain.SourceHuggingFace:
		parts = append(parts, huggingFacePreviewMetricParts(entry, metrics)...)
	case domain.SourceLobsters:
		parts = append(parts, lobstersPreviewMetricParts(metrics)...)
	case domain.SourceProductHunt:
		parts = append(parts, productHuntPreviewMetricParts(metrics)...)
	}
	return strings.Join(parts, " | ")
}

func previewRankParts(source domain.ItemSource) []string {
	if source.SourceRank <= 0 {
		return nil
	}
	rank := fmt.Sprintf("#%d", source.SourceRank)
	if source.SourceView != "" {
		rank += " " + source.SourceView
	}
	return []string{rank}
}

func mergePreviewMetrics(primary, fallback domain.Metrics) domain.Metrics {
	if primary.Score == nil {
		primary.Score = fallback.Score
	}
	if primary.Upvotes == nil {
		primary.Upvotes = fallback.Upvotes
	}
	if primary.Comments == nil {
		primary.Comments = fallback.Comments
	}
	if primary.Stars == nil {
		primary.Stars = fallback.Stars
	}
	if primary.StarsToday == nil {
		primary.StarsToday = fallback.StarsToday
	}
	if primary.Forks == nil {
		primary.Forks = fallback.Forks
	}
	if primary.GitHubStars == nil {
		primary.GitHubStars = fallback.GitHubStars
	}
	return primary
}

func githubPreviewMetricParts(metrics domain.Metrics) []string {
	var parts []string
	if metrics.Stars != nil {
		parts = append(parts, fmt.Sprintf("%d stars", *metrics.Stars))
	}
	if metrics.StarsToday != nil {
		parts = append(parts, fmt.Sprintf("%d today", *metrics.StarsToday))
	}
	if metrics.Forks != nil {
		parts = append(parts, fmt.Sprintf("%d forks", *metrics.Forks))
	}
	return parts
}

func hackerNewsPreviewMetricParts(metrics domain.Metrics) []string {
	var parts []string
	if metrics.Upvotes != nil {
		parts = append(parts, fmt.Sprintf("%d points", *metrics.Upvotes))
	}
	if metrics.Comments != nil {
		parts = append(parts, fmt.Sprintf("%d comments", *metrics.Comments))
	}
	return parts
}

func huggingFacePreviewMetricParts(entry domain.FeedEntry, metrics domain.Metrics) []string {
	var parts []string
	if entry.Item.Refs.PaperID != "" {
		parts = append(parts, "paper "+entry.Item.Refs.PaperID)
	} else if entry.Item.Refs.ArxivID != "" {
		parts = append(parts, "arXiv "+entry.Item.Refs.ArxivID)
	}
	if entry.Item.Refs.Repo != "" {
		parts = append(parts, "repo "+entry.Item.Refs.Repo)
	}
	if metrics.Upvotes != nil {
		parts = append(parts, fmt.Sprintf("%d upvotes", *metrics.Upvotes))
	}
	if metrics.Comments != nil {
		parts = append(parts, fmt.Sprintf("%d comments", *metrics.Comments))
	}
	if metrics.GitHubStars != nil {
		parts = append(parts, fmt.Sprintf("%d github stars", *metrics.GitHubStars))
	}
	return parts
}

func lobstersPreviewMetricParts(metrics domain.Metrics) []string {
	var parts []string
	if metrics.Score != nil {
		parts = append(parts, fmt.Sprintf("%g pts", *metrics.Score))
	} else if metrics.Upvotes != nil {
		parts = append(parts, fmt.Sprintf("%d pts", *metrics.Upvotes))
	}
	if metrics.Comments != nil {
		parts = append(parts, fmt.Sprintf("%d comments", *metrics.Comments))
	}
	return parts
}

func productHuntPreviewMetricParts(metrics domain.Metrics) []string {
	var parts []string
	if metrics.Upvotes != nil {
		parts = append(parts, fmt.Sprintf("%d votes", *metrics.Upvotes))
	}
	if metrics.Comments != nil {
		parts = append(parts, fmt.Sprintf("%d comments", *metrics.Comments))
	}
	return parts
}

func previewContentCueLines(entry domain.FeedEntry, width int) []string {
	item := entry.Item
	var parts []string
	if item.Language != "" {
		parts = append(parts, item.Language)
	}
	if item.ItemType != "" {
		parts = append(parts, item.ItemType)
	}
	if item.Author != "" {
		parts = append(parts, "by "+item.Author)
	}
	if item.Organization != "" {
		parts = append(parts, item.Organization)
	}
	for _, tag := range item.Tags {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			parts = append(parts, tag)
		}
	}
	if len(parts) == 0 {
		return nil
	}
	return []string{mutedStyle.Render(clip(strings.Join(parts, " | "), width))}
}

func previewLinkLines(entry domain.FeedEntry, width int) []string {
	var lines []string
	if entry.Item.URL != "" {
		lines = append(lines, clip(entry.Item.URL, width))
	}
	if entry.Item.CommentsURL != "" && entry.Item.CommentsURL != entry.Item.URL {
		lines = append(lines, mutedStyle.Render(clip(entry.Item.CommentsURL, width)))
	}
	return lines
}

func maxPreviewOffset(totalLines, visibleHeight int) int {
	if visibleHeight <= 0 {
		return 0
	}
	return max(0, totalLines-visibleHeight)
}

func maxDetailOffset(totalLines, visibleHeight int) int {
	if visibleHeight <= 0 {
		return 0
	}
	return max(0, totalLines-visibleHeight)
}

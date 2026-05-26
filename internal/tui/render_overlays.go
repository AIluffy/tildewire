package tui

import (
	"fmt"
	"strings"

	"github.com/AIluffy/tildewire/internal/app"
	"github.com/AIluffy/tildewire/internal/domain"
)

func (m Model) renderPalette() string {
	width := max(52, m.width-6)
	commands := m.filteredPaletteCommands()
	filter := m.paletteFilter
	if filter == "" {
		filter = "Type to filter"
	}
	visible := m.paletteVisibleRows()
	start := clamp(m.paletteOffset, 0, max(0, len(commands)-visible))
	end := min(len(commands), start+visible)
	lines := []string{
		m.styles.header.Render("COMMAND PALETTE"),
		m.styles.muted.Render("> " + filter),
	}
	if len(commands) == 0 {
		lines = append(lines, m.styles.muted.Render("  No matching commands."))
	} else {
		for idx, command := range commands[start:end] {
			commandIndex := start + idx
			line := command.label
			if commandIndex == m.paletteCursor {
				lines = append(lines, m.styles.active.Render("> "+line))
			} else {
				lines = append(lines, "  "+line)
			}
		}
	}
	lines = append(lines, "", m.styles.muted.Render("j/k choose  enter run  esc cancel"))
	panel := m.styles.panel.Width(width).Render(strings.Join(lines, "\n"))
	return placeBlock(panel, max(0, (m.width-width)/2), 2, m.width)
}

func (m Model) renderSettings() string {
	return m.renderSettingsPanel()
}

func (m Model) renderRuleForm() string {
	width := max(48, m.width-4)
	return m.styles.panel.Width(width).Render(strings.Join([]string{
		m.styles.header.Render("PERSONALIZATION RULE"),
		"",
		m.ruleForm.View(),
	}, "\n"))
}

func (m Model) renderFilter() string {
	width := max(48, m.width-4)
	draftView := m.normalizeFilterDraftView(m.filterDraftView)
	draft := m.normalizeFilterDraftForView(draftView, m.filterDraft)
	rows := []string{
		m.styles.header.Render("tildewire FILTER"),
		"",
		m.filterRow(m.filterCursor == filterFieldSource, "Source", m.filterSourceLabel(draftView)),
		m.filterRow(m.filterCursor == filterFieldScope, "Scope", m.filterScopeValue(draftView, draft.SourceView)),
		m.filterRow(m.filterCursor == filterFieldSaved, "Saved", boolLabel(draft.SavedOnly)),
		m.filterRow(m.filterCursor == filterFieldUnread, "Unread", boolLabel(draft.UnreadOnly)),
		m.filterRow(m.filterCursor == filterFieldLanguage, "Language", emptyAsAll(draft.Language)),
		m.filterRow(m.filterCursor == filterFieldTag, "Tag", emptyAsAll(draft.Tag)),
		m.filterRow(m.filterCursor == filterFieldHidden, "Show hidden items", boolLabel(draft.IncludeHidden)),
		"",
		m.styles.muted.Render("j/k move  l/space change  type tag  c clear facets  enter apply  esc cancel"),
	}
	return m.styles.panel.Width(width).Render(strings.Join(rows, "\n"))
}

func (m Model) renderHealth() string {
	width := max(52, m.width-4)
	lines := []string{m.styles.header.Render("SOURCE HEALTH"), ""}
	statuses := m.visibleStatuses()
	if len(statuses) == 0 {
		lines = append(lines, m.styles.muted.Render("No source status yet."))
	}
	for _, status := range statuses {
		title := fmt.Sprintf("%s  %s", status.Name, status.Status)
		switch status.Status {
		case domain.SourceStatusOK:
			lines = append(lines, m.styles.ok.Render(title))
		case domain.SourceStatusRefreshing, domain.SourceStatusStale, domain.SourceStatusRateLimited:
			lines = append(lines, m.styles.warn.Render(title))
		case domain.SourceStatusNetworkError, domain.SourceStatusParserBroken, domain.SourceStatusAuthRequired:
			lines = append(lines, m.styles.err.Render(title))
		default:
			lines = append(lines, m.styles.muted.Render(title))
		}
		lines = append(lines, "  last fetch: "+timeLabel(status.LastFetchAt))
		lines = append(lines, "  last success: "+timeLabel(status.LastSuccessAt))
		if status.LastError != "" {
			for _, line := range wrap("  error: "+status.LastError, width-4) {
				lines = append(lines, m.styles.err.Render(line))
			}
		}
		lines = append(lines, "")
	}
	lines = append(lines, m.styles.header.Render("RECENT FETCHES"), "")
	fetchHistory := m.visibleFetchHistory()
	if len(fetchHistory) == 0 {
		lines = append(lines, m.styles.muted.Render("No fetch history yet."), "")
	}
	for _, event := range fetchHistory {
		title := fmt.Sprintf("%s/%s  %s  %s  %s", app.SourceShortLabel(event.Source), event.SourceView, event.Status, event.Duration, itemCountLabel(event.ItemCount))
		switch event.Status {
		case domain.SourceStatusOK:
			lines = append(lines, m.styles.ok.Render(title))
		case domain.SourceStatusRefreshing, domain.SourceStatusStale, domain.SourceStatusRateLimited:
			lines = append(lines, m.styles.warn.Render(title))
		case domain.SourceStatusNetworkError, domain.SourceStatusParserBroken, domain.SourceStatusAuthRequired:
			lines = append(lines, m.styles.err.Render(title))
		default:
			lines = append(lines, m.styles.muted.Render(title))
		}
		if event.StaleReason != "" {
			lines = append(lines, "  stale: "+event.StaleReason)
		}
		if event.Error != "" {
			for _, line := range wrap("  error: "+event.Error, width-4) {
				lines = append(lines, m.styles.err.Render(line))
			}
		}
		lines = append(lines, "  finished: "+timeLabel(&event.FinishedAt))
		lines = append(lines, "")
	}
	lines = append(lines, m.styles.muted.Render("r retry  esc back  ? help  q quit"))
	if m.help.ShowAll {
		lines = append(lines, "", m.help.View(m.keys))
	}
	return m.styles.panel.Width(width).Render(strings.Join(lines, "\n"))
}

func (m Model) renderRules() string {
	width := max(52, m.width-4)
	lines := []string{m.styles.header.Render("PERSONALIZATION RULES"), ""}
	if len(m.rules) == 0 {
		lines = append(lines, m.styles.muted.Render("No personalization rules yet."))
	}
	for idx, rule := range m.rules {
		prefix := "  "
		if idx == m.ruleCursor {
			prefix = "> "
		}
		state := "off"
		if rule.Enabled {
			state = "on"
		}
		line := fmt.Sprintf("%s[%s] %s %s:%s", prefix, state, rule.Effect, rule.Target, rule.Value)
		if idx == m.ruleCursor {
			lines = append(lines, m.styles.active.Render(clip(line, width-2)))
		} else {
			lines = append(lines, clip(line, width-2))
		}
	}
	lines = append(lines, "", m.styles.muted.Render("j/k choose  a add  e edit  space toggle  d delete  esc back  ? help  q quit"))
	if m.help.ShowAll {
		lines = append(lines, "", m.help.View(m.keys))
	}
	return m.styles.panel.Width(width).Render(strings.Join(lines, "\n"))
}

func (m Model) renderDedupeCandidates() string {
	width := max(64, m.width-4)
	lines := []string{m.styles.header.Render("DEDUPE CANDIDATES"), ""}
	if len(m.dedupeCandidates) == 0 {
		lines = append(lines, m.styles.muted.Render("No dedupe candidates yet."))
	}
	for idx, candidate := range m.dedupeCandidates {
		prefix := "  "
		if idx == m.dedupeCursor {
			prefix = "> "
		}
		title := fmt.Sprintf("%s%.2f d=%d %s", prefix, candidate.Score, candidate.Distance, candidate.Reason)
		if idx == m.dedupeCursor {
			lines = append(lines, m.styles.active.Render(clip(title, width-2)))
		} else {
			lines = append(lines, clip(title, width-2))
		}
		lines = append(lines, m.styles.muted.Render(clip("    A: "+candidate.ItemA.Title, width-4)))
		lines = append(lines, m.styles.muted.Render(clip("    B: "+candidate.ItemB.Title, width-4)))
	}
	if candidate, ok := m.selectedDedupeCandidate(); ok {
		lines = append(lines, m.dedupeCandidateDebugLines(candidate, width)...)
	}
	lines = append(lines, "", m.styles.muted.Render("j/k choose  i ignore  esc back  ? help  q quit"))
	if m.help.ShowAll {
		lines = append(lines, "", m.help.View(m.keys))
	}
	return m.styles.panel.Width(width).Render(strings.Join(lines, "\n"))
}

func (m Model) dedupeCandidateDebugLines(candidate domain.DedupeCandidate, width int) []string {
	lines := []string{
		"",
		m.styles.header.Render("SELECTED CANDIDATE"),
		clip(fmt.Sprintf("key %s", candidate.Key), width-2),
		clip(fmt.Sprintf("score %.2f  distance %d", candidate.Score, candidate.Distance), width-2),
		clip("reason "+candidate.Reason, width-2),
	}
	if !candidate.CreatedAt.IsZero() || !candidate.UpdatedAt.IsZero() {
		lines = append(lines, clip(fmt.Sprintf("created %s  updated %s", timeLabel(&candidate.CreatedAt), timeLabel(&candidate.UpdatedAt)), width-2))
	}
	lines = append(lines, m.dedupeItemDebugLines("A", candidate.ItemA, width)...)
	lines = append(lines, m.dedupeItemDebugLines("B", candidate.ItemB, width)...)
	return lines
}

func (m Model) dedupeItemDebugLines(label string, item domain.FeedItem, width int) []string {
	lines := []string{"", m.styles.active.Render(label + " " + clip(item.Title, max(8, width-4)))}
	if source, ok := primaryItemSource(item); ok {
		sourceLine := fmt.Sprintf("%s/%s rank %d", app.SourceLabel(source.Source), source.SourceView, source.SourceRank)
		lines = append(lines, clip(label+" source "+sourceLine, width-2))
	}
	if item.CanonicalKey != "" {
		lines = append(lines, clip(label+" canonical "+item.CanonicalKey, width-2))
	}
	if refs := dedupeRefsLine(item); refs != "" {
		lines = append(lines, clip(label+" refs "+refs, width-2))
	}
	if item.SimHash != "" {
		lines = append(lines, clip(label+" simhash "+item.SimHash, width-2))
	}
	if item.URL != "" {
		lines = append(lines, clip(label+" url "+item.URL, width-2))
	}
	return lines
}

func primaryItemSource(item domain.FeedItem) (domain.ItemSource, bool) {
	if len(item.Sources) == 0 {
		return domain.ItemSource{}, false
	}
	best := item.Sources[0]
	for _, source := range item.Sources[1:] {
		if source.SourceRank > 0 && (best.SourceRank == 0 || source.SourceRank < best.SourceRank) {
			best = source
		}
	}
	return best, true
}

func dedupeRefsLine(item domain.FeedItem) string {
	var refs []string
	if item.Refs.Repo != "" {
		refs = append(refs, "repo="+item.Refs.Repo)
	}
	if item.Refs.ArxivID != "" {
		refs = append(refs, "arxiv="+item.Refs.ArxivID)
	}
	return strings.Join(refs, " ")
}

func itemCountLabel(count int) string {
	if count == 1 {
		return "1 item"
	}
	return fmt.Sprintf("%d items", count)
}

func (m Model) filterRow(active bool, label, value string) string {
	line := fmt.Sprintf("%-10s %s", label, value)
	if active {
		return m.styles.active.Render("> " + line)
	}
	return "  " + line
}

func (m Model) filterScopeValue(source domain.SourceID, sourceView string) string {
	source = m.normalizeFilterDraftView(source)
	if source == domain.SourceAll {
		return "all"
	}
	return sourceViewLabel(source, sourceView)
}

func emptyAsAll(value string) string {
	if strings.TrimSpace(value) == "" {
		return "all"
	}
	return value
}

func boolLabel(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

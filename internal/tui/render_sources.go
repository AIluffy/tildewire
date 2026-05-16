package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/zhangxueai/tildewire/internal/app"
	"github.com/zhangxueai/tildewire/internal/domain"
)

type sourceRow struct {
	source domain.SourceID
	label  string
}

func (m Model) sourceRows() []sourceRow {
	rows := []sourceRow{
		{},
		{source: domain.SourceAll, label: "All"},
	}
	for _, entry := range m.sourceCatalog() {
		rows = append(rows, sourceRow{source: entry.Source, label: entry.Label})
	}
	return rows
}

func (m Model) sourceCatalog() []app.SourceCatalogEntry {
	return sourceCatalogForOrder(m.config.SourceOrder, m.enabledSourceSet())
}

func sourceCatalogForOrder(order []string, enabled map[domain.SourceID]bool) []app.SourceCatalogEntry {
	defaultCatalog := app.SourceCatalog()
	if len(order) == 0 {
		return filterSourceCatalog(defaultCatalog, enabled)
	}
	bySource := make(map[domain.SourceID]app.SourceCatalogEntry, len(defaultCatalog))
	for _, entry := range defaultCatalog {
		bySource[entry.Source] = entry
	}
	seen := make(map[domain.SourceID]bool, len(defaultCatalog))
	ordered := make([]app.SourceCatalogEntry, 0, len(defaultCatalog))
	for _, value := range order {
		source := domain.SourceID(strings.ToLower(strings.TrimSpace(value)))
		entry, ok := bySource[source]
		if !ok || seen[source] || !enabled[source] {
			continue
		}
		seen[source] = true
		ordered = append(ordered, entry)
	}
	for _, entry := range defaultCatalog {
		if !seen[entry.Source] && enabled[entry.Source] {
			ordered = append(ordered, entry)
		}
	}
	return ordered
}

func filterSourceCatalog(catalog []app.SourceCatalogEntry, enabled map[domain.SourceID]bool) []app.SourceCatalogEntry {
	filtered := make([]app.SourceCatalogEntry, 0, len(catalog))
	for _, entry := range catalog {
		if enabled[entry.Source] {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func sourceOrderFromCatalog(catalog []app.SourceCatalogEntry) []string {
	order := make([]string, 0, len(catalog))
	for _, entry := range catalog {
		order = append(order, string(entry.Source))
	}
	return order
}

func (m Model) enabledSourceSet() map[domain.SourceID]bool {
	return enabledSourceSetFromConfig(m.config.EnabledSources)
}

func (m Model) enabledSourceIDs() []domain.SourceID {
	enabled := m.enabledSourceSet()
	sources := make([]domain.SourceID, 0, len(enabled))
	for _, source := range app.SourceIDs() {
		if enabled[source] {
			sources = append(sources, source)
		}
	}
	return sources
}

func (m Model) sourceEnabled(source domain.SourceID) bool {
	if source == "" || source == domain.SourceAll {
		return true
	}
	return m.enabledSourceSet()[source]
}

func (m Model) visibleStatuses() []domain.SourceHealth {
	if len(m.statuses) == 0 {
		return nil
	}
	statuses := make([]domain.SourceHealth, 0, len(m.statuses))
	for _, status := range m.statuses {
		if m.sourceEnabled(status.Source) {
			statuses = append(statuses, status)
		}
	}
	return statuses
}

func (m Model) visibleFetchHistory() []domain.FetchEvent {
	if len(m.fetchHistory) == 0 {
		return nil
	}
	events := make([]domain.FetchEvent, 0, len(m.fetchHistory))
	for _, event := range m.fetchHistory {
		if m.sourceEnabled(event.Source) {
			events = append(events, event)
		}
	}
	return events
}

func enabledSourceSetFromConfig(values []string) map[domain.SourceID]bool {
	if len(values) == 0 {
		values = sourceStrings(app.SourceIDs())
	}
	allowed := make(map[domain.SourceID]bool)
	for _, source := range app.SourceIDs() {
		allowed[source] = true
	}
	enabled := make(map[domain.SourceID]bool, len(values))
	for _, value := range values {
		source := domain.SourceID(strings.ToLower(strings.TrimSpace(value)))
		if allowed[source] {
			enabled[source] = true
		}
	}
	if len(enabled) == 0 {
		for source := range allowed {
			enabled[source] = true
		}
	}
	return enabled
}

func (r sourceRow) render(m Model) string {
	if r.source == "" {
		return ""
	}
	return sourceLine(m.view == r.source, r.label, m.sourceCount(r.source))
}

func sourceLine(active bool, label string, count int) string {
	line := fmt.Sprintf("%-12s %3d", label, count)
	if active {
		return activeStyle.Render(line)
	}
	return line
}

func (m Model) sourceCount(source domain.SourceID) int {
	if source == m.view && source != domain.SourceAll {
		return len(m.entries)
	}
	if m.counts == nil {
		if source == domain.SourceAll {
			return len(m.entries)
		}
		return 0
	}
	return m.counts[source]
}

func stateFlags(state domain.ItemState) string {
	var flags []string
	if state.Saved {
		flags = append(flags, "saved")
	}
	if state.Read {
		flags = append(flags, "read")
	}
	if state.Hidden {
		flags = append(flags, "hidden")
	}
	if len(flags) == 0 {
		return ""
	}
	return "(" + strings.Join(flags, ",") + ")"
}

func timeLabel(value *time.Time) string {
	if value == nil || value.IsZero() {
		return "never"
	}
	return value.Local().Format("2006-01-02 15:04:05")
}

func shortSource(source domain.SourceID) string {
	return app.SourceShortLabel(source)
}

func sourceBadge(source domain.SourceID) string {
	return app.SourceBadge(source)
}

func renderFeedItemLine(idx int, entry domain.FeedEntry, active bool, width int, search string) string {
	prefix := "  "
	if active {
		prefix = "> "
	}
	lead := fmt.Sprintf("%s%d ", prefix, idx+1)
	tail := entry.Item.Title
	if flags := stateFlags(entry.State); flags != "" {
		tail += " " + flags
	}
	tokens := searchQueryTokens(search)
	renderedTail := renderSearchHighlightedTextWithStyle(tail, tokens, lipgloss.NewStyle())
	line := lead + renderSourceBadge(entry.PrimarySource().Source) + " " + renderedTail
	if active {
		line = activeStyle.Render(lead) + renderSourceBadge(entry.PrimarySource().Source) + renderSearchHighlightedTextWithStyle(" "+tail, tokens, activeStyle)
	}
	return clip(line, width)
}

func renderSourceBadge(source domain.SourceID) string {
	return sourceBadgeStyle(source).Render("[" + sourceBadge(source) + "]")
}

func sourceBadgeStyle(source domain.SourceID) lipgloss.Style {
	switch source {
	case domain.SourceGitHub:
		return ghStyle
	case domain.SourceHackerNews:
		return hnStyle
	case domain.SourceHuggingFace:
		return hfStyle
	case domain.SourceLobsters:
		return lobStyle
	case domain.SourceProductHunt:
		return phStyle
	default:
		return mutedStyle
	}
}

func (m Model) renderStatus() string {
	var parts []string
	for _, status := range m.visibleStatuses() {
		label := fmt.Sprintf("%s:%s", shortSource(status.Source), status.Status)
		switch status.Status {
		case domain.SourceStatusOK:
			parts = append(parts, okStyle.Render(label))
		case domain.SourceStatusRefreshing, domain.SourceStatusStale, domain.SourceStatusRateLimited:
			parts = append(parts, warnStyle.Render(label))
		case domain.SourceStatusNetworkError, domain.SourceStatusParserBroken, domain.SourceStatusAuthRequired:
			parts = append(parts, errStyle.Render(label))
		default:
			parts = append(parts, mutedStyle.Render(label))
		}
	}
	if m.message != "" {
		parts = append(parts, m.message)
	}
	if m.lastError != "" {
		parts = append(parts, errStyle.Render(clip(m.lastError, max(20, m.width/2))))
	}
	return clip(strings.Join(parts, " | "), m.width)
}

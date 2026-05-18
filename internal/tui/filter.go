package tui

import (
	"sort"
	"strings"

	"github.com/AIluffy/tildewire/internal/app"
	"github.com/AIluffy/tildewire/internal/domain"
)

const (
	filterFieldSource = iota
	filterFieldScope
	filterFieldSaved
	filterFieldUnread
	filterFieldLanguage
	filterFieldTag
	filterFieldHidden
	filterFieldCount
)

var primaryFilterLanguages = []string{"go", "rust", "python", "typescript"}

func (m *Model) openFilter() {
	m.filterOpen = true
	m.filterDraft = m.filter
	m.filterDraftView = m.view
	m.filterCursor = 0
	m.detail = false
	m.health = false
	m.rulesOpen = false
	m.dedupeOpen = false
	m.paletteOpen = false
	m.message = "filter"
}

func (m *Model) cycleFilterDraftField(direction int) {
	switch m.filterCursor {
	case filterFieldSource:
		m.filterDraftView = m.cycleFilterSource(m.filterDraftView, direction)
		m.filterDraft.SourceView = defaultSourceView(m.filterDraftView)
	case filterFieldScope:
		m.filterDraft.SourceView = cycleFilterScope(m.filterDraftView, m.filterDraft.SourceView, direction)
	case filterFieldSaved:
		m.filterDraft.SavedOnly = !m.filterDraft.SavedOnly
	case filterFieldUnread:
		m.filterDraft.UnreadOnly = !m.filterDraft.UnreadOnly
	case filterFieldLanguage:
		m.filterDraft.Language = cycleStringOption(m.filterDraft.Language, m.filterLanguageOptions(), direction)
	case filterFieldTag:
		m.filterDraft.Tag = cycleStringOption(m.filterDraft.Tag, m.filterTagOptions(), direction)
	case filterFieldHidden:
		m.filterDraft.IncludeHidden = !m.filterDraft.IncludeHidden
	}
}

func cycleStringOption(current string, values []string, direction int) string {
	if len(values) == 0 {
		return ""
	}
	current = strings.ToLower(strings.TrimSpace(current))
	idx := 0
	for i, value := range values {
		if value == current {
			idx = i
			break
		}
	}
	idx = (idx + direction) % len(values)
	if idx < 0 {
		idx += len(values)
	}
	return values[idx]
}

func (m Model) cycleFilterSource(current domain.SourceID, direction int) domain.SourceID {
	values := append([]domain.SourceID{domain.SourceAll}, m.enabledSourceIDs()...)
	current = m.normalizeFilterDraftView(current)
	idx := 0
	for i, value := range values {
		if value == current {
			idx = i
			break
		}
	}
	idx = (idx + direction) % len(values)
	if idx < 0 {
		idx += len(values)
	}
	return values[idx]
}

func cycleFilterScope(source domain.SourceID, current string, direction int) string {
	if source == domain.SourceAll {
		return ""
	}
	sourceViews := app.SourceViews(source)
	if len(sourceViews) == 0 {
		return ""
	}
	values := make([]string, 0, len(sourceViews))
	for _, sourceView := range sourceViews {
		values = append(values, sourceView.View)
	}
	return cycleStringOption(current, values, direction)
}

func (m Model) filterLanguageOptions() []string {
	values := []string{""}
	seen := map[string]bool{"": true}
	addFilterOptionValues(&values, seen, primaryFilterLanguages)
	for _, entry := range m.entries {
		addFilterOptionValue(&values, seen, entry.Item.Language)
	}
	for _, option := range app.GitHubScopeOptions(app.GitHubScopeLanguage) {
		addFilterOptionValue(&values, seen, option.Value)
	}
	addFilterOptionValue(&values, seen, m.filterDraft.Language)
	return values
}

func (m Model) filterTagOptions() []string {
	values := []string{""}
	seen := map[string]bool{"": true}
	var tags []string
	for _, entry := range m.entries {
		tags = append(tags, entry.Item.Tags...)
	}
	sort.Slice(tags, func(i, j int) bool {
		return strings.ToLower(tags[i]) < strings.ToLower(tags[j])
	})
	addFilterOptionValues(&values, seen, tags)
	addFilterOptionValue(&values, seen, m.filterDraft.Tag)
	return values
}

func addFilterOptionValues(values *[]string, seen map[string]bool, options []string) {
	for _, option := range options {
		addFilterOptionValue(values, seen, option)
	}
}

func addFilterOptionValue(values *[]string, seen map[string]bool, value string) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || seen[value] {
		return
	}
	seen[value] = true
	*values = append(*values, value)
}

func clearFilterFacets(filter app.FeedFilter) app.FeedFilter {
	filter.SavedOnly = false
	filter.UnreadOnly = false
	filter.Language = ""
	filter.Tag = ""
	filter.IncludeHidden = false
	return filter
}

func normalizeFilterDraft(filter app.FeedFilter) app.FeedFilter {
	filter.Language = strings.ToLower(strings.TrimSpace(filter.Language))
	filter.Tag = strings.ToLower(strings.TrimSpace(filter.Tag))
	return filter
}

func (m Model) normalizeFilterDraftView(view domain.SourceID) domain.SourceID {
	if view == "" {
		return domain.SourceAll
	}
	if view == domain.SourceAll {
		return view
	}
	for _, source := range m.enabledSourceIDs() {
		if source == view {
			return view
		}
	}
	return domain.SourceAll
}

func (m Model) normalizeFilterDraftForView(view domain.SourceID, filter app.FeedFilter) app.FeedFilter {
	filter = normalizeFilterDraft(filter)
	view = m.normalizeFilterDraftView(view)
	if view == domain.SourceAll {
		filter.SourceView = ""
		return filter
	}
	if _, ok := app.ScopeForSourceView(view, filter.SourceView); !ok || strings.TrimSpace(filter.SourceView) == "" {
		filter.SourceView = defaultSourceView(view)
	}
	return filter
}

func filterLabel(filter app.FeedFilter) string {
	var parts []string
	if filter.SavedOnly {
		parts = append(parts, "saved")
	}
	if filter.UnreadOnly {
		parts = append(parts, "unread")
	}
	if len(parts) == 0 {
		parts = append(parts, "all")
	}
	if filter.Language != "" {
		parts = append(parts, filter.Language)
	}
	if filter.Tag != "" {
		parts = append(parts, filter.Tag)
	}
	if filter.IncludeHidden {
		parts = append(parts, "hidden")
	}
	return strings.Join(parts, " ")
}

func (m Model) filterSourceLabel(source domain.SourceID) string {
	source = m.normalizeFilterDraftView(source)
	if source == domain.SourceAll {
		return "All"
	}
	return app.SourceLabel(source)
}

func defaultSourceView(source domain.SourceID) string {
	return app.DefaultFeedSourceView(source)
}

func nextSourceView(source domain.SourceID, current string) (string, bool) {
	sourceViews := app.SourceViews(source)
	views := make([]string, 0, len(sourceViews))
	for _, view := range sourceViews {
		views = append(views, view.View)
	}
	if len(views) <= 1 {
		return "", false
	}
	current = strings.ToLower(strings.TrimSpace(current))
	for idx, view := range views {
		if view == current {
			return views[(idx+1)%len(views)], true
		}
	}
	return views[0], true
}

func sourceViewLabel(source domain.SourceID, sourceView string) string {
	return app.SourceViewLabel(source, sourceView)
}

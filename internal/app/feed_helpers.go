package app

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/AIluffy/tildewire/internal/domain"
	"github.com/AIluffy/tildewire/internal/score"
)

func (s *Service) preferenceProfileForView(ctx context.Context, view domain.SourceID) (domain.PreferenceProfile, error) {
	if view != "" && view != domain.SourceAll && view != domain.SourceRecommend {
		return domain.PreferenceProfile{}, nil
	}
	return s.store.PreferenceProfile(ctx)
}

func (s *Service) sourceTabCounts(ctx context.Context) (map[domain.SourceID]int, error) {
	storedCounts, err := s.store.SourceCounts(ctx)
	if err != nil {
		return nil, err
	}
	sourceViewCounts, err := s.store.SourceViewCounts(ctx)
	if err != nil {
		return nil, err
	}
	counts := make(map[domain.SourceID]int, len(s.sources)+2)
	for _, source := range s.sources {
		counts[source] = storedCounts[source]
	}
	for _, source := range s.sources {
		viewKeys := s.primarySourceViewKeys(source)
		if len(viewKeys) == 0 {
			continue
		}
		total := 0
		for _, viewKey := range viewKeys {
			total += sourceViewCounts[source][viewKey]
		}
		counts[source] = total
	}
	recommendCount, err := s.store.CountRecommendedFeed(ctx, domain.FeedQuery{})
	if err != nil {
		return nil, err
	}
	counts[domain.SourceRecommend] = capRecommendCount(recommendCount)
	counts[domain.SourceAll] = sumSourceCounts(counts, s.sources)
	return counts, nil
}

func capRecommendCount(count int) int {
	return min(count, recommendDisplayLimit)
}

func (s *Service) primarySourceViewKeys(source domain.SourceID) []string {
	for _, adapter := range s.adapters {
		if adapter.Source() != source {
			continue
		}
		scopes := primaryScopes(adapter)
		keys := make([]string, 0, len(scopes))
		for _, scope := range scopes {
			keys = append(keys, sourceViewKey(scope))
		}
		return keys
	}
	sourceView := DefaultSourceView(source)
	if sourceView == "" {
		return nil
	}
	return []string{sourceView}
}

func activePersonalizationRules(rules []domain.PersonalizationRule) []domain.PersonalizationRule {
	active := make([]domain.PersonalizationRule, 0, len(rules))
	for _, rule := range rules {
		if rule.Enabled {
			active = append(active, rule)
		}
	}
	return active
}

func (s *Service) filterEntriesForEnabledSources(entries []domain.FeedEntry, view domain.SourceID) []domain.FeedEntry {
	if len(entries) == 0 {
		return entries
	}
	filtered := make([]domain.FeedEntry, 0, len(entries))
	for _, entry := range entries {
		sources := s.filterItemSourcesForEnabledSources(entry.Sources)
		if len(sources) == 0 && (view == "" || view == domain.SourceAll || view == domain.SourceRecommend) {
			continue
		}
		entry.Sources = sources
		entry.Item.Sources = sources
		filtered = append(filtered, entry)
	}
	return filtered
}

func (s *Service) filterItemSourcesForEnabledSources(sources []domain.ItemSource) []domain.ItemSource {
	if len(sources) == 0 {
		return sources
	}
	filtered := make([]domain.ItemSource, 0, len(sources))
	for _, source := range sources {
		if s.sourceEnabled(source.Source) {
			filtered = append(filtered, source)
		}
	}
	return filtered
}

func (s *Service) filterStatusesForEnabledSources(statuses []domain.SourceHealth) []domain.SourceHealth {
	if len(statuses) == 0 {
		return statuses
	}
	filtered := make([]domain.SourceHealth, 0, len(statuses))
	for _, status := range statuses {
		if s.sourceEnabled(status.Source) {
			filtered = append(filtered, status)
		}
	}
	return filtered
}

func (s *Service) filterFetchHistoryForEnabledSources(events []domain.FetchEvent) []domain.FetchEvent {
	if len(events) == 0 {
		return events
	}
	filtered := make([]domain.FetchEvent, 0, len(events))
	for _, event := range events {
		if s.sourceEnabled(event.Source) {
			filtered = append(filtered, event)
		}
	}
	return filtered
}

func (s *Service) sourceEnabled(source domain.SourceID) bool {
	if source == "" || source == domain.SourceAll || source == domain.SourceRecommend {
		return true
	}
	if len(s.enabled) == 0 {
		for _, known := range SourceIDs() {
			if source == known {
				return true
			}
		}
		return true
	}
	return s.enabled[source]
}

func sumSourceCounts(counts map[domain.SourceID]int, sources []domain.SourceID) int {
	total := 0
	for _, source := range sources {
		total += counts[source]
	}
	return total
}

func enabledSourceSet(sources []domain.SourceID) map[domain.SourceID]bool {
	if len(sources) == 0 {
		return nil
	}
	enabled := make(map[domain.SourceID]bool, len(sources))
	for _, source := range sources {
		if source == "" || source == domain.SourceAll || source == domain.SourceRecommend {
			continue
		}
		enabled[source] = true
	}
	if len(enabled) == 0 {
		return nil
	}
	return enabled
}

func applySort(entries []domain.FeedEntry, view domain.SourceID, now time.Time, rules []domain.PersonalizationRule, profile domain.PreferenceProfile) {
	for idx := range entries {
		entries[idx].HotScore = score.Hot(entries[idx], now)
		if view == "" || view == domain.SourceAll {
			entries[idx].HotScore += personalizationAdjustment(entries[idx], rules, profile)
		}
	}
	if view != "" && view != domain.SourceAll {
		sort.SliceStable(entries, func(i, j int) bool {
			return entries[i].PrimarySource().SourceRank < entries[j].PrimarySource().SourceRank
		})
		return
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].HotScore == entries[j].HotScore {
			return entries[i].Item.LastSeenAt.After(entries[j].Item.LastSeenAt)
		}
		return entries[i].HotScore > entries[j].HotScore
	})
}

func normalizeFilter(filter FeedFilter) FeedFilter {
	filter.Search = strings.TrimSpace(filter.Search)
	filter.SourceView = strings.ToLower(strings.TrimSpace(filter.SourceView))
	filter.Language = strings.ToLower(strings.TrimSpace(filter.Language))
	filter.Tag = strings.ToLower(strings.TrimSpace(filter.Tag))
	return filter
}

func normalizeViewFilter(view domain.SourceID, filter FeedFilter) FeedFilter {
	filter = normalizeFilter(filter)
	if view == domain.SourceRecommend {
		filter.SourceView = ""
		return filter
	}
	if view != "" && view != domain.SourceAll && filter.SourceView == "" {
		filter.SourceView = DefaultFeedSourceView(view)
	}
	return filter
}

func orderedAdapters(adapters []SourceAdapter) []SourceAdapter {
	order := []domain.SourceID{domain.SourceGitHub, domain.SourceAILabs, domain.SourceHuggingFace, domain.SourceLobsters, domain.SourceProductHunt, domain.SourceHackerNews}
	bySource := make(map[domain.SourceID]SourceAdapter, len(adapters))
	for _, adapter := range adapters {
		bySource[adapter.Source()] = adapter
	}
	ordered := make([]SourceAdapter, 0, len(adapters))
	seen := make(map[domain.SourceID]bool, len(bySource))
	for _, source := range order {
		adapter, ok := bySource[source]
		if !ok {
			continue
		}
		ordered = append(ordered, adapter)
		seen[source] = true
	}
	var remaining []domain.SourceID
	for source := range bySource {
		if !seen[source] {
			remaining = append(remaining, source)
		}
	}
	sort.Slice(remaining, func(i, j int) bool {
		return remaining[i] < remaining[j]
	})
	for _, source := range remaining {
		ordered = append(ordered, bySource[source])
	}
	return ordered
}

func countSources(adapters []SourceAdapter, enabled map[domain.SourceID]bool) []domain.SourceID {
	sources := SourceIDs()
	seen := make(map[domain.SourceID]bool, len(sources)+len(adapters))
	filtered := make([]domain.SourceID, 0, len(sources))
	for _, source := range sources {
		if len(enabled) > 0 && !enabled[source] {
			continue
		}
		seen[source] = true
		filtered = append(filtered, source)
	}
	var extra []domain.SourceID
	for _, adapter := range adapters {
		source := adapter.Source()
		if seen[source] || (len(enabled) > 0 && !enabled[source]) {
			continue
		}
		seen[source] = true
		extra = append(extra, source)
	}
	sort.Slice(extra, func(i, j int) bool {
		return extra[i] < extra[j]
	})
	return append(filtered, extra...)
}

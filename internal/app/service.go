package app

import (
	"context"
	"sync"
	"time"

	"github.com/AIluffy/tildewire/internal/domain"
	"github.com/AIluffy/tildewire/internal/httpx"
)

// Service coordinates stores, sources, sorting, and user state.
type Service struct {
	store    FeedStore
	client   httpx.Requester
	adapters []SourceAdapter
	sources  []domain.SourceID
	enabled  map[domain.SourceID]bool
	storeMu  sync.Mutex
}

// Snapshot is the TUI-ready feed state.
type Snapshot struct {
	Entries          []domain.FeedEntry
	Statuses         []domain.SourceHealth
	FetchHistory     []domain.FetchEvent
	Rules            []domain.PersonalizationRule
	DedupeCandidates []domain.DedupeCandidate
	Counts           map[domain.SourceID]int
	View             domain.SourceID
	Filter           FeedFilter
	LoadedAt         time.Time
}

// FeedFilter controls visible feed filtering.
type FeedFilter struct {
	Search        string
	SourceView    string
	SavedOnly     bool
	UnreadOnly    bool
	Language      string
	Tag           string
	IncludeHidden bool
}

// RefreshMode controls which source scopes are refreshed.
type RefreshMode string

const (
	// RefreshModeAll refreshes every adapter default scope.
	RefreshModeAll RefreshMode = "all"
	// RefreshModeStartup refreshes the minimum useful scope for each source.
	RefreshModeStartup RefreshMode = "startup"
	// RefreshModeVisible refreshes the active source view, or every primary scope when All is visible.
	RefreshModeVisible RefreshMode = "visible"
)

// RefreshOptions controls background versus user-initiated refresh behavior.
type RefreshOptions struct {
	Force bool
	Mode  RefreshMode
}

// NewService creates an application service.
func NewService(store FeedStore, client httpx.Requester, adapters []SourceAdapter) *Service {
	ordered := orderedAdapters(adapters)
	enabled := enabledSourceSet(nil)
	return &Service{
		store:    store,
		client:   client,
		adapters: ordered,
		sources:  countSources(ordered, enabled),
		enabled:  enabled,
	}
}

// SetSourceConfig applies source visibility and token settings without rebuilding the service.
func (s *Service) SetSourceConfig(enabled []domain.SourceID, tokens map[domain.SourceID]string) {
	s.enabled = enabledSourceSet(enabled)
	s.sources = countSources(s.adapters, s.enabled)
	for _, adapter := range s.adapters {
		setter, ok := adapter.(TokenAdapter)
		if !ok {
			continue
		}
		token, ok := tokens[adapter.Source()]
		if !ok {
			continue
		}
		setter.SetToken(token)
	}
}

// SetHTTPCacheTTL updates the raw HTTP cache lifetime used by source refreshes.
func (s *Service) SetHTTPCacheTTL(ttl time.Duration) {
	if s.client == nil {
		return
	}
	setter, ok := s.client.(httpx.CacheTTLSetter)
	if !ok {
		return
	}
	setter.SetCacheTTL(ttl)
}

// LoadFeed loads cached visible items for a view and filter.
func (s *Service) LoadFeed(ctx context.Context, view domain.SourceID, filter FeedFilter) (Snapshot, error) {
	if view != "" && view != domain.SourceAll && !s.sourceEnabled(view) {
		view = domain.SourceAll
		filter.SourceView = ""
	}
	filter = normalizeViewFilter(view, filter)
	query := domain.FeedQuery{
		Limit:         250,
		Search:        filter.Search,
		SourceView:    filter.SourceView,
		SavedOnly:     filter.SavedOnly,
		UnreadOnly:    filter.UnreadOnly,
		Language:      filter.Language,
		Tag:           filter.Tag,
		IncludeHidden: filter.IncludeHidden,
	}
	if view != "" && view != domain.SourceAll {
		query.Source = view
	}
	entries, err := s.store.ListFeed(ctx, query)
	if err != nil {
		return Snapshot{}, err
	}
	rules, err := s.store.ListPersonalizationRules(ctx, true)
	if err != nil {
		return Snapshot{}, err
	}
	activeRules := activePersonalizationRules(rules)
	if !filter.IncludeHidden {
		entries = filterPersonalizedHidden(entries, activeRules)
	}
	entries = s.filterEntriesForEnabledSources(entries, view)
	profile, err := s.preferenceProfileForView(ctx, view)
	if err != nil {
		return Snapshot{}, err
	}
	applySort(entries, view, time.Now().UTC(), activeRules, profile)
	statuses, err := s.store.SourceStatuses(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	statuses = s.filterStatusesForEnabledSources(statuses)
	counts, err := s.sourceTabCounts(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	if view != "" && view != domain.SourceAll {
		counts[view] = len(entries)
		counts[domain.SourceAll] = sumSourceCounts(counts, s.sources)
	}
	fetchHistory, err := s.store.RecentFetchEvents(ctx, 12)
	if err != nil {
		return Snapshot{}, err
	}
	fetchHistory = s.filterFetchHistoryForEnabledSources(fetchHistory)
	candidates, err := s.store.ListDedupeCandidates(ctx, 20)
	if err != nil {
		return Snapshot{}, err
	}
	if view == "" {
		view = domain.SourceAll
	}
	return Snapshot{
		Entries:          entries,
		Statuses:         statuses,
		FetchHistory:     fetchHistory,
		Rules:            rules,
		DedupeCandidates: candidates,
		Counts:           counts,
		View:             view,
		Filter:           filter,
		LoadedAt:         time.Now().UTC(),
	}, nil
}

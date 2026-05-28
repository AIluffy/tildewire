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
	store               FeedStore
	client              httpx.Requester
	adapters            []SourceAdapter
	sources             []domain.SourceID
	enabled             map[domain.SourceID]bool
	recommendationDirty bool
	recommendationGen   uint64
	runtimeMu           sync.RWMutex
	storeMu             sync.Mutex
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
		store:               store,
		client:              client,
		adapters:            ordered,
		sources:             countSources(ordered, enabled),
		enabled:             enabled,
		recommendationDirty: true,
		recommendationGen:   1,
	}
}

// SetSourceConfig applies source visibility and token settings without rebuilding the service.
func (s *Service) SetSourceConfig(enabled []domain.SourceID, tokens map[domain.SourceID]string) {
	nextEnabled := enabledSourceSet(enabled)
	nextSources := countSources(s.adapters, nextEnabled)
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	s.setAdapterTokens(tokens)
	s.enabled = nextEnabled
	s.sources = nextSources
	s.markRecommendationsDirtyLocked()
}

func (s *Service) setAdapterTokens(tokens map[domain.SourceID]string) {
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
	view, filter = s.normalizeLoadFeedRequest(view, filter)
	if err := s.ensureRecommendations(ctx, view); err != nil {
		return Snapshot{}, err
	}
	query := feedQueryForLoad(view, filter)
	entries, rules, err := s.loadFeedEntries(ctx, view, filter, query)
	if err != nil {
		return Snapshot{}, err
	}
	statuses, err := s.loadSourceStatuses(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	counts, err := s.loadSourceCounts(ctx, view, query)
	if err != nil {
		return Snapshot{}, err
	}
	fetchHistory, err := s.loadFetchHistory(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	candidates, err := s.store.ListDedupeCandidates(ctx, 20)
	if err != nil {
		return Snapshot{}, err
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

func (s *Service) normalizeLoadFeedRequest(view domain.SourceID, filter FeedFilter) (domain.SourceID, FeedFilter) {
	if view != "" && view != domain.SourceAll && view != domain.SourceRecommend && !s.sourceEnabled(view) {
		view = domain.SourceAll
		filter.SourceView = ""
	}
	filter = normalizeViewFilter(view, filter)
	if view == "" {
		view = domain.SourceAll
	}
	return view, filter
}

func feedQueryForLoad(view domain.SourceID, filter FeedFilter) domain.FeedQuery {
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
	if view == domain.SourceRecommend {
		query.Limit = recommendDisplayLimit
	}
	if view != "" && view != domain.SourceAll && view != domain.SourceRecommend {
		query.Source = view
	}
	return query
}

func (s *Service) loadFeedEntries(ctx context.Context, view domain.SourceID, filter FeedFilter, query domain.FeedQuery) ([]domain.FeedEntry, []domain.PersonalizationRule, error) {
	entries, err := s.listFeedForView(ctx, view, query)
	if err != nil {
		return nil, nil, err
	}
	rules, err := s.store.ListPersonalizationRules(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	activeRules := activePersonalizationRules(rules)
	if view != domain.SourceRecommend && !filter.IncludeHidden {
		entries = filterPersonalizedHidden(entries, activeRules)
	}
	entries = s.filterEntriesForEnabledSources(entries, view)
	profile, err := s.preferenceProfileForView(ctx, view)
	if err != nil {
		return nil, nil, err
	}
	if view != domain.SourceRecommend {
		applySort(entries, view, time.Now().UTC(), activeRules, profile)
	}
	return entries, rules, nil
}

func (s *Service) loadSourceStatuses(ctx context.Context) ([]domain.SourceHealth, error) {
	statuses, err := s.store.SourceStatuses(ctx)
	if err != nil {
		return nil, err
	}
	return s.filterStatusesForEnabledSources(statuses), nil
}

func (s *Service) loadSourceCounts(ctx context.Context, view domain.SourceID, query domain.FeedQuery) (map[domain.SourceID]int, error) {
	counts, err := s.sourceTabCounts(ctx)
	if err != nil {
		return nil, err
	}
	if view == domain.SourceRecommend {
		viewCount, err := s.store.CountRecommendedFeed(ctx, query)
		if err != nil {
			return nil, err
		}
		counts[domain.SourceRecommend] = capRecommendCount(viewCount)
	} else if view != "" && view != domain.SourceAll {
		viewCount, err := s.store.CountFeed(ctx, query)
		if err != nil {
			return nil, err
		}
		counts[view] = viewCount
	}
	return counts, nil
}

func (s *Service) loadFetchHistory(ctx context.Context) ([]domain.FetchEvent, error) {
	fetchHistory, err := s.store.RecentFetchEvents(ctx, 12)
	if err != nil {
		return nil, err
	}
	return s.filterFetchHistoryForEnabledSources(fetchHistory), nil
}

func (s *Service) listFeedForView(ctx context.Context, view domain.SourceID, query domain.FeedQuery) ([]domain.FeedEntry, error) {
	if view == domain.SourceRecommend {
		return s.store.ListRecommendedFeed(ctx, query)
	}
	return s.store.ListFeed(ctx, query)
}

func (s *Service) markRecommendationsDirty() {
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	s.markRecommendationsDirtyLocked()
}

func (s *Service) markRecommendationsDirtyLocked() {
	s.recommendationDirty = true
	s.recommendationGen++
}

func (s *Service) ensureRecommendations(ctx context.Context, view domain.SourceID) error {
	dirty, generation := s.recommendationState(view)
	if !dirty {
		return nil
	}
	if err := s.recomputeRecommendations(ctx); err != nil {
		return err
	}
	s.markRecommendationsClean(generation)
	return nil
}

func (s *Service) markRecommendationsClean(generation uint64) {
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	if s.recommendationGen == generation {
		s.recommendationDirty = false
	}
}

func (s *Service) recommendationState(view domain.SourceID) (bool, uint64) {
	s.runtimeMu.RLock()
	defer s.runtimeMu.RUnlock()
	return s.recommendationDirty || view == domain.SourceRecommend, s.recommendationGen
}

func (s *Service) recommendationsDirty(view domain.SourceID) bool {
	s.runtimeMu.RLock()
	defer s.runtimeMu.RUnlock()
	return s.recommendationDirty || view == domain.SourceRecommend
}

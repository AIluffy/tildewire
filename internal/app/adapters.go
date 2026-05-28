package app

import (
	"context"
	"time"

	"github.com/AIluffy/tildewire/internal/domain"
	"github.com/AIluffy/tildewire/internal/httpx"
)

// SourceAdapter fetches and normalizes one source for the application layer.
type SourceAdapter interface {
	Source() domain.SourceID
	DefaultScopes() []domain.FetchScope
	Fetch(context.Context, domain.FetchScope, httpx.Requester) (*domain.FetchResult, error)
	Normalize(context.Context, domain.FetchScope, *domain.FetchResult) ([]domain.FeedItem, error)
	CachePolicy(domain.FetchScope) domain.CachePolicy
}

// PrimaryScopesAdapter can expose several scopes that together represent the source's default feed.
type PrimaryScopesAdapter interface {
	PrimaryScopes() []domain.FetchScope
}

// OptionalAuthAdapter reports whether an optional source is unavailable until credentials are configured.
type OptionalAuthAdapter interface {
	AuthRequired() (bool, string)
}

// TokenAdapter accepts a runtime credential update for an optional source feature.
type TokenAdapter interface {
	SetToken(string)
}

// DetailAdapter optionally enriches an item detail view with source-native content.
type DetailAdapter interface {
	Detail(context.Context, domain.FeedEntry, httpx.Getter) (domain.ItemDetail, error)
}

// FeedReader loads visible feed windows and aggregate counts.
type FeedReader interface {
	ListFeed(context.Context, domain.FeedQuery) ([]domain.FeedEntry, error)
	CountFeed(context.Context, domain.FeedQuery) (int, error)
	ListRecommendedFeed(context.Context, domain.FeedQuery) ([]domain.FeedEntry, error)
	CountRecommendedFeed(context.Context, domain.FeedQuery) (int, error)
	SourceCounts(context.Context) (map[domain.SourceID]int, error)
	SourceViewCounts(context.Context) (map[domain.SourceID]map[string]int, error)
}

// FeedWriter persists refreshed source windows.
type FeedWriter interface {
	ReplaceFeedItemsForSourceView(context.Context, domain.SourceID, string, []domain.FeedItem) error
}

// ItemStateStore persists per-item user state and events.
type ItemStateStore interface {
	SetSaved(context.Context, string, bool) error
	SetRead(context.Context, string, bool) error
	SetHidden(context.Context, string, bool) error
	RecordItemEvent(context.Context, domain.ItemEvent) error
}

// SourceTelemetryStore records source health and fetch history.
type SourceTelemetryStore interface {
	UpdateSourceStatus(context.Context, domain.SourceID, domain.SourceStatus, string) error
	SourceStatuses(context.Context) ([]domain.SourceHealth, error)
	RecordFetchEvent(context.Context, domain.FetchEvent) error
	RecentFetchEvents(context.Context, int) ([]domain.FetchEvent, error)
}

// PersonalizationStore persists rules and preference profiles.
type PersonalizationStore interface {
	CreatePersonalizationRule(context.Context, domain.PersonalizationRule) (domain.PersonalizationRule, error)
	UpdatePersonalizationRule(context.Context, int64, domain.PersonalizationRule) (domain.PersonalizationRule, error)
	ListPersonalizationRules(context.Context, bool) ([]domain.PersonalizationRule, error)
	SetPersonalizationRuleEnabled(context.Context, int64, bool) error
	DeletePersonalizationRule(context.Context, int64) error
	PreferenceProfile(context.Context) (domain.PreferenceProfile, error)
}

// RecommendationStore persists and explains the virtual Recommend view.
type RecommendationStore interface {
	ReplaceRecommendationScores(context.Context, []domain.RecommendationScore) error
	RecommendationProfile(context.Context, time.Time) (domain.RecommendationProfile, error)
}

// DedupeStore persists duplicate candidates.
type DedupeStore interface {
	ListDedupeCandidates(context.Context, int) ([]domain.DedupeCandidate, error)
	IgnoreDedupeCandidate(context.Context, string) error
}

// CacheStore clears persisted refresh/cache data.
type CacheStore interface {
	ClearCache(context.Context) error
}

// ServiceStores groups the small persistence roles used by Service.
type ServiceStores struct {
	Feeds           FeedReader
	FeedWriter      FeedWriter
	Items           ItemStateStore
	SourceTelemetry SourceTelemetryStore
	Personalization PersonalizationStore
	Recommendations RecommendationStore
	Dedupe          DedupeStore
	Cache           CacheStore
}

// FeedStore is the complete persistence surface needed to build a Service.
type FeedStore interface {
	FeedReader
	FeedWriter
	ItemStateStore
	SourceTelemetryStore
	PersonalizationStore
	RecommendationStore
	DedupeStore
	CacheStore
}

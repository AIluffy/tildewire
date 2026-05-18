package app

import (
	"context"

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

// FeedStore is the persistence surface required by Service.
type FeedStore interface {
	ListFeed(context.Context, domain.FeedQuery) ([]domain.FeedEntry, error)
	UpsertFeedItems(context.Context, []domain.FeedItem) error
	SetSaved(context.Context, string, bool) error
	SetRead(context.Context, string, bool) error
	SetHidden(context.Context, string, bool) error
	UpdateSourceStatus(context.Context, domain.SourceID, domain.SourceStatus, string) error
	SourceStatuses(context.Context) ([]domain.SourceHealth, error)
	RecordFetchEvent(context.Context, domain.FetchEvent) error
	RecentFetchEvents(context.Context, int) ([]domain.FetchEvent, error)
	CreatePersonalizationRule(context.Context, domain.PersonalizationRule) (domain.PersonalizationRule, error)
	UpdatePersonalizationRule(context.Context, int64, domain.PersonalizationRule) (domain.PersonalizationRule, error)
	ListPersonalizationRules(context.Context, bool) ([]domain.PersonalizationRule, error)
	SetPersonalizationRuleEnabled(context.Context, int64, bool) error
	DeletePersonalizationRule(context.Context, int64) error
	PreferenceProfile(context.Context) (domain.PreferenceProfile, error)
	ListDedupeCandidates(context.Context, int) ([]domain.DedupeCandidate, error)
	IgnoreDedupeCandidate(context.Context, string) error
	ClearCache(context.Context) error
	SourceCounts(context.Context) (map[domain.SourceID]int, error)
	SourceViewCounts(context.Context) (map[domain.SourceID]map[string]int, error)
}

package tui

import (
	"context"

	"github.com/AIluffy/tildewire/internal/app"
	"github.com/AIluffy/tildewire/internal/domain"
)

type feedLoader interface {
	LoadFeed(context.Context, domain.SourceID, app.FeedFilter) (app.Snapshot, error)
	Refresh(context.Context, domain.SourceID, app.FeedFilter, app.RefreshOptions) (app.Snapshot, error)
	ClearCache(context.Context, domain.SourceID, app.FeedFilter) (app.Snapshot, error)
}

type itemMutator interface {
	SetSaved(context.Context, string, bool, domain.SourceID, app.FeedFilter) (app.Snapshot, error)
	SetRead(context.Context, string, bool, domain.SourceID, app.FeedFilter) (app.Snapshot, error)
	SetHidden(context.Context, string, bool, domain.SourceID, app.FeedFilter) (app.Snapshot, error)
	RecordItemEvent(context.Context, domain.ItemEvent) error
}

type detailLoader interface {
	LoadDetail(context.Context, domain.FeedEntry) (domain.ItemDetail, error)
}

type savedExporter interface {
	ExportSaved(context.Context, app.ExportOptions) (app.ExportResult, error)
}

type ruleService interface {
	CreatePersonalizationRule(context.Context, domain.PersonalizationRule, domain.SourceID, app.FeedFilter) (app.Snapshot, error)
	UpdatePersonalizationRule(context.Context, int64, domain.PersonalizationRule, domain.SourceID, app.FeedFilter) (app.Snapshot, error)
	SetPersonalizationRuleEnabled(context.Context, int64, bool, domain.SourceID, app.FeedFilter) (app.Snapshot, error)
	DeletePersonalizationRule(context.Context, int64, domain.SourceID, app.FeedFilter) (app.Snapshot, error)
}

type dedupeService interface {
	IgnoreDedupeCandidate(context.Context, string, domain.SourceID, app.FeedFilter) (app.Snapshot, error)
}

type runtimeConfigService interface {
	SetSourceConfig([]domain.SourceID, map[domain.SourceID]string)
}

// FeedService is the application surface used by the TUI.
type FeedService interface {
	feedLoader
	itemMutator
	detailLoader
	savedExporter
	ruleService
	dedupeService
	runtimeConfigService
}

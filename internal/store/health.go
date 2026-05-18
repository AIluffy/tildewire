package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/AIluffy/tildewire/internal/domain"
	"github.com/AIluffy/tildewire/internal/store/generated"
)

// UpdateSourceStatus persists current source health.
func (s *Store) UpdateSourceStatus(ctx context.Context, source domain.SourceID, status domain.SourceStatus, lastErr string) error {
	now := formatTime(time.Now().UTC())
	lastSuccess := sql.NullString{}
	if status == domain.SourceStatusOK {
		lastSuccess = sql.NullString{String: now, Valid: true}
	}
	return s.queries.UpsertSourceStatus(ctx, generated.UpsertSourceStatusParams{
		ID:            string(source),
		Name:          sourceName(source),
		LastFetchAt:   sql.NullString{String: now, Valid: true},
		LastSuccessAt: lastSuccess,
		LastError:     nullString(lastErr),
		Status:        string(status),
	})
}

// SourceStatuses returns all known source health rows.
func (s *Store) SourceStatuses(ctx context.Context) ([]domain.SourceHealth, error) {
	rows, err := s.queries.ListSourceStatuses(ctx)
	if err != nil {
		return nil, err
	}

	var statuses []domain.SourceHealth
	for _, row := range rows {
		health := domain.SourceHealth{
			Source:        domain.SourceID(row.ID),
			Name:          row.Name,
			Status:        domain.SourceStatus(row.Status),
			LastFetchAt:   parseNullTime(row.LastFetchAt),
			LastSuccessAt: parseNullTime(row.LastSuccessAt),
			LastError:     row.LastError,
		}
		statuses = append(statuses, health)
	}
	return statuses, nil
}

// RecordFetchEvent persists one refresh attempt and keeps recent history bounded.
func (s *Store) RecordFetchEvent(ctx context.Context, event domain.FetchEvent) error {
	event = normalizeFetchEvent(event)
	if err := s.queries.InsertFetchEvent(ctx, generated.InsertFetchEventParams{
		Source:      string(event.Source),
		SourceView:  event.SourceView,
		Status:      string(event.Status),
		StartedAt:   formatTime(event.StartedAt),
		FinishedAt:  formatTime(event.FinishedAt),
		DurationMs:  event.Duration.Milliseconds(),
		ItemCount:   int64(event.ItemCount),
		Stale:       boolInt(event.Stale),
		StaleReason: nullString(event.StaleReason),
		Error:       nullString(event.Error),
	}); err != nil {
		return err
	}
	return s.queries.PruneFetchHistory(ctx, maxFetchHistoryEntries)
}

// RecentFetchEvents returns newest-first refresh history.
func (s *Store) RecentFetchEvents(ctx context.Context, limit int) ([]domain.FetchEvent, error) {
	if limit <= 0 {
		limit = 12
	}
	rows, err := s.queries.ListRecentFetchEvents(ctx, int64(limit))
	if err != nil {
		return nil, err
	}
	events := make([]domain.FetchEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, domain.FetchEvent{
			Source:      domain.SourceID(row.Source),
			SourceView:  row.SourceView,
			Status:      domain.SourceStatus(row.Status),
			StartedAt:   parseTime(row.StartedAt),
			FinishedAt:  parseTime(row.FinishedAt),
			Duration:    time.Duration(row.DurationMs) * time.Millisecond,
			ItemCount:   int(row.ItemCount),
			Stale:       row.Stale == 1,
			StaleReason: row.StaleReason,
			Error:       row.Error,
		})
	}
	return events, nil
}

func normalizeFetchEvent(event domain.FetchEvent) domain.FetchEvent {
	if event.StartedAt.IsZero() {
		event.StartedAt = time.Now().UTC()
	}
	if event.FinishedAt.IsZero() {
		if event.Duration > 0 {
			event.FinishedAt = event.StartedAt.Add(event.Duration)
		} else {
			event.FinishedAt = event.StartedAt
		}
	}
	if event.Duration <= 0 && !event.StartedAt.IsZero() && !event.FinishedAt.IsZero() {
		event.Duration = event.FinishedAt.Sub(event.StartedAt)
	}
	if event.Duration < 0 {
		event.Duration = 0
	}
	if event.Status == "" {
		event.Status = domain.SourceStatusUnknown
	}
	return event
}

func sourceName(source domain.SourceID) string {
	switch source {
	case domain.SourceHackerNews:
		return "Hacker News"
	case domain.SourceGitHub:
		return "GitHub Trending"
	case domain.SourceAILabs:
		return "AI Labs"
	case domain.SourceHuggingFace:
		return "Hugging Face Papers"
	case domain.SourceLobsters:
		return "Lobsters"
	case domain.SourceProductHunt:
		return "Product Hunt"
	default:
		return string(source)
	}
}

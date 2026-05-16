package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/zhangxueai/tildewire/internal/store/generated"
)

// PutHTTPCache stores a raw response cache entry.
func (s *Store) PutHTTPCache(ctx context.Context, entry HTTPCacheEntry) error {
	return s.queries.UpsertHTTPCache(ctx, generated.UpsertHTTPCacheParams{
		RequestKey:   entry.RequestKey,
		Source:       string(entry.Source),
		Method:       entry.Method,
		Url:          entry.URL,
		StatusCode:   sql.NullInt64{Int64: int64(entry.StatusCode), Valid: entry.StatusCode != 0},
		HeadersJson:  nullString(entry.HeadersJSON),
		Body:         entry.Body,
		FetchedAt:    formatTime(entry.FetchedAt),
		ExpiresAt:    formatTime(entry.ExpiresAt),
		Etag:         nullString(entry.ETag),
		LastModified: nullString(entry.LastModified),
	})
}

// GetHTTPCache loads a raw response cache entry.
func (s *Store) GetHTTPCache(ctx context.Context, requestKey string) (HTTPCacheEntry, bool, error) {
	row, err := s.queries.GetHTTPCache(ctx, requestKey)
	if errors.Is(err, sql.ErrNoRows) {
		return HTTPCacheEntry{}, false, nil
	}
	if err != nil {
		return HTTPCacheEntry{}, false, err
	}
	entry := HTTPCacheEntry{
		RequestKey:   row.RequestKey,
		Source:       row.Source,
		Method:       row.Method,
		URL:          row.Url,
		StatusCode:   int(row.StatusCode),
		HeadersJSON:  row.HeadersJson,
		Body:         row.Body,
		FetchedAt:    parseTime(row.FetchedAt),
		ExpiresAt:    parseTime(row.ExpiresAt),
		ETag:         row.Etag,
		LastModified: row.LastModified,
	}
	return entry, true, nil
}

// ClearCache removes refreshable cache data while preserving explicitly saved items.
func (s *Store) ClearCache(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	queries := s.queries.WithTx(tx)
	if err := queries.ClearHTTPCache(ctx); err != nil {
		return err
	}
	if err := queries.ClearRateLimitState(ctx); err != nil {
		return err
	}
	if err := queries.ClearDedupeCandidates(ctx); err != nil {
		return err
	}
	if err := queries.ClearUnsavedItemSearch(ctx); err != nil {
		return err
	}
	if err := queries.ClearUnsavedItems(ctx); err != nil {
		return err
	}
	if err := queries.ResetSourceStatuses(ctx); err != nil {
		return err
	}
	return tx.Commit()
}

// RateLimitCooldown returns a persisted cooldown for a source bucket.
func (s *Store) RateLimitCooldown(ctx context.Context, source, bucket string) (time.Time, bool, error) {
	value, err := s.queries.GetRateLimitCooldown(ctx, generated.GetRateLimitCooldownParams{Source: source, Bucket: bucket})
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	parsed := parseTime(value)
	if parsed.IsZero() {
		return time.Time{}, false, nil
	}
	return parsed, true, nil
}

// SetRateLimitCooldown persists a cooldown for a source bucket.
func (s *Store) SetRateLimitCooldown(ctx context.Context, source, bucket string, until time.Time) error {
	return s.queries.UpsertRateLimitCooldown(ctx, generated.UpsertRateLimitCooldownParams{
		Source:        source,
		Bucket:        bucket,
		CooldownUntil: sql.NullString{String: formatTime(until.UTC()), Valid: true},
		UpdatedAt:     formatTime(time.Now().UTC()),
	})
}

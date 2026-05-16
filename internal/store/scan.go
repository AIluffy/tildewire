package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/zhangxueai/tildewire/internal/domain"
	"github.com/zhangxueai/tildewire/internal/store/generated"
)

func scanEntry(row generated.ListFeedRow) domain.FeedEntry {
	var entry domain.FeedEntry
	entry.Item.ID = row.ID
	entry.Item.CanonicalKey = row.CanonicalKey
	entry.Item.Title = row.Title
	entry.Item.Subtitle = row.Subtitle.String
	entry.Item.Summary = row.Summary.String
	entry.Item.URL = row.Url.String
	entry.Item.CanonicalURL = row.CanonicalUrl.String
	entry.Item.CommentsURL = row.CommentsUrl.String
	entry.Item.ItemType = row.ItemType
	entry.Item.Author = row.Author.String
	entry.Item.Organization = row.Organization.String
	entry.Item.Language = row.Language.String
	entry.Item.PublishedAt = parseNullTime(row.PublishedAt)
	entry.Item.FirstSeenAt = parseTime(row.FirstSeenAt)
	entry.Item.LastSeenAt = parseTime(row.LastSeenAt)
	entry.Item.Refs.Repo = row.Repo.String
	entry.Item.Refs.ArxivID = row.ArxivID.String
	if row.MetricsJson.Valid && row.MetricsJson.String != "" {
		_ = json.Unmarshal([]byte(row.MetricsJson.String), &entry.Item.Metrics)
	}
	if row.RefsJson.Valid && row.RefsJson.String != "" {
		_ = json.Unmarshal([]byte(row.RefsJson.String), &entry.Item.Refs)
	}
	if row.MetadataJson.Valid && row.MetadataJson.String != "" {
		entry.Item.Metadata = json.RawMessage(row.MetadataJson.String)
	}
	entry.Item.SimHash = row.Simhash
	entry.State = domain.ItemState{
		ItemID:   entry.Item.ID,
		Read:     row.Read == 1,
		Saved:    row.Saved == 1,
		Hidden:   row.Hidden == 1,
		ReadAt:   parseNullTime(row.ReadAt),
		SavedAt:  parseNullTime(row.SavedAt),
		HiddenAt: parseNullTime(row.HiddenAt),
		Note:     row.Note,
	}
	return entry
}

func (s *Store) listSourcesByItemIDs(ctx context.Context, itemIDs []string) (map[string][]domain.ItemSource, error) {
	sourcesByItem := make(map[string][]domain.ItemSource, len(itemIDs))
	if len(itemIDs) == 0 {
		return sourcesByItem, nil
	}
	placeholders, args := placeholdersFor(itemIDs)
	rows, err := s.db.QueryContext(ctx, `SELECT item_id, source, source_view, source_id, source_rank,
		COALESCE(source_url, '') AS source_url,
		COALESCE(metrics_json, '') AS metrics_json,
		COALESCE(raw_json, '') AS raw_json,
		seen_at
		FROM item_sources
		WHERE item_id IN (`+placeholders+`)
		ORDER BY item_id, source_rank ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var itemID string
		var source domain.ItemSource
		var sourceID string
		var sourceRank int64
		var metricsJSON string
		var rawJSON string
		var seenAt string
		if err := rows.Scan(
			&itemID,
			&sourceID,
			&source.SourceView,
			&source.SourceIDRaw,
			&sourceRank,
			&source.SourceURL,
			&metricsJSON,
			&rawJSON,
			&seenAt,
		); err != nil {
			return nil, err
		}
		source.ItemID = itemID
		source.Source = domain.SourceID(sourceID)
		source.SourceRank = int(sourceRank)
		source.SeenAt = parseTime(seenAt)
		if metricsJSON != "" {
			_ = json.Unmarshal([]byte(metricsJSON), &source.Metrics)
		}
		if rawJSON != "" {
			source.Raw = json.RawMessage(rawJSON)
		}
		sourcesByItem[itemID] = append(sourcesByItem[itemID], source)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return sourcesByItem, nil
}

func (s *Store) listTagsByItemIDs(ctx context.Context, itemIDs []string) (map[string][]string, error) {
	tagsByItem := make(map[string][]string, len(itemIDs))
	if len(itemIDs) == 0 {
		return tagsByItem, nil
	}
	placeholders, args := placeholdersFor(itemIDs)
	rows, err := s.db.QueryContext(ctx, `SELECT item_id, tag
		FROM item_tags
		WHERE item_id IN (`+placeholders+`)
		ORDER BY item_id, tag`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var itemID string
		var tag string
		if err := rows.Scan(&itemID, &tag); err != nil {
			return nil, err
		}
		tagsByItem[itemID] = append(tagsByItem[itemID], tag)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return tagsByItem, nil
}

func placeholdersFor(values []string) (string, []any) {
	args := make([]any, len(values))
	for idx, value := range values {
		args[idx] = value
	}
	return strings.TrimSuffix(strings.Repeat("?,", len(values)), ","), args
}

func sourceRank(entry domain.FeedEntry, source domain.SourceID) int {
	for _, itemSource := range entry.Sources {
		if itemSource.Source == source && itemSource.SourceRank > 0 {
			return itemSource.SourceRank
		}
	}
	return 1 << 30
}

func boolInt(value bool) int64 {
	if value {
		return 1
	}
	return 0
}

func stateTimestamp(enabled bool) sql.NullString {
	if !enabled {
		return sql.NullString{}
	}
	return sql.NullString{String: formatTime(time.Now().UTC()), Valid: true}
}

func nullString(value string) sql.NullString {
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}

func nullJSON(value json.RawMessage) sql.NullString {
	if len(value) == 0 {
		return sql.NullString{}
	}
	return sql.NullString{String: string(value), Valid: true}
}

func nullTime(value *time.Time) sql.NullString {
	if value == nil || value.IsZero() {
		return sql.NullString{}
	}
	return sql.NullString{String: formatTime(value.UTC()), Valid: true}
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseNullTime(value sql.NullString) *time.Time {
	if !value.Valid || value.String == "" {
		return nil
	}
	parsed := parseTime(value.String)
	if parsed.IsZero() {
		return nil
	}
	return &parsed
}

func parseTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

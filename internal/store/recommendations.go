package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/AIluffy/tildewire/internal/domain"
	"github.com/AIluffy/tildewire/internal/store/generated"
)

// ReplaceRecommendationScores replaces the durable virtual Recommend view.
func (s *Store) ReplaceRecommendationScores(ctx context.Context, scores []domain.RecommendationScore) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	queries := generated.New(tx)
	if err := queries.DeleteRecommendationScores(ctx); err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, score := range scores {
		if score.ItemID == "" {
			continue
		}
		computedAt := score.ComputedAt
		if computedAt.IsZero() {
			computedAt = now
		}
		if err := queries.InsertRecommendationScore(ctx, generated.InsertRecommendationScoreParams{
			ItemID:        score.ItemID,
			Score:         score.Score,
			InterestScore: score.InterestScore,
			HotScore:      score.HotScore,
			ComputedAt:    formatTime(computedAt),
		}); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListRecommendedFeed returns items ordered by persisted recommendation score.
func (s *Store) ListRecommendedFeed(ctx context.Context, query FeedQuery) ([]domain.FeedEntry, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = 200
	}
	normalized, ok := normalizeFeedQueryForStore(query)
	if !ok {
		return []domain.FeedEntry{}, nil
	}

	rows, err := s.queries.ListRecommendedFeed(ctx, generated.ListRecommendedFeedParams{
		SavedOnly:  normalized.savedOnly,
		UnreadOnly: normalized.unreadOnly,
		Language:   normalized.language,
		Tag:        normalized.tag,
		Search:     normalized.search,
		Limit:      int64(limit),
	})
	if err != nil {
		return nil, err
	}

	entries := make([]domain.FeedEntry, 0, len(rows))
	itemIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		entry := scanRecommendedEntry(row)
		entries = append(entries, entry)
		itemIDs = append(itemIDs, entry.Item.ID)
	}
	sourcesByItem, err := s.listSourcesByItemIDs(ctx, itemIDs)
	if err != nil {
		return nil, err
	}
	tagsByItem, err := s.listTagsByItemIDs(ctx, itemIDs)
	if err != nil {
		return nil, err
	}
	for idx := range entries {
		itemID := entries[idx].Item.ID
		entries[idx].Sources = sourcesByItem[itemID]
		entries[idx].Item.Sources = sourcesByItem[itemID]
		entries[idx].Item.Tags = tagsByItem[itemID]
	}
	return entries, nil
}

// CountRecommendedFeed counts the persisted virtual Recommend view after filters.
func (s *Store) CountRecommendedFeed(ctx context.Context, query FeedQuery) (int, error) {
	normalized, ok := normalizeFeedQueryForStore(query)
	if !ok {
		return 0, nil
	}
	total, err := s.queries.CountRecommendedFeed(ctx, generated.CountRecommendedFeedParams{
		SavedOnly:  normalized.savedOnly,
		UnreadOnly: normalized.unreadOnly,
		Language:   normalized.language,
		Tag:        normalized.tag,
		Search:     normalized.search,
	})
	if err != nil {
		return 0, err
	}
	return int(total), nil
}

func scanRecommendedEntry(row generated.ListRecommendedFeedRow) domain.FeedEntry {
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

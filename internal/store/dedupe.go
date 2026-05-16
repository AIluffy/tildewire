package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/zhangxueai/tildewire/internal/dedupe"
	"github.com/zhangxueai/tildewire/internal/domain"
	"github.com/zhangxueai/tildewire/internal/store/generated"
)

// ListDedupeCandidates returns non-ignored fuzzy duplicate suggestions.
func (s *Store) ListDedupeCandidates(ctx context.Context, limit int) ([]domain.DedupeCandidate, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.queries.ListDedupeCandidates(ctx, int64(limit))
	if err != nil {
		return nil, err
	}
	candidates := make([]domain.DedupeCandidate, 0, len(rows))
	for _, row := range rows {
		candidates = append(candidates, scanDedupeCandidate(row))
	}
	return candidates, nil
}

// IgnoreDedupeCandidate hides a fuzzy duplicate suggestion without merging items.
func (s *Store) IgnoreDedupeCandidate(ctx context.Context, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("dedupe candidate key is required")
	}
	return s.queries.IgnoreDedupeCandidate(ctx, generated.IgnoreDedupeCandidateParams{
		CandidateKey: key,
		DecidedAt:    formatTime(time.Now().UTC()),
	})
}

func refreshDedupeCandidates(ctx context.Context, queries *generated.Queries, now time.Time) error {
	rows, err := queries.ListDedupeItems(ctx, dedupeCandidateLimit)
	if err != nil {
		return err
	}
	type dedupeItem struct {
		id   string
		hash uint64
	}
	items := make([]dedupeItem, 0, len(rows))
	for _, row := range rows {
		hash, ok := dedupe.ParseSimHashHex(row.Simhash)
		if !ok {
			continue
		}
		items = append(items, dedupeItem{id: row.ID, hash: hash})
	}
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			distance := dedupe.Distance(items[i].hash, items[j].hash)
			if distance > dedupeCandidateThreshold {
				continue
			}
			itemA, itemB := items[i], items[j]
			if itemA.id > itemB.id {
				itemA, itemB = itemB, itemA
			}
			score := float64(64-distance) / 64
			if err := queries.UpsertDedupeCandidate(ctx, generated.UpsertDedupeCandidateParams{
				Key:       dedupe.DedupeCandidateKey(itemA.id, itemB.id),
				ItemIDA:   itemA.id,
				ItemIDB:   itemB.id,
				Score:     score,
				Distance:  int64(distance),
				Reason:    "simhash title/subtitle/summary",
				CreatedAt: formatTime(now),
				UpdatedAt: formatTime(now),
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func scanDedupeCandidate(row generated.ListDedupeCandidatesRow) domain.DedupeCandidate {
	return domain.DedupeCandidate{
		Key:       row.Key,
		Score:     row.Score,
		Distance:  int(row.Distance),
		Reason:    row.Reason,
		CreatedAt: parseTime(row.CreatedAt),
		UpdatedAt: parseTime(row.UpdatedAt),
		ItemA: domain.FeedItem{
			ID:           row.ItemAID,
			CanonicalKey: row.ItemACanonicalKey,
			Title:        row.ItemATitle,
			Subtitle:     row.ItemASubtitle,
			Summary:      row.ItemASummary,
			URL:          row.ItemAUrl,
			CanonicalURL: row.ItemACanonicalUrl,
			CommentsURL:  row.ItemACommentsUrl,
			ItemType:     row.ItemAItemType,
			Author:       row.ItemAAuthor,
			Organization: row.ItemAOrganization,
			Language:     row.ItemALanguage,
			FirstSeenAt:  parseTime(row.ItemAFirstSeenAt),
			LastSeenAt:   parseTime(row.ItemALastSeenAt),
			Refs:         domain.Refs{Repo: row.ItemARepo, ArxivID: row.ItemAArxivID},
			Metadata:     json.RawMessage(row.ItemAMetadataJson),
			SimHash:      row.ItemASimhash,
			Sources:      dedupeCandidateSources(row.ItemAID, row.ItemASource, row.ItemASourceView, row.ItemASourceID, int(row.ItemASourceRank), row.ItemASourceUrl, row.ItemASourceSeenAt),
		},
		ItemB: domain.FeedItem{
			ID:           row.ItemBID,
			CanonicalKey: row.ItemBCanonicalKey,
			Title:        row.ItemBTitle,
			Subtitle:     row.ItemBSubtitle,
			Summary:      row.ItemBSummary,
			URL:          row.ItemBUrl,
			CanonicalURL: row.ItemBCanonicalUrl,
			CommentsURL:  row.ItemBCommentsUrl,
			ItemType:     row.ItemBItemType,
			Author:       row.ItemBAuthor,
			Organization: row.ItemBOrganization,
			Language:     row.ItemBLanguage,
			FirstSeenAt:  parseTime(row.ItemBFirstSeenAt),
			LastSeenAt:   parseTime(row.ItemBLastSeenAt),
			Refs:         domain.Refs{Repo: row.ItemBRepo, ArxivID: row.ItemBArxivID},
			Metadata:     json.RawMessage(row.ItemBMetadataJson),
			SimHash:      row.ItemBSimhash,
			Sources:      dedupeCandidateSources(row.ItemBID, row.ItemBSource, row.ItemBSourceView, row.ItemBSourceID, int(row.ItemBSourceRank), row.ItemBSourceUrl, row.ItemBSourceSeenAt),
		},
	}
}

func dedupeCandidateSources(itemID, source, sourceView, sourceID string, rank int, sourceURL, seenAt string) []domain.ItemSource {
	if source == "" {
		return nil
	}
	return []domain.ItemSource{{
		ItemID:      itemID,
		Source:      domain.SourceID(source),
		SourceView:  sourceView,
		SourceIDRaw: sourceID,
		SourceRank:  rank,
		SourceURL:   sourceURL,
		SeenAt:      parseTime(seenAt),
	}}
}

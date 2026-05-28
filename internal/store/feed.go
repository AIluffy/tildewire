package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/AIluffy/tildewire/internal/dedupe"
	"github.com/AIluffy/tildewire/internal/domain"
	"github.com/AIluffy/tildewire/internal/recommend"
	"github.com/AIluffy/tildewire/internal/store/generated"
)

type normalizedFeedQuery struct {
	includeHidden int
	source        string
	sourceView    string
	savedOnly     int
	unreadOnly    int
	language      string
	tag           string
	search        string
}

// UpsertFeedItems writes normalized items, source context, tags, and default state.
func (s *Store) UpsertFeedItems(ctx context.Context, items []domain.FeedItem) error {
	if len(items) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	queries := generated.New(tx)
	if err := upsertFeedItems(ctx, queries, items, now); err != nil {
		return err
	}
	return tx.Commit()
}

// ReplaceFeedItemsForSourceView replaces one source-view window with freshly fetched items.
func (s *Store) ReplaceFeedItemsForSourceView(ctx context.Context, source domain.SourceID, sourceView string, items []domain.FeedItem) error {
	sourceView = strings.ToLower(strings.TrimSpace(sourceView))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	queries := generated.New(tx)
	if err := queries.DeleteItemSourcesForSourceView(ctx, generated.DeleteItemSourcesForSourceViewParams{
		Source:     string(source),
		SourceView: sourceView,
	}); err != nil {
		return err
	}
	now := time.Now().UTC()
	if err := upsertFeedItems(ctx, queries, items, now); err != nil {
		return err
	}
	if err := queries.DeleteUnreferencedUnsavedItemSearch(ctx); err != nil {
		return err
	}
	if err := queries.DeleteUnreferencedUnsavedItemTags(ctx); err != nil {
		return err
	}
	if err := queries.DeleteUnreferencedUnsavedDedupeCandidates(ctx); err != nil {
		return err
	}
	if err := queries.DeleteUnreferencedUnsavedItemStates(ctx); err != nil {
		return err
	}
	if err := queries.DeleteUnreferencedUnsavedItems(ctx); err != nil {
		return err
	}
	return tx.Commit()
}

// ListFeed returns persisted items with source context and user state.
func (s *Store) ListFeed(ctx context.Context, query domain.FeedQuery) ([]domain.FeedEntry, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = 200
	}
	normalized, ok := normalizeFeedQueryForStore(query)
	if !ok {
		return []domain.FeedEntry{}, nil
	}

	rows, err := s.queries.ListFeed(ctx, generated.ListFeedParams{
		IncludeHidden: normalized.includeHidden,
		Source:        normalized.source,
		SourceView:    normalized.sourceView,
		SavedOnly:     normalized.savedOnly,
		UnreadOnly:    normalized.unreadOnly,
		Language:      normalized.language,
		Tag:           normalized.tag,
		Search:        normalized.search,
		Limit:         int64(limit),
	})
	if err != nil {
		return nil, err
	}

	entries := make([]domain.FeedEntry, 0, len(rows))
	itemIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		entry := scanEntry(row)
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
		sources := filterSourcesForQuery(sourcesByItem[itemID], query.Source, normalized.sourceView)
		entries[idx].Sources = sources
		entries[idx].Item.Tags = tagsByItem[itemID]
	}
	if query.Source != "" {
		sort.SliceStable(entries, func(i, j int) bool {
			return sourceRank(entries[i], query.Source) < sourceRank(entries[j], query.Source)
		})
	}
	return entries, nil
}

func normalizeFeedQueryForStore(query domain.FeedQuery) (normalizedFeedQuery, bool) {
	normalized := normalizedFeedQuery{
		source:     string(query.Source),
		sourceView: strings.ToLower(strings.TrimSpace(query.SourceView)),
		language:   strings.ToLower(strings.TrimSpace(query.Language)),
		tag:        strings.ToLower(strings.TrimSpace(query.Tag)),
	}
	if query.IncludeHidden {
		normalized.includeHidden = 1
	}
	if query.SavedOnly {
		normalized.savedOnly = 1
	}
	if query.UnreadOnly {
		normalized.unreadOnly = 1
	}
	rawSearch := strings.TrimSpace(query.Search)
	normalized.search = ftsMatchQuery(rawSearch)
	if rawSearch != "" && normalized.search == "" {
		return normalizedFeedQuery{}, false
	}
	return normalized, true
}

func upsertFeedItems(ctx context.Context, queries *generated.Queries, items []domain.FeedItem, now time.Time) error {
	for _, item := range items {
		item = dedupe.CanonicalizeItem(item)
		if err := upsertItem(ctx, queries, item, now); err != nil {
			return err
		}
	}
	return refreshDedupeCandidates(ctx, queries, now)
}

func filterSourcesForQuery(sources []domain.ItemSource, source domain.SourceID, sourceView string) []domain.ItemSource {
	if source == "" {
		return sources
	}
	filtered := make([]domain.ItemSource, 0, len(sources))
	for _, itemSource := range sources {
		if itemSource.Source != source {
			continue
		}
		if sourceView != "" && strings.ToLower(itemSource.SourceView) != sourceView {
			continue
		}
		filtered = append(filtered, itemSource)
	}
	return filtered
}

func upsertItem(ctx context.Context, queries *generated.Queries, item domain.FeedItem, now time.Time) error {
	if item.ID == "" {
		return errors.New("item id is required")
	}
	if item.CanonicalKey == "" {
		return errors.New("canonical key is required")
	}
	if strings.TrimSpace(item.Title) == "" {
		return errors.New("item title is required")
	}
	if item.FirstSeenAt.IsZero() {
		item.FirstSeenAt = now
	}
	if item.LastSeenAt.IsZero() {
		item.LastSeenAt = now
	}
	item.SimHash = dedupe.SimHashHex(dedupe.SimHash(itemDedupeText(item)))
	metricsJSON, err := json.Marshal(item.Metrics)
	if err != nil {
		return err
	}
	refsJSON, err := json.Marshal(item.Refs)
	if err != nil {
		return err
	}
	if err := queries.UpsertItem(ctx, generated.UpsertItemParams{
		ID:           item.ID,
		CanonicalKey: item.CanonicalKey,
		Title:        item.Title,
		Subtitle:     nullString(item.Subtitle),
		Summary:      nullString(item.Summary),
		Url:          nullString(item.URL),
		CanonicalUrl: nullString(item.CanonicalURL),
		CommentsUrl:  nullString(item.CommentsURL),
		ItemType:     item.ItemType,
		Author:       nullString(item.Author),
		Organization: nullString(item.Organization),
		Language:     nullString(item.Language),
		PublishedAt:  nullTime(item.PublishedAt),
		FirstSeenAt:  formatTime(item.FirstSeenAt),
		LastSeenAt:   formatTime(item.LastSeenAt),
		Repo:         nullString(item.Refs.Repo),
		ArxivID:      nullString(item.Refs.ArxivID),
		MetricsJson:  sql.NullString{String: string(metricsJSON), Valid: true},
		RefsJson:     sql.NullString{String: string(refsJSON), Valid: true},
		MetadataJson: nullJSON(item.Metadata),
		Simhash:      nullString(item.SimHash),
	}); err != nil {
		return err
	}
	if err := queries.EnsureItemState(ctx, item.ID); err != nil {
		return err
	}
	if err := queries.DeleteItemTags(ctx, item.ID); err != nil {
		return err
	}
	for _, tag := range item.Tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if err := queries.InsertItemTag(ctx, generated.InsertItemTagParams{ItemID: item.ID, Tag: tag}); err != nil {
			return err
		}
	}
	if err := queries.DeleteItemTerms(ctx, item.ID); err != nil {
		return err
	}
	for _, term := range recommend.TermsForItem(item) {
		if err := queries.InsertItemTerm(ctx, generated.InsertItemTermParams{
			ItemID: item.ID,
			Kind:   term.Kind,
			Value:  term.Value,
			Weight: term.Weight,
		}); err != nil {
			return err
		}
	}
	if err := queries.DeleteItemSearch(ctx, item.ID); err != nil {
		return err
	}
	if err := queries.InsertItemSearch(ctx, generated.InsertItemSearchParams{
		ItemID:  item.ID,
		Content: itemSearchContent(item),
	}); err != nil {
		return err
	}
	for _, source := range item.Sources {
		if source.ItemID == "" {
			source.ItemID = item.ID
		}
		if source.SeenAt.IsZero() {
			source.SeenAt = item.LastSeenAt
		}
		metricsJSON, err := json.Marshal(source.Metrics)
		if err != nil {
			return err
		}
		if err := queries.UpsertItemSource(ctx, generated.UpsertItemSourceParams{
			ItemID:      source.ItemID,
			Source:      string(source.Source),
			SourceView:  source.SourceView,
			SourceID:    source.SourceIDRaw,
			SourceRank:  int64(source.SourceRank),
			SourceUrl:   nullString(source.SourceURL),
			MetricsJson: sql.NullString{String: string(metricsJSON), Valid: true},
			RawJson:     nullJSON(source.Raw),
			SeenAt:      formatTime(source.SeenAt),
		}); err != nil {
			return err
		}
	}
	return nil
}

func itemSearchContent(item domain.FeedItem) string {
	parts := []string{
		item.Title,
		item.Subtitle,
		item.Summary,
		item.Author,
		item.Organization,
		item.Language,
		item.Refs.Repo,
		item.Refs.ArxivID,
		item.Refs.PaperID,
	}
	parts = append(parts, searchableMetadataText(item.Metadata)...)
	parts = append(parts, item.Tags...)
	return strings.Join(parts, " ")
}

func searchableMetadataText(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	var parts []string
	collectSearchableMetadata(partsAppender{parts: &parts}, "", false, value)
	return parts
}

type partsAppender struct {
	parts *[]string
}

func (a partsAppender) append(value string) {
	value = strings.TrimSpace(value)
	if value != "" {
		*a.parts = append(*a.parts, value)
	}
}

func collectSearchableMetadata(appender partsAppender, key string, searchable bool, value any) {
	searchable = searchable || searchableMetadataKey(key)
	switch value := value.(type) {
	case string:
		if searchable {
			appender.append(value)
		}
	case []any:
		for _, item := range value {
			collectSearchableMetadata(appender, "", searchable, item)
		}
	case map[string]any:
		for childKey, childValue := range value {
			collectSearchableMetadata(appender, childKey, searchable, childValue)
		}
	}
}

func searchableMetadataKey(key string) bool {
	switch normalizedMetadataKey(key) {
	case "title", "name", "summary", "description", "tagline", "repo", "language", "category", "author", "organization", "keywords", "tags", "topics", "labels":
		return true
	default:
		return false
	}
}

func normalizedMetadataKey(key string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(key) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func itemDedupeText(item domain.FeedItem) string {
	return strings.Join([]string{item.Title, item.Subtitle, item.Summary}, " ")
}

func ftsMatchQuery(search string) string {
	search = strings.TrimSpace(search)
	if search == "" {
		return ""
	}
	var tokens []string
	var current strings.Builder
	flush := func() {
		if current.Len() == 0 {
			return
		}
		tokens = append(tokens, strings.ToLower(current.String())+"*")
		current.Reset()
	}
	for _, r := range search {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return strings.Join(tokens, " ")
}

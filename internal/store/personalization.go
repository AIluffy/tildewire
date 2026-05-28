package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/AIluffy/tildewire/internal/domain"
	"github.com/AIluffy/tildewire/internal/recommend"
	"github.com/AIluffy/tildewire/internal/store/generated"
)

const recommendationTermBackfillBatchSize = 400

// CreatePersonalizationRule persists a new enabled personalization rule.
func (s *Store) CreatePersonalizationRule(ctx context.Context, rule domain.PersonalizationRule) (domain.PersonalizationRule, error) {
	rule, err := normalizePersonalizationRule(rule)
	if err != nil {
		return domain.PersonalizationRule{}, err
	}
	now := time.Now().UTC()
	row, err := s.queries.CreatePersonalizationRule(ctx, generated.CreatePersonalizationRuleParams{
		Effect:    string(rule.Effect),
		Target:    string(rule.Target),
		Value:     rule.Value,
		Enabled:   boolInt(rule.Enabled),
		CreatedAt: formatTime(now),
		UpdatedAt: formatTime(now),
	})
	if err != nil {
		return domain.PersonalizationRule{}, err
	}
	return scanPersonalizationRule(row), nil
}

// UpdatePersonalizationRule replaces a durable personalization rule.
func (s *Store) UpdatePersonalizationRule(ctx context.Context, id int64, rule domain.PersonalizationRule) (domain.PersonalizationRule, error) {
	rule, err := normalizePersonalizationRule(rule)
	if err != nil {
		return domain.PersonalizationRule{}, err
	}
	row, err := s.queries.UpdatePersonalizationRule(ctx, generated.UpdatePersonalizationRuleParams{
		Effect:    string(rule.Effect),
		Target:    string(rule.Target),
		Value:     rule.Value,
		Enabled:   boolInt(rule.Enabled),
		UpdatedAt: formatTime(time.Now().UTC()),
		ID:        id,
	})
	if err != nil {
		return domain.PersonalizationRule{}, err
	}
	return scanPersonalizationRule(row), nil
}

// ListPersonalizationRules returns rules ordered for display and matching.
func (s *Store) ListPersonalizationRules(ctx context.Context, includeDisabled bool) ([]domain.PersonalizationRule, error) {
	rows, err := s.queries.ListPersonalizationRules(ctx, boolInt(includeDisabled))
	if err != nil {
		return nil, err
	}
	rules := make([]domain.PersonalizationRule, 0, len(rows))
	for _, row := range rows {
		rules = append(rules, scanPersonalizationRule(row))
	}
	return rules, nil
}

// SetPersonalizationRuleEnabled toggles one rule.
func (s *Store) SetPersonalizationRuleEnabled(ctx context.Context, id int64, enabled bool) error {
	return s.queries.SetPersonalizationRuleEnabled(ctx, generated.SetPersonalizationRuleEnabledParams{
		Enabled:   boolInt(enabled),
		UpdatedAt: formatTime(time.Now().UTC()),
		ID:        id,
	})
}

// DeletePersonalizationRule removes one rule.
func (s *Store) DeletePersonalizationRule(ctx context.Context, id int64) error {
	return s.queries.DeletePersonalizationRule(ctx, id)
}

// PreferenceProfile returns implicit saved and hidden preference signals.
func (s *Store) PreferenceProfile(ctx context.Context) (domain.PreferenceProfile, error) {
	rows, err := s.queries.ListPreferenceSignals(ctx)
	if err != nil {
		return domain.PreferenceProfile{}, err
	}
	profile := newPreferenceProfile()
	for _, row := range rows {
		addPreferenceSignal(&profile, row.State, row.Target, row.Value, int(row.Total))
	}
	return profile, nil
}

// RecommendationProfile returns decayed item-term signals for the virtual Recommend view.
func (s *Store) RecommendationProfile(ctx context.Context, now time.Time) (domain.RecommendationProfile, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	since := now.Add(-recommend.ProfileWindow)
	if err := s.backfillMissingRecommendationTerms(ctx, since); err != nil {
		return domain.RecommendationProfile{}, err
	}
	rows, err := s.queries.ListRecommendationProfileSignals(ctx, sql.NullString{String: formatTime(since), Valid: true})
	if err != nil {
		return domain.RecommendationProfile{}, err
	}
	signals := make([]recommend.Signal, 0, len(rows))
	for _, row := range rows {
		if !row.OccurredAt.Valid || row.OccurredAt.String == "" {
			continue
		}
		signals = append(signals, recommend.Signal{
			Term: domain.RecommendationTerm{
				ItemID: row.ItemID,
				Kind:   row.Kind,
				Value:  row.Value,
				Weight: row.Weight,
			},
			EventType:  domain.ItemEventType(row.EventType),
			OccurredAt: parseTime(row.OccurredAt.String),
		})
	}
	return recommend.BuildProfile(signals, now), nil
}

func (s *Store) backfillMissingRecommendationTerms(ctx context.Context, since time.Time) error {
	itemIDs, err := s.recommendationSignalItemIDsMissingTerms(ctx, since)
	if err != nil || len(itemIDs) == 0 {
		return err
	}
	entries, err := s.listEntriesByItemIDs(ctx, itemIDs)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	queries := generated.New(tx)
	for _, entry := range entries {
		item := entry.ItemWithSources()
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
	}
	return tx.Commit()
}

func (s *Store) recommendationSignalItemIDsMissingTerms(ctx context.Context, since time.Time) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT DISTINCT signal.item_id
FROM (
  SELECT st.item_id, st.saved_at AS occurred_at
  FROM item_state st
  WHERE st.saved = 1 AND st.saved_at IS NOT NULL AND st.saved_at >= ?
  UNION ALL
  SELECT st.item_id, st.hidden_at AS occurred_at
  FROM item_state st
  WHERE st.hidden = 1 AND st.hidden_at IS NOT NULL AND st.hidden_at >= ?
  UNION ALL
  SELECT ev.item_id, ev.occurred_at
  FROM item_events ev
  WHERE ev.occurred_at >= ?
) signal
WHERE signal.occurred_at IS NOT NULL
  AND NOT EXISTS (
    SELECT 1 FROM item_terms term WHERE term.item_id = signal.item_id
  )`, formatTime(since), formatTime(since), formatTime(since))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var itemIDs []string
	for rows.Next() {
		var itemID string
		if err := rows.Scan(&itemID); err != nil {
			return nil, err
		}
		itemIDs = append(itemIDs, itemID)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return itemIDs, nil
}

func (s *Store) listEntriesByItemIDs(ctx context.Context, itemIDs []string) ([]domain.FeedEntry, error) {
	if len(itemIDs) == 0 {
		return nil, nil
	}
	entries := make([]domain.FeedEntry, 0, len(itemIDs))
	for start := 0; start < len(itemIDs); start += recommendationTermBackfillBatchSize {
		end := min(start+recommendationTermBackfillBatchSize, len(itemIDs))
		batch, err := s.listEntriesByItemIDBatch(ctx, itemIDs[start:end])
		if err != nil {
			return nil, err
		}
		entries = append(entries, batch...)
	}
	return entries, nil
}

func (s *Store) listEntriesByItemIDBatch(ctx context.Context, itemIDs []string) ([]domain.FeedEntry, error) {
	placeholders, args := placeholdersFor(itemIDs)
	rows, err := s.db.QueryContext(ctx, `SELECT i.id, i.canonical_key, i.title, i.subtitle, i.summary, i.url, i.canonical_url, i.comments_url,
       i.item_type, i.author, i.organization, i.language, i.published_at, i.first_seen_at, i.last_seen_at,
       i.repo, i.arxiv_id, i.metrics_json, i.refs_json, i.metadata_json, COALESCE(i.simhash, '') AS simhash,
       COALESCE(st.read, 0) AS read, COALESCE(st.saved, 0) AS saved, COALESCE(st.hidden, 0) AS hidden,
       st.read_at, st.saved_at, st.hidden_at, COALESCE(st.note, '') AS note
FROM items i
LEFT JOIN item_state st ON st.item_id = i.id
WHERE i.id IN (`+placeholders+`)
ORDER BY i.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make([]domain.FeedEntry, 0, len(itemIDs))
	for rows.Next() {
		var row generated.ListFeedRow
		if err := rows.Scan(
			&row.ID,
			&row.CanonicalKey,
			&row.Title,
			&row.Subtitle,
			&row.Summary,
			&row.Url,
			&row.CanonicalUrl,
			&row.CommentsUrl,
			&row.ItemType,
			&row.Author,
			&row.Organization,
			&row.Language,
			&row.PublishedAt,
			&row.FirstSeenAt,
			&row.LastSeenAt,
			&row.Repo,
			&row.ArxivID,
			&row.MetricsJson,
			&row.RefsJson,
			&row.MetadataJson,
			&row.Simhash,
			&row.Read,
			&row.Saved,
			&row.Hidden,
			&row.ReadAt,
			&row.SavedAt,
			&row.HiddenAt,
			&row.Note,
		); err != nil {
			return nil, err
		}
		entries = append(entries, scanEntry(row))
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.Item.ID)
	}
	sourcesByItem, err := s.listSourcesByItemIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	tagsByItem, err := s.listTagsByItemIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	for idx := range entries {
		itemID := entries[idx].Item.ID
		entries[idx].Sources = sourcesByItem[itemID]
		entries[idx].Item.Tags = tagsByItem[itemID]
	}
	return entries, nil
}

func normalizePersonalizationRule(rule domain.PersonalizationRule) (domain.PersonalizationRule, error) {
	rule.Effect = domain.RuleEffect(strings.ToLower(strings.TrimSpace(string(rule.Effect))))
	rule.Target = domain.RuleTarget(strings.ToLower(strings.TrimSpace(string(rule.Target))))
	rule.Value = strings.ToLower(strings.TrimSpace(rule.Value))
	if rule.Value == "" {
		return domain.PersonalizationRule{}, errors.New("personalization rule value is required")
	}
	switch rule.Effect {
	case domain.RuleEffectBoost, domain.RuleEffectMute, domain.RuleEffectHide:
	default:
		return domain.PersonalizationRule{}, errors.New("invalid personalization rule effect")
	}
	switch rule.Target {
	case domain.RuleTargetKeyword, domain.RuleTargetLanguage, domain.RuleTargetTag, domain.RuleTargetDomain, domain.RuleTargetSource, domain.RuleTargetRepo, domain.RuleTargetAuthor:
	default:
		return domain.PersonalizationRule{}, errors.New("invalid personalization rule target")
	}
	return rule, nil
}

func scanPersonalizationRule(row generated.PersonalizationRule) domain.PersonalizationRule {
	return domain.PersonalizationRule{
		ID:        row.ID,
		Effect:    domain.RuleEffect(row.Effect),
		Target:    domain.RuleTarget(row.Target),
		Value:     row.Value,
		Enabled:   row.Enabled == 1,
		CreatedAt: parseTime(row.CreatedAt),
		UpdatedAt: parseTime(row.UpdatedAt),
	}
}

func newPreferenceProfile() domain.PreferenceProfile {
	return domain.PreferenceProfile{
		SavedTags:       make(map[string]int),
		SavedLanguages:  make(map[string]int),
		SavedRepos:      make(map[string]int),
		SavedAuthors:    make(map[string]int),
		SavedSources:    make(map[string]int),
		HiddenTags:      make(map[string]int),
		HiddenLanguages: make(map[string]int),
		HiddenRepos:     make(map[string]int),
		HiddenAuthors:   make(map[string]int),
		HiddenSources:   make(map[string]int),
	}
}

func addPreferenceSignal(profile *domain.PreferenceProfile, state, target, value string, total int) {
	if total <= 0 {
		return
	}
	switch state + ":" + target {
	case "saved:tag":
		profile.SavedTags[value] = total
	case "saved:language":
		profile.SavedLanguages[value] = total
	case "saved:repo":
		profile.SavedRepos[value] = total
	case "saved:author":
		profile.SavedAuthors[value] = total
	case "saved:source":
		profile.SavedSources[value] = total
	case "hidden:tag":
		profile.HiddenTags[value] = total
	case "hidden:language":
		profile.HiddenLanguages[value] = total
	case "hidden:repo":
		profile.HiddenRepos[value] = total
	case "hidden:author":
		profile.HiddenAuthors[value] = total
	case "hidden:source":
		profile.HiddenSources[value] = total
	}
}

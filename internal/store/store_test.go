package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AIluffy/tildewire/internal/dedupe"
	"github.com/AIluffy/tildewire/internal/domain"
	"github.com/AIluffy/tildewire/internal/httpcache"
	"github.com/AIluffy/tildewire/internal/recommend"
)

func TestStoreMigratesUpsertsStateAndHiddenFiltering(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	item := testItem("id-1", "hackernews:1", "First", 1)
	if err := store.UpsertFeedItems(ctx, []domain.FeedItem{item}); err != nil {
		t.Fatal(err)
	}
	entries, err := store.ListFeed(ctx, FeedQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if err := store.SetSaved(ctx, item.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := store.SetRead(ctx, item.ID, true); err != nil {
		t.Fatal(err)
	}
	entries, err = store.ListFeed(ctx, FeedQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if !entries[0].State.Saved || !entries[0].State.Read {
		t.Fatalf("state was not persisted: %+v", entries[0].State)
	}
	if entries[0].State.SavedAt == nil || entries[0].State.ReadAt == nil {
		t.Fatalf("state timestamps were not persisted: %+v", entries[0].State)
	}
	if err := store.SetHidden(ctx, item.ID, true); err != nil {
		t.Fatal(err)
	}
	entries, err = store.ListFeed(ctx, FeedQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("hidden item should be filtered, got %d", len(entries))
	}
	entries, err = store.ListFeed(ctx, FeedQuery{IncludeHidden: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !entries[0].State.Hidden {
		t.Fatalf("hidden item should be visible with IncludeHidden: %+v", entries)
	}
}

func TestStoreSourceNativeOrdering(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	items := []domain.FeedItem{
		testItem("id-2", "hackernews:2", "Second", 2),
		testItem("id-1", "hackernews:1", "First", 1),
	}
	if err := store.UpsertFeedItems(ctx, items); err != nil {
		t.Fatal(err)
	}
	entries, err := store.ListFeed(ctx, FeedQuery{Source: domain.SourceHackerNews})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Item.Title != "First" {
		t.Fatalf("entries not source-rank sorted: %+v", entries)
	}
}

func TestStoreSourceCounts(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	weekly := testSourceItem("repo-weekly", "repo:owner/weekly", "owner/weekly", domain.SourceGitHub, 1)
	weekly.Sources[0].SourceView = "trending:weekly"
	items := []domain.FeedItem{
		testItem("id-1", "hackernews:1", "HN", 1),
		testSourceItem("repo-1", "repo:owner/repo", "owner/repo", domain.SourceGitHub, 1),
		weekly,
	}
	if err := store.UpsertFeedItems(ctx, items); err != nil {
		t.Fatal(err)
	}
	counts, err := store.SourceCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if counts[domain.SourceAll] != 3 || counts[domain.SourceHackerNews] != 1 || counts[domain.SourceGitHub] != 2 {
		t.Fatalf("unexpected counts: %+v", counts)
	}
	dailyCount, err := store.SourceViewCount(ctx, domain.SourceGitHub, "trending:daily")
	if err != nil {
		t.Fatal(err)
	}
	if dailyCount != 1 {
		t.Fatalf("daily GitHub count = %d, want 1", dailyCount)
	}
	viewCounts, err := store.SourceViewCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if viewCounts[domain.SourceGitHub]["trending:daily"] != 1 || viewCounts[domain.SourceGitHub]["trending:weekly"] != 1 || viewCounts[domain.SourceHackerNews]["top"] != 1 {
		t.Fatalf("unexpected source view counts: %+v", viewCounts)
	}
	if err := store.SetHidden(ctx, items[0].ID, true); err != nil {
		t.Fatal(err)
	}
	counts, err = store.SourceCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if counts[domain.SourceAll] != 2 || counts[domain.SourceHackerNews] != 0 || counts[domain.SourceGitHub] != 2 {
		t.Fatalf("hidden counts mismatch: %+v", counts)
	}
	viewCounts, err = store.SourceViewCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if viewCounts[domain.SourceHackerNews]["top"] != 0 {
		t.Fatalf("hidden source view count mismatch: %+v", viewCounts)
	}
}

func TestStoreUpsertCanonicalizesItemsBeforePersisting(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	item := domain.FeedItem{
		ID:           "hn-42",
		CanonicalKey: "hackernews:42",
		Title:        "GitHub repo on HN",
		URL:          "https://github.com/CharmBracelet/BubbleTea?utm_source=hn",
		CanonicalURL: "https://github.com/CharmBracelet/BubbleTea?utm_source=hn",
		ItemType:     "story",
		FirstSeenAt:  now,
		LastSeenAt:   now,
		Refs:         domain.Refs{HNID: "42"},
		Sources: []domain.ItemSource{{
			ItemID:      "hn-42",
			Source:      domain.SourceHackerNews,
			SourceView:  "top",
			SourceIDRaw: "42",
			SourceRank:  1,
			SourceURL:   "https://news.ycombinator.com/item?id=42",
			SeenAt:      now,
		}},
	}

	if err := store.UpsertFeedItems(ctx, []domain.FeedItem{item}); err != nil {
		t.Fatal(err)
	}
	entries, err := store.ListFeed(ctx, FeedQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	got := entries[0]
	if got.Item.CanonicalKey != "repo:charmbracelet/bubbletea" {
		t.Fatalf("canonical key = %q", got.Item.CanonicalKey)
	}
	if got.Item.ID == "hn-42" || got.Sources[0].ItemID != got.Item.ID {
		t.Fatalf("item/source ids were not canonicalized together: item=%+v sources=%+v", got.Item, got.Sources)
	}
	if got.Item.Refs.Repo != "charmbracelet/bubbletea" {
		t.Fatalf("repo ref = %q", got.Item.Refs.Repo)
	}
}

func TestStoreListFeedAssemblesSourcesAndTagsForMultipleItems(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	first := testSourceItem("repo-1", "repo:owner/repo", "owner/repo", domain.SourceGitHub, 2)
	first.Tags = []string{"github", "go"}
	first.Sources = append(first.Sources, domain.ItemSource{
		ItemID:      first.ID,
		Source:      domain.SourceHackerNews,
		SourceView:  "top",
		SourceIDRaw: "42",
		SourceRank:  1,
		SourceURL:   "https://news.ycombinator.com/item?id=42",
		SeenAt:      first.LastSeenAt,
	})
	second := testItem("hn-7", "hackernews:7", "HN only", 1)
	second.Tags = []string{"hn", "sqlite"}

	if err := store.UpsertFeedItems(ctx, []domain.FeedItem{first, second}); err != nil {
		t.Fatal(err)
	}
	entries, err := store.ListFeed(ctx, FeedQuery{IncludeHidden: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	byID := make(map[string]domain.FeedEntry, len(entries))
	for _, entry := range entries {
		byID[entry.Item.ID] = entry
	}
	repo := byID[first.ID]
	if len(repo.Sources) != 2 || len(repo.Item.Sources) != 2 {
		t.Fatalf("repo sources not assembled: %+v", repo)
	}
	if len(repo.Item.Tags) != 2 || repo.Item.Tags[0] != "github" || repo.Item.Tags[1] != "go" {
		t.Fatalf("repo tags not assembled: %+v", repo.Item.Tags)
	}
	hn := byID[second.ID]
	if len(hn.Sources) != 1 || len(hn.Item.Tags) != 2 {
		t.Fatalf("hn entry not assembled: %+v", hn)
	}
}

func TestStoreListFeedSearchUsesFTSAndReindexesItems(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	item := testItem("id-fts", "hackernews:fts", "SQLite Search", 1)
	item.Summary = "Fast terminal radar with normalized cache"
	item.Author = "Ada"
	item.Organization = "Tilde Labs"
	item.Language = "Go"
	item.Tags = []string{"observability", "daily-signal"}
	item.Metadata = []byte(`{"engine":"sqlite fts5","topic":"source health"}`)
	if err := store.UpsertFeedItems(ctx, []domain.FeedItem{item}); err != nil {
		t.Fatal(err)
	}

	for _, search := range []string{"observability", "fts5", "SQLite: FTS5!"} {
		entries, err := store.ListFeed(ctx, FeedQuery{Search: search})
		if err != nil {
			t.Fatalf("search %q failed: %v", search, err)
		}
		if len(entries) != 1 || entries[0].Item.ID != item.ID {
			t.Fatalf("search %q returned %+v, want indexed item", search, entries)
		}
	}
	if _, err := store.ListFeed(ctx, FeedQuery{Search: "!!!"}); err != nil {
		t.Fatalf("punctuation-only search should not error: %v", err)
	}

	item.Title = "Personal Radar"
	item.Summary = "Fresh ranking inputs"
	item.Tags = []string{"freshness"}
	item.Metadata = []byte(`{"engine":"sqlite fts5","topic":"fetch history"}`)
	if err := store.UpsertFeedItems(ctx, []domain.FeedItem{item}); err != nil {
		t.Fatal(err)
	}
	entries, err := store.ListFeed(ctx, FeedQuery{Search: "observability"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("old indexed tag should have been removed, got %+v", entries)
	}
	entries, err = store.ListFeed(ctx, FeedQuery{Search: "fresh"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Item.ID != item.ID {
		t.Fatalf("updated indexed content missing: %+v", entries)
	}
}

func TestStoreSourceStatus(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()
	if err := store.UpdateSourceStatus(ctx, domain.SourceHackerNews, domain.SourceStatusOK, ""); err != nil {
		t.Fatal(err)
	}
	statuses, err := store.SourceStatuses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[domain.SourceID]domain.SourceStatus)
	for _, status := range statuses {
		seen[status.Source] = status.Status
	}
	if seen[domain.SourceHackerNews] != domain.SourceStatusOK {
		t.Fatalf("status not persisted: %+v", statuses)
	}
}

func TestStoreFetchHistoryRoundTripAndPrune(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	base := time.Date(2026, 5, 13, 9, 0, 0, 0, time.UTC)
	first := domain.FetchEvent{
		Source:      domain.SourceGitHub,
		SourceView:  "trending:daily",
		Status:      domain.SourceStatusOK,
		StartedAt:   base,
		FinishedAt:  base.Add(120 * time.Millisecond),
		Duration:    120 * time.Millisecond,
		ItemCount:   7,
		Stale:       true,
		StaleReason: "NETWORK_ERROR",
		Error:       "served stale cache",
	}
	if err := store.RecordFetchEvent(ctx, first); err != nil {
		t.Fatal(err)
	}
	recent, err := store.RecentFetchEvents(ctx, 12)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 1 {
		t.Fatalf("recent events = %d, want 1", len(recent))
	}
	got := recent[0]
	if got.Source != first.Source || got.SourceView != first.SourceView || got.Status != first.Status ||
		got.ItemCount != first.ItemCount || !got.Stale || got.StaleReason != first.StaleReason || got.Error != first.Error {
		t.Fatalf("fetch event mismatch: %+v", got)
	}
	if got.Duration != first.Duration || !got.StartedAt.Equal(first.StartedAt) || !got.FinishedAt.Equal(first.FinishedAt) {
		t.Fatalf("fetch event timing mismatch: %+v", got)
	}

	for i := 0; i < 502; i++ {
		startedAt := base.Add(time.Duration(i+1) * time.Second)
		if err := store.RecordFetchEvent(ctx, domain.FetchEvent{
			Source:     domain.SourceHackerNews,
			SourceView: "view-" + time.Duration(i).String(),
			Status:     domain.SourceStatusOK,
			StartedAt:  startedAt,
			FinishedAt: startedAt.Add(time.Millisecond),
			Duration:   time.Millisecond,
			ItemCount:  i,
		}); err != nil {
			t.Fatal(err)
		}
	}
	recent, err = store.RecentFetchEvents(ctx, 600)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 500 {
		t.Fatalf("recent events = %d, want pruned 500", len(recent))
	}
	if recent[0].ItemCount != 501 || recent[len(recent)-1].ItemCount != 2 {
		t.Fatalf("fetch history not newest-first/pruned: first=%+v last=%+v", recent[0], recent[len(recent)-1])
	}
}

func TestStorePersonalizationRulesAndPreferenceProfile(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	rule, err := store.CreatePersonalizationRule(ctx, domain.PersonalizationRule{
		Effect:  domain.RuleEffectBoost,
		Target:  domain.RuleTargetTag,
		Value:   "Go",
		Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rule.ID == 0 || !rule.Enabled || rule.Value != "go" {
		t.Fatalf("created rule not normalized: %+v", rule)
	}
	if _, err := store.CreatePersonalizationRule(ctx, domain.PersonalizationRule{
		Effect:  domain.RuleEffectHide,
		Target:  domain.RuleTargetKeyword,
		Value:   "sponsored",
		Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetPersonalizationRuleEnabled(ctx, rule.ID, false); err != nil {
		t.Fatal(err)
	}
	active, err := store.ListPersonalizationRules(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].Effect != domain.RuleEffectHide {
		t.Fatalf("active rules = %+v, want only hide rule", active)
	}
	all, err := store.ListPersonalizationRules(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("all rules = %d, want 2", len(all))
	}
	if err := store.DeletePersonalizationRule(ctx, rule.ID); err != nil {
		t.Fatal(err)
	}
	all, err = store.ListPersonalizationRules(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("all rules after delete = %+v, want 1", all)
	}

	saved := testSourceItem("repo-saved", "repo:owner/saved", "owner/saved", domain.SourceGitHub, 1)
	saved.Language = "Go"
	saved.Author = "Ada"
	saved.Tags = []string{"ai", "terminal"}
	hidden := testItem("hn-hidden", "hackernews:hidden", "Hidden crypto", 2)
	hidden.Tags = []string{"crypto"}
	if err := store.UpsertFeedItems(ctx, []domain.FeedItem{saved, hidden}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSaved(ctx, saved.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := store.SetHidden(ctx, hidden.ID, true); err != nil {
		t.Fatal(err)
	}
	profile, err := store.PreferenceProfile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if profile.SavedTags["ai"] != 1 || profile.SavedLanguages["go"] != 1 || profile.SavedRepos["owner/saved"] != 1 || profile.SavedAuthors["ada"] != 1 {
		t.Fatalf("saved profile mismatch: %+v", profile)
	}
	if profile.HiddenTags["crypto"] != 1 {
		t.Fatalf("hidden profile mismatch: %+v", profile)
	}
}

func TestStoreUpdatesPersonalizationRuleFields(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	rule, err := store.CreatePersonalizationRule(ctx, domain.PersonalizationRule{
		Effect:  domain.RuleEffectBoost,
		Target:  domain.RuleTargetKeyword,
		Value:   "SQLite",
		Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	updated, err := store.UpdatePersonalizationRule(ctx, rule.ID, domain.PersonalizationRule{
		Effect:  domain.RuleEffectMute,
		Target:  domain.RuleTargetDomain,
		Value:   "Example.COM",
		Enabled: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != rule.ID || updated.Effect != domain.RuleEffectMute || updated.Target != domain.RuleTargetDomain || updated.Value != "example.com" || updated.Enabled {
		t.Fatalf("updated rule = %+v", updated)
	}

	all, err := store.ListPersonalizationRules(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].Effect != domain.RuleEffectMute || all[0].Target != domain.RuleTargetDomain || all[0].Value != "example.com" || all[0].Enabled {
		t.Fatalf("persisted rules = %+v", all)
	}
}

func TestStoreDedupeCandidatesRoundTripAndIgnore(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	first := testItem("sim-a", "hackernews:sim-a", "SQLite FTS search for terminal source health", 1)
	first.Summary = "Fast local search and source health history"
	second := testItem("sim-b", "hackernews:sim-b", "SQLite full text search for terminal source health", 2)
	second.Summary = "Fast local search and source health dashboard"
	unrelated := testItem("sim-c", "hackernews:sim-c", "GPU diffusion image benchmark", 3)
	unrelated.Summary = "Model quality comparisons for image generation"
	if err := store.UpsertFeedItems(ctx, []domain.FeedItem{first, second, unrelated}); err != nil {
		t.Fatal(err)
	}
	entries, err := store.ListFeed(ctx, FeedQuery{IncludeHidden: true})
	if err != nil {
		t.Fatal(err)
	}
	simhashes := make(map[string]string)
	for _, entry := range entries {
		simhashes[entry.Item.ID] = entry.Item.SimHash
	}
	if simhashes[first.ID] == "" || simhashes[second.ID] == "" || simhashes[unrelated.ID] == "" {
		t.Fatalf("simhashes were not persisted: %+v", simhashes)
	}

	candidates, err := store.ListDedupeCandidates(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) == 0 {
		t.Fatal("expected at least one dedupe candidate")
	}
	candidate := candidates[0]
	if candidate.ItemA.ID != first.ID || candidate.ItemB.ID != second.ID {
		t.Fatalf("candidate pair = %+v, want similar items", candidate)
	}
	if len(candidate.ItemA.Sources) != 1 || candidate.ItemA.Sources[0].Source != domain.SourceHackerNews || candidate.ItemA.Sources[0].SourceView != "top" || candidate.ItemA.Sources[0].SourceRank != 1 {
		t.Fatalf("candidate item A source context = %+v", candidate.ItemA.Sources)
	}
	if len(candidate.ItemB.Sources) != 1 || candidate.ItemB.Sources[0].Source != domain.SourceHackerNews || candidate.ItemB.Sources[0].SourceView != "top" || candidate.ItemB.Sources[0].SourceRank != 2 {
		t.Fatalf("candidate item B source context = %+v", candidate.ItemB.Sources)
	}
	if candidate.Distance > 16 || candidate.Score <= 0 || candidate.Reason == "" {
		t.Fatalf("candidate details invalid: %+v", candidate)
	}
	if err := store.IgnoreDedupeCandidate(ctx, candidate.Key); err != nil {
		t.Fatal(err)
	}
	candidates, err = store.ListDedupeCandidates(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range candidates {
		if got.Key == candidate.Key {
			t.Fatalf("ignored candidate still returned: %+v", got)
		}
	}
}

func TestStoreRecommendationScoresRoundTripAndFilters(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	goItem := testSourceItem("repo-go", "repo:owner/go", "Go terminal radar", domain.SourceGitHub, 1)
	goItem.Language = "Go"
	goItem.Tags = []string{"ai", "terminal"}
	pythonItem := testItem("hn-python", "hackernews:python", "Python launch", 2)
	pythonItem.Language = "Python"
	pythonItem.Tags = []string{"launch"}
	if err := store.UpsertFeedItems(ctx, []domain.FeedItem{goItem, pythonItem}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSaved(ctx, goItem.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := store.SetRead(ctx, pythonItem.ID, true); err != nil {
		t.Fatal(err)
	}

	computedAt := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	if err := store.ReplaceRecommendationScores(ctx, []domain.RecommendationScore{
		{ItemID: pythonItem.ID, Score: 0.7, InterestScore: 0.5, HotScore: 0.8, ComputedAt: computedAt},
		{ItemID: goItem.ID, Score: 1.2, InterestScore: 1.0, HotScore: 0.8, ComputedAt: computedAt},
	}); err != nil {
		t.Fatal(err)
	}

	entries, err := store.ListRecommendedFeed(ctx, domain.FeedQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Item.ID != goItem.ID || entries[1].Item.ID != pythonItem.ID {
		t.Fatalf("recommendations not score sorted: %+v", entries)
	}
	count, err := store.CountRecommendedFeed(ctx, domain.FeedQuery{Search: "terminal", SavedOnly: true, Language: "go", Tag: "ai"})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("filtered recommendation count = %d, want 1", count)
	}
	entries, err = store.ListRecommendedFeed(ctx, domain.FeedQuery{UnreadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Item.ID != goItem.ID {
		t.Fatalf("unread recommendations = %+v, want only unread go item", entries)
	}

	if err := store.ReplaceRecommendationScores(ctx, []domain.RecommendationScore{
		{ItemID: pythonItem.ID, Score: 2.0, InterestScore: 1.5, HotScore: 2.0, ComputedAt: computedAt.Add(time.Minute)},
	}); err != nil {
		t.Fatal(err)
	}
	entries, err = store.ListRecommendedFeed(ctx, domain.FeedQuery{IncludeHidden: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Item.ID != pythonItem.ID {
		t.Fatalf("replace should remove stale recommendation rows, got %+v", entries)
	}
	if err := store.SetHidden(ctx, pythonItem.ID, true); err != nil {
		t.Fatal(err)
	}
	entries, err = store.ListRecommendedFeed(ctx, domain.FeedQuery{IncludeHidden: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("recommendations should ignore IncludeHidden and hide durable hidden rows: %+v", entries)
	}
}

func TestStoreRecommendationEventsTermsProfileAndReasons(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	item := testSourceItem("repo-ai", "repo:owner/ai", "AI terminal agent", domain.SourceGitHub, 1)
	item.LastSeenAt = now
	item.Tags = []string{"ai"}
	if err := store.UpsertFeedItems(ctx, []domain.FeedItem{item}); err != nil {
		t.Fatal(err)
	}
	terms := itemTermsForTest(t, store, item.ID)
	if !terms[recommend.TermKey("tag", "ai")] || !terms[recommend.TermKey("source", "github")] {
		t.Fatalf("persisted terms missing expected tag/source: %+v", terms)
	}

	item.Tags = []string{"rust"}
	if err := store.UpsertFeedItems(ctx, []domain.FeedItem{item}); err != nil {
		t.Fatal(err)
	}
	terms = itemTermsForTest(t, store, item.ID)
	if terms[recommend.TermKey("tag", "ai")] || !terms[recommend.TermKey("tag", "rust")] {
		t.Fatalf("terms were not refreshed after upsert: %+v", terms)
	}

	if err := store.RecordItemEvent(ctx, domain.ItemEvent{
		ItemID:     item.ID,
		EventType:  domain.ItemEventOpenURL,
		Source:     domain.SourceGitHub,
		View:       domain.SourceRecommend,
		OccurredAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	profile, err := store.RecommendationProfile(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	sourceTerm := profile.Terms[recommend.TermKey("source", "github")]
	if sourceTerm.Positive <= 0 {
		t.Fatalf("source profile term = %+v, want positive", sourceTerm)
	}
	if len(sourceTerm.Reasons) == 0 || sourceTerm.Reasons[0].Label != "opened github" {
		t.Fatalf("source reasons = %+v, want opened github", sourceTerm.Reasons)
	}

	reasons := []domain.RecommendationReason{{Kind: "tag", Value: "rust", Label: "opened rust", Weight: 0.8}}
	if err := store.ReplaceRecommendationScores(ctx, []domain.RecommendationScore{{
		ItemID:        item.ID,
		Score:         1.2,
		InterestScore: 1.0,
		HotScore:      0.8,
		Reasons:       reasons,
		ComputedAt:    now,
	}}); err != nil {
		t.Fatal(err)
	}
	entries, err := store.ListRecommendedFeed(ctx, domain.FeedQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || len(entries[0].RecommendationReasons) != 1 || entries[0].RecommendationReasons[0].Label != "opened rust" {
		t.Fatalf("recommendation reasons did not round-trip: %+v", entries)
	}
}

func TestStoreRecommendationProfileBackfillsMissingTerms(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	now := time.Now().UTC()
	item := testSourceItem("repo-ai-backfill", "repo:owner/ai-backfill", "AI agent backfill", domain.SourceGitHub, 1)
	item.LastSeenAt = now
	item.Tags = []string{"ai"}
	if err := store.UpsertFeedItems(ctx, []domain.FeedItem{item}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSaved(ctx, item.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `DELETE FROM item_terms WHERE item_id = ?`, item.ID); err != nil {
		t.Fatal(err)
	}

	profile, err := store.RecommendationProfile(ctx, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if term := profile.Terms[recommend.TermKey("tag", "ai")]; term.Positive <= 0 {
		t.Fatalf("backfilled tag profile term = %+v, want positive", term)
	}
	if terms := itemTermsForTest(t, store, item.ID); !terms[recommend.TermKey("tag", "ai")] {
		t.Fatalf("item terms were not backfilled: %+v", terms)
	}
}

func TestStoreInitialMVPSourceStatusesAreEnabled(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	statuses, err := store.SourceStatuses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[domain.SourceID]domain.SourceStatus)
	for _, status := range statuses {
		seen[status.Source] = status.Status
	}
	for _, source := range []domain.SourceID{domain.SourceHackerNews, domain.SourceGitHub, domain.SourceAILabs, domain.SourceHuggingFace, domain.SourceLobsters, domain.SourceProductHunt} {
		got, ok := seen[source]
		if !ok {
			t.Fatalf("%s initial status missing", source)
		}
		if got == domain.SourceStatusDisabled {
			t.Fatalf("%s initial status = DISABLED, want enabled MVP source", source)
		}
	}
}

func TestStoreMigrateIsIdempotent(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	before := migrationVersionRows(t, store.db)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	after := migrationVersionRows(t, store.db)
	if after != before {
		t.Fatalf("migration version rows changed after second migrate: before=%d after=%d", before, after)
	}
	latest := latestMigrationVersion(t, store.db)
	if latest != 8 {
		t.Fatalf("latest migration version = %d, want 8", latest)
	}
}

func TestStoreMigrateAcceptsExistingGooseVersionTable(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tildewire.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	schema, err := os.ReadFile("schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, string(schema)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `CREATE TABLE goose_db_version (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		version_id INTEGER NOT NULL,
		is_applied INTEGER NOT NULL,
		tstamp TIMESTAMP DEFAULT (datetime('now'))
	)`); err != nil {
		t.Fatal(err)
	}
	for version := 1; version <= 6; version++ {
		if _, err := store.db.ExecContext(ctx, `INSERT INTO goose_db_version (version_id, is_applied) VALUES (?, 1)`, version); err != nil {
			t.Fatal(err)
		}
	}

	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if got := migrationVersionRows(t, store.db); got != 8 {
		t.Fatalf("migration version rows = %d, want versions 1 through 8", got)
	}
	if got := latestMigrationVersion(t, store.db); got != 8 {
		t.Fatalf("latest migration version = %d, want 8", got)
	}
}

func TestStoreOpenSerializesSQLiteConnections(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	if got := store.db.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("max open connections = %d, want 1 to avoid SQLITE_BUSY during refresh writes", got)
	}
}

func TestStoreHTTPCacheRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	fetchedAt := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	expiresAt := fetchedAt.Add(time.Hour)
	entry := httpcache.Entry{
		RequestKey:   "GET https://example.com/api",
		Source:       "hackernews",
		Method:       "GET",
		URL:          "https://example.com/api",
		StatusCode:   200,
		HeadersJSON:  `{"ETag":["abc"]}`,
		Body:         []byte(`{"ok":true}`),
		FetchedAt:    fetchedAt,
		ExpiresAt:    expiresAt,
		ETag:         "abc",
		LastModified: "Sun, 10 May 2026 10:00:00 GMT",
	}
	if err := store.PutHTTPCache(ctx, entry); err != nil {
		t.Fatal(err)
	}
	got, ok, err := store.GetHTTPCache(ctx, entry.RequestKey)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected cache hit")
	}
	if string(got.Body) != string(entry.Body) || got.StatusCode != entry.StatusCode || got.ETag != entry.ETag {
		t.Fatalf("cache roundtrip mismatch: %+v", got)
	}
	if !got.FetchedAt.Equal(fetchedAt) || !got.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("cache times mismatch: fetched=%s expires=%s", got.FetchedAt, got.ExpiresAt)
	}
}

func TestStoreClearCacheRemovesCachedDataAndKeepsSavedItems(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	saved := testItem("saved", "hackernews:saved", "Saved item", 1)
	unsaved := testItem("unsaved", "hackernews:unsaved", "Cached item", 2)
	if err := store.UpsertFeedItems(ctx, []domain.FeedItem{saved, unsaved}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSaved(ctx, saved.ID, true); err != nil {
		t.Fatal(err)
	}
	cacheEntry := httpcache.Entry{
		RequestKey: "GET https://example.com/api",
		Source:     "hackernews",
		Method:     "GET",
		URL:        "https://example.com/api",
		StatusCode: 200,
		Body:       []byte(`{"ok":true}`),
		FetchedAt:  time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC),
		ExpiresAt:  time.Date(2026, 5, 10, 11, 0, 0, 0, time.UTC),
	}
	if err := store.PutHTTPCache(ctx, cacheEntry); err != nil {
		t.Fatal(err)
	}
	if err := store.SetRateLimitCooldown(ctx, "hackernews", cacheEntry.RequestKey, time.Date(2026, 5, 10, 10, 30, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateSourceStatus(ctx, domain.SourceHackerNews, domain.SourceStatusOK, ""); err != nil {
		t.Fatal(err)
	}

	if err := store.ClearCache(ctx); err != nil {
		t.Fatal(err)
	}

	entries, err := store.ListFeed(ctx, FeedQuery{IncludeHidden: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Item.ID != saved.ID || !entries[0].State.Saved {
		t.Fatalf("clear cache should keep only saved item, got %+v", entries)
	}
	if _, ok, err := store.GetHTTPCache(ctx, cacheEntry.RequestKey); err != nil || ok {
		t.Fatalf("http cache should be cleared: ok=%v err=%v", ok, err)
	}
	if _, ok, err := store.RateLimitCooldown(ctx, "hackernews", cacheEntry.RequestKey); err != nil || ok {
		t.Fatalf("rate limit cooldown should be cleared: ok=%v err=%v", ok, err)
	}
	statuses, err := store.SourceStatuses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := findStoreStatus(statuses, domain.SourceHackerNews); got != domain.SourceStatusUnknown {
		t.Fatalf("source status = %s, want UNKNOWN", got)
	}
}

func TestStoreRateLimitCooldownRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	until := time.Date(2026, 5, 10, 10, 30, 0, 0, time.UTC)
	if err := store.SetRateLimitCooldown(ctx, "hackernews", "GET https://example.com/api", until); err != nil {
		t.Fatal(err)
	}
	got, ok, err := store.RateLimitCooldown(ctx, "hackernews", "GET https://example.com/api")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !got.Equal(until) {
		t.Fatalf("cooldown = %s/%v, want %s/true", got, ok, until)
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "tildewire.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return store
}

func migrationVersionRows(t *testing.T, db *sql.DB) int {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM goose_db_version WHERE is_applied = 1`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func latestMigrationVersion(t *testing.T, db *sql.DB) int {
	t.Helper()
	var version int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version_id), 0) FROM goose_db_version WHERE is_applied = 1`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	return version
}

func itemTermsForTest(t *testing.T, store *Store, itemID string) map[string]bool {
	t.Helper()
	rows, err := store.db.Query(`SELECT kind, value FROM item_terms WHERE item_id = ?`, itemID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	terms := make(map[string]bool)
	for rows.Next() {
		var kind string
		var value string
		if err := rows.Scan(&kind, &value); err != nil {
			t.Fatal(err)
		}
		terms[recommend.TermKey(kind, value)] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return terms
}

func findStoreStatus(statuses []domain.SourceHealth, source domain.SourceID) domain.SourceStatus {
	for _, status := range statuses {
		if status.Source == source {
			return status.Status
		}
	}
	return ""
}

func testItem(id, key, title string, rank int) domain.FeedItem {
	return testSourceItem(id, key, title, domain.SourceHackerNews, rank)
}

func testSourceItem(id, key, title string, source domain.SourceID, rank int) domain.FeedItem {
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	itemType := "story"
	sourceView := "top"
	sourceURL := "https://news.ycombinator.com/item?id=" + id
	if source == domain.SourceGitHub {
		itemType = "repo"
		sourceView = "trending:daily"
		sourceURL = "https://github.com/" + title
	}
	item := domain.FeedItem{
		ID:           id,
		CanonicalKey: key,
		Title:        title,
		URL:          "https://example.com/" + id,
		ItemType:     itemType,
		FirstSeenAt:  now,
		LastSeenAt:   now.Add(time.Duration(rank) * time.Minute),
		Sources: []domain.ItemSource{{
			ItemID:      id,
			Source:      source,
			SourceView:  sourceView,
			SourceIDRaw: id,
			SourceRank:  rank,
			SourceURL:   sourceURL,
			SeenAt:      now,
		}},
	}
	return dedupe.CanonicalizeItem(item)
}

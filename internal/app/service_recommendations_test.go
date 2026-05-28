package app

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/AIluffy/tildewire/internal/domain"
	"github.com/AIluffy/tildewire/internal/httpx"
)

func loadRecommendFeed(t *testing.T, service *Service, ctx context.Context, filter FeedFilter) Snapshot {
	t.Helper()
	if err := service.RefreshRecommendations(ctx); err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.LoadFeed(ctx, domain.SourceRecommend, filter)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestLoadFeedAppliesPersonalizationRulesAndKeepsSourceRank(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()

	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	highRank := appTestSourceItem("hn-high", "hackernews:high", "Low value story", domain.SourceHackerNews, 1, now)
	highRank.Tags = []string{"general"}
	boosted := appTestSourceItem("hn-boosted", "hackernews:boosted", "Rust terminal radar", domain.SourceHackerNews, 20, now)
	boosted.Language = "Rust"
	boosted.Tags = []string{"ai"}
	hiddenByRule := appTestSourceItem("hn-sponsored", "hackernews:sponsored", "Sponsored compiler launch", domain.SourceHackerNews, 2, now)
	if err := db.UpsertFeedItems(ctx, []domain.FeedItem{highRank, boosted, hiddenByRule}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreatePersonalizationRule(ctx, domain.PersonalizationRule{Effect: domain.RuleEffectBoost, Target: domain.RuleTargetTag, Value: "ai", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreatePersonalizationRule(ctx, domain.PersonalizationRule{Effect: domain.RuleEffectHide, Target: domain.RuleTargetKeyword, Value: "sponsored", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, httpx.New(time.Second), nil)

	all, err := service.LoadFeed(ctx, domain.SourceAll, FeedFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Entries) != 2 {
		t.Fatalf("all entries = %+v, want hidden rule to filter sponsored item", all.Entries)
	}
	if all.Entries[0].Item.ID != boosted.ID {
		t.Fatalf("boosted item should lead All view: %+v", all.Entries)
	}
	withHidden, err := service.LoadFeed(ctx, domain.SourceAll, FeedFilter{IncludeHidden: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(withHidden.Entries) != 3 {
		t.Fatalf("include hidden entries = %d, want 3", len(withHidden.Entries))
	}
	sourceView, err := service.LoadFeed(ctx, domain.SourceHackerNews, FeedFilter{IncludeHidden: true})
	if err != nil {
		t.Fatal(err)
	}
	if sourceView.Entries[0].Item.ID != highRank.ID {
		t.Fatalf("single-source view should preserve source rank: %+v", sourceView.Entries)
	}
}

func TestLoadFeedUsesSavedAndHiddenPreferenceSignals(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()

	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	savedSeed := appTestSourceItem("seed-ai", "hackernews:seed-ai", "Saved AI seed", domain.SourceHackerNews, 10, now)
	savedSeed.Tags = []string{"ai"}
	hiddenSeed := appTestSourceItem("seed-crypto", "hackernews:seed-crypto", "Hidden crypto seed", domain.SourceHackerNews, 10, now)
	hiddenSeed.Tags = []string{"crypto"}
	aiItem := appTestSourceItem("candidate-ai", "hackernews:candidate-ai", "AI candidate", domain.SourceHackerNews, 10, now)
	aiItem.Tags = []string{"ai"}
	cryptoItem := appTestSourceItem("candidate-crypto", "hackernews:candidate-crypto", "Crypto candidate", domain.SourceHackerNews, 10, now)
	cryptoItem.Tags = []string{"crypto"}
	if err := db.UpsertFeedItems(ctx, []domain.FeedItem{savedSeed, hiddenSeed, aiItem, cryptoItem}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSaved(ctx, savedSeed.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := db.SetHidden(ctx, hiddenSeed.ID, true); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, httpx.New(time.Second), nil)

	snapshot, err := service.LoadFeed(ctx, domain.SourceAll, FeedFilter{IncludeHidden: true})
	if err != nil {
		t.Fatal(err)
	}
	scores := make(map[string]float64)
	for _, entry := range snapshot.Entries {
		scores[entry.Item.ID] = entry.HotScore
	}
	if scores[aiItem.ID] <= scores[cryptoItem.ID] {
		t.Fatalf("saved/hidden preferences should favor ai over crypto: scores=%+v", scores)
	}
}

func TestRecommendFeedUsesBoostRulesAndSavedProfile(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()

	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	boosted := appTestSourceItem("hn-boosted", "hackernews:boosted", "SQLite terminal radar", domain.SourceHackerNews, 5, now)
	boosted.Tags = []string{"sqlite"}
	savedSeed := appTestSourceItem("hn-seed", "hackernews:seed", "Saved AI seed", domain.SourceHackerNews, 1, now)
	savedSeed.Tags = []string{"ai"}
	aiCandidate := appTestSourceItem("hn-ai", "hackernews:ai", "AI candidate", domain.SourceHackerNews, 2, now)
	aiCandidate.Tags = []string{"ai"}
	general := appTestSourceItem("gh-general", "repo:owner/general", "General launch", domain.SourceGitHub, 3, now)
	if err := db.UpsertFeedItems(ctx, []domain.FeedItem{boosted, savedSeed, aiCandidate, general}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSaved(ctx, savedSeed.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreatePersonalizationRule(ctx, domain.PersonalizationRule{
		Effect:  domain.RuleEffectBoost,
		Target:  domain.RuleTargetKeyword,
		Value:   "sqlite",
		Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, httpx.New(time.Second), nil)

	snapshot := loadRecommendFeed(t, service, ctx, FeedFilter{})
	ids := appEntryIDs(snapshot.Entries)
	if !slices.Contains(ids, boosted.ID) || !slices.Contains(ids, aiCandidate.ID) || slices.Contains(ids, general.ID) {
		t.Fatalf("recommend ids = %+v, want boosted and saved-profile candidate without general item", ids)
	}
	if snapshot.Counts[domain.SourceRecommend] != len(snapshot.Entries) {
		t.Fatalf("recommend count = %d, entries = %d", snapshot.Counts[domain.SourceRecommend], len(snapshot.Entries))
	}
}

func TestRecommendFeedUsesInteractionProfileAndReasons(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()

	now := time.Now().UTC()
	seed := appTestSourceItem("hn-ai-seed", "hackernews:ai-seed", "Opened AI seed", domain.SourceHackerNews, 1, now)
	seed.Tags = []string{"ai"}
	candidate := appTestSourceItem("gh-ai-candidate", "repo:owner/ai-candidate", "AI candidate", domain.SourceGitHub, 1, now)
	candidate.Tags = []string{"ai"}
	general := appTestSourceItem("gh-general", "repo:owner/general", "General candidate", domain.SourceGitHub, 2, now)
	if err := db.UpsertFeedItems(ctx, []domain.FeedItem{seed, candidate, general}); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordItemEvent(ctx, domain.ItemEvent{
		ItemID:     seed.ID,
		EventType:  domain.ItemEventOpenURL,
		Source:     domain.SourceHackerNews,
		View:       domain.SourceAll,
		OccurredAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, httpx.New(time.Second), nil)

	snapshot := loadRecommendFeed(t, service, ctx, FeedFilter{})
	ids := appEntryIDs(snapshot.Entries)
	if !slices.Contains(ids, candidate.ID) || slices.Contains(ids, general.ID) {
		t.Fatalf("recommend ids = %+v, want interaction-trained candidate without general item", ids)
	}
	entry, ok := appEntryByID(snapshot.Entries, candidate.ID)
	if !ok {
		t.Fatalf("candidate %s missing from %+v", candidate.ID, ids)
	}
	if !recommendationReasonsContain(entry.RecommendationReasons, "opened ai") {
		t.Fatalf("candidate reasons = %+v, want opened ai", entry.RecommendationReasons)
	}
}

func TestRecommendFeedDecaysOlderInteractionSignals(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()

	now := time.Now().UTC()
	oldSeed := appTestSourceItem("hn-ai-old", "hackernews:ai-old", "Old AI seed", domain.SourceHackerNews, 10, now)
	oldSeed.Tags = []string{"ai"}
	recentSeed := appTestSourceItem("hn-rust-recent", "hackernews:rust-recent", "Recent Rust seed", domain.SourceHackerNews, 10, now)
	recentSeed.Tags = []string{"rust"}
	aiCandidate := appTestSourceItem("gh-ai", "repo:owner/ai", "AI candidate", domain.SourceGitHub, 10, now)
	aiCandidate.Tags = []string{"ai"}
	rustCandidate := appTestSourceItem("gh-rust", "repo:owner/rust", "Rust candidate", domain.SourceGitHub, 10, now)
	rustCandidate.Tags = []string{"rust"}
	if err := db.UpsertFeedItems(ctx, []domain.FeedItem{oldSeed, recentSeed, aiCandidate, rustCandidate}); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordItemEvent(ctx, domain.ItemEvent{ItemID: oldSeed.ID, EventType: domain.ItemEventOpenURL, OccurredAt: now.Add(-60 * 24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordItemEvent(ctx, domain.ItemEvent{ItemID: recentSeed.ID, EventType: domain.ItemEventOpenURL, OccurredAt: now}); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, httpx.New(time.Second), nil)

	snapshot := loadRecommendFeed(t, service, ctx, FeedFilter{})
	rustIndex := appEntryIndex(snapshot.Entries, rustCandidate.ID)
	aiIndex := appEntryIndex(snapshot.Entries, aiCandidate.ID)
	if rustIndex < 0 || aiIndex < 0 || rustIndex > aiIndex {
		t.Fatalf("recommend order = %+v, want recent rust candidate before older ai candidate", appEntryIDs(snapshot.Entries))
	}
}

func TestRecommendFeedLimitsToTopTen(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()

	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	items := make([]domain.FeedItem, 0, 12)
	for rank := 1; rank <= 12; rank++ {
		item := appTestSourceItem(
			fmt.Sprintf("hn-match-%02d", rank),
			fmt.Sprintf("hackernews:match-%02d", rank),
			fmt.Sprintf("Match candidate %02d", rank),
			domain.SourceHackerNews,
			rank,
			now,
		)
		items = append(items, item)
	}
	if err := db.UpsertFeedItems(ctx, items); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreatePersonalizationRule(ctx, domain.PersonalizationRule{
		Effect:  domain.RuleEffectBoost,
		Target:  domain.RuleTargetKeyword,
		Value:   "match",
		Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, httpx.New(time.Second), nil)

	snapshot := loadRecommendFeed(t, service, ctx, FeedFilter{})
	if len(snapshot.Entries) != 10 {
		t.Fatalf("recommend entries = %d, want top 10", len(snapshot.Entries))
	}
	if snapshot.Counts[domain.SourceRecommend] != 10 {
		t.Fatalf("recommend count = %d, want capped 10", snapshot.Counts[domain.SourceRecommend])
	}
	for idx, entry := range snapshot.Entries {
		wantID := items[idx].ID
		if entry.Item.ID != wantID {
			t.Fatalf("recommend entry %d = %s, want %s", idx, entry.Item.ID, wantID)
		}
	}
}

func TestRecommendFeedSurfacesSavedSourceInTopTen(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()

	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	items := make([]domain.FeedItem, 0, 15)
	aiSeed := appTestSourceItem("hn-ai-seed", "hackernews:ai-seed", "Saved AI seed", domain.SourceHackerNews, 1, now.Add(time.Minute))
	aiSeed.Tags = []string{"ai"}
	aiSeed.Language = "go"
	aiSeed.Author = "acme"
	items = append(items, aiSeed)
	for rank := 1; rank <= 12; rank++ {
		item := appTestSourceItem(
			fmt.Sprintf("hn-ai-%02d", rank),
			fmt.Sprintf("hackernews:ai-%02d", rank),
			fmt.Sprintf("AI candidate %02d", rank),
			domain.SourceHackerNews,
			rank,
			now.Add(time.Duration(rank)*time.Second),
		)
		item.Tags = []string{"ai"}
		item.Language = "go"
		item.Author = "acme"
		items = append(items, item)
	}
	githubOne := appTestSourceItem("gh-one", "repo:owner/one", "owner/one", domain.SourceGitHub, 25, now)
	githubTwo := appTestSourceItem("gh-two", "repo:owner/two", "owner/two", domain.SourceGitHub, 26, now.Add(time.Second))
	items = append(items, githubOne, githubTwo)
	if err := db.UpsertFeedItems(ctx, items); err != nil {
		t.Fatal(err)
	}
	for _, itemID := range []string{aiSeed.ID, githubOne.ID, githubTwo.ID} {
		if err := db.SetSaved(ctx, itemID, true); err != nil {
			t.Fatal(err)
		}
	}
	service := NewService(db, httpx.New(time.Second), nil)

	snapshot := loadRecommendFeed(t, service, ctx, FeedFilter{})
	if !snapshotHasSource(snapshot, domain.SourceGitHub) {
		t.Fatalf("recommend ids = %+v, want saved GitHub source represented in top 10", appEntryIDs(snapshot.Entries))
	}
}

func TestRecommendFeedSamplesAcrossSourcesWhenLatestWindowIsDominated(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()

	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	seed := appTestSourceItem("hn-ai-seed-balanced", "hackernews:ai-seed-balanced", "Saved AI seed", domain.SourceHackerNews, 1, now.Add(-2*time.Hour))
	seed.Tags = []string{"ai"}
	githubCandidate := appTestSourceItem("gh-ai-balanced", "repo:owner/ai-balanced", "AI candidate", domain.SourceGitHub, 500, now.Add(-48*time.Hour))
	githubCandidate.Tags = []string{"ai"}
	items := []domain.FeedItem{seed, githubCandidate}
	for idx := 0; idx < recommendLatestFeedLimit; idx++ {
		item := appTestSourceItem(
			fmt.Sprintf("ph-ai-%03d", idx),
			fmt.Sprintf("producthunt:ai-%03d", idx),
			fmt.Sprintf("AI Product Hunt item %03d", idx),
			domain.SourceProductHunt,
			idx+1,
			now.Add(time.Duration(idx)*time.Second),
		)
		item.Tags = []string{"ai"}
		items = append(items, item)
	}
	if err := db.UpsertFeedItems(ctx, items); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSaved(ctx, seed.ID, true); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, httpx.New(time.Second), nil)

	snapshot := loadRecommendFeed(t, service, ctx, FeedFilter{})
	if !snapshotHasSource(snapshot, domain.SourceGitHub) {
		t.Fatalf("recommend ids = %+v, want non-Product-Hunt candidate represented when Product Hunt fills latest window", appEntryIDs(snapshot.Entries))
	}
}

func TestRecommendFeedUsesLatestCachedFeedOnly(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()

	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	const latestFeedLimit = 250
	recent := make([]domain.FeedItem, 0, latestFeedLimit)
	for idx := 0; idx < latestFeedLimit; idx++ {
		recent = append(recent, appTestSourceItem(
			fmt.Sprintf("recent-%03d", idx),
			fmt.Sprintf("hackernews:recent-%03d", idx),
			fmt.Sprintf("Recent item %03d", idx),
			domain.SourceHackerNews,
			idx+1,
			now.Add(time.Duration(idx)*time.Second),
		))
	}
	oldBoosted := appTestSourceItem("old-sqlite", "hackernews:old-sqlite", "SQLite old hit", domain.SourceHackerNews, 1, now.Add(-24*time.Hour))
	if err := db.UpsertFeedItems(ctx, append(recent, oldBoosted)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreatePersonalizationRule(ctx, domain.PersonalizationRule{
		Effect:  domain.RuleEffectBoost,
		Target:  domain.RuleTargetKeyword,
		Value:   "sqlite",
		Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, httpx.New(time.Second), nil)

	snapshot := loadRecommendFeed(t, service, ctx, FeedFilter{})
	if slices.Contains(appEntryIDs(snapshot.Entries), oldBoosted.ID) {
		t.Fatalf("recommend ids = %+v, old item outside latest feed should not be recommended", appEntryIDs(snapshot.Entries))
	}
	entries, err := db.ListFeed(ctx, domain.FeedQuery{Limit: latestFeedLimit + 1})
	if err != nil {
		t.Fatal(err)
	}
	selected, ok := appEntryByID(entries, oldBoosted.ID)
	if !ok {
		t.Fatalf("old boosted item %s missing from feed", oldBoosted.ID)
	}
	diagnostics, err := service.RecommendationDiagnostics(ctx, selected)
	if err != nil {
		t.Fatal(err)
	}
	if diagnostics.Selected.Eligible || diagnostics.Selected.ExclusionReason != string(domain.RecommendationExclusionOutsideWindow) {
		t.Fatalf("old boosted diagnostics = %+v, want outside-window exclusion", diagnostics.Selected)
	}
}

func TestRecommendFeedExcludesMutedHiddenAndDisabledSources(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()

	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	kept := appTestSourceItem("hn-keep", "hackernews:keep", "AI kept", domain.SourceHackerNews, 1, now)
	muted := appTestSourceItem("hn-muted", "hackernews:muted", "AI muted", domain.SourceHackerNews, 2, now)
	hiddenByRule := appTestSourceItem("hn-sponsored", "hackernews:sponsored", "AI sponsored", domain.SourceHackerNews, 3, now)
	durableHidden := appTestSourceItem("hn-hidden", "hackernews:hidden", "AI hidden", domain.SourceHackerNews, 4, now)
	disabledSource := appTestSourceItem("gh-ai", "repo:owner/ai", "AI github", domain.SourceGitHub, 1, now)
	if err := db.UpsertFeedItems(ctx, []domain.FeedItem{kept, muted, hiddenByRule, durableHidden, disabledSource}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetHidden(ctx, durableHidden.ID, true); err != nil {
		t.Fatal(err)
	}
	for _, rule := range []domain.PersonalizationRule{
		{Effect: domain.RuleEffectBoost, Target: domain.RuleTargetKeyword, Value: "ai", Enabled: true},
		{Effect: domain.RuleEffectMute, Target: domain.RuleTargetKeyword, Value: "muted", Enabled: true},
		{Effect: domain.RuleEffectHide, Target: domain.RuleTargetKeyword, Value: "sponsored", Enabled: true},
	} {
		if _, err := db.CreatePersonalizationRule(ctx, rule); err != nil {
			t.Fatal(err)
		}
	}
	service := NewService(db, httpx.New(time.Second), nil)
	service.SetSourceConfig([]domain.SourceID{domain.SourceHackerNews}, nil)

	snapshot := loadRecommendFeed(t, service, ctx, FeedFilter{IncludeHidden: true})
	ids := appEntryIDs(snapshot.Entries)
	if !reflect.DeepEqual(ids, []string{kept.ID}) {
		t.Fatalf("recommend ids = %+v, want only kept HN item", ids)
	}
}

func TestRecommendFeedRecomputesAfterRuleAndStateChanges(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()

	item := appTestSourceItem("hn-sqlite", "hackernews:sqlite", "SQLite launch", domain.SourceHackerNews, 1, time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC))
	if err := db.UpsertFeedItems(ctx, []domain.FeedItem{item}); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, httpx.New(time.Second), nil)

	snapshot := loadRecommendFeed(t, service, ctx, FeedFilter{})
	if len(snapshot.Entries) != 0 {
		t.Fatalf("cold recommend entries = %+v, want empty", snapshot.Entries)
	}
	snapshot, err := service.CreatePersonalizationRule(ctx, domain.PersonalizationRule{
		Effect:  domain.RuleEffectBoost,
		Target:  domain.RuleTargetKeyword,
		Value:   "sqlite",
		Enabled: true,
	}, domain.SourceRecommend, FeedFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Entries) != 1 || snapshot.Entries[0].Item.ID != item.ID {
		t.Fatalf("recommend after boost = %+v, want sqlite item", snapshot.Entries)
	}
	if err := service.SetHidden(ctx, item.ID, true); err != nil {
		t.Fatal(err)
	}
	snapshot = loadRecommendFeed(t, service, ctx, FeedFilter{IncludeHidden: true})
	if len(snapshot.Entries) != 0 {
		t.Fatalf("recommend after hide = %+v, want empty despite IncludeHidden", snapshot.Entries)
	}
}

func TestRecommendationDiagnosticsReportsProfileAndSelectedItem(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()

	now := time.Now().UTC()
	seed := appTestSourceItem("gh-ai-seed", "repo:owner/ai-seed", "Opened AI seed", domain.SourceGitHub, 1, now)
	seed.Tags = []string{"ai"}
	hiddenSeed := appTestSourceItem("hn-rust-hidden", "hackernews:rust-hidden", "Hidden Rust seed", domain.SourceHackerNews, 2, now)
	hiddenSeed.Tags = []string{"rust"}
	candidate := appTestSourceItem("gh-ai-candidate", "repo:owner/ai-candidate", "AI candidate", domain.SourceGitHub, 1, now)
	candidate.Tags = []string{"ai"}
	if err := db.UpsertFeedItems(ctx, []domain.FeedItem{seed, hiddenSeed, candidate}); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordItemEvent(ctx, domain.ItemEvent{ItemID: seed.ID, EventType: domain.ItemEventOpenURL, OccurredAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetHidden(ctx, hiddenSeed.ID, true); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, httpx.New(time.Second), nil)
	entries, err := db.ListFeed(ctx, domain.FeedQuery{IncludeHidden: true})
	if err != nil {
		t.Fatal(err)
	}
	selected, ok := appEntryByID(entries, candidate.ID)
	if !ok {
		t.Fatalf("candidate %s missing from entries", candidate.ID)
	}

	diagnostics, err := service.RecommendationDiagnostics(ctx, selected)
	if err != nil {
		t.Fatal(err)
	}
	if !profileTermsContain(diagnostics.PositiveTerms, "tag", "ai") {
		t.Fatalf("positive terms = %+v, want tag ai", diagnostics.PositiveTerms)
	}
	if !profileTermsContain(diagnostics.NegativeTerms, "tag", "rust") {
		t.Fatalf("negative terms = %+v, want tag rust", diagnostics.NegativeTerms)
	}
	if diagnostics.Selected.ItemID != candidate.ID || !diagnostics.Selected.Eligible {
		t.Fatalf("selected diagnostics = %+v, want eligible candidate", diagnostics.Selected)
	}
	if diagnostics.Selected.InterestScore <= 0 || diagnostics.Selected.Score <= diagnostics.Selected.InterestScore {
		t.Fatalf("selected score breakdown = %+v, want positive interest and hot contribution", diagnostics.Selected)
	}
	if want := diagnostics.Selected.InterestScore + 0.25*diagnostics.Selected.HotScore; math.Abs(diagnostics.Selected.Score-want) > 0.000001 {
		t.Fatalf("selected score = %.6f, want formula %.6f from %+v", diagnostics.Selected.Score, want, diagnostics.Selected)
	}
	if !diagnostics.Selected.HasPositiveSignal {
		t.Fatalf("selected diagnostics = %+v, want positive-signal gate", diagnostics.Selected)
	}
	if !diagnosticTermsContain(diagnostics.Selected.MatchedTerms, "tag", "ai") {
		t.Fatalf("matched terms = %+v, want tag ai", diagnostics.Selected.MatchedTerms)
	}
	if !recommendationReasonsContain(diagnostics.Selected.Reasons, "opened ai") {
		t.Fatalf("reasons = %+v, want opened ai", diagnostics.Selected.Reasons)
	}
}

func TestRecommendationDiagnosticsReportsExclusionReasons(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()

	now := time.Now().UTC()
	seed := appTestSourceItem("gh-ai-seed", "repo:owner/ai-seed", "Opened AI seed", domain.SourceGitHub, 1, now)
	seed.Tags = []string{"ai"}
	hidden := appTestSourceItem("hn-ai-hidden", "hackernews:ai-hidden", "AI hidden", domain.SourceHackerNews, 1, now)
	hidden.Tags = []string{"ai"}
	hideRuleItem := appTestSourceItem("hn-ai-sponsored", "hackernews:ai-sponsored", "AI sponsored", domain.SourceHackerNews, 1, now)
	hideRuleItem.Tags = []string{"ai"}
	disabled := appTestSourceItem("gh-ai-disabled", "repo:owner/ai-disabled", "AI disabled", domain.SourceGitHub, 1, now)
	disabled.Tags = []string{"ai"}
	nonPositive := appTestSourceItem("hn-ai-negative", "hackernews:ai-negative", "AI negative", domain.SourceHackerNews, 1, now)
	nonPositive.Tags = []string{"ai"}
	cold := appTestSourceItem("hn-cold", "hackernews:cold", "Cold item", domain.SourceHackerNews, 1, now)
	if err := db.UpsertFeedItems(ctx, []domain.FeedItem{seed, hidden, hideRuleItem, disabled, nonPositive, cold}); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordItemEvent(ctx, domain.ItemEvent{ItemID: seed.ID, EventType: domain.ItemEventOpenURL, OccurredAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetHidden(ctx, hidden.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreatePersonalizationRule(ctx, domain.PersonalizationRule{
		Effect:  domain.RuleEffectHide,
		Target:  domain.RuleTargetKeyword,
		Value:   "sponsored",
		Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, httpx.New(time.Second), nil)
	service.SetSourceConfig([]domain.SourceID{domain.SourceHackerNews}, nil)
	entries, err := db.ListFeed(ctx, domain.FeedQuery{IncludeHidden: true})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		itemID string
		want   string
	}{
		{itemID: hidden.ID, want: string(domain.RecommendationExclusionHidden)},
		{itemID: hideRuleItem.ID, want: string(domain.RecommendationExclusionHideRule)},
		{itemID: disabled.ID, want: string(domain.RecommendationExclusionDisabledSource)},
		{itemID: nonPositive.ID, want: string(domain.RecommendationExclusionNonPositiveScore)},
		{itemID: cold.ID, want: string(domain.RecommendationExclusionNoPositiveSignal)},
	}
	for _, tt := range tests {
		entry, ok := appEntryByID(entries, tt.itemID)
		if !ok {
			t.Fatalf("entry %s missing", tt.itemID)
		}
		diagnostics, err := service.RecommendationDiagnostics(ctx, entry)
		if err != nil {
			t.Fatal(err)
		}
		if diagnostics.Selected.Eligible || diagnostics.Selected.ExclusionReason != tt.want {
			t.Fatalf("%s diagnostics = %+v, want exclusion %q", tt.itemID, diagnostics.Selected, tt.want)
		}
	}
}

func TestRecommendationDiagnosticsUsesProductionPositiveInterestGate(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()

	now := time.Now().UTC()
	oldEventAt := now.Add(-120 * 24 * time.Hour)
	seed := appTestSourceItem("hn-ai-seed", "hackernews:ai-seed", "AI seed", domain.SourceHackerNews, 1, oldEventAt)
	seed.Tags = []string{"ai"}
	candidate := appTestSourceItem("hn-ai-candidate", "hackernews:ai-candidate", "AI candidate", domain.SourceHackerNews, 1, now)
	candidate.Tags = []string{"ai"}
	if err := db.UpsertFeedItems(ctx, []domain.FeedItem{seed, candidate}); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordItemEvent(ctx, domain.ItemEvent{ItemID: seed.ID, EventType: domain.ItemEventRead, OccurredAt: oldEventAt}); err != nil {
		t.Fatal(err)
	}
	entries, err := db.ListFeed(ctx, domain.FeedQuery{})
	if err != nil {
		t.Fatal(err)
	}
	selected, ok := appEntryByID(entries, candidate.ID)
	if !ok {
		t.Fatalf("candidate %s missing from entries", candidate.ID)
	}
	selected.State.Read = true
	selected.Item.LastSeenAt = time.Time{}
	for idx := range selected.Sources {
		selected.Sources[idx].SourceRank = 0
	}
	service := NewService(db, httpx.New(time.Second), nil)

	diagnostics, err := service.RecommendationDiagnostics(ctx, selected)
	if err != nil {
		t.Fatal(err)
	}
	if !diagnostics.Selected.HasPositiveSignal || diagnostics.Selected.InterestScore <= 0 {
		t.Fatalf("selected diagnostics = %+v, want positive interest gate", diagnostics.Selected)
	}
	if diagnostics.Selected.Score >= 0 {
		t.Fatalf("selected diagnostics = %+v, want non-positive final score fixture", diagnostics.Selected)
	}
	if !diagnostics.Selected.Eligible {
		t.Fatalf("selected diagnostics = %+v, want production-gate eligible despite non-positive final score", diagnostics.Selected)
	}
}

func TestRecommendationDiagnosticsDoesNotReplaceRecommendationScores(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()

	now := time.Now().UTC()
	item := appTestSourceItem("hn-ai", "hackernews:ai", "AI candidate", domain.SourceHackerNews, 1, now)
	item.Tags = []string{"ai"}
	if err := db.UpsertFeedItems(ctx, []domain.FeedItem{item}); err != nil {
		t.Fatal(err)
	}
	if err := db.ReplaceRecommendationScores(ctx, []domain.RecommendationScore{{
		ItemID:        item.ID,
		Score:         9,
		InterestScore: 8,
		HotScore:      4,
		ComputedAt:    now,
	}}); err != nil {
		t.Fatal(err)
	}
	before, err := db.ListRecommendedFeed(ctx, domain.FeedQuery{})
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(db, httpx.New(time.Second), nil)

	if _, err := service.RecommendationDiagnostics(ctx, before[0]); err != nil {
		t.Fatal(err)
	}
	after, err := db.ListRecommendedFeed(ctx, domain.FeedQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) || after[0].Item.ID != before[0].Item.ID || after[0].HotScore != before[0].HotScore {
		t.Fatalf("recommendation scores changed: before=%+v after=%+v", before, after)
	}
}

func TestRecommendFeedRecomputesWhenMarkedDirty(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()

	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	hnSeed := appTestSourceItem("hn-seed", "hackernews:seed", "Saved HN seed", domain.SourceHackerNews, 1, now)
	githubOne := appTestSourceItem("gh-one", "repo:owner/one", "owner/one", domain.SourceGitHub, 1, now.Add(time.Minute))
	githubTwo := appTestSourceItem("gh-two", "repo:owner/two", "owner/two", domain.SourceGitHub, 2, now.Add(2*time.Minute))
	if err := db.UpsertFeedItems(ctx, []domain.FeedItem{hnSeed}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSaved(ctx, hnSeed.ID, true); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, httpx.New(time.Second), nil)
	loadRecommendFeed(t, service, ctx, FeedFilter{})

	if err := db.UpsertFeedItems(ctx, []domain.FeedItem{githubOne, githubTwo}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSaved(ctx, githubOne.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSaved(ctx, githubTwo.ID, true); err != nil {
		t.Fatal(err)
	}
	service.markRecommendationsDirty()

	snapshot := loadRecommendFeed(t, service, ctx, FeedFilter{})
	if !snapshotHasSource(snapshot, domain.SourceGitHub) {
		t.Fatalf("stale recommend scores did not refresh saved GitHub interest: ids=%+v", appEntryIDs(snapshot.Entries))
	}
}

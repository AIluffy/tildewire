package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zhangxueai/tildewire/internal/dedupe"
	"github.com/zhangxueai/tildewire/internal/domain"
	"github.com/zhangxueai/tildewire/internal/httpx"
	"github.com/zhangxueai/tildewire/internal/normalize"
	"github.com/zhangxueai/tildewire/internal/sources"
	"github.com/zhangxueai/tildewire/internal/store"
)

func TestRefreshPassesForceAndMarksStale(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()

	adapter := &fakeAdapter{
		result: domain.FetchResult{
			Source:    domain.SourceHackerNews,
			Scope:     domain.FetchScope{Source: domain.SourceHackerNews, View: "top"},
			FetchedAt: time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC),
			Stale:     true,
		},
		items: []domain.FeedItem{appTestItem("id-1", "hackernews:1", "Cached HN")},
	}
	service := NewService(db, httpx.New(time.Second), []SourceAdapter{adapter})

	snapshot, err := service.Refresh(ctx, domain.SourceAll, FeedFilter{}, RefreshOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if !adapter.sawForce {
		t.Fatal("expected force flag to reach adapter scope")
	}
	if len(snapshot.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(snapshot.Entries))
	}
	if got := findStatus(snapshot.Statuses, domain.SourceHackerNews); got != domain.SourceStatusStale {
		t.Fatalf("status = %s, want STALE", got)
	}
}

func TestRefreshMarksRateLimitedWhenUsingRateLimitedStaleCache(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()

	adapter := &fakeAdapter{
		result: domain.FetchResult{
			Source:      domain.SourceHackerNews,
			Scope:       domain.FetchScope{Source: domain.SourceHackerNews, View: "top"},
			FetchedAt:   time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC),
			Stale:       true,
			StaleReason: "rate_limited",
		},
		items: []domain.FeedItem{appTestItem("id-1", "hackernews:1", "Cached HN")},
	}
	service := NewService(db, httpx.New(time.Second), []SourceAdapter{adapter})

	snapshot, err := service.Refresh(ctx, domain.SourceAll, FeedFilter{}, RefreshOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := findStatus(snapshot.Statuses, domain.SourceHackerNews); got != domain.SourceStatusRateLimited {
		t.Fatalf("status = %s, want RATE_LIMITED", got)
	}
}

func TestRefreshErrorKeepsCachedFeed(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()
	if err := db.UpsertFeedItems(ctx, []domain.FeedItem{appTestItem("id-1", "hackernews:1", "Cached HN")}); err != nil {
		t.Fatal(err)
	}

	adapter := &fakeAdapter{fetchErr: errors.New("network down")}
	service := NewService(db, httpx.New(time.Second), []SourceAdapter{adapter})
	snapshot, err := service.Refresh(ctx, domain.SourceAll, FeedFilter{}, RefreshOptions{})
	if err == nil {
		t.Fatal("expected refresh error")
	}
	if len(snapshot.Entries) != 1 {
		t.Fatalf("cached feed was lost: %d", len(snapshot.Entries))
	}
	if got := findStatus(snapshot.Statuses, domain.SourceHackerNews); got != domain.SourceStatusNetworkError {
		t.Fatalf("status = %s, want NETWORK_ERROR", got)
	}
}

func TestRefreshClassifiesLocalLimiterWaitAsRateLimited(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()

	adapter := &fakeAdapter{
		source:   domain.SourceGitHub,
		fetchErr: errors.New("rate: Wait(n=1) would exceed context deadline"),
	}
	service := NewService(db, httpx.New(time.Second), []SourceAdapter{adapter})

	snapshot, err := service.Refresh(ctx, domain.SourceAll, FeedFilter{}, RefreshOptions{Mode: RefreshModeStartup})
	if err == nil {
		t.Fatal("expected refresh error")
	}
	if got := findStatus(snapshot.Statuses, domain.SourceGitHub); got != domain.SourceStatusRateLimited {
		t.Fatalf("github status = %s, want RATE_LIMITED", got)
	}
}

func TestRefreshNormalizeErrorKeepsCachedFeedAndMarksParserBroken(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()
	if err := db.UpsertFeedItems(ctx, []domain.FeedItem{appTestItem("id-1", "hackernews:1", "Cached HN")}); err != nil {
		t.Fatal(err)
	}

	adapter := &fakeAdapter{
		result: domain.FetchResult{
			Source:    domain.SourceHackerNews,
			Scope:     domain.FetchScope{Source: domain.SourceHackerNews, View: "top"},
			FetchedAt: time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC),
		},
		normalizeErr: errors.New("parser broke"),
	}
	service := NewService(db, httpx.New(time.Second), []SourceAdapter{adapter})

	snapshot, err := service.Refresh(ctx, domain.SourceAll, FeedFilter{}, RefreshOptions{})
	if err == nil {
		t.Fatal("expected refresh error")
	}
	if len(snapshot.Entries) != 1 {
		t.Fatalf("cached feed was lost: %d", len(snapshot.Entries))
	}
	if got := findStatus(snapshot.Statuses, domain.SourceHackerNews); got != domain.SourceStatusParserBroken {
		t.Fatalf("status = %s, want PARSER_BROKEN", got)
	}
}

func TestClearCacheRemovesCachedFeedAndReloadsSnapshot(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()

	saved := appTestItem("saved", "hackernews:saved", "Saved HN")
	unsaved := appTestItem("unsaved", "hackernews:unsaved", "Cached HN")
	if err := db.UpsertFeedItems(ctx, []domain.FeedItem{saved, unsaved}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSaved(ctx, saved.ID, true); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, httpx.New(time.Second), nil)

	snapshot, err := service.ClearCache(ctx, domain.SourceAll, FeedFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Entries) != 1 || snapshot.Entries[0].Item.ID != saved.ID || !snapshot.Entries[0].State.Saved {
		t.Fatalf("clear cache snapshot should keep only saved item: %+v", snapshot.Entries)
	}
	if snapshot.Counts[domain.SourceAll] != 1 {
		t.Fatalf("all count = %d, want 1", snapshot.Counts[domain.SourceAll])
	}
}

func TestRefreshSkipsProductHuntWhenAuthRequired(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()

	adapter := &fakeAdapter{
		source:       domain.SourceProductHunt,
		scopes:       []domain.FetchScope{{Source: domain.SourceProductHunt, View: "today"}},
		fetchErr:     errors.New("product hunt fetch should be skipped"),
		authRequired: true,
		authReason:   "PRODUCT_HUNT_TOKEN is not set",
	}
	service := NewService(db, httpx.New(time.Second), []SourceAdapter{adapter})

	snapshot, err := service.Refresh(ctx, domain.SourceAll, FeedFilter{}, RefreshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(adapter.calls) != 0 {
		t.Fatalf("product hunt fetch calls = %d, want 0", len(adapter.calls))
	}
	if got := findStatus(snapshot.Statuses, domain.SourceProductHunt); got != domain.SourceStatusAuthRequired {
		t.Fatalf("status = %s, want AUTH_REQUIRED", got)
	}
	health := findHealth(snapshot.Statuses, domain.SourceProductHunt)
	if !strings.Contains(health.LastError, "PRODUCT_HUNT_TOKEN") {
		t.Fatalf("last error = %q, want token guidance", health.LastError)
	}
}

func TestRefreshSkipsRealProductHuntAdapterWithoutToken(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()
	service := NewService(db, httpx.New(time.Second), []SourceAdapter{sources.NewProductHuntAdapter("")})

	snapshot, err := service.Refresh(ctx, domain.SourceAll, FeedFilter{}, RefreshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := findStatus(snapshot.Statuses, domain.SourceProductHunt); got != domain.SourceStatusAuthRequired {
		t.Fatalf("status = %s, want AUTH_REQUIRED", got)
	}
}

func TestRefreshFetchesProductHuntWhenAuthAvailable(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	adapter := &fakeAdapter{
		source: domain.SourceProductHunt,
		scopes: []domain.FetchScope{{Source: domain.SourceProductHunt, View: "today"}},
		items:  []domain.FeedItem{appTestSourceItem("ph-1", "producthunt:one", "Product One", domain.SourceProductHunt, 1, now)},
	}
	service := NewService(db, httpx.New(time.Second), []SourceAdapter{adapter})

	snapshot, err := service.Refresh(ctx, domain.SourceAll, FeedFilter{}, RefreshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(adapter.calls) != 1 {
		t.Fatalf("product hunt fetch calls = %d, want 1", len(adapter.calls))
	}
	if got := findStatus(snapshot.Statuses, domain.SourceProductHunt); got != domain.SourceStatusOK {
		t.Fatalf("status = %s, want OK", got)
	}
	if len(snapshot.Entries) != 1 || snapshot.Entries[0].Item.Title != "Product One" {
		t.Fatalf("entries = %+v, want product item", snapshot.Entries)
	}
}

func TestSourceConfigSkipsDisabledSourcesDuringRefresh(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()

	github := &fakeAdapter{
		source: domain.SourceGitHub,
		scopes: []domain.FetchScope{{Source: domain.SourceGitHub, View: "trending", Period: "daily"}},
		items:  []domain.FeedItem{appTestSourceItem("gh-1", "repo:owner/repo", "owner/repo", domain.SourceGitHub, 1, time.Now().UTC())},
	}
	hn := &fakeAdapter{
		source: domain.SourceHackerNews,
		scopes: []domain.FetchScope{{Source: domain.SourceHackerNews, View: "top"}},
		items:  []domain.FeedItem{appTestSourceItem("hn-1", "hackernews:1", "HN One", domain.SourceHackerNews, 1, time.Now().UTC())},
	}
	service := NewService(db, httpx.New(time.Second), []SourceAdapter{github, hn})
	service.SetSourceConfig([]domain.SourceID{domain.SourceHackerNews}, nil)

	snapshot, err := service.Refresh(ctx, domain.SourceAll, FeedFilter{}, RefreshOptions{Mode: RefreshModeAll})
	if err != nil {
		t.Fatal(err)
	}
	if len(github.calls) != 0 {
		t.Fatalf("disabled github calls = %d, want 0", len(github.calls))
	}
	if len(hn.calls) == 0 {
		t.Fatal("enabled hackernews should refresh")
	}
	if got := findStatus(snapshot.Statuses, domain.SourceGitHub); got != domain.SourceStatusUnknown {
		t.Fatalf("disabled github status = %s, want omitted", got)
	}
	if snapshotHasSource(snapshot, domain.SourceGitHub) {
		t.Fatalf("snapshot leaked disabled github source: %+v", snapshot.Entries)
	}
}

func TestSourceConfigFiltersDisabledSourcesFromCachedAllView(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	crossSource := appTestSourceItem("cross", "repo:owner/repo", "owner/repo", domain.SourceGitHub, 1, now)
	crossSource.Sources = append(crossSource.Sources, domain.ItemSource{
		ItemID:      crossSource.ID,
		Source:      domain.SourceHackerNews,
		SourceView:  "top",
		SourceIDRaw: "hn-cross",
		SourceRank:  2,
		SourceURL:   "https://news.ycombinator.com/item?id=42",
		SeenAt:      now,
	})
	githubOnly := appTestSourceItem("gh-only", "repo:owner/only", "owner/only", domain.SourceGitHub, 2, now)
	if err := db.UpsertFeedItems(ctx, []domain.FeedItem{crossSource, githubOnly}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateSourceStatus(ctx, domain.SourceGitHub, domain.SourceStatusOK, ""); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateSourceStatus(ctx, domain.SourceHackerNews, domain.SourceStatusOK, ""); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, httpx.New(time.Second), nil)
	service.SetSourceConfig([]domain.SourceID{domain.SourceHackerNews}, nil)

	snapshot, err := service.LoadFeed(ctx, domain.SourceAll, FeedFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Entries) != 1 {
		t.Fatalf("entries = %d, want only cross-source item", len(snapshot.Entries))
	}
	entry := snapshot.Entries[0]
	if len(entry.Sources) != 1 || entry.Sources[0].Source != domain.SourceHackerNews {
		t.Fatalf("sources were not stripped to enabled source: %+v", entry.Sources)
	}
	if snapshot.Counts[domain.SourceGitHub] != 0 || snapshot.Counts[domain.SourceAll] != snapshot.Counts[domain.SourceHackerNews] {
		t.Fatalf("counts leaked disabled source: %+v", snapshot.Counts)
	}
	if got := findStatus(snapshot.Statuses, domain.SourceGitHub); got != domain.SourceStatusUnknown {
		t.Fatalf("disabled github status = %s, want omitted", got)
	}
}

func TestSourceConfigUpdatesAdapterTokens(t *testing.T) {
	github := &fakeAdapter{source: domain.SourceGitHub}
	productHunt := &fakeAdapter{source: domain.SourceProductHunt}
	service := NewService(nil, nil, []SourceAdapter{github, productHunt})

	service.SetSourceConfig(nil, map[domain.SourceID]string{
		domain.SourceGitHub:      "gh-token",
		domain.SourceProductHunt: "ph-token",
	})

	if github.token != "gh-token" || productHunt.token != "ph-token" {
		t.Fatalf("tokens not applied: github=%q producthunt=%q", github.token, productHunt.token)
	}
}

func TestRefreshRecordsFetchHistoryForOutcomes(t *testing.T) {
	tests := []struct {
		name       string
		adapter    *fakeAdapter
		wantStatus domain.SourceStatus
		wantStale  bool
		wantItems  int
		wantErr    bool
	}{
		{
			name: "success",
			adapter: &fakeAdapter{
				items: []domain.FeedItem{appTestItem("id-success", "hackernews:success", "Success")},
			},
			wantStatus: domain.SourceStatusOK,
			wantItems:  1,
		},
		{
			name: "stale",
			adapter: &fakeAdapter{
				result: domain.FetchResult{Stale: true, StaleReason: "network_error"},
				items:  []domain.FeedItem{appTestItem("id-stale", "hackernews:stale", "Stale")},
			},
			wantStatus: domain.SourceStatusNetworkError,
			wantStale:  true,
			wantItems:  1,
		},
		{
			name: "auth required",
			adapter: &fakeAdapter{
				source:       domain.SourceProductHunt,
				scopes:       []domain.FetchScope{{Source: domain.SourceProductHunt, View: "today"}},
				authRequired: true,
				authReason:   "token missing",
			},
			wantStatus: domain.SourceStatusAuthRequired,
		},
		{
			name:       "fetch error",
			adapter:    &fakeAdapter{fetchErr: errors.New("network down")},
			wantStatus: domain.SourceStatusNetworkError,
			wantErr:    true,
		},
		{
			name: "normalize error",
			adapter: &fakeAdapter{
				normalizeErr: errors.New("bad json"),
			},
			wantStatus: domain.SourceStatusParserBroken,
			wantErr:    true,
		},
		{
			name: "upsert error",
			adapter: &fakeAdapter{
				items: []domain.FeedItem{{
					ID:           "bad",
					CanonicalKey: "hackernews:bad",
					ItemType:     "story",
				}},
			},
			wantStatus: domain.SourceStatusStale,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			db := openAppTestStore(t)
			defer db.Close()
			service := NewService(db, httpx.New(time.Second), []SourceAdapter{tt.adapter})

			snapshot, err := service.Refresh(ctx, domain.SourceAll, FeedFilter{}, RefreshOptions{})
			if tt.wantErr && err == nil {
				t.Fatal("expected refresh error")
			}
			if !tt.wantErr && err != nil {
				t.Fatal(err)
			}
			if len(snapshot.FetchHistory) == 0 {
				t.Fatalf("snapshot fetch history is empty")
			}
			event := snapshot.FetchHistory[0]
			if event.Status != tt.wantStatus || event.Stale != tt.wantStale || event.ItemCount != tt.wantItems {
				t.Fatalf("fetch event = %+v, want status=%s stale=%v items=%d", event, tt.wantStatus, tt.wantStale, tt.wantItems)
			}
			if event.StartedAt.IsZero() || event.FinishedAt.IsZero() {
				t.Fatalf("fetch event should include timing: %+v", event)
			}
		})
	}
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

func TestUpdatePersonalizationRuleReloadsSnapshot(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()

	rule, err := db.CreatePersonalizationRule(ctx, domain.PersonalizationRule{
		Effect:  domain.RuleEffectBoost,
		Target:  domain.RuleTargetKeyword,
		Value:   "sqlite",
		Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	item := appTestSourceItem("hn-sqlite", "hackernews:sqlite", "SQLite FTS", domain.SourceHackerNews, 1, time.Now().UTC())
	if err := db.UpsertFeedItems(ctx, []domain.FeedItem{item}); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, httpx.New(time.Second), nil)

	snapshot, err := service.UpdatePersonalizationRule(ctx, rule.ID, domain.PersonalizationRule{
		Effect:  domain.RuleEffectHide,
		Target:  domain.RuleTargetTag,
		Value:   "Noise",
		Enabled: false,
	}, domain.SourceAll, FeedFilter{Search: "sqlite"})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Filter.Search != "sqlite" {
		t.Fatalf("snapshot filter = %+v, want search reload", snapshot.Filter)
	}
	if len(snapshot.Rules) != 1 || snapshot.Rules[0].ID != rule.ID || snapshot.Rules[0].Effect != domain.RuleEffectHide || snapshot.Rules[0].Target != domain.RuleTargetTag || snapshot.Rules[0].Value != "noise" || snapshot.Rules[0].Enabled {
		t.Fatalf("snapshot rules = %+v", snapshot.Rules)
	}
}

func TestRefreshCanonicalizesFetchedItemsBeforeUpsert(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)

	item := appTestSourceItem("hn-42", "hackernews:42", "GitHub repo on HN", domain.SourceHackerNews, 1, now)
	item.URL = "https://github.com/CharmBracelet/BubbleTea?utm_source=hn"
	item.CanonicalURL = normalize.CanonicalURL(item.URL)
	item.Refs = domain.Refs{HNID: "42"}
	adapter := &fakeAdapter{
		result: domain.FetchResult{
			Source:    domain.SourceHackerNews,
			Scope:     domain.FetchScope{Source: domain.SourceHackerNews, View: "top"},
			FetchedAt: now,
		},
		items: []domain.FeedItem{item},
	}
	service := NewService(db, httpx.New(time.Second), []SourceAdapter{adapter})

	snapshot, err := service.Refresh(ctx, domain.SourceAll, FeedFilter{}, RefreshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(snapshot.Entries))
	}
	got := snapshot.Entries[0]
	wantKey := "repo:charmbracelet/bubbletea"
	if got.Item.CanonicalKey != wantKey || got.Item.ID != normalize.StableID(wantKey) {
		t.Fatalf("item was not canonicalized: %+v", got.Item)
	}
	if got.Item.Refs.Repo != "charmbracelet/bubbletea" {
		t.Fatalf("repo ref = %q", got.Item.Refs.Repo)
	}
	if len(got.Sources) != 1 || got.Sources[0].ItemID != got.Item.ID {
		t.Fatalf("source context not rewritten to canonical item id: %+v", got.Sources)
	}
}

func TestRefreshStartupUsesPrimaryScopesOnly(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()

	github := &fakeAdapter{
		source: domain.SourceGitHub,
		scopes: []domain.FetchScope{
			{Source: domain.SourceGitHub, View: "trending", Period: "daily"},
			{Source: domain.SourceGitHub, View: "trending", Period: "weekly"},
			{Source: domain.SourceGitHub, View: "trending", Period: "daily", Language: "go"},
		},
	}
	hn := &fakeAdapter{
		source: domain.SourceHackerNews,
		scopes: []domain.FetchScope{
			{Source: domain.SourceHackerNews, View: "top"},
			{Source: domain.SourceHackerNews, View: "best"},
		},
	}
	hf := &fakeAdapter{
		source: domain.SourceHuggingFace,
		scopes: []domain.FetchScope{
			{Source: domain.SourceHuggingFace, View: "daily"},
		},
	}
	service := NewService(db, httpx.New(time.Second), []SourceAdapter{github, hn, hf})

	if _, err := service.Refresh(ctx, domain.SourceAll, FeedFilter{}, RefreshOptions{Mode: RefreshModeStartup}); err != nil {
		t.Fatal(err)
	}

	assertScopeViews(t, github.calls, []string{"trending:daily"})
	assertScopeViews(t, hn.calls, []string{"top"})
	assertScopeViews(t, hf.calls, []string{"daily"})
}

func TestRefreshVisibleIncludesCurrentSourceViewWithoutDuplicate(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()

	github := &fakeAdapter{
		source: domain.SourceGitHub,
		scopes: []domain.FetchScope{
			{Source: domain.SourceGitHub, View: "trending", Period: "daily"},
			{Source: domain.SourceGitHub, View: "trending", Period: "weekly"},
			{Source: domain.SourceGitHub, View: "trending", Period: "daily", Language: "rust"},
			{Source: domain.SourceGitHub, View: "trending", Period: "daily", Language: "python"},
		},
	}
	hn := &fakeAdapter{source: domain.SourceHackerNews, scopes: []domain.FetchScope{{Source: domain.SourceHackerNews, View: "top"}}}
	hf := &fakeAdapter{source: domain.SourceHuggingFace, scopes: []domain.FetchScope{{Source: domain.SourceHuggingFace, View: "daily"}}}
	service := NewService(db, httpx.New(time.Second), []SourceAdapter{hn, hf, github})

	_, err := service.Refresh(ctx, domain.SourceGitHub, FeedFilter{SourceView: "trending:daily:rust"}, RefreshOptions{Mode: RefreshModeVisible})
	if err != nil {
		t.Fatal(err)
	}

	assertScopeViews(t, github.calls, []string{"trending:daily", "trending:daily:rust"})
	assertScopeViews(t, hn.calls, []string{})
	assertScopeViews(t, hf.calls, []string{})

	github.calls = nil
	_, err = service.Refresh(ctx, domain.SourceGitHub, FeedFilter{SourceView: "trending:daily"}, RefreshOptions{Mode: RefreshModeVisible})
	if err != nil {
		t.Fatal(err)
	}
	assertScopeViews(t, github.calls, []string{"trending:daily"})

	github.calls = nil
	_, err = service.Refresh(ctx, domain.SourceGitHub, FeedFilter{SourceView: "trending:monthly:c++:spoken:zh"}, RefreshOptions{Mode: RefreshModeVisible})
	if err != nil {
		t.Fatal(err)
	}
	assertScopeViews(t, github.calls, []string{"trending:daily", "trending:monthly:c++:spoken:zh"})
}

func TestRefreshVisibleAllViewRefreshesPrimaryScopes(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()

	github := &fakeAdapter{
		source: domain.SourceGitHub,
		scopes: []domain.FetchScope{
			{Source: domain.SourceGitHub, View: "trending", Period: "daily"},
			{Source: domain.SourceGitHub, View: "trending", Period: "weekly"},
		},
	}
	hn := &fakeAdapter{source: domain.SourceHackerNews, scopes: []domain.FetchScope{{Source: domain.SourceHackerNews, View: "top"}}}
	hf := &fakeAdapter{source: domain.SourceHuggingFace, scopes: []domain.FetchScope{{Source: domain.SourceHuggingFace, View: "daily"}}}
	service := NewService(db, httpx.New(time.Second), []SourceAdapter{hn, hf, github})

	if _, err := service.Refresh(ctx, domain.SourceAll, FeedFilter{}, RefreshOptions{Mode: RefreshModeVisible}); err != nil {
		t.Fatal(err)
	}

	assertScopeViews(t, github.calls, []string{"trending:daily"})
	assertScopeViews(t, hn.calls, []string{"top"})
	assertScopeViews(t, hf.calls, []string{"daily"})
}

func TestRefreshStartsEnabledAdaptersInBatch(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()

	started := make(chan domain.SourceID, 2)
	releaseFetch := make(chan struct{})
	github := &fakeAdapter{
		source:       domain.SourceGitHub,
		scopes:       []domain.FetchScope{{Source: domain.SourceGitHub, View: "trending", Period: "daily"}},
		startSignal:  started,
		releaseFetch: releaseFetch,
	}
	hn := &fakeAdapter{
		source:       domain.SourceHackerNews,
		scopes:       []domain.FetchScope{{Source: domain.SourceHackerNews, View: "top"}},
		items:        []domain.FeedItem{appTestSourceItem("hn-1", "hackernews:1", "HN", domain.SourceHackerNews, 1, time.Now())},
		startSignal:  started,
		releaseFetch: releaseFetch,
	}
	service := NewService(db, httpx.New(time.Second), []SourceAdapter{github, hn})

	errCh := make(chan error, 1)
	go func() {
		_, err := service.Refresh(ctx, domain.SourceAll, FeedFilter{}, RefreshOptions{Mode: RefreshModeStartup})
		errCh <- err
	}()

	first := <-started
	select {
	case got := <-started:
		if got == first {
			t.Fatalf("both started signals came from %s", got)
		}
	case <-time.After(100 * time.Millisecond):
		close(releaseFetch)
		t.Fatal("second adapter did not start while first adapter was still fetching")
	}

	close(releaseFetch)
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}

func TestRefreshZeroModeDefaultsToVisibleScopes(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()

	github := &fakeAdapter{
		source: domain.SourceGitHub,
		scopes: []domain.FetchScope{
			{Source: domain.SourceGitHub, View: "trending", Period: "daily"},
			{Source: domain.SourceGitHub, View: "trending", Period: "weekly"},
			{Source: domain.SourceGitHub, View: "trending", Period: "daily", Language: "rust"},
		},
	}
	hn := &fakeAdapter{
		source: domain.SourceHackerNews,
		scopes: []domain.FetchScope{
			{Source: domain.SourceHackerNews, View: "top"},
			{Source: domain.SourceHackerNews, View: "best"},
		},
	}
	hf := &fakeAdapter{source: domain.SourceHuggingFace, scopes: []domain.FetchScope{{Source: domain.SourceHuggingFace, View: "daily"}}}
	service := NewService(db, httpx.New(time.Second), []SourceAdapter{github, hn, hf})

	_, err := service.Refresh(ctx, domain.SourceGitHub, FeedFilter{SourceView: "trending:daily:rust"}, RefreshOptions{})
	if err != nil {
		t.Fatal(err)
	}

	assertScopeViews(t, github.calls, []string{"trending:daily", "trending:daily:rust"})
	assertScopeViews(t, hn.calls, []string{})
	assertScopeViews(t, hf.calls, []string{})
}

func TestRefreshNetworkStaleStatusKeepsFetchedItems(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)

	adapter := &fakeAdapter{
		result: domain.FetchResult{
			Source:      domain.SourceHackerNews,
			Scope:       domain.FetchScope{Source: domain.SourceHackerNews, View: "top"},
			FetchedAt:   now,
			Stale:       true,
			StaleReason: httpx.StaleReasonNetworkError,
		},
		items: []domain.FeedItem{appTestSourceItem("hn-1", "hackernews:1", "HN partial", domain.SourceHackerNews, 1, now)},
	}
	service := NewService(db, httpx.New(time.Second), []SourceAdapter{adapter})

	snapshot, err := service.Refresh(ctx, domain.SourceAll, FeedFilter{}, RefreshOptions{Mode: RefreshModeStartup})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(snapshot.Entries))
	}
	if got := findStatus(snapshot.Statuses, domain.SourceHackerNews); got != domain.SourceStatusNetworkError {
		t.Fatalf("status = %s, want NETWORK_ERROR", got)
	}
}

func TestRefreshLoadsCachedSnapshotAfterRefreshContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	db := openAppTestStore(t)
	defer db.Close()
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	cached := appTestSourceItem("lob-1", "lobsters:abc123", "Cached Lobsters", domain.SourceLobsters, 1, now)
	cached.Sources[0].SourceView = "hottest"
	if err := db.UpsertFeedItems(context.Background(), []domain.FeedItem{cached}); err != nil {
		t.Fatal(err)
	}

	adapter := &fakeAdapter{
		source:        domain.SourceLobsters,
		scopes:        []domain.FetchScope{{Source: domain.SourceLobsters, View: "hottest"}},
		fetchErr:      context.DeadlineExceeded,
		cancelOnFetch: cancel,
	}
	service := NewService(db, httpx.New(time.Second), []SourceAdapter{adapter})

	snapshot, err := service.Refresh(ctx, domain.SourceLobsters, FeedFilter{SourceView: "hottest"}, RefreshOptions{Mode: RefreshModeVisible})
	if err == nil {
		t.Fatal("expected refresh error")
	}
	if len(snapshot.Entries) != 1 || snapshot.Entries[0].Item.Title != "Cached Lobsters" {
		t.Fatalf("cached snapshot was not loaded after context cancellation: %+v", snapshot.Entries)
	}
	if snapshot.View != domain.SourceLobsters || snapshot.Filter.SourceView != "hottest" {
		t.Fatalf("snapshot view/filter mismatch: view=%s filter=%+v", snapshot.View, snapshot.Filter)
	}
}

func TestRefreshContinuesAfterFailureWhenAdaptersRunInBatch(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()

	github := &fakeAdapter{source: domain.SourceGitHub, fetchErr: errors.New("github down")}
	hn := &fakeAdapter{
		source: domain.SourceHackerNews,
		items:  []domain.FeedItem{appTestSourceItem("hn-1", "hackernews:1", "HN", domain.SourceHackerNews, 1, time.Now())},
	}
	hf := &fakeAdapter{
		source: domain.SourceHuggingFace,
		items:  []domain.FeedItem{appTestSourceItem("hf-1", "paper:1", "HF", domain.SourceHuggingFace, 1, time.Now())},
	}
	service := NewService(db, httpx.New(time.Second), []SourceAdapter{hn, hf, github})

	snapshot, err := service.Refresh(ctx, domain.SourceAll, FeedFilter{}, RefreshOptions{Mode: RefreshModeStartup})
	if err == nil {
		t.Fatal("expected refresh error from github")
	}
	assertScopeViews(t, github.calls, []string{"trending:daily"})
	assertScopeViews(t, hn.calls, []string{"top"})
	assertScopeViews(t, hf.calls, []string{"daily"})
	if len(snapshot.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(snapshot.Entries))
	}
	if got := findStatus(snapshot.Statuses, domain.SourceGitHub); got != domain.SourceStatusNetworkError {
		t.Fatalf("github status = %s, want NETWORK_ERROR", got)
	}
}

func TestLoadFeedIncludesGitHubAndSourceCounts(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	githubWeekly := appTestSourceItem("gh-weekly", "repo:owner/weekly", "owner/weekly", domain.SourceGitHub, 1, now.Add(3*time.Minute))
	githubWeekly.Sources[0].SourceView = "trending:weekly"
	hnBest := appTestSourceItem("hn-best", "hackernews:best", "Best HN", domain.SourceHackerNews, 1, now.Add(4*time.Minute))
	hnBest.Sources[0].SourceView = "best"
	items := []domain.FeedItem{
		appTestSourceItem("gh-2", "repo:owner/two", "owner/two", domain.SourceGitHub, 2, now.Add(2*time.Minute)),
		appTestSourceItem("hn-1", "hackernews:1", "Cached HN", domain.SourceHackerNews, 1, now.Add(time.Minute)),
		appTestSourceItem("gh-1", "repo:owner/one", "owner/one", domain.SourceGitHub, 1, now),
		githubWeekly,
		hnBest,
	}
	if err := db.UpsertFeedItems(ctx, items); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, httpx.New(time.Second), nil)
	all, err := service.LoadFeed(ctx, domain.SourceAll, FeedFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Entries) != 5 || !snapshotHasSource(all, domain.SourceGitHub) || !snapshotHasSource(all, domain.SourceHackerNews) {
		t.Fatalf("all feed did not merge sources: %+v", all.Entries)
	}
	if all.Counts[domain.SourceAll] != 3 || all.Counts[domain.SourceGitHub] != 2 || all.Counts[domain.SourceHackerNews] != 1 {
		t.Fatalf("counts mismatch: %+v", all.Counts)
	}
	if all.Counts[domain.SourceAll] != all.Counts[domain.SourceGitHub]+all.Counts[domain.SourceHackerNews]+all.Counts[domain.SourceHuggingFace] {
		t.Fatalf("all count should equal source tab counts: %+v", all.Counts)
	}

	github, err := service.LoadFeed(ctx, domain.SourceGitHub, FeedFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(github.Entries) != 2 || github.Entries[0].Item.Title != "owner/one" || github.Entries[1].Item.Title != "owner/two" {
		t.Fatalf("github view not source-rank sorted: %+v", github.Entries)
	}

	githubWeeklySnapshot, err := service.LoadFeed(ctx, domain.SourceGitHub, FeedFilter{SourceView: "trending:weekly"})
	if err != nil {
		t.Fatal(err)
	}
	if githubWeeklySnapshot.Counts[domain.SourceGitHub] != 1 {
		t.Fatalf("weekly GitHub count = %d, want 1", githubWeeklySnapshot.Counts[domain.SourceGitHub])
	}
	if githubWeeklySnapshot.Counts[domain.SourceAll] != githubWeeklySnapshot.Counts[domain.SourceGitHub]+githubWeeklySnapshot.Counts[domain.SourceHackerNews]+githubWeeklySnapshot.Counts[domain.SourceHuggingFace] {
		t.Fatalf("all count should track active source scope count: %+v", githubWeeklySnapshot.Counts)
	}
}

func TestLoadFeedAppliesSearchAndStateFilters(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	items := []domain.FeedItem{
		appTestSourceItem("gh-1", "repo:owner/agent", "owner/agent", domain.SourceGitHub, 1, now),
		appTestSourceItem("gh-2", "repo:owner/cli", "owner/cli", domain.SourceGitHub, 2, now),
		appTestSourceItem("hn-1", "hackernews:1", "SQLite discussion", domain.SourceHackerNews, 1, now),
	}
	items[0].Summary = "Terminal agent workflow"
	items[0].Language = "Go"
	items[0].Tags = []string{"agents", "terminal"}
	items[1].Summary = "Command-line utilities"
	items[1].Language = "Rust"
	items[1].Tags = []string{"cli"}
	if err := db.UpsertFeedItems(ctx, items); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSaved(ctx, items[0].ID, true); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSaved(ctx, items[1].ID, true); err != nil {
		t.Fatal(err)
	}
	if err := db.SetRead(ctx, items[1].ID, true); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, httpx.New(time.Second), nil)

	savedAgent, err := service.LoadFeed(ctx, domain.SourceAll, FeedFilter{Search: "agent", SavedOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(savedAgent.Entries) != 1 || savedAgent.Entries[0].Item.Title != "owner/agent" {
		t.Fatalf("saved text filter mismatch: %+v", savedAgent.Entries)
	}

	unreadGoTag, err := service.LoadFeed(ctx, domain.SourceAll, FeedFilter{UnreadOnly: true, Language: "go", Tag: "terminal"})
	if err != nil {
		t.Fatal(err)
	}
	if len(unreadGoTag.Entries) != 1 || unreadGoTag.Entries[0].Item.Title != "owner/agent" {
		t.Fatalf("unread language tag filter mismatch: %+v", unreadGoTag.Entries)
	}

	savedUnread, err := service.LoadFeed(ctx, domain.SourceAll, FeedFilter{SavedOnly: true, UnreadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(savedUnread.Entries) != 1 || savedUnread.Entries[0].Item.Title != "owner/agent" {
		t.Fatalf("saved unread filter mismatch: %+v", savedUnread.Entries)
	}
}

func TestLoadFeedFiltersSingleSourceBySourceView(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	top := appTestSourceItem("hn-top", "hackernews:top", "Top story", domain.SourceHackerNews, 1, now)
	top.Sources[0].SourceView = "top"
	best := appTestSourceItem("hn-best", "hackernews:best", "Best story", domain.SourceHackerNews, 1, now)
	best.Sources[0].SourceView = "best"
	if err := db.UpsertFeedItems(ctx, []domain.FeedItem{top, best}); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, httpx.New(time.Second), nil)

	snapshot, err := service.LoadFeed(ctx, domain.SourceHackerNews, FeedFilter{SourceView: "best"})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Entries) != 1 || snapshot.Entries[0].Item.Title != "Best story" {
		t.Fatalf("source view filter mismatch: %+v", snapshot.Entries)
	}
	if snapshot.Filter.SourceView != "best" {
		t.Fatalf("snapshot source view = %q, want best", snapshot.Filter.SourceView)
	}
}

func TestLoadFeedSingleSourceKeepsSourceNativeContext(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		source     domain.SourceID
		sourceView string
		sourceID   string
		sourceURL  string
	}{
		{
			name:       "hackernews",
			source:     domain.SourceHackerNews,
			sourceView: "top",
			sourceID:   "42",
			sourceURL:  "https://news.ycombinator.com/item?id=42",
		},
		{
			name:       "lobsters",
			source:     domain.SourceLobsters,
			sourceView: "hottest",
			sourceID:   "abc123",
			sourceURL:  "https://lobste.rs/s/abc123/story",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openAppTestStore(t)
			defer db.Close()

			item := appTestSourceItem("repo-1", "repo:owner/repo", "owner/repo", domain.SourceGitHub, 1, now)
			item.Sources = append(item.Sources, domain.ItemSource{
				ItemID:      item.ID,
				Source:      tt.source,
				SourceView:  tt.sourceView,
				SourceIDRaw: tt.sourceID,
				SourceRank:  2,
				SourceURL:   tt.sourceURL,
				SeenAt:      now,
			})
			if err := db.UpsertFeedItems(ctx, []domain.FeedItem{item}); err != nil {
				t.Fatal(err)
			}
			service := NewService(db, httpx.New(time.Second), nil)

			snapshot, err := service.LoadFeed(ctx, tt.source, FeedFilter{SourceView: tt.sourceView})
			if err != nil {
				t.Fatal(err)
			}
			if len(snapshot.Entries) != 1 {
				t.Fatalf("entries = %d, want 1", len(snapshot.Entries))
			}
			entry := snapshot.Entries[0]
			if got := entry.PrimarySource().Source; got != tt.source {
				t.Fatalf("primary source = %s, want %s; sources=%+v", got, tt.source, entry.Sources)
			}
			if len(entry.Sources) != 1 || entry.Sources[0].Source != tt.source || entry.Sources[0].SourceView != tt.sourceView {
				t.Fatalf("single-source feed leaked other source context: %+v", entry.Sources)
			}
		})
	}
}

func TestLoadFeedCanIncludeHiddenItems(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	item := appTestSourceItem("hn-hidden", "hackernews:hidden", "SQLite hidden discussion", domain.SourceHackerNews, 1, now)
	item.Summary = "hidden sqlite cache"
	if err := db.UpsertFeedItems(ctx, []domain.FeedItem{item}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetHidden(ctx, item.ID, true); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, httpx.New(time.Second), nil)

	withoutHidden, err := service.LoadFeed(ctx, domain.SourceAll, FeedFilter{Search: "sqlite"})
	if err != nil {
		t.Fatal(err)
	}
	if len(withoutHidden.Entries) != 0 {
		t.Fatalf("hidden item should be excluded by default: %+v", withoutHidden.Entries)
	}

	withHidden, err := service.LoadFeed(ctx, domain.SourceAll, FeedFilter{Search: "sqlite", IncludeHidden: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(withHidden.Entries) != 1 || !withHidden.Entries[0].State.Hidden {
		t.Fatalf("hidden item was not included: %+v", withHidden.Entries)
	}
	if !withHidden.Filter.IncludeHidden {
		t.Fatalf("snapshot filter did not preserve IncludeHidden: %+v", withHidden.Filter)
	}
}

func TestLoadDetailReturnsProviderContentAndBaseDetailOnProviderFailure(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	entry := domain.FeedEntry{
		Item: appTestSourceItem("repo-1", "repo:owner/repo", "owner/repo", domain.SourceGitHub, 1, now),
		Sources: []domain.ItemSource{
			{Source: domain.SourceGitHub, SourceRank: 1},
			{Source: domain.SourceHackerNews, SourceIDRaw: "42", SourceRank: 2},
		},
	}
	github := &fakeAdapter{
		source: domain.SourceGitHub,
		detail: domain.ItemDetail{
			ItemID: entry.Item.ID,
			Sections: []domain.DetailSection{{
				Title: "GitHub README",
				Body:  "README preview",
			}},
		},
	}
	hn := &fakeAdapter{source: domain.SourceHackerNews, detailErr: errors.New("hn down")}
	service := NewService(db, httpx.New(time.Second), []SourceAdapter{github, hn})

	detail, err := service.LoadDetail(ctx, entry)
	if err == nil {
		t.Fatal("expected provider error to be returned with partial detail")
	}
	if detail.ItemID != entry.Item.ID || detail.Title != "owner/repo" {
		t.Fatalf("base detail missing: %+v", detail)
	}
	if len(detail.Sections) != 1 || detail.Sections[0].Body != "README preview" {
		t.Fatalf("provider section missing: %+v", detail.Sections)
	}
	if github.detailCalls != 1 || hn.detailCalls != 1 {
		t.Fatalf("detail provider calls github=%d hn=%d", github.detailCalls, hn.detailCalls)
	}
}

func TestExportSavedWritesAllFormatsAndIncludesHiddenSavedItems(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	visible := appTestSourceItem("hn-1", "hackernews:1", "Visible saved", domain.SourceHackerNews, 1, now)
	hidden := appTestSourceItem("hn-2", "hackernews:2", "Hidden saved", domain.SourceHackerNews, 2, now)
	if err := db.UpsertFeedItems(ctx, []domain.FeedItem{visible, hidden}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSaved(ctx, visible.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSaved(ctx, hidden.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := db.SetHidden(ctx, hidden.ID, true); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, httpx.New(time.Second), nil)

	for _, format := range []ExportFormat{ExportMarkdown, ExportJSON, ExportCSV} {
		t.Run(string(format), func(t *testing.T) {
			result, err := service.ExportSaved(ctx, ExportOptions{
				Format: format,
				Dir:    t.TempDir(),
				Now:    time.Date(2026, 5, 10, 12, 30, 0, 0, time.UTC),
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Count != 2 {
				t.Fatalf("count = %d, want 2", result.Count)
			}
			data, err := os.ReadFile(result.Path)
			if err != nil {
				t.Fatal(err)
			}
			text := string(data)
			if !strings.Contains(text, "Visible saved") || !strings.Contains(text, "Hidden saved") {
				t.Fatalf("export missing saved items:\n%s", text)
			}
		})
	}
}

type fakeAdapter struct {
	result        domain.FetchResult
	items         []domain.FeedItem
	fetchErr      error
	normalizeErr  error
	sawForce      bool
	source        domain.SourceID
	scopes        []domain.FetchScope
	calls         []domain.FetchScope
	detail        domain.ItemDetail
	detailErr     error
	detailCalls   int
	cancelOnFetch func()
	authRequired  bool
	authReason    string
	token         string
	startSignal   chan<- domain.SourceID
	releaseFetch  <-chan struct{}
}

func (f *fakeAdapter) Source() domain.SourceID {
	if f.source == "" {
		return domain.SourceHackerNews
	}
	return f.source
}

func (f *fakeAdapter) DefaultScopes() []domain.FetchScope {
	if len(f.scopes) > 0 {
		return f.scopes
	}
	return []domain.FetchScope{{Source: domain.SourceHackerNews, View: "top", Limit: 10}}
}

func (f *fakeAdapter) Fetch(ctx context.Context, scope domain.FetchScope, _ *httpx.Client) (*domain.FetchResult, error) {
	f.sawForce = scope.ForceRefresh
	f.calls = append(f.calls, scope)
	if f.startSignal != nil {
		f.startSignal <- f.Source()
	}
	if f.releaseFetch != nil {
		select {
		case <-f.releaseFetch:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if f.cancelOnFetch != nil {
		f.cancelOnFetch()
	}
	if f.fetchErr != nil {
		return nil, f.fetchErr
	}
	f.result.Source = f.Source()
	f.result.Scope = scope
	if f.result.FetchedAt.IsZero() {
		f.result.FetchedAt = time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	}
	return &f.result, nil
}

func (f *fakeAdapter) Normalize(context.Context, domain.FetchScope, *domain.FetchResult) ([]domain.FeedItem, error) {
	if f.normalizeErr != nil {
		return nil, f.normalizeErr
	}
	return f.items, nil
}

func (f *fakeAdapter) CachePolicy(domain.FetchScope) domain.CachePolicy {
	return domain.CachePolicy{TTL: time.Minute}
}

func (f *fakeAdapter) AuthRequired() (bool, string) {
	return f.authRequired, f.authReason
}

func (f *fakeAdapter) SetToken(token string) {
	f.token = token
}

func (f *fakeAdapter) Detail(context.Context, domain.FeedEntry, *httpx.Client) (domain.ItemDetail, error) {
	f.detailCalls++
	return f.detail, f.detailErr
}

func openAppTestStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "tildewire.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return db
}

func appTestItem(id, key, title string) domain.FeedItem {
	return appTestSourceItem(id, key, title, domain.SourceHackerNews, 1, time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC))
}

func appTestSourceItem(id, key, title string, source domain.SourceID, rank int, seenAt time.Time) domain.FeedItem {
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
		LastSeenAt:   seenAt,
		Sources: []domain.ItemSource{{
			ItemID:      id,
			Source:      source,
			SourceView:  sourceView,
			SourceIDRaw: id,
			SourceRank:  rank,
			SourceURL:   sourceURL,
			SeenAt:      seenAt,
		}},
	}
	return dedupe.CanonicalizeItem(item)
}

func findStatus(statuses []domain.SourceHealth, source domain.SourceID) domain.SourceStatus {
	return findHealth(statuses, source).Status
}

func findHealth(statuses []domain.SourceHealth, source domain.SourceID) domain.SourceHealth {
	for _, status := range statuses {
		if status.Source == source {
			return status
		}
	}
	return domain.SourceHealth{Source: source, Status: domain.SourceStatusUnknown}
}

func snapshotHasSource(snapshot Snapshot, source domain.SourceID) bool {
	for _, entry := range snapshot.Entries {
		for _, itemSource := range entry.Sources {
			if itemSource.Source == source {
				return true
			}
		}
	}
	return false
}

func assertScopeViews(t *testing.T, scopes []domain.FetchScope, want []string) {
	t.Helper()
	got := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		got = append(got, sourceViewKey(scope))
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scope views = %+v, want %+v", got, want)
	}
}

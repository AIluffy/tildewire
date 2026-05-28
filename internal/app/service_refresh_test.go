package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AIluffy/tildewire/internal/domain"
	"github.com/AIluffy/tildewire/internal/httpx"
	"github.com/AIluffy/tildewire/internal/normalize"
	"github.com/AIluffy/tildewire/internal/sources"
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

func TestServiceRuntimeConfigConcurrentAccess(t *testing.T) {
	github := &fakeAdapter{source: domain.SourceGitHub}
	hn := &fakeAdapter{source: domain.SourceHackerNews}
	service := newRuntimeConfigTestService([]SourceAdapter{github, hn})

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if (idx+j)%2 == 0 {
					service.SetSourceConfig([]domain.SourceID{domain.SourceGitHub}, map[domain.SourceID]string{
						domain.SourceGitHub: fmt.Sprintf("gh-%d-%d", idx, j),
					})
				} else {
					service.SetSourceConfig([]domain.SourceID{domain.SourceHackerNews}, nil)
				}
			}
		}(i)
	}
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = service.sourceEnabled(domain.SourceGitHub)
				_ = service.sourceEnabled(domain.SourceHackerNews)
				_ = service.sourceIDsSnapshot()
				service.markRecommendationsDirty()
			}
		}()
	}
	wg.Wait()
}

func TestSetSourceConfigDoesNotWaitForInFlightFetch(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()

	started := make(chan domain.SourceID, 1)
	release := make(chan struct{})
	adapter := &fakeAdapter{
		source:       domain.SourceHackerNews,
		scopes:       []domain.FetchScope{{Source: domain.SourceHackerNews, View: "top", Limit: 10}},
		startSignal:  started,
		releaseFetch: release,
		items:        []domain.FeedItem{appTestSourceItem("hn-top", "hackernews:top", "HN top", domain.SourceHackerNews, 1, time.Now().UTC())},
	}
	service := NewService(db, httpx.New(time.Second), []SourceAdapter{adapter})

	refreshDone := make(chan error, 1)
	go func() {
		_, err := service.Refresh(ctx, domain.SourceHackerNews, FeedFilter{}, RefreshOptions{Mode: RefreshModeVisible})
		refreshDone <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("refresh did not start fetch")
	}

	configDone := make(chan struct{})
	go func() {
		service.SetSourceConfig([]domain.SourceID{domain.SourceHackerNews}, nil)
		close(configDone)
	}()
	select {
	case <-configDone:
	case <-time.After(200 * time.Millisecond):
		close(release)
		<-refreshDone
		t.Fatal("SetSourceConfig blocked behind in-flight fetch")
	}

	close(release)
	if err := <-refreshDone; err != nil {
		t.Fatal(err)
	}
}

func TestEnsureRecommendationsKeepsDirtyMarkFromConcurrentMutation(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()

	replaceStarted := make(chan struct{})
	releaseReplace := make(chan struct{})
	blockingStore := &blockingRecommendationStore{
		RecommendationStore: db,
		started:             replaceStarted,
		releaseReplace:      releaseReplace,
	}
	stores := serviceStoresFromFeedStore(db)
	stores.Recommendations = blockingStore
	service := NewServiceWithStores(stores, httpx.New(time.Second), nil)

	recomputeDone := make(chan error, 1)
	go func() {
		recomputeDone <- service.RefreshRecommendations(ctx)
	}()
	select {
	case <-replaceStarted:
	case <-time.After(time.Second):
		close(releaseReplace)
		t.Fatal("recommendation recompute did not reach score replacement")
	}

	service.markRecommendationsDirty()
	close(releaseReplace)
	if err := <-recomputeDone; err != nil {
		t.Fatal(err)
	}
	if !service.recommendationsDirty(domain.SourceAll) {
		t.Fatal("concurrent recommendation dirty mark was cleared by finishing recompute")
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
		fetchErr: fmt.Errorf("%w: rate: Wait(n=1) would exceed context deadline", httpx.ErrRateLimited),
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
	service := newRuntimeConfigTestService([]SourceAdapter{github, productHunt})

	service.SetSourceConfig(nil, map[domain.SourceID]string{
		domain.SourceGitHub:      "gh-token",
		domain.SourceProductHunt: "ph-token",
	})

	if github.token != "gh-token" || productHunt.token != "ph-token" {
		t.Fatalf("tokens not applied: github=%q producthunt=%q", github.token, productHunt.token)
	}
}

func newRuntimeConfigTestService(adapters []SourceAdapter) *Service {
	ordered := orderedAdapters(adapters)
	enabled := enabledSourceSet(nil)
	return &Service{
		adapters:            ordered,
		sources:             countSources(ordered, enabled),
		enabled:             enabled,
		recommendationDirty: true,
		recommendationGen:   1,
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
			name: "http auth status",
			adapter: &fakeAdapter{
				fetchErr: &httpx.HTTPStatusError{StatusCode: http.StatusUnauthorized, URL: "https://example.test"},
			},
			wantStatus: domain.SourceStatusAuthRequired,
			wantErr:    true,
		},
		{
			name: "http rate limit status",
			adapter: &fakeAdapter{
				fetchErr: fmt.Errorf("%w: %w", httpx.ErrRateLimited, &httpx.HTTPStatusError{StatusCode: http.StatusTooManyRequests, URL: "https://example.test"}),
			},
			wantStatus: domain.SourceStatusRateLimited,
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
func TestRecommendVisibleRefreshRefreshesPrimaryScopes(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()

	hn := &fakeAdapter{source: domain.SourceHackerNews, items: []domain.FeedItem{appTestItem("hn-1", "hackernews:1", "HN")}}
	github := &fakeAdapter{
		source: domain.SourceGitHub,
		items:  []domain.FeedItem{appTestSourceItem("repo-1", "repo:owner/repo", "owner/repo", domain.SourceGitHub, 1, time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC))},
	}
	service := NewService(db, httpx.New(time.Second), []SourceAdapter{hn, github})

	if _, err := service.Refresh(ctx, domain.SourceRecommend, FeedFilter{}, RefreshOptions{Mode: RefreshModeVisible}); err != nil {
		t.Fatal(err)
	}
	if len(hn.calls) == 0 || len(github.calls) == 0 {
		t.Fatalf("recommend visible refresh calls: hn=%+v github=%+v, want both sources", hn.calls, github.calls)
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

func TestRefreshStartupUsesAdapterPrimaryScopes(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()

	aiLabs := &fakeAdapter{
		source: domain.SourceAILabs,
		scopes: []domain.FetchScope{
			{Source: domain.SourceAILabs, View: "openai"},
			{Source: domain.SourceAILabs, View: "anthropic"},
			{Source: domain.SourceAILabs, View: "deepmind"},
			{Source: domain.SourceAILabs, View: "meta"},
		},
		primaryScopes: []domain.FetchScope{
			{Source: domain.SourceAILabs, View: "openai"},
			{Source: domain.SourceAILabs, View: "anthropic"},
			{Source: domain.SourceAILabs, View: "deepmind"},
			{Source: domain.SourceAILabs, View: "meta"},
		},
	}
	service := NewService(db, httpx.New(time.Second), []SourceAdapter{aiLabs})

	if _, err := service.Refresh(ctx, domain.SourceAll, FeedFilter{}, RefreshOptions{Mode: RefreshModeStartup}); err != nil {
		t.Fatal(err)
	}

	assertScopeViews(t, aiLabs.calls, []string{"openai", "anthropic", "deepmind", "meta"})
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

func TestRefreshVisibleSpecificMultiPrimarySourceUsesSelectedViewOnly(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()

	aiLabs := &fakeAdapter{
		source: domain.SourceAILabs,
		scopes: []domain.FetchScope{
			{Source: domain.SourceAILabs, View: "openai"},
			{Source: domain.SourceAILabs, View: "anthropic"},
			{Source: domain.SourceAILabs, View: "deepmind"},
			{Source: domain.SourceAILabs, View: "meta"},
		},
		primaryScopes: []domain.FetchScope{
			{Source: domain.SourceAILabs, View: "openai"},
			{Source: domain.SourceAILabs, View: "anthropic"},
			{Source: domain.SourceAILabs, View: "deepmind"},
			{Source: domain.SourceAILabs, View: "meta"},
		},
	}
	service := NewService(db, httpx.New(time.Second), []SourceAdapter{aiLabs})

	_, err := service.Refresh(ctx, domain.SourceAILabs, FeedFilter{SourceView: "meta"}, RefreshOptions{Mode: RefreshModeVisible})
	if err != nil {
		t.Fatal(err)
	}

	assertScopeViews(t, aiLabs.calls, []string{"meta"})
}

func TestRefreshVisibleMultiPrimarySourceDefaultsToAllPrimaryScopes(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()

	aiLabs := &fakeAdapter{
		source: domain.SourceAILabs,
		primaryScopes: []domain.FetchScope{
			{Source: domain.SourceAILabs, View: "openai"},
			{Source: domain.SourceAILabs, View: "anthropic"},
			{Source: domain.SourceAILabs, View: "deepmind"},
			{Source: domain.SourceAILabs, View: "meta"},
		},
	}
	service := NewService(db, httpx.New(time.Second), []SourceAdapter{aiLabs})

	_, err := service.Refresh(ctx, domain.SourceAILabs, FeedFilter{}, RefreshOptions{Mode: RefreshModeVisible})
	if err != nil {
		t.Fatal(err)
	}

	assertScopeViews(t, aiLabs.calls, []string{"openai", "anthropic", "deepmind", "meta"})
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

func TestRefreshReplacesSourceViewWindowInsteadOfAccumulatingHistory(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	adapter := &fakeAdapter{
		source: domain.SourceHackerNews,
		scopes: []domain.FetchScope{{Source: domain.SourceHackerNews, View: "top"}},
		items: []domain.FeedItem{
			appTestSourceItem("hn-1", "hackernews:1", "HN one", domain.SourceHackerNews, 1, now),
			appTestSourceItem("hn-2", "hackernews:2", "HN two", domain.SourceHackerNews, 2, now.Add(time.Second)),
			appTestSourceItem("hn-3", "hackernews:3", "HN three", domain.SourceHackerNews, 3, now.Add(2*time.Second)),
		},
	}
	service := NewService(db, httpx.New(time.Second), []SourceAdapter{adapter})

	if _, err := service.Refresh(ctx, domain.SourceHackerNews, FeedFilter{}, RefreshOptions{Mode: RefreshModeVisible}); err != nil {
		t.Fatal(err)
	}

	adapter.items = []domain.FeedItem{
		appTestSourceItem("hn-2", "hackernews:2", "HN two updated", domain.SourceHackerNews, 1, now.Add(3*time.Second)),
		appTestSourceItem("hn-4", "hackernews:4", "HN four", domain.SourceHackerNews, 2, now.Add(4*time.Second)),
	}
	snapshot, err := service.Refresh(ctx, domain.SourceHackerNews, FeedFilter{}, RefreshOptions{Mode: RefreshModeVisible})
	if err != nil {
		t.Fatal(err)
	}

	if len(snapshot.Entries) != 2 {
		t.Fatalf("Hacker News entries = %d, want latest upstream window size 2: %+v", len(snapshot.Entries), snapshot.Entries)
	}
	if snapshot.Counts[domain.SourceHackerNews] != 2 || snapshot.Counts[domain.SourceAll] != 2 {
		t.Fatalf("counts = %+v, want Hacker News and All to reflect latest upstream window size 2", snapshot.Counts)
	}
	gotIDs := []string{snapshot.Entries[0].Sources[0].SourceIDRaw, snapshot.Entries[1].Sources[0].SourceIDRaw}
	if !reflect.DeepEqual(gotIDs, []string{"hn-2", "hn-4"}) {
		t.Fatalf("Hacker News window item ids = %+v, want [hn-2 hn-4]", gotIDs)
	}
}

func TestRefreshReplacementDoesNotLetOrphanedItemsOccupyAllFeedPage(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	githubItems := make([]domain.FeedItem, 0, 20)
	for idx := 1; idx <= 20; idx++ {
		id := fmt.Sprintf("gh-%03d", idx)
		githubItems = append(githubItems, appTestSourceItem(id, "repo:owner/"+id, "owner/"+id, domain.SourceGitHub, idx, now.Add(-time.Hour)))
	}
	if err := db.UpsertFeedItems(ctx, githubItems); err != nil {
		t.Fatal(err)
	}
	hnItems := make([]domain.FeedItem, 0, 260)
	for idx := 1; idx <= 260; idx++ {
		id := fmt.Sprintf("hn-old-%03d", idx)
		hnItems = append(hnItems, appTestSourceItem(id, "hackernews:"+id, "Old HN "+id, domain.SourceHackerNews, idx, now.Add(time.Duration(idx)*time.Second)))
	}
	adapter := &fakeAdapter{
		source: domain.SourceHackerNews,
		scopes: []domain.FetchScope{{Source: domain.SourceHackerNews, View: "top"}},
		items:  hnItems,
	}
	service := NewService(db, httpx.New(time.Second), []SourceAdapter{adapter})

	if _, err := service.Refresh(ctx, domain.SourceHackerNews, FeedFilter{}, RefreshOptions{Mode: RefreshModeVisible}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSaved(ctx, hnItems[0].ID, true); err != nil {
		t.Fatal(err)
	}

	adapter.items = []domain.FeedItem{
		appTestSourceItem("hn-new-1", "hackernews:new-1", "New HN one", domain.SourceHackerNews, 1, now.Add(2*time.Hour)),
		appTestSourceItem("hn-new-2", "hackernews:new-2", "New HN two", domain.SourceHackerNews, 2, now.Add(2*time.Hour+time.Second)),
	}
	if _, err := service.Refresh(ctx, domain.SourceHackerNews, FeedFilter{}, RefreshOptions{Mode: RefreshModeVisible}); err != nil {
		t.Fatal(err)
	}

	all, err := service.LoadFeed(ctx, domain.SourceAll, FeedFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Entries) != 22 {
		t.Fatalf("all entries = %d, want current HN window plus GitHub items", len(all.Entries))
	}
	if !snapshotHasSource(all, domain.SourceGitHub) {
		t.Fatalf("all feed lost GitHub items after HN window replacement: %+v", all.Entries)
	}
	for _, entry := range all.Entries {
		if strings.HasPrefix(entry.Item.ID, "hn-old-") {
			t.Fatalf("all feed leaked orphaned old HN item: %+v", entry)
		}
	}
	if all.Counts[domain.SourceAll] != 22 || all.Counts[domain.SourceHackerNews] != 2 || all.Counts[domain.SourceGitHub] != 20 {
		t.Fatalf("counts = %+v, want all=22 hn=2 github=20", all.Counts)
	}
	exported, err := service.ExportSaved(ctx, ExportOptions{Format: ExportJSON, Dir: t.TempDir(), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if exported.Count != 1 {
		t.Fatalf("exported saved count = %d, want saved orphan preserved", exported.Count)
	}
}

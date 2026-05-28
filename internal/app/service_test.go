package app

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AIluffy/tildewire/internal/dedupe"
	"github.com/AIluffy/tildewire/internal/domain"
	"github.com/AIluffy/tildewire/internal/httpx"
	"github.com/AIluffy/tildewire/internal/normalize"
	"github.com/AIluffy/tildewire/internal/sources"
	"github.com/AIluffy/tildewire/internal/store"
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
	service := NewService(nil, nil, []SourceAdapter{github, hn})

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
		Store:          db,
		started:        replaceStarted,
		releaseReplace: releaseReplace,
	}
	service := NewService(blockingStore, httpx.New(time.Second), nil)

	recomputeDone := make(chan error, 1)
	go func() {
		recomputeDone <- service.ensureRecommendations(ctx, domain.SourceAll)
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

	snapshot, err := service.LoadFeed(ctx, domain.SourceRecommend, FeedFilter{})
	if err != nil {
		t.Fatal(err)
	}
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

	snapshot, err := service.LoadFeed(ctx, domain.SourceRecommend, FeedFilter{})
	if err != nil {
		t.Fatal(err)
	}
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

	snapshot, err := service.LoadFeed(ctx, domain.SourceRecommend, FeedFilter{})
	if err != nil {
		t.Fatal(err)
	}
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

	snapshot, err := service.LoadFeed(ctx, domain.SourceRecommend, FeedFilter{})
	if err != nil {
		t.Fatal(err)
	}
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

	snapshot, err := service.LoadFeed(ctx, domain.SourceRecommend, FeedFilter{})
	if err != nil {
		t.Fatal(err)
	}
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

	snapshot, err := service.LoadFeed(ctx, domain.SourceRecommend, FeedFilter{})
	if err != nil {
		t.Fatal(err)
	}
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

	snapshot, err := service.LoadFeed(ctx, domain.SourceRecommend, FeedFilter{})
	if err != nil {
		t.Fatal(err)
	}
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

	snapshot, err := service.LoadFeed(ctx, domain.SourceRecommend, FeedFilter{IncludeHidden: true})
	if err != nil {
		t.Fatal(err)
	}
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

	snapshot, err := service.LoadFeed(ctx, domain.SourceRecommend, FeedFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Entries) != 0 {
		t.Fatalf("cold recommend entries = %+v, want empty", snapshot.Entries)
	}
	snapshot, err = service.CreatePersonalizationRule(ctx, domain.PersonalizationRule{
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
	snapshot, err = service.LoadFeed(ctx, domain.SourceRecommend, FeedFilter{IncludeHidden: true})
	if err != nil {
		t.Fatal(err)
	}
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

func TestRecommendFeedRecomputesStaleScoresWhenOpened(t *testing.T) {
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
	if _, err := service.LoadFeed(ctx, domain.SourceRecommend, FeedFilter{}); err != nil {
		t.Fatal(err)
	}

	if err := db.UpsertFeedItems(ctx, []domain.FeedItem{githubOne, githubTwo}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSaved(ctx, githubOne.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSaved(ctx, githubTwo.ID, true); err != nil {
		t.Fatal(err)
	}

	snapshot, err := service.LoadFeed(ctx, domain.SourceRecommend, FeedFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshotHasSource(snapshot, domain.SourceGitHub) {
		t.Fatalf("stale recommend scores did not refresh saved GitHub interest: ids=%+v", appEntryIDs(snapshot.Entries))
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
	if githubWeeklySnapshot.Counts[domain.SourceAll] != all.Counts[domain.SourceAll] {
		t.Fatalf("all count should stay on All tab aggregate count when active source scope changes: all=%+v weekly=%+v", all.Counts, githubWeeklySnapshot.Counts)
	}
}

func TestLoadFeedSingleSourceCountIsNotCappedByPageLimit(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	items := make([]domain.FeedItem, 0, 306)
	for idx := 1; idx <= 306; idx++ {
		id := fmt.Sprintf("hn-%03d", idx)
		items = append(items, appTestSourceItem(id, "hackernews:"+id, "HN story "+id, domain.SourceHackerNews, idx, now.Add(time.Duration(idx)*time.Second)))
	}
	if err := db.UpsertFeedItems(ctx, items); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, httpx.New(time.Second), nil)

	all, err := service.LoadFeed(ctx, domain.SourceAll, FeedFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if all.Counts[domain.SourceHackerNews] != 306 {
		t.Fatalf("all view Hacker News count = %d, want 306", all.Counts[domain.SourceHackerNews])
	}

	hackerNews, err := service.LoadFeed(ctx, domain.SourceHackerNews, FeedFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hackerNews.Entries) != 250 {
		t.Fatalf("loaded Hacker News entries = %d, want page limit 250", len(hackerNews.Entries))
	}
	if hackerNews.Counts[domain.SourceHackerNews] != 306 {
		t.Fatalf("selected Hacker News count = %d, want 306", hackerNews.Counts[domain.SourceHackerNews])
	}
}

func TestLoadFeedCountsMultiPrimarySourceViews(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	openAI := appTestSourceItem("ai-openai", "url:https://openai.com/news/one", "OpenAI one", domain.SourceAILabs, 1, now)
	openAI.Sources[0].SourceView = "openai"
	meta := appTestSourceItem("ai-meta", "url:https://ai.meta.com/blog/one", "Meta one", domain.SourceAILabs, 1, now.Add(time.Minute))
	meta.Sources[0].SourceView = "meta"
	if err := db.UpsertFeedItems(ctx, []domain.FeedItem{openAI, meta}); err != nil {
		t.Fatal(err)
	}
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

	snapshot, err := service.LoadFeed(ctx, domain.SourceAll, FeedFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Counts[domain.SourceAILabs] != 2 || snapshot.Counts[domain.SourceAll] != 2 {
		t.Fatalf("counts = %+v, want AI Labs and All to count both provider items", snapshot.Counts)
	}
}

func TestLoadFeedMultiPrimarySourceDefaultsToAllViews(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	openAI := appTestSourceItem("ai-openai", "url:https://openai.com/news/one", "OpenAI one", domain.SourceAILabs, 1, now)
	openAI.Sources[0].SourceView = "openai"
	meta := appTestSourceItem("ai-meta", "url:https://ai.meta.com/blog/one", "Meta one", domain.SourceAILabs, 1, now.Add(time.Minute))
	meta.Sources[0].SourceView = "meta"
	if err := db.UpsertFeedItems(ctx, []domain.FeedItem{openAI, meta}); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, httpx.New(time.Second), []SourceAdapter{&fakeAdapter{source: domain.SourceAILabs}})

	snapshot, err := service.LoadFeed(ctx, domain.SourceAILabs, FeedFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Entries) != 2 || snapshot.Filter.SourceView != "" {
		t.Fatalf("AI Labs default feed = entries:%d filter:%+v, want all provider entries with empty source view", len(snapshot.Entries), snapshot.Filter)
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
	primaryScopes []domain.FetchScope
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

func (f *fakeAdapter) PrimaryScopes() []domain.FetchScope {
	return f.primaryScopes
}

func (f *fakeAdapter) Fetch(ctx context.Context, scope domain.FetchScope, _ httpx.Requester) (*domain.FetchResult, error) {
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

func (f *fakeAdapter) Detail(context.Context, domain.FeedEntry, httpx.Getter) (domain.ItemDetail, error) {
	f.detailCalls++
	return f.detail, f.detailErr
}

type blockingRecommendationStore struct {
	*store.Store
	replaceStarted sync.Once
	started        chan struct{}
	releaseReplace <-chan struct{}
}

func (s *blockingRecommendationStore) ReplaceRecommendationScores(ctx context.Context, scores []domain.RecommendationScore) error {
	if s.started != nil {
		s.replaceStarted.Do(func() {
			close(s.started)
		})
	}
	if s.releaseReplace != nil {
		select {
		case <-s.releaseReplace:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return s.Store.ReplaceRecommendationScores(ctx, scores)
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

func appEntryIDs(entries []domain.FeedEntry) []string {
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.Item.ID)
	}
	return ids
}

func appEntryByID(entries []domain.FeedEntry, itemID string) (domain.FeedEntry, bool) {
	for _, entry := range entries {
		if entry.Item.ID == itemID {
			return entry, true
		}
	}
	return domain.FeedEntry{}, false
}

func appEntryIndex(entries []domain.FeedEntry, itemID string) int {
	for idx, entry := range entries {
		if entry.Item.ID == itemID {
			return idx
		}
	}
	return -1
}

func recommendationReasonsContain(reasons []domain.RecommendationReason, label string) bool {
	for _, reason := range reasons {
		if reason.Label == label {
			return true
		}
	}
	return false
}

func profileTermsContain(terms []domain.RecommendationProfileTerm, kind, value string) bool {
	for _, term := range terms {
		if term.Kind == kind && term.Value == value {
			return true
		}
	}
	return false
}

func diagnosticTermsContain(terms []domain.RecommendationDiagnosticsTerm, kind, value string) bool {
	for _, term := range terms {
		if term.Kind == kind && term.Value == value {
			return true
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

package app

import (
	"context"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/AIluffy/tildewire/internal/dedupe"
	"github.com/AIluffy/tildewire/internal/domain"
	"github.com/AIluffy/tildewire/internal/httpx"
	"github.com/AIluffy/tildewire/internal/store"
)

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
	RecommendationStore
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
	return s.RecommendationStore.ReplaceRecommendationScores(ctx, scores)
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

package sources

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/AIluffy/tildewire/internal/domain"
	"github.com/AIluffy/tildewire/internal/httpx"
)

func TestHackerNewsNormalizePreservesRankAndFields(t *testing.T) {
	raw := &domain.FetchResult{
		Source:    domain.SourceHackerNews,
		FetchedAt: time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC),
		Body: []byte(`{
			"ids": [42, 7],
			"items": [
				{"id":42,"type":"story","by":"alice","time":1778407200,"url":"https://example.com/?utm_source=hn","score":120,"title":"SQLite for agents","descendants":18},
				{"id":7,"type":"story","by":"bob","time":1778407100,"score":5,"title":"Ask HN: Offline tools","descendants":2}
			]
		}`),
	}
	items, err := NewHackerNewsAdapter().Normalize(context.Background(), domain.FetchScope{Source: domain.SourceHackerNews, View: "top"}, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if items[0].Title != "SQLite for agents" || items[0].Sources[0].SourceRank != 1 {
		t.Fatalf("first item not normalized with rank: %+v", items[0])
	}
	if items[0].CanonicalKey != "url:https://example.com" {
		t.Fatalf("canonical key = %q", items[0].CanonicalKey)
	}
	if items[1].CommentsURL == "" || items[1].URL != items[1].CommentsURL {
		t.Fatalf("HN-only story should use comments URL: %+v", items[1])
	}
}

func TestHackerNewsDefaultScopesCoverMVPViews(t *testing.T) {
	scopes := NewHackerNewsAdapter().DefaultScopes()
	got := make(map[string]bool)
	for _, scope := range scopes {
		got[scope.View] = true
		if scope.Source != domain.SourceHackerNews || scope.Limit <= 0 {
			t.Fatalf("invalid scope: %+v", scope)
		}
	}
	for _, view := range []string{"top", "best", "new", "show"} {
		if !got[view] {
			t.Fatalf("missing HN scope %q in %+v", view, scopes)
		}
	}
}

func TestHackerNewsFetchCachesAndFallsBackToStale(t *testing.T) {
	fail := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		switch r.URL.Path {
		case "/topstories.json":
			_, _ = w.Write([]byte(`[42]`))
		case "/item/42.json":
			_, _ = w.Write([]byte(`{"id":42,"type":"story","by":"alice","time":1778407200,"url":"https://example.com/article","score":120,"title":"SQLite for agents","descendants":18}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cache := newSourceTestCache()
	client := httpx.New(time.Second, cache)
	adapter := HackerNewsAdapter{BaseURL: server.URL}
	scope := domain.FetchScope{Source: domain.SourceHackerNews, View: "top", Limit: 10}

	live, err := adapter.Fetch(context.Background(), scope, client)
	if err != nil {
		t.Fatal(err)
	}
	if live.Stale || live.FromCache {
		t.Fatalf("expected live response: %+v", live)
	}
	itemKey := httpx.RequestKey(http.MethodGet, server.URL+"/item/42.json")
	itemCache, ok, err := cache.GetHTTPCache(context.Background(), itemKey)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected item cache entry")
	}

	fail = true
	scope.ForceRefresh = true
	stale, err := adapter.Fetch(context.Background(), scope, client)
	if err != nil {
		t.Fatal(err)
	}
	if !stale.Stale || !stale.FromCache {
		t.Fatalf("expected stale cache fallback: %+v", stale)
	}
	items, err := adapter.Normalize(context.Background(), scope, stale)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if !items[0].LastSeenAt.Equal(itemCache.FetchedAt) {
		t.Fatalf("stale normalize refreshed LastSeenAt: got %s want %s", items[0].LastSeenAt, itemCache.FetchedAt)
	}
}

func TestHackerNewsFetchMarksPartialItemFailuresStale(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/topstories.json":
			_, _ = w.Write([]byte(`[42,7]`))
		case "/item/42.json":
			_, _ = w.Write([]byte(`{"id":42,"type":"story","by":"alice","time":1778407200,"url":"https://example.com/article","score":120,"title":"SQLite for agents","descendants":18}`))
		case "/item/7.json":
			http.Error(w, "down", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := httpx.New(time.Second, newSourceTestCache())
	adapter := HackerNewsAdapter{BaseURL: server.URL}
	scope := domain.FetchScope{Source: domain.SourceHackerNews, View: "top", Limit: 10}

	result, err := adapter.Fetch(context.Background(), scope, client)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Stale || result.StaleReason != httpx.StaleReasonNetworkError {
		t.Fatalf("partial item failure should mark result stale/network_error: %+v", result)
	}
	items, err := adapter.Normalize(context.Background(), scope, result)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Refs.HNID != "42" {
		t.Fatalf("partial item failure should preserve successful items: %+v", items)
	}
}

func TestHackerNewsFetchDoesNotThrottleColdItemFanout(t *testing.T) {
	const itemCount = 15
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/topstories.json":
			_, _ = w.Write([]byte(`[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15]`))
		default:
			time.Sleep(25 * time.Millisecond)
			var id int
			if _, err := fmt.Sscanf(r.URL.Path, "/item/%d.json", &id); err != nil {
				http.NotFound(w, r)
				return
			}
			_, _ = fmt.Fprintf(w, `{"id":%d,"type":"story","by":"alice","time":1778407200,"url":"https://example.com/%d","score":120,"title":"Story %d","descendants":18}`, id, id, id)
		}
	}))
	defer server.Close()

	client := httpx.New(3*time.Second, newSourceTestCache())
	adapter := HackerNewsAdapter{BaseURL: server.URL}
	scope := domain.FetchScope{Source: domain.SourceHackerNews, View: "top", Limit: itemCount}

	start := time.Now()
	result, err := adapter.Fetch(context.Background(), scope, client)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	items, err := adapter.Normalize(context.Background(), scope, result)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != itemCount {
		t.Fatalf("items = %d, want %d", len(items), itemCount)
	}
	if elapsed > time.Second {
		t.Fatalf("cold HN item fanout took %s, want under 1s", elapsed)
	}
}

func TestHackerNewsFetchReturnsErrorWhenAllItemDetailsFail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/topstories.json":
			_, _ = w.Write([]byte(`[42,7]`))
		case "/item/42.json", "/item/7.json":
			http.Error(w, "down", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := httpx.New(time.Second, newSourceTestCache())
	adapter := HackerNewsAdapter{BaseURL: server.URL}
	scope := domain.FetchScope{Source: domain.SourceHackerNews, View: "top", Limit: 10}

	_, err := adapter.Fetch(context.Background(), scope, client)
	if err == nil {
		t.Fatal("expected all item detail failures to return an error")
	}
}

func TestHackerNewsDetailLoadsTopLevelComments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/item/42.json":
			_, _ = w.Write([]byte(`{"id":42,"type":"story","by":"alice","time":1778407200,"title":"SQLite for agents","kids":[100,101]}`))
		case "/item/100.json":
			_, _ = w.Write([]byte(`{"id":100,"type":"comment","by":"bob","time":1778407300,"text":"<p>Useful comment.</p>","parent":42}`))
		case "/item/101.json":
			_, _ = w.Write([]byte(`{"id":101,"type":"comment","by":"carol","time":1778407350,"text":"<p>Another comment.</p>","parent":42,"deleted":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	adapter := HackerNewsAdapter{BaseURL: server.URL}
	entry := domain.FeedEntry{
		Item: domain.FeedItem{ID: "id-1", Title: "SQLite for agents", Refs: domain.Refs{HNID: "42"}},
		Sources: []domain.ItemSource{{
			Source:      domain.SourceHackerNews,
			SourceIDRaw: "42",
			SourceURL:   "https://news.ycombinator.com/item?id=42",
		}},
	}
	detail, err := adapter.Detail(context.Background(), entry, httpx.New(time.Second, newSourceTestCache()))
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Comments) != 1 {
		t.Fatalf("comments = %+v, want one visible top-level comment", detail.Comments)
	}
	if detail.Comments[0].Author != "bob" || detail.Comments[0].Body != "Useful comment." {
		t.Fatalf("comment not normalized: %+v", detail.Comments[0])
	}
}

type sourceTestCache struct {
	mu      sync.Mutex
	entries map[string]httpx.CacheEntry
}

func newSourceTestCache() *sourceTestCache {
	return &sourceTestCache{entries: make(map[string]httpx.CacheEntry)}
}

func (c *sourceTestCache) GetHTTPCache(_ context.Context, key string) (httpx.CacheEntry, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	return entry, ok, nil
}

func (c *sourceTestCache) PutHTTPCache(_ context.Context, entry httpx.CacheEntry) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[entry.RequestKey] = entry
	return nil
}

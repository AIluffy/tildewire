package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zhangxueai/tildewire/internal/domain"
	"github.com/zhangxueai/tildewire/internal/httpx"
)

func TestLobstersNormalizeFixture(t *testing.T) {
	fetchedAt := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	raw := &domain.FetchResult{
		Source:    domain.SourceLobsters,
		FetchedAt: fetchedAt,
		Body:      []byte(lobstersListFixture),
	}
	items, err := NewLobstersAdapter().Normalize(context.Background(), domain.FetchScope{
		Source: domain.SourceLobsters,
		View:   "hottest",
		Limit:  10,
	}, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	first := items[0]
	if first.Title != "Tiny terminal radar" || first.Author != "alice" || first.Summary != "A small terminal feed." {
		t.Fatalf("fields not normalized: %+v", first)
	}
	if first.CanonicalKey != "url:https://example.com/radar" || first.Refs.LobstersID != "abc123" {
		t.Fatalf("identity mismatch: %+v", first)
	}
	if first.Metrics.Score == nil || *first.Metrics.Score != 33 || first.Metrics.Comments == nil || *first.Metrics.Comments != 4 {
		t.Fatalf("metrics mismatch: %+v", first.Metrics)
	}
	if first.Sources[0].Source != domain.SourceLobsters || first.Sources[0].SourceView != "hottest" || first.Sources[0].SourceRank != 1 {
		t.Fatalf("source context mismatch: %+v", first.Sources[0])
	}
	if len(first.Tags) < 4 {
		t.Fatalf("tags missing source/view/native tags: %+v", first.Tags)
	}
	second := items[1]
	if second.CanonicalKey != "lobsters:def456" || second.URL != second.CommentsURL {
		t.Fatalf("lobsters-only story identity mismatch: %+v", second)
	}
}

func TestLobstersFetchBuildsViewURLAndUsesCache(t *testing.T) {
	hits := 0
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(lobstersListFixture))
	}))
	defer server.Close()

	cache := newSourceTestCache()
	client := httpx.New(time.Second, cache)
	adapter := LobstersAdapter{BaseURL: server.URL}
	scope := domain.FetchScope{Source: domain.SourceLobsters, View: "newest", Limit: 25}

	live, err := adapter.Fetch(context.Background(), scope, client)
	if err != nil {
		t.Fatal(err)
	}
	cached, err := adapter.Fetch(context.Background(), scope, client)
	if err != nil {
		t.Fatal(err)
	}
	if live.FromCache || !cached.FromCache || hits != 1 {
		t.Fatalf("cache behavior mismatch live=%+v cached=%+v hits=%d", live, cached, hits)
	}
	if gotPath != "/newest.json" {
		t.Fatalf("path = %q, want /newest.json", gotPath)
	}
}

func TestLobstersDetailLoadsTopLevelComments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/s/abc123.json" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(lobstersStoryFixture))
	}))
	defer server.Close()

	adapter := LobstersAdapter{BaseURL: server.URL}
	entry := domain.FeedEntry{
		Item: domain.FeedItem{ID: "id-1", Title: "Tiny terminal radar", Refs: domain.Refs{LobstersID: "abc123"}},
		Sources: []domain.ItemSource{{
			Source:      domain.SourceLobsters,
			SourceIDRaw: "abc123",
			SourceURL:   server.URL + "/s/abc123/tiny_terminal_radar",
		}},
	}
	detail, err := adapter.Detail(context.Background(), entry, httpx.New(time.Second, newSourceTestCache()))
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Comments) != 1 {
		t.Fatalf("comments = %+v, want one top-level comment", detail.Comments)
	}
	if detail.Comments[0].Author != "bob" || detail.Comments[0].Body != "Great point." {
		t.Fatalf("comment not normalized: %+v", detail.Comments[0])
	}
}

func TestLobstersNormalizeParserError(t *testing.T) {
	_, err := NewLobstersAdapter().Normalize(context.Background(), domain.FetchScope{Source: domain.SourceLobsters, View: "hottest"}, &domain.FetchResult{
		Body: []byte(`{"not":"a list"}`),
	})
	if err == nil {
		t.Fatal("expected parser error")
	}
}

const lobstersListFixture = `[
  {
    "short_id": "abc123",
    "short_id_url": "https://lobste.rs/s/abc123/tiny_terminal_radar",
    "created_at": "2026-05-10T09:00:00.000Z",
    "title": "Tiny terminal radar",
    "url": "https://example.com/radar?utm_source=lobsters",
    "score": 33,
    "comment_count": 4,
    "description": "A small terminal feed.",
    "submitter_user": {"username": "alice"},
    "tags": ["go", "tui"]
  },
  {
    "short_id": "def456",
    "short_id_url": "https://lobste.rs/s/def456/ask_terminal_tools",
    "created_at": "2026-05-10T08:00:00.000Z",
    "title": "Ask: terminal tools",
    "score": 3,
    "comment_count": 1,
    "submitter_user": "carol",
    "tags": [{"tag": "ask"}]
  }
]`

const lobstersStoryFixture = `{
  "short_id": "abc123",
  "short_id_url": "https://lobste.rs/s/abc123/tiny_terminal_radar",
  "title": "Tiny terminal radar",
  "comments": [
    {
      "short_id": "c1",
      "comment": "<p>Great point.</p>",
      "commenting_user": {"username": "bob"},
      "score": 5,
      "created_at": "2026-05-10T10:00:00.000Z",
      "indent_level": 0
    },
    {
      "short_id": "c2",
      "comment": "<p>Nested reply.</p>",
      "commenting_user": {"username": "dana"},
      "score": 2,
      "created_at": "2026-05-10T10:05:00.000Z",
      "indent_level": 1
    }
  ]
}`

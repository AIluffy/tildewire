package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AIluffy/tildewire/internal/domain"
	"github.com/AIluffy/tildewire/internal/httpx"
)

func TestHuggingFacePapersNormalizeFixture(t *testing.T) {
	fetchedAt := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	raw := &domain.FetchResult{
		Source:    domain.SourceHuggingFace,
		FetchedAt: fetchedAt,
		Body:      []byte(huggingFacePapersFixture),
	}
	items, err := NewHuggingFacePapersAdapter().Normalize(context.Background(), domain.FetchScope{
		Source: domain.SourceHuggingFace,
		View:   "daily",
		Limit:  10,
	}, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}

	first := items[0]
	if first.CanonicalKey != "arxiv:2605.12345" || first.Refs.ArxivID != "2605.12345" || first.Refs.PaperID != "2605.12345" {
		t.Fatalf("paper id not normalized: %+v", first)
	}
	if first.Title != "Tiny Agents for Terminal Workflows" || first.Summary != "A small agent benchmark." {
		t.Fatalf("fields not normalized: %+v", first)
	}
	if first.Author != "Alice Example, Bob Example" {
		t.Fatalf("author = %q, want joined authors", first.Author)
	}
	if first.Refs.Repo != "example/tiny-agents" {
		t.Fatalf("github repo not extracted: %+v", first.Refs)
	}
	if first.Metrics.Upvotes == nil || *first.Metrics.Upvotes != 42 {
		t.Fatalf("upvotes = %+v, want 42", first.Metrics.Upvotes)
	}
	if first.Metrics.Comments == nil || *first.Metrics.Comments != 7 {
		t.Fatalf("comments = %+v, want 7", first.Metrics.Comments)
	}
	if first.Metrics.GitHubStars == nil || *first.Metrics.GitHubStars != 1234 {
		t.Fatalf("github stars = %+v, want 1234", first.Metrics.GitHubStars)
	}
	if first.Sources[0].SourceRank != 1 || first.Sources[0].SourceView != "daily" {
		t.Fatalf("source context mismatch: %+v", first.Sources[0])
	}
	if first.URL != "https://huggingface.co/papers/2605.12345" || first.CommentsURL != "https://huggingface.co/papers/2605.12345#discussion" {
		t.Fatalf("paper urls mismatch: %+v", first)
	}

	second := items[1]
	if second.CanonicalKey != "paper:hf-paper-only" || second.Author != "Carol Example" {
		t.Fatalf("fallback paper id/author mismatch: %+v", second)
	}
	if second.Sources[0].SourceRank != 2 {
		t.Fatalf("rank = %d, want 2", second.Sources[0].SourceRank)
	}
}

func TestHuggingFacePapersFetchBuildsURLAndUsesCache(t *testing.T) {
	hits := 0
	var gotPath, gotLimit string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		gotPath = r.URL.Path
		gotLimit = r.URL.Query().Get("limit")
		_, _ = w.Write([]byte(huggingFacePapersFixture))
	}))
	defer server.Close()

	cache := newSourceTestCache()
	client := httpx.New(time.Second, cache)
	adapter := HuggingFacePapersAdapter{BaseURL: server.URL}
	scope := domain.FetchScope{Source: domain.SourceHuggingFace, View: "daily", Limit: 25}

	live, err := adapter.Fetch(context.Background(), scope, client)
	if err != nil {
		t.Fatal(err)
	}
	if live.FromCache || live.Stale {
		t.Fatalf("expected live response: %+v", live)
	}
	cached, err := adapter.Fetch(context.Background(), scope, client)
	if err != nil {
		t.Fatal(err)
	}
	if !cached.FromCache || cached.Stale {
		t.Fatalf("expected fresh cache response: %+v", cached)
	}
	if hits != 1 {
		t.Fatalf("server hits = %d, want 1", hits)
	}
	if gotPath != "/api/daily_papers" || gotLimit != "25" {
		t.Fatalf("url path/limit = %q/%q, want /api/daily_papers/25", gotPath, gotLimit)
	}
}

func TestHuggingFacePapersDetailExpandsMetadata(t *testing.T) {
	adapter := NewHuggingFacePapersAdapter()
	raw := &domain.FetchResult{
		Source:    domain.SourceHuggingFace,
		FetchedAt: time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC),
		Body:      []byte(huggingFacePapersFixture),
	}
	items, err := adapter.Normalize(context.Background(), domain.FetchScope{Source: domain.SourceHuggingFace, View: "daily"}, raw)
	if err != nil {
		t.Fatal(err)
	}
	entry := domain.FeedEntry{Item: items[0], Sources: items[0].Sources}

	detail, err := adapter.Detail(context.Background(), entry, httpx.New(time.Second, newSourceTestCache()))
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Sections) < 2 {
		t.Fatalf("expected paper metadata sections: %+v", detail.Sections)
	}
	if detail.Sections[0].Title != "Paper Details" || !strings.Contains(detail.Sections[0].Body, "2605.12345") {
		t.Fatalf("paper details missing id: %+v", detail.Sections[0])
	}
	if !strings.Contains(detail.Sections[1].Body, "example/tiny-agents") || !strings.Contains(detail.Sections[1].Body, "agents") {
		t.Fatalf("paper links/keywords missing: %+v", detail.Sections[1])
	}
}

const huggingFacePapersFixture = `[
  {
    "paper": {
      "id": "2605.12345",
      "title": "Tiny Agents for Terminal Workflows",
      "summary": "A small agent benchmark.",
      "publishedAt": "2026-05-10T09:00:00.000Z",
      "authors": [
        {"_id": "a1", "name": "Alice Example", "hidden": false},
        {"_id": "b1", "name": "Bob Example", "hidden": false}
      ],
      "upvotes": 42,
      "discussionId": "disc-1",
      "githubRepo": "https://github.com/example/tiny-agents",
      "githubStars": 1234,
      "ai_keywords": ["agents", "terminal"]
    },
    "publishedAt": "2026-05-10T09:05:00.000Z",
    "title": "Tiny Agents for Terminal Workflows",
    "summary": "A small agent benchmark.",
    "numComments": 7
  },
  {
    "paper": {
      "id": "hf-paper-only",
      "title": "Paper Without Arxiv Shape",
      "summary": "",
      "publishedAt": "2026-05-10T08:00:00.000Z",
      "authors": [
        {"_id": "c1", "name": "Carol Example", "hidden": false}
      ],
      "upvotes": 3,
      "discussionId": "disc-2"
    },
    "publishedAt": "2026-05-10T08:05:00.000Z",
    "title": "Paper Without Arxiv Shape",
    "summary": "",
    "numComments": 0
  }
]`

package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AIluffy/tildewire/internal/domain"
	"github.com/AIluffy/tildewire/internal/httpx"
)

func TestProductHuntFetchRequiresTokenWithoutNetwork(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write([]byte(productHuntFixture))
	}))
	defer server.Close()

	adapter := ProductHuntAdapter{BaseURL: server.URL}
	_, err := adapter.Fetch(context.Background(), domain.FetchScope{Source: domain.SourceProductHunt, View: "today"}, httpx.New(time.Second, newSourceTestCache()))
	if err == nil ||
		!strings.Contains(strings.ToLower(err.Error()), "auth required") ||
		!strings.Contains(err.Error(), "PRODUCT_HUNT_TOKEN") ||
		strings.Contains(err.Error(), "TILDEWIRE_PRODUCT_HUNT_TOKEN") {
		t.Fatalf("expected auth required error, got %v", err)
	}
	if hits != 0 {
		t.Fatalf("server hits = %d, want no network without token", hits)
	}
}

func TestProductHuntFetchBuildsGraphQLRequest(t *testing.T) {
	now := time.Date(2026, 5, 12, 15, 30, 0, 0, time.UTC)
	var gotAuth string
	var request struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(productHuntFixture))
	}))
	defer server.Close()

	adapter := ProductHuntAdapter{
		BaseURL: server.URL,
		Token:   "secret-token",
		Now:     func() time.Time { return now },
	}
	raw, err := adapter.Fetch(context.Background(), domain.FetchScope{Source: domain.SourceProductHunt, View: "weekly", Limit: 10}, httpx.New(time.Second, newSourceTestCache()))
	if err != nil {
		t.Fatal(err)
	}
	if raw.Source != domain.SourceProductHunt || raw.Scope.View != "weekly" {
		t.Fatalf("raw result mismatch: %+v", raw)
	}
	if gotAuth != "Bearer secret-token" {
		t.Fatalf("authorization = %q, want bearer token", gotAuth)
	}
	for _, want := range []string{"posts", "featured", "RANKING", "productLinks", "topics"} {
		if !strings.Contains(request.Query, want) {
			t.Fatalf("query missing %q:\n%s", want, request.Query)
		}
	}
	if request.Variables["first"].(float64) != 10 {
		t.Fatalf("first variable = %+v, want 10", request.Variables["first"])
	}
	if request.Variables["postedAfter"] != "2026-05-05T07:00:00Z" || request.Variables["postedBefore"] != "2026-05-13T07:00:00Z" {
		t.Fatalf("date variables = %+v", request.Variables)
	}
}

func TestProductHuntTokenConcurrentWithAuthAndFetch(t *testing.T) {
	adapter := ProductHuntAdapter{
		Token: "initial",
		Now:   func() time.Time { return time.Date(2026, 5, 12, 15, 30, 0, 0, time.UTC) },
	}
	client := staticRequester{body: []byte(productHuntFixture)}
	scope := domain.FetchScope{Source: domain.SourceProductHunt, View: "today", Limit: 5}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 25; j++ {
				adapter.SetToken(fmt.Sprintf("token-%d-%d", idx, j))
			}
		}(i)
	}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 25; j++ {
				_, _ = adapter.AuthRequired()
				if _, err := adapter.Fetch(context.Background(), scope, client); err != nil {
					t.Errorf("Fetch: %v", err)
				}
			}
		}()
	}
	wg.Wait()
}

type staticRequester struct {
	body []byte
}

func (r staticRequester) DoGET(context.Context, httpx.GetOptions) (httpx.Response, error) {
	return httpx.Response{Body: r.body, FetchedAt: time.Now().UTC()}, nil
}

func (r staticRequester) DoPOSTJSON(context.Context, httpx.PostOptions) (httpx.Response, error) {
	return httpx.Response{Body: r.body, FetchedAt: time.Now().UTC()}, nil
}

func TestProductHuntRequestUsesProductHuntLaunchDay(t *testing.T) {
	now := time.Date(2026, 5, 15, 3, 51, 0, 0, time.UTC)
	request := productHuntRequest(domain.FetchScope{
		Source: domain.SourceProductHunt,
		View:   "today",
		Limit:  25,
	}, now)

	if request.Variables.PostedAfter != "2026-05-14T07:00:00Z" || request.Variables.PostedBefore != "2026-05-15T07:00:00Z" {
		t.Fatalf("date variables = %+v, want Product Hunt launch day in Pacific time", request.Variables)
	}
}

func TestProductHuntNormalizeFixture(t *testing.T) {
	fetchedAt := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	raw := &domain.FetchResult{
		Source:    domain.SourceProductHunt,
		FetchedAt: fetchedAt,
		Body:      []byte(productHuntFixture),
	}
	items, err := NewProductHuntAdapter("token").Normalize(context.Background(), domain.FetchScope{
		Source: domain.SourceProductHunt,
		View:   "today",
		Limit:  10,
	}, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	first := items[0]
	if first.ItemType != "product" || first.Title != "Tildewire" || first.URL != "https://tildewire.dev?utm_source=producthunt" {
		t.Fatalf("product fields mismatch: %+v", first)
	}
	if first.CommentsURL != "https://www.producthunt.com/posts/tildewire" || first.Sources[0].SourceURL != first.CommentsURL {
		t.Fatalf("product hunt attribution URL mismatch: %+v source=%+v", first, first.Sources[0])
	}
	if first.Refs.ProductHuntID != "101" || first.Refs.ProductHuntSlug != "tildewire" {
		t.Fatalf("product hunt refs mismatch: %+v", first.Refs)
	}
	if first.Metrics.Upvotes == nil || *first.Metrics.Upvotes != 120 || first.Metrics.Comments == nil || *first.Metrics.Comments != 8 {
		t.Fatalf("metrics mismatch: %+v", first.Metrics)
	}
	if first.Sources[0].Source != domain.SourceProductHunt || first.Sources[0].SourceView != "today" || first.Sources[0].SourceRank != 2 {
		t.Fatalf("source context mismatch: %+v", first.Sources[0])
	}
	for _, want := range []string{"producthunt", "today", "developer-tools", "open-source"} {
		if !hasString(first.Tags, want) {
			t.Fatalf("tags missing %q: %+v", want, first.Tags)
		}
	}
	second := items[1]
	if second.Sources[0].SourceRank != 2 || second.URL != second.CommentsURL {
		t.Fatalf("rank fallback or URL fallback mismatch: %+v source=%+v", second, second.Sources[0])
	}
}

func TestProductHuntNormalizeGraphQLErrors(t *testing.T) {
	_, err := NewProductHuntAdapter("token").Normalize(context.Background(), domain.FetchScope{Source: domain.SourceProductHunt, View: "today"}, &domain.FetchResult{
		Body: []byte(`{"errors":[{"message":"invalid token"}]}`),
	})
	if err == nil || !strings.Contains(err.Error(), "graphql") {
		t.Fatalf("expected graphql error, got %v", err)
	}
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

const productHuntFixture = `{
  "data": {
    "posts": {
      "nodes": [
        {
          "id": "101",
          "slug": "tildewire",
          "name": "Tildewire",
          "tagline": "Terminal radar for technical signals",
          "description": "A keyboard-first daily scan for developers.",
          "url": "https://www.producthunt.com/posts/tildewire",
          "website": "https://tildewire.dev?utm_source=producthunt",
          "votesCount": 120,
          "commentsCount": 8,
          "dailyRank": 2,
          "weeklyRank": 5,
          "createdAt": "2026-05-12T08:00:00Z",
          "featuredAt": "2026-05-12T09:00:00Z",
          "makers": [{"name": "Alice"}, {"username": "bob"}],
          "topics": {
            "nodes": [
              {"name": "Developer Tools", "slug": "developer-tools"},
              {"name": "Open Source", "slug": "open-source"}
            ]
          },
          "thumbnail": {"type": "image", "url": "https://img.producthunt.com/tildewire.png"},
          "productLinks": [{"type": "Website", "url": "https://tildewire.dev"}]
        },
        {
          "id": "102",
          "slug": "cached-cli",
          "name": "Cached CLI",
          "tagline": "Offline command cache",
          "description": "",
          "url": "https://www.producthunt.com/posts/cached-cli",
          "website": "",
          "votesCount": 12,
          "commentsCount": 0,
          "dailyRank": null,
          "weeklyRank": null,
          "createdAt": "2026-05-12T07:00:00Z",
          "featuredAt": null,
          "makers": [],
          "topics": {"nodes": []},
          "thumbnail": null,
          "productLinks": []
        }
      ]
    }
  }
}`

package sources

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AIluffy/tildewire/internal/domain"
	"github.com/AIluffy/tildewire/internal/httpcache"
	"github.com/AIluffy/tildewire/internal/httpx"
)

func TestGitHubTrendingNormalizeFixture(t *testing.T) {
	fetchedAt := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	raw := &domain.FetchResult{
		Source:    domain.SourceGitHub,
		FetchedAt: fetchedAt,
		Body:      []byte(githubTrendingFixture),
	}
	items, err := NewGitHubTrendingAdapter().Normalize(context.Background(), domain.FetchScope{
		Source: domain.SourceGitHub,
		View:   "trending",
		Period: "daily",
		Limit:  10,
	}, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	first := items[0]
	if first.Refs.Repo != "charmbracelet/bubbletea" || first.CanonicalKey != "repo:charmbracelet/bubbletea" {
		t.Fatalf("repo not normalized: %+v", first)
	}
	if first.ItemType != "repo" || first.Language != "Go" || first.Summary != "Powerful little TUI framework" {
		t.Fatalf("fields not normalized: %+v", first)
	}
	if first.Metrics.Stars == nil || *first.Metrics.Stars != 28123 {
		t.Fatalf("stars = %+v, want 28123", first.Metrics.Stars)
	}
	if first.Metrics.Forks == nil || *first.Metrics.Forks != 912 {
		t.Fatalf("forks = %+v, want 912", first.Metrics.Forks)
	}
	if first.Metrics.StarsToday == nil || *first.Metrics.StarsToday != 123 {
		t.Fatalf("stars today = %+v, want 123", first.Metrics.StarsToday)
	}
	if first.Sources[0].SourceRank != 1 || first.Sources[0].SourceView != "trending:daily" {
		t.Fatalf("source context mismatch: %+v", first.Sources[0])
	}
	second := items[1]
	if second.Refs.Repo != "openai/codex" || second.Language != "" || second.Summary != "" {
		t.Fatalf("missing optional fields not tolerated: %+v", second)
	}
	if second.Sources[0].SourceRank != 2 {
		t.Fatalf("rank = %d, want 2", second.Sources[0].SourceRank)
	}
}

func TestGitHubTrendingDefaultScopesCoverMVPViewsAndLanguages(t *testing.T) {
	scopes := NewGitHubTrendingAdapter().DefaultScopes()
	got := make(map[string]bool)
	for _, scope := range scopes {
		got[githubSourceView(scope)] = true
		if scope.Source != domain.SourceGitHub || scope.Limit <= 0 {
			t.Fatalf("invalid scope: %+v", scope)
		}
	}
	for _, sourceView := range []string{
		"trending:daily",
		"trending:weekly",
		"trending:daily:go",
		"trending:daily:rust",
		"trending:daily:python",
		"trending:daily:typescript",
	} {
		if !got[sourceView] {
			t.Fatalf("missing GitHub scope %q in %+v", sourceView, scopes)
		}
	}
}

func TestGitHubTrendingFetchBuildsURLs(t *testing.T) {
	tests := []struct {
		name            string
		scope           domain.FetchScope
		wantEscapedPath string
		wantSince       string
		wantSpoken      string
	}{
		{
			name:            "any language any spoken today",
			scope:           domain.FetchScope{Source: domain.SourceGitHub, View: "trending", Period: "daily"},
			wantEscapedPath: "/trending",
			wantSince:       "daily",
		},
		{
			name:            "weekly language",
			scope:           domain.FetchScope{Source: domain.SourceGitHub, View: "trending", Period: "weekly", Language: "TypeScript"},
			wantEscapedPath: "/trending/typescript",
			wantSince:       "weekly",
		},
		{
			name:            "monthly c sharp and chinese spoken language",
			scope:           domain.FetchScope{Source: domain.SourceGitHub, View: "trending", Period: "monthly", Language: "C#", SpokenLanguageCode: "ZH"},
			wantEscapedPath: "/trending/c%23",
			wantSince:       "monthly",
			wantSpoken:      "zh",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotEscapedPath, gotSince, gotSpoken string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotEscapedPath = r.URL.EscapedPath()
				gotSince = r.URL.Query().Get("since")
				gotSpoken = r.URL.Query().Get("spoken_language_code")
				_, _ = w.Write([]byte(githubTrendingFixture))
			}))
			defer server.Close()

			adapter := GitHubTrendingAdapter{BaseURL: server.URL}
			raw, err := adapter.Fetch(context.Background(), tt.scope, httpx.New(time.Second, newSourceTestCache()))
			if err != nil {
				t.Fatal(err)
			}
			if raw.Source != domain.SourceGitHub || raw.FromCache || raw.Stale {
				t.Fatalf("unexpected raw result: %+v", raw)
			}
			if gotEscapedPath != tt.wantEscapedPath || gotSince != tt.wantSince || gotSpoken != tt.wantSpoken {
				t.Fatalf("url path/since/spoken = %q/%q/%q, want %q/%q/%q", gotEscapedPath, gotSince, gotSpoken, tt.wantEscapedPath, tt.wantSince, tt.wantSpoken)
			}
		})
	}
}

func TestGitHubTrendingFetchUsesFreshCache(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(githubTrendingFixture))
	}))
	defer server.Close()

	cache := newSourceTestCache()
	client := httpx.New(time.Second, cache)
	adapter := GitHubTrendingAdapter{BaseURL: server.URL}
	scope := domain.FetchScope{Source: domain.SourceGitHub, View: "trending", Period: "daily"}
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
}

func TestGitHubTrendingFetchFallsBackToStale(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	defer server.Close()

	cache := newSourceTestCache()
	adapter := GitHubTrendingAdapter{BaseURL: server.URL}
	scope := domain.FetchScope{Source: domain.SourceGitHub, View: "trending", Period: "daily", ForceRefresh: true}
	requestURL := adapter.trendingURL(scope)
	fetchedAt := time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC)
	cache.entries[httpx.RequestKey(http.MethodGet, requestURL)] = httpcache.Entry{
		RequestKey: httpx.RequestKey(http.MethodGet, requestURL),
		Source:     string(domain.SourceGitHub),
		Method:     http.MethodGet,
		URL:        requestURL,
		StatusCode: 200,
		Body:       []byte(githubTrendingFixture),
		FetchedAt:  fetchedAt,
		ExpiresAt:  fetchedAt.Add(-time.Minute),
	}

	raw, err := adapter.Fetch(context.Background(), scope, httpx.New(time.Second, cache))
	if err != nil {
		t.Fatal(err)
	}
	if !raw.FromCache || !raw.Stale {
		t.Fatalf("expected stale cache fallback: %+v", raw)
	}
	items, err := adapter.Normalize(context.Background(), scope, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 || !items[0].LastSeenAt.Equal(fetchedAt) {
		t.Fatalf("stale normalize refreshed LastSeenAt: %+v", items)
	}
}

func TestGitHubTrendingDetailLoadsREADMEPreview(t *testing.T) {
	readme := "# owner/repo\n\nA README preview for terminal users."
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/readme" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"encoding":"base64","content":"` + base64.StdEncoding.EncodeToString([]byte(readme)) + `","html_url":"https://github.com/owner/repo#readme"}`))
	}))
	defer server.Close()

	adapter := GitHubTrendingAdapter{APIBaseURL: server.URL}
	entry := domain.FeedEntry{
		Item: domain.FeedItem{
			ID:    "repo-1",
			Title: "owner/repo",
			Refs:  domain.Refs{Repo: "owner/repo"},
		},
		Sources: []domain.ItemSource{{Source: domain.SourceGitHub, SourceIDRaw: "owner/repo"}},
	}
	detail, err := adapter.Detail(context.Background(), entry, httpx.New(time.Second, newSourceTestCache()))
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Sections) != 1 || detail.Sections[0].Title != "GitHub README" {
		t.Fatalf("README section missing: %+v", detail.Sections)
	}
	if detail.Sections[0].Body != readme || detail.Sections[0].URL != "https://github.com/owner/repo#readme" {
		t.Fatalf("README detail mismatch: %+v", detail.Sections[0])
	}
}

func TestGitHubTrendingDetailPrefersReadmeDownloadURL(t *testing.T) {
	readme := "# owner/repo\n\n![Screenshot](docs/screenshot.png)"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/readme" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"encoding":"base64","content":"` + base64.StdEncoding.EncodeToString([]byte(readme)) + `","html_url":"https://github.com/owner/repo#readme","download_url":"https://raw.githubusercontent.com/owner/repo/main/README.md"}`))
	}))
	defer server.Close()

	adapter := GitHubTrendingAdapter{APIBaseURL: server.URL}
	entry := domain.FeedEntry{
		Item: domain.FeedItem{
			ID:    "repo-1",
			Title: "owner/repo",
			Refs:  domain.Refs{Repo: "owner/repo"},
		},
		Sources: []domain.ItemSource{{Source: domain.SourceGitHub, SourceIDRaw: "owner/repo"}},
	}

	detail, err := adapter.Detail(context.Background(), entry, httpx.New(time.Second, newSourceTestCache()))
	if err != nil {
		t.Fatal(err)
	}

	if len(detail.Sections) != 1 || detail.Sections[0].URL != "https://raw.githubusercontent.com/owner/repo/main/README.md" {
		t.Fatalf("README URL = %+v, want download URL", detail.Sections)
	}
}

func TestGitHubTrendingDetailDoesNotTruncateREADME(t *testing.T) {
	readme := "# owner/repo\n\n" + strings.Repeat("full readme content ", 400) + "\nfinal-marker"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/readme" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"encoding":"base64","content":"` + base64.StdEncoding.EncodeToString([]byte(readme)) + `","html_url":"https://github.com/owner/repo#readme"}`))
	}))
	defer server.Close()

	adapter := GitHubTrendingAdapter{APIBaseURL: server.URL}
	entry := domain.FeedEntry{
		Item: domain.FeedItem{
			ID:    "repo-1",
			Title: "owner/repo",
			Refs:  domain.Refs{Repo: "owner/repo"},
		},
		Sources: []domain.ItemSource{{Source: domain.SourceGitHub, SourceIDRaw: "owner/repo"}},
	}
	detail, err := adapter.Detail(context.Background(), entry, httpx.New(time.Second, newSourceTestCache()))
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Sections) != 1 {
		t.Fatalf("README section missing: %+v", detail.Sections)
	}
	if detail.Sections[0].Body != readme {
		t.Fatalf("README was truncated: got %d bytes, want %d", len(detail.Sections[0].Body), len(readme))
	}
}

func TestGitHubTrendingDetailUsesTokenForReadmeRequest(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"encoding":"base64","content":"cmVhZG1l","html_url":"https://github.com/owner/repo#readme"}`))
	}))
	defer server.Close()

	adapter := GitHubTrendingAdapter{APIBaseURL: server.URL, Token: "github-token"}
	entry := domain.FeedEntry{
		Item: domain.FeedItem{
			ID:    "repo-1",
			Title: "owner/repo",
			Refs:  domain.Refs{Repo: "owner/repo"},
		},
		Sources: []domain.ItemSource{{Source: domain.SourceGitHub, SourceIDRaw: "owner/repo"}},
	}
	if _, err := adapter.Detail(context.Background(), entry, httpx.New(time.Second, newSourceTestCache())); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer github-token" {
		t.Fatalf("authorization header = %q, want bearer token", gotAuth)
	}
}

func TestGitHubTrendingTokenConcurrentWithDetail(t *testing.T) {
	payload := `{"encoding":"base64","content":"cmVhZG1l","html_url":"https://github.com/owner/repo#readme"}`
	adapter := GitHubTrendingAdapter{Token: "initial"}
	entry := domain.FeedEntry{
		Item: domain.FeedItem{
			ID:    "repo-1",
			Title: "owner/repo",
			Refs:  domain.Refs{Repo: "owner/repo"},
		},
		Sources: []domain.ItemSource{{Source: domain.SourceGitHub, SourceIDRaw: "owner/repo"}},
	}
	client := getterFunc(func(context.Context, httpx.GetOptions) (httpx.Response, error) {
		return httpx.Response{Body: []byte(payload), FetchedAt: time.Now().UTC()}, nil
	})

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
				if _, err := adapter.Detail(context.Background(), entry, client); err != nil {
					t.Errorf("Detail: %v", err)
				}
			}
		}()
	}
	wg.Wait()
}

type getterFunc func(context.Context, httpx.GetOptions) (httpx.Response, error)

func (f getterFunc) DoGET(ctx context.Context, options httpx.GetOptions) (httpx.Response, error) {
	return f(ctx, options)
}

func TestGitHubTrendingDetailUsesSeparateAPIRateLimitBucket(t *testing.T) {
	readme := "# owner/repo\n\nA README preview for terminal users."
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/trending":
			_, _ = w.Write([]byte(githubTrendingFixture))
		case "/repos/owner/repo/readme":
			_, _ = w.Write([]byte(`{"encoding":"base64","content":"` + base64.StdEncoding.EncodeToString([]byte(readme)) + `","html_url":"https://github.com/owner/repo#readme"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := httpx.New(time.Second, newSourceTestCache())
	adapter := GitHubTrendingAdapter{BaseURL: server.URL, APIBaseURL: server.URL}
	if _, err := adapter.Fetch(context.Background(), domain.FetchScope{Source: domain.SourceGitHub, View: "trending", Period: "daily"}, client); err != nil {
		t.Fatal(err)
	}
	entry := domain.FeedEntry{
		Item: domain.FeedItem{
			ID:    "repo-1",
			Title: "owner/repo",
			Refs:  domain.Refs{Repo: "owner/repo"},
		},
		Sources: []domain.ItemSource{{Source: domain.SourceGitHub, SourceIDRaw: "owner/repo"}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	detail, err := adapter.Detail(ctx, entry, client)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Sections) != 1 || detail.Sections[0].Body != readme {
		t.Fatalf("README detail mismatch: %+v", detail.Sections)
	}
}

const githubTrendingFixture = `
<html>
  <body>
    <article class="Box-row">
      <h2>
        <a href="/Charmbracelet/BubbleTea">
          <span>charmbracelet</span> / <span>bubbletea</span>
        </a>
      </h2>
      <p>Powerful little TUI framework</p>
      <span itemprop="programmingLanguage">Go</span>
      <a href="/charmbracelet/bubbletea/stargazers">28,123</a>
      <a href="/charmbracelet/bubbletea/forks">912</a>
      <span class="d-inline-block float-sm-right">123 stars today</span>
    </article>
    <article class="Box-row">
      <h2><a href="/OpenAI/Codex">openai / codex</a></h2>
      <a href="/openai/codex/stargazers">1.2k</a>
      <a href="/openai/codex/forks">77</a>
    </article>
  </body>
</html>`

package sources

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/AIluffy/tildewire/internal/domain"
	"github.com/AIluffy/tildewire/internal/httpx"
	"github.com/AIluffy/tildewire/internal/normalize"
)

const githubTrendingBaseURL = "https://github.com"
const githubAPIBaseURL = "https://api.github.com"
const githubAPIRateLimitBucket = "github_api"

var starsSincePattern = regexp.MustCompile(`(?i)([\d,.]+(?:[km])?)\s+stars?\s+(?:today|this week)`)

// GitHubTrendingAdapter fetches GitHub Trending repository pages.
type GitHubTrendingAdapter struct {
	BaseURL    string
	APIBaseURL string
	Token      string
	tokenMu    sync.RWMutex
}

// NewGitHubTrendingAdapter creates the GitHub Trending source adapter.
func NewGitHubTrendingAdapter(tokens ...string) *GitHubTrendingAdapter {
	token := ""
	if len(tokens) > 0 {
		token = strings.TrimSpace(tokens[0])
	}
	return &GitHubTrendingAdapter{BaseURL: githubTrendingBaseURL, APIBaseURL: githubAPIBaseURL, Token: token}
}

// SetToken updates the token used for GitHub REST detail requests.
func (a *GitHubTrendingAdapter) SetToken(token string) {
	a.tokenMu.Lock()
	defer a.tokenMu.Unlock()
	a.Token = strings.TrimSpace(token)
}

// Source returns the adapter source id.
func (a *GitHubTrendingAdapter) Source() domain.SourceID {
	return domain.SourceGitHub
}

// DefaultScopes returns the GitHub scopes refreshed by the MVP.
func (a *GitHubTrendingAdapter) DefaultScopes() []domain.FetchScope {
	return []domain.FetchScope{
		{Source: domain.SourceGitHub, View: "trending", Period: "daily", Limit: 25},
		{Source: domain.SourceGitHub, View: "trending", Period: "weekly", Limit: 25},
		{Source: domain.SourceGitHub, View: "trending", Period: "daily", Language: "go", Limit: 25},
		{Source: domain.SourceGitHub, View: "trending", Period: "daily", Language: "rust", Limit: 25},
		{Source: domain.SourceGitHub, View: "trending", Period: "daily", Language: "python", Limit: 25},
		{Source: domain.SourceGitHub, View: "trending", Period: "daily", Language: "typescript", Limit: 25},
	}
}

// Fetch downloads one GitHub Trending HTML page.
func (a *GitHubTrendingAdapter) Fetch(ctx context.Context, scope domain.FetchScope, client httpx.Requester) (*domain.FetchResult, error) {
	scope = normalize.NormalizeGitHubScope(scope)
	resp, err := client.DoGET(ctx, httpx.GetOptions{
		Source:       string(domain.SourceGitHub),
		URL:          a.trendingURL(scope),
		TTL:          a.CachePolicy(scope).TTL,
		ForceRefresh: scope.ForceRefresh,
	})
	if err != nil {
		return nil, err
	}
	return &domain.FetchResult{
		Source:      domain.SourceGitHub,
		Scope:       scope,
		StatusCode:  resp.StatusCode,
		Body:        resp.Body,
		FetchedAt:   resp.FetchedAt,
		FromCache:   resp.FromCache,
		Stale:       resp.Stale,
		StaleReason: resp.StaleReason,
	}, nil
}

// Normalize converts GitHub Trending HTML into FeedItems.
func (a *GitHubTrendingAdapter) Normalize(_ context.Context, scope domain.FetchScope, raw *domain.FetchResult) ([]domain.FeedItem, error) {
	if raw == nil {
		return nil, fmt.Errorf("github raw response is nil")
	}
	scope = normalize.NormalizeGitHubScope(scope)
	items, err := parseGitHubTrending(raw.Body, scope, raw.FetchedAt, a.baseURL())
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("no github trending repositories found")
	}
	return items, nil
}

// CachePolicy returns GitHub Trending TTLs.
func (a *GitHubTrendingAdapter) CachePolicy(scope domain.FetchScope) domain.CachePolicy {
	period := strings.ToLower(strings.TrimSpace(scope.Period))
	if period == "weekly" || period == "monthly" {
		return domain.CachePolicy{TTL: 2 * time.Hour}
	}
	return domain.CachePolicy{TTL: 30 * time.Minute}
}

// Detail loads a GitHub README preview for repository items.
func (a *GitHubTrendingAdapter) Detail(ctx context.Context, entry domain.FeedEntry, client httpx.Getter) (domain.ItemDetail, error) {
	repo := normalize.NormalizeRepo(entry.Item.Refs.Repo)
	if repo == "" {
		return domain.ItemDetail{}, fmt.Errorf("github repo is missing")
	}
	token := a.token()
	resp, err := client.DoGET(ctx, httpx.GetOptions{
		Source:          string(domain.SourceGitHub),
		RateLimitBucket: githubAPIRateLimitBucket,
		URL:             a.readmeURL(repo),
		TTL:             6 * time.Hour,
		BearerToken:     token,
	})
	if err != nil {
		return domain.ItemDetail{}, err
	}
	var payload githubReadmePayload
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		return domain.ItemDetail{}, fmt.Errorf("decode github readme: %w", err)
	}
	body, err := decodeGitHubReadme(payload)
	if err != nil {
		return domain.ItemDetail{}, err
	}
	return domain.ItemDetail{
		ItemID:   entry.Item.ID,
		Title:    entry.Item.Title,
		URL:      entry.Item.URL,
		LoadedAt: resp.FetchedAt,
		Sections: []domain.DetailSection{{
			Title:  "GitHub README",
			Body:   body,
			URL:    githubReadmeSectionURL(payload),
			Source: domain.SourceGitHub,
		}},
	}, nil
}

func (a *GitHubTrendingAdapter) trendingURL(scope domain.FetchScope) string {
	scope = normalize.NormalizeGitHubScope(scope)
	path := "/trending"
	if scope.Language != "" {
		path += "/" + scope.Language
	}
	u, err := url.Parse(a.baseURL())
	if err != nil {
		return a.baseURL() + path
	}
	u.Path = strings.TrimRight(u.Path, "/") + path
	query := u.Query()
	query.Set("since", scope.Period)
	if scope.SpokenLanguageCode != "" {
		query.Set("spoken_language_code", scope.SpokenLanguageCode)
	}
	u.RawQuery = query.Encode()
	return u.String()
}

func (a *GitHubTrendingAdapter) baseURL() string {
	if strings.TrimSpace(a.BaseURL) == "" {
		return githubTrendingBaseURL
	}
	return strings.TrimRight(a.BaseURL, "/")
}

func (a *GitHubTrendingAdapter) apiBaseURL() string {
	if strings.TrimSpace(a.APIBaseURL) == "" {
		return githubAPIBaseURL
	}
	return strings.TrimRight(a.APIBaseURL, "/")
}

func (a *GitHubTrendingAdapter) readmeURL(repo string) string {
	return a.apiBaseURL() + "/repos/" + repo + "/readme"
}

func (a *GitHubTrendingAdapter) token() string {
	a.tokenMu.RLock()
	defer a.tokenMu.RUnlock()
	return strings.TrimSpace(a.Token)
}

type githubReadmePayload struct {
	Content     string `json:"content"`
	Encoding    string `json:"encoding"`
	HTMLURL     string `json:"html_url"`
	DownloadURL string `json:"download_url"`
}

func githubReadmeSectionURL(payload githubReadmePayload) string {
	if strings.TrimSpace(payload.DownloadURL) != "" {
		return strings.TrimSpace(payload.DownloadURL)
	}
	return strings.TrimSpace(payload.HTMLURL)
}

func decodeGitHubReadme(payload githubReadmePayload) (string, error) {
	if strings.ToLower(strings.TrimSpace(payload.Encoding)) != "base64" {
		return "", fmt.Errorf("unsupported github readme encoding %q", payload.Encoding)
	}
	encoded := strings.NewReplacer("\n", "", "\r", "", " ", "").Replace(payload.Content)
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func parseGitHubTrending(body []byte, scope domain.FetchScope, fetchedAt time.Time, baseURL string) ([]domain.FeedItem, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse github trending html: %w", err)
	}
	scope = normalize.NormalizeGitHubScope(scope)
	if fetchedAt.IsZero() {
		fetchedAt = time.Now().UTC()
	}
	repositories := doc.Find("article.Box-row")
	if repositories.Length() == 0 {
		repositories = doc.Find("article")
	}

	items := make([]domain.FeedItem, 0, repositories.Length())
	repositories.Each(func(idx int, article *goquery.Selection) {
		if scope.Limit > 0 && len(items) >= scope.Limit {
			return
		}
		repo := repoFromArticle(article)
		if repo == "" {
			return
		}
		item := githubRepoToFeedItem(article, scope, repo, idx+1, fetchedAt, strings.TrimRight(baseURL, "/"))
		items = append(items, item)
	})
	return items, nil
}

func repoFromArticle(article *goquery.Selection) string {
	link := article.Find("h2 a").First()
	if href, ok := link.Attr("href"); ok {
		if repo, ok := normalize.RepoFromPath(href); ok {
			return repo
		}
	}
	text := strings.Join(strings.Fields(link.Text()), "")
	return normalize.NormalizeRepo(text)
}

func githubRepoToFeedItem(article *goquery.Selection, scope domain.FetchScope, repo string, rank int, seenAt time.Time, baseURL string) domain.FeedItem {
	description := cleanText(article.Find("p").First().Text())
	language := cleanText(article.Find("[itemprop='programmingLanguage']").First().Text())
	stars := countFromRepoLink(article, "/stargazers")
	forks := countFromRepoLink(article, "/forks")
	starsSince := starsSinceCount(article.Text())
	repoURL := baseURL + "/" + repo
	canonicalKey := normalize.RepoKey(repo)
	itemID := normalize.StableID(canonicalKey)
	raw, _ := json.Marshal(githubTrendingRaw{
		Repo:               repo,
		Period:             scope.Period,
		Language:           language,
		SpokenLanguageCode: scope.SpokenLanguageCode,
		Rank:               rank,
	})
	metrics := domain.Metrics{
		Stars:      int64PtrIfPositive(stars),
		Forks:      int64PtrIfPositive(forks),
		StarsToday: int64PtrIfPositive(starsSince),
	}
	source := domain.ItemSource{
		ItemID:      itemID,
		Source:      domain.SourceGitHub,
		SourceView:  githubSourceView(scope),
		SourceIDRaw: repo,
		SourceRank:  rank,
		SourceURL:   repoURL,
		Metrics:     metrics,
		Raw:         raw,
		SeenAt:      seenAt,
	}
	return domain.FeedItem{
		ID:           itemID,
		CanonicalKey: canonicalKey,
		Title:        repo,
		Subtitle:     githubSubtitle(language, stars, forks, starsSince, scope.Period),
		Summary:      description,
		URL:          repoURL,
		CanonicalURL: repoURL,
		ItemType:     "repo",
		Language:     language,
		Tags:         githubTags(scope, language),
		FirstSeenAt:  seenAt,
		LastSeenAt:   seenAt,
		Metrics:      metrics,
		Refs:         domain.Refs{Repo: repo},
		Metadata:     raw,
		Sources:      []domain.ItemSource{source},
	}
}

type githubTrendingRaw struct {
	Repo               string `json:"repo"`
	Period             string `json:"period"`
	Language           string `json:"language,omitempty"`
	SpokenLanguageCode string `json:"spoken_language_code,omitempty"`
	Rank               int    `json:"rank"`
}

func githubSourceView(scope domain.FetchScope) string {
	view := scope.View
	if view == "" {
		view = "trending"
	}
	period := scope.Period
	if period == "" {
		period = "daily"
	}
	parts := []string{view, period}
	if scope.Language != "" {
		parts = append(parts, scope.Language)
	}
	if scope.SpokenLanguageCode != "" {
		parts = append(parts, "spoken", scope.SpokenLanguageCode)
	}
	return strings.Join(parts, ":")
}

func countFromRepoLink(article *goquery.Selection, suffix string) int64 {
	var value int64
	article.Find("a").EachWithBreak(func(_ int, link *goquery.Selection) bool {
		href, _ := link.Attr("href")
		href = strings.TrimSuffix(href, "/")
		if !strings.HasSuffix(href, suffix) {
			return true
		}
		count, ok := parseGitHubCount(link.Text())
		if !ok {
			return true
		}
		value = count
		return false
	})
	return value
}

func starsSinceCount(text string) int64 {
	match := starsSincePattern.FindStringSubmatch(text)
	if len(match) != 2 {
		return 0
	}
	count, ok := parseGitHubCount(match[1])
	if !ok {
		return 0
	}
	return count
}

func parseGitHubCount(raw string) (int64, bool) {
	value := strings.ToLower(cleanText(raw))
	value = strings.ReplaceAll(value, ",", "")
	value = strings.ReplaceAll(value, " ", "")
	if value == "" {
		return 0, false
	}
	multiplier := float64(1)
	switch {
	case strings.HasSuffix(value, "k"):
		multiplier = 1000
		value = strings.TrimSuffix(value, "k")
	case strings.HasSuffix(value, "m"):
		multiplier = 1000000
		value = strings.TrimSuffix(value, "m")
	}
	number, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, false
	}
	return int64(math.Round(number * multiplier)), true
}

func githubSubtitle(language string, stars, forks, starsSince int64, period string) string {
	var parts []string
	if language != "" {
		parts = append(parts, language)
	}
	if stars > 0 {
		parts = append(parts, fmt.Sprintf("%d stars", stars))
	}
	if forks > 0 {
		parts = append(parts, fmt.Sprintf("%d forks", forks))
	}
	if starsSince > 0 {
		label := "today"
		if period == "weekly" {
			label = "this week"
		}
		parts = append(parts, fmt.Sprintf("%d stars %s", starsSince, label))
	}
	return strings.Join(parts, " · ")
}

func githubTags(scope domain.FetchScope, language string) []string {
	tags := []string{"github", "trending", scope.Period}
	if language != "" {
		tags = append(tags, strings.ToLower(language))
	}
	return tags
}

func cleanText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func int64PtrIfPositive(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	return domain.Int64Ptr(value)
}

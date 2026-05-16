package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/AIluffy/tildewire/internal/domain"
	"github.com/AIluffy/tildewire/internal/httpx"
	"github.com/AIluffy/tildewire/internal/normalize"
)

const huggingFaceBaseURL = "https://huggingface.co"

// HuggingFacePapersAdapter fetches Hugging Face Daily Papers.
type HuggingFacePapersAdapter struct {
	BaseURL string
}

// NewHuggingFacePapersAdapter creates the Hugging Face Papers source adapter.
func NewHuggingFacePapersAdapter() HuggingFacePapersAdapter {
	return HuggingFacePapersAdapter{BaseURL: huggingFaceBaseURL}
}

// Source returns the adapter source id.
func (a HuggingFacePapersAdapter) Source() domain.SourceID {
	return domain.SourceHuggingFace
}

// DefaultScopes returns the Hugging Face scopes refreshed by the MVP.
func (a HuggingFacePapersAdapter) DefaultScopes() []domain.FetchScope {
	return []domain.FetchScope{
		{Source: domain.SourceHuggingFace, View: "daily", Limit: 50},
	}
}

// Fetch downloads one Hugging Face Daily Papers page.
func (a HuggingFacePapersAdapter) Fetch(ctx context.Context, scope domain.FetchScope, client *httpx.Client) (*domain.FetchResult, error) {
	scope = normalizeHuggingFaceScope(scope)
	resp, err := client.DoGET(ctx, httpx.GetOptions{
		Source:       string(domain.SourceHuggingFace),
		URL:          a.dailyPapersURL(scope),
		TTL:          a.CachePolicy(scope).TTL,
		ForceRefresh: scope.ForceRefresh,
	})
	if err != nil {
		return nil, err
	}
	return &domain.FetchResult{
		Source:      domain.SourceHuggingFace,
		Scope:       scope,
		StatusCode:  resp.StatusCode,
		Body:        resp.Body,
		FetchedAt:   resp.FetchedAt,
		FromCache:   resp.FromCache,
		Stale:       resp.Stale,
		StaleReason: resp.StaleReason,
	}, nil
}

// Normalize converts Hugging Face Daily Papers JSON into FeedItems.
func (a HuggingFacePapersAdapter) Normalize(_ context.Context, scope domain.FetchScope, raw *domain.FetchResult) ([]domain.FeedItem, error) {
	if raw == nil {
		return nil, fmt.Errorf("huggingface raw response is nil")
	}
	scope = normalizeHuggingFaceScope(scope)
	var entries []huggingFaceDailyPaper
	if err := json.Unmarshal(raw.Body, &entries); err != nil {
		return nil, fmt.Errorf("decode huggingface daily papers: %w", err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no huggingface papers found")
	}
	fetchedAt := raw.FetchedAt
	if fetchedAt.IsZero() {
		fetchedAt = time.Now().UTC()
	}
	items := make([]domain.FeedItem, 0, len(entries))
	for idx, entry := range entries {
		item, ok := a.dailyPaperToFeedItem(entry, scope, idx+1, fetchedAt)
		if ok {
			items = append(items, item)
		}
	}
	return items, nil
}

// CachePolicy returns Hugging Face Daily Papers TTLs.
func (a HuggingFacePapersAdapter) CachePolicy(domain.FetchScope) domain.CachePolicy {
	return domain.CachePolicy{TTL: time.Hour}
}

// Detail expands Hugging Face paper metadata already stored with the item.
func (a HuggingFacePapersAdapter) Detail(_ context.Context, entry domain.FeedEntry, _ *httpx.Client) (domain.ItemDetail, error) {
	if entry.Item.Refs.PaperID == "" && entry.Item.Refs.ArxivID == "" {
		return domain.ItemDetail{}, fmt.Errorf("huggingface paper id is missing")
	}
	var raw huggingFaceDailyPaper
	if len(entry.Item.Metadata) > 0 {
		_ = json.Unmarshal(entry.Item.Metadata, &raw)
	}
	paperID := firstNonEmpty(entry.Item.Refs.PaperID, raw.Paper.ID, entry.Item.Refs.ArxivID)
	var details []string
	if paperID != "" {
		details = append(details, "Paper: "+paperID)
	}
	if entry.Item.Refs.ArxivID != "" {
		details = append(details, "arXiv: "+entry.Item.Refs.ArxivID)
	}
	if entry.Item.Author != "" {
		details = append(details, "Authors: "+entry.Item.Author)
	}
	if raw.Paper.ProjectPage != "" {
		details = append(details, "Project: "+raw.Paper.ProjectPage)
	}
	var links []string
	if entry.Item.Refs.Repo != "" {
		links = append(links, "Repository: "+entry.Item.Refs.Repo)
	}
	keywords := entry.Item.Tags
	if len(raw.Paper.AIKeywords) > 0 {
		keywords = raw.Paper.AIKeywords
	}
	if len(keywords) > 0 {
		links = append(links, "Keywords: "+strings.Join(keywords, ", "))
	}
	sections := []domain.DetailSection{{
		Title:  "Paper Details",
		Body:   strings.Join(details, "\n"),
		URL:    entry.Item.URL,
		Source: domain.SourceHuggingFace,
	}}
	if len(links) > 0 {
		sections = append(sections, domain.DetailSection{
			Title:  "Related Links",
			Body:   strings.Join(links, "\n"),
			Source: domain.SourceHuggingFace,
		})
	}
	return domain.ItemDetail{
		ItemID:   entry.Item.ID,
		Title:    entry.Item.Title,
		URL:      entry.Item.URL,
		LoadedAt: time.Now().UTC(),
		Sections: sections,
	}, nil
}

func (a HuggingFacePapersAdapter) dailyPapersURL(scope domain.FetchScope) string {
	scope = normalizeHuggingFaceScope(scope)
	u, err := url.Parse(a.baseURL() + "/api/daily_papers")
	if err != nil {
		return a.baseURL() + "/api/daily_papers"
	}
	query := u.Query()
	query.Set("limit", strconv.Itoa(scope.Limit))
	for _, key := range []string{"date", "week", "month", "submitter", "sort"} {
		if value := strings.TrimSpace(scope.Params[key]); value != "" {
			query.Set(key, value)
		}
	}
	u.RawQuery = query.Encode()
	return u.String()
}

func (a HuggingFacePapersAdapter) baseURL() string {
	if strings.TrimSpace(a.BaseURL) == "" {
		return huggingFaceBaseURL
	}
	return strings.TrimRight(a.BaseURL, "/")
}

func (a HuggingFacePapersAdapter) dailyPaperToFeedItem(entry huggingFaceDailyPaper, scope domain.FetchScope, rank int, seenAt time.Time) (domain.FeedItem, bool) {
	paperID := strings.TrimSpace(entry.Paper.ID)
	title := firstNonEmpty(entry.Title, entry.Paper.Title)
	if paperID == "" || strings.TrimSpace(title) == "" {
		return domain.FeedItem{}, false
	}
	summary := firstNonEmpty(entry.Summary, entry.Paper.Summary)
	paperURL := a.baseURL() + "/papers/" + paperID
	commentsURL := paperURL + "#discussion"
	canonicalKey, arxivID := huggingFaceCanonicalKey(paperID)
	itemID := normalize.StableID(canonicalKey)
	published := parseHFTime(firstNonEmpty(entry.PublishedAt, entry.Paper.PublishedAt))
	authors := visibleHFAuthors(entry.Paper.Authors)
	repo := huggingFaceRepo(entry.Paper.GitHubRepo)
	raw, _ := json.Marshal(entry)
	metrics := domain.Metrics{
		Upvotes:     int64PtrIfPositive(int64(entry.Paper.Upvotes)),
		Comments:    int64PtrIfPositive(int64(entry.NumComments)),
		GitHubStars: int64PtrIfPositive(int64(entry.Paper.GitHubStars)),
	}
	source := domain.ItemSource{
		ItemID:      itemID,
		Source:      domain.SourceHuggingFace,
		SourceView:  scope.View,
		SourceIDRaw: paperID,
		SourceRank:  rank,
		SourceURL:   paperURL,
		Metrics:     metrics,
		Raw:         raw,
		SeenAt:      seenAt,
	}
	return domain.FeedItem{
		ID:           itemID,
		CanonicalKey: canonicalKey,
		Title:        title,
		Subtitle:     huggingFaceSubtitle(metrics, authors),
		Summary:      summary,
		URL:          paperURL,
		CanonicalURL: paperURL,
		CommentsURL:  commentsURL,
		ItemType:     "paper",
		Author:       authors,
		Tags:         huggingFaceTags(scope, entry.Paper.AIKeywords),
		PublishedAt:  published,
		FirstSeenAt:  seenAt,
		LastSeenAt:   seenAt,
		Metrics:      metrics,
		Refs: domain.Refs{
			Repo:    repo,
			ArxivID: arxivID,
			PaperID: paperID,
		},
		Metadata: raw,
		Sources:  []domain.ItemSource{source},
	}, true
}

type huggingFaceDailyPaper struct {
	Paper       huggingFacePaper `json:"paper"`
	PublishedAt string           `json:"publishedAt"`
	Title       string           `json:"title"`
	Summary     string           `json:"summary"`
	NumComments int64            `json:"numComments"`
}

type huggingFacePaper struct {
	ID           string              `json:"id"`
	Authors      []huggingFaceAuthor `json:"authors"`
	PublishedAt  string              `json:"publishedAt"`
	Title        string              `json:"title"`
	Summary      string              `json:"summary"`
	Upvotes      int64               `json:"upvotes"`
	DiscussionID string              `json:"discussionId"`
	ProjectPage  string              `json:"projectPage"`
	GitHubRepo   string              `json:"githubRepo"`
	AIKeywords   []string            `json:"ai_keywords"`
	GitHubStars  int64               `json:"githubStars"`
}

type huggingFaceAuthor struct {
	Name   string `json:"name"`
	Hidden bool   `json:"hidden"`
}

func normalizeHuggingFaceScope(scope domain.FetchScope) domain.FetchScope {
	scope.Source = domain.SourceHuggingFace
	if strings.TrimSpace(scope.View) == "" {
		scope.View = "daily"
	}
	if scope.Limit <= 0 {
		scope.Limit = 50
	}
	if scope.Limit > 100 {
		scope.Limit = 100
	}
	return scope
}

func huggingFaceCanonicalKey(paperID string) (string, string) {
	arxivID := normalize.NormalizeArxivID(paperID)
	if arxivID != "" {
		return "arxiv:" + arxivID, arxivID
	}
	normalized := strings.ToLower(strings.TrimSpace(paperID))
	return "paper:" + normalized, ""
}

func visibleHFAuthors(authors []huggingFaceAuthor) string {
	names := make([]string, 0, len(authors))
	for _, author := range authors {
		name := strings.TrimSpace(author.Name)
		if name == "" || author.Hidden {
			continue
		}
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}

func huggingFaceRepo(value string) string {
	repo, ok := normalize.RepoFromPath(value)
	if !ok {
		return ""
	}
	return repo
}

func huggingFaceSubtitle(metrics domain.Metrics, authors string) string {
	var parts []string
	if metrics.Upvotes != nil {
		parts = append(parts, fmt.Sprintf("%d upvotes", *metrics.Upvotes))
	}
	if metrics.Comments != nil {
		parts = append(parts, fmt.Sprintf("%d comments", *metrics.Comments))
	}
	if authors != "" {
		parts = append(parts, authors)
	}
	return strings.Join(parts, " · ")
}

func huggingFaceTags(scope domain.FetchScope, keywords []string) []string {
	tags := []string{"hf", "papers", scope.View}
	for _, keyword := range keywords {
		keyword = strings.ToLower(strings.TrimSpace(keyword))
		if keyword != "" {
			tags = append(tags, keyword)
		}
	}
	return tags
}

func parseHFTime(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			parsed = parsed.UTC()
			return &parsed
		}
	}
	return nil
}

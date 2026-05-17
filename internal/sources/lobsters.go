package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/AIluffy/tildewire/internal/domain"
	"github.com/AIluffy/tildewire/internal/httpx"
	"github.com/AIluffy/tildewire/internal/normalize"
)

const lobstersBaseURL = "https://lobste.rs"

// LobstersAdapter fetches Lobsters story lists and story details.
type LobstersAdapter struct {
	BaseURL string
}

// NewLobstersAdapter creates the Lobsters source adapter.
func NewLobstersAdapter() LobstersAdapter {
	return LobstersAdapter{BaseURL: lobstersBaseURL}
}

// Source returns the adapter source id.
func (a LobstersAdapter) Source() domain.SourceID {
	return domain.SourceLobsters
}

// DefaultScopes returns Lobsters scopes for v0.2.
func (a LobstersAdapter) DefaultScopes() []domain.FetchScope {
	return []domain.FetchScope{
		{Source: domain.SourceLobsters, View: "hottest", Limit: 50},
		{Source: domain.SourceLobsters, View: "newest", Limit: 50},
	}
}

// Fetch downloads one Lobsters JSON page.
func (a LobstersAdapter) Fetch(ctx context.Context, scope domain.FetchScope, client httpx.Requester) (*domain.FetchResult, error) {
	scope = normalizeLobstersScope(scope)
	resp, err := client.DoGET(ctx, httpx.GetOptions{
		Source:       string(domain.SourceLobsters),
		URL:          a.listURL(scope),
		TTL:          a.CachePolicy(scope).TTL,
		ForceRefresh: scope.ForceRefresh,
	})
	if err != nil {
		return nil, err
	}
	return &domain.FetchResult{
		Source:      domain.SourceLobsters,
		Scope:       scope,
		StatusCode:  resp.StatusCode,
		Body:        resp.Body,
		FetchedAt:   resp.FetchedAt,
		FromCache:   resp.FromCache,
		Stale:       resp.Stale,
		StaleReason: resp.StaleReason,
	}, nil
}

// Normalize converts Lobsters JSON into FeedItems.
func (a LobstersAdapter) Normalize(_ context.Context, scope domain.FetchScope, raw *domain.FetchResult) ([]domain.FeedItem, error) {
	if raw == nil {
		return nil, fmt.Errorf("lobsters raw response is nil")
	}
	scope = normalizeLobstersScope(scope)
	var stories []lobstersStory
	if err := json.Unmarshal(raw.Body, &stories); err != nil {
		return nil, fmt.Errorf("decode lobsters stories: %w", err)
	}
	if len(stories) == 0 {
		return nil, fmt.Errorf("no lobsters stories found")
	}
	fetchedAt := raw.FetchedAt
	if fetchedAt.IsZero() {
		fetchedAt = time.Now().UTC()
	}
	items := make([]domain.FeedItem, 0, len(stories))
	for idx, story := range stories {
		if scope.Limit > 0 && len(items) >= scope.Limit {
			break
		}
		item, ok := a.storyToFeedItem(story, scope, idx+1, fetchedAt)
		if ok {
			items = append(items, item)
		}
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("no usable lobsters stories found")
	}
	return items, nil
}

// CachePolicy returns Lobsters TTLs.
func (a LobstersAdapter) CachePolicy(scope domain.FetchScope) domain.CachePolicy {
	scope = normalizeLobstersScope(scope)
	if scope.View == "newest" {
		return domain.CachePolicy{TTL: 10 * time.Minute}
	}
	return domain.CachePolicy{TTL: 15 * time.Minute}
}

// Detail loads one Lobsters story and returns top-level comments.
func (a LobstersAdapter) Detail(ctx context.Context, entry domain.FeedEntry, client httpx.Getter) (domain.ItemDetail, error) {
	id := lobstersEntryID(entry)
	if id == "" {
		return domain.ItemDetail{}, fmt.Errorf("lobsters id is missing")
	}
	resp, err := client.DoGET(ctx, httpx.GetOptions{
		Source: string(domain.SourceLobsters),
		URL:    a.storyURL(id),
		TTL:    10 * time.Minute,
	})
	if err != nil {
		return domain.ItemDetail{}, err
	}
	var story lobstersStory
	if err := json.Unmarshal(resp.Body, &story); err != nil {
		return domain.ItemDetail{}, fmt.Errorf("decode lobsters story detail: %w", err)
	}
	detail := domain.ItemDetail{
		ItemID:   entry.Item.ID,
		Title:    firstNonEmpty(story.Title, entry.Item.Title),
		URL:      firstNonEmpty(story.ShortIDURL, entry.Item.CommentsURL, entry.Item.URL),
		LoadedAt: resp.FetchedAt,
	}
	for _, comment := range story.Comments {
		if comment.Depth != 0 || strings.TrimSpace(comment.Comment) == "" {
			continue
		}
		body := plainHTMLText(comment.Comment)
		if body == "" {
			continue
		}
		published := parseLobstersTime(comment.CreatedAt)
		detail.Comments = append(detail.Comments, domain.DetailComment{
			Author:      lobstersUserName(comment.CommentingUser),
			Body:        body,
			URL:         comment.ShortIDURL,
			Score:       int64PtrIfPositive(comment.Score),
			PublishedAt: published,
			Source:      domain.SourceLobsters,
		})
		if len(detail.Comments) >= 8 {
			break
		}
	}
	return detail, nil
}

type lobstersStory struct {
	ShortID       string            `json:"short_id"`
	ShortIDURL    string            `json:"short_id_url"`
	CreatedAt     string            `json:"created_at"`
	Title         string            `json:"title"`
	URL           string            `json:"url"`
	Score         int64             `json:"score"`
	CommentCount  int64             `json:"comment_count"`
	Description   string            `json:"description"`
	SubmitterUser json.RawMessage   `json:"submitter_user"`
	Tags          []json.RawMessage `json:"tags"`
	Comments      []lobstersComment `json:"comments"`
}

type lobstersComment struct {
	ShortID        string          `json:"short_id"`
	ShortIDURL     string          `json:"short_id_url"`
	Comment        string          `json:"comment"`
	CommentingUser json.RawMessage `json:"commenting_user"`
	Score          int64           `json:"score"`
	CreatedAt      string          `json:"created_at"`
	Depth          int             `json:"indent_level"`
}

func (a LobstersAdapter) storyToFeedItem(story lobstersStory, scope domain.FetchScope, rank int, seenAt time.Time) (domain.FeedItem, bool) {
	id := strings.TrimSpace(story.ShortID)
	title := strings.TrimSpace(story.Title)
	if id == "" || title == "" {
		return domain.FeedItem{}, false
	}
	commentsURL := firstNonEmpty(story.ShortIDURL, a.baseURL()+"/s/"+id)
	itemURL := firstNonEmpty(story.URL, commentsURL)
	canonicalURL := normalize.CanonicalURL(itemURL)
	canonicalKey := ""
	if story.URL != "" && canonicalURL != "" {
		canonicalKey = "url:" + canonicalURL
	} else {
		canonicalURL = commentsURL
		canonicalKey = "lobsters:" + strings.ToLower(id)
	}
	itemID := normalize.StableID(canonicalKey)
	published := parseLobstersTime(story.CreatedAt)
	tags := lobstersTags(scope, story.Tags)
	raw, _ := json.Marshal(story)
	score := float64(story.Score)
	metrics := domain.Metrics{
		Score:    float64PtrIfPositive(score),
		Upvotes:  int64PtrIfPositive(story.Score),
		Comments: int64PtrIfPositive(story.CommentCount),
	}
	source := domain.ItemSource{
		ItemID:      itemID,
		Source:      domain.SourceLobsters,
		SourceView:  scope.View,
		SourceIDRaw: id,
		SourceRank:  rank,
		SourceURL:   commentsURL,
		Metrics:     metrics,
		Raw:         raw,
		SeenAt:      seenAt,
	}
	return domain.FeedItem{
		ID:           itemID,
		CanonicalKey: canonicalKey,
		Title:        title,
		Subtitle:     lobstersSubtitle(metrics, tags),
		Summary:      strings.TrimSpace(story.Description),
		URL:          itemURL,
		CanonicalURL: canonicalURL,
		CommentsURL:  commentsURL,
		ItemType:     "story",
		Author:       lobstersUserName(story.SubmitterUser),
		Tags:         tags,
		PublishedAt:  published,
		FirstSeenAt:  seenAt,
		LastSeenAt:   seenAt,
		Metrics:      metrics,
		Refs:         domain.Refs{LobstersID: id},
		Metadata:     raw,
		Sources:      []domain.ItemSource{source},
	}, true
}

func normalizeLobstersScope(scope domain.FetchScope) domain.FetchScope {
	scope.Source = domain.SourceLobsters
	switch strings.ToLower(strings.TrimSpace(scope.View)) {
	case "newest":
		scope.View = "newest"
	default:
		scope.View = "hottest"
	}
	if scope.Limit <= 0 {
		scope.Limit = 50
	}
	return scope
}

func (a LobstersAdapter) listURL(scope domain.FetchScope) string {
	scope = normalizeLobstersScope(scope)
	return a.baseURL() + "/" + scope.View + ".json"
}

func (a LobstersAdapter) storyURL(id string) string {
	return a.baseURL() + "/s/" + url.PathEscape(strings.TrimSpace(id)) + ".json"
}

func (a LobstersAdapter) baseURL() string {
	if strings.TrimSpace(a.BaseURL) == "" {
		return lobstersBaseURL
	}
	return strings.TrimRight(a.BaseURL, "/")
}

func lobstersEntryID(entry domain.FeedEntry) string {
	if entry.Item.Refs.LobstersID != "" {
		return entry.Item.Refs.LobstersID
	}
	for _, source := range append(entry.Sources, entry.Item.Sources...) {
		if source.Source == domain.SourceLobsters && source.SourceIDRaw != "" {
			return source.SourceIDRaw
		}
	}
	return ""
}

func lobstersUserName(raw json.RawMessage) string {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var object struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal(raw, &object); err == nil {
		return object.Username
	}
	return ""
}

func lobstersTags(scope domain.FetchScope, values []json.RawMessage) []string {
	tags := []string{"lobsters", scope.View}
	for _, raw := range values {
		tag := lobstersTag(raw)
		if tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}

func lobstersTag(raw json.RawMessage) string {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.ToLower(strings.TrimSpace(text))
	}
	var object struct {
		Tag string `json:"tag"`
	}
	if err := json.Unmarshal(raw, &object); err == nil {
		return strings.ToLower(strings.TrimSpace(object.Tag))
	}
	return ""
}

func lobstersSubtitle(metrics domain.Metrics, tags []string) string {
	var parts []string
	if metrics.Score != nil {
		parts = append(parts, fmt.Sprintf("%g pts", *metrics.Score))
	}
	if metrics.Comments != nil {
		parts = append(parts, fmt.Sprintf("%d comments", *metrics.Comments))
	}
	if len(tags) > 2 {
		parts = append(parts, strings.Join(tags[2:], ", "))
	}
	return strings.Join(parts, " · ")
}

func float64PtrIfPositive(value float64) *float64 {
	if value <= 0 {
		return nil
	}
	return &value
}

func parseLobstersTime(value string) *time.Time {
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

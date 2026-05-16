package sources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/zhangxueai/tildewire/internal/domain"
	"github.com/zhangxueai/tildewire/internal/httpx"
	"github.com/zhangxueai/tildewire/internal/normalize"
)

const hnBaseURL = "https://hacker-news.firebaseio.com/v0"

const hnItemTTL = 10 * time.Minute

// HackerNewsAdapter fetches Hacker News story lists and item details.
type HackerNewsAdapter struct {
	BaseURL string
}

// NewHackerNewsAdapter creates the Hacker News source adapter.
func NewHackerNewsAdapter() HackerNewsAdapter {
	return HackerNewsAdapter{BaseURL: hnBaseURL}
}

// Source returns the adapter source id.
func (a HackerNewsAdapter) Source() domain.SourceID {
	return domain.SourceHackerNews
}

// DefaultScopes returns HN scopes available in the MVP.
func (a HackerNewsAdapter) DefaultScopes() []domain.FetchScope {
	return []domain.FetchScope{
		{Source: domain.SourceHackerNews, View: "top", Limit: 50},
		{Source: domain.SourceHackerNews, View: "best", Limit: 50},
		{Source: domain.SourceHackerNews, View: "new", Limit: 50},
		{Source: domain.SourceHackerNews, View: "show", Limit: 50},
	}
}

// Fetch downloads a story list and its item details.
func (a HackerNewsAdapter) Fetch(ctx context.Context, scope domain.FetchScope, client *httpx.Client) (*domain.FetchResult, error) {
	if scope.View == "" {
		scope.View = "top"
	}
	if scope.Limit <= 0 {
		scope.Limit = 50
	}
	endpoint, err := hnEndpoint(scope.View)
	if err != nil {
		return nil, err
	}
	listURL := a.BaseURL + "/" + endpoint + ".json"
	listResp, err := client.DoGET(ctx, httpx.GetOptions{
		Source:       string(domain.SourceHackerNews),
		URL:          listURL,
		TTL:          a.CachePolicy(scope).TTL,
		ForceRefresh: scope.ForceRefresh,
	})
	if err != nil {
		return nil, err
	}
	var ids []int
	if err := json.Unmarshal(listResp.Body, &ids); err != nil {
		return nil, fmt.Errorf("decode hn %s ids: %w", scope.View, err)
	}
	if len(ids) > scope.Limit {
		ids = ids[:scope.Limit]
	}
	items, itemStale, itemStaleReason, err := fetchHNItems(ctx, client, a.BaseURL, ids, scope.ForceRefresh)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(hnPayload{IDs: ids, Items: items})
	if err != nil {
		return nil, err
	}
	return &domain.FetchResult{
		Source:      domain.SourceHackerNews,
		Scope:       scope,
		StatusCode:  listResp.StatusCode,
		Body:        payload,
		FetchedAt:   listResp.FetchedAt,
		FromCache:   listResp.FromCache,
		Stale:       listResp.Stale || itemStale,
		StaleReason: firstNonEmpty(listResp.StaleReason, itemStaleReason),
	}, nil
}

// Normalize converts HN API JSON into FeedItems.
func (a HackerNewsAdapter) Normalize(_ context.Context, scope domain.FetchScope, raw *domain.FetchResult) ([]domain.FeedItem, error) {
	var payload hnPayload
	if err := json.Unmarshal(raw.Body, &payload); err != nil {
		return nil, fmt.Errorf("decode hn payload: %w", err)
	}
	ranks := make(map[int]int, len(payload.IDs))
	for idx, id := range payload.IDs {
		ranks[id] = idx + 1
	}
	now := raw.FetchedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	items := make([]domain.FeedItem, 0, len(payload.Items))
	for _, story := range payload.Items {
		if story.ID == 0 || story.Title == "" || story.Deleted || story.Dead {
			continue
		}
		seenAt := story.cacheFetchedAt()
		if seenAt.IsZero() {
			seenAt = now
		}
		item := hnStoryToFeedItem(story, scope, ranks[story.ID], seenAt)
		items = append(items, item)
	}
	return items, nil
}

// CachePolicy returns HN TTLs.
func (a HackerNewsAdapter) CachePolicy(scope domain.FetchScope) domain.CachePolicy {
	if scope.View == "" || scope.View == "top" || scope.View == "best" || scope.View == "new" || scope.View == "show" {
		return domain.CachePolicy{TTL: 5 * time.Minute}
	}
	return domain.CachePolicy{TTL: 10 * time.Minute}
}

type hnPayload struct {
	IDs   []int    `json:"ids"`
	Items []hnItem `json:"items"`
}

type hnItem struct {
	ID          int    `json:"id"`
	Deleted     bool   `json:"deleted"`
	Type        string `json:"type"`
	By          string `json:"by"`
	Time        int64  `json:"time"`
	Dead        bool   `json:"dead"`
	Kids        []int  `json:"kids"`
	URL         string `json:"url"`
	Score       int64  `json:"score"`
	Title       string `json:"title"`
	Text        string `json:"text"`
	Parent      int    `json:"parent"`
	Descendants int64  `json:"descendants"`
	FetchedAt   string `json:"_fetched_at,omitempty"`
	Stale       bool   `json:"_stale,omitempty"`
}

// Detail loads a Hacker News story and its top-level comments.
func (a HackerNewsAdapter) Detail(ctx context.Context, entry domain.FeedEntry, client *httpx.Client) (domain.ItemDetail, error) {
	id, ok := hnEntryID(entry)
	if !ok {
		return domain.ItemDetail{}, fmt.Errorf("hackernews id is missing")
	}
	baseURL := a.BaseURL
	if baseURL == "" {
		baseURL = hnBaseURL
	}
	resp, err := client.DoGET(ctx, httpx.GetOptions{
		Source: string(domain.SourceHackerNews),
		URL:    fmt.Sprintf("%s/item/%d.json", baseURL, id),
		TTL:    hnItemTTL,
	})
	if err != nil {
		return domain.ItemDetail{}, err
	}
	var story hnItem
	if err := json.Unmarshal(resp.Body, &story); err != nil {
		return domain.ItemDetail{}, fmt.Errorf("decode hn story detail: %w", err)
	}
	if len(story.Kids) > 8 {
		story.Kids = story.Kids[:8]
	}
	comments, _, _, err := fetchHNItems(ctx, client, baseURL, story.Kids, false)
	if err != nil {
		return domain.ItemDetail{}, err
	}
	detail := domain.ItemDetail{
		ItemID:   entry.Item.ID,
		Title:    firstNonEmpty(story.Title, entry.Item.Title),
		URL:      firstNonEmpty(entry.Item.CommentsURL, fmt.Sprintf("https://news.ycombinator.com/item?id=%d", id)),
		LoadedAt: resp.FetchedAt,
	}
	for _, comment := range comments {
		if comment.Deleted || comment.Dead || comment.Text == "" {
			continue
		}
		body := plainHTMLText(comment.Text)
		if body == "" {
			continue
		}
		published := time.Unix(comment.Time, 0).UTC()
		detail.Comments = append(detail.Comments, domain.DetailComment{
			Author:      comment.By,
			Body:        body,
			URL:         fmt.Sprintf("https://news.ycombinator.com/item?id=%d", comment.ID),
			PublishedAt: &published,
			Source:      domain.SourceHackerNews,
		})
	}
	return detail, nil
}

func hnEntryID(entry domain.FeedEntry) (int, bool) {
	if entry.Item.Refs.HNID != "" {
		id, err := strconv.Atoi(entry.Item.Refs.HNID)
		return id, err == nil
	}
	for _, source := range append(entry.Sources, entry.Item.Sources...) {
		if source.Source != domain.SourceHackerNews || source.SourceIDRaw == "" {
			continue
		}
		id, err := strconv.Atoi(source.SourceIDRaw)
		return id, err == nil
	}
	return 0, false
}

func fetchHNItems(ctx context.Context, client *httpx.Client, baseURL string, ids []int, forceRefresh bool) ([]hnItem, bool, string, error) {
	type result struct {
		idx         int
		item        hnItem
		staleReason string
		err         error
	}
	jobs := make(chan int)
	results := make(chan result, len(ids))
	var wg sync.WaitGroup
	workers := 4
	if len(ids) < workers {
		workers = len(ids)
	}
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				id := ids[idx]
				resp, err := client.DoGET(ctx, httpx.GetOptions{
					Source:       string(domain.SourceHackerNews),
					URL:          fmt.Sprintf("%s/item/%d.json", baseURL, id),
					TTL:          hnItemTTL,
					ForceRefresh: forceRefresh,
				})
				if err != nil {
					results <- result{idx: idx, err: fmt.Errorf("fetch hn item %d: %w", id, err)}
					continue
				}
				var item hnItem
				if err := json.Unmarshal(resp.Body, &item); err != nil {
					results <- result{idx: idx, err: fmt.Errorf("decode hn item %d: %w", id, err)}
					continue
				}
				item.FetchedAt = resp.FetchedAt.Format(time.RFC3339Nano)
				item.Stale = resp.Stale
				results <- result{idx: idx, item: item, staleReason: resp.StaleReason}
			}
		}()
	}
	for idx := range ids {
		jobs <- idx
	}
	close(jobs)
	wg.Wait()
	close(results)

	ordered := make([]hnItem, len(ids))
	anyStale := false
	staleReason := ""
	var failures []error
	for result := range results {
		if result.err != nil {
			failures = append(failures, result.err)
			continue
		}
		ordered[result.idx] = result.item
		if result.item.Stale {
			anyStale = true
			staleReason = firstNonEmpty(staleReason, result.staleReason)
		}
	}
	items := make([]hnItem, 0, len(ids))
	for _, item := range ordered {
		if item.ID != 0 {
			items = append(items, item)
		}
	}
	if len(failures) > 0 {
		if len(items) == 0 && len(ids) > 0 {
			return nil, false, "", fmt.Errorf("fetch hn item details: %w", errors.Join(failures...))
		}
		anyStale = true
		staleReason = firstNonEmpty(staleReason, httpx.StaleReasonNetworkError)
	}
	return items, anyStale, staleReason, nil
}

func hnStoryToFeedItem(story hnItem, scope domain.FetchScope, rank int, seenAt time.Time) domain.FeedItem {
	published := time.Unix(story.Time, 0).UTC()
	commentsURL := fmt.Sprintf("https://news.ycombinator.com/item?id=%d", story.ID)
	canonicalURL := normalize.CanonicalURL(story.URL)
	canonicalKey := ""
	if canonicalURL != "" {
		canonicalKey = "url:" + canonicalURL
	} else {
		canonicalURL = commentsURL
		canonicalKey = "hackernews:" + strconv.Itoa(story.ID)
	}
	itemID := normalize.StableID(canonicalKey)
	score := float64(story.Score)
	raw, _ := json.Marshal(story)
	metrics := domain.Metrics{
		Score:    &score,
		Comments: domain.Int64Ptr(story.Descendants),
		Upvotes:  domain.Int64Ptr(story.Score),
	}
	source := domain.ItemSource{
		ItemID:      itemID,
		Source:      domain.SourceHackerNews,
		SourceView:  scope.View,
		SourceIDRaw: strconv.Itoa(story.ID),
		SourceRank:  rank,
		SourceURL:   commentsURL,
		Metrics:     metrics,
		Raw:         raw,
		SeenAt:      seenAt,
	}
	return domain.FeedItem{
		ID:           itemID,
		CanonicalKey: canonicalKey,
		Title:        story.Title,
		Subtitle:     fmt.Sprintf("%d pts · %d comments", story.Score, story.Descendants),
		URL:          firstNonEmpty(story.URL, commentsURL),
		CanonicalURL: canonicalURL,
		CommentsURL:  commentsURL,
		ItemType:     "story",
		Author:       story.By,
		Tags:         []string{"hn", scope.View},
		PublishedAt:  &published,
		FirstSeenAt:  seenAt,
		LastSeenAt:   seenAt,
		Metrics:      metrics,
		Refs:         domain.Refs{HNID: strconv.Itoa(story.ID)},
		Sources:      []domain.ItemSource{source},
	}
}

func hnEndpoint(view string) (string, error) {
	switch view {
	case "top":
		return "topstories", nil
	case "best":
		return "beststories", nil
	case "new":
		return "newstories", nil
	case "show":
		return "showstories", nil
	default:
		return "", fmt.Errorf("unsupported hackernews view %q", view)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (item hnItem) cacheFetchedAt() time.Time {
	if item.FetchedAt == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, item.FetchedAt)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

package sources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	// Load IANA zone data so static builds can calculate Product Hunt launch days.
	_ "time/tzdata"

	"github.com/AIluffy/tildewire/internal/domain"
	"github.com/AIluffy/tildewire/internal/httpx"
	"github.com/AIluffy/tildewire/internal/normalize"
)

const (
	productHuntGraphQLURL  = "https://api.producthunt.com/v2/api/graphql"
	productHuntLocationKey = "America/Los_Angeles"
)

// ErrProductHuntAuthRequired is returned when the optional Product Hunt token is not configured.
var ErrProductHuntAuthRequired = errors.New("auth required: PRODUCT_HUNT_TOKEN is not set")

const productHuntAuthRequiredMessage = "PRODUCT_HUNT_TOKEN is not set"

var productHuntLocation = loadProductHuntLocation()

// ProductHuntAdapter fetches featured Product Hunt posts through the GraphQL API.
type ProductHuntAdapter struct {
	BaseURL string
	Token   string
	Now     func() time.Time
}

// NewProductHuntAdapter creates the Product Hunt source adapter.
func NewProductHuntAdapter(token string) *ProductHuntAdapter {
	return &ProductHuntAdapter{BaseURL: productHuntGraphQLURL, Token: strings.TrimSpace(token)}
}

// SetToken updates the token used for Product Hunt GraphQL requests.
func (a *ProductHuntAdapter) SetToken(token string) {
	a.Token = strings.TrimSpace(token)
}

// Source returns the adapter source id.
func (a ProductHuntAdapter) Source() domain.SourceID {
	return domain.SourceProductHunt
}

// AuthRequired reports whether Product Hunt should wait for an access token before refresh.
func (a ProductHuntAdapter) AuthRequired() (bool, string) {
	if strings.TrimSpace(a.Token) == "" {
		return true, productHuntAuthRequiredMessage
	}
	return false, ""
}

// DefaultScopes returns Product Hunt scopes for v0.2.
func (a ProductHuntAdapter) DefaultScopes() []domain.FetchScope {
	return []domain.FetchScope{
		{Source: domain.SourceProductHunt, View: "today", Limit: 25},
		{Source: domain.SourceProductHunt, View: "weekly", Limit: 25},
	}
}

// Fetch posts the Product Hunt GraphQL query for one source view.
func (a ProductHuntAdapter) Fetch(ctx context.Context, scope domain.FetchScope, client httpx.Requester) (*domain.FetchResult, error) {
	if strings.TrimSpace(a.Token) == "" {
		return nil, ErrProductHuntAuthRequired
	}
	scope = normalizeProductHuntScope(scope)
	body, err := json.Marshal(productHuntRequest(scope, a.now()))
	if err != nil {
		return nil, err
	}
	resp, err := client.DoPOSTJSON(ctx, httpx.PostOptions{
		Source:       string(domain.SourceProductHunt),
		URL:          a.graphQLURL(),
		Body:         body,
		BearerToken:  a.Token,
		TTL:          a.CachePolicy(scope).TTL,
		ForceRefresh: scope.ForceRefresh,
	})
	if err != nil {
		return nil, err
	}
	return &domain.FetchResult{
		Source:      domain.SourceProductHunt,
		Scope:       scope,
		StatusCode:  resp.StatusCode,
		Body:        resp.Body,
		FetchedAt:   resp.FetchedAt,
		FromCache:   resp.FromCache,
		Stale:       resp.Stale,
		StaleReason: resp.StaleReason,
	}, nil
}

// Normalize converts Product Hunt GraphQL JSON into FeedItems.
func (a ProductHuntAdapter) Normalize(_ context.Context, scope domain.FetchScope, raw *domain.FetchResult) ([]domain.FeedItem, error) {
	if raw == nil {
		return nil, fmt.Errorf("product hunt raw response is nil")
	}
	scope = normalizeProductHuntScope(scope)
	var payload productHuntResponse
	if err := json.Unmarshal(raw.Body, &payload); err != nil {
		return nil, fmt.Errorf("decode product hunt posts: %w", err)
	}
	if len(payload.Errors) > 0 {
		return nil, fmt.Errorf("product hunt graphql error: %s", payload.Errors[0].Message)
	}
	if len(payload.Data.Posts.Nodes) == 0 {
		return nil, fmt.Errorf("no product hunt posts found")
	}
	fetchedAt := raw.FetchedAt
	if fetchedAt.IsZero() {
		fetchedAt = time.Now().UTC()
	}
	items := make([]domain.FeedItem, 0, len(payload.Data.Posts.Nodes))
	for idx, post := range payload.Data.Posts.Nodes {
		if scope.Limit > 0 && len(items) >= scope.Limit {
			break
		}
		item, ok := productHuntPostToFeedItem(post, scope, idx+1, fetchedAt)
		if ok {
			items = append(items, item)
		}
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("no usable product hunt posts found")
	}
	return items, nil
}

// CachePolicy returns Product Hunt TTLs.
func (a ProductHuntAdapter) CachePolicy(scope domain.FetchScope) domain.CachePolicy {
	scope = normalizeProductHuntScope(scope)
	if scope.View == "weekly" {
		return domain.CachePolicy{TTL: 2 * time.Hour}
	}
	return domain.CachePolicy{TTL: 30 * time.Minute}
}

func (a ProductHuntAdapter) graphQLURL() string {
	if strings.TrimSpace(a.BaseURL) == "" {
		return productHuntGraphQLURL
	}
	return strings.TrimSpace(a.BaseURL)
}

func (a ProductHuntAdapter) now() time.Time {
	if a.Now != nil {
		return a.Now().UTC()
	}
	return time.Now().UTC()
}

type productHuntGraphQLRequest struct {
	Query     string                    `json:"query"`
	Variables productHuntQueryVariables `json:"variables"`
}

type productHuntQueryVariables struct {
	First        int    `json:"first"`
	PostedAfter  string `json:"postedAfter"`
	PostedBefore string `json:"postedBefore"`
}

func productHuntRequest(scope domain.FetchScope, now time.Time) productHuntGraphQLRequest {
	after, before := productHuntWindow(scope, now)
	return productHuntGraphQLRequest{
		Query: productHuntPostsQuery,
		Variables: productHuntQueryVariables{
			First:        scope.Limit,
			PostedAfter:  after.Format(time.RFC3339),
			PostedBefore: before.Format(time.RFC3339),
		},
	}
}

func productHuntWindow(scope domain.FetchScope, now time.Time) (time.Time, time.Time) {
	launchDay := now.In(productHuntLocation)
	day := time.Date(launchDay.Year(), launchDay.Month(), launchDay.Day(), 0, 0, 0, 0, productHuntLocation)
	before := day.AddDate(0, 0, 1)
	if scope.View == "weekly" {
		return day.AddDate(0, 0, -7).UTC(), before.UTC()
	}
	return day.UTC(), before.UTC()
}

func loadProductHuntLocation() *time.Location {
	loc, err := time.LoadLocation(productHuntLocationKey)
	if err != nil {
		return time.UTC
	}
	return loc
}

const productHuntPostsQuery = `
query ProductHuntPosts($first: Int!, $postedAfter: DateTime!, $postedBefore: DateTime!) {
  posts(first: $first, featured: true, order: RANKING, postedAfter: $postedAfter, postedBefore: $postedBefore) {
    nodes {
      id
      slug
      name
      tagline
      description
      url
      website
      votesCount
      commentsCount
      dailyRank
      weeklyRank
      createdAt
      featuredAt
      makers {
        name
        username
      }
      topics(first: 5) {
        nodes {
          name
          slug
        }
      }
      thumbnail {
        type
        url
      }
      productLinks {
        type
        url
      }
    }
  }
}`

type productHuntResponse struct {
	Data struct {
		Posts struct {
			Nodes []productHuntPost `json:"nodes"`
		} `json:"posts"`
	} `json:"data"`
	Errors []productHuntGraphQLError `json:"errors"`
}

type productHuntGraphQLError struct {
	Message string `json:"message"`
}

type productHuntPost struct {
	ID            string            `json:"id"`
	Slug          string            `json:"slug"`
	Name          string            `json:"name"`
	Tagline       string            `json:"tagline"`
	Description   string            `json:"description"`
	URL           string            `json:"url"`
	Website       string            `json:"website"`
	VotesCount    int64             `json:"votesCount"`
	CommentsCount int64             `json:"commentsCount"`
	DailyRank     *int              `json:"dailyRank"`
	WeeklyRank    *int              `json:"weeklyRank"`
	CreatedAt     string            `json:"createdAt"`
	FeaturedAt    string            `json:"featuredAt"`
	Makers        []productHuntUser `json:"makers"`
	Topics        productHuntTopics `json:"topics"`
	Thumbnail     *productHuntMedia `json:"thumbnail"`
	ProductLinks  []productHuntLink `json:"productLinks"`
}

type productHuntUser struct {
	Name     string `json:"name"`
	Username string `json:"username"`
}

type productHuntTopics struct {
	Nodes []productHuntTopic `json:"nodes"`
}

type productHuntTopic struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type productHuntMedia struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

type productHuntLink struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

func productHuntPostToFeedItem(post productHuntPost, scope domain.FetchScope, rank int, seenAt time.Time) (domain.FeedItem, bool) {
	id := strings.TrimSpace(post.ID)
	slug := strings.TrimSpace(post.Slug)
	title := cleanText(post.Name)
	if title == "" || (id == "" && slug == "") {
		return domain.FeedItem{}, false
	}
	postURL := strings.TrimSpace(post.URL)
	itemURL := firstNonEmpty(strings.TrimSpace(post.Website), productHuntPreferredLink(post.ProductLinks), postURL)
	canonicalURL := normalize.CanonicalURL(itemURL)
	canonicalKey := ""
	if canonicalURL != "" {
		canonicalKey = "url:" + canonicalURL
	} else {
		canonicalKey = "producthunt:" + strings.ToLower(firstNonEmpty(slug, id))
	}
	itemID := normalize.StableID(canonicalKey)
	published := parseProductHuntTime(firstNonEmpty(post.FeaturedAt, post.CreatedAt))
	raw, _ := json.Marshal(post)
	sourceRank := productHuntRank(post, scope, rank)
	score := float64(post.VotesCount)
	metrics := domain.Metrics{
		Score:    float64PtrIfPositive(score),
		Upvotes:  int64PtrIfPositive(post.VotesCount),
		Comments: int64PtrIfPositive(post.CommentsCount),
	}
	source := domain.ItemSource{
		ItemID:      itemID,
		Source:      domain.SourceProductHunt,
		SourceView:  scope.View,
		SourceIDRaw: firstNonEmpty(id, slug),
		SourceRank:  sourceRank,
		SourceURL:   postURL,
		Metrics:     metrics,
		Raw:         raw,
		SeenAt:      seenAt,
	}
	return domain.FeedItem{
		ID:           itemID,
		CanonicalKey: canonicalKey,
		Title:        title,
		Subtitle:     productHuntSubtitle(metrics, sourceRank, scope.View),
		Summary:      firstNonEmpty(cleanText(post.Description), cleanText(post.Tagline)),
		URL:          itemURL,
		CanonicalURL: canonicalURL,
		CommentsURL:  postURL,
		ItemType:     "product",
		Author:       productHuntMakers(post.Makers),
		Tags:         productHuntTags(scope, post.Topics.Nodes),
		PublishedAt:  published,
		FirstSeenAt:  seenAt,
		LastSeenAt:   seenAt,
		Metrics:      metrics,
		Refs: domain.Refs{
			ProductHuntID:   id,
			ProductHuntSlug: slug,
		},
		Metadata: raw,
		Sources:  []domain.ItemSource{source},
	}, true
}

func productHuntRank(post productHuntPost, scope domain.FetchScope, fallback int) int {
	var rank *int
	if scope.View == "weekly" {
		rank = post.WeeklyRank
	} else {
		rank = post.DailyRank
	}
	if rank != nil && *rank > 0 {
		return *rank
	}
	return fallback
}

func productHuntPreferredLink(links []productHuntLink) string {
	for _, link := range links {
		if strings.TrimSpace(link.URL) != "" {
			return strings.TrimSpace(link.URL)
		}
	}
	return ""
}

func productHuntMakers(makers []productHuntUser) string {
	names := make([]string, 0, len(makers))
	for _, maker := range makers {
		name := firstNonEmpty(cleanText(maker.Name), strings.TrimSpace(maker.Username))
		if name != "" {
			names = append(names, name)
		}
	}
	return strings.Join(names, ", ")
}

func productHuntTags(scope domain.FetchScope, topics []productHuntTopic) []string {
	tags := []string{"producthunt", scope.View}
	seen := map[string]bool{"producthunt": true, scope.View: true}
	for _, topic := range topics {
		tag := strings.ToLower(strings.TrimSpace(firstNonEmpty(topic.Slug, topic.Name)))
		tag = strings.Join(strings.Fields(tag), "-")
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		tags = append(tags, tag)
	}
	return tags
}

func productHuntSubtitle(metrics domain.Metrics, rank int, view string) string {
	var parts []string
	if metrics.Upvotes != nil {
		parts = append(parts, fmt.Sprintf("%d votes", *metrics.Upvotes))
	}
	if metrics.Comments != nil {
		parts = append(parts, fmt.Sprintf("%d comments", *metrics.Comments))
	}
	if rank > 0 {
		parts = append(parts, fmt.Sprintf("#%d %s", rank, view))
	}
	return strings.Join(parts, " · ")
}

func parseProductHuntTime(value string) *time.Time {
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

func normalizeProductHuntScope(scope domain.FetchScope) domain.FetchScope {
	scope.Source = domain.SourceProductHunt
	switch strings.ToLower(strings.TrimSpace(scope.View)) {
	case "weekly":
		scope.View = "weekly"
	default:
		scope.View = "today"
	}
	if scope.Limit <= 0 {
		scope.Limit = 25
	}
	return scope
}

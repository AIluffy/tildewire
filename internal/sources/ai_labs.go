package sources

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/AIluffy/tildewire/internal/domain"
	"github.com/AIluffy/tildewire/internal/httpx"
	"github.com/AIluffy/tildewire/internal/normalize"
)

const aiLabsDefaultLimit = 20

var aiLabDatePattern = regexp.MustCompile(`(?i)\b(?:\d{4}-\d{2}-\d{2}|(?:jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|jun(?:e)?|jul(?:y)?|aug(?:ust)?|sep(?:t(?:ember)?)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?)\s+\d{1,2},?\s+\d{4}|\d{1,2}\s+(?:jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|jun(?:e)?|jul(?:y)?|aug(?:ust)?|sep(?:t(?:ember)?)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?)\s+\d{4})\b`)

type aiLabFeedKind string

const (
	aiLabFeedRSS  aiLabFeedKind = "rss"
	aiLabFeedHTML aiLabFeedKind = "html"
)

// AILabsAdapter fetches official AI lab news and blog indexes.
type AILabsAdapter struct{}

// NewAILabsAdapter creates the AI Labs source adapter.
func NewAILabsAdapter() AILabsAdapter {
	return AILabsAdapter{}
}

// Source returns the adapter source id.
func (a AILabsAdapter) Source() domain.SourceID {
	return domain.SourceAILabs
}

// DefaultScopes returns every provider feed under AI Labs.
func (a AILabsAdapter) DefaultScopes() []domain.FetchScope {
	return []domain.FetchScope{
		{Source: domain.SourceAILabs, View: "openai", Limit: aiLabsDefaultLimit},
		{Source: domain.SourceAILabs, View: "anthropic", Limit: aiLabsDefaultLimit},
		{Source: domain.SourceAILabs, View: "deepmind", Limit: aiLabsDefaultLimit},
		{Source: domain.SourceAILabs, View: "meta", Limit: aiLabsDefaultLimit},
	}
}

// PrimaryScopes returns every provider because no single lab represents this aggregate source.
func (a AILabsAdapter) PrimaryScopes() []domain.FetchScope {
	return a.DefaultScopes()
}

// Fetch downloads one AI Labs provider page or feed.
func (a AILabsAdapter) Fetch(ctx context.Context, scope domain.FetchScope, client httpx.Requester) (*domain.FetchResult, error) {
	scope = normalizeAILabsScope(scope)
	spec, ok := aiLabSpec(scope.View)
	if !ok {
		return nil, fmt.Errorf("unsupported AI Labs view %q", scope.View)
	}
	resp, err := client.DoGET(ctx, httpx.GetOptions{
		Source:       string(domain.SourceAILabs),
		URL:          spec.URL,
		TTL:          a.CachePolicy(scope).TTL,
		ForceRefresh: scope.ForceRefresh,
	})
	if err != nil {
		return nil, err
	}
	return fetchResultFromResponse(domain.SourceAILabs, scope, resp), nil
}

// Normalize converts AI Labs provider responses into FeedItems.
func (a AILabsAdapter) Normalize(_ context.Context, scope domain.FetchScope, raw *domain.FetchResult) ([]domain.FeedItem, error) {
	if raw == nil {
		return nil, fmt.Errorf("AI Labs raw response is nil")
	}
	scope = normalizeAILabsScope(scope)
	spec, ok := aiLabSpec(scope.View)
	if !ok {
		return nil, fmt.Errorf("unsupported AI Labs view %q", scope.View)
	}
	fetchedAt := raw.FetchedAt
	if fetchedAt.IsZero() {
		fetchedAt = time.Now().UTC()
	}
	entries, err := parseAILabEntries(raw.Body, spec)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no AI Labs %s items found", spec.Provider)
	}
	sortAILabEntriesByDate(entries)
	items := make([]domain.FeedItem, 0, len(entries))
	for idx, entry := range entries {
		if scope.Limit > 0 && len(items) >= scope.Limit {
			break
		}
		item, ok := aiLabEntryToFeedItem(entry, spec, scope, idx+1, fetchedAt)
		if ok {
			items = append(items, item)
		}
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("no usable AI Labs %s items found", spec.Provider)
	}
	return items, nil
}

func sortAILabEntriesByDate(entries []aiLabEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		left := entries[i].PublishedAt
		right := entries[j].PublishedAt
		switch {
		case left != nil && right != nil:
			return left.After(*right)
		case left != nil:
			return true
		case right != nil:
			return false
		default:
			return false
		}
	})
}

// CachePolicy returns AI Labs feed TTLs.
func (a AILabsAdapter) CachePolicy(domain.FetchScope) domain.CachePolicy {
	return domain.CachePolicy{TTL: time.Hour}
}

type aiLabFeedSpec struct {
	Provider string
	Label    string
	URL      string
	Kind     aiLabFeedKind
}

type aiLabEntry struct {
	Provider    string     `json:"provider"`
	Title       string     `json:"title"`
	Summary     string     `json:"summary,omitempty"`
	URL         string     `json:"url"`
	Category    string     `json:"category,omitempty"`
	ID          string     `json:"id,omitempty"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
}

type aiLabRSS struct {
	Channel struct {
		Items []aiLabRSSItem `xml:"item"`
	} `xml:"channel"`
}

type aiLabRSSItem struct {
	Title       string   `xml:"title"`
	Link        string   `xml:"link"`
	Description string   `xml:"description"`
	PubDate     string   `xml:"pubDate"`
	GUID        string   `xml:"guid"`
	Categories  []string `xml:"category"`
}

type aiLabAtom struct {
	Entries []aiLabAtomEntry `xml:"entry"`
}

type aiLabAtomEntry struct {
	Title     string `xml:"title"`
	ID        string `xml:"id"`
	Summary   string `xml:"summary"`
	Updated   string `xml:"updated"`
	Published string `xml:"published"`
	Links     []struct {
		Href string `xml:"href,attr"`
		Rel  string `xml:"rel,attr"`
	} `xml:"link"`
	Categories []struct {
		Term string `xml:"term,attr"`
	} `xml:"category"`
}

func aiLabSpec(view string) (aiLabFeedSpec, bool) {
	switch strings.ToLower(strings.TrimSpace(view)) {
	case "openai":
		return aiLabFeedSpec{Provider: "openai", Label: "OpenAI", URL: "https://openai.com/news/rss.xml", Kind: aiLabFeedRSS}, true
	case "anthropic":
		return aiLabFeedSpec{Provider: "anthropic", Label: "Anthropic", URL: "https://www.anthropic.com/news", Kind: aiLabFeedHTML}, true
	case "deepmind":
		return aiLabFeedSpec{Provider: "deepmind", Label: "Google DeepMind", URL: "https://deepmind.google/blog/", Kind: aiLabFeedHTML}, true
	case "meta":
		return aiLabFeedSpec{Provider: "meta", Label: "Meta AI", URL: "https://ai.meta.com/blog/", Kind: aiLabFeedHTML}, true
	default:
		return aiLabFeedSpec{}, false
	}
}

func normalizeAILabsScope(scope domain.FetchScope) domain.FetchScope {
	scope.Source = domain.SourceAILabs
	scope.View = strings.ToLower(strings.TrimSpace(scope.View))
	if scope.View == "" {
		scope.View = "openai"
	}
	if scope.Limit <= 0 {
		scope.Limit = aiLabsDefaultLimit
	}
	return scope
}

func parseAILabEntries(body []byte, spec aiLabFeedSpec) ([]aiLabEntry, error) {
	switch spec.Kind {
	case aiLabFeedRSS:
		return parseAILabRSS(body, spec)
	case aiLabFeedHTML:
		return parseAILabHTML(body, spec)
	default:
		return nil, fmt.Errorf("unsupported AI Labs feed kind %q", spec.Kind)
	}
}

func parseAILabRSS(body []byte, spec aiLabFeedSpec) ([]aiLabEntry, error) {
	var rss aiLabRSS
	if err := xml.Unmarshal(body, &rss); err != nil {
		return nil, fmt.Errorf("decode AI Labs RSS: %w", err)
	}
	if len(rss.Channel.Items) > 0 {
		entries := make([]aiLabEntry, 0, len(rss.Channel.Items))
		for _, item := range rss.Channel.Items {
			entries = append(entries, aiLabEntry{
				Provider:    spec.Provider,
				Title:       cleanText(item.Title),
				Summary:     plainHTMLText(item.Description),
				URL:         cleanText(item.Link),
				Category:    firstNonEmptyCategory(item.Categories),
				ID:          cleanText(firstNonEmpty(item.GUID, item.Link)),
				PublishedAt: parseAILabDate(item.PubDate),
			})
		}
		return entries, nil
	}
	var atom aiLabAtom
	if err := xml.Unmarshal(body, &atom); err != nil {
		return nil, fmt.Errorf("decode AI Labs Atom: %w", err)
	}
	entries := make([]aiLabEntry, 0, len(atom.Entries))
	for _, item := range atom.Entries {
		entries = append(entries, aiLabEntry{
			Provider:    spec.Provider,
			Title:       cleanText(item.Title),
			Summary:     plainHTMLText(item.Summary),
			URL:         atomEntryLink(item),
			Category:    atomEntryCategory(item),
			ID:          cleanText(item.ID),
			PublishedAt: parseAILabDate(firstNonEmpty(item.Published, item.Updated)),
		})
	}
	return entries, nil
}

func parseAILabHTML(body []byte, spec aiLabFeedSpec) ([]aiLabEntry, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse AI Labs %s html: %w", spec.Provider, err)
	}
	entries := make([]aiLabEntry, 0)
	seen := make(map[string]bool)
	doc.Find("article").Each(func(_ int, article *goquery.Selection) {
		if entry, ok := aiLabEntryFromArticle(article, spec); ok {
			appendUniqueAILabEntry(&entries, seen, entry)
		}
	})
	if len(entries) > 0 {
		return entries, nil
	}
	doc.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		if entry, ok := aiLabEntryFromLink(link, spec); ok {
			appendUniqueAILabEntry(&entries, seen, entry)
		}
	})
	return entries, nil
}

func aiLabEntryFromArticle(article *goquery.Selection, spec aiLabFeedSpec) (aiLabEntry, bool) {
	link := firstAILabArticleLink(article, spec)
	if link == nil {
		return aiLabEntry{}, false
	}
	href, _ := link.Attr("href")
	itemURL := absoluteAILabURL(spec.URL, href)
	if !validAILabURL(spec, itemURL) {
		return aiLabEntry{}, false
	}
	title := cleanText(article.Find("h1,h2,h3").First().Text())
	if title == "" || lowSignalAILabTitle(title) {
		title = cleanText(link.Text())
	}
	if title == "" || lowSignalAILabTitle(title) {
		return aiLabEntry{}, false
	}
	summary := cleanText(article.Find("p").First().Text())
	published := aiLabDateFromSelection(article)
	return aiLabEntry{
		Provider:    spec.Provider,
		Title:       title,
		Summary:     summary,
		URL:         itemURL,
		ID:          itemURL,
		PublishedAt: published,
	}, true
}

func aiLabEntryFromLink(link *goquery.Selection, spec aiLabFeedSpec) (aiLabEntry, bool) {
	href, _ := link.Attr("href")
	itemURL := absoluteAILabURL(spec.URL, href)
	if !validAILabURL(spec, itemURL) {
		return aiLabEntry{}, false
	}
	title := cleanText(link.Text())
	if title == "" || lowSignalAILabTitle(title) {
		return aiLabEntry{}, false
	}
	card := closestAILabCard(link)
	published := aiLabDateFromSelection(card)
	if published == nil {
		return aiLabEntry{}, false
	}
	return aiLabEntry{
		Provider:    spec.Provider,
		Title:       title,
		Summary:     cleanText(card.Find("p").First().Text()),
		URL:         itemURL,
		ID:          itemURL,
		PublishedAt: published,
	}, true
}

func firstAILabArticleLink(article *goquery.Selection, spec aiLabFeedSpec) *goquery.Selection {
	var found *goquery.Selection
	article.Find("a[href]").EachWithBreak(func(_ int, link *goquery.Selection) bool {
		href, _ := link.Attr("href")
		if validAILabURL(spec, absoluteAILabURL(spec.URL, href)) {
			found = link
			return false
		}
		return true
	})
	return found
}

func appendUniqueAILabEntry(entries *[]aiLabEntry, seen map[string]bool, entry aiLabEntry) {
	key := normalize.CanonicalURL(entry.URL)
	if key == "" {
		key = entry.URL
	}
	if key == "" || seen[key] {
		return
	}
	seen[key] = true
	*entries = append(*entries, entry)
}

func closestAILabCard(link *goquery.Selection) *goquery.Selection {
	for _, selector := range []string{"article", "li", "div"} {
		if card := link.Closest(selector); card.Length() > 0 {
			return card
		}
	}
	return link.Parent()
}

func absoluteAILabURL(baseURL, href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return href
	}
	ref, err := url.Parse(href)
	if err != nil {
		return ""
	}
	return base.ResolveReference(ref).String()
}

func validAILabURL(spec aiLabFeedSpec, raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return false
	}
	base, err := url.Parse(spec.URL)
	if err != nil || !strings.EqualFold(parsed.Host, base.Host) {
		return false
	}
	path := strings.ToLower(parsed.Path)
	switch spec.Provider {
	case "openai", "anthropic":
		return strings.HasPrefix(path, "/news/")
	case "deepmind", "meta":
		return strings.HasPrefix(path, "/blog/")
	default:
		return true
	}
}

func aiLabDateFromSelection(selection *goquery.Selection) *time.Time {
	if selection == nil || selection.Length() == 0 {
		return nil
	}
	var raw string
	selection.Find("time").EachWithBreak(func(_ int, node *goquery.Selection) bool {
		if value, ok := node.Attr("datetime"); ok && strings.TrimSpace(value) != "" {
			raw = value
			return false
		}
		raw = cleanText(node.Text())
		return raw == ""
	})
	if raw == "" {
		raw = aiLabDatePattern.FindString(cleanText(selection.Text()))
	}
	return parseAILabDate(raw)
}

func parseAILabDate(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	value = strings.TrimSuffix(value, ".")
	layouts := []string{
		time.RFC1123Z,
		time.RFC1123,
		time.RFC3339,
		"2006-01-02",
		"January 2, 2006",
		"Jan 2, 2006",
		"January 2 2006",
		"Jan 2 2006",
		"2 January 2006",
		"2 Jan 2006",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			utc := parsed.UTC()
			return &utc
		}
	}
	if match := aiLabDatePattern.FindString(value); match != "" && match != value {
		return parseAILabDate(match)
	}
	return nil
}

func aiLabEntryToFeedItem(entry aiLabEntry, spec aiLabFeedSpec, scope domain.FetchScope, rank int, seenAt time.Time) (domain.FeedItem, bool) {
	title := cleanText(entry.Title)
	itemURL := strings.TrimSpace(entry.URL)
	if title == "" || itemURL == "" {
		return domain.FeedItem{}, false
	}
	canonicalURL := normalize.CanonicalURL(itemURL)
	if canonicalURL == "" {
		canonicalURL = itemURL
	}
	canonicalKey := "url:" + canonicalURL
	itemID := normalize.StableID(canonicalKey)
	raw, _ := json.Marshal(entry)
	source := domain.ItemSource{
		ItemID:      itemID,
		Source:      domain.SourceAILabs,
		SourceView:  scope.View,
		SourceIDRaw: firstNonEmpty(entry.ID, canonicalURL),
		SourceRank:  rank,
		SourceURL:   itemURL,
		Raw:         raw,
		SeenAt:      seenAt,
	}
	return domain.FeedItem{
		ID:           itemID,
		CanonicalKey: canonicalKey,
		Title:        title,
		Subtitle:     aiLabSubtitle(spec.Label, entry.Category, entry.PublishedAt),
		Summary:      cleanText(entry.Summary),
		URL:          itemURL,
		CanonicalURL: canonicalURL,
		ItemType:     "news",
		Organization: spec.Label,
		Tags:         aiLabTags(spec, entry.Category),
		PublishedAt:  entry.PublishedAt,
		FirstSeenAt:  seenAt,
		LastSeenAt:   seenAt,
		Metadata:     raw,
		Sources:      []domain.ItemSource{source},
	}, true
}

func aiLabSubtitle(label, category string, published *time.Time) string {
	parts := []string{label}
	if category = cleanText(category); category != "" {
		parts = append(parts, category)
	}
	if published != nil {
		parts = append(parts, published.Format("Jan 2, 2006"))
	}
	return strings.Join(parts, " · ")
}

func aiLabTags(spec aiLabFeedSpec, category string) []string {
	tags := []string{"ai", "ai-labs", spec.Provider}
	if category = strings.ToLower(cleanText(category)); category != "" {
		tags = append(tags, strings.ReplaceAll(category, " ", "-"))
	}
	return tags
}

func atomEntryLink(item aiLabAtomEntry) string {
	for _, link := range item.Links {
		if strings.TrimSpace(link.Href) == "" {
			continue
		}
		if link.Rel == "" || strings.EqualFold(link.Rel, "alternate") {
			return cleanText(link.Href)
		}
	}
	if len(item.Links) == 0 {
		return ""
	}
	return cleanText(item.Links[0].Href)
}

func atomEntryCategory(item aiLabAtomEntry) string {
	for _, category := range item.Categories {
		if value := cleanText(category.Term); value != "" {
			return value
		}
	}
	return ""
}

func firstNonEmptyCategory(values []string) string {
	for _, value := range values {
		if value = cleanText(value); value != "" {
			return value
		}
	}
	return ""
}

func lowSignalAILabTitle(value string) bool {
	value = strings.ToLower(cleanText(value))
	switch value {
	case "", "read more", "learn more", "more", "blog", "news", "openai", "anthropic", "google deepmind", "meta ai":
		return true
	default:
		return len(value) < 4
	}
}

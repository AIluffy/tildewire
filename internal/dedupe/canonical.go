package dedupe

import (
	"net/url"
	"strings"

	"github.com/zhangxueai/tildewire/internal/domain"
	"github.com/zhangxueai/tildewire/internal/normalize"
)

// CanonicalizeItems applies the v0.1 strong-key strategy to a batch of items.
func CanonicalizeItems(items []domain.FeedItem) []domain.FeedItem {
	canonical := make([]domain.FeedItem, len(items))
	for i, item := range items {
		canonical[i] = CanonicalizeItem(item)
	}
	return canonical
}

// CanonicalizeItem applies the documented strong-key priority to one item.
func CanonicalizeItem(item domain.FeedItem) domain.FeedItem {
	canonicalURL := itemCanonicalURL(item)
	if canonicalURL != "" {
		item.CanonicalURL = canonicalURL
	}

	key := ""
	if repo := itemRepo(item); repo != "" {
		item.Refs.Repo = repo
		key = normalize.RepoKey(repo)
	} else if arxivID := itemArxivID(item); arxivID != "" {
		item.Refs.ArxivID = arxivID
		key = "arxiv:" + arxivID
	} else if paperID := normalizePaperID(item.Refs.PaperID); paperID != "" {
		item.Refs.PaperID = paperID
		key = "paper:" + paperID
	} else if canonicalURL != "" {
		key = "url:" + canonicalURL
	} else if sourceKey := itemSourceKey(item); sourceKey != "" {
		key = sourceKey
	} else {
		key = strings.TrimSpace(item.CanonicalKey)
	}

	if key != "" {
		item.CanonicalKey = key
		item.ID = normalize.StableID(key)
	}
	for i := range item.Sources {
		if item.ID != "" {
			item.Sources[i].ItemID = item.ID
		}
	}
	return item
}

func itemCanonicalURL(item domain.FeedItem) string {
	for _, raw := range []string{item.URL, item.CanonicalURL} {
		if canonical := normalize.CanonicalURL(raw); canonical != "" {
			return canonical
		}
	}
	return ""
}

func itemRepo(item domain.FeedItem) string {
	if repo := normalize.NormalizeRepo(item.Refs.Repo); repo != "" {
		return repo
	}
	for _, raw := range itemURLs(item) {
		if repo := githubRepoFromURL(raw); repo != "" {
			return repo
		}
	}
	return ""
}

func itemArxivID(item domain.FeedItem) string {
	if arxivID := normalize.NormalizeArxivID(item.Refs.ArxivID); arxivID != "" {
		return arxivID
	}
	for _, raw := range itemURLs(item) {
		if arxivID, ok := normalize.ArxivIDFromURL(raw); ok {
			return arxivID
		}
	}
	return ""
}

func itemURLs(item domain.FeedItem) []string {
	urls := []string{item.URL, item.CanonicalURL}
	for _, source := range item.Sources {
		urls = append(urls, source.SourceURL)
	}
	return urls
}

func githubRepoFromURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
	if host != "github.com" {
		return ""
	}
	return normalize.NormalizeRepo(parsed.Path)
}

func normalizePaperID(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func itemSourceKey(item domain.FeedItem) string {
	for _, source := range item.Sources {
		sourceID := strings.ToLower(strings.TrimSpace(source.SourceIDRaw))
		if source.Source != "" && sourceID != "" {
			return string(source.Source) + ":" + sourceID
		}
	}
	return ""
}

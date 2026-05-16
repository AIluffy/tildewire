package dedupe

import (
	"testing"
	"time"

	"github.com/zhangxueai/tildewire/internal/domain"
	"github.com/zhangxueai/tildewire/internal/normalize"
)

func TestCanonicalizeItemPromotesGitHubURLToRepoKey(t *testing.T) {
	item := testDedupeItem("hn-42", "hackernews:42", "https://github.com/CharmBracelet/BubbleTea?utm_source=hn")
	item.Refs.HNID = "42"

	got := CanonicalizeItem(item)
	wantKey := "repo:charmbracelet/bubbletea"
	if got.CanonicalKey != wantKey {
		t.Fatalf("canonical key = %q, want %q", got.CanonicalKey, wantKey)
	}
	if got.ID != normalize.StableID(wantKey) {
		t.Fatalf("id = %q, want stable repo id", got.ID)
	}
	if got.Refs.Repo != "charmbracelet/bubbletea" {
		t.Fatalf("repo ref = %q", got.Refs.Repo)
	}
	if got.Sources[0].ItemID != got.ID {
		t.Fatalf("source item id = %q, want %q", got.Sources[0].ItemID, got.ID)
	}
}

func TestCanonicalizeItemPromotesArxivURLToArxivKey(t *testing.T) {
	item := testDedupeItem("hn-43", "hackernews:43", "https://arxiv.org/pdf/2605.12345v2.pdf?utm_source=hn")
	item.Refs.HNID = "43"

	got := CanonicalizeItem(item)
	wantKey := "arxiv:2605.12345v2"
	if got.CanonicalKey != wantKey {
		t.Fatalf("canonical key = %q, want %q", got.CanonicalKey, wantKey)
	}
	if got.ID != normalize.StableID(wantKey) {
		t.Fatalf("id = %q, want stable arxiv id", got.ID)
	}
	if got.Refs.ArxivID != "2605.12345v2" {
		t.Fatalf("arxiv ref = %q", got.Refs.ArxivID)
	}
}

func TestCanonicalizeItemUsesDocumentedPriority(t *testing.T) {
	item := testDedupeItem("paper-1", "url:https://example.com/paper", "https://example.com/paper")
	item.Refs.PaperID = "HF-Paper-Only"

	got := CanonicalizeItem(item)
	wantKey := "paper:hf-paper-only"
	if got.CanonicalKey != wantKey {
		t.Fatalf("canonical key = %q, want %q", got.CanonicalKey, wantKey)
	}
}

func testDedupeItem(id, key, rawURL string) domain.FeedItem {
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	return domain.FeedItem{
		ID:           id,
		CanonicalKey: key,
		Title:        "Test item",
		URL:          rawURL,
		CanonicalURL: normalize.CanonicalURL(rawURL),
		ItemType:     "story",
		FirstSeenAt:  now,
		LastSeenAt:   now,
		Sources: []domain.ItemSource{{
			ItemID:      id,
			Source:      domain.SourceHackerNews,
			SourceView:  "top",
			SourceIDRaw: id,
			SourceRank:  1,
			SourceURL:   "https://news.ycombinator.com/item?id=" + id,
			SeenAt:      now,
		}},
	}
}

package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AIluffy/tildewire/internal/domain"
	"github.com/AIluffy/tildewire/internal/httpx"
)

func TestLoadFeedIncludesGitHubAndSourceCounts(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	githubWeekly := appTestSourceItem("gh-weekly", "repo:owner/weekly", "owner/weekly", domain.SourceGitHub, 1, now.Add(3*time.Minute))
	githubWeekly.Sources[0].SourceView = "trending:weekly"
	hnBest := appTestSourceItem("hn-best", "hackernews:best", "Best HN", domain.SourceHackerNews, 1, now.Add(4*time.Minute))
	hnBest.Sources[0].SourceView = "best"
	items := []domain.FeedItem{
		appTestSourceItem("gh-2", "repo:owner/two", "owner/two", domain.SourceGitHub, 2, now.Add(2*time.Minute)),
		appTestSourceItem("hn-1", "hackernews:1", "Cached HN", domain.SourceHackerNews, 1, now.Add(time.Minute)),
		appTestSourceItem("gh-1", "repo:owner/one", "owner/one", domain.SourceGitHub, 1, now),
		githubWeekly,
		hnBest,
	}
	if err := db.UpsertFeedItems(ctx, items); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, httpx.New(time.Second), nil)
	all, err := service.LoadFeed(ctx, domain.SourceAll, FeedFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Entries) != 5 || !snapshotHasSource(all, domain.SourceGitHub) || !snapshotHasSource(all, domain.SourceHackerNews) {
		t.Fatalf("all feed did not merge sources: %+v", all.Entries)
	}
	if all.Counts[domain.SourceAll] != 3 || all.Counts[domain.SourceGitHub] != 2 || all.Counts[domain.SourceHackerNews] != 1 {
		t.Fatalf("counts mismatch: %+v", all.Counts)
	}
	if all.Counts[domain.SourceAll] != all.Counts[domain.SourceGitHub]+all.Counts[domain.SourceHackerNews]+all.Counts[domain.SourceHuggingFace] {
		t.Fatalf("all count should equal source tab counts: %+v", all.Counts)
	}

	github, err := service.LoadFeed(ctx, domain.SourceGitHub, FeedFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(github.Entries) != 2 || github.Entries[0].Item.Title != "owner/one" || github.Entries[1].Item.Title != "owner/two" {
		t.Fatalf("github view not source-rank sorted: %+v", github.Entries)
	}

	githubWeeklySnapshot, err := service.LoadFeed(ctx, domain.SourceGitHub, FeedFilter{SourceView: "trending:weekly"})
	if err != nil {
		t.Fatal(err)
	}
	if githubWeeklySnapshot.Counts[domain.SourceGitHub] != 1 {
		t.Fatalf("weekly GitHub count = %d, want 1", githubWeeklySnapshot.Counts[domain.SourceGitHub])
	}
	if githubWeeklySnapshot.Counts[domain.SourceAll] != all.Counts[domain.SourceAll] {
		t.Fatalf("all count should stay on All tab aggregate count when active source scope changes: all=%+v weekly=%+v", all.Counts, githubWeeklySnapshot.Counts)
	}
}

func TestLoadFeedSingleSourceCountIsNotCappedByPageLimit(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	items := make([]domain.FeedItem, 0, 306)
	for idx := 1; idx <= 306; idx++ {
		id := fmt.Sprintf("hn-%03d", idx)
		items = append(items, appTestSourceItem(id, "hackernews:"+id, "HN story "+id, domain.SourceHackerNews, idx, now.Add(time.Duration(idx)*time.Second)))
	}
	if err := db.UpsertFeedItems(ctx, items); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, httpx.New(time.Second), nil)

	all, err := service.LoadFeed(ctx, domain.SourceAll, FeedFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if all.Counts[domain.SourceHackerNews] != 306 {
		t.Fatalf("all view Hacker News count = %d, want 306", all.Counts[domain.SourceHackerNews])
	}

	hackerNews, err := service.LoadFeed(ctx, domain.SourceHackerNews, FeedFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hackerNews.Entries) != 250 {
		t.Fatalf("loaded Hacker News entries = %d, want page limit 250", len(hackerNews.Entries))
	}
	if hackerNews.Counts[domain.SourceHackerNews] != 306 {
		t.Fatalf("selected Hacker News count = %d, want 306", hackerNews.Counts[domain.SourceHackerNews])
	}
}

func TestLoadFeedCountsMultiPrimarySourceViews(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	openAI := appTestSourceItem("ai-openai", "url:https://openai.com/news/one", "OpenAI one", domain.SourceAILabs, 1, now)
	openAI.Sources[0].SourceView = "openai"
	meta := appTestSourceItem("ai-meta", "url:https://ai.meta.com/blog/one", "Meta one", domain.SourceAILabs, 1, now.Add(time.Minute))
	meta.Sources[0].SourceView = "meta"
	if err := db.UpsertFeedItems(ctx, []domain.FeedItem{openAI, meta}); err != nil {
		t.Fatal(err)
	}
	aiLabs := &fakeAdapter{
		source: domain.SourceAILabs,
		primaryScopes: []domain.FetchScope{
			{Source: domain.SourceAILabs, View: "openai"},
			{Source: domain.SourceAILabs, View: "anthropic"},
			{Source: domain.SourceAILabs, View: "deepmind"},
			{Source: domain.SourceAILabs, View: "meta"},
		},
	}
	service := NewService(db, httpx.New(time.Second), []SourceAdapter{aiLabs})

	snapshot, err := service.LoadFeed(ctx, domain.SourceAll, FeedFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Counts[domain.SourceAILabs] != 2 || snapshot.Counts[domain.SourceAll] != 2 {
		t.Fatalf("counts = %+v, want AI Labs and All to count both provider items", snapshot.Counts)
	}
}

func TestLoadFeedMultiPrimarySourceDefaultsToAllViews(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	openAI := appTestSourceItem("ai-openai", "url:https://openai.com/news/one", "OpenAI one", domain.SourceAILabs, 1, now)
	openAI.Sources[0].SourceView = "openai"
	meta := appTestSourceItem("ai-meta", "url:https://ai.meta.com/blog/one", "Meta one", domain.SourceAILabs, 1, now.Add(time.Minute))
	meta.Sources[0].SourceView = "meta"
	if err := db.UpsertFeedItems(ctx, []domain.FeedItem{openAI, meta}); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, httpx.New(time.Second), []SourceAdapter{&fakeAdapter{source: domain.SourceAILabs}})

	snapshot, err := service.LoadFeed(ctx, domain.SourceAILabs, FeedFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Entries) != 2 || snapshot.Filter.SourceView != "" {
		t.Fatalf("AI Labs default feed = entries:%d filter:%+v, want all provider entries with empty source view", len(snapshot.Entries), snapshot.Filter)
	}
}

func TestLoadFeedAppliesSearchAndStateFilters(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	items := []domain.FeedItem{
		appTestSourceItem("gh-1", "repo:owner/agent", "owner/agent", domain.SourceGitHub, 1, now),
		appTestSourceItem("gh-2", "repo:owner/cli", "owner/cli", domain.SourceGitHub, 2, now),
		appTestSourceItem("hn-1", "hackernews:1", "SQLite discussion", domain.SourceHackerNews, 1, now),
	}
	items[0].Summary = "Terminal agent workflow"
	items[0].Language = "Go"
	items[0].Tags = []string{"agents", "terminal"}
	items[1].Summary = "Command-line utilities"
	items[1].Language = "Rust"
	items[1].Tags = []string{"cli"}
	if err := db.UpsertFeedItems(ctx, items); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSaved(ctx, items[0].ID, true); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSaved(ctx, items[1].ID, true); err != nil {
		t.Fatal(err)
	}
	if err := db.SetRead(ctx, items[1].ID, true); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, httpx.New(time.Second), nil)

	savedAgent, err := service.LoadFeed(ctx, domain.SourceAll, FeedFilter{Search: "agent", SavedOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(savedAgent.Entries) != 1 || savedAgent.Entries[0].Item.Title != "owner/agent" {
		t.Fatalf("saved text filter mismatch: %+v", savedAgent.Entries)
	}

	unreadGoTag, err := service.LoadFeed(ctx, domain.SourceAll, FeedFilter{UnreadOnly: true, Language: "go", Tag: "terminal"})
	if err != nil {
		t.Fatal(err)
	}
	if len(unreadGoTag.Entries) != 1 || unreadGoTag.Entries[0].Item.Title != "owner/agent" {
		t.Fatalf("unread language tag filter mismatch: %+v", unreadGoTag.Entries)
	}

	savedUnread, err := service.LoadFeed(ctx, domain.SourceAll, FeedFilter{SavedOnly: true, UnreadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(savedUnread.Entries) != 1 || savedUnread.Entries[0].Item.Title != "owner/agent" {
		t.Fatalf("saved unread filter mismatch: %+v", savedUnread.Entries)
	}
}

func TestLoadFeedFiltersSingleSourceBySourceView(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	top := appTestSourceItem("hn-top", "hackernews:top", "Top story", domain.SourceHackerNews, 1, now)
	top.Sources[0].SourceView = "top"
	best := appTestSourceItem("hn-best", "hackernews:best", "Best story", domain.SourceHackerNews, 1, now)
	best.Sources[0].SourceView = "best"
	if err := db.UpsertFeedItems(ctx, []domain.FeedItem{top, best}); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, httpx.New(time.Second), nil)

	snapshot, err := service.LoadFeed(ctx, domain.SourceHackerNews, FeedFilter{SourceView: "best"})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Entries) != 1 || snapshot.Entries[0].Item.Title != "Best story" {
		t.Fatalf("source view filter mismatch: %+v", snapshot.Entries)
	}
	if snapshot.Filter.SourceView != "best" {
		t.Fatalf("snapshot source view = %q, want best", snapshot.Filter.SourceView)
	}
}

func TestLoadFeedSingleSourceKeepsSourceNativeContext(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		source     domain.SourceID
		sourceView string
		sourceID   string
		sourceURL  string
	}{
		{
			name:       "hackernews",
			source:     domain.SourceHackerNews,
			sourceView: "top",
			sourceID:   "42",
			sourceURL:  "https://news.ycombinator.com/item?id=42",
		},
		{
			name:       "lobsters",
			source:     domain.SourceLobsters,
			sourceView: "hottest",
			sourceID:   "abc123",
			sourceURL:  "https://lobste.rs/s/abc123/story",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openAppTestStore(t)
			defer db.Close()

			item := appTestSourceItem("repo-1", "repo:owner/repo", "owner/repo", domain.SourceGitHub, 1, now)
			item.Sources = append(item.Sources, domain.ItemSource{
				ItemID:      item.ID,
				Source:      tt.source,
				SourceView:  tt.sourceView,
				SourceIDRaw: tt.sourceID,
				SourceRank:  2,
				SourceURL:   tt.sourceURL,
				SeenAt:      now,
			})
			if err := db.UpsertFeedItems(ctx, []domain.FeedItem{item}); err != nil {
				t.Fatal(err)
			}
			service := NewService(db, httpx.New(time.Second), nil)

			snapshot, err := service.LoadFeed(ctx, tt.source, FeedFilter{SourceView: tt.sourceView})
			if err != nil {
				t.Fatal(err)
			}
			if len(snapshot.Entries) != 1 {
				t.Fatalf("entries = %d, want 1", len(snapshot.Entries))
			}
			entry := snapshot.Entries[0]
			if got := entry.PrimarySource().Source; got != tt.source {
				t.Fatalf("primary source = %s, want %s; sources=%+v", got, tt.source, entry.Sources)
			}
			if len(entry.Sources) != 1 || entry.Sources[0].Source != tt.source || entry.Sources[0].SourceView != tt.sourceView {
				t.Fatalf("single-source feed leaked other source context: %+v", entry.Sources)
			}
		})
	}
}

func TestLoadFeedCanIncludeHiddenItems(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	item := appTestSourceItem("hn-hidden", "hackernews:hidden", "SQLite hidden discussion", domain.SourceHackerNews, 1, now)
	item.Summary = "hidden sqlite cache"
	if err := db.UpsertFeedItems(ctx, []domain.FeedItem{item}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetHidden(ctx, item.ID, true); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, httpx.New(time.Second), nil)

	withoutHidden, err := service.LoadFeed(ctx, domain.SourceAll, FeedFilter{Search: "sqlite"})
	if err != nil {
		t.Fatal(err)
	}
	if len(withoutHidden.Entries) != 0 {
		t.Fatalf("hidden item should be excluded by default: %+v", withoutHidden.Entries)
	}

	withHidden, err := service.LoadFeed(ctx, domain.SourceAll, FeedFilter{Search: "sqlite", IncludeHidden: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(withHidden.Entries) != 1 || !withHidden.Entries[0].State.Hidden {
		t.Fatalf("hidden item was not included: %+v", withHidden.Entries)
	}
	if !withHidden.Filter.IncludeHidden {
		t.Fatalf("snapshot filter did not preserve IncludeHidden: %+v", withHidden.Filter)
	}
}

func TestLoadDetailReturnsProviderContentAndBaseDetailOnProviderFailure(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	entry := domain.FeedEntry{
		Item: appTestSourceItem("repo-1", "repo:owner/repo", "owner/repo", domain.SourceGitHub, 1, now),
		Sources: []domain.ItemSource{
			{Source: domain.SourceGitHub, SourceRank: 1},
			{Source: domain.SourceHackerNews, SourceIDRaw: "42", SourceRank: 2},
		},
	}
	github := &fakeAdapter{
		source: domain.SourceGitHub,
		detail: domain.ItemDetail{
			ItemID: entry.Item.ID,
			Sections: []domain.DetailSection{{
				Title: "GitHub README",
				Body:  "README preview",
			}},
		},
	}
	hn := &fakeAdapter{source: domain.SourceHackerNews, detailErr: errors.New("hn down")}
	service := NewService(db, httpx.New(time.Second), []SourceAdapter{github, hn})

	detail, err := service.LoadDetail(ctx, entry)
	if err == nil {
		t.Fatal("expected provider error to be returned with partial detail")
	}
	if detail.ItemID != entry.Item.ID || detail.Title != "owner/repo" {
		t.Fatalf("base detail missing: %+v", detail)
	}
	if len(detail.Sections) != 1 || detail.Sections[0].Body != "README preview" {
		t.Fatalf("provider section missing: %+v", detail.Sections)
	}
	if github.detailCalls != 1 || hn.detailCalls != 1 {
		t.Fatalf("detail provider calls github=%d hn=%d", github.detailCalls, hn.detailCalls)
	}
}

func TestExportSavedWritesAllFormatsAndIncludesHiddenSavedItems(t *testing.T) {
	ctx := context.Background()
	db := openAppTestStore(t)
	defer db.Close()
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	visible := appTestSourceItem("hn-1", "hackernews:1", "Visible saved", domain.SourceHackerNews, 1, now)
	hidden := appTestSourceItem("hn-2", "hackernews:2", "Hidden saved", domain.SourceHackerNews, 2, now)
	if err := db.UpsertFeedItems(ctx, []domain.FeedItem{visible, hidden}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSaved(ctx, visible.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSaved(ctx, hidden.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := db.SetHidden(ctx, hidden.ID, true); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, httpx.New(time.Second), nil)

	for _, format := range []ExportFormat{ExportMarkdown, ExportJSON, ExportCSV} {
		t.Run(string(format), func(t *testing.T) {
			result, err := service.ExportSaved(ctx, ExportOptions{
				Format: format,
				Dir:    t.TempDir(),
				Now:    time.Date(2026, 5, 10, 12, 30, 0, 0, time.UTC),
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Count != 2 {
				t.Fatalf("count = %d, want 2", result.Count)
			}
			data, err := os.ReadFile(result.Path)
			if err != nil {
				t.Fatal(err)
			}
			text := string(data)
			if !strings.Contains(text, "Visible saved") || !strings.Contains(text, "Hidden saved") {
				t.Fatalf("export missing saved items:\n%s", text)
			}
		})
	}
}

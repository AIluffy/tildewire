package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/AIluffy/tildewire/internal/app"
	"github.com/AIluffy/tildewire/internal/domain"
)

func TestModelGitHubHeaderUsesTrendingTitle(t *testing.T) {
	service := &fakeService{snapshot: tuiSnapshot(false)}
	model := NewModel(service, tuiSnapshot(false))

	model = runKeyCommand(t, model, "1")
	header := model.renderHeader()

	if !strings.Contains(strings.ToLower(header), "view: trending") {
		t.Fatalf("github header = %q, want View: Trending", header)
	}
}

func TestModelHeaderRendersGradientTitle(t *testing.T) {
	model := NewModel(&fakeService{snapshot: tuiSnapshot(false)}, tuiSnapshot(false))

	header := model.renderHeader()
	visible := ansi.Strip(header)
	if !strings.HasPrefix(visible, "Tildewire  View: All") {
		t.Fatalf("header visible title = %q, want Tildewire prefix", visible)
	}

	firstStyle := ansiSequenceBefore(header, "T")
	accentStyle := ansiSequenceBefore(header, "w")
	if firstStyle == "" || accentStyle == "" {
		t.Fatalf("title was not styled:\n%q", header)
	}
	if !strings.Contains(firstStyle, "\x1b[1") {
		t.Fatalf("title first style = %q, want bold style", firstStyle)
	}
	if firstStyle == accentStyle {
		t.Fatalf("title should use multiple gradient colors, got %q", firstStyle)
	}
}

func TestModelRenderSourceBadgesAndCounts(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Entries[0].Sources = []domain.ItemSource{{Source: domain.SourceGitHub, SourceRank: 1}}
	model := NewModel(&fakeService{snapshot: snapshot}, snapshot)
	view := model.render()
	if !strings.Contains(view, "[GH]") {
		t.Fatalf("render missing GitHub badge:\n%s", view)
	}
	if strings.Contains(view, "GitHub disabled") {
		t.Fatalf("GitHub should not render disabled:\n%s", view)
	}
	if !strings.Contains(view, "HF Papers") || strings.Contains(view, "disabled") {
		t.Fatalf("HF Papers should render as an enabled source:\n%s", view)
	}
}

func TestModelRenderActiveSourceCountUsesLoadedEntries(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.View = domain.SourceGitHub
	snapshot.Filter = app.FeedFilter{SourceView: "trending:daily:spoken:zh"}
	snapshot.Entries = []domain.FeedEntry{
		feedEntry("gh-1", "GitHub one", domain.SourceGitHub, 1, snapshot.LoadedAt),
		feedEntry("gh-2", "GitHub two", domain.SourceGitHub, 2, snapshot.LoadedAt),
	}
	snapshot.Counts[domain.SourceGitHub] = 162
	model := NewModel(&fakeService{snapshot: snapshot}, snapshot)

	rendered := ansi.Strip(model.renderSources(24, 8))
	for _, line := range strings.Split(rendered, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "GitHub" {
			if fields[1] != "2" {
				t.Fatalf("active GitHub count = %s, want 2:\n%s", fields[1], rendered)
			}
			return
		}
	}
	t.Fatalf("active GitHub row missing:\n%s", rendered)
}

func TestModelRenderActiveAllCountUsesSnapshotTotal(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.View = domain.SourceAll
	snapshot.Counts[domain.SourceAll] = 410
	model := NewModel(&fakeService{snapshot: snapshot}, snapshot)

	rendered := ansi.Strip(model.renderSources(24, 8))
	for _, line := range strings.Split(rendered, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "All" {
			if fields[1] != "410" {
				t.Fatalf("active All count = %s, want 410:\n%s", fields[1], rendered)
			}
			return
		}
	}
	t.Fatalf("active All row missing:\n%s", rendered)
}

func TestModelRenderSourceBadgesUseDistinctColors(t *testing.T) {
	snapshot := tuiSnapshot(false)
	now := snapshot.LoadedAt
	snapshot.Entries = []domain.FeedEntry{
		feedEntry("gh", "GitHub item", domain.SourceGitHub, 1, now),
		feedEntry("hn", "Hacker News item", domain.SourceHackerNews, 2, now),
		feedEntry("hf", "HF item", domain.SourceHuggingFace, 3, now),
	}
	model := NewModel(&fakeService{snapshot: snapshot}, snapshot)
	model.cursor = len(snapshot.Entries)

	rendered := model.renderFeed(80, 11)
	visible := ansi.Strip(rendered)
	for _, badge := range []string{"[GH]", "[HN]", "[HF]"} {
		if !strings.Contains(visible, badge) {
			t.Fatalf("feed render missing visible badge %s:\n%s", badge, rendered)
		}
	}

	colors := map[string]string{}
	for _, badge := range []string{"[GH]", "[HN]", "[HF]"} {
		colors[badge] = ansiSequenceBefore(rendered, badge)
		if colors[badge] == "" {
			t.Fatalf("badge %s was not colorized:\n%s", badge, rendered)
		}
	}
	if colors["[GH]"] == colors["[HN]"] || colors["[GH]"] == colors["[HF]"] || colors["[HN]"] == colors["[HF]"] {
		t.Fatalf("source badge colors should differ, got GH=%q HN=%q HF=%q", colors["[GH]"], colors["[HN]"], colors["[HF]"])
	}
}

func TestModelRenderFeedSeparatesTitleFromContent(t *testing.T) {
	snapshot := tuiSnapshot(false)
	model := NewModel(&fakeService{snapshot: snapshot}, snapshot)

	rendered := ansi.Strip(model.renderFeed(80, 8))
	lines := strings.Split(rendered, "\n")
	if len(lines) < 3 {
		t.Fatalf("feed rendered too few lines:\n%s", rendered)
	}
	if lines[0] != "FEED" {
		t.Fatalf("feed title line = %q, want FEED:\n%s", lines[0], rendered)
	}
	if strings.TrimSpace(lines[1]) != "" {
		t.Fatalf("feed title margin line should be blank, got %q:\n%s", lines[1], rendered)
	}
	if !strings.Contains(lines[2], "First") {
		t.Fatalf("first item should start after title margin:\n%s", rendered)
	}
}

func TestModelRenderFeedAddsMarginBetweenItems(t *testing.T) {
	snapshot := tuiSnapshot(false)
	model := NewModel(&fakeService{snapshot: snapshot}, snapshot)

	rendered := ansi.Strip(model.renderFeed(80, 8))
	lines := strings.Split(rendered, "\n")
	if len(lines) < 5 {
		t.Fatalf("feed rendered too few lines:\n%s", rendered)
	}
	if strings.TrimSpace(lines[4]) != "" {
		t.Fatalf("feed item margin line should be blank, got %q:\n%s", lines[4], rendered)
	}
	if !strings.Contains(lines[5], "Second") {
		t.Fatalf("second item should start after margin line:\n%s", rendered)
	}
}

func TestModelRenderKeepsMainPanelsWithinTerminalWidth(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Entries[0].Item.Title = "A very long HF paper title with wide characters 模型模型模型 and no helpful short ending"
	snapshot.Entries[0].Item.Summary = longPreviewSummary()
	snapshot.Entries[0].Item.URL = "https://huggingface.co/papers/2026/05/11/a-very-long-paper-url-that-must-not-soft-wrap-inside-preview"
	snapshot.Entries[0].Sources = []domain.ItemSource{{Source: domain.SourceHuggingFace, SourceRank: 1}}
	model := NewModel(&fakeService{snapshot: snapshot}, snapshot)
	model.width = 84
	model.height = 12

	for lineNumber, line := range strings.Split(model.render(), "\n") {
		if got := lipgloss.Width(line); got > model.width {
			t.Fatalf("render line %d width = %d, want <= %d:\n%s", lineNumber+1, got, model.width, line)
		}
	}
}

func TestModelRenderKeepsHomeShortcutsAtTerminalBottom(t *testing.T) {
	model := NewModel(&fakeService{snapshot: tuiSnapshot(false)}, tuiSnapshot(false))
	model.width = 220
	model.height = 18

	rendered := ansi.Strip(model.render())
	lines := strings.Split(rendered, "\n")
	if len(lines) != model.height {
		t.Fatalf("home render height = %d, want %d:\n%s", len(lines), model.height, rendered)
	}
	footer := lines[len(lines)-1]
	for _, want := range []string{"left focus left", "s save"} {
		if !strings.Contains(footer, want) {
			t.Fatalf("home footer missing %q on bottom line %q:\n%s", want, footer, rendered)
		}
	}
}

func TestModelMainLayoutGivesPreviewMoreRoomWithoutCrowdingFeed(t *testing.T) {
	model := NewModel(&fakeService{snapshot: tuiSnapshot(false)}, tuiSnapshot(false))
	model.width = 120
	model.height = 22

	layout := model.mainLayout()

	if layout.preview.width <= model.width/3 {
		t.Fatalf("preview width = %d, want more than one third of terminal width", layout.preview.width)
	}
	if layout.feed.width < layout.preview.width {
		t.Fatalf("feed width = %d, want at least preview width %d", layout.feed.width, layout.preview.width)
	}
	if got := layout.sources.width + layout.feed.width + layout.preview.width; got != model.width {
		t.Fatalf("panel widths sum = %d, want %d", got, model.width)
	}
}

func TestModelMainLayoutPreservesPanelMinimumsInNarrowTerminal(t *testing.T) {
	model := NewModel(&fakeService{snapshot: tuiSnapshot(false)}, tuiSnapshot(false))
	model.width = 40
	model.height = 12

	layout := model.mainLayout()

	if got := layout.sources.width + layout.feed.width + layout.preview.width; got != model.width {
		t.Fatalf("panel widths sum = %d, want %d", got, model.width)
	}
	for _, box := range []panelBox{layout.sources, layout.feed, layout.preview} {
		if box.contentWidth < 6 {
			t.Fatalf("panel %v content width = %d, want at least 6", box.panel, box.contentWidth)
		}
	}
}

func TestModelPreviewKeepsFixedHeightAndScrollsWithinPanel(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Entries[0].Item.Title = "Preview start"
	snapshot.Entries[0].Item.Summary = longPreviewSummary()
	model := NewModel(&fakeService{snapshot: snapshot}, snapshot)
	model.width = 84
	model.height = 11

	rendered := model.renderPreview(28, 6)
	if got := renderedLineCount(rendered); got != 6 {
		t.Fatalf("preview rendered %d lines, want 6:\n%s", got, rendered)
	}
	if strings.Contains(rendered, "final-marker") {
		t.Fatalf("preview should hide overflow content before scrolling:\n%s", rendered)
	}

	for range 10 {
		model, _ = updateModelWithKey(t, model, "pgdown")
	}
	rendered = model.renderPreview(28, 6)
	if got := renderedLineCount(rendered); got != 6 {
		t.Fatalf("scrolled preview rendered %d lines, want 6:\n%s", got, rendered)
	}
	if !strings.Contains(rendered, "final-marker") {
		t.Fatalf("preview should reveal overflow content after scrolling:\n%s", rendered)
	}
}

func TestModelPreviewScrollResetsWhenSelectionChanges(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Entries[0].Item.Title = "Preview start"
	snapshot.Entries[0].Item.Summary = longPreviewSummary()
	model := NewModel(&fakeService{snapshot: snapshot}, snapshot)
	model.width = 84
	model.height = 11

	for range 10 {
		model, _ = updateModelWithKey(t, model, "pgdown")
	}
	model, _ = updateModelWithKey(t, model, "j")
	model, _ = updateModelWithKey(t, model, "k")

	rendered := model.renderPreview(28, 6)
	if strings.Contains(rendered, "final-marker") {
		t.Fatalf("preview scroll should reset when selection changes:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Preview start") {
		t.Fatalf("preview should return to the top of the selected item:\n%s", rendered)
	}
}

func TestModelPreviewRendersCompactSourceNativeSignals(t *testing.T) {
	snapshot := tuiSnapshot(false)
	stars := int64(18400)
	starsToday := int64(812)
	forks := int64(77)
	snapshot.Entries[0] = domain.FeedEntry{
		Item: domain.FeedItem{
			ID:          "gh-1",
			Title:       "openai/codex",
			Summary:     "A lightweight coding agent that runs in your terminal.",
			URL:         "https://github.com/openai/codex",
			CommentsURL: "https://github.com/openai/codex/issues",
			ItemType:    "repo",
			Author:      "OpenAI",
			Language:    "Go",
			Tags:        []string{"github", "trending", "daily", "agent"},
			LastSeenAt:  time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC),
			Metrics:     domain.Metrics{Stars: &stars, StarsToday: &starsToday, Forks: &forks},
			Refs:        domain.Refs{Repo: "openai/codex"},
		},
		Sources: []domain.ItemSource{{
			Source:     domain.SourceGitHub,
			SourceView: "trending:daily",
			SourceRank: 2,
			Metrics:    domain.Metrics{Stars: &stars, StarsToday: &starsToday, Forks: &forks},
		}},
		State: domain.ItemState{ItemID: "gh-1"},
	}
	model := NewModel(&fakeService{snapshot: snapshot}, snapshot)

	rendered := ansi.Strip(model.renderPreview(84, 12))
	for _, want := range []string{
		"PREVIEW [GH]",
		"openai/codex",
		"#2",
		"18400 stars",
		"812 today",
		"77 forks",
		"A lightweight coding agent",
		"Go",
		"agent",
		"https://github.com/openai/codex",
		"https://github.com/openai/codex/issues",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("preview missing %q:\n%s", want, rendered)
		}
	}
}

func TestModelPreviewRendersHackerNewsMetrics(t *testing.T) {
	snapshot := tuiSnapshot(false)
	points := int64(421)
	comments := int64(88)
	snapshot.Entries[0] = domain.FeedEntry{
		Item: domain.FeedItem{
			ID:          "hn-1",
			Title:       "SQLite discussion",
			Summary:     "A deep thread about SQLite internals.",
			URL:         "https://news.ycombinator.com/item?id=42",
			CommentsURL: "https://news.ycombinator.com/item?id=42",
			Tags:        []string{"hackernews", "top"},
			Metrics:     domain.Metrics{Upvotes: &points, Comments: &comments},
		},
		Sources: []domain.ItemSource{{
			Source:     domain.SourceHackerNews,
			SourceView: "top",
			SourceRank: 6,
			Metrics:    domain.Metrics{Upvotes: &points, Comments: &comments},
		}},
		State: domain.ItemState{ItemID: "hn-1"},
	}
	model := NewModel(&fakeService{snapshot: snapshot}, snapshot)

	rendered := ansi.Strip(model.renderPreview(44, 10))
	for _, want := range []string{"PREVIEW [HN]", "#6 top", "421 points", "88 comments", "SQLite discussion"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("HN preview missing %q:\n%s", want, rendered)
		}
	}
}

func TestModelPreviewSkipsMissingMetricsWithoutZeroes(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Entries[0] = domain.FeedEntry{
		Item: domain.FeedItem{
			ID:      "minimal",
			Title:   "Minimal item",
			Summary: "Summary only.",
			URL:     "https://example.com/minimal",
		},
		Sources: []domain.ItemSource{{Source: domain.SourceGitHub}},
		State:   domain.ItemState{ItemID: "minimal"},
	}
	model := NewModel(&fakeService{snapshot: snapshot}, snapshot)

	rendered := ansi.Strip(model.renderPreview(42, 8))
	for _, want := range []string{"PREVIEW [GH]", "Minimal item", "Summary only.", "https://example.com/minimal"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("minimal preview missing %q:\n%s", want, rendered)
		}
	}
	for _, unwanted := range []string{"#0", "0 stars", "0 comments", "0 forks"} {
		if strings.Contains(rendered, unwanted) {
			t.Fatalf("minimal preview rendered misleading zero %q:\n%s", unwanted, rendered)
		}
	}
}

func TestModelViewEnablesMouseCellMotion(t *testing.T) {
	model := NewModel(&fakeService{snapshot: tuiSnapshot(false)}, tuiSnapshot(false))
	view := model.View()
	if view.MouseMode != tea.MouseModeCellMotion {
		t.Fatalf("mouse mode = %v, want cell motion", view.MouseMode)
	}
}

func TestModelMouseClickFeedSelectsRowAndResetsPreviewScroll(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Entries[0].Item.Summary = longPreviewSummary()
	service := &fakeService{snapshot: snapshot}
	model := NewModel(service, snapshot)
	model.width = 100
	model.height = 16

	model.scrollPreview(20)
	if model.previewOffset == 0 {
		t.Fatal("test setup expected preview to be scrollable")
	}
	layout := model.mainLayout()
	updated, cmd := model.Update(mouseClick(layout.feed.contentX+2, layout.feed.contentY+model.feedTopRows()+feedItemContentRows))
	model = updated.(Model)
	if cmd != nil {
		t.Fatal("feed margin click should not load data")
	}
	if model.cursor != 0 {
		t.Fatalf("margin click changed cursor = %d, want 0", model.cursor)
	}

	updated, cmd = model.Update(mouseClick(layout.feed.contentX+2, layout.feed.contentY+model.feedTopRows()+feedItemBlockRows))
	model = updated.(Model)

	if cmd != nil {
		t.Fatal("feed click should not load data")
	}
	if model.cursor != 1 {
		t.Fatalf("cursor = %d, want second feed row", model.cursor)
	}
	if model.previewOffset != 0 {
		t.Fatalf("preview offset = %d, want reset", model.previewOffset)
	}
}

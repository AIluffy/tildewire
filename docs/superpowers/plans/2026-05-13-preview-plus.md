# Preview+ Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Upgrade the right-side preview panel into a compact decision surface that shows source-native heat signals and content cues without loading network detail.

**Architecture:** Keep the change inside `internal/tui`. `renderPreview()` continues to own viewport height and scroll state, while small pure helpers assemble the preview header, metrics, content cues, and links from the selected `domain.FeedEntry`.

**Tech Stack:** Go, Bubble Tea v2, Lip Gloss v2, existing `domain.FeedEntry` and `app.SourceBadge`/`SourceViewLabel` catalog helpers.

---

## File Structure

- Modify `internal/tui/render.go`: replace the current simple preview row assembly with Preview+ helpers, keeping fixed-height and scroll behavior intact.
- Modify `internal/tui/model_test.go`: add focused tests for compact metrics, graceful degradation, non-GitHub metrics, and no detail loading from preview.
- Do not modify `internal/app`, `internal/domain`, `internal/sources`, `internal/store`, or generated code.

### Task 1: Add Preview+ TUI Tests

**Files:**
- Modify: `internal/tui/model_test.go`

- [ ] **Step 1: Add tests for compact source-native preview content**

Insert these tests after `TestModelPreviewScrollResetsWhenSelectionChanges` in `internal/tui/model_test.go`:

```go
func TestModelPreviewRendersCompactSourceNativeSignals(t *testing.T) {
	snapshot := tuiSnapshot(false)
	stars := int64(18400)
	starsToday := int64(812)
	forks := int64(77)
	snapshot.Entries[0] = domain.FeedEntry{
		Item: domain.FeedItem{
			ID:           "gh-1",
			Title:        "openai/codex",
			Summary:      "A lightweight coding agent that runs in your terminal.",
			URL:          "https://github.com/openai/codex",
			CommentsURL:  "https://github.com/openai/codex/issues",
			ItemType:     "repo",
			Author:       "OpenAI",
			Language:     "Go",
			Tags:         []string{"github", "trending", "daily", "agent"},
			LastSeenAt:   time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC),
			Metrics:      domain.Metrics{Stars: &stars, StarsToday: &starsToday, Forks: &forks},
			Refs:         domain.Refs{Repo: "openai/codex"},
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

	rendered := ansi.Strip(model.renderPreview(48, 12))
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

func TestModelPreviewDoesNotLoadDetail(t *testing.T) {
	snapshot := tuiSnapshot(false)
	service := &fakeService{snapshot: snapshot}
	model := NewModel(service, snapshot)

	updated, cmd := model.Update(keyPress("right"))
	model = updated.(Model)
	if cmd != nil {
		t.Fatal("focusing preview should not run a command")
	}
	updated, cmd = model.Update(keyPress("down"))
	model = updated.(Model)
	if cmd != nil {
		t.Fatal("scrolling preview should not load detail")
	}
	if service.detailCalls != 0 {
		t.Fatalf("preview loaded detail %d times, want 0", service.detailCalls)
	}
}
```

- [ ] **Step 2: Run the new tests and verify failure**

Run:

```bash
go test ./internal/tui -run 'TestModelPreview(RendersCompactSourceNativeSignals|RendersHackerNewsMetrics|SkipsMissingMetricsWithoutZeroes|DoesNotLoadDetail)$' -count=1
```

Expected before implementation: the first three tests fail because the current preview does not render source badges and source-native metric rows. `TestModelPreviewDoesNotLoadDetail` should pass because preview navigation currently does not call `LoadDetail`.

- [ ] **Step 3: Commit only if this task is executed as a standalone checkpoint**

Do not commit after this failing-test-only task if the next implementation task will be completed in the same session. Keep the failing tests local until Task 2 makes them pass.

### Task 2: Implement Preview+ Helpers

**Files:**
- Modify: `internal/tui/render.go`

- [ ] **Step 1: Replace `renderPreview` header construction**

In `internal/tui/render.go`, replace the local `header := "PREVIEW"` block inside `renderPreview` with:

```go
header := m.previewHeader(offset, limit, width)
if height == 1 {
	return header
}
end := min(len(content), offset+visibleHeight)
lines := append([]string{header}, content[offset:end]...)
return fillLines(lines, height)
```

Add this helper near `renderPreview`:

```go
func (m Model) previewHeader(offset, limit, width int) string {
	header := "PREVIEW"
	if entry, ok := m.selected(); ok {
		if source := entry.PrimarySource().Source; source != "" {
			header = fmt.Sprintf("PREVIEW [%s]", app.SourceBadge(source))
		}
	}
	if limit > 0 {
		header = fmt.Sprintf("%s %d/%d", header, offset+1, limit+1)
	}
	return clip(header, width)
}
```

- [ ] **Step 2: Replace `previewContentLines` with compact assembly**

Replace the current `previewContentLines` function with:

```go
func (m Model) previewContentLines(width int) []string {
	lines := []string{}
	entry, ok := m.selected()
	if !ok {
		lines = append(lines, "", mutedStyle.Render("Select an item."))
		return lines
	}
	lines = append(lines, "", activeStyle.Render(clip(entry.Item.Title, width)))
	if metrics := previewMetricLine(entry); metrics != "" {
		lines = append(lines, mutedStyle.Render(clip(metrics, width)))
	}
	if entry.Item.Summary != "" {
		lines = append(lines, "")
		lines = append(lines, wrap(entry.Item.Summary, width)...)
	}
	if cues := previewContentCueLines(entry, width); len(cues) > 0 {
		lines = append(lines, "")
		lines = append(lines, cues...)
	}
	if links := previewLinkLines(entry, width); len(links) > 0 {
		lines = append(lines, "")
		lines = append(lines, links...)
	}
	return lines
}
```

- [ ] **Step 3: Add source-native metric helpers**

Add these helpers below `previewContentLines`:

```go
func previewMetricLine(entry domain.FeedEntry) string {
	source := entry.PrimarySource()
	metrics := source.Metrics
	metrics = mergePreviewMetrics(metrics, entry.Item.Metrics)
	parts := previewRankParts(source)
	switch source.Source {
	case domain.SourceGitHub:
		parts = append(parts, githubPreviewMetricParts(metrics)...)
	case domain.SourceHackerNews:
		parts = append(parts, hackerNewsPreviewMetricParts(metrics)...)
	case domain.SourceHuggingFace:
		parts = append(parts, huggingFacePreviewMetricParts(entry, metrics)...)
	case domain.SourceLobsters:
		parts = append(parts, lobstersPreviewMetricParts(metrics)...)
	case domain.SourceProductHunt:
		parts = append(parts, productHuntPreviewMetricParts(metrics)...)
	}
	return strings.Join(parts, " | ")
}

func previewRankParts(source domain.ItemSource) []string {
	if source.SourceRank <= 0 {
		return nil
	}
	rank := fmt.Sprintf("#%d", source.SourceRank)
	if source.SourceView != "" {
		rank += " " + source.SourceView
	}
	return []string{rank}
}

func mergePreviewMetrics(primary, fallback domain.Metrics) domain.Metrics {
	if primary.Score == nil {
		primary.Score = fallback.Score
	}
	if primary.Upvotes == nil {
		primary.Upvotes = fallback.Upvotes
	}
	if primary.Comments == nil {
		primary.Comments = fallback.Comments
	}
	if primary.Stars == nil {
		primary.Stars = fallback.Stars
	}
	if primary.StarsToday == nil {
		primary.StarsToday = fallback.StarsToday
	}
	if primary.Forks == nil {
		primary.Forks = fallback.Forks
	}
	if primary.GitHubStars == nil {
		primary.GitHubStars = fallback.GitHubStars
	}
	return primary
}

func githubPreviewMetricParts(metrics domain.Metrics) []string {
	var parts []string
	if metrics.Stars != nil {
		parts = append(parts, fmt.Sprintf("%d stars", *metrics.Stars))
	}
	if metrics.StarsToday != nil {
		parts = append(parts, fmt.Sprintf("%d today", *metrics.StarsToday))
	}
	if metrics.Forks != nil {
		parts = append(parts, fmt.Sprintf("%d forks", *metrics.Forks))
	}
	return parts
}

func hackerNewsPreviewMetricParts(metrics domain.Metrics) []string {
	var parts []string
	if metrics.Upvotes != nil {
		parts = append(parts, fmt.Sprintf("%d points", *metrics.Upvotes))
	}
	if metrics.Comments != nil {
		parts = append(parts, fmt.Sprintf("%d comments", *metrics.Comments))
	}
	return parts
}

func huggingFacePreviewMetricParts(entry domain.FeedEntry, metrics domain.Metrics) []string {
	var parts []string
	if entry.Item.Refs.PaperID != "" {
		parts = append(parts, "paper "+entry.Item.Refs.PaperID)
	} else if entry.Item.Refs.ArxivID != "" {
		parts = append(parts, "arXiv "+entry.Item.Refs.ArxivID)
	}
	if entry.Item.Refs.Repo != "" {
		parts = append(parts, "repo "+entry.Item.Refs.Repo)
	}
	if metrics.Upvotes != nil {
		parts = append(parts, fmt.Sprintf("%d upvotes", *metrics.Upvotes))
	}
	if metrics.Comments != nil {
		parts = append(parts, fmt.Sprintf("%d comments", *metrics.Comments))
	}
	if metrics.GitHubStars != nil {
		parts = append(parts, fmt.Sprintf("%d github stars", *metrics.GitHubStars))
	}
	return parts
}

func lobstersPreviewMetricParts(metrics domain.Metrics) []string {
	var parts []string
	if metrics.Score != nil {
		parts = append(parts, fmt.Sprintf("%g pts", *metrics.Score))
	} else if metrics.Upvotes != nil {
		parts = append(parts, fmt.Sprintf("%d pts", *metrics.Upvotes))
	}
	if metrics.Comments != nil {
		parts = append(parts, fmt.Sprintf("%d comments", *metrics.Comments))
	}
	return parts
}

func productHuntPreviewMetricParts(metrics domain.Metrics) []string {
	var parts []string
	if metrics.Upvotes != nil {
		parts = append(parts, fmt.Sprintf("%d votes", *metrics.Upvotes))
	}
	if metrics.Comments != nil {
		parts = append(parts, fmt.Sprintf("%d comments", *metrics.Comments))
	}
	return parts
}
```

- [ ] **Step 4: Add content cue and link helpers**

Add these helpers below the metric helpers:

```go
func previewContentCueLines(entry domain.FeedEntry, width int) []string {
	item := entry.Item
	var parts []string
	if item.Language != "" {
		parts = append(parts, item.Language)
	}
	if item.ItemType != "" {
		parts = append(parts, item.ItemType)
	}
	if item.Author != "" {
		parts = append(parts, "by "+item.Author)
	}
	if item.Organization != "" {
		parts = append(parts, item.Organization)
	}
	for _, tag := range item.Tags {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			parts = append(parts, tag)
		}
	}
	if len(parts) == 0 {
		return nil
	}
	return []string{mutedStyle.Render(clip(strings.Join(parts, " | "), width))}
}

func previewLinkLines(entry domain.FeedEntry, width int) []string {
	var lines []string
	if entry.Item.URL != "" {
		lines = append(lines, clip(entry.Item.URL, width))
	}
	if entry.Item.CommentsURL != "" && entry.Item.CommentsURL != entry.Item.URL {
		lines = append(lines, mutedStyle.Render(clip(entry.Item.CommentsURL, width)))
	}
	return lines
}
```

- [ ] **Step 5: Run the focused tests and verify pass**

Run:

```bash
go test ./internal/tui -run 'TestModelPreview(RendersCompactSourceNativeSignals|RendersHackerNewsMetrics|SkipsMissingMetricsWithoutZeroes|DoesNotLoadDetail|KeepsFixedHeightAndScrollsWithinPanel|ScrollResetsWhenSelectionChanges)$' -count=1
```

Expected after implementation: all listed tests pass.

### Task 3: Validate and Clean Up

**Files:**
- Modify: `internal/tui/render.go`
- Modify: `internal/tui/model_test.go`

- [ ] **Step 1: Format changed Go files**

Run:

```bash
gofmt -w internal/tui/render.go internal/tui/model_test.go
```

Expected: command exits successfully and produces no output.

- [ ] **Step 2: Run narrow TUI tests**

Run:

```bash
go test ./internal/tui -count=1
```

Expected: `ok  	tildewire/internal/tui`.

- [ ] **Step 3: Run repo verification**

Run:

```bash
go test ./...
go build -o tildewire .
go run . --help
```

Expected:
- `go test ./...` passes.
- `go build -o tildewire .` exits successfully.
- `go run . --help` prints the supported command shape including `--config`, `--debug`, `--version`, and `--help`.

- [ ] **Step 4: Review diff scope**

Run:

```bash
git diff -- internal/tui/render.go internal/tui/model_test.go
git status --short
```

Expected:
- Code diff is limited to preview rendering helpers and preview tests.
- No source, app, store, generated, or migration files changed for Preview+.
- Existing unrelated staged `.codex/config.toml` and untracked `.superpowers/` are left untouched.

- [ ] **Step 5: Commit only if requested**

If the user asks for a commit, run:

```bash
git add internal/tui/render.go internal/tui/model_test.go docs/superpowers/plans/2026-05-13-preview-plus.md
git commit -m "feat(tui): enrich preview signals"
```

Expected: commit includes only the Preview+ implementation, tests, and implementation plan.

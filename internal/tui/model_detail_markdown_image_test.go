package tui

import (
	"errors"
	"image/color"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/AIluffy/tildewire/internal/config"
	"github.com/AIluffy/tildewire/internal/domain"
)

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

func TestModelHealthViewRendersSourceDetailsAndKeepsHelp(t *testing.T) {
	snapshot := tuiSnapshot(false)
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	snapshot.Statuses = []domain.SourceHealth{{
		Source:        domain.SourceGitHub,
		Name:          "GitHub Trending",
		Status:        domain.SourceStatusRateLimited,
		LastFetchAt:   &now,
		LastSuccessAt: &now,
		LastError:     "http 429 for https://github.com/trending",
	}}
	snapshot.FetchHistory = []domain.FetchEvent{{
		Source:      domain.SourceGitHub,
		SourceView:  "trending:daily",
		Status:      domain.SourceStatusRateLimited,
		StartedAt:   now.Add(-2 * time.Second),
		FinishedAt:  now,
		Duration:    2 * time.Second,
		ItemCount:   7,
		Stale:       true,
		StaleReason: "RATE_LIMITED",
		Error:       "served stale cache",
	}}
	model := testModel(snapshot)

	model, cmd := updateModelWithKey(t, model, "!")
	if cmd != nil {
		t.Fatal("health view should not run a command")
	}
	rendered := model.render()
	for _, want := range []string{"SOURCE HEALTH", "GitHub Trending", "RATE_LIMITED", "http 429", "RECENT FETCHES", "trending:daily", "2s", "7 items", "served stale cache"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("health render missing %q:\n%s", want, rendered)
		}
	}

	model, _ = updateModelWithKey(t, model, "?")
	if !model.help.ShowAll {
		t.Fatal("help should still toggle from health view")
	}
	model, _ = updateModelWithKey(t, model, "esc")
	if strings.Contains(model.render(), "SOURCE HEALTH") {
		t.Fatalf("esc should return to feed:\n%s", model.render())
	}
}

func TestModelDetailUsesGlamourAndFallsBack(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Entries[0].Item.Summary = "A **markdown** summary."
	model := testModel(snapshot, ModelOptions{Config: config.Config{GlamourStyle: "dark"}})

	model, _ = updateModelWithKey(t, model, "enter")
	rendered := model.render()
	visible := ansi.Strip(rendered)
	if strings.Contains(visible, "Tildewire detail") {
		t.Fatalf("detail render should not include title:\n%s", rendered)
	}
	for _, want := range []string{"First", "markdown", "https://example.com/1"} {
		if !strings.Contains(visible, want) {
			t.Fatalf("detail render missing %q:\n%s", want, rendered)
		}
	}

	model.config.GlamourStyle = "missing-style"
	rendered = model.render()
	visible = ansi.Strip(rendered)
	if strings.Contains(visible, "Tildewire detail") {
		t.Fatalf("fallback detail render should not include title:\n%s", rendered)
	}
	for _, want := range []string{"First", "A **markdown** summary."} {
		if !strings.Contains(visible, want) {
			t.Fatalf("fallback detail render missing %q:\n%s", want, rendered)
		}
	}
}

func TestModelRenderMarkdownUsesLipglossTableStyle(t *testing.T) {
	model := testModel(tuiSnapshot(false), ModelOptions{Config: config.Config{GlamourStyle: "dark"}})
	markdown := strings.Join([]string{
		"| What | How | Speed |",
		"| --- | --- | ---: |",
		"| Pose estimation | CSI subcarrier amplitude/phase | 171K emb/s |",
		"| Breathing detection | Bandpass filtering | 6-30 BPM |",
	}, "\n")

	rendered, err := model.renderMarkdown(markdown, 88)
	if err != nil {
		t.Fatalf("render markdown: %v", err)
	}
	visible := ansi.Strip(rendered)
	for _, want := range []string{"┌", "┐", "└", "┘", "What", "How", "Speed", "Pose estimation"} {
		if !strings.Contains(visible, want) {
			t.Fatalf("styled table missing %q:\n%s", want, visible)
		}
	}
	if strings.Contains(visible, "| --- |") {
		t.Fatalf("styled table should not expose raw markdown delimiter:\n%s", visible)
	}
}

func TestModelRenderMarkdownUsesTerminalAdaptiveCodeBlockBackground(t *testing.T) {
	model := testModel(tuiSnapshot(false), ModelOptions{Config: config.Config{GlamourStyle: "dark"}})
	markdown := strings.Join([]string{
		"```go",
		`fmt.Println("tildewire")`,
		"return nil",
		"```",
	}, "\n")

	rendered, err := model.renderMarkdown(markdown, 88)
	if err != nil {
		t.Fatalf("render markdown: %v", err)
	}
	if !strings.Contains(ansi.Strip(rendered), `fmt.Println("tildewire")`) {
		t.Fatalf("code block text missing:\n%s", rendered)
	}
	if !strings.Contains(rendered, "48;") {
		t.Fatalf("code block should use a distinct adaptive background:\n%q", rendered)
	}
}

func TestModelBackgroundColorMessageUpdatesCodeBlockTheme(t *testing.T) {
	model := testModel(tuiSnapshot(false), ModelOptions{Config: config.Config{GlamourStyle: "dark"}})
	markdown := strings.Join([]string{
		"```go",
		`fmt.Println("tildewire")`,
		"```",
	}, "\n")

	dark, err := model.renderMarkdown(markdown, 72)
	if err != nil {
		t.Fatalf("render dark markdown: %v", err)
	}
	updated, _ := model.Update(tea.BackgroundColorMsg{Color: color.RGBA{R: 245, G: 245, B: 245, A: 255}})
	model = updated.(Model)
	light, err := model.renderMarkdown(markdown, 72)
	if err != nil {
		t.Fatalf("render light markdown: %v", err)
	}
	if dark == light {
		t.Fatalf("background color message should change code block rendering")
	}
}

func TestModelRenderMarkdownRendersCodeBlockWithPaddingAndCopyAffordance(t *testing.T) {
	model := testModel(tuiSnapshot(false), ModelOptions{Config: config.Config{GlamourStyle: "dark"}})
	markdown := strings.Join([]string{
		"```sh",
		"# Download DMG, EXEs over at https://tinyhumans.ai/openhuman",
		"",
		"curl -fsSL https://raw.githubusercontent.com/tinyhumansai/openhuman/main/scripts/install.sh | bash",
		"```",
	}, "\n")

	rendered, err := model.renderMarkdown(markdown, 96)
	if err != nil {
		t.Fatalf("render markdown: %v", err)
	}
	visible := ansi.Strip(rendered)
	for _, want := range []string{"sh", "  # Download DMG", "  curl -fsSL", "⧉"} {
		if !strings.Contains(visible, want) {
			t.Fatalf("padded code block missing %q:\n%s", want, visible)
		}
	}
	if strings.Contains(visible, "c copy") {
		t.Fatalf("code block should render copy icon instead of old text affordance:\n%s", visible)
	}
	if strings.Contains(visible, "code: sh") {
		t.Fatalf("code block should not render the old external language label:\n%s", visible)
	}
	if !strings.Contains(rendered, "48;") {
		t.Fatalf("code block should use a distinct adaptive background:\n%q", rendered)
	}
}

func TestModelRenderMarkdownRendersCodeBlockWithoutFenceMarkers(t *testing.T) {
	model := testModel(tuiSnapshot(false), ModelOptions{Config: config.Config{GlamourStyle: "dark"}})
	markdown := strings.Join([]string{
		"```go",
		`fmt.Println("tildewire")`,
		"```",
	}, "\n")

	rendered, err := model.renderMarkdown(markdown, 88)
	if err != nil {
		t.Fatalf("render markdown: %v", err)
	}
	visible := ansi.Strip(rendered)
	for _, want := range []string{"go", `fmt.Println("tildewire")`, "⧉"} {
		if !strings.Contains(visible, want) {
			t.Fatalf("rendered code block missing %q:\n%s", want, visible)
		}
	}
	if strings.Contains(visible, "```") {
		t.Fatalf("raw markdown fence leaked:\n%s", visible)
	}
}

func TestModelRenderMarkdownRendersThematicBreakSeparator(t *testing.T) {
	model := testModel(tuiSnapshot(false), ModelOptions{Config: config.Config{GlamourStyle: "dark"}})
	markdown := strings.Join([]string{
		"Before",
		"",
		"----",
		"",
		"After",
	}, "\n")

	rendered, err := model.renderMarkdown(markdown, 40)
	if err != nil {
		t.Fatalf("render markdown: %v", err)
	}
	visible := ansi.Strip(rendered)
	if !strings.Contains(visible, "Before") || !strings.Contains(visible, "After") {
		t.Fatalf("content around thematic break missing:\n%s", visible)
	}
	if strings.Contains(visible, "----") {
		t.Fatalf("raw thematic break leaked:\n%s", visible)
	}
	if !strings.Contains(visible, strings.Repeat("─", 16)) {
		t.Fatalf("visible separator missing:\n%s", visible)
	}
}

func TestModelGitHubDetailRendersReadmeTripleDashAsSeparator(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Entries[0].Item = domain.FeedItem{
		ID:    "gh-readme-divider",
		Title: "owner/repo",
		URL:   "https://github.com/owner/repo",
		Refs:  domain.Refs{Repo: "owner/repo"},
	}
	snapshot.Entries[0].Sources = []domain.ItemSource{{Source: domain.SourceGitHub}}
	service := &fakeService{
		snapshot: snapshot,
		detail: domain.ItemDetail{
			ItemID: "gh-readme-divider",
			Sections: []domain.DetailSection{{
				Title:  "GitHub README",
				Source: domain.SourceGitHub,
				URL:    "https://github.com/owner/repo/blob/main/README.md",
				Body: strings.Join([]string{
					"> No hardware? Verify the signal processing pipeline with the deterministic reference signal:",
					"> `python archive/v1/data/proof/verify.py`",
					"---",
					"",
					"Next section",
				}, "\n"),
			}},
		},
	}
	model := NewModel(service, snapshot, ModelOptions{Config: config.Config{GlamourStyle: "dark"}})
	model.width = 100
	model.height = 24

	model, cmd := updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected detail load command")
	}
	model = runOptionalCmd(t, model, cmd)

	visible := ansi.Strip(model.render())
	for _, want := range []string{"No hardware?", "python archive/v1/data/proof/verify.py", "Next section"} {
		if !strings.Contains(visible, want) {
			t.Fatalf("github README detail missing %q:\n%s", want, visible)
		}
	}
	if strings.Contains(visible, "\n---\n") {
		t.Fatalf("raw README thematic break leaked:\n%s", visible)
	}
	if !strings.Contains(visible, strings.Repeat("─", 16)) {
		t.Fatalf("README separator missing:\n%s", visible)
	}
}

func TestModelGitHubDetailRendersDividerAfterProvider(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Entries[0].Item = domain.FeedItem{
		ID:    "gh-provider-divider",
		Title: "owner/repo",
		URL:   "https://github.com/owner/repo",
		Refs:  domain.Refs{Repo: "owner/repo"},
	}
	snapshot.Entries[0].Sources = []domain.ItemSource{{Source: domain.SourceGitHub}}
	service := &fakeService{
		snapshot: snapshot,
		detail: domain.ItemDetail{
			ItemID:    "gh-provider-divider",
			Providers: []domain.SourceID{domain.SourceGitHub},
			Sections: []domain.DetailSection{{
				Title:  "GitHub README",
				Source: domain.SourceGitHub,
				URL:    "https://github.com/owner/repo/blob/main/README.md",
				Body:   "Repo readme.",
			}},
		},
	}
	model := NewModel(service, snapshot, ModelOptions{Config: config.Config{GlamourStyle: "dark"}})
	model.width = 100
	model.height = 24

	model, cmd := updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected detail load command")
	}
	model = runOptionalCmd(t, model, cmd)

	lines := strings.Split(ansi.Strip(strings.Join(model.detailContentLines(model.detailContentWidth()), "\n")), "\n")
	providerLine := -1
	for idx, line := range lines {
		if strings.Contains(line, "Detail providers") {
			providerLine = idx
			break
		}
	}
	if providerLine == -1 {
		t.Fatalf("github detail missing provider line:\n%s", strings.Join(lines, "\n"))
	}
	next := ""
	for _, line := range lines[providerLine+1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		next = line
		break
	}
	if !strings.Contains(next, strings.Repeat("─", 16)) {
		t.Fatalf("provider line should be followed by divider, got %q:\n%s", next, strings.Join(lines, "\n"))
	}
	if !strings.HasPrefix(next, "  ") {
		t.Fatalf("provider divider should align with markdown document body, got %q", next)
	}
	if got, want := ansi.StringWidth(strings.TrimSpace(next)), model.detailContentWidth()-4; got != want {
		t.Fatalf("provider divider width = %d, want document width %d:\n%s", got, want, strings.Join(lines, "\n"))
	}
}

func TestModelDetailMouseClickCopiesGitHubReadmeCodeBlock(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Entries[0].Item = domain.FeedItem{
		ID:    "gh-readme-code-copy",
		Title: "owner/repo",
		URL:   "https://github.com/owner/repo",
		Refs:  domain.Refs{Repo: "owner/repo"},
	}
	snapshot.Entries[0].Sources = []domain.ItemSource{{Source: domain.SourceGitHub}}
	service := &fakeService{
		snapshot: snapshot,
		detail: domain.ItemDetail{
			ItemID: "gh-readme-code-copy",
			Sections: []domain.DetailSection{{
				Title:  "GitHub README",
				Source: domain.SourceGitHub,
				URL:    "https://github.com/owner/repo/blob/main/README.md",
				Body: strings.Join([]string{
					"```sh",
					"# For macOS or Linux x64",
					"curl -fsSL https://raw.githubusercontent.com/tinyhumansai/openhuman/main/scripts/install.sh | bash",
					"```",
				}, "\n"),
			}},
		},
	}
	model := NewModel(service, snapshot, ModelOptions{Config: config.Config{GlamourStyle: "dark"}})
	model.width = 110
	model.height = 24
	model, cmd := updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected detail load command")
	}
	model = runOptionalCmd(t, model, cmd)

	x, y, ok := visibleCellContaining(model.render(), func(line string) bool {
		return strings.Contains(line, "sh") && strings.Contains(line, "⧉")
	}, "⧉")
	if !ok {
		t.Fatalf("GitHub README code block copy icon not visible:\n%s", ansi.Strip(model.render()))
	}

	var copied string
	previousClipboard := writeClipboard
	writeClipboard = func(value string) error {
		copied = value
		return nil
	}
	defer func() { writeClipboard = previousClipboard }()

	updated, cmd := model.Update(mouseClick(x, y))
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("expected copy code command")
	}
	msg := commandMsg(t, cmd)
	status, ok := msg.(statusMsg)
	if !ok {
		t.Fatalf("copy code command returned %T, want statusMsg", msg)
	}
	if status.err != nil || status.message != "copied code block" {
		t.Fatalf("copy code status = %+v", status)
	}
	want := strings.Join([]string{
		"# For macOS or Linux x64",
		"curl -fsSL https://raw.githubusercontent.com/tinyhumansai/openhuman/main/scripts/install.sh | bash",
	}, "\n")
	if copied != want {
		t.Fatalf("copied code = %q, want %q", copied, want)
	}
}

func TestModelDetailMouseClickCopiesTargetCodeBlock(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Entries[0].Item = domain.FeedItem{
		ID:    "detail-click-code-copy",
		Title: "owner/repo",
		URL:   "https://github.com/owner/repo",
		Refs:  domain.Refs{Repo: "owner/repo"},
	}
	snapshot.Entries[0].Sources = []domain.ItemSource{{Source: domain.SourceGitHub}}
	service := &fakeService{
		snapshot: snapshot,
		detail: domain.ItemDetail{
			ItemID: "detail-click-code-copy",
			Sections: []domain.DetailSection{{
				Title:  "GitHub README",
				Source: domain.SourceGitHub,
				URL:    "https://github.com/owner/repo/blob/main/README.md",
				Body: strings.Join([]string{
					"```go",
					`fmt.Println("first")`,
					"```",
					"",
					"```sh",
					"echo second",
					"```",
				}, "\n"),
			}},
		},
	}
	model := NewModel(service, snapshot, ModelOptions{Config: config.Config{GlamourStyle: "dark"}})
	model.width = 110
	model.height = 32
	model, cmd := updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected detail load command")
	}
	model = runOptionalCmd(t, model, cmd)

	x, y, ok := visibleCellContaining(model.render(), func(line string) bool {
		return strings.Contains(line, "sh") && strings.Contains(line, "⧉")
	}, "⧉")
	if !ok {
		t.Fatalf("second code block copy icon not visible:\n%s", ansi.Strip(model.render()))
	}

	var copied string
	previousClipboard := writeClipboard
	writeClipboard = func(value string) error {
		copied = value
		return nil
	}
	defer func() { writeClipboard = previousClipboard }()

	updated, cmd := model.Update(mouseClick(x, y))
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("expected code block copy command")
	}
	msg := commandMsg(t, cmd)
	status, ok := msg.(statusMsg)
	if !ok {
		t.Fatalf("copy code command returned %T, want statusMsg", msg)
	}
	if status.err != nil || status.message != "copied code block" {
		t.Fatalf("copy code status = %+v", status)
	}
	if copied != "echo second" {
		t.Fatalf("copied code = %q, want %q", copied, "echo second")
	}
	if model.cursor != 0 || model.feedOffset != 0 || model.previewOffset != 0 {
		t.Fatalf("detail code click changed main view state: cursor=%d feed=%d preview=%d", model.cursor, model.feedOffset, model.previewOffset)
	}
}

func TestModelDetailMouseClickCopiesCodeBlockFromIconPadding(t *testing.T) {
	snapshot := tuiSnapshot(false)
	service := &fakeService{
		snapshot: snapshot,
		detail: domain.ItemDetail{
			ItemID: snapshot.Entries[0].Item.ID,
			Sections: []domain.DetailSection{{
				Title: "README",
				Body: strings.Join([]string{
					"```sh",
					"echo from padding",
					"```",
				}, "\n"),
			}},
		},
	}
	model := NewModel(service, snapshot, ModelOptions{Config: config.Config{GlamourStyle: "dark"}})
	model.width = 100
	model.height = 24
	model, cmd := updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected detail load command")
	}
	model = runOptionalCmd(t, model, cmd)

	x, y, ok := visibleCellContaining(model.render(), func(line string) bool {
		return strings.Contains(line, "sh") && strings.Contains(line, "⧉")
	}, "⧉")
	if !ok {
		t.Fatalf("code block copy icon not visible:\n%s", ansi.Strip(model.render()))
	}

	var copied string
	previousClipboard := writeClipboard
	writeClipboard = func(value string) error {
		copied = value
		return nil
	}
	defer func() { writeClipboard = previousClipboard }()

	updated, cmd := model.Update(mouseClick(x+1, y))
	_ = updated.(Model)
	if cmd == nil {
		t.Fatal("expected code block copy command from icon padding")
	}
	msg := commandMsg(t, cmd)
	status, ok := msg.(statusMsg)
	if !ok {
		t.Fatalf("copy code command returned %T, want statusMsg", msg)
	}
	if status.err != nil || status.message != "copied code block" {
		t.Fatalf("copy code status = %+v", status)
	}
	if copied != "echo from padding" {
		t.Fatalf("copied code = %q, want %q", copied, "echo from padding")
	}
}

func TestModelDetailMouseClickCopyShowsToast(t *testing.T) {
	snapshot := tuiSnapshot(false)
	service := &fakeService{
		snapshot: snapshot,
		detail: domain.ItemDetail{
			ItemID: snapshot.Entries[0].Item.ID,
			Sections: []domain.DetailSection{{
				Title: "README",
				Body: strings.Join([]string{
					"```sh",
					"echo toast",
					"```",
				}, "\n"),
			}},
		},
	}
	model := NewModel(service, snapshot, ModelOptions{Config: config.Config{GlamourStyle: "dark"}})
	model.width = 100
	model.height = 24
	model, cmd := updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected detail load command")
	}
	model = runOptionalCmd(t, model, cmd)

	x, y, ok := visibleCellContaining(model.render(), func(line string) bool {
		return strings.Contains(line, "sh") && strings.Contains(line, "⧉")
	}, "⧉")
	if !ok {
		t.Fatalf("code block copy icon not visible:\n%s", ansi.Strip(model.render()))
	}

	previousClipboard := writeClipboard
	writeClipboard = func(value string) error { return nil }
	defer func() { writeClipboard = previousClipboard }()

	updated, cmd := model.Update(mouseClick(x, y))
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("expected code block copy command")
	}
	msg := commandMsg(t, cmd)
	updated, cmd = model.Update(msg)
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("expected toast clear command")
	}
	if !strings.Contains(ansi.Strip(model.render()), "copied code block") {
		t.Fatalf("copy toast not rendered:\n%s", ansi.Strip(model.render()))
	}
	updated, cmd = model.Update(clearToastMsg(model.toastID))
	model = updated.(Model)
	if cmd != nil {
		t.Fatal("clearing toast should not return a command")
	}
	if strings.Contains(ansi.Strip(model.render()), "copied code block") {
		t.Fatalf("copy toast should clear:\n%s", ansi.Strip(model.render()))
	}
}

func TestModelDetailMouseClickCodeBodyDoesNotCopy(t *testing.T) {
	snapshot := tuiSnapshot(false)
	service := &fakeService{
		snapshot: snapshot,
		detail: domain.ItemDetail{
			ItemID: snapshot.Entries[0].Item.ID,
			Sections: []domain.DetailSection{{
				Title: "README",
				Body: strings.Join([]string{
					"```go",
					`fmt.Println("body")`,
					"```",
				}, "\n"),
			}},
		},
	}
	model := NewModel(service, snapshot, ModelOptions{Config: config.Config{GlamourStyle: "dark"}})
	model.width = 100
	model.height = 24
	model, cmd := updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected detail load command")
	}
	model = runOptionalCmd(t, model, cmd)

	x, y, ok := visibleCellContaining(model.render(), func(line string) bool {
		return strings.Contains(line, `fmt.Println("body")`)
	}, `fmt.Println("body")`)
	if !ok {
		t.Fatalf("code body not visible:\n%s", ansi.Strip(model.render()))
	}
	updated, cmd := model.Update(mouseClick(x, y))
	_ = updated.(Model)
	if cmd != nil {
		t.Fatal("clicking code body should not copy")
	}
}

func TestModelDetailKeyboardCDoesNotCopyCodeBlock(t *testing.T) {
	snapshot := tuiSnapshot(false)
	service := &fakeService{
		snapshot: snapshot,
		detail: domain.ItemDetail{
			ItemID: snapshot.Entries[0].Item.ID,
			Sections: []domain.DetailSection{{
				Title: "README",
				Body: strings.Join([]string{
					"```go",
					`fmt.Println("first")`,
					"```",
					"",
					"Some prose between blocks.",
					"",
					"```go",
					`fmt.Println("second")`,
					"```",
				}, "\n"),
			}},
		},
	}
	model := NewModel(service, snapshot, ModelOptions{Config: config.Config{GlamourStyle: "dark"}})
	model.width = 100
	model.height = 12
	model, cmd := updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected detail load command")
	}
	model = runOptionalCmd(t, model, cmd)
	lines := model.detailContentLines(model.detailContentWidth())
	secondLine := -1
	for i, line := range lines {
		if strings.Contains(ansi.Strip(line), `fmt.Println("second")`) {
			secondLine = i
			break
		}
	}
	if secondLine < 0 {
		t.Fatalf("second code block not found:\n%s", strings.Join(lines, "\n"))
	}
	model.detailOffset = max(0, secondLine-1)
	if strings.Contains(ansi.Strip(model.render()), "c copy code") {
		t.Fatalf("detail footer should not advertise keyboard code copy:\n%s", ansi.Strip(model.render()))
	}

	var copied string
	previousClipboard := writeClipboard
	writeClipboard = func(value string) error {
		copied = value
		return nil
	}
	defer func() { writeClipboard = previousClipboard }()

	model, cmd = updateModelWithKey(t, model, "c")
	if cmd != nil {
		t.Fatal("c should not trigger a detail code copy command")
	}
	if copied != "" {
		t.Fatalf("c should not copy detail code block, copied %q", copied)
	}
}

func TestModelGitHubDetailRendersReadmeTablesWithLipglossStyle(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Entries[0].Item = domain.FeedItem{
		ID:    "gh-table",
		Title: "owner/repo",
		URL:   "https://github.com/owner/repo",
		Refs:  domain.Refs{Repo: "owner/repo"},
	}
	snapshot.Entries[0].Sources = []domain.ItemSource{{Source: domain.SourceGitHub}}
	service := &fakeService{
		snapshot: snapshot,
		detail: domain.ItemDetail{
			ItemID: "gh-table",
			Sections: []domain.DetailSection{{
				Title:  "GitHub README",
				Source: domain.SourceGitHub,
				URL:    "https://github.com/owner/repo/blob/main/README.md",
				Body: strings.Join([]string{
					"## Results",
					"",
					"| What | How | Speed |",
					"| --- | --- | ---: |",
					"| 🦴 **Pose estimation** | CSI subcarrier amplitude/phase and lightweight model | 171K emb/s (M4 Pro) |",
					"| 🫁 Breathing detection | Bandpass 0.1-0.5 Hz → zero-crossing | 6-30 BPM |",
					"| 📡 **Multi-frequency** | MediaPipe + ESP32 CSI → 35%+ precision | ~19 min on laptop |",
				}, "\n"),
			}},
		},
	}
	model := NewModel(service, snapshot, ModelOptions{Config: config.Config{GlamourStyle: "dark"}})
	model.width = 110
	model.height = 22

	model, cmd := updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected detail load command")
	}
	model = runOptionalCmd(t, model, cmd)

	visible := ansi.Strip(model.render())
	for _, want := range []string{"┌", "┐", "└", "┘", "What", "How", "Speed", "Pose estimation"} {
		if !strings.Contains(visible, want) {
			t.Fatalf("github detail table missing %q:\n%s", want, visible)
		}
	}
	if strings.Contains(visible, "─┼") && !strings.Contains(visible, "┌") {
		t.Fatalf("github detail table still looks like glamour's borderless table:\n%s", visible)
	}
}

func TestModelGitHubDetailRendersSingleLineBlockquoteReadmeTables(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Entries[0].Item = domain.FeedItem{
		ID:    "gh-ruview",
		Title: "ruvnet/RuView",
		URL:   "https://github.com/ruvnet/RuView",
		Refs:  domain.Refs{Repo: "ruvnet/RuView"},
	}
	snapshot.Entries[0].Sources = []domain.ItemSource{{Source: domain.SourceGitHub}}
	service := &fakeService{
		snapshot: snapshot,
		detail: domain.ItemDetail{
			ItemID: "gh-ruview",
			Sections: []domain.DetailSection{{
				Title:  "GitHub README",
				Source: domain.SourceGitHub,
				URL:    "https://raw.githubusercontent.com/ruvnet/RuView/refs/heads/main/README.md",
				Body: strings.Join([]string{
					"Built for low-power edge applications",
					"",
					"> | What | How | Speed | > |------|-----|-------| > | 🦴 **Pose estimation** | CSI subcarrier amplitude/phase → 17 COCO keypoints | 171K emb/s (M4 Pro) | > | 🫁 Breathing detection | Bandpass 0.1-0.5 Hz → zero-crossing BPM | 6-30 BPM |",
				}, "\n"),
			}},
		},
	}
	model := NewModel(service, snapshot, ModelOptions{Config: config.Config{GlamourStyle: "dark"}})
	model.width = 110
	model.height = 24

	model, cmd := updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected detail load command")
	}
	model = runOptionalCmd(t, model, cmd)

	visible := ansi.Strip(model.render())
	for _, want := range []string{"┌", "┐", "└", "┘", "What", "How", "Speed", "Pose estimation", "Breathing detection"} {
		if !strings.Contains(visible, want) {
			t.Fatalf("single-line blockquote table missing %q:\n%s", want, visible)
		}
	}
	for _, raw := range []string{"|------|", "> |"} {
		if strings.Contains(visible, raw) {
			t.Fatalf("single-line blockquote table leaked raw markdown %q:\n%s", raw, visible)
		}
	}
}

func TestModelGitHubDetailPreservesReadmeImagesAsTerminalPlaceholders(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Entries[0].Item = domain.FeedItem{
		ID:    "gh-image",
		Title: "owner/repo",
		URL:   "https://github.com/owner/repo",
		Refs:  domain.Refs{Repo: "owner/repo"},
	}
	snapshot.Entries[0].Sources = []domain.ItemSource{{Source: domain.SourceGitHub}}
	service := &fakeService{
		snapshot: snapshot,
		detail: domain.ItemDetail{
			ItemID: "gh-image",
			Sections: []domain.DetailSection{{
				Title:  "GitHub README",
				Source: domain.SourceGitHub,
				URL:    "https://github.com/owner/repo/blob/main/README.md",
				Body: strings.Join([]string{
					"![Pose fusion demo](docs/pose-fusion.png)",
					"[![Rust 1.85+](https://img.shields.io/badge/rust-1.85+-orange.svg)](https://www.rust-lang.org/)",
				}, "\n"),
			}},
		},
	}
	model := NewModel(service, snapshot, ModelOptions{Config: config.Config{GlamourStyle: "dark", MarkdownImagePreview: config.MarkdownImagePreviewOff}})
	model.width = 110
	model.height = 20

	model, cmd := updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected detail load command")
	}
	model = runOptionalCmd(t, model, cmd)

	visible := ansi.Strip(model.render())
	for _, want := range []string{"Image: Pose fusion demo", "docs/pose-fusion.png"} {
		if !strings.Contains(visible, want) {
			t.Fatalf("readme image placeholder missing %q:\n%s", want, visible)
		}
	}
}

func TestResolveGitHubReadmeImageURLUsesRawGitHubAsset(t *testing.T) {
	got, ok := resolveGitHubReadmeImageURL("assets/v2-screen.png", "https://github.com/ruvnet/RuView/blob/main/README.md")
	if !ok {
		t.Fatal("expected relative README image to resolve")
	}
	want := "https://raw.githubusercontent.com/ruvnet/RuView/main/assets/v2-screen.png"
	if got != want {
		t.Fatalf("resolved image URL = %q, want %q", got, want)
	}
}

func TestResolveGitHubReadmeImageURLUsesRawGitHubRefsHeadURL(t *testing.T) {
	readmeURL := "https://raw.githubusercontent.com/ruvnet/RuView/refs/heads/main/README.md"
	got, ok := resolveGitHubReadmeImageURL("assets/v2-screen.png", readmeURL)
	if !ok {
		t.Fatal("expected raw refs/heads README image to resolve")
	}
	want := "https://raw.githubusercontent.com/ruvnet/RuView/main/assets/v2-screen.png"
	if got != want {
		t.Fatalf("resolved image URL = %q, want %q", got, want)
	}
}

func TestGitHubReadmeImageRefsSkipBadgesAndSupportHTMLImages(t *testing.T) {
	readmeURL := "https://github.com/owner/repo/blob/main/docs/README.md"
	markdown := strings.Join([]string{
		"[![Rust 1.85+](https://img.shields.io/badge/rust-1.85+-orange.svg)](https://www.rust-lang.org/)",
		"![Pose fusion demo](../assets/v2-screen.png)",
		"<img alt=\"Point cloud\" src=\"images/point-cloud.png\">",
	}, "\n")

	images := githubReadmeImageRefs(markdown, readmeURL)
	if len(images) != 2 {
		t.Fatalf("image refs = %#v, want two non-badge images", images)
	}
	if images[0].Alt != "Pose fusion demo" || images[0].URL != "https://raw.githubusercontent.com/owner/repo/main/assets/v2-screen.png" {
		t.Fatalf("first image ref = %#v", images[0])
	}
	if images[1].Alt != "Point cloud" || images[1].URL != "https://raw.githubusercontent.com/owner/repo/main/docs/images/point-cloud.png" {
		t.Fatalf("second image ref = %#v", images[1])
	}
}

func TestGitHubReadmeImageRefsSupportWrappedHTMLImages(t *testing.T) {
	readmeURL := "https://github.com/owner/repo/blob/main/docs/README.md"
	markdown := strings.Join([]string{
		`<p align="center"><a href="https://example.com"><img alt="Dashboard" src="../assets/dashboard.png"></a></p>`,
		`<picture><source media="(prefers-color-scheme: dark)" srcset="../assets/dark.png"><img alt="Theme preview" src="../assets/light.png"></picture>`,
	}, "\n")

	images := githubReadmeImageRefs(markdown, readmeURL)
	if len(images) != 2 {
		t.Fatalf("image refs = %#v, want two wrapped HTML images", images)
	}
	if images[0].Alt != "Dashboard" || images[0].URL != "https://raw.githubusercontent.com/owner/repo/main/assets/dashboard.png" {
		t.Fatalf("first image ref = %#v", images[0])
	}
	if images[1].Alt != "Theme preview" || images[1].URL != "https://raw.githubusercontent.com/owner/repo/main/assets/light.png" {
		t.Fatalf("second image ref = %#v", images[1])
	}
}

func TestModelGitHubDetailQueuesReadmeImagePreview(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Entries[0].Item = domain.FeedItem{
		ID:    "gh-image-preview",
		Title: "ruvnet/RuView",
		URL:   "https://github.com/ruvnet/RuView",
		Refs:  domain.Refs{Repo: "ruvnet/RuView"},
	}
	snapshot.Entries[0].Sources = []domain.ItemSource{{Source: domain.SourceGitHub}}
	service := &fakeService{
		snapshot: snapshot,
		detail: domain.ItemDetail{
			ItemID: "gh-image-preview",
			Sections: []domain.DetailSection{{
				Title:  "GitHub README",
				Source: domain.SourceGitHub,
				URL:    "https://raw.githubusercontent.com/ruvnet/RuView/refs/heads/main/README.md",
				Body: strings.Join([]string{
					"![Pose fusion demo](assets/v2-screen.png)",
					"[![Rust 1.85+](https://img.shields.io/badge/rust-1.85+-orange.svg)](https://www.rust-lang.org/)",
				}, "\n"),
			}},
		},
	}
	previewer := &fakeMarkdownImagePreviewer{
		result: markdownImagePreviewResult{Content: "rendered image block", Backend: "test"},
	}
	model := NewModel(service, snapshot, ModelOptions{
		Config:         config.Config{GlamourStyle: "dark", CacheDir: t.TempDir(), MarkdownImagePreview: "auto"},
		ImagePreviewer: previewer,
	})
	model.width = 100
	model.height = 24

	model, cmd := updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected detail load command")
	}
	model = runOptionalCmd(t, model, cmd)

	if len(previewer.requests) != 1 {
		t.Fatalf("preview requests = %#v, want one non-badge README image", previewer.requests)
	}
	if got := previewer.requests[0].URL; got != "https://raw.githubusercontent.com/ruvnet/RuView/main/assets/v2-screen.png" {
		t.Fatalf("preview URL = %q", got)
	}
	if got := previewer.requests[0].Mode; got != config.MarkdownImagePreviewHalfblocks {
		t.Fatalf("auto detail image preview mode = %q, want %q", got, config.MarkdownImagePreviewHalfblocks)
	}
	visible := ansi.Strip(model.render())
	if strings.Contains(visible, "Image: Pose fusion demo") {
		t.Fatalf("successful image preview should not include label chrome:\n%s", visible)
	}
	for _, want := range []string{"rendered image block"} {
		if !strings.Contains(visible, want) {
			t.Fatalf("rendered preview missing %q:\n%s", want, visible)
		}
	}
	if strings.Contains(visible, "Rust 1.85+") {
		t.Fatalf("badge image should not render as a preview:\n%s", visible)
	}
}

func TestModelGitHubDetailRendersWrappedReadmeImageWithoutChrome(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Entries[0].Item = domain.FeedItem{
		ID:    "gh-image-only-preview",
		Title: "ruvnet/RuView",
		URL:   "https://github.com/ruvnet/RuView",
		Refs:  domain.Refs{Repo: "ruvnet/RuView"},
	}
	snapshot.Entries[0].Sources = []domain.ItemSource{{Source: domain.SourceGitHub}}
	service := &fakeService{
		snapshot: snapshot,
		detail: domain.ItemDetail{
			ItemID: "gh-image-only-preview",
			Sections: []domain.DetailSection{{
				Title:  "GitHub README",
				Source: domain.SourceGitHub,
				URL:    "https://github.com/ruvnet/RuView/blob/main/README.md",
				Body: strings.Join([]string{
					`<a href="https://ruvnet.github.io/RuView/">`,
					`  <img alt="WiFi DensePose - Live pose detection with setup guide" src="assets/v2-screen.png">`,
					`</a>`,
				}, "\n"),
			}},
		},
	}
	previewer := &fakeMarkdownImagePreviewer{
		result: markdownImagePreviewResult{Content: "rendered image block", Backend: "kitty"},
	}
	model := NewModel(service, snapshot, ModelOptions{
		Config:         config.Config{GlamourStyle: "dark", CacheDir: t.TempDir(), MarkdownImagePreview: "auto"},
		ImagePreviewer: previewer,
	})
	model.width = 100
	model.height = 24

	model, cmd := updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected detail load command")
	}
	model = runOptionalCmd(t, model, cmd)

	visible := ansi.Strip(model.render())
	if !strings.Contains(visible, "rendered image block") {
		t.Fatalf("rendered image missing:\n%s", visible)
	}
	for _, unwanted := range []string{"<a href", "ruvnet.github.io/RuView", "Image: WiFi DensePose", "[kitty]"} {
		if strings.Contains(visible, unwanted) {
			t.Fatalf("image chrome leaked %q:\n%s", unwanted, visible)
		}
	}
}

func TestModelGitHubDetailRendersMultilineHTMLReadmeHeader(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Entries[0].Item = domain.FeedItem{
		ID:    "gh-html-readme-header",
		Title: "owner/taste-skill",
		URL:   "https://github.com/owner/taste-skill",
		Refs:  domain.Refs{Repo: "owner/taste-skill"},
	}
	snapshot.Entries[0].Sources = []domain.ItemSource{{Source: domain.SourceGitHub}}
	service := &fakeService{
		snapshot: snapshot,
		detail: domain.ItemDetail{
			ItemID: "gh-html-readme-header",
			Sections: []domain.DetailSection{{
				Title:  "GitHub README",
				Source: domain.SourceGitHub,
				URL:    "https://github.com/owner/taste-skill/blob/main/README.md",
				Body: strings.Join([]string{
					`<p align="center">`,
					`  <em>The Anti-Slop Frontend Framework for AI Agents</em>`,
					`</p>`,
					``,
					`<p align="center">`,
					`  <a href="https://tasteskill.dev" title="Taste Skill - tasteskill.dev">`,
					`    <img src="assets/taste-skill-logo.webp" width="80" height="80" alt="Taste Skill" />`,
					`  </a>`,
					`</p>`,
					``,
					`<p align="center">`,
					`  <a href="https://tasteskill.dev">`,
					`    <img src="https://img.shields.io/badge/OPEN-tasteskill.dev-%23a855f7?style=for-the-badge&labelColor=%230f172a" alt="Open tasteskill.dev" />`,
					`  </a>`,
					`</p>`,
				}, "\n"),
			}},
		},
	}
	previewer := &fakeMarkdownImagePreviewer{
		result: markdownImagePreviewResult{Content: "rendered taste skill logo", Backend: "halfblocks"},
	}
	model := NewModel(service, snapshot, ModelOptions{
		Config:         config.Config{GlamourStyle: "dark", CacheDir: t.TempDir(), MarkdownImagePreview: "auto"},
		ImagePreviewer: previewer,
	})
	model.width = 100
	model.height = 24

	model, cmd := updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected detail load command")
	}
	model = runOptionalCmd(t, model, cmd)

	if len(previewer.requests) != 1 {
		t.Fatalf("preview requests = %+v, want one non-badge logo", previewer.requests)
	}
	if got := previewer.requests[0].URL; got != "https://raw.githubusercontent.com/owner/taste-skill/main/assets/taste-skill-logo.webp" {
		t.Fatalf("preview URL = %q", got)
	}
	visible := ansi.Strip(model.render())
	for _, want := range []string{"The Anti-Slop Frontend Framework for AI Agents", "rendered taste skill logo"} {
		if !strings.Contains(visible, want) {
			t.Fatalf("rendered README missing %q:\n%s", want, visible)
		}
	}
	for _, unwanted := range []string{"<p align=", "<a href=", "<em>", "<img", "img.shields.io", "Open tasteskill.dev"} {
		if strings.Contains(visible, unwanted) {
			t.Fatalf("HTML README chrome leaked %q:\n%s", unwanted, visible)
		}
	}
}

func TestModelGitHubDetailDrawsRawImagePreviewViaRawCommand(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Entries[0].Item = domain.FeedItem{
		ID:    "gh-raw-image",
		Title: "owner/repo",
		URL:   "https://github.com/owner/repo",
		Refs:  domain.Refs{Repo: "owner/repo"},
	}
	snapshot.Entries[0].Sources = []domain.ItemSource{{Source: domain.SourceGitHub}}
	service := &fakeService{
		snapshot: snapshot,
		detail: domain.ItemDetail{
			ItemID: "gh-raw-image",
			Sections: []domain.DetailSection{{
				Title:  "GitHub README",
				Source: domain.SourceGitHub,
				URL:    "https://github.com/owner/repo/blob/main/README.md",
				Body:   "![Inline protocol image](assets/preview.png)",
			}},
		},
	}
	raw := "\x1b]1337;File=inline=1:" + strings.Repeat("A", 120) + "RAW-END\a"
	previewer := &fakeMarkdownImagePreviewer{
		result: markdownImagePreviewResult{Content: raw, Backend: "iterm", Raw: true, Rows: 4},
	}
	model := NewModel(service, snapshot, ModelOptions{
		Config:         config.Config{GlamourStyle: "dark", CacheDir: t.TempDir(), MarkdownImagePreview: "auto"},
		ImagePreviewer: previewer,
	})
	model.width = 64
	model.height = 18

	model, cmd := updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected detail load command")
	}
	updated, next := model.Update(commandMsg(t, cmd))
	model = updated.(Model)
	if next == nil {
		t.Fatal("expected image preview command")
	}
	updated, drawCmd := model.Update(commandMsg(t, next))
	model = updated.(Model)
	if drawCmd == nil {
		t.Fatal("expected raw image draw command")
	}
	drawMsg := drawCmd()
	rawMsg, ok := drawMsg.(tea.RawMsg)
	if !ok {
		t.Fatalf("draw command returned %T, want tea.RawMsg", drawMsg)
	}
	draw, ok := rawMsg.Msg.(string)
	if !ok {
		t.Fatalf("raw message payload = %T, want string", rawMsg.Msg)
	}
	for _, want := range []string{ansi.SaveCursor, "\x1b[", "RAW-END", ansi.RestoreCursor} {
		if !strings.Contains(draw, want) {
			t.Fatalf("raw draw payload missing %q: %q", want, draw)
		}
	}

	rendered := model.render()
	if strings.Contains(rendered, "RAW-END") {
		t.Fatalf("raw image protocol should not be embedded in View content:\n%s", rendered)
	}
	if strings.Contains(rendered, detailRawLinePrefix) {
		t.Fatalf("raw line sentinel leaked into render output:\n%s", rendered)
	}
	if strings.Contains(ansi.Strip(rendered), "Image: Inline protocol image") {
		t.Fatalf("raw image preview should not include label chrome:\n%s", rendered)
	}
}

func TestModelGitHubDetailCentersRawImagePreviewWithSpacing(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Entries[0].Item = domain.FeedItem{
		ID:    "gh-raw-image-centered",
		Title: "owner/repo",
		URL:   "https://github.com/owner/repo",
		Refs:  domain.Refs{Repo: "owner/repo"},
	}
	snapshot.Entries[0].Sources = []domain.ItemSource{{Source: domain.SourceGitHub}}
	service := &fakeService{
		snapshot: snapshot,
		detail: domain.ItemDetail{
			ItemID: "gh-raw-image-centered",
			Sections: []domain.DetailSection{{
				Title:  "GitHub README",
				Source: domain.SourceGitHub,
				URL:    "https://github.com/owner/repo/blob/main/README.md",
				Body: strings.Join([]string{
					"![Centered image](assets/preview.png)",
					"",
					"After image marker",
				}, "\n"),
			}},
		},
	}
	previewer := &fakeMarkdownImagePreviewer{
		result: markdownImagePreviewResult{Content: "\x1b_Ga=T;RAW\x1b\\", Backend: "kitty", Raw: true, Rows: 4, Columns: 20},
	}
	model := NewModel(service, snapshot, ModelOptions{
		Config:         config.Config{GlamourStyle: "dark", CacheDir: t.TempDir(), MarkdownImagePreview: "auto"},
		ImagePreviewer: previewer,
	})
	model.width = 90
	model.height = 18

	model, cmd := updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected detail load command")
	}
	model = runOptionalCmd(t, model, cmd)

	lines := model.detailContentLines(model.detailContentWidth())
	rawIndex := -1
	for idx, line := range lines {
		if _, ok := detailRawLine(line); ok {
			rawIndex = idx
			break
		}
	}
	if rawIndex < 1 {
		t.Fatalf("raw image should have top spacing before it, got raw index %d in %#v", rawIndex, lines)
	}
	if strings.TrimSpace(ansi.Strip(lines[rawIndex-1])) != "" {
		t.Fatalf("line before raw image should be blank, got %q", ansi.Strip(lines[rawIndex-1]))
	}
	bottomSpacingIndex := rawIndex + previewer.result.Rows
	if bottomSpacingIndex >= len(lines) || strings.TrimSpace(ansi.Strip(lines[bottomSpacingIndex])) != "" {
		t.Fatalf("raw image should reserve a bottom spacing row at %d in %#v", bottomSpacingIndex, lines)
	}
	markerIndex := -1
	for idx, line := range lines {
		if strings.Contains(ansi.Strip(line), "After image marker") {
			markerIndex = idx
			break
		}
	}
	if markerIndex <= bottomSpacingIndex {
		t.Fatalf("content after image should render after bottom spacing, marker=%d bottom=%d", markerIndex, bottomSpacingIndex)
	}

	draws := model.visibleDetailRawImageDraws()
	if len(draws) != 1 {
		t.Fatalf("visible raw draws = %#v, want one", draws)
	}
	contentWidth := model.detailContentWidth()
	indent := max(0, (max(40, model.width)-contentWidth)/2)
	expectedCol := indent + 1 + (contentWidth-previewer.result.Columns)/2
	if draws[0].col != expectedCol {
		t.Fatalf("raw image draw column = %d, want centered column %d", draws[0].col, expectedCol)
	}
}

func TestModelDetailScrollDelaysRawImageRedraw(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Entries[0].Item = domain.FeedItem{
		ID:    "gh-raw-image-scroll",
		Title: "owner/repo",
		URL:   "https://github.com/owner/repo",
		Refs:  domain.Refs{Repo: "owner/repo"},
	}
	snapshot.Entries[0].Sources = []domain.ItemSource{{Source: domain.SourceGitHub}}
	service := &fakeService{
		snapshot: snapshot,
		detail: domain.ItemDetail{
			ItemID: "gh-raw-image-scroll",
			Sections: []domain.DetailSection{{
				Title:  "GitHub README",
				Source: domain.SourceGitHub,
				URL:    "https://github.com/owner/repo/blob/main/README.md",
				Body: strings.Join([]string{
					"![Inline protocol image](assets/preview.png)",
					"",
					longDetailBody(),
				}, "\n"),
			}},
		},
	}
	previewer := &fakeMarkdownImagePreviewer{
		result: markdownImagePreviewResult{Content: "\x1b_Ga=T;RAW\x1b\\", Backend: "kitty", Raw: true, Rows: 6},
	}
	model := NewModel(service, snapshot, ModelOptions{
		Config:         config.Config{GlamourStyle: "dark", CacheDir: t.TempDir(), MarkdownImagePreview: "auto"},
		ImagePreviewer: previewer,
	})
	model.width = 90
	model.height = 24

	model, cmd := updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected detail load command")
	}
	updated, next := model.Update(commandMsg(t, cmd))
	model = updated.(Model)
	if next == nil {
		t.Fatal("expected image preview command")
	}
	updated, drawCmd := model.Update(commandMsg(t, next))
	model = updated.(Model)
	if drawCmd == nil {
		t.Fatal("expected initial raw image draw command")
	}

	model, scrollCmd := updateModelWithKey(t, model, "j")
	if scrollCmd == nil {
		t.Fatal("expected delayed raw image redraw after scrolling")
	}
	if msg := scrollCmd(); isRawMsg(msg) {
		t.Fatalf("detail scroll returned immediate raw image draw %T; want delayed redraw", msg)
	}
	stale := detailRawImageRedrawMsg{
		drawID:       model.detailRawImageDrawID,
		itemID:       model.detailEntryID,
		detailOffset: model.detailOffset,
		imageVersion: model.detailImageVersion,
	}

	model, scrollCmd = updateModelWithKey(t, model, "j")
	if scrollCmd == nil {
		t.Fatal("expected delayed raw image redraw after second scroll")
	}
	updated, staleDrawCmd := model.Update(stale)
	model = updated.(Model)
	if staleDrawCmd != nil {
		t.Fatal("stale raw image redraw should be ignored after a newer scroll")
	}
	latest := detailRawImageRedrawMsg{
		drawID:       model.detailRawImageDrawID,
		itemID:       model.detailEntryID,
		detailOffset: model.detailOffset,
		imageVersion: model.detailImageVersion,
	}
	updated, latestDrawCmd := model.Update(latest)
	model = updated.(Model)
	if latestDrawCmd == nil {
		t.Fatal("expected latest delayed raw image redraw to draw")
	}
	if msg := latestDrawCmd(); !isRawMsg(msg) {
		t.Fatalf("latest delayed redraw returned %T, want tea.RawMsg", msg)
	}
}

func TestModelDetailScrollClearsRawImageWhenItLeavesViewport(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Entries[0].Item = domain.FeedItem{
		ID:    "gh-raw-image-scroll-clear",
		Title: "owner/repo",
		URL:   "https://github.com/owner/repo",
		Refs:  domain.Refs{Repo: "owner/repo"},
	}
	snapshot.Entries[0].Sources = []domain.ItemSource{{Source: domain.SourceGitHub}}
	service := &fakeService{
		snapshot: snapshot,
		detail: domain.ItemDetail{
			ItemID: "gh-raw-image-scroll-clear",
			Sections: []domain.DetailSection{{
				Title:  "GitHub README",
				Source: domain.SourceGitHub,
				URL:    "https://github.com/owner/repo/blob/main/README.md",
				Body: strings.Join([]string{
					"![Inline protocol image](assets/preview.png)",
					"",
					longDetailBody(),
				}, "\n"),
			}},
		},
	}
	previewer := &fakeMarkdownImagePreviewer{
		result: markdownImagePreviewResult{Content: "\x1b_Ga=T;RAW\x1b\\", Backend: "kitty", Raw: true, Rows: 6},
	}
	model := NewModel(service, snapshot, ModelOptions{
		Config:         config.Config{GlamourStyle: "dark", CacheDir: t.TempDir(), MarkdownImagePreview: "auto"},
		ImagePreviewer: previewer,
	})
	model.width = 90
	model.height = 24

	model, cmd := updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected detail load command")
	}
	updated, next := model.Update(commandMsg(t, cmd))
	model = updated.(Model)
	if next == nil {
		t.Fatal("expected image preview command")
	}
	updated, drawCmd := model.Update(commandMsg(t, next))
	model = updated.(Model)
	if drawCmd == nil {
		t.Fatal("expected initial raw image draw command")
	}

	rawIndex := -1
	for idx, line := range model.detailContentLines(model.detailContentWidth()) {
		if _, ok := detailRawLine(line); ok {
			rawIndex = idx
			break
		}
	}
	if rawIndex < 1 {
		t.Fatalf("expected raw image line after top spacing, got index %d", rawIndex)
	}
	model.height = 4
	model.detailOffset = rawIndex - 1

	model, cmd = updateModelWithKey(t, model, "j")
	if cmd == nil {
		t.Fatal("expected raw image scroll command while image is visible")
	}
	model, cmd = updateModelWithKey(t, model, "j")
	if cmd == nil {
		t.Fatal("expected raw image clear command when image leaves the viewport")
	}
	msg := commandMsg(t, cmd)
	rawMsg, ok := msg.(tea.RawMsg)
	if !ok {
		t.Fatalf("scroll command returned %T, want immediate raw clear", msg)
	}
	raw, ok := rawMsg.Msg.(string)
	if !ok {
		t.Fatalf("raw message payload = %T, want string", rawMsg.Msg)
	}
	if !strings.Contains(raw, "a=d") {
		t.Fatalf("raw clear command should delete terminal image placements, got %q", raw)
	}
}

func TestModelGitHubDetailReservesRawImageRows(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Entries[0].Item = domain.FeedItem{
		ID:    "gh-raw-image-rows",
		Title: "owner/repo",
		URL:   "https://github.com/owner/repo",
		Refs:  domain.Refs{Repo: "owner/repo"},
	}
	snapshot.Entries[0].Sources = []domain.ItemSource{{Source: domain.SourceGitHub}}
	service := &fakeService{
		snapshot: snapshot,
		detail: domain.ItemDetail{
			ItemID: "gh-raw-image-rows",
			Sections: []domain.DetailSection{{
				Title:  "GitHub README",
				Source: domain.SourceGitHub,
				URL:    "https://github.com/owner/repo/blob/main/README.md",
				Body: strings.Join([]string{
					"![Inline protocol image](assets/preview.png)",
					"",
					"After image marker",
				}, "\n"),
			}},
		},
	}
	previewer := &fakeMarkdownImagePreviewer{
		result: markdownImagePreviewResult{Content: "\x1b_Ga=T;RAW\x1b\\", Backend: "kitty", Raw: true, Rows: 6},
	}
	model := NewModel(service, snapshot, ModelOptions{
		Config:         config.Config{GlamourStyle: "dark", CacheDir: t.TempDir(), MarkdownImagePreview: "auto"},
		ImagePreviewer: previewer,
	})
	model.width = 90
	model.height = 24

	model, cmd := updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected detail load command")
	}
	model = runOptionalCmd(t, model, cmd)

	lines := model.detailContentLines(model.detailContentWidth())
	rawIndex := -1
	markerIndex := -1
	for idx, line := range lines {
		if raw, ok := detailRawLine(line); ok && strings.Contains(raw, "RAW") {
			rawIndex = idx
		}
		if strings.Contains(ansi.Strip(line), "After image marker") {
			markerIndex = idx
		}
	}
	if rawIndex < 0 || markerIndex < 0 {
		t.Fatalf("raw image or marker missing: raw=%d marker=%d lines=%#v", rawIndex, markerIndex, lines)
	}
	if markerIndex < rawIndex+6 {
		t.Fatalf("marker rendered at line %d, want after reserved raw image rows from %d\n%#v", markerIndex, rawIndex, lines)
	}
}

func TestModelGitHubDetailFallsBackWhenImagePreviewFails(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Entries[0].Item = domain.FeedItem{
		ID:    "gh-image-fallback",
		Title: "owner/repo",
		URL:   "https://github.com/owner/repo",
		Refs:  domain.Refs{Repo: "owner/repo"},
	}
	snapshot.Entries[0].Sources = []domain.ItemSource{{Source: domain.SourceGitHub}}
	service := &fakeService{
		snapshot: snapshot,
		detail: domain.ItemDetail{
			ItemID: "gh-image-fallback",
			Sections: []domain.DetailSection{{
				Title:  "GitHub README",
				Source: domain.SourceGitHub,
				URL:    "https://github.com/owner/repo/blob/main/README.md",
				Body:   "![Unavailable preview](assets/missing.png)",
			}},
		},
	}
	previewer := &fakeMarkdownImagePreviewer{err: errors.New("no image backend")}
	model := NewModel(service, snapshot, ModelOptions{
		Config:         config.Config{GlamourStyle: "dark", CacheDir: t.TempDir(), MarkdownImagePreview: "auto"},
		ImagePreviewer: previewer,
	})
	model.width = 90
	model.height = 18

	model, cmd := updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected detail load command")
	}
	model = runOptionalCmd(t, model, cmd)

	visible := ansi.Strip(model.render())
	for _, want := range []string{"Image: Unavailable preview", "preview unavailable"} {
		if !strings.Contains(visible, want) {
			t.Fatalf("fallback image placeholder missing %q:\n%s", want, visible)
		}
	}
}

func TestModelRenderKeepsDetailShortcutsAtTerminalBottom(t *testing.T) {
	model := testModel(tuiSnapshot(false), ModelOptions{Config: config.Config{GlamourStyle: "dark"}})
	model.width = 90
	model.height = 16

	model, _ = updateModelWithKey(t, model, "enter")
	rendered := ansi.Strip(model.render())
	lines := strings.Split(rendered, "\n")
	if len(lines) != model.height {
		t.Fatalf("detail render height = %d, want %d:\n%s", len(lines), model.height, rendered)
	}
	footer := lines[len(lines)-1]
	for _, want := range []string{"esc back", "q quit"} {
		if !strings.Contains(footer, want) {
			t.Fatalf("detail footer missing %q on bottom line %q:\n%s", want, footer, rendered)
		}
	}
}

func TestModelDetailCentersContentWithoutTitleOnWideTerminal(t *testing.T) {
	model := testModel(tuiSnapshot(false), ModelOptions{Config: config.Config{GlamourStyle: "dark"}})
	model.width = 120
	model.height = 16

	model, _ = updateModelWithKey(t, model, "enter")
	visible := ansi.Strip(model.render())
	if strings.Contains(visible, "Tildewire detail") {
		t.Fatalf("detail render should not include title:\n%s", visible)
	}
	lines := strings.Split(visible, "\n")
	wantIndent := 10
	titleLine := ""
	for _, line := range lines {
		if strings.Contains(line, "First") {
			titleLine = line
			break
		}
	}
	if titleLine == "" {
		t.Fatalf("detail render missing title:\n%s", visible)
	}
	if got := strings.Index(titleLine, "First"); got < wantIndent {
		t.Fatalf("detail body indent = %d, want at least %d:\n%s", got, wantIndent, visible)
	}
}

func TestModelDetailLoadsEnrichmentAsync(t *testing.T) {
	snapshot := tuiSnapshot(false)
	score := int64(42)
	service := &fakeService{
		snapshot: snapshot,
		detail: domain.ItemDetail{
			ItemID: snapshot.Entries[0].Item.ID,
			Providers: []domain.SourceID{
				domain.SourceHackerNews,
			},
			Comments: []domain.DetailComment{{
				Author: "alice",
				Body:   "Useful detail comment.",
				URL:    "https://news.ycombinator.com/item?id=42",
				Score:  &score,
				Source: domain.SourceHackerNews,
			}},
		},
	}
	model := NewModel(service, snapshot, ModelOptions{Config: config.Config{GlamourStyle: "dark"}})

	model, cmd := updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected detail load command")
	}
	if !model.detailLoading {
		t.Fatal("detail should be loading before command completes")
	}
	rendered := model.render()
	if !strings.Contains(rendered, "Loading detail") {
		t.Fatalf("detail render missing loading state:\n%s", rendered)
	}
	model = runOptionalCmd(t, model, cmd)
	rendered = ansi.Strip(model.render())
	for _, want := range []string{"alice", "Useful detail comment.", "42 points", "Hacker News", "Open comment"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("detail enrichment missing %q:\n%s", want, rendered)
		}
	}
}

func TestModelNonGitHubDetailRendersMarkdownHeadingsWithoutMarkers(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Entries[0].Item = domain.FeedItem{
		ID:          "lobsters-story",
		Title:       "Lobsters story",
		URL:         "https://example.com/story",
		CommentsURL: "https://lobste.rs/s/example",
	}
	snapshot.Entries[0].Sources = []domain.ItemSource{{Source: domain.SourceLobsters}}
	score := int64(132)
	service := &fakeService{
		snapshot: snapshot,
		detail: domain.ItemDetail{
			ItemID:    "lobsters-story",
			Providers: []domain.SourceID{domain.SourceLobsters},
			Comments: []domain.DetailComment{{
				Author: "Internet_Janitor",
				Body:   "Sounds **good** to me.",
				URL:    "https://lobste.rs/c/1ecn7s",
				Score:  &score,
				Source: domain.SourceLobsters,
			}},
		},
	}
	model := NewModel(service, snapshot, ModelOptions{Config: config.Config{GlamourStyle: "dark"}})
	model.width = 90
	model.height = 24

	model, cmd := updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected detail load command")
	}
	model = runOptionalCmd(t, model, cmd)

	visible := ansi.Strip(strings.Join(model.detailContentLines(model.detailContentWidth()), "\n"))
	for _, want := range []string{"Top Comments", "Internet_Janitor", "Sounds good to me.", "132 points", "Lobsters"} {
		if !strings.Contains(visible, want) {
			t.Fatalf("source detail missing %q:\n%s", want, visible)
		}
	}
	for _, marker := range []string{"## Top Comments", "### Internet_Janitor"} {
		if strings.Contains(visible, marker) {
			t.Fatalf("source detail should render headings without markdown markers %q:\n%s", marker, visible)
		}
	}
}

func TestModelDetailRendersTagsAndProviders(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Entries[0].Item.Tags = []string{"go", "tui"}
	service := &fakeService{
		snapshot: snapshot,
		detail: domain.ItemDetail{
			ItemID:    snapshot.Entries[0].Item.ID,
			Providers: []domain.SourceID{domain.SourceHackerNews, domain.SourceGitHub},
		},
	}
	model := NewModel(service, snapshot, ModelOptions{Config: config.Config{GlamourStyle: "dark"}})

	model, cmd := updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected detail load command")
	}
	model = runOptionalCmd(t, model, cmd)

	rendered := ansi.Strip(model.render())
	for _, want := range []string{"Tags", "go, tui", "Detail providers", "Hacker News", "GitHub"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("detail metadata missing %q:\n%s", want, rendered)
		}
	}
}

func TestModelGitHubDetailCleansReadmeNoise(t *testing.T) {
	snapshot := tuiSnapshot(false)
	stars := int64(6492)
	forks := int64(526)
	starsToday := int64(1696)
	summary := "OpenHuman is your Personal AI super intelligence. Private, Simple and extremely powerful."
	snapshot.Entries[0].Item = domain.FeedItem{
		ID:       "gh-openhuman",
		Title:    "tinyhumansai/openhuman",
		Summary:  summary,
		URL:      "https://github.com/tinyhumansai/openhuman",
		Language: "Rust",
		Metrics:  domain.Metrics{Stars: &stars, Forks: &forks, StarsToday: &starsToday},
		Refs:     domain.Refs{Repo: "tinyhumansai/openhuman"},
	}
	snapshot.Entries[0].Sources = []domain.ItemSource{{
		Source:  domain.SourceGitHub,
		Metrics: domain.Metrics{Stars: &stars, Forks: &forks, StarsToday: &starsToday},
	}}
	longDocsURL := "https://tinyhumans.gitbook.io/openhuman/features/integrations?utm_source=github&utm_medium=readme"
	service := &fakeService{
		snapshot: snapshot,
		detail: domain.ItemDetail{
			ItemID:    "gh-openhuman",
			Providers: []domain.SourceID{domain.SourceGitHub},
			Sections: []domain.DetailSection{{
				Title:  "GitHub README",
				Source: domain.SourceGitHub,
				URL:    "https://github.com/tinyhumansai/openhuman/blob/main/README.md",
				Body: strings.Join([]string{
					"![build](https://img.shields.io/badge/build-passing-green)",
					"<img alt=\"build\" src=\"https://img.shields.io/badge/build-passing-green\">",
					"<picture><source srcset=\"https://example.com/banner-dark.png\"></picture>",
					summary,
					"[Discord](https://discord.gg/openhuman) | [Docs](https://tinyhumans.gitbook.io/openhuman/) | [X/Twitter](https://x.com/openhuman)",
					"",
					"## What is OpenHuman?",
					"OpenHuman is an open-source agentic assistant. Read the [docs](" + longDocsURL + ") for setup.",
					"- Simple, UI-first & Human: clean desktop experience.",
					"- 118+ third-party integrations " + longDocsURL + " with auto-fetch.",
				}, "\n"),
			}},
		},
	}
	model := NewModel(service, snapshot, ModelOptions{Config: config.Config{GlamourStyle: "dark"}})

	model, cmd := updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected detail load command")
	}
	model = runOptionalCmd(t, model, cmd)

	visible := ansi.Strip(model.render())
	for _, want := range []string{"tinyhumansai/openhuman", "Rust", "6492 stars", "526 forks", "1696 stars today", "README", "What is OpenHuman?", "docs", "Simple, UI-first & Human"} {
		if !strings.Contains(visible, want) {
			t.Fatalf("github detail missing %q:\n%s", want, visible)
		}
	}
	for _, noise := range []string{"img.shields.io", "<picture", "discord.gg", longDocsURL} {
		if strings.Contains(visible, noise) {
			t.Fatalf("github detail should hide noise %q:\n%s", noise, visible)
		}
	}
	for _, marker := range []string{"## README", "## What is OpenHuman?"} {
		if strings.Contains(visible, marker) {
			t.Fatalf("github detail should render headings without markdown markers %q:\n%s", marker, visible)
		}
	}
	if got := strings.Count(visible, summary); got != 1 {
		t.Fatalf("summary rendered %d times, want once:\n%s", got, visible)
	}
}

func TestModelGitHubDetailCapsMarkdownReadWidth(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Entries[0].Item = domain.FeedItem{
		ID:      "gh-wide",
		Title:   "owner/repo",
		URL:     "https://github.com/owner/repo",
		Summary: "short repo summary",
		Refs:    domain.Refs{Repo: "owner/repo"},
	}
	snapshot.Entries[0].Sources = []domain.ItemSource{{Source: domain.SourceGitHub}}
	service := &fakeService{
		snapshot: snapshot,
		detail: domain.ItemDetail{
			ItemID:    "gh-wide",
			Providers: []domain.SourceID{domain.SourceGitHub},
			Sections: []domain.DetailSection{{
				Title:  "GitHub README",
				Source: domain.SourceGitHub,
				URL:    "https://github.com/owner/repo/blob/main/README.md",
				Body:   "## Overview\n\n" + strings.Repeat("readability ", 40),
			}},
		},
	}
	model := NewModel(service, snapshot, ModelOptions{Config: config.Config{GlamourStyle: "dark"}})
	model.width = 160
	model.height = 30

	model, cmd := updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected detail load command")
	}
	model = runOptionalCmd(t, model, cmd)

	for _, line := range model.detailContentLines(max(40, model.width-4)) {
		if width := ansi.StringWidth(line); width > 100 {
			t.Fatalf("github detail line width = %d, want <= 100:\n%s", width, ansi.Strip(line))
		}
	}
}

func TestModelGitHubDetailKeepsSourceComments(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Entries[0].Item = domain.FeedItem{
		ID:    "cross-source",
		Title: "owner/repo",
		URL:   "https://github.com/owner/repo",
		Refs:  domain.Refs{Repo: "owner/repo"},
	}
	snapshot.Entries[0].Sources = []domain.ItemSource{{Source: domain.SourceGitHub}}
	score := int64(12)
	service := &fakeService{
		snapshot: snapshot,
		detail: domain.ItemDetail{
			ItemID:    "cross-source",
			Providers: []domain.SourceID{domain.SourceGitHub, domain.SourceHackerNews},
			Sections: []domain.DetailSection{{
				Title:  "GitHub README",
				Source: domain.SourceGitHub,
				URL:    "https://github.com/owner/repo/blob/main/README.md",
				Body:   "## Overview\n\nRepo readme.",
			}},
			Comments: []domain.DetailComment{{
				Author: "alice",
				Body:   "Useful linked discussion.",
				URL:    "https://news.ycombinator.com/item?id=42",
				Score:  &score,
				Source: domain.SourceHackerNews,
			}},
		},
	}
	model := NewModel(service, snapshot, ModelOptions{Config: config.Config{GlamourStyle: "dark"}})

	model, cmd := updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected detail load command")
	}
	model = runOptionalCmd(t, model, cmd)

	visible := ansi.Strip(model.render())
	for _, want := range []string{"Repo readme.", "Top Comments", "alice", "Useful linked discussion.", "12 points"} {
		if !strings.Contains(visible, want) {
			t.Fatalf("github detail missing comment content %q:\n%s", want, visible)
		}
	}
	for _, marker := range []string{"## Top Comments", "### alice"} {
		if strings.Contains(visible, marker) {
			t.Fatalf("github detail should render comment headings without markdown markers %q:\n%s", marker, visible)
		}
	}
}

func TestModelDetailDoesNotRenderEmptyProviderLine(t *testing.T) {
	snapshot := tuiSnapshot(false)
	model := testModel(snapshot, ModelOptions{Config: config.Config{GlamourStyle: "dark"}})

	model, _ = updateModelWithKey(t, model, "enter")
	rendered := ansi.Strip(model.render())
	if strings.Contains(rendered, "Detail providers") {
		t.Fatalf("detail enrichment missing:\n%s", rendered)
	}
}

func TestModelDetailScrollsWithoutChangingSelection(t *testing.T) {
	snapshot := tuiSnapshot(false)
	service := &fakeService{
		snapshot: snapshot,
		detail: domain.ItemDetail{
			ItemID: snapshot.Entries[0].Item.ID,
			Sections: []domain.DetailSection{{
				Title: "README",
				Body:  longDetailBody(),
			}},
		},
	}
	model := NewModel(service, snapshot, ModelOptions{Config: config.Config{GlamourStyle: "dark"}})
	model.width = 90
	model.height = 12

	model, cmd := updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected detail load command")
	}
	model = runOptionalCmd(t, model, cmd)
	before := ansi.Strip(model.render())
	if strings.Contains(before, "final-marker") {
		t.Fatalf("detail should hide overflow before scrolling:\n%s", before)
	}

	for range 6 {
		model, cmd = updateModelWithKey(t, model, "pgdown")
		if cmd != nil {
			t.Fatal("detail scrolling should not run commands")
		}
	}

	after := ansi.Strip(model.render())
	if model.cursor != 0 {
		t.Fatalf("detail scroll changed feed cursor = %d, want 0", model.cursor)
	}
	if !strings.Contains(after, "final-marker") {
		t.Fatalf("detail scroll should reveal overflow content:\n%s", after)
	}
}

func TestModelDetailMouseWheelScrollsDetail(t *testing.T) {
	snapshot := tuiSnapshot(false)
	service := &fakeService{
		snapshot: snapshot,
		detail: domain.ItemDetail{
			ItemID: snapshot.Entries[0].Item.ID,
			Sections: []domain.DetailSection{{
				Title: "README",
				Body:  longDetailBody(),
			}},
		},
	}
	model := NewModel(service, snapshot, ModelOptions{Config: config.Config{GlamourStyle: "dark"}})
	model.width = 90
	model.height = 12

	model, cmd := updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected detail load command")
	}
	model = runOptionalCmd(t, model, cmd)

	updated, cmd := model.Update(mouseWheelDown(40, 5))
	model = updated.(Model)
	if cmd != nil {
		t.Fatal("detail mouse wheel should not run commands")
	}
	if model.detailOffset != 1 {
		t.Fatalf("detail offset after wheel down = %d, want 1", model.detailOffset)
	}
	if model.cursor != 0 || model.feedOffset != 0 || model.previewOffset != 0 {
		t.Fatalf("detail wheel changed main view state: cursor=%d feed=%d preview=%d", model.cursor, model.feedOffset, model.previewOffset)
	}

	updated, _ = model.Update(mouseWheelUp(40, 5))
	model = updated.(Model)
	if model.detailOffset != 0 {
		t.Fatalf("detail offset after wheel up = %d, want 0", model.detailOffset)
	}
}

func TestModelDetailMouseWheelReusesRenderedLines(t *testing.T) {
	snapshot := tuiSnapshot(false)
	service := &fakeService{
		snapshot: snapshot,
		detail: domain.ItemDetail{
			ItemID: snapshot.Entries[0].Item.ID,
			Sections: []domain.DetailSection{{
				Title: "README",
				Body:  longDetailBody(),
			}},
		},
	}
	model := NewModel(service, snapshot, ModelOptions{Config: config.Config{GlamourStyle: "dark"}})
	model.width = 90
	model.height = 12

	model, cmd := updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected detail load command")
	}
	model = runOptionalCmd(t, model, cmd)

	width := model.detailContentWidth()
	before := model.detailContentLines(width)
	if len(before) == 0 {
		t.Fatal("expected cached detail lines")
	}

	updated, cmd := model.Update(mouseWheelDown(40, 5))
	model = updated.(Model)
	if cmd != nil {
		t.Fatal("detail mouse wheel should not run commands")
	}
	after := model.detailContentLines(width)
	if len(after) == 0 {
		t.Fatal("expected detail lines after scroll")
	}
	if &after[0] != &before[0] {
		t.Fatal("detail mouse wheel recomputed rendered lines instead of reusing the cached detail content")
	}
}

func TestModelDetailContentAddsBottomSpacing(t *testing.T) {
	snapshot := tuiSnapshot(false)
	model := testModel(snapshot, ModelOptions{Config: config.Config{GlamourStyle: "dark"}})

	model, _ = updateModelWithKey(t, model, "enter")
	assertDetailBottomSpacing(t, model.detailContentLines(model.detailContentWidth()))
}

func TestModelGitHubDetailContentAddsBottomSpacing(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Entries[0].Item = domain.FeedItem{
		ID:    "gh-spaced",
		Title: "owner/repo",
		URL:   "https://github.com/owner/repo",
		Refs:  domain.Refs{Repo: "owner/repo"},
	}
	snapshot.Entries[0].Sources = []domain.ItemSource{{Source: domain.SourceGitHub}}
	service := &fakeService{
		snapshot: snapshot,
		detail: domain.ItemDetail{
			ItemID: "gh-spaced",
			Sections: []domain.DetailSection{{
				Title:  "GitHub README",
				Source: domain.SourceGitHub,
				URL:    "https://github.com/owner/repo/blob/main/README.md",
				Body:   "## Overview\n\nRepo readme.",
			}},
		},
	}
	model := NewModel(service, snapshot, ModelOptions{Config: config.Config{GlamourStyle: "dark"}})

	model, cmd := updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected detail load command")
	}
	model = runOptionalCmd(t, model, cmd)

	assertDetailBottomSpacing(t, model.detailContentLines(model.detailContentWidth()))
}

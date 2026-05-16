package tui

import (
	"context"
	"errors"
	"image/color"
	"slices"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/zhangxueai/tildewire/internal/app"
	"github.com/zhangxueai/tildewire/internal/config"
	"github.com/zhangxueai/tildewire/internal/domain"
)

func TestModelMovesSelection(t *testing.T) {
	model := NewModel(&fakeService{snapshot: tuiSnapshot(false)}, tuiSnapshot(false))
	updated, _ := model.Update(keyPress("j"))
	model = updated.(Model)
	if model.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", model.cursor)
	}
	updated, _ = model.Update(keyPress("k"))
	model = updated.(Model)
	if model.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", model.cursor)
	}
}

func TestModelSaveCommandReloadsSnapshot(t *testing.T) {
	service := &fakeService{snapshot: tuiSnapshot(true)}
	model := NewModel(service, tuiSnapshot(false))
	updated, cmd := model.Update(keyPress("s"))
	if cmd == nil {
		t.Fatal("expected save command")
	}
	msg := cmd()
	updated, _ = updated.Update(msg)
	model = updated.(Model)
	if !service.savedCalled {
		t.Fatal("service SetSaved was not called")
	}
	if !model.entries[0].State.Saved {
		t.Fatalf("saved state not reflected: %+v", model.entries[0].State)
	}
}

func TestModelRefreshErrorKeepsCachedItems(t *testing.T) {
	service := &fakeService{snapshot: tuiSnapshot(false), refreshErr: errors.New("network down")}
	model := NewModel(service, tuiSnapshot(false))
	model.refreshing = false
	updated, cmd := model.Update(keyPress("r"))
	if cmd == nil {
		t.Fatal("expected refresh command")
	}
	msg := firstBatchCommandMsg(t, cmd)
	updated, _ = updated.Update(msg)
	model = updated.(Model)
	if model.lastError == "" {
		t.Fatal("expected lastError after refresh failure")
	}
	if len(model.entries) != 2 {
		t.Fatalf("cached entries were lost: %d", len(model.entries))
	}
	if !service.lastForce {
		t.Fatal("manual refresh should force network refresh")
	}
}

func TestModelRefreshStartsProgressPolling(t *testing.T) {
	service := &fakeService{snapshot: tuiSnapshot(false)}
	model := NewModel(service, tuiSnapshot(false))
	model.refreshing = false

	updated, cmd := model.Update(keyPress("r"))
	if cmd == nil {
		t.Fatal("expected refresh command")
	}
	model = updated.(Model)
	if !model.refreshing {
		t.Fatal("model should be refreshing before command completes")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("refresh should start final refresh and progress polling, got %T", msg)
	}
	if len(batch) != 3 {
		t.Fatalf("refresh batch command count = %d, want 3", len(batch))
	}
}

func TestModelRefreshKeyDoesNotStartDuplicateWhileRefreshing(t *testing.T) {
	service := &fakeService{snapshot: tuiSnapshot(false)}
	model := NewModel(service, tuiSnapshot(false))
	model.refreshing = true
	refreshID := model.refreshID

	updated, cmd := model.Update(keyPress("r"))
	model = updated.(Model)
	if cmd != nil {
		t.Fatal("refresh key should not start another refresh while one is running")
	}
	if model.refreshID != refreshID {
		t.Fatalf("refresh id = %d, want unchanged %d", model.refreshID, refreshID)
	}
	if model.message != "refresh already running" {
		t.Fatalf("message = %q, want refresh already running", model.message)
	}
}

func TestModelRefreshProgressUpdatesUntilFinalSnapshot(t *testing.T) {
	initial := tuiSnapshot(false)
	initial.Entries = nil
	partial := tuiSnapshot(false)
	partial.Entries = partial.Entries[:1]
	final := tuiSnapshot(false)
	final.Entries = append(final.Entries, feedEntry("id-3", "Third", domain.SourceGitHub, 3, final.LoadedAt))
	late := partial

	model := NewModel(&fakeService{snapshot: final}, initial)
	model.refreshing = true
	updated, cmd := model.Update(refreshProgressMsg{refreshID: model.refreshID, snapshot: partial})
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("refresh progress should schedule another progress poll")
	}
	if len(model.entries) != 1 {
		t.Fatalf("progress entries = %d, want 1", len(model.entries))
	}

	updated, cmd = model.Update(snapshotMsg{refreshID: model.refreshID, snapshot: final, message: "refresh complete"})
	model = updated.(Model)
	if cmd != nil {
		t.Fatal("final snapshot should not schedule more work")
	}
	if model.refreshing {
		t.Fatal("final snapshot should stop refreshing")
	}
	if len(model.entries) != 3 {
		t.Fatalf("final entries = %d, want 3", len(model.entries))
	}

	updated, _ = model.Update(refreshProgressMsg{refreshID: model.refreshID, snapshot: late})
	model = updated.(Model)
	if len(model.entries) != 3 {
		t.Fatalf("late progress should not replace final entries, got %d", len(model.entries))
	}
}

func TestModelInitRefreshDoesNotForce(t *testing.T) {
	service := &fakeService{snapshot: tuiSnapshot(false)}
	model := NewModel(service, tuiSnapshot(false))
	cmd := model.Init()
	if cmd == nil {
		t.Fatal("expected init refresh command")
	}
	_ = firstBatchCommandMsg(t, cmd)
	if service.lastForce {
		t.Fatal("init refresh should respect cache TTL")
	}
	if service.lastRefreshMode != app.RefreshModeStartup {
		t.Fatalf("init refresh mode = %s, want startup", service.lastRefreshMode)
	}
}

func TestModelSourceKeysLoadViews(t *testing.T) {
	service := &fakeService{snapshot: tuiSnapshot(false)}
	model := NewModel(service, tuiSnapshot(false))
	for _, tt := range []struct {
		key  string
		view domain.SourceID
	}{
		{key: "1", view: domain.SourceGitHub},
		{key: "2", view: domain.SourceHackerNews},
		{key: "3", view: domain.SourceHuggingFace},
		{key: "4", view: domain.SourceLobsters},
		{key: "5", view: domain.SourceProductHunt},
	} {
		updated, cmd := model.Update(keyPress(tt.key))
		if cmd == nil {
			t.Fatalf("expected load command for key %s", tt.key)
		}
		msg := cmd()
		updated, _ = updated.Update(msg)
		model = updated.(Model)
		if service.lastLoadView != tt.view {
			t.Fatalf("key %s loaded %s, want %s", tt.key, service.lastLoadView, tt.view)
		}
		if model.view != tt.view {
			t.Fatalf("model view = %s, want %s", model.view, tt.view)
		}
	}
}

func TestModelDisabledSourceKeyDoesNotLoadView(t *testing.T) {
	service := &fakeService{snapshot: tuiSnapshot(false)}
	model := NewModel(service, tuiSnapshot(false), ModelOptions{
		Config: configWithEnabledSources(t, []string{"github", "hackernews", "huggingface", "lobsters"}),
	})

	updated, cmd := model.Update(keyPress("5"))
	model = updated.(Model)
	if cmd != nil {
		t.Fatal("disabled Product Hunt key should not load")
	}
	if model.view != domain.SourceAll {
		t.Fatalf("view = %s, want all", model.view)
	}
	if service.lastLoadView != "" {
		t.Fatalf("loaded disabled source %s", service.lastLoadView)
	}
	if !strings.Contains(model.message, "disabled") {
		t.Fatalf("message = %q, want disabled source guidance", model.message)
	}
}

func TestModelSourceKeysSetDefaultSourceView(t *testing.T) {
	service := &fakeService{snapshot: tuiSnapshot(false)}
	model := NewModel(service, tuiSnapshot(false))

	model = runKeyCommand(t, model, "1")
	if service.lastFilter.SourceView != "trending:daily" {
		t.Fatalf("github source view = %q, want trending:daily", service.lastFilter.SourceView)
	}
	model = runKeyCommand(t, model, "2")
	if service.lastFilter.SourceView != "top" {
		t.Fatalf("hn source view = %q, want top", service.lastFilter.SourceView)
	}
	model = runKeyCommand(t, model, "3")
	if service.lastFilter.SourceView != "daily" {
		t.Fatalf("hf source view = %q, want daily", service.lastFilter.SourceView)
	}
	model = runKeyCommand(t, model, "4")
	if service.lastFilter.SourceView != "hottest" {
		t.Fatalf("lobsters source view = %q, want hottest", service.lastFilter.SourceView)
	}
	model = runKeyCommand(t, model, "5")
	if service.lastFilter.SourceView != "today" {
		t.Fatalf("product hunt source view = %q, want today", service.lastFilter.SourceView)
	}
	model = runKeyCommand(t, model, "a")
	if service.lastFilter.SourceView != "" {
		t.Fatalf("all source view = %q, want cleared", service.lastFilter.SourceView)
	}
}

func TestModelSourceTabsRememberIndependentFeedSelection(t *testing.T) {
	service := &fakeService{snapshot: tuiSnapshot(false)}
	model := NewModel(service, tuiSnapshot(false))

	model, _ = updateModelWithKey(t, model, "down")
	if model.cursor != 1 {
		t.Fatalf("all cursor = %d, want second row", model.cursor)
	}

	model = runKeyCommand(t, model, "1")
	if model.cursor != 0 {
		t.Fatalf("new github tab cursor = %d, want first row", model.cursor)
	}

	model = runKeyCommand(t, model, "a")
	if model.cursor != 1 {
		t.Fatalf("restored all cursor = %d, want second row", model.cursor)
	}

	model = runKeyCommand(t, model, "1")
	if model.cursor != 0 {
		t.Fatalf("restored github cursor = %d, want first row", model.cursor)
	}
}

func TestModelSourceKeyLoadsThenRefreshesVisibleScope(t *testing.T) {
	service := &fakeService{snapshot: tuiSnapshot(false)}
	model := NewModel(service, tuiSnapshot(false))

	model, cmd := updateModelWithKey(t, model, "1")
	if cmd == nil {
		t.Fatal("expected github key to load feed")
	}
	updated, next := model.Update(cmd())
	model = updated.(Model)
	if next == nil {
		t.Fatal("expected visible refresh after cached load")
	}
	updated, _ = model.Update(firstBatchCommandMsg(t, next))
	model = updated.(Model)

	if service.lastLoadView != domain.SourceGitHub {
		t.Fatalf("loaded view = %s, want github", service.lastLoadView)
	}
	if service.lastRefreshMode != app.RefreshModeVisible {
		t.Fatalf("refresh mode = %s, want visible", service.lastRefreshMode)
	}
	if service.lastForce {
		t.Fatal("source switch refresh should not force network")
	}
}

func TestModelScopeKeyCyclesCurrentSourceViews(t *testing.T) {
	service := &fakeService{snapshot: tuiSnapshot(false)}
	model := NewModel(service, tuiSnapshot(false))

	model = runKeyCommand(t, model, "2")
	model = runKeyCommand(t, model, "v")
	if service.lastFilter.SourceView != "best" {
		t.Fatalf("hn cycled source view = %q, want best", service.lastFilter.SourceView)
	}

	model = runKeyCommand(t, model, "1")
	model = runKeyCommand(t, model, "v")
	if service.lastFilter.SourceView != "trending:weekly" {
		t.Fatalf("github cycled source view = %q, want trending:weekly", service.lastFilter.SourceView)
	}
}

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

func TestModelGitHubFeedHidesTopScopeControls(t *testing.T) {
	service := &fakeService{snapshot: tuiSnapshot(false)}
	model := NewModel(service, tuiSnapshot(false))

	model = runKeyCommand(t, model, "1")
	rendered := ansi.Strip(model.renderFeed(80, 8))
	for _, blocked := range []string{"Spoken Language:", "Language:", "Date Range:"} {
		if strings.Contains(rendered, blocked) {
			t.Fatalf("github feed rendered removed scope control %q:\n%s", blocked, rendered)
		}
	}
}

func TestModelMouseClickGitHubFeedDoesNotOpenScopePicker(t *testing.T) {
	service := &fakeService{snapshot: tuiSnapshot(false)}
	model := NewModel(service, tuiSnapshot(false))
	model.width = 100
	model.height = 22
	model = runKeyCommand(t, model, "1")
	layout := model.mainLayout()

	updated, cmd := model.Update(mouseClick(layout.feed.contentX+42, layout.feed.contentY+2))
	model = updated.(Model)
	if cmd != nil {
		t.Fatal("clicking the feed should not open or load a GitHub scope picker")
	}
	if strings.Contains(ansi.Strip(model.render()), "GITHUB SCOPE") {
		t.Fatalf("feed click should not open scope picker:\n%s", model.render())
	}
}

func TestModelSearchLoadsFilteredFeed(t *testing.T) {
	service := &fakeService{snapshot: tuiSnapshot(false)}
	model := NewModel(service, tuiSnapshot(false))

	model, _ = updateModelWithKey(t, model, "/")
	model, _ = updateModelWithKey(t, model, "a")
	model, _ = updateModelWithKey(t, model, "g")
	model, cmd := updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected search submit to load filtered feed")
	}
	updated, _ := model.Update(cmd())
	model = updated.(Model)

	if service.lastFilter.Search != "ag" {
		t.Fatalf("search filter = %q, want ag", service.lastFilter.Search)
	}
	if model.filter.Search != "ag" {
		t.Fatalf("model search filter = %q, want ag", model.filter.Search)
	}
	if !strings.Contains(model.render(), "Search: ag") {
		t.Fatalf("render missing active search:\n%s", model.render())
	}
}

func TestModelSearchFiltersFeedAsUserTypes(t *testing.T) {
	service := &fakeService{snapshot: tuiSnapshot(false)}
	model := NewModel(service, tuiSnapshot(false))

	model, _ = updateModelWithKey(t, model, "/")
	model, _ = updateModelWithKey(t, model, "a")
	model, cmd := updateModelWithKey(t, model, "g")
	if cmd == nil {
		t.Fatal("expected search input to load filtered feed")
	}
	updated, _ := model.Update(cmd())
	model = updated.(Model)

	if service.lastFilter.Search != "ag" {
		t.Fatalf("search filter = %q, want ag", service.lastFilter.Search)
	}
	if model.filter.Search != "ag" {
		t.Fatalf("model search filter = %q, want ag", model.filter.Search)
	}
	if model.mode != inputModeSearch {
		t.Fatal("typing search should keep search mode open")
	}
}

func TestModelSearchIgnoresStaleLiveResults(t *testing.T) {
	service := &fakeService{snapshot: tuiSnapshot(false)}
	model := NewModel(service, tuiSnapshot(false))

	model, _ = updateModelWithKey(t, model, "/")
	model, firstCmd := updateModelWithKey(t, model, "a")
	if firstCmd == nil {
		t.Fatal("expected first search input to load feed")
	}
	model, latestCmd := updateModelWithKey(t, model, "g")
	if latestCmd == nil {
		t.Fatal("expected latest search input to load feed")
	}

	updated, _ := model.Update(latestCmd())
	model = updated.(Model)
	if model.filter.Search != "ag" {
		t.Fatalf("latest search filter = %q, want ag", model.filter.Search)
	}

	updated, _ = model.Update(firstCmd())
	model = updated.(Model)
	if model.filter.Search != "ag" {
		t.Fatalf("stale search result reset filter to %q, want ag", model.filter.Search)
	}
}

func TestModelSearchCancelRestoresPreviousFilter(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Filter = app.FeedFilter{Search: "sqlite"}
	service := &fakeService{snapshot: snapshot}
	model := NewModel(service, snapshot)

	model, _ = updateModelWithKey(t, model, "/")
	model, liveCmd := updateModelWithKey(t, model, "g")
	if liveCmd == nil {
		t.Fatal("expected search input to load feed")
	}
	updated, _ := model.Update(liveCmd())
	model = updated.(Model)
	if model.filter.Search != "sqliteg" {
		t.Fatalf("live search filter = %q, want sqliteg", model.filter.Search)
	}

	model, cancelCmd := updateModelWithKey(t, model, "esc")
	if cancelCmd == nil {
		t.Fatal("expected search cancel to reload previous filter")
	}
	updated, _ = model.Update(cancelCmd())
	model = updated.(Model)

	if model.filter.Search != "sqlite" {
		t.Fatalf("cancelled search filter = %q, want sqlite", model.filter.Search)
	}
	if model.mode != inputModeNormal {
		t.Fatal("cancelled search should return to normal mode")
	}
}

func TestModelFilterKeyCyclesUnreadSavedAll(t *testing.T) {
	service := &fakeService{snapshot: tuiSnapshot(false)}
	model := NewModel(service, tuiSnapshot(false))

	model, cmd := updateModelWithKey(t, model, "f")
	if cmd != nil {
		t.Fatal("opening filter overlay should not load feed")
	}
	if !strings.Contains(model.render(), "FILTER") {
		t.Fatalf("render missing filter overlay:\n%s", model.render())
	}
	model, _ = updateModelWithKey(t, model, "j")
	model, _ = updateModelWithKey(t, model, "j")
	model, _ = updateModelWithKey(t, model, "j")
	model, _ = updateModelWithKey(t, model, "l")
	model, cmd = updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected applying filter to load feed")
	}
	updated, _ := model.Update(cmd())
	model = updated.(Model)
	if !service.lastFilter.UnreadOnly || service.lastFilter.SavedOnly {
		t.Fatalf("applied filter = %+v, want unread only", service.lastFilter)
	}
}

func TestModelFilterOverlayAppliesSavedLanguageTagAndHidden(t *testing.T) {
	service := &fakeService{snapshot: tuiSnapshot(false)}
	model := NewModel(service, tuiSnapshot(false))

	model, _ = updateModelWithKey(t, model, "f")
	model, _ = updateModelWithKey(t, model, "j")
	model, _ = updateModelWithKey(t, model, "j")
	model, _ = updateModelWithKey(t, model, "l")
	model, _ = updateModelWithKey(t, model, "j")
	model, _ = updateModelWithKey(t, model, "j")
	model, _ = updateModelWithKey(t, model, "l")
	model, _ = updateModelWithKey(t, model, "j")
	model, _ = updateModelWithKey(t, model, "a")
	model, _ = updateModelWithKey(t, model, "i")
	model, _ = updateModelWithKey(t, model, "j")
	model, _ = updateModelWithKey(t, model, "l")
	model, cmd := updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected applying filter to load feed")
	}
	updated, _ := model.Update(cmd())
	model = updated.(Model)

	if !service.lastFilter.SavedOnly || service.lastFilter.UnreadOnly {
		t.Fatalf("state filter = %+v, want saved only", service.lastFilter)
	}
	if service.lastFilter.Language != "go" || service.lastFilter.Tag != "ai" || !service.lastFilter.IncludeHidden {
		t.Fatalf("filter values = %+v, want language go tag ai include hidden", service.lastFilter)
	}
	if !strings.Contains(model.render(), "saved") || !strings.Contains(model.render(), "go") || !strings.Contains(model.render(), "ai") || !strings.Contains(model.render(), "hidden") {
		t.Fatalf("render missing active filters:\n%s", model.render())
	}
}

func TestModelFilterOverlayAppliesSourceScopeAndIndependentFacets(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Entries[0].Item.Language = "Elixir"
	snapshot.Entries[0].Item.Tags = []string{"distributed", "ai"}
	snapshot.Entries[1].Item.Language = "Svelte"
	snapshot.Entries[1].Item.Tags = []string{"frontend"}
	service := &fakeService{snapshot: snapshot}
	model := NewModel(service, snapshot)

	model, cmd := updateModelWithKey(t, model, "f")
	if cmd != nil {
		t.Fatal("opening filter overlay should not load feed")
	}
	rendered := ansi.Strip(model.render())
	for _, want := range []string{"Source", "Scope", "Saved", "Unread"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("filter overlay missing %q:\n%s", want, rendered)
		}
	}

	for _, key := range []string{
		"l", "l", "l", "l",
		"j", "l",
		"j", "l",
		"j", "l",
		"j", "l", "l", "l", "l", "l",
		"j", "l",
		"j", "l",
	} {
		model, cmd = updateModelWithKey(t, model, key)
		if cmd != nil {
			t.Fatalf("filter key %q should not load data before apply", key)
		}
	}

	model, cmd = updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected applying filter to load selected source")
	}
	model = runOptionalCmd(t, model, cmd)

	if service.lastLoadView != domain.SourceLobsters {
		t.Fatalf("filter source = %s, want lobsters", service.lastLoadView)
	}
	if service.lastFilter.SourceView != "newest" {
		t.Fatalf("filter source view = %q, want newest", service.lastFilter.SourceView)
	}
	if !service.lastFilter.SavedOnly || !service.lastFilter.UnreadOnly {
		t.Fatalf("state filters = %+v, want saved and unread", service.lastFilter)
	}
	if service.lastFilter.Language != "elixir" || service.lastFilter.Tag != "ai" || !service.lastFilter.IncludeHidden {
		t.Fatalf("facet filters = %+v, want language elixir tag ai include hidden", service.lastFilter)
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

func TestModelHiddenSourcesAreOmittedFromSourceFilterPaletteAndHealth(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Statuses = append(snapshot.Statuses, domain.SourceHealth{
		Source: domain.SourceProductHunt,
		Name:   "Product Hunt",
		Status: domain.SourceStatusAuthRequired,
	})
	snapshot.FetchHistory = []domain.FetchEvent{{
		Source:     domain.SourceProductHunt,
		SourceView: "today",
		Status:     domain.SourceStatusAuthRequired,
		StartedAt:  snapshot.LoadedAt,
		FinishedAt: snapshot.LoadedAt,
	}}
	model := NewModel(&fakeService{snapshot: snapshot}, snapshot, ModelOptions{
		Config: configWithEnabledSources(t, []string{"github", "hackernews", "huggingface", "lobsters"}),
	})

	if rendered := ansi.Strip(model.renderSources(24, 8)); strings.Contains(rendered, "Product Hunt") {
		t.Fatalf("sources rendered hidden Product Hunt:\n%s", rendered)
	}
	model.openFilter()
	for range 5 {
		model.cycleFilterDraftField(1)
	}
	if model.filterDraftView == domain.SourceProductHunt {
		t.Fatal("filter source cycle reached hidden Product Hunt")
	}
	model.openPalette()
	if strings.Contains(ansi.Strip(model.renderPalette()), "Product Hunt view") {
		t.Fatalf("palette rendered hidden Product Hunt:\n%s", model.renderPalette())
	}
	model.paletteOpen = false
	model.health = true
	if strings.Contains(ansi.Strip(model.renderHealth()), "Product Hunt") {
		t.Fatalf("health rendered hidden Product Hunt:\n%s", model.renderHealth())
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

func TestModelRenderFeedShowsSearchMatchContext(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Filter = app.FeedFilter{Search: "linux"}
	snapshot.Entries[0].Item.Title = "Asteroid"
	snapshot.Entries[0].Item.Subtitle = "135 votes · 26 comments · #10 today"
	snapshot.Entries[0].Item.Summary = "Asteroid lets teams build computer-use agents for browser, Linux, and Windows workflows in minutes."
	model := NewModel(&fakeService{snapshot: snapshot}, snapshot)

	rendered := ansi.Strip(model.renderFeed(96, 8))
	if !strings.Contains(rendered, "Asteroid") {
		t.Fatalf("feed missing search result title:\n%s", rendered)
	}
	if !strings.Contains(rendered, "match:") || !strings.Contains(rendered, "Linux") {
		t.Fatalf("feed should show search match context for summary-only hits:\n%s", rendered)
	}

	raw := model.renderFeed(96, 8)
	matchStyle := ansiSequenceBefore(raw, "match:")
	linuxStyle := ansiSequenceBefore(raw, "Linux")
	if matchStyle == "" || linuxStyle == "" {
		t.Fatalf("search context should be styled with ANSI sequences:\n%q", raw)
	}
	if matchStyle == linuxStyle {
		t.Fatalf("matched search term should use a distinct highlight style, got %q", linuxStyle)
	}
	if !strings.Contains(linuxStyle, "\x1b[1") {
		t.Fatalf("matched search term should use bold highlight style, got %q", linuxStyle)
	}
}

func TestModelRenderFeedHighlightsSearchTermInTitle(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Filter = app.FeedFilter{Search: "linux"}
	snapshot.Entries[1].Item.Title = "Linux Kernel Startup"
	model := NewModel(&fakeService{snapshot: snapshot}, snapshot)

	raw := model.renderFeed(96, 8)
	rendered := ansi.Strip(raw)
	if !strings.Contains(rendered, "Linux Kernel Startup") {
		t.Fatalf("feed missing title search hit:\n%s", rendered)
	}
	linuxStyle := ansiSequenceBefore(raw, "Linux")
	if !strings.Contains(linuxStyle, "\x1b[1") {
		t.Fatalf("matched title term should use bold highlight style, got %q", linuxStyle)
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

func TestModelLeftRightKeysMoveFocusBetweenMainPanels(t *testing.T) {
	model := NewModel(&fakeService{snapshot: tuiSnapshot(false)}, tuiSnapshot(false))
	if model.activePanel != panelFeed {
		t.Fatalf("initial active panel = %v, want feed", model.activePanel)
	}

	model, cmd := updateModelWithKey(t, model, "right")
	if cmd != nil {
		t.Fatal("focus right should not run a command")
	}
	if model.activePanel != panelPreview {
		t.Fatalf("right focus = %v, want preview", model.activePanel)
	}

	model, _ = updateModelWithKey(t, model, "right")
	if model.activePanel != panelSources {
		t.Fatalf("right should wrap to sources, got %v", model.activePanel)
	}

	model, _ = updateModelWithKey(t, model, "left")
	if model.activePanel != panelPreview {
		t.Fatalf("left should wrap back to preview, got %v", model.activePanel)
	}
}

func TestModelSourcesFocusUpDownSwitchesSources(t *testing.T) {
	service := &fakeService{snapshot: tuiSnapshot(false)}
	model := NewModel(service, tuiSnapshot(false))

	model, cmd := updateModelWithKey(t, model, "left")
	if cmd != nil {
		t.Fatal("focus left should not run a command")
	}
	if model.activePanel != panelSources {
		t.Fatalf("active panel = %v, want sources", model.activePanel)
	}

	model, cmd = updateModelWithKey(t, model, "down")
	if cmd == nil {
		t.Fatal("source down should load the next source")
	}
	updated, _ := model.Update(cmd())
	model = updated.(Model)
	if model.view != domain.SourceGitHub {
		t.Fatalf("source down view = %s, want github", model.view)
	}
	if service.lastLoadView != domain.SourceGitHub {
		t.Fatalf("source down loaded %s, want github", service.lastLoadView)
	}

	model, cmd = updateModelWithKey(t, model, "up")
	if cmd == nil {
		t.Fatal("source up should load the previous source")
	}
	updated, _ = model.Update(cmd())
	model = updated.(Model)
	if model.view != domain.SourceAll {
		t.Fatalf("source up view = %s, want all", model.view)
	}
}

func TestModelSourcesUsePersistedOrder(t *testing.T) {
	cfg := configWithSourceOrder(t, []string{"producthunt", "github", "hackernews"})
	model := NewModel(&fakeService{snapshot: tuiSnapshot(false)}, tuiSnapshot(false), ModelOptions{Config: cfg})

	rendered := ansi.Strip(model.renderSources(24, 8))
	productHuntIndex := strings.Index(rendered, "Product Hunt")
	githubIndex := strings.Index(rendered, "GitHub")
	hackerNewsIndex := strings.Index(rendered, "Hacker News")
	if productHuntIndex < 0 || githubIndex < 0 || hackerNewsIndex < 0 {
		t.Fatalf("render missing configured sources:\n%s", rendered)
	}
	if !(productHuntIndex < githubIndex && githubIndex < hackerNewsIndex) {
		t.Fatalf("sources not rendered in persisted order:\n%s", rendered)
	}
}

func TestModelSourcesFocusedSortsAndPersistsOrder(t *testing.T) {
	service := &fakeService{snapshot: tuiSnapshot(false)}
	var savedConfig config.Config
	model := NewModel(service, tuiSnapshot(false), ModelOptions{
		Config: configWithSourceOrder(t, []string{"github", "hackernews", "huggingface", "lobsters", "producthunt"}),
		SaveConfig: func(cfg config.Config) error {
			savedConfig = cfg
			return nil
		},
	})

	model = runKeyCommand(t, model, "1")
	service.lastLoadView = ""
	service.lastFilter = app.FeedFilter{}
	var cmd tea.Cmd
	model, cmd = updateModelWithKey(t, model, "J")
	if cmd == nil {
		t.Fatal("source sort should persist config")
	}
	model = runOptionalCmd(t, model, cmd)

	if service.lastLoadView != "" {
		t.Fatalf("source sort loaded feed for %s", service.lastLoadView)
	}
	if model.view != domain.SourceGitHub {
		t.Fatalf("view after sort = %s, want %s", model.view, domain.SourceGitHub)
	}
	wantOrder := []string{"hackernews", "github", "huggingface", "lobsters", "producthunt"}
	if got := model.config.SourceOrder; !slices.Equal(got, wantOrder) {
		t.Fatalf("model source order = %#v, want %#v", got, wantOrder)
	}
	if got := savedConfig.SourceOrder; !slices.Equal(got, wantOrder) {
		t.Fatalf("saved source order = %#v, want %#v", got, wantOrder)
	}
}

func TestModelViewEnablesMouseCellMotion(t *testing.T) {
	model := NewModel(&fakeService{snapshot: tuiSnapshot(false)}, tuiSnapshot(false))
	view := model.View()
	if view.MouseMode != tea.MouseModeCellMotion {
		t.Fatalf("mouse mode = %v, want cell motion", view.MouseMode)
	}
}

func TestModelMouseClickSourcesLoadsSelectedSource(t *testing.T) {
	service := &fakeService{snapshot: tuiSnapshot(false)}
	model := NewModel(service, tuiSnapshot(false))
	model.width = 100
	model.height = 16

	updated, cmd := model.Update(mouseClick(3, 6))
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("expected source click to load feed")
	}
	updated, _ = model.Update(cmd())
	model = updated.(Model)

	if model.view != domain.SourceHackerNews {
		t.Fatalf("clicked source view = %s, want %s", model.view, domain.SourceHackerNews)
	}
	if service.lastLoadView != domain.SourceHackerNews {
		t.Fatalf("loaded source view = %s, want %s", service.lastLoadView, domain.SourceHackerNews)
	}
}

func TestModelMouseSourceTabsRememberIndependentFeedSelection(t *testing.T) {
	service := &fakeService{snapshot: tuiSnapshot(false)}
	model := NewModel(service, tuiSnapshot(false))
	model.width = 100
	model.height = 16

	model, _ = updateModelWithKey(t, model, "down")
	layout := model.mainLayout()
	updated, cmd := model.Update(mouseClick(layout.sources.contentX+1, layout.sources.contentY+3))
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("expected source click to load feed")
	}
	model = runOptionalCmd(t, model, cmd)
	if model.view != domain.SourceGitHub {
		t.Fatalf("clicked source view = %s, want %s", model.view, domain.SourceGitHub)
	}
	if model.cursor != 0 {
		t.Fatalf("new github tab cursor = %d, want first row", model.cursor)
	}

	layout = model.mainLayout()
	updated, cmd = model.Update(mouseClick(layout.sources.contentX+1, layout.sources.contentY+2))
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("expected all source click to load feed")
	}
	model = runOptionalCmd(t, model, cmd)
	if model.view != domain.SourceAll {
		t.Fatalf("clicked source view = %s, want %s", model.view, domain.SourceAll)
	}
	if model.cursor != 1 {
		t.Fatalf("restored all cursor = %d, want second row", model.cursor)
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

func TestModelMouseWheelScrollsOnlyTargetPanel(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Entries[0].Item.Summary = longPreviewSummary()
	for i := 2; i < 12; i++ {
		entry := snapshot.Entries[1]
		entry.Item.ID = "id-extra-" + time.Date(2026, 5, i, 0, 0, 0, 0, time.UTC).Format("20060102")
		entry.Item.Title = "Extra item"
		snapshot.Entries = append(snapshot.Entries, entry)
	}
	model := NewModel(&fakeService{snapshot: snapshot}, snapshot)
	model.width = 100
	model.height = 12

	updated, cmd := model.Update(mouseWheelDown(88, 4))
	model = updated.(Model)
	if cmd != nil {
		t.Fatal("preview wheel should not load data")
	}
	if model.previewOffset == 0 {
		t.Fatal("preview wheel should scroll preview")
	}
	if model.feedOffset != 0 || model.sourcesOffset != 0 {
		t.Fatalf("preview wheel changed other offsets: feed=%d sources=%d", model.feedOffset, model.sourcesOffset)
	}

	previewOffset := model.previewOffset
	updated, _ = model.Update(mouseWheelDown(30, 4))
	model = updated.(Model)
	if model.feedOffset == 0 {
		t.Fatal("feed wheel should scroll feed")
	}
	if model.previewOffset != previewOffset {
		t.Fatalf("feed wheel changed preview offset: got %d want %d", model.previewOffset, previewOffset)
	}

	updated, _ = model.Update(mouseWheelDown(3, 4))
	model = updated.(Model)
	if model.sourcesOffset == 0 {
		t.Fatal("sources wheel should scroll sources")
	}
}

func TestModelHiddenKeyTogglesHiddenState(t *testing.T) {
	service := &fakeService{snapshot: tuiSnapshot(false)}
	model := NewModel(service, tuiSnapshot(false))

	updated, cmd := model.Update(keyPress("h"))
	if cmd == nil {
		t.Fatal("expected hide command")
	}
	_, _ = updated.Update(cmd())
	if !service.lastHiddenValue {
		t.Fatal("visible item should be hidden")
	}

	snapshot := tuiSnapshot(false)
	snapshot.Filter.IncludeHidden = true
	snapshot.Entries[0].State.Hidden = true
	service = &fakeService{snapshot: snapshot}
	model = NewModel(service, snapshot)
	updated, cmd = model.Update(keyPress("h"))
	if cmd == nil {
		t.Fatal("expected restore command")
	}
	_, _ = updated.Update(cmd())
	if service.lastHiddenValue {
		t.Fatal("hidden item should be restored")
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
	model := NewModel(&fakeService{snapshot: snapshot}, snapshot)

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
	model := NewModel(&fakeService{snapshot: snapshot}, snapshot, ModelOptions{Config: config.Config{GlamourStyle: "dark"}})

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
	model := NewModel(&fakeService{snapshot: tuiSnapshot(false)}, tuiSnapshot(false), ModelOptions{Config: config.Config{GlamourStyle: "dark"}})
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
	model := NewModel(&fakeService{snapshot: tuiSnapshot(false)}, tuiSnapshot(false), ModelOptions{Config: config.Config{GlamourStyle: "dark"}})
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
	model := NewModel(&fakeService{snapshot: tuiSnapshot(false)}, tuiSnapshot(false), ModelOptions{Config: config.Config{GlamourStyle: "dark"}})
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
	model := NewModel(&fakeService{snapshot: tuiSnapshot(false)}, tuiSnapshot(false), ModelOptions{Config: config.Config{GlamourStyle: "dark"}})
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
	model := NewModel(&fakeService{snapshot: tuiSnapshot(false)}, tuiSnapshot(false), ModelOptions{Config: config.Config{GlamourStyle: "dark"}})
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
	model := NewModel(&fakeService{snapshot: tuiSnapshot(false)}, tuiSnapshot(false), ModelOptions{Config: config.Config{GlamourStyle: "dark"}})
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
	model := NewModel(&fakeService{snapshot: tuiSnapshot(false)}, tuiSnapshot(false), ModelOptions{Config: config.Config{GlamourStyle: "dark"}})
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
	model := NewModel(&fakeService{snapshot: tuiSnapshot(false)}, tuiSnapshot(false), ModelOptions{Config: config.Config{GlamourStyle: "dark"}})
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

func TestModelDetailLoadingSpinnerAnimatesWithoutRefresh(t *testing.T) {
	snapshot := tuiSnapshot(false)
	service := &fakeService{snapshot: snapshot}
	model := NewModel(service, snapshot, ModelOptions{Config: config.Config{GlamourStyle: "dark"}})
	model.refreshing = false

	model, cmd := updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected detail load command")
	}
	before := model.render()

	updated, next := model.Update(spinner.TickMsg{})
	model = updated.(Model)
	if next == nil {
		t.Fatal("detail loading spinner should schedule the next tick")
	}
	after := model.render()
	if before == after {
		t.Fatalf("detail loading spinner did not change render:\n%s", ansi.Strip(after))
	}
	if !strings.Contains(ansi.Strip(after), "Loading detail") {
		t.Fatalf("detail loading render lost loading label:\n%s", ansi.Strip(after))
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
	model := NewModel(&fakeService{snapshot: snapshot}, snapshot, ModelOptions{Config: config.Config{GlamourStyle: "dark"}})

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
	model := NewModel(&fakeService{snapshot: snapshot}, snapshot, ModelOptions{Config: config.Config{GlamourStyle: "dark"}})

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

func TestModelPaletteFiltersAndExecutesExport(t *testing.T) {
	snapshot := tuiSnapshot(false)
	service := &fakeService{snapshot: snapshot}
	model := NewModel(service, snapshot, ModelOptions{Config: config.Config{DataDir: t.TempDir()}})

	model, cmd := updateModelWithKey(t, model, "p")
	if cmd != nil {
		t.Fatal("opening palette should not run command")
	}
	for _, key := range []string{"c", "s", "v"} {
		model, cmd = updateModelWithKey(t, model, key)
		if cmd != nil {
			t.Fatalf("typing %q should not run command", key)
		}
	}
	if !strings.Contains(model.render(), "Export saved CSV") {
		t.Fatalf("palette did not filter to CSV export:\n%s", model.render())
	}
	model, cmd = updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected export command")
	}
	model = runOptionalCmd(t, model, cmd)
	if service.exportFormat != app.ExportCSV {
		t.Fatalf("export format = %s, want csv", service.exportFormat)
	}
	if !strings.Contains(model.message, "exported saved items") {
		t.Fatalf("message = %q, want export success", model.message)
	}
}

func TestModelPaletteCreatesBoostRuleFromSearch(t *testing.T) {
	snapshot := tuiSnapshot(false)
	service := &fakeService{snapshot: snapshot}
	model := NewModel(service, snapshot)
	model.filter.Search = "sqlite"

	model, _ = updateModelWithKey(t, model, "p")
	for _, key := range []string{"b", "o", "o", "s", "t"} {
		model, _ = updateModelWithKey(t, model, key)
	}
	if !strings.Contains(model.render(), "Boost from current item") {
		t.Fatalf("palette missing boost rule command:\n%s", model.render())
	}
	model, cmd := updateModelWithKey(t, model, "enter")
	model = runOptionalCmd(t, model, cmd)

	if service.createdRule.Effect != domain.RuleEffectBoost || service.createdRule.Target != domain.RuleTargetKeyword || service.createdRule.Value != "sqlite" {
		t.Fatalf("created rule = %+v, want boost keyword sqlite", service.createdRule)
	}
	if !strings.Contains(model.message, "rule created") {
		t.Fatalf("message = %q, want rule created", model.message)
	}
}

func TestModelPaletteOpensAddRuleForm(t *testing.T) {
	snapshot := tuiSnapshot(false)
	service := &fakeService{snapshot: snapshot}
	model := NewModel(service, snapshot)

	model, _ = updateModelWithKey(t, model, "p")
	for _, key := range []string{"a", "d", "d", " ", "p", "e", "r", "s", "o", "n"} {
		model, _ = updateModelWithKey(t, model, key)
	}
	if !strings.Contains(model.render(), "Add personalization rule") {
		t.Fatalf("palette missing add rule command:\n%s", model.render())
	}
	model, cmd := updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected rule form init command")
	}
	if model.ruleForm == nil {
		t.Fatal("expected rule form to open")
	}
	if !strings.Contains(model.render(), "PERSONALIZATION RULE") {
		t.Fatalf("render missing rule form:\n%s", model.render())
	}
}

func TestModelRuleFormCreatesRuleFromRulesPanel(t *testing.T) {
	snapshot := tuiSnapshot(false)
	service := &fakeService{snapshot: snapshot}
	model := NewModel(service, snapshot)
	model.openRulesPanel()

	model, cmd := updateModelWithKey(t, model, "a")
	if cmd == nil {
		t.Fatal("expected rule form init command")
	}
	if model.ruleForm == nil {
		t.Fatal("expected rule form to open")
	}
	model.ruleDraft.Effect = string(domain.RuleEffectHide)
	model.ruleDraft.Target = string(domain.RuleTargetDomain)
	model.ruleDraft.Value = "Example.COM"
	model.ruleDraft.Enabled = false
	model.ruleForm.State = huh.StateCompleted
	updated, cmd := model.applyRuleFormState(nil)
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("expected create rule command")
	}
	model = runOptionalCmd(t, model, cmd)

	if service.createdRule.Effect != domain.RuleEffectHide || service.createdRule.Target != domain.RuleTargetDomain || service.createdRule.Value != "Example.COM" || service.createdRule.Enabled {
		t.Fatalf("created rule = %+v", service.createdRule)
	}
	if model.ruleForm != nil {
		t.Fatal("rule form should close after create")
	}
}

func TestModelPaletteFiltersAndExecutesClearCache(t *testing.T) {
	snapshot := tuiSnapshot(false)
	service := &fakeService{snapshot: snapshot}
	model := NewModel(service, snapshot)

	model, cmd := updateModelWithKey(t, model, "p")
	if cmd != nil {
		t.Fatal("opening palette should not run command")
	}
	for _, key := range []string{"c", "l", "e", "a", "r"} {
		model, cmd = updateModelWithKey(t, model, key)
		if cmd != nil {
			t.Fatalf("typing %q should not run command", key)
		}
	}
	if !strings.Contains(model.render(), "Clear cache") {
		t.Fatalf("palette did not filter to clear cache:\n%s", model.render())
	}
	model, cmd = updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected clear cache command")
	}
	updated, next := model.Update(commandMsg(t, cmd))
	model = updated.(Model)
	if !service.cacheCleared {
		t.Fatal("expected service cache clear")
	}
	if len(model.entries) != 0 {
		t.Fatalf("entries = %d, want cleared feed", len(model.entries))
	}
	if !model.refreshing {
		t.Fatal("clear cache should immediately start a refresh")
	}
	if next == nil {
		t.Fatal("expected refresh command after clear cache")
	}
	model = runOptionalCmd(t, model, next)
	if !service.lastForce {
		t.Fatal("clear cache refresh should force a network request")
	}
	if service.lastRefreshMode != app.RefreshModeVisible {
		t.Fatalf("refresh mode = %s, want visible", service.lastRefreshMode)
	}
}

func TestModelRulesPanelTogglesAndDeletesRules(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Rules = []domain.PersonalizationRule{{
		ID:      7,
		Effect:  domain.RuleEffectMute,
		Target:  domain.RuleTargetKeyword,
		Value:   "crypto",
		Enabled: true,
	}}
	service := &fakeService{snapshot: snapshot}
	model := NewModel(service, snapshot)

	model, _ = updateModelWithKey(t, model, "p")
	for _, key := range []string{"r", "u", "l", "e", "s"} {
		model, _ = updateModelWithKey(t, model, key)
	}
	model, cmd := updateModelWithKey(t, model, "enter")
	if cmd != nil {
		t.Fatal("opening rules panel should not run command")
	}
	if !strings.Contains(model.render(), "PERSONALIZATION RULES") || !strings.Contains(model.render(), "crypto") {
		t.Fatalf("rules panel did not render:\n%s", model.render())
	}
	for _, want := range []string{"a add", "e edit"} {
		if !strings.Contains(model.render(), want) {
			t.Fatalf("rules panel missing help %q:\n%s", want, model.render())
		}
	}

	model, cmd = updateModelWithKey(t, model, "space")
	model = runOptionalCmd(t, model, cmd)
	if service.toggledRuleID != 7 || service.toggledRuleEnabled {
		t.Fatalf("toggle = id %d enabled %v, want 7 false", service.toggledRuleID, service.toggledRuleEnabled)
	}
	model, cmd = updateModelWithKey(t, model, "d")
	model = runOptionalCmd(t, model, cmd)
	if service.deletedRuleID != 7 {
		t.Fatalf("deleted rule = %d, want 7", service.deletedRuleID)
	}
}

func TestModelRuleFormEditsSelectedRule(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Rules = []domain.PersonalizationRule{{
		ID:      7,
		Effect:  domain.RuleEffectMute,
		Target:  domain.RuleTargetKeyword,
		Value:   "crypto",
		Enabled: true,
	}}
	service := &fakeService{snapshot: snapshot}
	model := NewModel(service, snapshot)
	model.openRulesPanel()

	model, cmd := updateModelWithKey(t, model, "e")
	if cmd == nil {
		t.Fatal("expected edit form init command")
	}
	if model.ruleForm == nil {
		t.Fatal("expected rule form to open")
	}
	if model.ruleDraft.Effect != string(domain.RuleEffectMute) || model.ruleDraft.Target != string(domain.RuleTargetKeyword) || model.ruleDraft.Value != "crypto" || !model.ruleDraft.Enabled {
		t.Fatalf("rule draft not populated: %+v", model.ruleDraft)
	}
	model.ruleDraft.Effect = string(domain.RuleEffectBoost)
	model.ruleDraft.Target = string(domain.RuleTargetTag)
	model.ruleDraft.Value = "go"
	model.ruleDraft.Enabled = false
	model.ruleForm.State = huh.StateCompleted
	updated, cmd := model.applyRuleFormState(nil)
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("expected update rule command")
	}
	model = runOptionalCmd(t, model, cmd)

	if service.updatedRuleID != 7 || service.updatedRule.Effect != domain.RuleEffectBoost || service.updatedRule.Target != domain.RuleTargetTag || service.updatedRule.Value != "go" || service.updatedRule.Enabled {
		t.Fatalf("updated rule id=%d rule=%+v", service.updatedRuleID, service.updatedRule)
	}
}

func TestModelDedupePanelRendersAndIgnoresCandidate(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.DedupeCandidates = []domain.DedupeCandidate{{
		Key:       "a:b",
		Score:     0.92,
		Distance:  5,
		Reason:    "simhash title",
		CreatedAt: time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 5, 13, 10, 5, 0, 0, time.UTC),
		ItemA: domain.FeedItem{
			ID:           "a",
			CanonicalKey: "repo:owner/a",
			Title:        "SQLite FTS search",
			URL:          "https://github.com/owner/a",
			Refs:         domain.Refs{Repo: "owner/a"},
			SimHash:      "1111111111111111",
			Sources: []domain.ItemSource{{
				Source:     domain.SourceGitHub,
				SourceView: "trending:daily",
				SourceRank: 3,
			}},
		},
		ItemB: domain.FeedItem{
			ID:           "b",
			CanonicalKey: "hackernews:42",
			Title:        "SQLite full text search",
			URL:          "https://news.ycombinator.com/item?id=42",
			Refs:         domain.Refs{HNID: "42"},
			SimHash:      "2222222222222222",
			Sources: []domain.ItemSource{{
				Source:     domain.SourceHackerNews,
				SourceView: "top",
				SourceRank: 1,
			}},
		},
	}}
	service := &fakeService{snapshot: snapshot}
	model := NewModel(service, snapshot)

	model, _ = updateModelWithKey(t, model, "p")
	for _, key := range []string{"d", "e", "d", "u", "p", "e"} {
		model, _ = updateModelWithKey(t, model, key)
	}
	model, cmd := updateModelWithKey(t, model, "enter")
	if cmd != nil {
		t.Fatal("opening dedupe panel should not run command")
	}
	if !strings.Contains(model.render(), "DEDUPE CANDIDATES") || !strings.Contains(model.render(), "SQLite FTS search") {
		t.Fatalf("dedupe panel did not render:\n%s", model.render())
	}
	for _, want := range []string{"key a:b", "score 0.92", "distance 5", "GitHub/trending:daily rank 3", "repo:owner/a", "owner/a", "1111111111111111", "https://github.com/owner/a", "Hacker News/top rank 1", "hackernews:42", "2222222222222222"} {
		if !strings.Contains(model.render(), want) {
			t.Fatalf("dedupe debug details missing %q:\n%s", want, model.render())
		}
	}

	model, cmd = updateModelWithKey(t, model, "i")
	model = runOptionalCmd(t, model, cmd)
	if service.ignoredCandidateKey != "a:b" {
		t.Fatalf("ignored candidate = %q, want a:b", service.ignoredCandidateKey)
	}
}

func TestModelSettingsFormSavesRuntimeConfig(t *testing.T) {
	snapshot := tuiSnapshot(false)
	saved := false
	var savedConfig config.Config
	service := &fakeService{snapshot: snapshot}
	model := NewModel(service, snapshot, ModelOptions{
		Config: config.Config{
			GlamourStyle:         "dark",
			MarkdownImagePreview: "auto",
			HTTPCacheTTLHours:    6,
			EnabledSources:       []string{"github", "hackernews", "huggingface", "lobsters", "producthunt"},
			GitHubToken:          "old-gh",
			ProductHuntToken:     "old-ph",
		},
		SaveConfig: func(cfg config.Config) error {
			saved = true
			savedConfig = cfg
			return nil
		},
	})

	model, cmd := updateModelWithKey(t, model, "c")
	if cmd != nil {
		t.Fatal("settings should open without an init command")
	}
	if !strings.Contains(model.render(), "SETTINGS") {
		t.Fatalf("render missing settings form:\n%s", model.render())
	}
	model.view = domain.SourceGitHub
	model.settingsDraft.MarkdownImagePreview = "halfblocks"
	model.settingsDraft.EnabledSources = []string{"hackernews"}
	model.settingsDraft.GitHubToken = "new-gh"
	model.settingsDraft.ProductHuntToken = "new-ph"
	updated, cmd := model.applySettingsState(nil)
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("expected save settings command")
	}
	model = runOptionalCmd(t, model, cmd)
	if !saved {
		t.Fatal("settings save callback was not called")
	}
	if savedConfig.GlamourStyle != "dark" || savedConfig.MarkdownImagePreview != "halfblocks" || savedConfig.HTTPCacheTTLHours != 6 || savedConfig.AccessibleForms {
		t.Fatalf("unexpected saved config: %+v", savedConfig)
	}
	if !slices.Equal(savedConfig.EnabledSources, []string{"hackernews"}) || savedConfig.GitHubToken != "new-gh" || savedConfig.ProductHuntToken != "new-ph" {
		t.Fatalf("source settings not saved: %+v", savedConfig)
	}
	if !slices.Equal(service.enabledSources, []domain.SourceID{domain.SourceHackerNews}) {
		t.Fatalf("service enabled sources = %+v", service.enabledSources)
	}
	if service.tokens[domain.SourceGitHub] != "new-gh" || service.tokens[domain.SourceProductHunt] != "new-ph" {
		t.Fatalf("service tokens = %+v", service.tokens)
	}
	if model.view != domain.SourceAll {
		t.Fatalf("disabled active source should switch to all, got %s", model.view)
	}
	if strings.Contains(model.render(), "SETTINGS") {
		t.Fatalf("settings form should close after submit:\n%s", model.render())
	}
}

func TestModelSettingsRenderGroupsAllSettings(t *testing.T) {
	snapshot := tuiSnapshot(false)
	model := NewModel(&fakeService{snapshot: snapshot}, snapshot, ModelOptions{
		Config: config.Config{
			GlamourStyle:         "dark",
			MarkdownImagePreview: "auto",
			HTTPCacheTTLHours:    6,
			EnabledSources:       []string{"github", "hackernews", "huggingface", "lobsters", "producthunt"},
			GitHubToken:          "old-gh",
			ProductHuntToken:     "old-ph",
		},
	})
	model.width = 120
	model.height = 32

	model, cmd := updateModelWithKey(t, model, "c")
	if cmd != nil {
		t.Fatal("settings should open without an init command")
	}

	plain := ansi.Strip(model.render())
	for _, want := range []string{
		"Display",
		"Markdown style",
		"Markdown image preview",
		"Sources",
		"Visible sources",
		"Credentials",
		"GitHub token",
		"Product Hunt token",
		"System",
		"HTTP cache TTL",
		"Accessible forms",
		"Save settings",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("settings layout missing %q:\n%s", want, plain)
		}
	}
}

func TestModelSettingsRenderFillsScreenWithModuleDividersAndBottomHelp(t *testing.T) {
	snapshot := tuiSnapshot(false)
	model := NewModel(&fakeService{snapshot: snapshot}, snapshot, ModelOptions{
		Config: config.Config{
			GlamourStyle:         "dark",
			MarkdownImagePreview: "auto",
			HTTPCacheTTLHours:    6,
			EnabledSources:       []string{"github", "hackernews", "huggingface", "lobsters", "producthunt"},
			GitHubToken:          "old-gh",
			ProductHuntToken:     "old-ph",
		},
	})
	model.width = 100
	model.height = 34

	model, cmd := updateModelWithKey(t, model, "c")
	if cmd != nil {
		t.Fatal("settings should open without an init command")
	}

	rendered := model.render()
	lines := strings.Split(rendered, "\n")
	if len(lines) != model.height {
		t.Fatalf("settings lines = %d, want terminal height %d:\n%s", len(lines), model.height, ansi.Strip(rendered))
	}
	if strings.TrimSpace(ansi.Strip(lines[0])) == "" {
		t.Fatalf("settings should start at top edge:\n%s", ansi.Strip(rendered))
	}
	for idx, line := range lines {
		if got := ansi.StringWidth(line); got != model.width {
			t.Fatalf("settings line %d width = %d, want %d: %q", idx, got, model.width, ansi.Strip(line))
		}
	}

	plain := ansi.Strip(rendered)
	dividers := settingsDividerLineIndexes(lines)
	if len(dividers) != 3 {
		t.Fatalf("settings module dividers = %d, want 3:\n%s", len(dividers), plain)
	}
	for _, label := range []string{"Markdown style", "GitHub token", "HTTP cache TTL"} {
		if settingsLineFollowedByDivider(lines, label) {
			t.Fatalf("setting row %q should not have an item-level divider:\n%s", label, plain)
		}
	}
	for _, label := range []string{"Markdown image preview", "Visible sources", "Product Hunt token"} {
		if !settingsLineFollowedByDivider(lines, label) {
			t.Fatalf("module ending row %q should be followed by a divider:\n%s", label, plain)
		}
	}
	help := settingsLineContent(lines[len(lines)-2])
	if !strings.Contains(help, " ｜ ") || !strings.Contains(help, "enter save") || !strings.Contains(help, "esc cancel") {
		t.Fatalf("settings help should be bottom-aligned and separated with full-width bars, got %q", help)
	}
	if strings.Contains(help, "  ") {
		t.Fatalf("settings help should use separators instead of repeated spacing, got %q", help)
	}
	actionLine := -1
	for idx, line := range lines {
		if strings.Contains(ansi.Strip(line), "Save settings") {
			actionLine = idx
			break
		}
	}
	if actionLine < 0 {
		t.Fatalf("settings action row missing:\n%s", plain)
	}
	actionContent := settingsContentLineRaw(lines[actionLine])
	if strings.Index(actionContent, "Save settings") < 55 {
		t.Fatalf("settings actions should be right aligned, got %q", actionContent)
	}
	if actionLine < 2 || settingsLineContent(lines[actionLine-1]) != "" || settingsLineContent(lines[actionLine-2]) != "" {
		t.Fatalf("settings actions should keep vertical spacing above them:\n%s", plain)
	}
}

func settingsDividerLineIndexes(lines []string) []int {
	var indexes []int
	for idx, line := range lines {
		content := settingsLineContent(line)
		if content != "" && strings.Trim(content, "─") == "" {
			indexes = append(indexes, idx)
		}
	}
	return indexes
}

func settingsLineFollowedByDivider(lines []string, label string) bool {
	for idx, line := range lines {
		if !strings.Contains(ansi.Strip(line), label) {
			continue
		}
		return idx+1 < len(lines) && strings.Trim(settingsLineContent(lines[idx+1]), "─") == ""
	}
	return false
}

func settingsLineContent(line string) string {
	return strings.Trim(strings.TrimSpace(ansi.Strip(line)), "│ ")
}

func settingsContentLineRaw(line string) string {
	content := ansi.Strip(line)
	content = strings.TrimPrefix(content, "│")
	content = strings.TrimSuffix(content, "│")
	return content
}

func TestModelSettingsMouseClickEditsIndependentToken(t *testing.T) {
	snapshot := tuiSnapshot(false)
	saved := false
	var savedConfig config.Config
	model := NewModel(&fakeService{snapshot: snapshot}, snapshot, ModelOptions{
		Config: config.Config{
			GlamourStyle:         "dark",
			MarkdownImagePreview: "auto",
			HTTPCacheTTLHours:    6,
			EnabledSources:       []string{"github", "hackernews", "huggingface", "lobsters", "producthunt"},
			GitHubToken:          "old-gh",
			ProductHuntToken:     "old-ph",
		},
		SaveConfig: func(cfg config.Config) error {
			saved = true
			savedConfig = cfg
			return nil
		},
	})
	model.width = 120
	model.height = 32

	model, _ = updateModelWithKey(t, model, "c")
	x, y, ok := visibleCellContaining(model.render(), func(line string) bool {
		return strings.Contains(line, "Product Hunt token")
	}, "Product Hunt token")
	if !ok {
		t.Fatalf("settings render missing Product Hunt token:\n%s", ansi.Strip(model.render()))
	}
	updated, cmd := model.Update(mouseClick(x+2, y))
	if cmd != nil {
		t.Fatal("selecting a settings row should not run a command")
	}
	model = updated.(Model)
	model, _ = updateModelWithKey(t, model, "ctrl+u")
	for _, key := range []string{"n", "e", "w", "-", "p", "h"} {
		model, _ = updateModelWithKey(t, model, key)
	}

	model, cmd = updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected save settings command")
	}
	model = runOptionalCmd(t, model, cmd)
	if !saved {
		t.Fatal("settings save callback was not called")
	}
	if savedConfig.ProductHuntToken != "new-ph" {
		t.Fatalf("product hunt token = %q, want new-ph", savedConfig.ProductHuntToken)
	}
}

func TestModelSettingsTokenInputAcceptsCodeOnlyKeyPress(t *testing.T) {
	snapshot := tuiSnapshot(false)
	model := NewModel(&fakeService{snapshot: snapshot}, snapshot, ModelOptions{
		Config: config.Config{
			GlamourStyle:         "dark",
			MarkdownImagePreview: "auto",
			HTTPCacheTTLHours:    6,
			EnabledSources:       []string{"github", "hackernews", "huggingface", "lobsters", "producthunt"},
			GitHubToken:          "old-gh",
			ProductHuntToken:     "old-ph",
		},
	})
	model.width = 120
	model.height = 32

	model, _ = updateModelWithKey(t, model, "c")
	x, y, ok := visibleCellContaining(model.render(), func(line string) bool {
		return strings.Contains(line, "GitHub token")
	}, "GitHub token")
	if !ok {
		t.Fatalf("settings render missing GitHub token:\n%s", ansi.Strip(model.render()))
	}
	updated, _ := model.Update(mouseClick(x+2, y))
	model = updated.(Model)
	model, _ = updateModelWithKey(t, model, "ctrl+u")
	updated, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: 'x'}))
	model = updated.(Model)

	if model.settingsDraft.GitHubToken != "x" {
		t.Fatalf("github token draft = %q, want x", model.settingsDraft.GitHubToken)
	}
}

func TestModelSettingsTokenEyeButtonTogglesPlainText(t *testing.T) {
	snapshot := tuiSnapshot(false)
	model := NewModel(&fakeService{snapshot: snapshot}, snapshot, ModelOptions{
		Config: config.Config{
			GlamourStyle:         "dark",
			MarkdownImagePreview: "auto",
			HTTPCacheTTLHours:    6,
			EnabledSources:       []string{"github", "hackernews", "huggingface", "lobsters", "producthunt"},
			GitHubToken:          "old-gh",
			ProductHuntToken:     "old-ph",
		},
	})
	model.width = 120
	model.height = 32

	model, _ = updateModelWithKey(t, model, "c")
	if plain := ansi.Strip(model.render()); strings.Contains(plain, "old-gh") {
		t.Fatalf("token should be hidden by default:\n%s", plain)
	}
	x, y, ok := visibleCellContaining(model.render(), func(line string) bool {
		return strings.Contains(line, "GitHub token")
	}, "👁")
	if !ok {
		t.Fatalf("settings render missing token eye button:\n%s", ansi.Strip(model.render()))
	}

	updated, _ := model.Update(mouseClick(x, y))
	model = updated.(Model)
	if plain := ansi.Strip(model.render()); !strings.Contains(plain, "old-gh") {
		t.Fatalf("token should be visible after eye click:\n%s", plain)
	}

	updated, _ = model.Update(mouseClick(x, y))
	model = updated.(Model)
	if plain := ansi.Strip(model.render()); strings.Contains(plain, "old-gh") {
		t.Fatalf("token should be hidden after second eye click:\n%s", plain)
	}
}

func TestModelFirstRunOpensSettingsForm(t *testing.T) {
	snapshot := tuiSnapshot(false)
	model := NewModel(&fakeService{snapshot: snapshot}, snapshot, ModelOptions{FirstRun: true, Config: config.Config{GlamourStyle: "dark"}})

	if !strings.Contains(model.render(), "SETTINGS") {
		t.Fatalf("first-run should render settings:\n%s", model.render())
	}
	model, _ = updateModelWithKey(t, model, "esc")
	if strings.Contains(model.render(), "SETTINGS") {
		t.Fatalf("esc should close first-run settings:\n%s", model.render())
	}
}

func TestModelLoadingStateRendersSourcesWhenNoCache(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Entries = nil
	model := NewModel(&fakeService{snapshot: snapshot}, snapshot)

	rendered := model.render()
	plain := ansi.Strip(rendered)
	for _, want := range []string{"Loading sources", "%", "[GH]", "[HN]", "[HF]"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("loading render missing %q:\n%s", want, plain)
		}
	}
}

func TestModelSingleSourceLoadingStateRendersOnlyCurrentSource(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.View = domain.SourceGitHub
	snapshot.Filter = app.FeedFilter{SourceView: "trending:daily"}
	snapshot.Entries = nil
	snapshot.Statuses = []domain.SourceHealth{
		{Source: domain.SourceGitHub, Name: "GitHub", Status: domain.SourceStatusRefreshing},
		{Source: domain.SourceHackerNews, Name: "Hacker News", Status: domain.SourceStatusOK},
		{Source: domain.SourceHuggingFace, Name: "HF Papers", Status: domain.SourceStatusOK},
		{Source: domain.SourceLobsters, Name: "Lobsters", Status: domain.SourceStatusOK},
		{Source: domain.SourceProductHunt, Name: "Product Hunt", Status: domain.SourceStatusOK},
	}
	model := NewModel(&fakeService{snapshot: snapshot}, snapshot)

	plain := ansi.Strip(model.renderFeed(80, 10))
	for _, want := range []string{"Loading GitHub", "[GH]", "REFRESHING"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("single-source loading render missing %q:\n%s", want, plain)
		}
	}
	for _, blocked := range []string{"[HN]", "[HF]", "[LB]", "[PH]", "%"} {
		if strings.Contains(plain, blocked) {
			t.Fatalf("single-source loading render leaked %q:\n%s", blocked, plain)
		}
	}
}

type fakeService struct {
	snapshot            app.Snapshot
	refreshErr          error
	savedCalled         bool
	lastForce           bool
	lastRefreshMode     app.RefreshMode
	lastLoadView        domain.SourceID
	lastFilter          app.FeedFilter
	lastHiddenValue     bool
	detail              domain.ItemDetail
	detailErr           error
	detailCalls         int
	exportFormat        app.ExportFormat
	createdRule         domain.PersonalizationRule
	updatedRuleID       int64
	updatedRule         domain.PersonalizationRule
	toggledRuleID       int64
	toggledRuleEnabled  bool
	deletedRuleID       int64
	ignoredCandidateKey string
	cacheCleared        bool
	enabledSources      []domain.SourceID
	tokens              map[domain.SourceID]string
}

type fakeMarkdownImagePreviewer struct {
	requests []markdownImagePreviewRequest
	result   markdownImagePreviewResult
	err      error
}

func (f *fakeMarkdownImagePreviewer) RenderMarkdownImage(_ context.Context, request markdownImagePreviewRequest) (markdownImagePreviewResult, error) {
	f.requests = append(f.requests, request)
	return f.result, f.err
}

func (f *fakeService) LoadFeed(_ context.Context, view domain.SourceID, filter app.FeedFilter) (app.Snapshot, error) {
	f.lastLoadView = view
	f.lastFilter = filter
	snapshot := f.snapshot
	snapshot.View = view
	snapshot.Filter = filter
	return snapshot, nil
}

func (f *fakeService) Refresh(_ context.Context, view domain.SourceID, filter app.FeedFilter, options app.RefreshOptions) (app.Snapshot, error) {
	f.lastForce = options.Force
	f.lastRefreshMode = options.Mode
	f.lastFilter = filter
	snapshot := f.snapshot
	snapshot.View = view
	snapshot.Filter = filter
	return snapshot, f.refreshErr
}

func (f *fakeService) SetSourceConfig(enabled []domain.SourceID, tokens map[domain.SourceID]string) {
	f.enabledSources = append([]domain.SourceID(nil), enabled...)
	f.tokens = make(map[domain.SourceID]string, len(tokens))
	for source, token := range tokens {
		f.tokens[source] = token
	}
}

func (f *fakeService) SetSaved(_ context.Context, _ string, saved bool, _ domain.SourceID, filter app.FeedFilter) (app.Snapshot, error) {
	f.savedCalled = true
	f.snapshot.Entries[0].State.Saved = saved
	f.snapshot.Filter = filter
	return f.snapshot, nil
}

func (f *fakeService) SetRead(context.Context, string, bool, domain.SourceID, app.FeedFilter) (app.Snapshot, error) {
	return f.snapshot, nil
}

func (f *fakeService) SetHidden(_ context.Context, _ string, hidden bool, _ domain.SourceID, filter app.FeedFilter) (app.Snapshot, error) {
	f.lastHiddenValue = hidden
	f.snapshot.Entries[0].State.Hidden = hidden
	f.snapshot.Filter = filter
	return f.snapshot, nil
}

func (f *fakeService) LoadDetail(context.Context, domain.FeedEntry) (domain.ItemDetail, error) {
	f.detailCalls++
	return f.detail, f.detailErr
}

func (f *fakeService) ExportSaved(_ context.Context, options app.ExportOptions) (app.ExportResult, error) {
	f.exportFormat = options.Format
	return app.ExportResult{Path: options.Dir + "/fake-export." + string(options.Format), Count: 2, Format: options.Format}, nil
}

func (f *fakeService) CreatePersonalizationRule(_ context.Context, rule domain.PersonalizationRule, _ domain.SourceID, filter app.FeedFilter) (app.Snapshot, error) {
	f.createdRule = rule
	f.snapshot.Rules = append(f.snapshot.Rules, rule)
	f.snapshot.Filter = filter
	return f.snapshot, nil
}

func (f *fakeService) UpdatePersonalizationRule(_ context.Context, id int64, rule domain.PersonalizationRule, _ domain.SourceID, filter app.FeedFilter) (app.Snapshot, error) {
	f.updatedRuleID = id
	f.updatedRule = rule
	for idx := range f.snapshot.Rules {
		if f.snapshot.Rules[idx].ID == id {
			rule.ID = id
			f.snapshot.Rules[idx] = rule
		}
	}
	f.snapshot.Filter = filter
	return f.snapshot, nil
}

func (f *fakeService) SetPersonalizationRuleEnabled(_ context.Context, id int64, enabled bool, _ domain.SourceID, filter app.FeedFilter) (app.Snapshot, error) {
	f.toggledRuleID = id
	f.toggledRuleEnabled = enabled
	for idx := range f.snapshot.Rules {
		if f.snapshot.Rules[idx].ID == id {
			f.snapshot.Rules[idx].Enabled = enabled
		}
	}
	f.snapshot.Filter = filter
	return f.snapshot, nil
}

func (f *fakeService) DeletePersonalizationRule(_ context.Context, id int64, _ domain.SourceID, filter app.FeedFilter) (app.Snapshot, error) {
	f.deletedRuleID = id
	next := f.snapshot.Rules[:0]
	for _, rule := range f.snapshot.Rules {
		if rule.ID != id {
			next = append(next, rule)
		}
	}
	f.snapshot.Rules = next
	f.snapshot.Filter = filter
	return f.snapshot, nil
}

func (f *fakeService) IgnoreDedupeCandidate(_ context.Context, key string, _ domain.SourceID, filter app.FeedFilter) (app.Snapshot, error) {
	f.ignoredCandidateKey = key
	next := f.snapshot.DedupeCandidates[:0]
	for _, candidate := range f.snapshot.DedupeCandidates {
		if candidate.Key != key {
			next = append(next, candidate)
		}
	}
	f.snapshot.DedupeCandidates = next
	f.snapshot.Filter = filter
	return f.snapshot, nil
}

func (f *fakeService) ClearCache(_ context.Context, view domain.SourceID, filter app.FeedFilter) (app.Snapshot, error) {
	f.cacheCleared = true
	f.snapshot.Entries = nil
	f.snapshot.View = view
	f.snapshot.Filter = filter
	return f.snapshot, nil
}

func tuiSnapshot(saved bool) app.Snapshot {
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	entries := []domain.FeedEntry{
		{
			Item: domain.FeedItem{
				ID:         "id-1",
				Title:      "First",
				URL:        "https://example.com/1",
				LastSeenAt: now,
			},
			Sources: []domain.ItemSource{{Source: domain.SourceHackerNews, SourceRank: 1}},
			State:   domain.ItemState{ItemID: "id-1", Saved: saved},
		},
		{
			Item: domain.FeedItem{
				ID:         "id-2",
				Title:      "Second",
				URL:        "https://example.com/2",
				LastSeenAt: now,
			},
			Sources: []domain.ItemSource{{Source: domain.SourceHackerNews, SourceRank: 2}},
			State:   domain.ItemState{ItemID: "id-2"},
		},
	}
	return app.Snapshot{
		View:    domain.SourceAll,
		Entries: entries,
		Counts: map[domain.SourceID]int{
			domain.SourceAll:         2,
			domain.SourceHackerNews:  2,
			domain.SourceGitHub:      0,
			domain.SourceHuggingFace: 0,
			domain.SourceLobsters:    0,
			domain.SourceProductHunt: 0,
		},
		Statuses: []domain.SourceHealth{{
			Source: domain.SourceHackerNews,
			Name:   "Hacker News",
			Status: domain.SourceStatusOK,
		}},
		LoadedAt: now,
	}
}

func configWithSourceOrder(t *testing.T, order []string) config.Config {
	t.Helper()
	return config.Config{GlamourStyle: "dark", SourceOrder: order}
}

func configWithEnabledSources(t *testing.T, sources []string) config.Config {
	t.Helper()
	return config.Config{GlamourStyle: "dark", EnabledSources: sources}
}

func feedEntry(id, title string, source domain.SourceID, rank int, seenAt time.Time) domain.FeedEntry {
	return domain.FeedEntry{
		Item: domain.FeedItem{
			ID:         id,
			Title:      title,
			URL:        "https://example.com/" + id,
			LastSeenAt: seenAt,
		},
		Sources: []domain.ItemSource{{Source: source, SourceRank: rank}},
		State:   domain.ItemState{ItemID: id},
	}
}

func ansiSequenceBefore(value, marker string) string {
	idx := strings.Index(value, marker)
	if idx < 0 {
		return ""
	}
	prefix := value[:idx]
	start := strings.LastIndex(prefix, "\x1b[")
	if start < 0 {
		return ""
	}
	end := strings.LastIndex(prefix[start:], "m")
	if end < 0 {
		return ""
	}
	return prefix[start : start+end+1]
}

func keyPress(value string) tea.KeyPressMsg {
	switch value {
	case "left":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyLeft})
	case "right":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyRight})
	case "up":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyUp})
	case "down":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyDown})
	case "pgup":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyPgUp})
	case "pgdown":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyPgDown})
	case "enter":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
	case "esc":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape})
	case "backspace":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyBackspace})
	case "ctrl+u":
		return tea.KeyPressMsg(tea.Key{Code: 'u', Mod: tea.ModCtrl})
	default:
		runes := []rune(value)
		return tea.KeyPressMsg(tea.Key{Text: value, Code: runes[0]})
	}
}

func mouseClick(x, y int) tea.MouseClickMsg {
	return tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft})
}

func visibleCellContaining(rendered string, lineMatch func(string) bool, needle string) (int, int, bool) {
	for y, line := range strings.Split(ansi.Strip(rendered), "\n") {
		if !lineMatch(line) {
			continue
		}
		idx := strings.Index(line, needle)
		if idx < 0 {
			continue
		}
		return lipgloss.Width(line[:idx]), y, true
	}
	return 0, 0, false
}

func mouseWheelDown(x, y int) tea.MouseWheelMsg {
	return tea.MouseWheelMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseWheelDown})
}

func mouseWheelUp(x, y int) tea.MouseWheelMsg {
	return tea.MouseWheelMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseWheelUp})
}

func assertDetailBottomSpacing(t *testing.T, lines []string) {
	t.Helper()
	const expectedDetailBottomSpacingLines = 3
	if len(lines) < expectedDetailBottomSpacingLines {
		t.Fatalf("detail lines = %d, want at least %d", len(lines), expectedDetailBottomSpacingLines)
	}
	for i := len(lines) - expectedDetailBottomSpacingLines; i < len(lines); i++ {
		if strings.TrimSpace(ansi.Strip(lines[i])) != "" {
			t.Fatalf("detail line %d should be bottom spacing, got %q", i, ansi.Strip(lines[i]))
		}
	}
}

func updateModelWithKey(t *testing.T, model Model, key string) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := model.Update(keyPress(key))
	return updated.(Model), cmd
}

func runKeyCommand(t *testing.T, model Model, key string) Model {
	t.Helper()
	updated, cmd := model.Update(keyPress(key))
	model = updated.(Model)
	if cmd == nil {
		t.Fatalf("expected command for key %s", key)
	}
	updated, next := model.Update(commandMsg(t, cmd))
	model = updated.(Model)
	if next != nil {
		updated, _ = model.Update(commandMsg(t, next))
		model = updated.(Model)
	}
	return model
}

func longPreviewSummary() string {
	return strings.Join([]string{
		"alpha", "bravo", "charlie", "delta", "echo", "foxtrot",
		"golf", "hotel", "india", "juliet", "kilo", "lima",
		"mike", "november", "oscar", "papa", "quebec", "romeo",
		"sierra", "tango", "uniform", "victor", "whiskey", "xray",
		"yankee", "zulu", "final-marker",
	}, " ")
}

func longDetailBody() string {
	lines := make([]string, 0, 31)
	for i := 0; i < 30; i++ {
		lines = append(lines, "detail line "+time.Date(2026, 5, i+1, 0, 0, 0, 0, time.UTC).Format("20060102"))
	}
	lines = append(lines, "final-marker")
	return strings.Join(lines, "\n")
}

func renderedLineCount(rendered string) int {
	if rendered == "" {
		return 0
	}
	return strings.Count(rendered, "\n") + 1
}

func runOptionalCmd(t *testing.T, model Model, cmd tea.Cmd) Model {
	t.Helper()
	for cmd != nil {
		updated, next := model.Update(commandMsg(t, cmd))
		model = updated.(Model)
		cmd = next
	}
	return model
}

func commandMsg(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		if len(batch) == 0 {
			t.Fatal("empty command batch")
		}
		return batch[0]()
	}
	return msg
}

func isRawMsg(msg tea.Msg) bool {
	_, ok := msg.(tea.RawMsg)
	return ok
}

func firstBatchCommandMsg(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("expected command batch, got %T", msg)
	}
	if len(batch) == 0 {
		t.Fatal("empty command batch")
	}
	return batch[0]()
}

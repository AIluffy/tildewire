package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/AIluffy/tildewire/internal/app"
	"github.com/AIluffy/tildewire/internal/config"
	"github.com/AIluffy/tildewire/internal/domain"
)

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
	for _, want := range []string{"Source", "Scope", "Saved", "Unread", "Show hidden items"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("filter overlay missing %q:\n%s", want, rendered)
		}
	}

	for _, key := range []string{
		"l", "l", "l", "l", "l",
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
	model := testModel(snapshot, ModelOptions{
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
	model.openOverlay(overlayHealth)
	if strings.Contains(ansi.Strip(model.renderHealth()), "Product Hunt") {
		t.Fatalf("health rendered hidden Product Hunt:\n%s", model.renderHealth())
	}
}

func TestModelRenderFeedShowsSearchMatchContext(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Filter = app.FeedFilter{Search: "linux"}
	snapshot.Entries[0].Item.Title = "Asteroid"
	snapshot.Entries[0].Item.Subtitle = "135 votes · 26 comments · #10 today"
	snapshot.Entries[0].Item.Summary = "Asteroid lets teams build computer-use agents for browser, Linux, and Windows workflows in minutes."
	model := testModel(snapshot)

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
	model := testModel(snapshot)

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

func TestModelPaletteShowHiddenItemsEnablesHiddenFilter(t *testing.T) {
	snapshot := tuiSnapshot(false)
	service := &fakeService{snapshot: snapshot}
	model := NewModel(service, snapshot)
	model.filter = app.FeedFilter{SavedOnly: true, Search: "sqlite"}

	model, cmd := updateModelWithKey(t, model, "p")
	if cmd != nil {
		t.Fatal("opening palette should not run command")
	}
	for _, key := range []string{"s", "h", "o", "w", " ", "h", "i", "d", "d", "e", "n"} {
		model, cmd = updateModelWithKey(t, model, key)
		if cmd != nil {
			t.Fatalf("typing %q should not run command", key)
		}
	}
	if rendered := ansi.Strip(model.render()); !strings.Contains(rendered, "Show hidden items") {
		t.Fatalf("palette did not filter to show hidden command:\n%s", rendered)
	}

	model, cmd = updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected show hidden command")
	}
	model = runOptionalCmd(t, model, cmd)

	if !service.lastFilter.IncludeHidden {
		t.Fatalf("filter = %+v, want include hidden", service.lastFilter)
	}
	if !service.lastFilter.SavedOnly || service.lastFilter.Search != "sqlite" {
		t.Fatalf("filter = %+v, want existing facets preserved", service.lastFilter)
	}
	if !model.filter.IncludeHidden {
		t.Fatalf("model filter = %+v, want include hidden", model.filter)
	}
	if !strings.Contains(model.message, "showing hidden items") {
		t.Fatalf("message = %q, want showing hidden items", model.message)
	}
}

func TestModelPaletteLabelsSelectedHiddenAction(t *testing.T) {
	snapshot := tuiSnapshot(false)
	model := testModel(snapshot)

	model, cmd := updateModelWithKey(t, model, "p")
	if cmd != nil {
		t.Fatal("opening palette should not run command")
	}
	rendered := ansi.Strip(model.render())
	if !strings.Contains(rendered, "Hide item") {
		t.Fatalf("palette should label visible item action as hide:\n%s", rendered)
	}
	if strings.Contains(rendered, "Hide or restore item") {
		t.Fatalf("palette should not show ambiguous hide label:\n%s", rendered)
	}

	snapshot = tuiSnapshot(false)
	snapshot.Filter.IncludeHidden = true
	snapshot.Entries[0].State.Hidden = true
	model = testModel(snapshot)
	model, cmd = updateModelWithKey(t, model, "p")
	if cmd != nil {
		t.Fatal("opening palette should not run command")
	}
	rendered = ansi.Strip(model.render())
	if !strings.Contains(rendered, "Restore item") {
		t.Fatalf("palette should label hidden item action as restore:\n%s", rendered)
	}
}

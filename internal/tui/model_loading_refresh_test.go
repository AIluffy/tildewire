package tui

import (
	"errors"
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/AIluffy/tildewire/internal/app"
	"github.com/AIluffy/tildewire/internal/config"
	"github.com/AIluffy/tildewire/internal/domain"
)

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

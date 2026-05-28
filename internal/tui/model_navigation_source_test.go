package tui

import (
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/AIluffy/tildewire/internal/app"
	"github.com/AIluffy/tildewire/internal/config"
	"github.com/AIluffy/tildewire/internal/domain"
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

func TestNewModelDefaultsToRecommendView(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.View = ""
	service := &fakeService{snapshot: snapshot}
	model := NewModel(service, snapshot)

	if model.view != domain.SourceRecommend {
		t.Fatalf("default view = %s, want recommend", model.view)
	}
	if model.filter.SourceView != "" {
		t.Fatalf("default recommend source view = %q, want empty", model.filter.SourceView)
	}
	msg := model.loadCmd()()
	if _, ok := msg.(snapshotMsg); !ok {
		t.Fatalf("load command message = %T, want snapshotMsg", msg)
	}
	if service.lastLoadView != domain.SourceRecommend {
		t.Fatalf("default load view = %s, want recommend", service.lastLoadView)
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

func TestModelSourceViewSavePatchesStateWithoutFeedReload(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.View = domain.SourceHackerNews
	service := &fakeService{snapshot: snapshot}
	model := NewModel(service, snapshot)

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
	if service.lastLoadView != "" {
		t.Fatalf("source-view state patch should not reload feed, loaded %s", service.lastLoadView)
	}
	if !model.entries[0].State.Saved {
		t.Fatalf("saved state not patched: %+v", model.entries[0].State)
	}
}

func TestModelSourceViewHiddenWithIncludeHiddenPatchesStateWithoutFeedReload(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.View = domain.SourceHackerNews
	snapshot.Filter.IncludeHidden = true
	service := &fakeService{snapshot: snapshot}
	model := NewModel(service, snapshot)

	updated, cmd := model.Update(keyPress("h"))
	if cmd == nil {
		t.Fatal("expected hide command")
	}
	msg := cmd()
	updated, _ = updated.Update(msg)
	model = updated.(Model)

	if !service.lastHiddenValue {
		t.Fatal("service SetHidden should hide item")
	}
	if service.lastLoadView != "" {
		t.Fatalf("source-view hidden state patch should not reload feed, loaded %s", service.lastLoadView)
	}
	if !model.entries[0].State.Hidden {
		t.Fatalf("hidden state not patched: %+v", model.entries[0].State)
	}
}

func TestModelOpenAndCopyCommandsRecordRecommendationEvents(t *testing.T) {
	previousOpen := openExternalURLFunc
	previousClipboard := writeClipboard
	defer func() {
		openExternalURLFunc = previousOpen
		writeClipboard = previousClipboard
	}()
	var openedURL string
	openExternalURLFunc = func(url string) error {
		openedURL = url
		return nil
	}
	var copiedValue string
	writeClipboard = func(value string) error {
		copiedValue = value
		return nil
	}

	snapshot := tuiSnapshot(false)
	service := &fakeService{snapshot: snapshot}
	model := NewModel(service, snapshot)

	_, openCmd := model.Update(keyPress("o"))
	if openCmd == nil {
		t.Fatal("expected open command")
	}
	if msg := openCmd(); msg.(statusMsg).message != "opened url" {
		t.Fatalf("open status = %+v", msg)
	}
	if openedURL != snapshot.Entries[0].Item.URL {
		t.Fatalf("opened url = %q, want %q", openedURL, snapshot.Entries[0].Item.URL)
	}

	_, copyCmd := model.Update(keyPress("y"))
	if copyCmd == nil {
		t.Fatal("expected copy command")
	}
	if msg := copyCmd(); msg.(statusMsg).message != "copied url" {
		t.Fatalf("copy status = %+v", msg)
	}
	if copiedValue != snapshot.Entries[0].Item.URL {
		t.Fatalf("copied value = %q, want %q", copiedValue, snapshot.Entries[0].Item.URL)
	}

	if len(service.itemEvents) != 2 {
		t.Fatalf("recorded events = %+v, want open and copy events", service.itemEvents)
	}
	if service.itemEvents[0].EventType != domain.ItemEventOpenURL || service.itemEvents[1].EventType != domain.ItemEventCopyURL {
		t.Fatalf("recorded event types = %+v", service.itemEvents)
	}
	if service.itemEvents[0].ItemID != snapshot.Entries[0].Item.ID || service.itemEvents[1].ItemID != snapshot.Entries[0].Item.ID {
		t.Fatalf("recorded item ids = %+v", service.itemEvents)
	}
}

func TestModelOpenSourceWithEmptyURLDoesNotRecordRecommendationEvent(t *testing.T) {
	previousOpen := openExternalURLFunc
	defer func() {
		openExternalURLFunc = previousOpen
	}()
	openCalls := 0
	openExternalURLFunc = func(url string) error {
		openCalls++
		if strings.TrimSpace(url) != "" {
			t.Fatalf("opened url = %q, want empty no-op", url)
		}
		return nil
	}

	snapshot := tuiSnapshot(false)
	service := &fakeService{snapshot: snapshot}
	model := NewModel(service, snapshot)

	_, openCmd := model.Update(keyPress("O"))
	if openCmd == nil {
		t.Fatal("expected open source command")
	}
	if msg := openCmd(); msg.(statusMsg).message != "opened url" {
		t.Fatalf("open source status = %+v", msg)
	}
	if openCalls != 1 {
		t.Fatalf("open calls = %d, want 1", openCalls)
	}
	if len(service.itemEvents) != 0 {
		t.Fatalf("recorded events = %+v, want none for empty source url", service.itemEvents)
	}
}

func TestModelSourceKeysLoadViews(t *testing.T) {
	service := &fakeService{snapshot: tuiSnapshot(false)}
	model := NewModel(service, tuiSnapshot(false))
	for _, tt := range []struct {
		key  string
		view domain.SourceID
	}{
		{key: "0", view: domain.SourceRecommend},
		{key: "1", view: domain.SourceGitHub},
		{key: "2", view: domain.SourceHackerNews},
		{key: "3", view: domain.SourceHuggingFace},
		{key: "4", view: domain.SourceLobsters},
		{key: "5", view: domain.SourceProductHunt},
		{key: "6", view: domain.SourceAILabs},
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
	model = runKeyCommand(t, model, "6")
	if service.lastFilter.SourceView != "" {
		t.Fatalf("AI Labs source view = %q, want aggregate source", service.lastFilter.SourceView)
	}
	model = runKeyCommand(t, model, "0")
	if service.lastFilter.SourceView != "" {
		t.Fatalf("recommend source view = %q, want virtual aggregate source", service.lastFilter.SourceView)
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

func TestModelRecommendScopeKeyNoOps(t *testing.T) {
	service := &fakeService{snapshot: tuiSnapshot(false)}
	model := NewModel(service, tuiSnapshot(false))

	model = runKeyCommand(t, model, "0")
	service.lastLoadView = ""
	model, cmd := updateModelWithKey(t, model, "v")
	if cmd != nil {
		t.Fatal("recommend scope key should not load feed")
	}
	if model.view != domain.SourceRecommend || model.filter.SourceView != "" {
		t.Fatalf("recommend view/filter changed: view=%s filter=%+v", model.view, model.filter)
	}
	if service.lastLoadView != "" {
		t.Fatalf("recommend scope key loaded %s", service.lastLoadView)
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
	if model.view != domain.SourceRecommend {
		t.Fatalf("source down view = %s, want recommend", model.view)
	}
	if service.lastLoadView != domain.SourceRecommend {
		t.Fatalf("source down loaded %s, want recommend", service.lastLoadView)
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

func TestModelSourceCountUsesSnapshotCountsDuringSwitch(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.Counts[domain.SourceGitHub] = 42
	service := &fakeService{snapshot: snapshot}
	model := NewModel(service, snapshot)

	model, cmd := model.switchSource(domain.SourceGitHub)
	if cmd == nil {
		t.Fatal("source switch should load feed")
	}
	if got := model.sourceCount(domain.SourceGitHub); got != 42 {
		t.Fatalf("active GitHub count during switch = %d, want stable snapshot count 42", got)
	}
}

func TestModelSourcesUsePersistedOrder(t *testing.T) {
	cfg := configWithSourceOrder(t, []string{"producthunt", "github", "hackernews"})
	model := NewModel(&fakeService{snapshot: tuiSnapshot(false)}, tuiSnapshot(false), ModelOptions{Config: cfg})

	rendered := ansi.Strip(model.renderSources(24, 8))
	recommendIndex := strings.Index(rendered, "Recommend")
	productHuntIndex := strings.Index(rendered, "Product Hunt")
	githubIndex := strings.Index(rendered, "GitHub")
	hackerNewsIndex := strings.Index(rendered, "Hacker News")
	if recommendIndex < 0 || productHuntIndex < 0 || githubIndex < 0 || hackerNewsIndex < 0 {
		t.Fatalf("render missing configured sources:\n%s", rendered)
	}
	if !(recommendIndex < productHuntIndex && productHuntIndex < githubIndex && githubIndex < hackerNewsIndex) {
		t.Fatalf("sources not rendered in persisted order:\n%s", rendered)
	}
}

func TestModelRecommendEmptyStatePromptsForTraining(t *testing.T) {
	snapshot := tuiSnapshot(false)
	snapshot.View = domain.SourceRecommend
	snapshot.Entries = nil
	model := NewModel(&fakeService{snapshot: snapshot}, snapshot)
	model.refreshing = false

	rendered := ansi.Strip(model.renderFeed(80, 8))
	if !strings.Contains(rendered, "No recommendations yet.") || !strings.Contains(rendered, "Save or boost items to train Recommend.") {
		t.Fatalf("recommend empty state missing training prompt:\n%s", rendered)
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
	wantOrder := []string{"hackernews", "github", "huggingface", "lobsters", "producthunt", "ailabs"}
	if got := model.config.SourceOrder; !slices.Equal(got, wantOrder) {
		t.Fatalf("model source order = %#v, want %#v", got, wantOrder)
	}
	if got := savedConfig.SourceOrder; !slices.Equal(got, wantOrder) {
		t.Fatalf("saved source order = %#v, want %#v", got, wantOrder)
	}
}

func TestModelMouseClickSourcesLoadsSelectedSource(t *testing.T) {
	service := &fakeService{snapshot: tuiSnapshot(false)}
	model := NewModel(service, tuiSnapshot(false))
	model.width = 100
	model.height = 16

	updated, cmd := model.Update(mouseClick(3, 7))
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
	updated, cmd := model.Update(mouseClick(layout.sources.contentX+1, layout.sources.contentY+4))
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

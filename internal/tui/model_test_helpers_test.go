package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/AIluffy/tildewire/internal/app"
	"github.com/AIluffy/tildewire/internal/config"
	"github.com/AIluffy/tildewire/internal/domain"
)

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
	itemEvents          []domain.ItemEvent
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

func (f *fakeService) RecordItemEvent(_ context.Context, event domain.ItemEvent) error {
	f.itemEvents = append(f.itemEvents, event)
	return nil
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
			domain.SourceAILabs:      0,
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

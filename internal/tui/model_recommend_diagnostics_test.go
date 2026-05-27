package tui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/AIluffy/tildewire/internal/domain"
)

func TestModelPaletteOpensRecommendDiagnostics(t *testing.T) {
	snapshot := tuiSnapshot(false)
	service := &fakeService{
		snapshot:    snapshot,
		diagnostics: testRecommendationDiagnostics(snapshot.Entries[0].Item.ID),
	}
	model := NewModel(service, snapshot)

	model, cmd := updateModelWithKey(t, model, "p")
	if cmd != nil {
		t.Fatal("opening palette should not run command")
	}
	for _, key := range []string{"d", "i", "a", "g"} {
		model, cmd = updateModelWithKey(t, model, key)
		if cmd != nil {
			t.Fatalf("typing %q should not run command", key)
		}
	}
	if rendered := ansi.Strip(model.render()); !strings.Contains(rendered, "Recommend diagnostics") {
		t.Fatalf("palette missing diagnostics command:\n%s", rendered)
	}
	model, cmd = updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected diagnostics command")
	}
	model = runOptionalCmd(t, model, cmd)

	rendered := ansi.Strip(model.render())
	for _, want := range []string{"RECOMMEND DIAGNOSTICS", "Profile", "Selected Item", "tag:ai", "opened ai"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("diagnostics panel missing %q:\n%s", want, rendered)
		}
	}
	if service.diagnosticsCalls != 1 {
		t.Fatalf("diagnostics calls = %d, want 1", service.diagnosticsCalls)
	}
}

func TestModelRecommendDiagnosticsEmptyAndNarrowRender(t *testing.T) {
	model := NewModel(&fakeService{snapshot: tuiSnapshot(false)}, tuiSnapshot(false))
	model.width = 42
	model.recommendDiagnosticsOpen = true
	model.recommendDiagnostics = domain.RecommendationDiagnostics{}

	rendered := ansi.Strip(model.render())
	for _, want := range []string{"RECOMMEND DIAGNOSTICS", "No profile terms yet.", "No selected item."} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("diagnostics empty panel missing %q:\n%s", want, rendered)
		}
	}
	for lineNumber, line := range strings.Split(rendered, "\n") {
		if len([]rune(line)) > model.width {
			t.Fatalf("line %d overflows width %d: %q", lineNumber, model.width, line)
		}
	}
}

func TestModelRecommendDiagnosticsKeys(t *testing.T) {
	snapshot := tuiSnapshot(false)
	model := NewModel(&fakeService{snapshot: snapshot}, snapshot)
	model.recommendDiagnosticsOpen = true

	updated, cmd := model.Update(keyPress("?"))
	model = updated.(Model)
	if cmd != nil {
		t.Fatal("help toggle should not run command")
	}
	if !model.help.ShowAll {
		t.Fatal("help should be visible")
	}

	updated, cmd = model.Update(keyPress("esc"))
	model = updated.(Model)
	if cmd != nil {
		t.Fatal("esc should not run command")
	}
	if model.recommendDiagnosticsOpen {
		t.Fatal("esc should close diagnostics")
	}

	model.recommendDiagnosticsOpen = true
	_, cmd = model.Update(keyPress("q"))
	if cmd == nil {
		t.Fatal("q should quit diagnostics panel")
	}
	if msg := cmd(); msg != tea.Quit() {
		t.Fatalf("quit command msg = %#v", msg)
	}
}

func TestModelRecommendDiagnosticsClearsStalePayload(t *testing.T) {
	snapshot := tuiSnapshot(false)
	model := NewModel(&fakeService{snapshot: snapshot}, snapshot)
	model.recommendDiagnostics = testRecommendationDiagnostics("stale")
	model.openRecommendDiagnosticsPanel()
	if model.recommendDiagnostics.Selected.ItemID != "" {
		t.Fatalf("open diagnostics retained stale payload: %+v", model.recommendDiagnostics.Selected)
	}

	updated, cmd := model.Update(recommendDiagnosticsMsg{err: errors.New("boom")})
	if cmd != nil {
		t.Fatal("diagnostics error should not run command")
	}
	model = updated.(Model)
	rendered := ansi.Strip(model.render())
	if strings.Contains(rendered, "opened ai") || strings.Contains(rendered, "stale") {
		t.Fatalf("diagnostics error rendered stale payload:\n%s", rendered)
	}
}

func testRecommendationDiagnostics(itemID string) domain.RecommendationDiagnostics {
	return domain.RecommendationDiagnostics{
		PositiveTerms: []domain.RecommendationProfileTerm{{
			Kind:     "tag",
			Value:    "ai",
			Positive: 1.2,
		}},
		NegativeTerms: []domain.RecommendationProfileTerm{{
			Kind:     "tag",
			Value:    "rust",
			Negative: 0.4,
		}},
		Selected: domain.RecommendationDiagnosticsItem{
			ItemID:            itemID,
			Title:             "First",
			InterestScore:     1.0,
			HotScore:          0.8,
			Score:             1.2,
			HasPositiveSignal: true,
			Eligible:          true,
			MatchedTerms: []domain.RecommendationDiagnosticsTerm{{
				Kind:         "tag",
				Value:        "ai",
				ItemWeight:   1,
				Positive:     1.2,
				Contribution: 1.2,
			}},
			Reasons: []domain.RecommendationReason{{
				Kind:   "tag",
				Value:  "ai",
				Label:  "opened ai",
				Weight: 1.2,
			}},
		},
	}
}

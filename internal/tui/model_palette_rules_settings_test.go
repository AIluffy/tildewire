package tui

import (
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/AIluffy/tildewire/internal/config"
	"github.com/AIluffy/tildewire/internal/domain"
)

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
			Theme:                "catppuccin",
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
	model.settingsDraft.Theme = "dracula"
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
	if savedConfig.Theme != "dracula" || savedConfig.GlamourStyle != "dracula" || savedConfig.MarkdownImagePreview != "halfblocks" || savedConfig.HTTPCacheTTLHours != 6 || savedConfig.AccessibleForms {
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
			Theme:                "catppuccin",
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
		"Theme",
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
	if strings.Contains(plain, "Markdown style") {
		t.Fatalf("settings layout should expose global Theme instead of Markdown style:\n%s", plain)
	}
}

func TestModelSettingsRenderFillsScreenWithModuleDividersAndBottomHelp(t *testing.T) {
	snapshot := tuiSnapshot(false)
	model := NewModel(&fakeService{snapshot: snapshot}, snapshot, ModelOptions{
		Config: config.Config{
			Theme:                "catppuccin",
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
	for _, label := range []string{"Theme", "GitHub token", "HTTP cache TTL"} {
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

func TestModelPaletteThemeCommandPersistsTheme(t *testing.T) {
	snapshot := tuiSnapshot(false)
	saved := false
	var savedConfig config.Config
	model := NewModel(&fakeService{snapshot: snapshot}, snapshot, ModelOptions{
		Config: config.Config{
			Theme:                "catppuccin",
			GlamourStyle:         "dark",
			MarkdownImagePreview: "auto",
			HTTPCacheTTLHours:    6,
			EnabledSources:       []string{"github", "hackernews", "huggingface", "lobsters", "producthunt"},
		},
		SaveConfig: func(cfg config.Config) error {
			saved = true
			savedConfig = cfg
			return nil
		},
	})

	model, _ = updateModelWithKey(t, model, "p")
	for _, key := range []string{"t", "h", "e", "m", "e", ":", " ", "d", "r", "a", "c", "u", "l", "a"} {
		model, _ = updateModelWithKey(t, model, key)
	}
	if plain := ansi.Strip(model.render()); !strings.Contains(plain, "Theme: Dracula") {
		t.Fatalf("palette missing dracula theme command:\n%s", plain)
	}

	model, cmd := updateModelWithKey(t, model, "enter")
	if cmd == nil {
		t.Fatal("expected save config command")
	}
	model = runOptionalCmd(t, model, cmd)

	if model.config.Theme != "dracula" {
		t.Fatalf("model theme = %q, want dracula", model.config.Theme)
	}
	if !saved || savedConfig.Theme != "dracula" {
		t.Fatalf("saved theme = %q saved=%v", savedConfig.Theme, saved)
	}
}

func TestModelThemeChangesRenderedColors(t *testing.T) {
	snapshot := tuiSnapshot(false)
	model := NewModel(&fakeService{snapshot: snapshot}, snapshot, ModelOptions{
		Config: config.Config{
			Theme:                "catppuccin",
			GlamourStyle:         "dark",
			MarkdownImagePreview: "auto",
			HTTPCacheTTLHours:    6,
			EnabledSources:       []string{"github", "hackernews", "huggingface", "lobsters", "producthunt"},
		},
	})
	model.width = 100
	model.height = 30
	catppuccin := ansiSequenceBefore(model.render(), "View:")

	model.applyTheme("dracula")
	dracula := ansiSequenceBefore(model.render(), "View:")

	if catppuccin == "" || dracula == "" {
		t.Fatalf("rendered title should be styled: catppuccin=%q dracula=%q", catppuccin, dracula)
	}
	if catppuccin == dracula {
		t.Fatalf("theme switch should change rendered ANSI color, got %q", catppuccin)
	}
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

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
	if model.settingsForm == nil {
		t.Fatal("expected settings huh form")
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
	model.settingsForm.State = huh.StateCompleted
	updated, cmd := model.applySettingsFormState(nil)
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("expected save settings command")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok || len(batch) < 2 {
		t.Fatalf("settings save should batch config save and feed reload, got %T len=%d", msg, len(batch))
	}
	updatedAfterReload, _ := model.Update(batch[1]())
	model = updatedAfterReload.(Model)
	if service.lastLoadView != domain.SourceAll {
		t.Fatalf("settings source visibility change reloaded %s, want all", service.lastLoadView)
	}
	model = runOptionalCmd(t, model, batch[0])
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
	if service.httpCacheTTL != 6*time.Hour {
		t.Fatalf("service http ttl = %s, want 6h", service.httpCacheTTL)
	}
	if model.view != domain.SourceAll {
		t.Fatalf("disabled active source should switch to all, got %s", model.view)
	}
	if model.settingsForm != nil {
		t.Fatal("settings huh form should clear after submit")
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
	model.height = 80

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
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("settings layout missing %q:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "Markdown style") {
		t.Fatalf("settings layout should expose global Theme instead of Markdown style:\n%s", plain)
	}
}

func TestModelSettingsSmallScreenMouseWheelScrollsForm(t *testing.T) {
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
	model.width = 80
	model.height = 12

	model, cmd := updateModelWithKey(t, model, "c")
	if cmd != nil {
		t.Fatal("settings should open without an init command")
	}
	initial := ansi.Strip(model.render())
	if strings.Contains(initial, "Accessible forms") {
		t.Fatalf("small settings viewport should start at the top:\n%s", initial)
	}
	if renderedLineCount(model.render()) > model.height {
		t.Fatalf("settings render height = %d, want <= %d", renderedLineCount(model.render()), model.height)
	}

	for range 20 {
		updated, cmd := model.Update(mouseWheelDown(10, 5))
		model = updated.(Model)
		if cmd != nil {
			t.Fatal("settings wheel scroll should not produce a command")
		}
	}
	scrolled := ansi.Strip(model.render())
	if model.settingsScrollOffset == 0 {
		t.Fatal("settings wheel should advance the viewport offset")
	}
	if !strings.Contains(scrolled, "Accessible forms") {
		t.Fatalf("settings wheel should reveal lower fields:\n%s", scrolled)
	}
	if renderedLineCount(model.render()) > model.height {
		t.Fatalf("settings render height after scroll = %d, want <= %d", renderedLineCount(model.render()), model.height)
	}
}

func TestModelSettingsSmallScreenPageKeysScrollForm(t *testing.T) {
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
	model.width = 80
	model.height = 12

	model, _ = updateModelWithKey(t, model, "c")
	model, _ = updateModelWithKey(t, model, "pgdown")
	if model.settingsScrollOffset == 0 {
		t.Fatal("pgdown should scroll settings on small screens")
	}
	model, _ = updateModelWithKey(t, model, "pgup")
	if model.settingsScrollOffset != 0 {
		t.Fatalf("pgup should scroll settings back to top, got offset %d", model.settingsScrollOffset)
	}
}

func TestModelSettingsFormRejectsEmptySources(t *testing.T) {
	snapshot := tuiSnapshot(false)
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
	})
	model.width = 100
	model.height = 34

	model, cmd := updateModelWithKey(t, model, "c")
	if cmd != nil {
		t.Fatal("settings should open without an init command")
	}
	model.settingsDraft.EnabledSources = nil
	model.settingsForm.State = huh.StateCompleted

	updated, cmd := model.applySettingsFormState(nil)
	model = updated.(Model)
	if cmd != nil {
		t.Fatal("invalid settings should not produce a save command")
	}
	if model.message != "settings invalid" {
		t.Fatalf("message = %q, want settings invalid", model.message)
	}
	if !model.settingsOpen || model.settingsForm == nil {
		t.Fatal("invalid settings should keep the form open")
	}
	if len(service.enabledSources) != 0 {
		t.Fatalf("service should not be updated for invalid settings: %+v", service.enabledSources)
	}
}

func TestModelSettingsFormRejectsInvalidTTL(t *testing.T) {
	snapshot := tuiSnapshot(false)
	model := NewModel(&fakeService{snapshot: snapshot}, snapshot, ModelOptions{
		Config: config.Config{
			Theme:                "catppuccin",
			GlamourStyle:         "dark",
			MarkdownImagePreview: "auto",
			HTTPCacheTTLHours:    6,
			EnabledSources:       []string{"github", "hackernews"},
		},
	})

	model, _ = updateModelWithKey(t, model, "c")
	model.settingsDraft.HTTPCacheTTLHours = "not-a-number"
	model.settingsForm.State = huh.StateCompleted
	updated, cmd := model.applySettingsFormState(nil)
	model = updated.(Model)

	if cmd != nil {
		t.Fatal("invalid ttl should not produce a save command")
	}
	if model.message != "settings invalid" {
		t.Fatalf("message = %q, want settings invalid", model.message)
	}
	if !model.settingsOpen || model.settingsForm == nil {
		t.Fatal("invalid ttl should keep the form open")
	}
}

func TestModelSettingsFormCancelDoesNotSave(t *testing.T) {
	snapshot := tuiSnapshot(false)
	saved := false
	model := NewModel(&fakeService{snapshot: snapshot}, snapshot, ModelOptions{
		Config: config.Config{
			Theme:                "catppuccin",
			GlamourStyle:         "dark",
			MarkdownImagePreview: "auto",
			HTTPCacheTTLHours:    6,
			EnabledSources:       []string{"github", "hackernews"},
		},
		SaveConfig: func(config.Config) error {
			saved = true
			return nil
		},
	})

	model, _ = updateModelWithKey(t, model, "c")
	model.settingsDraft.Theme = "dracula"
	model.settingsForm.State = huh.StateAborted
	updated, cmd := model.applySettingsFormState(nil)
	model = updated.(Model)

	if cmd != nil {
		t.Fatal("cancel should not produce a save command")
	}
	if saved {
		t.Fatal("cancel should not save config")
	}
	if model.settingsOpen || model.settingsForm != nil {
		t.Fatal("cancel should close settings form")
	}
	if model.config.Theme != "catppuccin" {
		t.Fatalf("theme changed on cancel: %q", model.config.Theme)
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

func TestModelSettingsPasswordInputsHideExistingTokens(t *testing.T) {
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
		t.Fatalf("github token should be hidden by huh password input:\n%s", plain)
	}
	if plain := ansi.Strip(model.render()); strings.Contains(plain, "old-ph") {
		t.Fatalf("product hunt token should be hidden by huh password input:\n%s", plain)
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

package app

import (
	"strings"
	"testing"

	"github.com/AIluffy/tildewire/internal/domain"
)

func TestGitHubScopeForSourceViewParsesThreeDimensions(t *testing.T) {
	scope, ok := ScopeForSourceView(domain.SourceGitHub, "trending:monthly:c++:spoken:zh")
	if !ok {
		t.Fatal("expected GitHub scope to parse")
	}
	if scope.Source != domain.SourceGitHub || scope.View != "trending" || scope.Period != "monthly" || scope.Language != "c++" || scope.SpokenLanguageCode != "zh" || scope.Limit != 25 {
		t.Fatalf("scope = %+v, want monthly C++ Chinese GitHub trending", scope)
	}
	if got := sourceViewKey(scope); got != "trending:monthly:c++:spoken:zh" {
		t.Fatalf("source view key = %q, want trending:monthly:c++:spoken:zh", got)
	}

	label := SourceViewLabel(domain.SourceGitHub, "trending:monthly:c++:spoken:zh")
	for _, want := range []string{"GitHub", "C++", "Chinese", "This month"} {
		if !strings.Contains(label, want) {
			t.Fatalf("label %q missing %q", label, want)
		}
	}
}

func TestGitHubScopeForSourceViewDefaultsLegacyAndInvalidPeriod(t *testing.T) {
	tests := []struct {
		name string
		view string
		want domain.FetchScope
	}{
		{
			name: "default",
			view: "",
			want: domain.FetchScope{Source: domain.SourceGitHub, View: "trending", Period: "daily", Limit: 25},
		},
		{
			name: "legacy language",
			view: "trending:daily:go",
			want: domain.FetchScope{Source: domain.SourceGitHub, View: "trending", Period: "daily", Language: "go", Limit: 25},
		},
		{
			name: "spoken only",
			view: "trending:weekly:spoken:en",
			want: domain.FetchScope{Source: domain.SourceGitHub, View: "trending", Period: "weekly", SpokenLanguageCode: "en", Limit: 25},
		},
		{
			name: "invalid period falls back",
			view: "trending:yearly:rust",
			want: domain.FetchScope{Source: domain.SourceGitHub, View: "trending", Period: "daily", Language: "rust", Limit: 25},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ScopeForSourceView(domain.SourceGitHub, tt.view)
			if !ok {
				t.Fatal("expected GitHub scope to parse")
			}
			if !sameFetchScope(got, tt.want) {
				t.Fatalf("scope = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestGitHubScopeCatalogUsesFocusedTrendingOptions(t *testing.T) {
	languages := GitHubScopeOptions(GitHubScopeLanguage)
	if len(languages) > 32 {
		t.Fatalf("language option count = %d, want focused common list", len(languages))
	}
	for _, want := range []GitHubScopeOption{
		{Label: "Any", Value: ""},
		{Label: "C#", Value: "c#"},
		{Label: "C++", Value: "c++"},
		{Label: "Go", Value: "go"},
		{Label: "Python", Value: "python"},
		{Label: "TypeScript", Value: "typescript"},
	} {
		if !hasGitHubScopeOption(languages, want) {
			t.Fatalf("language options missing %+v", want)
		}
	}
	if hasGitHubScopeOption(languages, GitHubScopeOption{Label: "ABAP", Value: "abap"}) {
		t.Fatal("language options should not include uncommon GitHub languages")
	}

	spoken := GitHubScopeOptions(GitHubScopeSpokenLanguage)
	if len(spoken) != 3 {
		t.Fatalf("spoken language option count = %d, want Any, English, Chinese", len(spoken))
	}
	for _, want := range []GitHubScopeOption{
		{Label: "Any", Value: ""},
		{Label: "English", Value: "en"},
		{Label: "Chinese", Value: "zh"},
	} {
		if !hasGitHubScopeOption(spoken, want) {
			t.Fatalf("spoken language options missing %+v", want)
		}
	}
	if hasGitHubScopeOption(spoken, GitHubScopeOption{Label: "Japanese", Value: "ja"}) {
		t.Fatal("spoken language options should only include English and Chinese besides Any")
	}
}

func TestSourceCatalogIncludesLobstersViews(t *testing.T) {
	scopes := SourceViews(domain.SourceLobsters)
	got := make(map[string]bool)
	for _, scope := range scopes {
		got[scope.View] = true
		if scope.Scope.Source != domain.SourceLobsters || scope.Scope.Limit <= 0 {
			t.Fatalf("invalid lobsters scope: %+v", scope)
		}
	}
	for _, view := range []string{"hottest", "newest"} {
		if !got[view] {
			t.Fatalf("missing Lobsters view %q in %+v", view, scopes)
		}
	}
	if DefaultSourceView(domain.SourceLobsters) != "hottest" {
		t.Fatalf("default Lobsters view = %q, want hottest", DefaultSourceView(domain.SourceLobsters))
	}
}

func TestSourceCatalogIncludesProductHuntViews(t *testing.T) {
	if SourceLabel(domain.SourceProductHunt) != "Product Hunt" || SourceBadge(domain.SourceProductHunt) != "PH" {
		t.Fatalf("product hunt labels = %q/%q", SourceLabel(domain.SourceProductHunt), SourceBadge(domain.SourceProductHunt))
	}
	scopes := SourceViews(domain.SourceProductHunt)
	got := make(map[string]bool)
	for _, scope := range scopes {
		got[scope.View] = true
		if scope.Scope.Source != domain.SourceProductHunt || scope.Scope.Limit <= 0 {
			t.Fatalf("invalid Product Hunt scope: %+v", scope)
		}
	}
	for _, view := range []string{"today", "weekly"} {
		if !got[view] {
			t.Fatalf("missing Product Hunt view %q in %+v", view, scopes)
		}
	}
	if DefaultSourceView(domain.SourceProductHunt) != "today" {
		t.Fatalf("default Product Hunt view = %q, want today", DefaultSourceView(domain.SourceProductHunt))
	}
}

func TestSourceCatalogIncludesAILabsViews(t *testing.T) {
	if SourceLabel(domain.SourceAILabs) != "AI Labs" || SourceBadge(domain.SourceAILabs) != "AI" {
		t.Fatalf("AI Labs labels = %q/%q", SourceLabel(domain.SourceAILabs), SourceBadge(domain.SourceAILabs))
	}
	scopes := SourceViews(domain.SourceAILabs)
	got := make(map[string]bool)
	for _, scope := range scopes {
		got[scope.View] = true
		if scope.Scope.Source != domain.SourceAILabs || scope.Scope.Limit <= 0 {
			t.Fatalf("invalid AI Labs scope: %+v", scope)
		}
	}
	for _, view := range []string{"openai", "anthropic", "deepmind", "meta"} {
		if !got[view] {
			t.Fatalf("missing AI Labs view %q in %+v", view, scopes)
		}
	}
	if DefaultSourceView(domain.SourceAILabs) != "openai" {
		t.Fatalf("default AI Labs view = %q, want openai", DefaultSourceView(domain.SourceAILabs))
	}
	label := SourceViewLabel(domain.SourceAILabs, "deepmind")
	if !strings.Contains(label, "DeepMind") {
		t.Fatalf("AI Labs DeepMind label = %q, want DeepMind", label)
	}
}

func hasGitHubScopeOption(options []GitHubScopeOption, want GitHubScopeOption) bool {
	for _, option := range options {
		if option == want {
			return true
		}
	}
	return false
}

func sameFetchScope(got, want domain.FetchScope) bool {
	return got.Source == want.Source &&
		got.View == want.View &&
		got.Period == want.Period &&
		got.Language == want.Language &&
		got.SpokenLanguageCode == want.SpokenLanguageCode &&
		got.Limit == want.Limit
}

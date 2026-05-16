package app

import (
	"strings"

	"github.com/AIluffy/tildewire/internal/domain"
)

// SourceView describes one selectable source-native feed slice.
type SourceView struct {
	View  string
	Label string
	Scope domain.FetchScope
}

// SourceCatalogEntry describes a source exposed to application and TUI views.
type SourceCatalogEntry struct {
	Source     domain.SourceID
	Label      string
	ShortLabel string
	Badge      string
}

// SourceCatalog returns the supported sources in TUI display order.
func SourceCatalog() []SourceCatalogEntry {
	return []SourceCatalogEntry{
		{Source: domain.SourceGitHub, Label: "GitHub", ShortLabel: "GH", Badge: "GH"},
		{Source: domain.SourceHackerNews, Label: "Hacker News", ShortLabel: "HN", Badge: "HN"},
		{Source: domain.SourceHuggingFace, Label: "HF Papers", ShortLabel: "HF", Badge: "HF"},
		{Source: domain.SourceLobsters, Label: "Lobsters", ShortLabel: "LOB", Badge: "LB"},
		{Source: domain.SourceProductHunt, Label: "Product Hunt", ShortLabel: "PH", Badge: "PH"},
	}
}

// SourceIDs returns the supported source ids in display order.
func SourceIDs() []domain.SourceID {
	catalog := SourceCatalog()
	sources := make([]domain.SourceID, 0, len(catalog))
	for _, entry := range catalog {
		sources = append(sources, entry.Source)
	}
	return sources
}

// SourceLabel returns the user-facing source name.
func SourceLabel(source domain.SourceID) string {
	for _, entry := range SourceCatalog() {
		if entry.Source == source {
			return entry.Label
		}
	}
	return string(source)
}

// SourceShortLabel returns the compact source label used in status text.
func SourceShortLabel(source domain.SourceID) string {
	for _, entry := range SourceCatalog() {
		if entry.Source == source {
			return entry.ShortLabel
		}
	}
	return string(source)
}

// SourceBadge returns the compact source badge text.
func SourceBadge(source domain.SourceID) string {
	for _, entry := range SourceCatalog() {
		if entry.Source == source {
			return entry.Badge
		}
	}
	return "--"
}

// SourceViews returns the source-native views exposed to the TUI.
func SourceViews(source domain.SourceID) []SourceView {
	switch source {
	case domain.SourceHackerNews:
		return []SourceView{
			{View: "top", Label: "HN Top", Scope: domain.FetchScope{Source: source, View: "top", Limit: 50}},
			{View: "best", Label: "HN Best", Scope: domain.FetchScope{Source: source, View: "best", Limit: 50}},
			{View: "new", Label: "HN New", Scope: domain.FetchScope{Source: source, View: "new", Limit: 50}},
			{View: "show", Label: "Show HN", Scope: domain.FetchScope{Source: source, View: "show", Limit: 50}},
		}
	case domain.SourceGitHub:
		return []SourceView{
			githubSourceView(domain.FetchScope{Source: source, View: "trending", Period: "daily", Limit: 25}),
			githubSourceView(domain.FetchScope{Source: source, View: "trending", Period: "weekly", Limit: 25}),
			githubSourceView(domain.FetchScope{Source: source, View: "trending", Period: "monthly", Limit: 25}),
			githubSourceView(domain.FetchScope{Source: source, View: "trending", Period: "daily", Language: "go", Limit: 25}),
			githubSourceView(domain.FetchScope{Source: source, View: "trending", Period: "daily", Language: "rust", Limit: 25}),
			githubSourceView(domain.FetchScope{Source: source, View: "trending", Period: "daily", Language: "python", Limit: 25}),
			githubSourceView(domain.FetchScope{Source: source, View: "trending", Period: "daily", Language: "typescript", Limit: 25}),
		}
	case domain.SourceHuggingFace:
		return []SourceView{
			{View: "daily", Label: "HF Daily", Scope: domain.FetchScope{Source: source, View: "daily", Limit: 50}},
		}
	case domain.SourceLobsters:
		return []SourceView{
			{View: "hottest", Label: "Lobsters Hottest", Scope: domain.FetchScope{Source: source, View: "hottest", Limit: 50}},
			{View: "newest", Label: "Lobsters Newest", Scope: domain.FetchScope{Source: source, View: "newest", Limit: 50}},
		}
	case domain.SourceProductHunt:
		return []SourceView{
			{View: "today", Label: "Product Hunt Today", Scope: domain.FetchScope{Source: source, View: "today", Limit: 25}},
			{View: "weekly", Label: "Product Hunt Weekly", Scope: domain.FetchScope{Source: source, View: "weekly", Limit: 25}},
		}
	default:
		return nil
	}
}

// DefaultSourceView returns the primary scope for a source.
func DefaultSourceView(source domain.SourceID) string {
	views := SourceViews(source)
	if len(views) == 0 {
		return ""
	}
	return views[0].View
}

// SourceViewLabel returns the user-facing label for a source view.
func SourceViewLabel(source domain.SourceID, view string) string {
	if source == domain.SourceGitHub {
		if scope, ok := ScopeForSourceView(source, view); ok {
			return githubScopeLabel(scope)
		}
	}
	if sourceView, ok := sourceViewByName(source, view); ok {
		return sourceView.Label
	}
	return view
}

// ScopeForSourceView returns the fetch scope represented by a source view key.
func ScopeForSourceView(source domain.SourceID, view string) (domain.FetchScope, bool) {
	if source == domain.SourceGitHub {
		return githubScopeForView(view), true
	}
	sourceView, ok := sourceViewByName(source, view)
	if !ok {
		return domain.FetchScope{}, false
	}
	return sourceView.Scope, true
}

func sourceViewByName(source domain.SourceID, view string) (SourceView, bool) {
	view = strings.ToLower(strings.TrimSpace(view))
	if source == domain.SourceGitHub {
		scope := githubScopeForView(view)
		return githubSourceView(scope), true
	}
	for _, sourceView := range SourceViews(source) {
		if sourceView.View == view {
			return sourceView, true
		}
	}
	return SourceView{}, false
}

func sourceViewKey(scope domain.FetchScope) string {
	if scope.Source == domain.SourceGitHub || scope.View == "trending" {
		return GitHubScopeView(scope)
	}
	view := strings.ToLower(strings.TrimSpace(scope.View))
	if view == "" {
		return DefaultSourceView(scope.Source)
	}
	return view
}

// GitHubScopeView returns the stable source view key for a GitHub Trending scope.
func GitHubScopeView(scope domain.FetchScope) string {
	scope = normalizeGitHubScope(scope)
	parts := []string{"trending", scope.Period}
	if scope.Language != "" {
		parts = append(parts, scope.Language)
	}
	if scope.SpokenLanguageCode != "" {
		parts = append(parts, "spoken", scope.SpokenLanguageCode)
	}
	return strings.Join(parts, ":")
}

func githubSourceView(scope domain.FetchScope) SourceView {
	scope = normalizeGitHubScope(scope)
	return SourceView{
		View:  GitHubScopeView(scope),
		Label: githubScopeLabel(scope),
		Scope: scope,
	}
}

func githubScopeForView(view string) domain.FetchScope {
	scope := domain.FetchScope{Source: domain.SourceGitHub, View: "trending", Period: "daily", Limit: 25}
	parts := strings.Split(strings.ToLower(strings.TrimSpace(view)), ":")
	if len(parts) == 0 || parts[0] == "" {
		return scope
	}
	if parts[0] != "trending" {
		return scope
	}
	if len(parts) > 1 {
		scope.Period = normalizeGitHubPeriod(parts[1])
	}
	for idx := 2; idx < len(parts); idx++ {
		if parts[idx] == "spoken" && idx+1 < len(parts) {
			scope.SpokenLanguageCode = normalizeGitHubSpokenLanguageCode(parts[idx+1])
			idx++
			continue
		}
		if scope.Language == "" {
			scope.Language = normalizeGitHubLanguage(parts[idx])
		}
	}
	return normalizeGitHubScope(scope)
}

func normalizeGitHubScope(scope domain.FetchScope) domain.FetchScope {
	scope.Source = domain.SourceGitHub
	scope.View = "trending"
	scope.Period = normalizeGitHubPeriod(scope.Period)
	scope.Language = normalizeGitHubLanguage(scope.Language)
	scope.SpokenLanguageCode = normalizeGitHubSpokenLanguageCode(scope.SpokenLanguageCode)
	if scope.Limit <= 0 {
		scope.Limit = 25
	}
	return scope
}

func githubScopeLabel(scope domain.FetchScope) string {
	scope = normalizeGitHubScope(scope)
	parts := []string{"GitHub"}
	if scope.Language != "" {
		parts = append(parts, GitHubScopeOptionLabel(GitHubScopeLanguage, scope.Language))
	}
	if scope.SpokenLanguageCode != "" {
		parts = append(parts, GitHubScopeOptionLabel(GitHubScopeSpokenLanguage, scope.SpokenLanguageCode))
	}
	parts = append(parts, GitHubScopeOptionLabel(GitHubScopeDateRange, scope.Period))
	return strings.Join(parts, " ")
}

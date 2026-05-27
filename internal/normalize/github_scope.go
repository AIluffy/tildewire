package normalize

import (
	"strings"

	"github.com/AIluffy/tildewire/internal/domain"
)

// NormalizeGitHubScope returns the canonical GitHub Trending fetch scope.
func NormalizeGitHubScope(scope domain.FetchScope) domain.FetchScope {
	scope.Source = domain.SourceGitHub
	scope.View = "trending"
	scope.Period = NormalizeGitHubPeriod(scope.Period)
	scope.Language = GitHubLanguageSlug(scope.Language)
	scope.SpokenLanguageCode = GitHubSpokenLanguageCode(scope.SpokenLanguageCode)
	if scope.Limit <= 0 {
		scope.Limit = 25
	}
	return scope
}

// NormalizeGitHubPeriod returns a GitHub Trending period supported by the site.
func NormalizeGitHubPeriod(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "weekly", "week", "this-week":
		return "weekly"
	case "monthly", "month", "this-month":
		return "monthly"
	default:
		return "daily"
	}
}

// GitHubSpokenLanguageCode returns the query code for a GitHub Trending spoken-language value.
func GitHubSpokenLanguageCode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "english":
		return "en"
	case "chinese":
		return "zh"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

// GitHubLanguageSlug returns the GitHub Trending URL slug for a language label.
func GitHubLanguageSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, " ", "-")
	return value
}

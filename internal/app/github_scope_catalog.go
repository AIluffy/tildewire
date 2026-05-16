package app

import "strings"

// GitHubScopeDimension identifies one GitHub Trending selector.
type GitHubScopeDimension string

const (
	// GitHubScopeSpokenLanguage selects repository description language.
	GitHubScopeSpokenLanguage GitHubScopeDimension = "spoken_language"
	// GitHubScopeLanguage selects repository programming language.
	GitHubScopeLanguage GitHubScopeDimension = "language"
	// GitHubScopeDateRange selects the Trending time window.
	GitHubScopeDateRange GitHubScopeDimension = "date_range"
)

// GitHubScopeOption is one selectable GitHub Trending scope value.
type GitHubScopeOption struct {
	Label string
	Value string
}

// GitHubScopeOptions returns GitHub Trending selector options.
func GitHubScopeOptions(dimension GitHubScopeDimension) []GitHubScopeOption {
	var source []GitHubScopeOption
	switch dimension {
	case GitHubScopeSpokenLanguage:
		source = githubSpokenLanguageOptions
	case GitHubScopeLanguage:
		source = githubLanguageOptions
	case GitHubScopeDateRange:
		source = githubDateRangeOptions
	default:
		source = nil
	}
	options := make([]GitHubScopeOption, len(source))
	copy(options, source)
	return options
}

// GitHubScopeOptionLabel returns the display label for a dimension value.
func GitHubScopeOptionLabel(dimension GitHubScopeDimension, value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, option := range GitHubScopeOptions(dimension) {
		if option.Value == value {
			return option.Label
		}
	}
	if value == "" {
		return "Any"
	}
	return titleScopeValue(value)
}

func normalizeGitHubLanguage(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, option := range githubLanguageOptions {
		if value == strings.ToLower(option.Label) || value == option.Value {
			return option.Value
		}
	}
	return githubLanguageValue(value)
}

func normalizeGitHubSpokenLanguageCode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, option := range githubSpokenLanguageOptions {
		if value == strings.ToLower(option.Label) || value == option.Value {
			return option.Value
		}
	}
	return value
}

func normalizeGitHubPeriod(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "weekly", "week", "this-week":
		return "weekly"
	case "monthly", "month", "this-month":
		return "monthly"
	default:
		return "daily"
	}
}

func githubLanguageValue(label string) string {
	label = strings.ToLower(strings.TrimSpace(label))
	label = strings.ReplaceAll(label, " ", "-")
	return label
}

func githubLanguageOption(label string) GitHubScopeOption {
	return GitHubScopeOption{Label: label, Value: githubLanguageValue(label)}
}

func titleScopeValue(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return ""
	case "c#":
		return "C#"
	case "c++":
		return "C++"
	case "go":
		return "Go"
	case "objective-c":
		return "Objective-C"
	case "typescript":
		return "TypeScript"
	default:
		parts := strings.Split(strings.ReplaceAll(strings.TrimSpace(value), "-", " "), " ")
		for idx, part := range parts {
			if part == "" {
				continue
			}
			parts[idx] = strings.ToUpper(part[:1]) + part[1:]
		}
		return strings.Join(parts, " ")
	}
}

var githubDateRangeOptions = []GitHubScopeOption{
	{Label: "Today", Value: "daily"},
	{Label: "This week", Value: "weekly"},
	{Label: "This month", Value: "monthly"},
}

var githubSpokenLanguageOptions = []GitHubScopeOption{
	{Label: "Any", Value: ""},
	{Label: "English", Value: "en"},
	{Label: "Chinese", Value: "zh"},
}

var githubLanguageOptions = []GitHubScopeOption{
	{Label: "Any", Value: ""},
	githubLanguageOption("Go"),
	githubLanguageOption("Rust"),
	githubLanguageOption("Python"),
	githubLanguageOption("TypeScript"),
	githubLanguageOption("JavaScript"),
	githubLanguageOption("Java"),
	githubLanguageOption("Kotlin"),
	githubLanguageOption("Swift"),
	githubLanguageOption("C"),
	githubLanguageOption("C++"),
	githubLanguageOption("C#"),
	githubLanguageOption("Objective-C"),
	githubLanguageOption("Shell"),
	githubLanguageOption("HTML"),
	githubLanguageOption("CSS"),
	githubLanguageOption("PHP"),
	githubLanguageOption("Ruby"),
	githubLanguageOption("Dart"),
	githubLanguageOption("Scala"),
	githubLanguageOption("Elixir"),
	githubLanguageOption("Haskell"),
	githubLanguageOption("Lua"),
	githubLanguageOption("R"),
	githubLanguageOption("Julia"),
	githubLanguageOption("SQL"),
	githubLanguageOption("Vue"),
	githubLanguageOption("Svelte"),
}

package domain

import "time"

// RuleEffect identifies how a personalization rule changes feed behavior.
type RuleEffect string

const (
	// RuleEffectBoost raises matching items in All view.
	RuleEffectBoost RuleEffect = "boost"
	// RuleEffectMute lowers matching items in All view.
	RuleEffectMute RuleEffect = "mute"
	// RuleEffectHide filters matching items unless IncludeHidden is active.
	RuleEffectHide RuleEffect = "hide"
)

// RuleTarget identifies the item attribute matched by a personalization rule.
type RuleTarget string

const (
	// RuleTargetKeyword matches title, subtitle, summary, metadata, tags, repo, author, and organization text.
	RuleTargetKeyword RuleTarget = "keyword"
	// RuleTargetLanguage matches item language.
	RuleTargetLanguage RuleTarget = "language"
	// RuleTargetTag matches item tags.
	RuleTargetTag RuleTarget = "tag"
	// RuleTargetDomain matches item URL host.
	RuleTargetDomain RuleTarget = "domain"
	// RuleTargetSource matches item source id.
	RuleTargetSource RuleTarget = "source"
	// RuleTargetRepo matches repository refs.
	RuleTargetRepo RuleTarget = "repo"
	// RuleTargetAuthor matches item author or organization.
	RuleTargetAuthor RuleTarget = "author"
)

// PersonalizationRule is a durable user rule for reducing feed noise.
type PersonalizationRule struct {
	ID        int64
	Effect    RuleEffect
	Target    RuleTarget
	Value     string
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// PreferenceProfile contains lightweight implicit preference signals.
type PreferenceProfile struct {
	SavedTags       map[string]int
	SavedLanguages  map[string]int
	SavedRepos      map[string]int
	SavedAuthors    map[string]int
	SavedSources    map[string]int
	HiddenTags      map[string]int
	HiddenLanguages map[string]int
	HiddenRepos     map[string]int
	HiddenAuthors   map[string]int
	HiddenSources   map[string]int
}

// DedupeCandidate is a non-destructive fuzzy duplicate suggestion.
type DedupeCandidate struct {
	Key       string
	ItemA     FeedItem
	ItemB     FeedItem
	Score     float64
	Distance  int
	Reason    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

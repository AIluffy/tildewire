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

// ItemEventType identifies explicit local user interactions that can train Recommend.
type ItemEventType string

const (
	ItemEventSave         ItemEventType = "save"
	ItemEventUnsave       ItemEventType = "unsave"
	ItemEventHide         ItemEventType = "hide"
	ItemEventRestore      ItemEventType = "restore"
	ItemEventRead         ItemEventType = "read"
	ItemEventUnread       ItemEventType = "unread"
	ItemEventDetailOpen   ItemEventType = "detail_open"
	ItemEventOpenURL      ItemEventType = "open_url"
	ItemEventOpenSource   ItemEventType = "open_source"
	ItemEventCopyURL      ItemEventType = "copy_url"
	ItemEventCopyMarkdown ItemEventType = "copy_markdown"
)

// ItemEvent is a local, durable recommendation-training interaction.
type ItemEvent struct {
	ID         int64
	ItemID     string
	EventType  ItemEventType
	Source     SourceID
	View       SourceID
	OccurredAt time.Time
}

// RecommendationTerm is a normalized item feature used for local recommendation matching.
type RecommendationTerm struct {
	ItemID string
	Kind   string
	Value  string
	Weight float64
}

// RecommendationProfile contains decayed signed preference scores keyed by term.
type RecommendationProfile struct {
	Terms map[string]RecommendationProfileTerm
}

// RecommendationProfileTerm stores positive and negative preference evidence for one term.
type RecommendationProfileTerm struct {
	Kind     string
	Value    string
	Score    float64
	Positive float64
	Negative float64
	Reasons  []RecommendationReason
}

// RecommendationReason is a short explainable reason attached to a recommended entry.
type RecommendationReason struct {
	Kind   string  `json:"kind"`
	Value  string  `json:"value"`
	Label  string  `json:"label"`
	Weight float64 `json:"weight"`
}

// RecommendationScore is a durable score for the virtual Recommend view.
type RecommendationScore struct {
	ItemID        string
	Score         float64
	InterestScore float64
	HotScore      float64
	Reasons       []RecommendationReason
	ComputedAt    time.Time
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

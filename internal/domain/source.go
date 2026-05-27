package domain

import "time"

// SourceID identifies a content source.
type SourceID string

const (
	// SourceAll is the merged feed view.
	SourceAll SourceID = "all"
	// SourceRecommend is a virtual personalized recommendation view.
	SourceRecommend SourceID = "recommend"
	// SourceHackerNews identifies Hacker News.
	SourceHackerNews SourceID = "hackernews"
	// SourceGitHub identifies GitHub Trending.
	SourceGitHub SourceID = "github"
	// SourceAILabs identifies official AI lab news feeds.
	SourceAILabs SourceID = "ailabs"
	// SourceHuggingFace identifies Hugging Face Papers.
	SourceHuggingFace SourceID = "huggingface"
	// SourceLobsters identifies Lobsters.
	SourceLobsters SourceID = "lobsters"
	// SourceProductHunt identifies Product Hunt.
	SourceProductHunt SourceID = "producthunt"
)

// SourceStatus describes the current health of a source.
type SourceStatus string

const (
	SourceStatusUnknown      SourceStatus = "UNKNOWN"
	SourceStatusOK           SourceStatus = "OK"
	SourceStatusStale        SourceStatus = "STALE"
	SourceStatusRefreshing   SourceStatus = "REFRESHING"
	SourceStatusRateLimited  SourceStatus = "RATE_LIMITED"
	SourceStatusAuthRequired SourceStatus = "AUTH_REQUIRED"
	SourceStatusParserBroken SourceStatus = "PARSER_BROKEN"
	SourceStatusNetworkError SourceStatus = "NETWORK_ERROR"
	SourceStatusDisabled     SourceStatus = "DISABLED"
)

// FetchScope identifies a source-specific feed slice.
type FetchScope struct {
	Source             SourceID
	View               string
	Period             string
	Language           string
	SpokenLanguageCode string
	Topic              string
	Limit              int
	ForceRefresh       bool
	Params             map[string]string
}

// CachePolicy controls how long raw responses can be reused.
type CachePolicy struct {
	TTL time.Duration
}

// FetchResult contains a raw source response suitable for normalization.
type FetchResult struct {
	Source      SourceID
	Scope       FetchScope
	StatusCode  int
	Body        []byte
	FetchedAt   time.Time
	FromCache   bool
	Stale       bool
	StaleReason string
}

// SourceHealth is persisted source status shown by the TUI.
type SourceHealth struct {
	Source        SourceID
	Name          string
	Status        SourceStatus
	LastFetchAt   *time.Time
	LastSuccessAt *time.Time
	LastError     string
}

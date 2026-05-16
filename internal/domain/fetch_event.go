package domain

import (
	"time"
)

// FetchEvent records one source refresh attempt for observability.
type FetchEvent struct {
	Source      SourceID
	SourceView  string
	Status      SourceStatus
	StartedAt   time.Time
	FinishedAt  time.Time
	Duration    time.Duration
	ItemCount   int
	Stale       bool
	StaleReason string
	Error       string
}

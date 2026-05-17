package httpcache

import (
	"context"
	"time"
)

// Entry is a raw HTTP response stored for stale-while-revalidate.
type Entry struct {
	RequestKey   string
	Source       string
	Method       string
	URL          string
	StatusCode   int
	HeadersJSON  string
	Body         []byte
	FetchedAt    time.Time
	ExpiresAt    time.Time
	ETag         string
	LastModified string
}

// Store persists and loads raw HTTP responses.
type Store interface {
	GetHTTPCache(context.Context, string) (Entry, bool, error)
	PutHTTPCache(context.Context, Entry) error
}

// CooldownStore persists source-specific rate-limit cooldowns.
type CooldownStore interface {
	RateLimitCooldown(context.Context, string, string) (time.Time, bool, error)
	SetRateLimitCooldown(context.Context, string, string, time.Time) error
}

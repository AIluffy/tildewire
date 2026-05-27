package httpx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"github.com/AIluffy/tildewire/internal/httpcache"
)

func TestDoGETWritesCacheOnSuccess(t *testing.T) {
	cache := newMemoryCache()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("ETag", "abc")
		_, _ = w.Write([]byte("live"))
	}))
	defer server.Close()

	client := New(time.Second, cache)
	client.client.RetryMax = 0
	resp, err := client.DoGET(context.Background(), GetOptions{
		Source: "hackernews",
		URL:    server.URL,
		TTL:    time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Body) != "live" || resp.FromCache || resp.Stale {
		t.Fatalf("unexpected response: %+v", resp)
	}
	entry, ok, err := cache.GetHTTPCache(context.Background(), RequestKey(http.MethodGet, server.URL))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || string(entry.Body) != "live" || entry.ETag != "abc" {
		t.Fatalf("cache not written correctly: ok=%v entry=%+v", ok, entry)
	}
}

func TestDoGETUsesConfiguredCacheTTL(t *testing.T) {
	cache := newMemoryCache()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("live"))
	}))
	defer server.Close()

	client := New(time.Second, cache)
	client.SetCacheTTL(6 * time.Hour)

	resp, err := client.DoGET(context.Background(), GetOptions{
		Source: "test",
		URL:    server.URL,
		TTL:    time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := resp.ExpiresAt.Sub(resp.FetchedAt)
	if got != 6*time.Hour {
		t.Fatalf("cache ttl = %s, want 6h", got)
	}
}

func TestDoGETAppliesConfiguredCacheTTLToExistingCache(t *testing.T) {
	cache := newMemoryCache()
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte("live"))
	}))
	defer server.Close()

	fetchedAt := time.Now().Add(-time.Hour)
	cache.put(server.URL, "cached", fetchedAt, fetchedAt.Add(time.Minute))
	client := New(time.Second, cache)
	client.SetCacheTTL(6 * time.Hour)

	resp, err := client.DoGET(context.Background(), GetOptions{
		Source: "test",
		URL:    server.URL,
		TTL:    time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.FromCache || string(resp.Body) != "cached" || hits != 0 {
		t.Fatalf("expected configured ttl to keep existing cache fresh: resp=%+v hits=%d", resp, hits)
	}
	if !resp.ExpiresAt.Equal(fetchedAt.Add(6 * time.Hour)) {
		t.Fatalf("cache expires at %s, want %s", resp.ExpiresAt, fetchedAt.Add(6*time.Hour))
	}
}

func TestDoGETUsesFreshCache(t *testing.T) {
	cache := newMemoryCache()
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write([]byte("live"))
	}))
	defer server.Close()
	cache.put(server.URL, "cached", time.Now().Add(-time.Minute), time.Now().Add(time.Minute))

	client := New(time.Second, cache)
	client.client.RetryMax = 0
	resp, err := client.DoGET(context.Background(), GetOptions{
		Source: "hackernews",
		URL:    server.URL,
		TTL:    time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Body) != "cached" || !resp.FromCache || resp.Stale {
		t.Fatalf("expected fresh cache response: %+v", resp)
	}
	if hits != 0 {
		t.Fatalf("server was hit %d times", hits)
	}
}

func TestDoGETForceRefreshHitsNetwork(t *testing.T) {
	cache := newMemoryCache()
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write([]byte("live"))
	}))
	defer server.Close()
	cache.put(server.URL, "cached", time.Now().Add(-time.Minute), time.Now().Add(time.Minute))

	client := New(time.Second, cache)
	client.client.RetryMax = 0
	resp, err := client.DoGET(context.Background(), GetOptions{
		Source:       "hackernews",
		URL:          server.URL,
		TTL:          time.Minute,
		ForceRefresh: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Body) != "live" || resp.FromCache {
		t.Fatalf("expected live response: %+v", resp)
	}
	if hits != 1 {
		t.Fatalf("server hits = %d, want 1", hits)
	}
}

func TestDoGETSendsAuthorization(t *testing.T) {
	cache := newMemoryCache()
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("live"))
	}))
	defer server.Close()

	client := New(time.Second, cache)
	client.client.RetryMax = 0
	_, err := client.DoGET(context.Background(), GetOptions{
		Source:      "github",
		URL:         server.URL,
		TTL:         time.Minute,
		BearerToken: "secret-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer secret-token" {
		t.Fatalf("authorization header = %q, want bearer token", gotAuth)
	}
}

func TestDoGETUsesRateLimitBucket(t *testing.T) {
	cache := newMemoryCache()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("live"))
	}))
	defer server.Close()

	client := New(time.Second, cache)
	client.client.RetryMax = 0
	client.limiters["github"] = rate.NewLimiter(rate.Every(time.Minute), 1)
	client.limiters["github_api"] = rate.NewLimiter(rate.Every(time.Millisecond), 1)
	if !client.limiters["github"].Allow() {
		t.Fatal("expected to drain initial github limiter token")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	resp, err := client.DoGET(ctx, GetOptions{
		Source:          "github",
		RateLimitBucket: "github_api",
		URL:             server.URL,
		TTL:             time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Body) != "live" || resp.FromCache {
		t.Fatalf("unexpected response through api bucket: %+v", resp)
	}
}

func TestDoGETTreatsLocalLimiterWaitAsRateLimitedStaleCache(t *testing.T) {
	cache := newMemoryCache()
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write([]byte("live"))
	}))
	defer server.Close()
	cache.put(server.URL, "stale", time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour))

	client := New(time.Second, cache)
	client.client.RetryMax = 0
	client.limiters["github"] = rate.NewLimiter(rate.Every(time.Minute), 1)
	if !client.limiters["github"].Allow() {
		t.Fatal("expected to drain initial github limiter token")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	resp, err := client.DoGET(ctx, GetOptions{
		Source:       "github",
		URL:          server.URL,
		TTL:          time.Minute,
		ForceRefresh: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Body) != "stale" || !resp.FromCache || !resp.Stale {
		t.Fatalf("expected stale cache when local limiter is exhausted: %+v", resp)
	}
	if resp.StaleReason != StaleReasonRateLimited {
		t.Fatalf("stale reason = %q, want %q", resp.StaleReason, StaleReasonRateLimited)
	}
	if hits != 0 {
		t.Fatalf("server hits = %d, want 0 while local limiter is exhausted", hits)
	}
}

func TestDoGETSendsConditionalHeadersAndExtendsCacheOnNotModified(t *testing.T) {
	cache := newMemoryCache()
	fetchedAt := time.Now().Add(-2 * time.Hour)
	expiresAt := time.Now().Add(-time.Hour)
	var gotETag, gotModified string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotETag = r.Header.Get("If-None-Match")
		gotModified = r.Header.Get("If-Modified-Since")
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()
	cache.entries[RequestKey(http.MethodGet, server.URL)] = httpcache.Entry{
		RequestKey:   RequestKey(http.MethodGet, server.URL),
		Source:       "hackernews",
		Method:       http.MethodGet,
		URL:          server.URL,
		StatusCode:   http.StatusOK,
		Body:         []byte("cached"),
		FetchedAt:    fetchedAt,
		ExpiresAt:    expiresAt,
		ETag:         `"abc"`,
		LastModified: "Sun, 10 May 2026 10:00:00 GMT",
	}

	client := New(time.Second, cache)
	client.client.RetryMax = 0
	resp, err := client.DoGET(context.Background(), GetOptions{
		Source: "hackernews",
		URL:    server.URL,
		TTL:    time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotETag != `"abc"` || gotModified != "Sun, 10 May 2026 10:00:00 GMT" {
		t.Fatalf("conditional headers = %q/%q", gotETag, gotModified)
	}
	if string(resp.Body) != "cached" || !resp.FromCache || resp.Stale || resp.StatusCode != http.StatusNotModified {
		t.Fatalf("expected refreshed cache response: %+v", resp)
	}
	entry, ok, err := cache.GetHTTPCache(context.Background(), RequestKey(http.MethodGet, server.URL))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected cache entry")
	}
	if !entry.ExpiresAt.After(time.Now()) || !entry.FetchedAt.After(fetchedAt) {
		t.Fatalf("cache was not extended: fetched=%s expires=%s", entry.FetchedAt, entry.ExpiresAt)
	}
}

func TestDoGETFallsBackToStaleCache(t *testing.T) {
	cache := newMemoryCache()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	defer server.Close()
	cache.put(server.URL, "stale", time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour))

	client := New(time.Second, cache)
	client.client.RetryMax = 0
	resp, err := client.DoGET(context.Background(), GetOptions{
		Source: "hackernews",
		URL:    server.URL,
		TTL:    time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Body) != "stale" || !resp.FromCache || !resp.Stale {
		t.Fatalf("expected stale cache response: %+v", resp)
	}
	if resp.StaleReason != StaleReasonNetworkError {
		t.Fatalf("stale reason = %q, want %q", resp.StaleReason, StaleReasonNetworkError)
	}
}

func TestDoGETReturnsForbiddenErrorWithoutCacheFallback(t *testing.T) {
	cache := newMemoryCache()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer server.Close()
	cache.put(server.URL, "stale", time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour))

	client := New(time.Second, cache)
	client.client.RetryMax = 0
	resp, err := client.DoGET(context.Background(), GetOptions{
		Source: "github",
		URL:    server.URL,
		TTL:    time.Minute,
	})
	if err == nil {
		t.Fatal("expected forbidden error")
	}
	if resp.FromCache || resp.Stale {
		t.Fatalf("403 should not be hidden by stale cache: %+v", resp)
	}
	var statusErr *HTTPStatusError
	if !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusForbidden {
		t.Fatalf("error = %v, want HTTPStatusError 403", err)
	}
	if len(cache.cooldowns) != 0 {
		t.Fatalf("403 without rate-limit headers should not store cooldown: %+v", cache.cooldowns)
	}
}

func TestDoGETReturnsErrorWithoutCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := New(time.Second, newMemoryCache())
	client.client.RetryMax = 0
	_, err := client.DoGET(context.Background(), GetOptions{
		Source: "hackernews",
		URL:    server.URL,
		TTL:    time.Minute,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDoGETStoresCooldownAndSkipsNetworkWhileActive(t *testing.T) {
	cache := newMemoryCache()
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.Header().Set("Retry-After", "120")
		http.Error(w, "slow down", http.StatusTooManyRequests)
	}))
	defer server.Close()
	cache.put(server.URL, "stale", time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour))

	client := New(time.Second, cache)
	client.client.RetryMax = 0
	resp, err := client.DoGET(context.Background(), GetOptions{
		Source:       "hackernews",
		URL:          server.URL,
		TTL:          time.Minute,
		ForceRefresh: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Body) != "stale" || !resp.FromCache || !resp.Stale {
		t.Fatalf("expected stale cache after 429: %+v", resp)
	}
	if resp.StaleReason != StaleReasonRateLimited {
		t.Fatalf("stale reason = %q, want %q", resp.StaleReason, StaleReasonRateLimited)
	}
	cooldown, ok, err := cache.RateLimitCooldown(context.Background(), "hackernews", RequestKey(http.MethodGet, server.URL))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || cooldown.Before(time.Now().Add(90*time.Second)) {
		t.Fatalf("cooldown was not persisted: %s/%v", cooldown, ok)
	}

	resp, err = client.DoGET(context.Background(), GetOptions{
		Source:       "hackernews",
		URL:          server.URL,
		TTL:          time.Minute,
		ForceRefresh: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Body) != "stale" || !resp.FromCache || !resp.Stale {
		t.Fatalf("expected stale cache during cooldown: %+v", resp)
	}
	if resp.StaleReason != StaleReasonRateLimited {
		t.Fatalf("cooldown stale reason = %q, want %q", resp.StaleReason, StaleReasonRateLimited)
	}
	if hits != 1 {
		t.Fatalf("server hits = %d, want 1", hits)
	}
}

func TestDoGETDoesNotRetryRateLimitResponses(t *testing.T) {
	cache := newMemoryCache()
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		http.Error(w, "slow down", http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := New(time.Second, cache)
	_, err := client.DoGET(context.Background(), GetOptions{
		Source: "github",
		URL:    server.URL,
		TTL:    time.Minute,
	})
	if err == nil {
		t.Fatal("expected rate limit error")
	}
	if hits != 1 {
		t.Fatalf("server hits = %d, want one request without retrying 429", hits)
	}
}

func TestDoGETStoresCooldownFromRateLimitResetHeader(t *testing.T) {
	cache := newMemoryCache()
	reset := time.Now().UTC().Add(45 * time.Minute).Truncate(time.Second)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", reset.Unix()))
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	defer server.Close()

	client := New(time.Second, cache)
	_, err := client.DoGET(context.Background(), GetOptions{
		Source: "github",
		URL:    server.URL,
		TTL:    time.Minute,
	})
	if err == nil {
		t.Fatal("expected rate limit error")
	}
	cooldown, ok, err := cache.RateLimitCooldown(context.Background(), "github", RequestKey(http.MethodGet, server.URL))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !cooldown.Equal(reset) {
		t.Fatalf("cooldown = %s/%v, want %s/true", cooldown, ok, reset)
	}
}

func TestDoGETReturnsErrorWhenBodyExceedsLimitWithoutCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("12345"))
	}))
	defer server.Close()

	client := New(time.Second, newMemoryCache())
	client.client.RetryMax = 0
	_, err := client.DoGET(context.Background(), GetOptions{
		Source:       "hackernews",
		URL:          server.URL,
		TTL:          time.Minute,
		MaxBodyBytes: 4,
	})
	if err == nil {
		t.Fatal("expected oversized body error")
	}
}

func TestDoGETFallsBackToStaleCacheWhenBodyExceedsLimit(t *testing.T) {
	cache := newMemoryCache()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("12345"))
	}))
	defer server.Close()
	cache.put(server.URL, "stale", time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour))

	client := New(time.Second, cache)
	client.client.RetryMax = 0
	resp, err := client.DoGET(context.Background(), GetOptions{
		Source:       "hackernews",
		URL:          server.URL,
		TTL:          time.Minute,
		ForceRefresh: true,
		MaxBodyBytes: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Body) != "stale" || !resp.FromCache || !resp.Stale {
		t.Fatalf("expected stale cache after oversized body: %+v", resp)
	}
	if resp.StaleReason != StaleReasonNetworkError {
		t.Fatalf("stale reason = %q, want %q", resp.StaleReason, StaleReasonNetworkError)
	}
}

func TestDoPOSTJSONCachesByBodyAndSendsAuthorization(t *testing.T) {
	cache := newMemoryCache()
	hits := 0
	var gotAuth, gotContentType, firstBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if hits == 1 {
			firstBody = string(body)
		}
		_, _ = w.Write([]byte(`{"data":{"ok":true}}`))
	}))
	defer server.Close()

	client := New(time.Second, cache)
	client.client.RetryMax = 0
	bodyOne := []byte(`{"query":"query one"}`)
	bodyTwo := []byte(`{"query":"query two"}`)
	first, err := client.DoPOSTJSON(context.Background(), PostOptions{
		Source:      "producthunt",
		URL:         server.URL,
		Body:        bodyOne,
		BearerToken: "secret-token",
		TTL:         time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.DoPOSTJSON(context.Background(), PostOptions{
		Source:      "producthunt",
		URL:         server.URL,
		Body:        bodyOne,
		BearerToken: "secret-token",
		TTL:         time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	third, err := client.DoPOSTJSON(context.Background(), PostOptions{
		Source:      "producthunt",
		URL:         server.URL,
		Body:        bodyTwo,
		BearerToken: "secret-token",
		TTL:         time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.FromCache || !second.FromCache || third.FromCache || hits != 2 {
		t.Fatalf("post cache behavior mismatch first=%+v second=%+v third=%+v hits=%d", first, second, third, hits)
	}
	if gotAuth != "Bearer secret-token" || !strings.HasPrefix(gotContentType, "application/json") {
		t.Fatalf("headers auth/content-type = %q/%q", gotAuth, gotContentType)
	}
	if firstBody != string(bodyOne) {
		t.Fatalf("body = %q, want %q", firstBody, bodyOne)
	}
	for _, entry := range cache.entries {
		if strings.Contains(entry.HeadersJSON, "secret-token") {
			t.Fatalf("authorization token leaked into cached response headers: %+v", entry)
		}
	}
}

func TestDoPOSTJSONFallsBackToStaleCache(t *testing.T) {
	cache := newMemoryCache()
	body := []byte(`{"query":"query ProductHunt"}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	defer server.Close()
	key := requestKeyWithBody(http.MethodPost, server.URL, body)
	cache.entries[key] = httpcache.Entry{
		RequestKey: key,
		Source:     "producthunt",
		Method:     http.MethodPost,
		URL:        server.URL,
		StatusCode: http.StatusOK,
		Body:       []byte(`{"data":{"posts":{"nodes":[]}}}`),
		FetchedAt:  time.Now().Add(-2 * time.Hour),
		ExpiresAt:  time.Now().Add(-time.Hour),
	}

	client := New(time.Second, cache)
	client.client.RetryMax = 0
	resp, err := client.DoPOSTJSON(context.Background(), PostOptions{
		Source:      "producthunt",
		URL:         server.URL,
		Body:        body,
		BearerToken: "secret-token",
		TTL:         time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.FromCache || !resp.Stale || resp.StaleReason != StaleReasonNetworkError {
		t.Fatalf("expected stale post cache after error: %+v", resp)
	}
}

func TestDoPOSTJSONStoresCooldownFromRateLimitReset(t *testing.T) {
	cache := newMemoryCache()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Rate-Limit-Reset", "120")
		http.Error(w, "slow down", http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := New(time.Second, cache)
	client.client.RetryMax = 0
	_, err := client.DoPOSTJSON(context.Background(), PostOptions{
		Source:      "producthunt",
		URL:         server.URL,
		Body:        []byte(`{"query":"query ProductHunt"}`),
		BearerToken: "secret-token",
		TTL:         time.Minute,
	})
	if err == nil {
		t.Fatal("expected 429 error without cache")
	}
	if len(cache.cooldowns) != 1 {
		t.Fatalf("cooldowns = %+v, want one producthunt cooldown", cache.cooldowns)
	}
	for _, until := range cache.cooldowns {
		if until.Before(time.Now().Add(90 * time.Second)) {
			t.Fatalf("cooldown = %s, want at least 90 seconds from now", until)
		}
	}
}

type memoryCache struct {
	entries   map[string]httpcache.Entry
	cooldowns map[string]time.Time
}

func newMemoryCache() *memoryCache {
	return &memoryCache{
		entries:   make(map[string]httpcache.Entry),
		cooldowns: make(map[string]time.Time),
	}
}

func (m *memoryCache) GetHTTPCache(_ context.Context, key string) (httpcache.Entry, bool, error) {
	entry, ok := m.entries[key]
	return entry, ok, nil
}

func (m *memoryCache) PutHTTPCache(_ context.Context, entry httpcache.Entry) error {
	m.entries[entry.RequestKey] = entry
	return nil
}

func (m *memoryCache) RateLimitCooldown(_ context.Context, source, bucket string) (time.Time, bool, error) {
	value, ok := m.cooldowns[source+"|"+bucket]
	return value, ok, nil
}

func (m *memoryCache) SetRateLimitCooldown(_ context.Context, source, bucket string, until time.Time) error {
	m.cooldowns[source+"|"+bucket] = until
	return nil
}

func (m *memoryCache) put(url, body string, fetchedAt, expiresAt time.Time) {
	m.entries[RequestKey(http.MethodGet, url)] = httpcache.Entry{
		RequestKey: RequestKey(http.MethodGet, url),
		Source:     "hackernews",
		Method:     http.MethodGet,
		URL:        url,
		StatusCode: http.StatusOK,
		Body:       []byte(body),
		FetchedAt:  fetchedAt,
		ExpiresAt:  expiresAt,
	}
}

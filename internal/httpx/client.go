package httpx

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"golang.org/x/time/rate"

	"github.com/AIluffy/tildewire/internal/httpcache"
)

// Client wraps HTTP retries, timeouts, and per-source token buckets.
type Client struct {
	client   *retryablehttp.Client
	limiters map[string]*rate.Limiter
	cache    httpcache.Store
	cacheTTL time.Duration
}

const (
	defaultMaxBodyBytes    int64 = 8 << 20
	defaultCacheTTL              = 6 * time.Hour
	hackerNewsRequestEvery       = 50 * time.Millisecond
	hackerNewsRequestBurst       = 32
	gitHubAPIRequestEvery        = 5 * time.Second
	gitHubAPIRequestBurst        = 2

	// StaleReasonNetworkError marks stale cache returned after a network or HTTP failure.
	StaleReasonNetworkError = "network_error"
	// StaleReasonRateLimited marks stale cache returned because a source is rate-limited.
	StaleReasonRateLimited = "rate_limited"
)

// GetOptions controls cache and refresh behavior for a GET.
type GetOptions struct {
	Source          string
	RateLimitBucket string
	URL             string
	TTL             time.Duration
	ForceRefresh    bool
	MaxBodyBytes    int64
	Cache           httpcache.Store
	BearerToken     string
}

// Response is the result of a cache-aware GET.
type Response struct {
	Source      string
	URL         string
	StatusCode  int
	Body        []byte
	FetchedAt   time.Time
	ExpiresAt   time.Time
	FromCache   bool
	Stale       bool
	StaleReason string
}

// Getter performs cache-aware GET requests.
type Getter interface {
	DoGET(context.Context, GetOptions) (Response, error)
}

// Poster performs cache-aware JSON POST requests.
type Poster interface {
	DoPOSTJSON(context.Context, PostOptions) (Response, error)
}

// Requester performs the HTTP operations required by source adapters.
type Requester interface {
	Getter
	Poster
}

// CacheTTLSetter updates the raw HTTP cache lifetime when supported.
type CacheTTLSetter interface {
	SetCacheTTL(time.Duration)
}

// New creates the default HTTP client used by source adapters.
func New(timeout time.Duration, caches ...httpcache.Store) *Client {
	retry := retryablehttp.NewClient()
	retry.RetryMax = 2
	retry.RetryWaitMin = 300 * time.Millisecond
	retry.RetryWaitMax = 2 * time.Second
	retry.CheckRetry = retryExceptRateLimit
	retry.HTTPClient = &http.Client{Timeout: timeout}
	retry.ErrorHandler = retryablehttp.PassthroughErrorHandler
	retry.Logger = nil
	client := &Client{
		client: retry,
		limiters: map[string]*rate.Limiter{
			"github":      rate.NewLimiter(rate.Every(30*time.Second), 1),
			"github_api":  rate.NewLimiter(rate.Every(gitHubAPIRequestEvery), gitHubAPIRequestBurst),
			"hackernews":  rate.NewLimiter(rate.Every(hackerNewsRequestEvery), hackerNewsRequestBurst),
			"huggingface": rate.NewLimiter(rate.Every(10*time.Second), 2),
			"lobsters":    rate.NewLimiter(rate.Every(10*time.Second), 2),
			"producthunt": rate.NewLimiter(rate.Every(10*time.Second), 2),
		},
	}
	if len(caches) > 0 {
		client.cache = caches[0]
	}
	return client
}

// SetCacheTTL sets the raw HTTP cache lifetime used for new and refreshed cache entries.
func (c *Client) SetCacheTTL(ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	c.cacheTTL = ttl
}

// DoGET performs a cache-aware, rate-limited GET.
func (c *Client) DoGET(ctx context.Context, options GetOptions) (Response, error) {
	if strings.TrimSpace(options.URL) == "" {
		return Response{}, fmt.Errorf("url is required")
	}
	source := options.Source
	if source == "" {
		source = "unknown"
	}
	ttl := c.resolveCacheTTL(options.TTL)
	maxBodyBytes := options.MaxBodyBytes
	if maxBodyBytes <= 0 {
		maxBodyBytes = defaultMaxBodyBytes
	}
	if options.Cache == nil {
		options.Cache = c.cache
	}
	key := RequestKey(http.MethodGet, options.URL)
	now := time.Now().UTC()

	var cached httpcache.Entry
	hasCache := false
	if options.Cache != nil {
		entry, ok, err := options.Cache.GetHTTPCache(ctx, key)
		if err != nil {
			return Response{}, fmt.Errorf("load http cache: %w", err)
		}
		cached = c.cacheEntryWithTTL(entry, ttl)
		hasCache = ok
		if ok && !options.ForceRefresh && cached.ExpiresAt.After(now) {
			return responseFromCache(cached, false, ""), nil
		}
	}
	if cooldowns, ok := options.Cache.(httpcache.CooldownStore); ok {
		cooldownUntil, ok, err := cooldowns.RateLimitCooldown(ctx, source, key)
		if err != nil {
			return Response{}, fmt.Errorf("load rate-limit cooldown: %w", err)
		}
		if ok && cooldownUntil.After(now) {
			if hasCache {
				return responseFromCache(cached, true, StaleReasonRateLimited), nil
			}
			return Response{}, fmt.Errorf("http 429 cooldown until %s for %s", cooldownUntil.Format(time.RFC3339), options.URL)
		}
	}

	limiterKey := source
	if bucket := strings.TrimSpace(options.RateLimitBucket); bucket != "" {
		limiterKey = bucket
	}
	if limiter, ok := c.limiters[limiterKey]; ok {
		if err := limiter.Wait(ctx); err != nil {
			if hasCache {
				return responseFromCache(cached, true, StaleReasonRateLimited), nil
			}
			return Response{}, err
		}
	}
	req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, options.URL, nil)
	if err != nil {
		return Response{}, err
	}
	req.Header.Set("User-Agent", "tildewire/0.1")
	if token := strings.TrimSpace(options.BearerToken); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if hasCache {
		if cached.ETag != "" {
			req.Header.Set("If-None-Match", cached.ETag)
		}
		if cached.LastModified != "" {
			req.Header.Set("If-Modified-Since", cached.LastModified)
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		if hasCache {
			return responseFromCache(cached, true, StaleReasonNetworkError), nil
		}
		return Response{}, err
	}
	defer resp.Body.Close()
	body, err := readLimitedBody(resp.Body, maxBodyBytes)
	if err != nil {
		if hasCache {
			return responseFromCache(cached, true, StaleReasonNetworkError), nil
		}
		return Response{}, err
	}
	if resp.StatusCode == http.StatusNotModified && hasCache {
		refreshed := cached
		refreshed.StatusCode = resp.StatusCode
		refreshed.FetchedAt = time.Now().UTC()
		refreshed.ExpiresAt = refreshed.FetchedAt.Add(ttl)
		headers, err := json.Marshal(resp.Header)
		if err != nil {
			return Response{}, err
		}
		refreshed.HeadersJSON = string(headers)
		if options.Cache != nil {
			if err := options.Cache.PutHTTPCache(ctx, refreshed); err != nil {
				return Response{}, fmt.Errorf("extend http cache: %w", err)
			}
		}
		return responseFromCache(refreshed, false, ""), nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		staleReason := StaleReasonNetworkError
		if isRateLimited(resp.StatusCode) {
			staleReason = StaleReasonRateLimited
			if cooldowns, ok := options.Cache.(httpcache.CooldownStore); ok {
				cooldownUntil := rateLimitCooldownUntil(resp.Header, time.Now().UTC())
				if err := cooldowns.SetRateLimitCooldown(ctx, source, key, cooldownUntil); err != nil {
					return Response{}, fmt.Errorf("store rate-limit cooldown: %w", err)
				}
			}
		}
		if hasCache {
			return responseFromCache(cached, true, staleReason), nil
		}
		return Response{}, fmt.Errorf("http %d for %s", resp.StatusCode, options.URL)
	}

	headers, err := json.Marshal(resp.Header)
	if err != nil {
		return Response{}, err
	}
	fetchedAt := time.Now().UTC()
	response := Response{
		Source:     source,
		URL:        options.URL,
		StatusCode: resp.StatusCode,
		Body:       body,
		FetchedAt:  fetchedAt,
		ExpiresAt:  fetchedAt.Add(ttl),
	}
	if options.Cache != nil {
		entry := httpcache.Entry{
			RequestKey:   key,
			Source:       source,
			Method:       http.MethodGet,
			URL:          options.URL,
			StatusCode:   resp.StatusCode,
			HeadersJSON:  string(headers),
			Body:         body,
			FetchedAt:    response.FetchedAt,
			ExpiresAt:    response.ExpiresAt,
			ETag:         resp.Header.Get("ETag"),
			LastModified: resp.Header.Get("Last-Modified"),
		}
		if err := options.Cache.PutHTTPCache(ctx, entry); err != nil {
			return Response{}, fmt.Errorf("store http cache: %w", err)
		}
	}
	return response, nil
}

// PostOptions controls cache and refresh behavior for a JSON POST.
type PostOptions struct {
	Source       string
	URL          string
	Body         []byte
	BearerToken  string
	TTL          time.Duration
	ForceRefresh bool
	MaxBodyBytes int64
	Cache        httpcache.Store
}

// DoPOSTJSON performs a cache-aware, rate-limited JSON POST.
func (c *Client) DoPOSTJSON(ctx context.Context, options PostOptions) (Response, error) {
	if strings.TrimSpace(options.URL) == "" {
		return Response{}, fmt.Errorf("url is required")
	}
	source := options.Source
	if source == "" {
		source = "unknown"
	}
	ttl := c.resolveCacheTTL(options.TTL)
	maxBodyBytes := options.MaxBodyBytes
	if maxBodyBytes <= 0 {
		maxBodyBytes = defaultMaxBodyBytes
	}
	if options.Cache == nil {
		options.Cache = c.cache
	}
	key := requestKeyWithBody(http.MethodPost, options.URL, options.Body)
	now := time.Now().UTC()

	var cached httpcache.Entry
	hasCache := false
	if options.Cache != nil {
		entry, ok, err := options.Cache.GetHTTPCache(ctx, key)
		if err != nil {
			return Response{}, fmt.Errorf("load http cache: %w", err)
		}
		cached = c.cacheEntryWithTTL(entry, ttl)
		hasCache = ok
		if ok && !options.ForceRefresh && cached.ExpiresAt.After(now) {
			return responseFromCache(cached, false, ""), nil
		}
	}
	if cooldowns, ok := options.Cache.(httpcache.CooldownStore); ok {
		cooldownUntil, ok, err := cooldowns.RateLimitCooldown(ctx, source, key)
		if err != nil {
			return Response{}, fmt.Errorf("load rate-limit cooldown: %w", err)
		}
		if ok && cooldownUntil.After(now) {
			if hasCache {
				return responseFromCache(cached, true, StaleReasonRateLimited), nil
			}
			return Response{}, fmt.Errorf("http 429 cooldown until %s for %s", cooldownUntil.Format(time.RFC3339), options.URL)
		}
	}

	if limiter, ok := c.limiters[source]; ok {
		if err := limiter.Wait(ctx); err != nil {
			if hasCache {
				return responseFromCache(cached, true, StaleReasonRateLimited), nil
			}
			return Response{}, err
		}
	}
	req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodPost, options.URL, bytes.NewReader(options.Body))
	if err != nil {
		return Response{}, err
	}
	req.Header.Set("User-Agent", "tildewire/0.1")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	if token := strings.TrimSpace(options.BearerToken); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		if hasCache {
			return responseFromCache(cached, true, StaleReasonNetworkError), nil
		}
		return Response{}, err
	}
	defer resp.Body.Close()
	body, err := readLimitedBody(resp.Body, maxBodyBytes)
	if err != nil {
		if hasCache {
			return responseFromCache(cached, true, StaleReasonNetworkError), nil
		}
		return Response{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		staleReason := StaleReasonNetworkError
		if isRateLimited(resp.StatusCode) {
			staleReason = StaleReasonRateLimited
			if cooldowns, ok := options.Cache.(httpcache.CooldownStore); ok {
				cooldownUntil := rateLimitCooldownUntil(resp.Header, time.Now().UTC())
				if err := cooldowns.SetRateLimitCooldown(ctx, source, key, cooldownUntil); err != nil {
					return Response{}, fmt.Errorf("store rate-limit cooldown: %w", err)
				}
			}
		}
		if hasCache {
			return responseFromCache(cached, true, staleReason), nil
		}
		return Response{}, fmt.Errorf("http %d for %s", resp.StatusCode, options.URL)
	}

	headers, err := json.Marshal(resp.Header)
	if err != nil {
		return Response{}, err
	}
	fetchedAt := time.Now().UTC()
	response := Response{
		Source:     source,
		URL:        options.URL,
		StatusCode: resp.StatusCode,
		Body:       body,
		FetchedAt:  fetchedAt,
		ExpiresAt:  fetchedAt.Add(ttl),
	}
	if options.Cache != nil {
		entry := httpcache.Entry{
			RequestKey:   key,
			Source:       source,
			Method:       http.MethodPost,
			URL:          options.URL,
			StatusCode:   resp.StatusCode,
			HeadersJSON:  string(headers),
			Body:         body,
			FetchedAt:    response.FetchedAt,
			ExpiresAt:    response.ExpiresAt,
			ETag:         resp.Header.Get("ETag"),
			LastModified: resp.Header.Get("Last-Modified"),
		}
		if err := options.Cache.PutHTTPCache(ctx, entry); err != nil {
			return Response{}, fmt.Errorf("store http cache: %w", err)
		}
	}
	return response, nil
}

func (c *Client) resolveCacheTTL(ttl time.Duration) time.Duration {
	if c.cacheTTL > 0 {
		return c.cacheTTL
	}
	if ttl > 0 {
		return ttl
	}
	return defaultCacheTTL
}

func (c *Client) cacheEntryWithTTL(entry httpcache.Entry, ttl time.Duration) httpcache.Entry {
	if c.cacheTTL <= 0 || entry.FetchedAt.IsZero() {
		return entry
	}
	entry.ExpiresAt = entry.FetchedAt.Add(ttl)
	return entry
}

// RequestKey returns the stable cache key for an HTTP request.
func RequestKey(method, url string) string {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = http.MethodGet
	}
	return method + " " + strings.TrimSpace(url)
}

func requestKeyWithBody(method, url string, body []byte) string {
	sum := sha256.Sum256(body)
	return fmt.Sprintf("%s body-sha256:%x", RequestKey(method, url), sum)
}

func responseFromCache(entry httpcache.Entry, stale bool, staleReason string) Response {
	return Response{
		Source:      entry.Source,
		URL:         entry.URL,
		StatusCode:  entry.StatusCode,
		Body:        entry.Body,
		FetchedAt:   entry.FetchedAt,
		ExpiresAt:   entry.ExpiresAt,
		FromCache:   true,
		Stale:       stale,
		StaleReason: staleReason,
	}
}

func readLimitedBody(body io.Reader, maxBytes int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("response body exceeds %d bytes", maxBytes)
	}
	return data, nil
}

func isRateLimited(status int) bool {
	return status == http.StatusForbidden || status == http.StatusTooManyRequests
}

func retryExceptRateLimit(ctx context.Context, resp *http.Response, err error) (bool, error) {
	if resp != nil && isRateLimited(resp.StatusCode) {
		return false, nil
	}
	return retryablehttp.DefaultRetryPolicy(ctx, resp, err)
}

func retryAfter(value string, now time.Time) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return now.Add(5 * time.Minute)
	}
	if seconds, err := time.ParseDuration(value + "s"); err == nil {
		return now.Add(seconds)
	}
	if parsed, err := http.ParseTime(value); err == nil {
		return parsed.UTC()
	}
	return now.Add(5 * time.Minute)
}

func rateLimitCooldownUntil(header http.Header, now time.Time) time.Time {
	if retry := strings.TrimSpace(header.Get("Retry-After")); retry != "" {
		return retryAfter(retry, now)
	}
	reset := strings.TrimSpace(header.Get("X-RateLimit-Reset"))
	if reset == "" {
		reset = strings.TrimSpace(header.Get("X-Rate-Limit-Reset"))
	}
	if reset != "" {
		if seconds, err := strconv.ParseInt(reset, 10, 64); err == nil {
			resetAt := time.Unix(seconds, 0).UTC()
			if resetAt.After(now) {
				return resetAt
			}
			if seconds > 0 && seconds <= int64((24*time.Hour).Seconds()) {
				return now.Add(time.Duration(seconds) * time.Second)
			}
		}
	}
	return retryAfter("", now)
}

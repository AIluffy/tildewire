package httpx

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"golang.org/x/time/rate"

	"github.com/AIluffy/tildewire/internal/httpcache"
)

// Client wraps HTTP retries, timeouts, and per-source token buckets.
type Client struct {
	client    *retryablehttp.Client
	limiterMu sync.RWMutex
	limiters  map[string]*rate.Limiter
	cache     httpcache.Store
	cacheTTL  time.Duration
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

// ErrRateLimited marks a local or remote rate-limit failure.
var ErrRateLimited = errors.New("rate limited")

// HTTPStatusError reports a non-success HTTP response that could not be hidden by cache fallback.
type HTTPStatusError struct {
	StatusCode int
	URL        string
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("http %d for %s", e.StatusCode, e.URL)
}

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
	return c.doRequest(ctx, requestOptions{
		Method:          http.MethodGet,
		Source:          options.Source,
		RateLimitBucket: options.RateLimitBucket,
		URL:             options.URL,
		TTL:             options.TTL,
		ForceRefresh:    options.ForceRefresh,
		MaxBodyBytes:    options.MaxBodyBytes,
		Cache:           options.Cache,
		BearerToken:     options.BearerToken,
		Conditional:     true,
	})
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
	return c.doRequest(ctx, requestOptions{
		Method:       http.MethodPost,
		Source:       options.Source,
		URL:          options.URL,
		Body:         options.Body,
		BearerToken:  options.BearerToken,
		TTL:          options.TTL,
		ForceRefresh: options.ForceRefresh,
		MaxBodyBytes: options.MaxBodyBytes,
		Cache:        options.Cache,
		JSON:         true,
	})
}

type requestOptions struct {
	Method          string
	Source          string
	RateLimitBucket string
	URL             string
	Body            []byte
	BearerToken     string
	TTL             time.Duration
	ForceRefresh    bool
	MaxBodyBytes    int64
	Cache           httpcache.Store
	Conditional     bool
	JSON            bool
}

func (c *Client) doRequest(ctx context.Context, options requestOptions) (Response, error) {
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
	cache := options.Cache
	if cache == nil {
		cache = c.cache
	}
	key := requestCacheKey(options)
	now := time.Now().UTC()

	cached, hasCache, err := c.loadCacheEntry(ctx, cache, key, ttl)
	if err != nil {
		return Response{}, err
	}
	if hasCache && !options.ForceRefresh && cached.ExpiresAt.After(now) {
		return responseFromCache(cached, false, ""), nil
	}
	if resp, ok, err := c.responseDuringCooldown(ctx, cache, source, key, options.URL, cached, hasCache, now); ok || err != nil {
		return resp, err
	}
	if err := c.waitForLimiter(ctx, limiterKey(source, options.RateLimitBucket)); err != nil {
		if hasCache {
			return responseFromCache(cached, true, StaleReasonRateLimited), nil
		}
		return Response{}, fmt.Errorf("%w: %v", ErrRateLimited, err)
	}

	req, err := newRequest(ctx, options)
	if err != nil {
		return Response{}, err
	}
	applyRequestHeaders(req, options, cached, hasCache)

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
	if options.Conditional && resp.StatusCode == http.StatusNotModified && hasCache {
		return c.extendCacheEntry(ctx, cache, cached, resp.Header, ttl)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.handleHTTPError(ctx, cache, source, key, options.URL, resp, cached, hasCache)
	}
	return c.storeResponse(ctx, cache, key, source, options.Method, options.URL, body, resp.Header, resp.StatusCode, ttl)
}

func requestCacheKey(options requestOptions) string {
	if len(options.Body) > 0 || strings.EqualFold(options.Method, http.MethodPost) {
		return requestKeyWithBody(options.Method, options.URL, options.Body)
	}
	return RequestKey(options.Method, options.URL)
}

func (c *Client) loadCacheEntry(ctx context.Context, cache httpcache.Store, key string, ttl time.Duration) (httpcache.Entry, bool, error) {
	if cache == nil {
		return httpcache.Entry{}, false, nil
	}
	entry, ok, err := cache.GetHTTPCache(ctx, key)
	if err != nil {
		return httpcache.Entry{}, false, fmt.Errorf("load http cache: %w", err)
	}
	return c.cacheEntryWithTTL(entry, ttl), ok, nil
}

func (c *Client) responseDuringCooldown(ctx context.Context, cache httpcache.Store, source, key, url string, cached httpcache.Entry, hasCache bool, now time.Time) (Response, bool, error) {
	cooldowns, ok := cache.(httpcache.CooldownStore)
	if !ok {
		return Response{}, false, nil
	}
	cooldownUntil, ok, err := cooldowns.RateLimitCooldown(ctx, source, key)
	if err != nil {
		return Response{}, false, fmt.Errorf("load rate-limit cooldown: %w", err)
	}
	if !ok || !cooldownUntil.After(now) {
		return Response{}, false, nil
	}
	if hasCache {
		return responseFromCache(cached, true, StaleReasonRateLimited), true, nil
	}
	return Response{}, true, fmt.Errorf("%w: http 429 cooldown until %s for %s", ErrRateLimited, cooldownUntil.Format(time.RFC3339), url)
}

func limiterKey(source, bucket string) string {
	if bucket := strings.TrimSpace(bucket); bucket != "" {
		return bucket
	}
	return source
}

func (c *Client) waitForLimiter(ctx context.Context, key string) error {
	c.limiterMu.RLock()
	limiter := c.limiters[key]
	c.limiterMu.RUnlock()
	if limiter == nil {
		return nil
	}
	return limiter.Wait(ctx)
}

func newRequest(ctx context.Context, options requestOptions) (*retryablehttp.Request, error) {
	var body io.Reader
	if len(options.Body) > 0 {
		body = bytes.NewReader(options.Body)
	}
	return retryablehttp.NewRequestWithContext(ctx, options.Method, options.URL, body)
}

func applyRequestHeaders(req *retryablehttp.Request, options requestOptions, cached httpcache.Entry, hasCache bool) {
	req.Header.Set("User-Agent", "tildewire/0.1")
	if options.JSON {
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
	}
	if token := strings.TrimSpace(options.BearerToken); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if options.Conditional && hasCache {
		if cached.ETag != "" {
			req.Header.Set("If-None-Match", cached.ETag)
		}
		if cached.LastModified != "" {
			req.Header.Set("If-Modified-Since", cached.LastModified)
		}
	}
}

func (c *Client) extendCacheEntry(ctx context.Context, cache httpcache.Store, cached httpcache.Entry, header http.Header, ttl time.Duration) (Response, error) {
	refreshed := cached
	refreshed.StatusCode = http.StatusNotModified
	refreshed.FetchedAt = time.Now().UTC()
	refreshed.ExpiresAt = refreshed.FetchedAt.Add(ttl)
	headers, err := json.Marshal(header)
	if err != nil {
		return Response{}, err
	}
	refreshed.HeadersJSON = string(headers)
	if cache != nil {
		if err := cache.PutHTTPCache(ctx, refreshed); err != nil {
			return Response{}, fmt.Errorf("extend http cache: %w", err)
		}
	}
	return responseFromCache(refreshed, false, ""), nil
}

func (c *Client) handleHTTPError(ctx context.Context, cache httpcache.Store, source, key, url string, resp *http.Response, cached httpcache.Entry, hasCache bool) (Response, error) {
	statusErr := &HTTPStatusError{StatusCode: resp.StatusCode, URL: url}
	if isRateLimitedResponse(resp) {
		if cooldowns, ok := cache.(httpcache.CooldownStore); ok {
			cooldownUntil := rateLimitCooldownUntil(resp.Header, time.Now().UTC())
			if err := cooldowns.SetRateLimitCooldown(ctx, source, key, cooldownUntil); err != nil {
				return Response{}, fmt.Errorf("store rate-limit cooldown: %w", err)
			}
		}
		if hasCache {
			return responseFromCache(cached, true, StaleReasonRateLimited), nil
		}
		return Response{}, fmt.Errorf("%w: %w", ErrRateLimited, statusErr)
	}
	if isAuthStatus(resp.StatusCode) {
		return Response{}, statusErr
	}
	if hasCache {
		return responseFromCache(cached, true, StaleReasonNetworkError), nil
	}
	return Response{}, statusErr
}

func (c *Client) storeResponse(ctx context.Context, cache httpcache.Store, key, source, method, url string, body []byte, header http.Header, statusCode int, ttl time.Duration) (Response, error) {
	headers, err := json.Marshal(header)
	if err != nil {
		return Response{}, err
	}
	fetchedAt := time.Now().UTC()
	response := Response{
		Source:     source,
		URL:        url,
		StatusCode: statusCode,
		Body:       body,
		FetchedAt:  fetchedAt,
		ExpiresAt:  fetchedAt.Add(ttl),
	}
	if cache != nil {
		entry := httpcache.Entry{
			RequestKey:   key,
			Source:       source,
			Method:       method,
			URL:          url,
			StatusCode:   statusCode,
			HeadersJSON:  string(headers),
			Body:         body,
			FetchedAt:    response.FetchedAt,
			ExpiresAt:    response.ExpiresAt,
			ETag:         header.Get("ETag"),
			LastModified: header.Get("Last-Modified"),
		}
		if err := cache.PutHTTPCache(ctx, entry); err != nil {
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

func isAuthStatus(status int) bool {
	return status == http.StatusUnauthorized || status == http.StatusForbidden
}

func isRateLimitedResponse(resp *http.Response) bool {
	if resp == nil {
		return false
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if resp.StatusCode != http.StatusForbidden {
		return false
	}
	return hasExhaustedRateLimitHeader(resp.Header) || strings.TrimSpace(resp.Header.Get("Retry-After")) != ""
}

func hasExhaustedRateLimitHeader(header http.Header) bool {
	remaining := strings.TrimSpace(header.Get("X-RateLimit-Remaining"))
	if remaining == "" {
		remaining = strings.TrimSpace(header.Get("X-Rate-Limit-Remaining"))
	}
	return remaining == "0"
}

func retryExceptRateLimit(ctx context.Context, resp *http.Response, err error) (bool, error) {
	if isRateLimitedResponse(resp) {
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

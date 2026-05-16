package normalize

import (
	"crypto/sha1"
	"encoding/hex"
	"net"
	"net/url"
	"path"
	"sort"
	"strings"
)

var trackingParams = map[string]struct{}{
	"fbclid": {},
	"gclid":  {},
	"ref":    {},
	"source": {},
}

// CanonicalURL returns a stable URL string for strong-key dedupe.
func CanonicalURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if u.Scheme == "" || u.Host == "" {
		return raw
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	if host, port, err := net.SplitHostPort(u.Host); err == nil {
		if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
			u.Host = host
		}
	}
	u.Fragment = ""
	u.Path = path.Clean("/" + u.EscapedPath())
	if u.Path == "/" {
		u.Path = ""
	}

	query := u.Query()
	for key := range query {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "utm_") {
			delete(query, key)
			continue
		}
		if _, ok := trackingParams[lower]; ok {
			delete(query, key)
		}
	}
	u.RawQuery = encodeSortedQuery(query)
	return u.String()
}

// StableID returns a compact deterministic id from a canonical key.
func StableID(canonicalKey string) string {
	sum := sha1.Sum([]byte(canonicalKey))
	return hex.EncodeToString(sum[:])
}

func encodeSortedQuery(query url.Values) string {
	if len(query) == 0 {
		return ""
	}
	keys := make([]string, 0, len(query))
	for key := range query {
		sort.Strings(query[key])
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		for _, value := range query[key] {
			parts = append(parts, url.QueryEscape(key)+"="+url.QueryEscape(value))
		}
	}
	return strings.Join(parts, "&")
}

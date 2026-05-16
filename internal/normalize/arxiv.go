package normalize

import (
	"net/url"
	"regexp"
	"strings"
)

var arxivIDPattern = regexp.MustCompile(`^\d{4}\.\d{4,5}(v\d+)?$`)

// NormalizeArxivID returns a canonical arXiv identifier if raw is supported.
func NormalizeArxivID(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	value = strings.TrimPrefix(value, "arxiv:")
	value = strings.TrimSuffix(value, ".pdf")
	if arxivIDPattern.MatchString(value) {
		return value
	}
	return ""
}

// ArxivIDFromURL extracts an arXiv identifier from common arxiv.org URLs.
func ArxivIDFromURL(raw string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", false
	}
	host := strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
	if host != "arxiv.org" {
		return "", false
	}
	parts := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
	if len(parts) < 2 {
		return "", false
	}
	switch strings.ToLower(parts[0]) {
	case "abs", "pdf", "html":
		id := NormalizeArxivID(parts[1])
		return id, id != ""
	default:
		return "", false
	}
}

package normalize

import (
	"net/url"
	"strings"
)

// NormalizeRepo returns a canonical lower-case owner/repo identifier.
func NormalizeRepo(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "/")
	raw = strings.TrimSuffix(raw, "/")
	parts := strings.Split(raw, "/")
	if len(parts) < 2 {
		return ""
	}
	owner := strings.ToLower(strings.TrimSpace(parts[0]))
	repo := strings.ToLower(strings.TrimSpace(parts[1]))
	if owner == "" || repo == "" {
		return ""
	}
	return owner + "/" + repo
}

// RepoKey returns the strong canonical key used to dedupe GitHub repositories.
func RepoKey(repo string) string {
	repo = NormalizeRepo(repo)
	if repo == "" {
		return ""
	}
	return "repo:" + repo
}

// RepoFromPath extracts owner/repo from a GitHub path or URL.
func RepoFromPath(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if parsed, err := url.Parse(raw); err == nil && parsed.Path != "" {
		raw = parsed.Path
	}
	repo := NormalizeRepo(raw)
	return repo, repo != ""
}

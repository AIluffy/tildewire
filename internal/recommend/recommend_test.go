package recommend

import (
	"strings"
	"testing"
	"time"

	"github.com/AIluffy/tildewire/internal/domain"
)

func TestBuildProfileDecaysOlderSignals(t *testing.T) {
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	fresh := BuildProfile([]Signal{{
		Term:       domain.RecommendationTerm{Kind: "tag", Value: "ai", Weight: 1},
		EventType:  domain.ItemEventSave,
		OccurredAt: now,
	}}, now)
	old := BuildProfile([]Signal{{
		Term:       domain.RecommendationTerm{Kind: "tag", Value: "ai", Weight: 1},
		EventType:  domain.ItemEventSave,
		OccurredAt: now.Add(-60 * 24 * time.Hour),
	}}, now)

	key := TermKey("tag", "ai")
	freshScore := fresh.Terms[key].Positive
	oldScore := old.Terms[key].Positive
	if freshScore <= 0 {
		t.Fatalf("fresh score = %.3f, want positive", freshScore)
	}
	if oldScore >= freshScore*0.30 {
		t.Fatalf("old score = %.3f, fresh score = %.3f, want roughly two half-lives of decay", oldScore, freshScore)
	}
}

func TestTermsForItemExtractsStableRecommendationTerms(t *testing.T) {
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	item := domain.FeedItem{
		ID:           "repo-ai",
		Title:        "Agentic coding benchmark for terminal developers",
		Summary:      "A local-first assistant benchmark",
		Language:     "Go",
		Tags:         []string{"AI", "Terminal"},
		Author:       "Ada",
		FirstSeenAt:  now,
		LastSeenAt:   now,
		Refs:         domain.Refs{Repo: "owner/repo"},
		Sources:      []domain.ItemSource{{Source: domain.SourceGitHub}},
		CanonicalKey: "repo:owner/repo",
	}

	terms := TermsForItem(item)
	termSet := make(map[string]bool, len(terms))
	for _, term := range terms {
		termSet[TermKey(term.Kind, term.Value)] = true
		if term.Value != strings.ToLower(term.Value) {
			t.Fatalf("term not normalized: %+v", term)
		}
	}

	for _, want := range []string{
		TermKey("tag", "ai"),
		TermKey("tag", "terminal"),
		TermKey("language", "go"),
		TermKey("repo", "owner/repo"),
		TermKey("author", "ada"),
		TermKey("source", "github"),
		TermKey("keyword", "agentic"),
	} {
		if !termSet[want] {
			t.Fatalf("missing term %q from %+v", want, terms)
		}
	}
}

func TestTopProfileTermsSortsByPolarityThenTerm(t *testing.T) {
	profile := domain.RecommendationProfile{Terms: map[string]domain.RecommendationProfileTerm{
		TermKey("tag", "go"): {
			Kind:     "tag",
			Value:    "go",
			Positive: 0.8,
			Negative: 0.1,
		},
		TermKey("keyword", "ai"): {
			Kind:     "keyword",
			Value:    "ai",
			Positive: 0.8,
			Negative: 0.4,
		},
		TermKey("source", "github"): {
			Kind:     "source",
			Value:    "github",
			Positive: 1.2,
			Negative: 0.2,
		},
		TermKey("tag", "rust"): {
			Kind:     "tag",
			Value:    "rust",
			Positive: 0.2,
			Negative: 0.9,
		},
	}}

	positive := TopProfileTerms(profile, ProfilePositive, 3)
	if got := termNames(positive); strings.Join(got, ",") != "source:github,keyword:ai,tag:go" {
		t.Fatalf("positive terms = %v", got)
	}

	negative := TopProfileTerms(profile, ProfileNegative, 2)
	if got := termNames(negative); strings.Join(got, ",") != "tag:rust,keyword:ai" {
		t.Fatalf("negative terms = %v", got)
	}
}

func termNames(terms []domain.RecommendationProfileTerm) []string {
	names := make([]string, 0, len(terms))
	for _, term := range terms {
		names = append(names, term.Kind+":"+term.Value)
	}
	return names
}

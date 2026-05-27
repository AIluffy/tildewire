package recommend

import (
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/AIluffy/tildewire/internal/domain"
)

const (
	HalfLife      = 30 * 24 * time.Hour
	ProfileWindow = 180 * 24 * time.Hour
)

type Signal struct {
	Term       domain.RecommendationTerm
	EventType  domain.ItemEventType
	OccurredAt time.Time
}

var tokenPattern = regexp.MustCompile(`[[:alnum:]][[:alnum:]_+\-./#]*`)

var stopWords = map[string]bool{
	"about": true, "after": true, "again": true, "also": true, "and": true, "are": true,
	"benchmark": true, "for": true, "from": true, "how": true, "into": true, "its": true,
	"new": true, "not": true, "of": true, "on": true, "open": true, "over": true,
	"show": true, "that": true, "the": true, "this": true, "to": true, "using": true,
	"with": true, "you": true, "your": true,
}

func TermKey(kind, value string) string {
	return strings.ToLower(strings.TrimSpace(kind)) + "\x00" + normalizeTermValue(value)
}

func TermsForItem(item domain.FeedItem) []domain.RecommendationTerm {
	terms := make(map[string]domain.RecommendationTerm)
	addTerm := func(kind, value string, weight float64) {
		value = normalizeTermValue(value)
		if value == "" || weight <= 0 {
			return
		}
		key := TermKey(kind, value)
		current := terms[key]
		if current.Weight >= weight {
			return
		}
		terms[key] = domain.RecommendationTerm{
			ItemID: item.ID,
			Kind:   strings.ToLower(strings.TrimSpace(kind)),
			Value:  value,
			Weight: weight,
		}
	}

	for _, tag := range item.Tags {
		addTerm("tag", tag, 1.0)
	}
	addTerm("language", item.Language, 0.70)
	addTerm("repo", item.Refs.Repo, 1.10)
	addTerm("author", item.Author, 0.75)
	addTerm("author", item.Organization, 0.75)
	for _, source := range item.Sources {
		addTerm("source", string(source.Source), 0.85)
	}
	addKeywordTerms(addTerm, item.Title, 0.35, 10)
	addKeywordTerms(addTerm, item.Subtitle, 0.25, 8)
	addKeywordTerms(addTerm, item.Summary, 0.15, 12)

	out := make([]domain.RecommendationTerm, 0, len(terms))
	for _, term := range terms {
		out = append(out, term)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind == out[j].Kind {
			return out[i].Value < out[j].Value
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

func BuildProfile(signals []Signal, now time.Time) domain.RecommendationProfile {
	profile := domain.RecommendationProfile{Terms: make(map[string]domain.RecommendationProfileTerm)}
	for _, signal := range signals {
		weight := EventWeight(signal.EventType)
		if weight == 0 || signal.Term.Value == "" {
			continue
		}
		contribution := weight * signal.Term.Weight * Decay(signal.OccurredAt, now)
		if contribution == 0 {
			continue
		}
		key := TermKey(signal.Term.Kind, signal.Term.Value)
		term := profile.Terms[key]
		if term.Kind == "" {
			term.Kind = strings.ToLower(strings.TrimSpace(signal.Term.Kind))
			term.Value = normalizeTermValue(signal.Term.Value)
		}
		term.Score += contribution
		if contribution > 0 {
			term.Positive += contribution
			term.Reasons = appendReason(term.Reasons, ReasonForSignal(signal, contribution))
		} else {
			term.Negative += -contribution
		}
		profile.Terms[key] = term
	}
	for key, term := range profile.Terms {
		term.Reasons = TopReasons(term.Reasons, 3)
		profile.Terms[key] = term
	}
	return profile
}

func EventWeight(eventType domain.ItemEventType) float64 {
	switch eventType {
	case domain.ItemEventSave:
		return 1.20
	case domain.ItemEventUnsave:
		return -0.25
	case domain.ItemEventHide:
		return -1.40
	case domain.ItemEventRestore:
		return 0.30
	case domain.ItemEventRead:
		return 0.20
	case domain.ItemEventUnread:
		return -0.10
	case domain.ItemEventDetailOpen:
		return 0.45
	case domain.ItemEventOpenURL:
		return 0.75
	case domain.ItemEventOpenSource:
		return 0.55
	case domain.ItemEventCopyURL, domain.ItemEventCopyMarkdown:
		return 0.35
	default:
		return 0
	}
}

func Decay(occurredAt, now time.Time) float64 {
	if occurredAt.IsZero() {
		return 0
	}
	if now.IsZero() || occurredAt.After(now) {
		return 1
	}
	age := now.Sub(occurredAt)
	return math.Pow(0.5, age.Seconds()/HalfLife.Seconds())
}

func ReasonForSignal(signal Signal, weight float64) domain.RecommendationReason {
	value := normalizeTermValue(signal.Term.Value)
	return domain.RecommendationReason{
		Kind:   strings.ToLower(strings.TrimSpace(signal.Term.Kind)),
		Value:  value,
		Label:  reasonVerb(signal.EventType) + " " + value,
		Weight: weight,
	}
}

func TopReasons(reasons []domain.RecommendationReason, limit int) []domain.RecommendationReason {
	if len(reasons) == 0 || limit <= 0 {
		return nil
	}
	sort.SliceStable(reasons, func(i, j int) bool {
		return reasons[i].Weight > reasons[j].Weight
	})
	seen := make(map[string]bool, len(reasons))
	out := make([]domain.RecommendationReason, 0, min(limit, len(reasons)))
	for _, reason := range reasons {
		if reason.Label == "" || seen[reason.Label] {
			continue
		}
		seen[reason.Label] = true
		out = append(out, reason)
		if len(out) == limit {
			break
		}
	}
	return out
}

func addKeywordTerms(add func(string, string, float64), text string, weight float64, limit int) {
	count := 0
	for _, token := range tokenPattern.FindAllString(strings.ToLower(text), -1) {
		token = normalizeTermValue(token)
		if token == "" || len([]rune(token)) < 3 || stopWords[token] {
			continue
		}
		add("keyword", token, weight)
		count++
		if count >= limit {
			return
		}
	}
}

func normalizeTermValue(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func appendReason(reasons []domain.RecommendationReason, reason domain.RecommendationReason) []domain.RecommendationReason {
	if reason.Label == "" {
		return reasons
	}
	return append(reasons, reason)
}

func reasonVerb(eventType domain.ItemEventType) string {
	switch eventType {
	case domain.ItemEventSave:
		return "saved"
	case domain.ItemEventUnsave:
		return "unsaved"
	case domain.ItemEventHide:
		return "hidden"
	case domain.ItemEventRestore:
		return "restored"
	case domain.ItemEventRead:
		return "read"
	case domain.ItemEventUnread:
		return "unread"
	case domain.ItemEventDetailOpen:
		return "opened"
	case domain.ItemEventOpenURL, domain.ItemEventOpenSource:
		return "opened"
	case domain.ItemEventCopyURL, domain.ItemEventCopyMarkdown:
		return "copied"
	default:
		return "matched"
	}
}

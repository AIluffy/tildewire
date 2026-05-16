package app

import (
	"math"
	"net/url"
	"strings"

	"github.com/AIluffy/tildewire/internal/domain"
)

const (
	explicitRuleWeight = 0.50
	implicitSignalUnit = 0.04
	implicitSignalCap  = 0.20
)

func filterPersonalizedHidden(entries []domain.FeedEntry, rules []domain.PersonalizationRule) []domain.FeedEntry {
	if len(entries) == 0 || len(rules) == 0 {
		return entries
	}
	filtered := make([]domain.FeedEntry, 0, len(entries))
	for _, entry := range entries {
		if hasMatchingRule(entry, rules, domain.RuleEffectHide) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func personalizationAdjustment(entry domain.FeedEntry, rules []domain.PersonalizationRule, profile domain.PreferenceProfile) float64 {
	adjustment := 0.0
	for _, rule := range rules {
		if !rule.Enabled || !ruleMatchesEntry(rule, entry) {
			continue
		}
		switch rule.Effect {
		case domain.RuleEffectBoost:
			adjustment += explicitRuleWeight
		case domain.RuleEffectMute:
			adjustment -= explicitRuleWeight
		}
	}
	adjustment += implicitPreferenceAdjustment(entry, profile.SavedTags, profile.SavedLanguages, profile.SavedRepos, profile.SavedAuthors, profile.SavedSources)
	adjustment -= implicitPreferenceAdjustment(entry, profile.HiddenTags, profile.HiddenLanguages, profile.HiddenRepos, profile.HiddenAuthors, profile.HiddenSources)
	return adjustment
}

func hasMatchingRule(entry domain.FeedEntry, rules []domain.PersonalizationRule, effect domain.RuleEffect) bool {
	for _, rule := range rules {
		if rule.Enabled && rule.Effect == effect && ruleMatchesEntry(rule, entry) {
			return true
		}
	}
	return false
}

func ruleMatchesEntry(rule domain.PersonalizationRule, entry domain.FeedEntry) bool {
	value := strings.ToLower(strings.TrimSpace(rule.Value))
	if value == "" {
		return false
	}
	item := entry.Item
	switch rule.Target {
	case domain.RuleTargetKeyword:
		return strings.Contains(entrySearchText(entry), value)
	case domain.RuleTargetLanguage:
		return strings.EqualFold(item.Language, value)
	case domain.RuleTargetTag:
		return containsLower(item.Tags, value)
	case domain.RuleTargetDomain:
		return itemDomain(item.URL) == value || itemDomain(item.CanonicalURL) == value || itemDomain(item.CommentsURL) == value
	case domain.RuleTargetSource:
		for _, source := range entry.Sources {
			if strings.EqualFold(string(source.Source), value) {
				return true
			}
		}
		return false
	case domain.RuleTargetRepo:
		return strings.EqualFold(item.Refs.Repo, value)
	case domain.RuleTargetAuthor:
		return strings.EqualFold(item.Author, value) || strings.EqualFold(item.Organization, value)
	default:
		return false
	}
}

func implicitPreferenceAdjustment(entry domain.FeedEntry, tags, languages, repos, authors, sources map[string]int) float64 {
	score := 0.0
	for _, tag := range entry.Item.Tags {
		score += signalWeight(tags[strings.ToLower(tag)])
	}
	score += signalWeight(languages[strings.ToLower(entry.Item.Language)])
	score += signalWeight(repos[strings.ToLower(entry.Item.Refs.Repo)])
	score += signalWeight(authors[strings.ToLower(entry.Item.Author)])
	score += signalWeight(authors[strings.ToLower(entry.Item.Organization)])
	seenSources := make(map[string]bool, len(entry.Sources))
	for _, source := range entry.Sources {
		key := strings.ToLower(string(source.Source))
		if seenSources[key] {
			continue
		}
		seenSources[key] = true
		score += signalWeight(sources[key])
	}
	return math.Min(score, implicitSignalCap)
}

func signalWeight(count int) float64 {
	if count <= 0 {
		return 0
	}
	return math.Min(float64(count)*implicitSignalUnit, implicitSignalCap)
}

func entrySearchText(entry domain.FeedEntry) string {
	item := entry.Item
	parts := []string{
		item.Title,
		item.Subtitle,
		item.Summary,
		item.Author,
		item.Organization,
		item.Language,
		item.Refs.Repo,
		item.Refs.ArxivID,
		string(item.Metadata),
		strings.Join(item.Tags, " "),
	}
	return strings.ToLower(strings.Join(parts, " "))
}

func containsLower(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(value, want) {
			return true
		}
	}
	return false
}

func itemDomain(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
}

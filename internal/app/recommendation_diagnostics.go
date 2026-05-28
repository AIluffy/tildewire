package app

import (
	"context"
	"time"

	"github.com/AIluffy/tildewire/internal/domain"
	"github.com/AIluffy/tildewire/internal/recommend"
	"github.com/AIluffy/tildewire/internal/score"
)

const recommendDiagnosticsProfileTermLimit = 8

// RecommendationDiagnostics explains the current local Recommend profile and selected item.
func (s *Service) RecommendationDiagnostics(ctx context.Context, selected domain.FeedEntry) (domain.RecommendationDiagnostics, error) {
	rules, err := s.personalization.ListPersonalizationRules(ctx, true)
	if err != nil {
		return domain.RecommendationDiagnostics{}, err
	}
	now := time.Now().UTC()
	profile, err := s.recommendations.RecommendationProfile(ctx, now)
	if err != nil {
		return domain.RecommendationDiagnostics{}, err
	}
	activeRules := activePersonalizationRules(rules)
	candidateIDs, err := s.recommendationCandidateIDs(ctx, activeRules)
	if err != nil {
		return domain.RecommendationDiagnostics{}, err
	}
	return domain.RecommendationDiagnostics{
		PositiveTerms: recommend.TopProfileTerms(profile, recommend.ProfilePositive, recommendDiagnosticsProfileTermLimit),
		NegativeTerms: recommend.TopProfileTerms(profile, recommend.ProfileNegative, recommendDiagnosticsProfileTermLimit),
		Selected:      s.recommendationDiagnosticsForEntry(selected, activeRules, profile, candidateIDs, now),
	}, nil
}

func (s *Service) recommendationCandidateIDs(ctx context.Context, rules []domain.PersonalizationRule) (map[string]struct{}, error) {
	entries, err := s.recommendationCandidateEntries(ctx)
	if err != nil {
		return nil, err
	}
	entries = filterPersonalizedHidden(entries, rules)
	entries = s.filterEntriesForEnabledSources(entries, domain.SourceRecommend)
	ids := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		ids[entry.Item.ID] = struct{}{}
	}
	return ids, nil
}

func (s *Service) recommendationDiagnosticsForEntry(entry domain.FeedEntry, rules []domain.PersonalizationRule, profile domain.RecommendationProfile, candidateIDs map[string]struct{}, now time.Time) domain.RecommendationDiagnosticsItem {
	if entry.Item.ID == "" {
		return domain.RecommendationDiagnosticsItem{
			ExclusionReason: string(domain.RecommendationExclusionNoSelection),
		}
	}
	item := domain.RecommendationDiagnosticsItem{
		ItemID: entry.Item.ID,
		Title:  entry.Item.Title,
	}
	exclusion := domain.RecommendationExclusionNone
	switch {
	case entry.State.Hidden:
		exclusion = domain.RecommendationExclusionHidden
	case hasMatchingRule(entry, rules, domain.RuleEffectHide):
		exclusion = domain.RecommendationExclusionHideRule
	default:
		filteredSources := s.filterItemSourcesForEnabledSources(entry.Sources)
		if len(entry.Sources) > 0 && len(filteredSources) == 0 {
			exclusion = domain.RecommendationExclusionDisabledSource
		}
		entry.Sources = filteredSources
	}

	interestScore, hasPositiveSignal, reasons := recommendationInterestScore(entry, rules, profile)
	hotScore := score.Hot(entry, now)
	finalScore := interestScore + 0.25*hotScore
	_, _, _, matchedTerms := recommendationProfileMatches(entry, profile)
	item.InterestScore = interestScore
	item.HotScore = hotScore
	item.Score = finalScore
	item.HasPositiveSignal = hasPositiveSignal
	item.MatchedTerms = matchedTerms
	item.Reasons = recommendationReasons(entry, reasons, hotScore)

	if exclusion == domain.RecommendationExclusionNone {
		switch {
		case !recommendationCandidateContains(candidateIDs, entry.Item.ID):
			exclusion = domain.RecommendationExclusionOutsideWindow
		case !hasPositiveSignal:
			exclusion = domain.RecommendationExclusionNoPositiveSignal
		case interestScore <= 0:
			exclusion = domain.RecommendationExclusionNonPositiveScore
		default:
			item.Eligible = true
		}
	}
	item.ExclusionReason = string(exclusion)
	return item
}

func recommendationCandidateContains(candidateIDs map[string]struct{}, itemID string) bool {
	_, ok := candidateIDs[itemID]
	return ok
}

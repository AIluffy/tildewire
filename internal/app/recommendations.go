package app

import (
	"context"
	"time"

	"github.com/AIluffy/tildewire/internal/domain"
	"github.com/AIluffy/tildewire/internal/score"
)

const recommendLatestFeedLimit = 250
const recommendDisplayLimit = 10

func (s *Service) recomputeRecommendations(ctx context.Context) error {
	entries, err := s.store.ListFeed(ctx, domain.FeedQuery{Limit: recommendLatestFeedLimit})
	if err != nil {
		return err
	}
	rules, err := s.store.ListPersonalizationRules(ctx, true)
	if err != nil {
		return err
	}
	activeRules := activePersonalizationRules(rules)
	entries = filterPersonalizedHidden(entries, activeRules)
	entries = s.filterEntriesForEnabledSources(entries, domain.SourceRecommend)
	profile, err := s.store.PreferenceProfile(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	scores := make([]domain.RecommendationScore, 0, len(entries))
	for _, entry := range entries {
		interestScore, hasPositiveSignal := recommendationInterestScore(entry, activeRules, profile)
		if !hasPositiveSignal || interestScore <= 0 {
			continue
		}
		hotScore := score.Hot(entry, now)
		scores = append(scores, domain.RecommendationScore{
			ItemID:        entry.Item.ID,
			Score:         interestScore + 0.25*hotScore,
			InterestScore: interestScore,
			HotScore:      hotScore,
			ComputedAt:    now,
		})
	}
	s.storeMu.Lock()
	defer s.storeMu.Unlock()
	return s.store.ReplaceRecommendationScores(ctx, scores)
}

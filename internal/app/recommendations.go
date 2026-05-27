package app

import (
	"context"
	"time"

	"github.com/AIluffy/tildewire/internal/domain"
	"github.com/AIluffy/tildewire/internal/recommend"
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
	now := time.Now().UTC()
	profile, err := s.store.RecommendationProfile(ctx, now)
	if err != nil {
		return err
	}
	scores := make([]domain.RecommendationScore, 0, len(entries))
	for _, entry := range entries {
		interestScore, hasPositiveSignal, reasons := recommendationInterestScore(entry, activeRules, profile)
		if !hasPositiveSignal || interestScore <= 0 {
			continue
		}
		hotScore := score.Hot(entry, now)
		reasons = recommendationReasons(entry, reasons, hotScore)
		scores = append(scores, domain.RecommendationScore{
			ItemID:        entry.Item.ID,
			Score:         interestScore + 0.25*hotScore,
			InterestScore: interestScore,
			HotScore:      hotScore,
			Reasons:       reasons,
			ComputedAt:    now,
		})
	}
	s.storeMu.Lock()
	defer s.storeMu.Unlock()
	return s.store.ReplaceRecommendationScores(ctx, scores)
}

func recommendationReasons(entry domain.FeedEntry, reasons []domain.RecommendationReason, hotScore float64) []domain.RecommendationReason {
	if hotScore >= 0.70 {
		source := entry.PrimarySource()
		if source.Source != "" {
			reasons = append(reasons, domain.RecommendationReason{
				Kind:   "hot",
				Value:  string(source.Source),
				Label:  "hot " + string(source.Source),
				Weight: hotScore * 0.25,
			})
		}
	}
	return recommend.TopReasons(reasons, 4)
}

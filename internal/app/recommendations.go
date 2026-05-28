package app

import (
	"context"
	"sort"
	"time"

	"github.com/AIluffy/tildewire/internal/domain"
	"github.com/AIluffy/tildewire/internal/recommend"
	"github.com/AIluffy/tildewire/internal/score"
)

const recommendLatestFeedLimit = 250
const recommendCandidateSourceLimit = 50
const recommendSourceSoftLimit = 4
const recommendDisplayLimit = 10

type recommendationScoreCandidate struct {
	score  domain.RecommendationScore
	source domain.SourceID
}

func (s *Service) recomputeRecommendations(ctx context.Context) error {
	entries, err := s.recommendationCandidateEntries(ctx)
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
	scoreCandidates := make([]recommendationScoreCandidate, 0, len(entries))
	for _, entry := range entries {
		interestScore, hasPositiveSignal, reasons := recommendationInterestScore(entry, activeRules, profile)
		if !hasPositiveSignal || interestScore <= 0 {
			continue
		}
		hotScore := score.Hot(entry, now)
		reasons = recommendationReasons(entry, reasons, hotScore)
		scoreCandidates = append(scoreCandidates, recommendationScoreCandidate{
			score: domain.RecommendationScore{
				ItemID:        entry.Item.ID,
				Score:         interestScore + 0.25*hotScore,
				InterestScore: interestScore,
				HotScore:      hotScore,
				Reasons:       reasons,
				ComputedAt:    now,
			},
			source: entry.PrimarySource().Source,
		})
	}
	scores := selectRecommendationScores(scoreCandidates, recommendDisplayLimit)
	s.storeMu.Lock()
	defer s.storeMu.Unlock()
	return s.store.ReplaceRecommendationScores(ctx, scores)
}

func (s *Service) recommendationCandidateEntries(ctx context.Context) ([]domain.FeedEntry, error) {
	entriesByID := make(map[string]domain.FeedEntry)
	orderedIDs := make([]string, 0, recommendLatestFeedLimit)
	addEntries := func(entries []domain.FeedEntry) {
		for _, entry := range entries {
			if entry.Item.ID == "" {
				continue
			}
			if _, ok := entriesByID[entry.Item.ID]; ok {
				continue
			}
			entriesByID[entry.Item.ID] = entry
			orderedIDs = append(orderedIDs, entry.Item.ID)
		}
	}

	latestEntries, err := s.store.ListFeed(ctx, domain.FeedQuery{Limit: recommendLatestFeedLimit})
	if err != nil {
		return nil, err
	}
	addEntries(latestEntries)
	for _, source := range s.sourceIDsSnapshot() {
		sourceViews := s.recommendationCandidateSourceViews(source)
		for _, sourceView := range sourceViews {
			entries, err := s.store.ListFeed(ctx, domain.FeedQuery{
				Source:     source,
				SourceView: sourceView,
				Limit:      recommendCandidateSourceLimit,
			})
			if err != nil {
				return nil, err
			}
			addEntries(entries)
		}
	}

	entries := make([]domain.FeedEntry, 0, len(orderedIDs))
	for _, itemID := range orderedIDs {
		entries = append(entries, entriesByID[itemID])
	}
	return entries, nil
}

func (s *Service) recommendationCandidateSourceViews(source domain.SourceID) []string {
	sourceViews := s.primarySourceViewKeys(source)
	if len(sourceViews) == 0 {
		return []string{""}
	}
	return sourceViews
}

func selectRecommendationScores(candidates []recommendationScoreCandidate, limit int) []domain.RecommendationScore {
	if len(candidates) == 0 || limit <= 0 {
		return nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left := candidates[i].score
		right := candidates[j].score
		switch {
		case left.Score != right.Score:
			return left.Score > right.Score
		case left.InterestScore != right.InterestScore:
			return left.InterestScore > right.InterestScore
		case left.HotScore != right.HotScore:
			return left.HotScore > right.HotScore
		default:
			return left.ItemID < right.ItemID
		}
	})

	selected := make([]domain.RecommendationScore, 0, min(limit, len(candidates)))
	selectedIDs := make(map[string]bool, min(limit, len(candidates)))
	sourceCounts := make(map[domain.SourceID]int)
	add := func(candidate recommendationScoreCandidate) bool {
		if selectedIDs[candidate.score.ItemID] {
			return false
		}
		selectedIDs[candidate.score.ItemID] = true
		sourceCounts[candidate.source]++
		selected = append(selected, candidate.score)
		return len(selected) == limit
	}
	for _, candidate := range candidates {
		if sourceCounts[candidate.source] >= recommendSourceSoftLimit {
			continue
		}
		if add(candidate) {
			return selected
		}
	}
	for _, candidate := range candidates {
		if add(candidate) {
			return selected
		}
	}
	return selected
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

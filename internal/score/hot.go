package score

import (
	"math"
	"time"

	"github.com/AIluffy/tildewire/internal/domain"
)

const (
	// sourceRankWeight favors items ranked highly within their native source.
	sourceRankWeight = 0.55
	// recencyWeight keeps recently observed items prominent without hiding older durable signals.
	recencyWeight = 0.25
	// metricWeight captures source-native popularity metrics such as points, upvotes, or stars.
	metricWeight = 0.15
	// sourceBonusWeight rewards items that appear in multiple sources.
	sourceBonusWeight = 0.05
	// readPenaltyWeight lowers already-read items while keeping them visible when other signals are strong.
	readPenaltyWeight = 0.20
)

// Hot computes the explainable v0.1 merged-feed score.
func Hot(entry domain.FeedEntry, now time.Time) float64 {
	sourceRank := 0.0
	primary := entry.PrimarySource()
	if primary.SourceRank > 0 {
		sourceRank = 1 / math.Sqrt(float64(primary.SourceRank))
	}

	recency := 0.0
	seen := entry.Item.LastSeenAt
	if !seen.IsZero() {
		hours := math.Max(now.Sub(seen).Hours(), 0)
		recency = 1 / (1 + hours/24)
	}

	metric := metricScore(primary.Metrics)
	if metric == 0 {
		metric = metricScore(entry.Item.Metrics)
	}

	sourceBonus := 0.0
	if len(entry.Sources) > 1 {
		sourceBonus = math.Min(float64(len(entry.Sources)-1)*0.25, 1)
	}

	readPenalty := 0.0
	if entry.State.Read {
		readPenalty = readPenaltyWeight
	}

	return sourceRankWeight*sourceRank + recencyWeight*recency + metricWeight*metric + sourceBonusWeight*sourceBonus - readPenalty
}

func metricScore(metrics domain.Metrics) float64 {
	switch {
	case metrics.Score != nil:
		return clampLog(*metrics.Score, 500)
	case metrics.Upvotes != nil:
		return clampLog(float64(*metrics.Upvotes), 500)
	case metrics.StarsToday != nil:
		return clampLog(float64(*metrics.StarsToday), 1000)
	case metrics.Stars != nil:
		return clampLog(float64(*metrics.Stars), 100000)
	default:
		return 0
	}
}

func clampLog(value, maxValue float64) float64 {
	if value <= 0 {
		return 0
	}
	return math.Min(math.Log1p(value)/math.Log1p(maxValue), 1)
}

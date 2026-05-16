package score

import (
	"testing"
	"time"

	"github.com/AIluffy/tildewire/internal/domain"
)

func TestHotRewardsRankAndPenalizesRead(t *testing.T) {
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	entry := domain.FeedEntry{
		Item: domain.FeedItem{LastSeenAt: now.Add(-time.Hour)},
		Sources: []domain.ItemSource{{
			SourceRank: 1,
			Metrics:    domain.Metrics{Score: domain.Float64Ptr(100)},
		}},
	}
	unread := Hot(entry, now)
	entry.State.Read = true
	read := Hot(entry, now)
	if unread <= read {
		t.Fatalf("unread hot score %.3f should exceed read score %.3f", unread, read)
	}
}

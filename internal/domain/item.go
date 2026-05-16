package domain

import (
	"encoding/json"
	"time"
)

// FeedItem is a normalized item shared across sources.
type FeedItem struct {
	ID           string
	CanonicalKey string

	Title        string
	Subtitle     string
	Summary      string
	URL          string
	CanonicalURL string
	CommentsURL  string

	ItemType     string
	Author       string
	Organization string
	Language     string
	Tags         []string

	PublishedAt *time.Time
	FirstSeenAt time.Time
	LastSeenAt  time.Time

	Metrics  Metrics
	Refs     Refs
	Metadata json.RawMessage
	SimHash  string
	Sources  []ItemSource
}

// ItemSource preserves source-native context for a normalized item.
type ItemSource struct {
	ItemID      string
	Source      SourceID
	SourceView  string
	SourceIDRaw string
	SourceRank  int
	SourceURL   string
	Metrics     Metrics
	Raw         json.RawMessage
	SeenAt      time.Time
}

// Metrics stores comparable signals without forcing every source to provide every field.
type Metrics struct {
	Score       *float64 `json:"score,omitempty"`
	Upvotes     *int64   `json:"upvotes,omitempty"`
	Comments    *int64   `json:"comments,omitempty"`
	Stars       *int64   `json:"stars,omitempty"`
	StarsToday  *int64   `json:"stars_today,omitempty"`
	Forks       *int64   `json:"forks,omitempty"`
	GitHubStars *int64   `json:"github_stars,omitempty"`
}

// Refs stores source-specific stable identifiers.
type Refs struct {
	Repo            string `json:"repo,omitempty"`
	ArxivID         string `json:"arxiv_id,omitempty"`
	PaperID         string `json:"paper_id,omitempty"`
	HNID            string `json:"hn_id,omitempty"`
	LobstersID      string `json:"lobsters_id,omitempty"`
	ProductHuntID   string `json:"product_hunt_id,omitempty"`
	ProductHuntSlug string `json:"product_hunt_slug,omitempty"`
}

// ItemState is durable user state for an item.
type ItemState struct {
	ItemID   string
	Read     bool
	Saved    bool
	Hidden   bool
	ReadAt   *time.Time
	SavedAt  *time.Time
	HiddenAt *time.Time
	Note     string
}

// FeedEntry combines normalized data, source context, and local state for display.
type FeedEntry struct {
	Item     FeedItem
	Sources  []ItemSource
	State    ItemState
	HotScore float64
}

// PrimarySource returns the highest-ranked source context for this entry.
func (e FeedEntry) PrimarySource() ItemSource {
	if len(e.Sources) == 0 {
		return ItemSource{}
	}
	best := e.Sources[0]
	for _, source := range e.Sources[1:] {
		if source.SourceRank > 0 && (best.SourceRank == 0 || source.SourceRank < best.SourceRank) {
			best = source
		}
	}
	return best
}

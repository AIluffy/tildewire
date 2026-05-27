package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/AIluffy/tildewire/internal/domain"
)

// LoadDetail loads source-native detail content for a feed entry.
func (s *Service) LoadDetail(ctx context.Context, entry domain.FeedEntry) (domain.ItemDetail, error) {
	_ = s.RecordItemEvent(ctx, domain.ItemEvent{
		ItemID:    entry.Item.ID,
		EventType: domain.ItemEventDetailOpen,
		Source:    entry.PrimarySource().Source,
	})
	detail := baseDetail(entry)
	if detail.LoadedAt.IsZero() {
		detail.LoadedAt = time.Now().UTC()
	}
	var errs []error
	for _, adapter := range s.adapters {
		detailer, ok := adapter.(DetailAdapter)
		if !ok || !entryUsesDetailSource(entry, adapter.Source()) {
			continue
		}
		next, err := detailer.Detail(ctx, entry, s.client)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s detail: %w", adapter.Source(), err))
			continue
		}
		detail = mergeDetail(detail, next, adapter.Source())
	}
	return detail, errors.Join(errs...)
}

func baseDetail(entry domain.FeedEntry) domain.ItemDetail {
	return domain.ItemDetail{
		ItemID: entry.Item.ID,
		Title:  entry.Item.Title,
		URL:    entry.Item.URL,
	}
}

func mergeDetail(current, next domain.ItemDetail, source domain.SourceID) domain.ItemDetail {
	if current.ItemID == "" {
		current.ItemID = next.ItemID
	}
	if current.Title == "" {
		current.Title = next.Title
	}
	if current.URL == "" {
		current.URL = next.URL
	}
	if next.LoadedAt.After(current.LoadedAt) {
		current.LoadedAt = next.LoadedAt
	}
	current.Sections = append(current.Sections, next.Sections...)
	current.Comments = append(current.Comments, next.Comments...)
	if len(next.Sections) > 0 || len(next.Comments) > 0 {
		current.Providers = append(current.Providers, source)
	}
	return current
}

func entryUsesDetailSource(entry domain.FeedEntry, source domain.SourceID) bool {
	if source == domain.SourceGitHub && entry.Item.Refs.Repo != "" {
		return true
	}
	if source == domain.SourceHuggingFace && (entry.Item.Refs.PaperID != "" || entry.Item.Refs.ArxivID != "") {
		return true
	}
	for _, itemSource := range entry.Sources {
		if itemSource.Source == source {
			return true
		}
	}
	return false
}

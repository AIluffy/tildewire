package app

import (
	"context"

	"github.com/zhangxueai/tildewire/internal/domain"
)

// ClearCache removes refreshable cached data and returns the updated visible snapshot.
func (s *Service) ClearCache(ctx context.Context, view domain.SourceID, filter FeedFilter) (Snapshot, error) {
	return s.reloadAfter(ctx, view, filter, s.store.ClearCache)
}

// SetSaved toggles saved state.
func (s *Service) SetSaved(ctx context.Context, itemID string, saved bool, view domain.SourceID, filter FeedFilter) (Snapshot, error) {
	return s.reloadAfter(ctx, view, filter, func(ctx context.Context) error {
		return s.store.SetSaved(ctx, itemID, saved)
	})
}

// SetRead toggles read state.
func (s *Service) SetRead(ctx context.Context, itemID string, read bool, view domain.SourceID, filter FeedFilter) (Snapshot, error) {
	return s.reloadAfter(ctx, view, filter, func(ctx context.Context) error {
		return s.store.SetRead(ctx, itemID, read)
	})
}

// SetHidden toggles hidden state.
func (s *Service) SetHidden(ctx context.Context, itemID string, hidden bool, view domain.SourceID, filter FeedFilter) (Snapshot, error) {
	return s.reloadAfter(ctx, view, filter, func(ctx context.Context) error {
		return s.store.SetHidden(ctx, itemID, hidden)
	})
}

// CreatePersonalizationRule persists a rule and reloads the current feed.
func (s *Service) CreatePersonalizationRule(ctx context.Context, rule domain.PersonalizationRule, view domain.SourceID, filter FeedFilter) (Snapshot, error) {
	return s.reloadAfter(ctx, view, filter, func(ctx context.Context) error {
		_, err := s.store.CreatePersonalizationRule(ctx, rule)
		return err
	})
}

// UpdatePersonalizationRule replaces a rule and reloads the current feed.
func (s *Service) UpdatePersonalizationRule(ctx context.Context, id int64, rule domain.PersonalizationRule, view domain.SourceID, filter FeedFilter) (Snapshot, error) {
	return s.reloadAfter(ctx, view, filter, func(ctx context.Context) error {
		_, err := s.store.UpdatePersonalizationRule(ctx, id, rule)
		return err
	})
}

// SetPersonalizationRuleEnabled toggles a rule and reloads the current feed.
func (s *Service) SetPersonalizationRuleEnabled(ctx context.Context, id int64, enabled bool, view domain.SourceID, filter FeedFilter) (Snapshot, error) {
	return s.reloadAfter(ctx, view, filter, func(ctx context.Context) error {
		return s.store.SetPersonalizationRuleEnabled(ctx, id, enabled)
	})
}

// DeletePersonalizationRule removes a rule and reloads the current feed.
func (s *Service) DeletePersonalizationRule(ctx context.Context, id int64, view domain.SourceID, filter FeedFilter) (Snapshot, error) {
	return s.reloadAfter(ctx, view, filter, func(ctx context.Context) error {
		return s.store.DeletePersonalizationRule(ctx, id)
	})
}

// IgnoreDedupeCandidate hides a duplicate suggestion and reloads the current feed.
func (s *Service) IgnoreDedupeCandidate(ctx context.Context, key string, view domain.SourceID, filter FeedFilter) (Snapshot, error) {
	return s.reloadAfter(ctx, view, filter, func(ctx context.Context) error {
		return s.store.IgnoreDedupeCandidate(ctx, key)
	})
}

func (s *Service) reloadAfter(ctx context.Context, view domain.SourceID, filter FeedFilter, mutate func(context.Context) error) (Snapshot, error) {
	if err := mutate(ctx); err != nil {
		return Snapshot{}, err
	}
	return s.LoadFeed(ctx, view, filter)
}

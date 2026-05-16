package store

import (
	"context"

	"github.com/AIluffy/tildewire/internal/store/generated"
)

// SetSaved persists saved state.
func (s *Store) SetSaved(ctx context.Context, itemID string, saved bool) error {
	if err := s.queries.EnsureItemState(ctx, itemID); err != nil {
		return err
	}
	return s.queries.SetSavedState(ctx, generated.SetSavedStateParams{
		Saved:   boolInt(saved),
		SavedAt: stateTimestamp(saved),
		ItemID:  itemID,
	})
}

// SetRead persists read state.
func (s *Store) SetRead(ctx context.Context, itemID string, read bool) error {
	if err := s.queries.EnsureItemState(ctx, itemID); err != nil {
		return err
	}
	return s.queries.SetReadState(ctx, generated.SetReadStateParams{
		Read:   boolInt(read),
		ReadAt: stateTimestamp(read),
		ItemID: itemID,
	})
}

// SetHidden persists hidden state.
func (s *Store) SetHidden(ctx context.Context, itemID string, hidden bool) error {
	if err := s.queries.EnsureItemState(ctx, itemID); err != nil {
		return err
	}
	return s.queries.SetHiddenState(ctx, generated.SetHiddenStateParams{
		Hidden:   boolInt(hidden),
		HiddenAt: stateTimestamp(hidden),
		ItemID:   itemID,
	})
}

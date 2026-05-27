package store

import (
	"context"
	"strings"
	"time"

	"github.com/AIluffy/tildewire/internal/domain"
	"github.com/AIluffy/tildewire/internal/store/generated"
)

// SetSaved persists saved state.
func (s *Store) SetSaved(ctx context.Context, itemID string, saved bool) error {
	eventType := domain.ItemEventUnsave
	if saved {
		eventType = domain.ItemEventSave
	}
	return s.setStateWithEvent(ctx, itemID, eventType, func(ctx context.Context, q *generated.Queries) error {
		return q.SetSavedState(ctx, generated.SetSavedStateParams{
			Saved:   boolInt(saved),
			SavedAt: stateTimestamp(saved),
			ItemID:  itemID,
		})
	})
}

// SetRead persists read state.
func (s *Store) SetRead(ctx context.Context, itemID string, read bool) error {
	eventType := domain.ItemEventUnread
	if read {
		eventType = domain.ItemEventRead
	}
	return s.setStateWithEvent(ctx, itemID, eventType, func(ctx context.Context, q *generated.Queries) error {
		return q.SetReadState(ctx, generated.SetReadStateParams{
			Read:   boolInt(read),
			ReadAt: stateTimestamp(read),
			ItemID: itemID,
		})
	})
}

// SetHidden persists hidden state.
func (s *Store) SetHidden(ctx context.Context, itemID string, hidden bool) error {
	eventType := domain.ItemEventRestore
	if hidden {
		eventType = domain.ItemEventHide
	}
	return s.setStateWithEvent(ctx, itemID, eventType, func(ctx context.Context, q *generated.Queries) error {
		return q.SetHiddenState(ctx, generated.SetHiddenStateParams{
			Hidden:   boolInt(hidden),
			HiddenAt: stateTimestamp(hidden),
			ItemID:   itemID,
		})
	})
}

// RecordItemEvent persists one explicit user interaction for recommendation scoring.
func (s *Store) RecordItemEvent(ctx context.Context, event domain.ItemEvent) error {
	event = normalizeItemEvent(event)
	if event.ItemID == "" || event.EventType == "" {
		return nil
	}
	return s.queries.InsertItemEvent(ctx, generated.InsertItemEventParams{
		ItemID:     event.ItemID,
		EventType:  string(event.EventType),
		Source:     string(event.Source),
		View:       string(event.View),
		OccurredAt: formatTime(event.OccurredAt),
	})
}

func (s *Store) setStateWithEvent(ctx context.Context, itemID string, eventType domain.ItemEventType, setState func(context.Context, *generated.Queries) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	queries := generated.New(tx)
	if err := queries.EnsureItemState(ctx, itemID); err != nil {
		return err
	}
	if err := setState(ctx, queries); err != nil {
		return err
	}
	if err := insertItemEvent(ctx, queries, domain.ItemEvent{ItemID: itemID, EventType: eventType}); err != nil {
		return err
	}
	return tx.Commit()
}

func insertItemEvent(ctx context.Context, queries *generated.Queries, event domain.ItemEvent) error {
	event = normalizeItemEvent(event)
	if event.ItemID == "" || event.EventType == "" {
		return nil
	}
	return queries.InsertItemEvent(ctx, generated.InsertItemEventParams{
		ItemID:     event.ItemID,
		EventType:  string(event.EventType),
		Source:     string(event.Source),
		View:       string(event.View),
		OccurredAt: formatTime(event.OccurredAt),
	})
}

func normalizeItemEvent(event domain.ItemEvent) domain.ItemEvent {
	event.ItemID = strings.TrimSpace(event.ItemID)
	event.EventType = domain.ItemEventType(strings.ToLower(strings.TrimSpace(string(event.EventType))))
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	event.OccurredAt = event.OccurredAt.UTC()
	return event
}

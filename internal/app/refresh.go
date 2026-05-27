package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/AIluffy/tildewire/internal/domain"
	"github.com/AIluffy/tildewire/internal/httpx"
)

// Refresh refreshes enabled source scopes, persists data, and returns a new snapshot.
func (s *Service) Refresh(ctx context.Context, view domain.SourceID, filter FeedFilter, options RefreshOptions) (Snapshot, error) {
	filter = normalizeViewFilter(view, filter)
	options.Mode = normalizeRefreshMode(options.Mode)
	var refreshErrs []error
	jobs := make([]refreshJob, 0, len(s.adapters))
	for _, adapter := range s.adapters {
		if !s.sourceEnabled(adapter.Source()) {
			continue
		}
		scopes := refreshScopes(adapter, view, filter, options.Mode)
		if len(scopes) == 0 {
			continue
		}
		jobs = append(jobs, refreshJob{adapter: adapter, scopes: scopes})
	}
	for _, err := range s.refreshAdapters(ctx, jobs, options) {
		if err != nil {
			refreshErrs = append(refreshErrs, err)
		}
	}
	snapshot, loadErr := s.loadFeedAfterRefresh(ctx, view, filter)
	if loadErr != nil {
		refreshErrs = append(refreshErrs, loadErr)
	}
	return snapshot, errors.Join(refreshErrs...)
}

type refreshJob struct {
	adapter SourceAdapter
	scopes  []domain.FetchScope
}

func (s *Service) refreshAdapters(ctx context.Context, jobs []refreshJob, options RefreshOptions) []error {
	if len(jobs) == 0 {
		return nil
	}
	errs := make([]error, len(jobs))
	var wg sync.WaitGroup
	for idx, job := range jobs {
		idx, job := idx, job
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[idx] = s.refreshAdapter(ctx, job.adapter, job.scopes, options)
		}()
	}
	wg.Wait()
	return errs
}

func (s *Service) refreshAdapter(ctx context.Context, adapter SourceAdapter, scopes []domain.FetchScope, options RefreshOptions) error {
	source := adapter.Source()
	if len(scopes) == 0 {
		return nil
	}
	if authAdapter, ok := adapter.(OptionalAuthAdapter); ok {
		if required, reason := authAdapter.AuthRequired(); required {
			var errs []error
			if err := s.updateSourceStatus(ctx, source, domain.SourceStatusAuthRequired, reason); err != nil {
				errs = append(errs, err)
			}
			for _, scope := range scopes {
				startedAt := time.Now().UTC()
				if err := s.recordFetchEvent(ctx, source, scope, domain.SourceStatusAuthRequired, startedAt, 0, false, "", reason); err != nil {
					errs = append(errs, err)
				}
			}
			return errors.Join(errs...)
		}
	}
	if err := s.updateSourceStatus(ctx, source, domain.SourceStatusRefreshing, ""); err != nil {
		return err
	}
	var errs []error
	sourceStatus := domain.SourceStatusOK
	for _, scope := range scopes {
		startedAt := time.Now().UTC()
		scope.ForceRefresh = options.Force
		raw, err := adapter.Fetch(ctx, scope, s.client)
		if err != nil {
			status := classifyFetchError(err)
			_ = s.updateSourceStatus(ctx, source, status, err.Error())
			if recordErr := s.recordFetchEvent(ctx, source, scope, status, startedAt, 0, false, "", err.Error()); recordErr != nil {
				errs = append(errs, recordErr)
			}
			errs = append(errs, fmt.Errorf("%s fetch %s: %w", source, scope.View, err))
			continue
		}
		items, err := adapter.Normalize(ctx, scope, raw)
		if err != nil {
			_ = s.updateSourceStatus(ctx, source, domain.SourceStatusParserBroken, err.Error())
			if recordErr := s.recordFetchEvent(ctx, source, scope, domain.SourceStatusParserBroken, startedAt, 0, false, "", err.Error()); recordErr != nil {
				errs = append(errs, recordErr)
			}
			errs = append(errs, fmt.Errorf("%s normalize %s: %w", source, scope.View, err))
			continue
		}
		if err := s.replaceSourceViewFeedItems(ctx, source, sourceViewKey(scope), items); err != nil {
			_ = s.updateSourceStatus(ctx, source, domain.SourceStatusStale, err.Error())
			if recordErr := s.recordFetchEvent(ctx, source, scope, domain.SourceStatusStale, startedAt, 0, false, "", err.Error()); recordErr != nil {
				errs = append(errs, recordErr)
			}
			errs = append(errs, fmt.Errorf("%s upsert %s: %w", source, scope.View, err))
			continue
		}
		if raw.Stale {
			sourceStatus = staleSourceStatus(raw.StaleReason)
		}
		if err := s.updateSourceStatus(ctx, source, sourceStatus, ""); err != nil {
			errs = append(errs, err)
		}
		if err := s.recordFetchEvent(ctx, source, scope, sourceStatus, startedAt, len(items), raw.Stale, raw.StaleReason, ""); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Service) recordFetchEvent(ctx context.Context, source domain.SourceID, scope domain.FetchScope, status domain.SourceStatus, startedAt time.Time, itemCount int, stale bool, staleReason, errText string) error {
	finishedAt := time.Now().UTC()
	s.storeMu.Lock()
	defer s.storeMu.Unlock()
	return s.store.RecordFetchEvent(ctx, domain.FetchEvent{
		Source:      source,
		SourceView:  sourceViewKey(scope),
		Status:      status,
		StartedAt:   startedAt,
		FinishedAt:  finishedAt,
		Duration:    finishedAt.Sub(startedAt),
		ItemCount:   itemCount,
		Stale:       stale,
		StaleReason: staleReason,
		Error:       errText,
	})
}

func (s *Service) updateSourceStatus(ctx context.Context, source domain.SourceID, status domain.SourceStatus, lastErr string) error {
	s.storeMu.Lock()
	defer s.storeMu.Unlock()
	return s.store.UpdateSourceStatus(ctx, source, status, lastErr)
}

func (s *Service) replaceSourceViewFeedItems(ctx context.Context, source domain.SourceID, sourceView string, items []domain.FeedItem) error {
	s.storeMu.Lock()
	defer s.storeMu.Unlock()
	return s.store.ReplaceFeedItemsForSourceView(ctx, source, sourceView, items)
}

func (s *Service) loadFeedAfterRefresh(ctx context.Context, view domain.SourceID, filter FeedFilter) (Snapshot, error) {
	if ctx.Err() == nil {
		return s.LoadFeed(ctx, view, filter)
	}
	loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return s.LoadFeed(loadCtx, view, filter)
}

func classifyFetchError(err error) domain.SourceStatus {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "401"), strings.Contains(message, "auth required"), strings.Contains(message, "unauthorized"):
		return domain.SourceStatusAuthRequired
	case strings.Contains(message, "403"), strings.Contains(message, "429"), isLocalRateLimitWaitError(message):
		return domain.SourceStatusRateLimited
	default:
		return domain.SourceStatusNetworkError
	}
}

func isLocalRateLimitWaitError(message string) bool {
	return strings.Contains(message, "rate:") && strings.Contains(message, "would exceed context deadline")
}

func staleSourceStatus(reason string) domain.SourceStatus {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case httpx.StaleReasonRateLimited:
		return domain.SourceStatusRateLimited
	case httpx.StaleReasonNetworkError:
		return domain.SourceStatusNetworkError
	default:
		return domain.SourceStatusStale
	}
}

func normalizeRefreshMode(mode RefreshMode) RefreshMode {
	switch mode {
	case RefreshModeStartup, RefreshModeVisible, RefreshModeAll:
		return mode
	default:
		return RefreshModeVisible
	}
}

func refreshScopes(adapter SourceAdapter, view domain.SourceID, filter FeedFilter, mode RefreshMode) []domain.FetchScope {
	switch mode {
	case RefreshModeStartup:
		return primaryScopes(adapter)
	case RefreshModeVisible:
		if view == "" || view == domain.SourceAll {
			return primaryScopes(adapter)
		}
		if view != adapter.Source() {
			return nil
		}
		sourceView := filter.SourceView
		if sourceView == "" {
			if _, ok := explicitPrimaryScopes(adapter); ok {
				return primaryScopes(adapter)
			}
			sourceView = DefaultSourceView(view)
		}
		if scope, ok := ScopeForSourceView(view, sourceView); ok {
			if _, ok := explicitPrimaryScopes(adapter); ok {
				return dedupeScopes([]domain.FetchScope{scope})
			}
			return dedupeScopes([]domain.FetchScope{primaryScope(adapter), scope})
		}
		return primaryScopes(adapter)
	default:
		return adapter.DefaultScopes()
	}
}

func primaryScopes(adapter SourceAdapter) []domain.FetchScope {
	if scopes, ok := explicitPrimaryScopes(adapter); ok {
		return scopes
	}
	return dedupeScopes([]domain.FetchScope{primaryScope(adapter)})
}

func explicitPrimaryScopes(adapter SourceAdapter) ([]domain.FetchScope, bool) {
	primaryAdapter, ok := adapter.(PrimaryScopesAdapter)
	if !ok {
		return nil, false
	}
	scopes := dedupeScopes(primaryAdapter.PrimaryScopes())
	if len(scopes) == 0 {
		return nil, false
	}
	return scopes, true
}

func primaryScope(adapter SourceAdapter) domain.FetchScope {
	if scope, ok := ScopeForSourceView(adapter.Source(), DefaultSourceView(adapter.Source())); ok {
		return scope
	}
	scopes := adapter.DefaultScopes()
	if len(scopes) == 0 {
		return domain.FetchScope{Source: adapter.Source()}
	}
	return scopes[0]
}

func dedupeScopes(scopes []domain.FetchScope) []domain.FetchScope {
	seen := make(map[string]bool, len(scopes))
	deduped := make([]domain.FetchScope, 0, len(scopes))
	for _, scope := range scopes {
		key := string(scope.Source) + ":" + sourceViewKey(scope)
		if seen[key] {
			continue
		}
		seen[key] = true
		deduped = append(deduped, scope)
	}
	return deduped
}

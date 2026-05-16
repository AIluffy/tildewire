package store

import (
	"context"
	"strings"

	"github.com/zhangxueai/tildewire/internal/domain"
	"github.com/zhangxueai/tildewire/internal/store/generated"
)

// SourceCounts returns visible item counts for all and each source.
func (s *Store) SourceCounts(ctx context.Context) (map[domain.SourceID]int, error) {
	rows, err := s.queries.SourceCounts(ctx)
	if err != nil {
		return nil, err
	}

	counts := make(map[domain.SourceID]int)
	for _, row := range rows {
		counts[domain.SourceID(row.Source)] = int(row.Total)
	}
	return counts, nil
}

// SourceViewCount returns the visible item count for one source-native view.
func (s *Store) SourceViewCount(ctx context.Context, source domain.SourceID, sourceView string) (int, error) {
	total, err := s.queries.SourceViewCount(ctx, generated.SourceViewCountParams{
		Source:     string(source),
		SourceView: strings.ToLower(strings.TrimSpace(sourceView)),
	})
	if err != nil {
		return 0, err
	}
	return int(total), nil
}

// SourceViewCounts returns visible item counts grouped by source-native view.
func (s *Store) SourceViewCounts(ctx context.Context) (map[domain.SourceID]map[string]int, error) {
	rows, err := s.queries.SourceViewCounts(ctx)
	if err != nil {
		return nil, err
	}
	counts := make(map[domain.SourceID]map[string]int)
	for _, row := range rows {
		source := domain.SourceID(row.Source)
		if counts[source] == nil {
			counts[source] = make(map[string]int)
		}
		counts[source][row.SourceView] = int(row.Total)
	}
	return counts, nil
}

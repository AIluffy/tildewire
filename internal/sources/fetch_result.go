package sources

import (
	"github.com/AIluffy/tildewire/internal/domain"
	"github.com/AIluffy/tildewire/internal/httpx"
)

func fetchResultFromResponse(source domain.SourceID, scope domain.FetchScope, resp httpx.Response) *domain.FetchResult {
	return &domain.FetchResult{
		Source:      source,
		Scope:       scope,
		StatusCode:  resp.StatusCode,
		Body:        resp.Body,
		FetchedAt:   resp.FetchedAt,
		FromCache:   resp.FromCache,
		Stale:       resp.Stale,
		StaleReason: resp.StaleReason,
	}
}

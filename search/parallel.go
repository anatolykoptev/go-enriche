package search

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// Parallel runs all providers concurrently and merges results.
// Unlike Fallback (first success wins), Parallel collects results from
// ALL successful providers, deduplicates URLs, and merges context.
type Parallel struct {
	providers []Provider
}

// NewParallel creates a parallel provider that queries all providers concurrently.
func NewParallel(providers ...Provider) *Parallel {
	return &Parallel{providers: providers}
}

// providerResult holds one provider's output.
type providerResult struct {
	result *SearchResult
	err    error
}

// Search queries all providers in parallel, merges successful results.
// Returns error only if ALL providers fail.
func (p *Parallel) Search(ctx context.Context, query string, timeRange string) (*SearchResult, error) {
	if len(p.providers) == 0 {
		return nil, errors.New("parallel: no providers configured")
	}

	results := make([]providerResult, len(p.providers))
	var wg sync.WaitGroup

	for i, prov := range p.providers {
		wg.Add(1)
		go func(idx int, pr Provider) {
			defer wg.Done()
			defer func() {
				if rv := recover(); rv != nil {
					results[idx] = providerResult{err: fmt.Errorf("provider panic: %v", rv)}
				}
			}()
			r, err := pr.Search(ctx, query, timeRange)
			results[idx] = providerResult{result: r, err: err}
		}(i, prov)
	}
	wg.Wait()

	return p.merge(results)
}

// merge combines all successful results, deduplicating sources.
func (p *Parallel) merge(results []providerResult) (*SearchResult, error) {
	var (
		contextParts []string
		sources      []string
		seen         = make(map[string]bool)
		errs         []error
	)

	for _, pr := range results {
		if pr.err != nil {
			errs = append(errs, pr.err)
			continue
		}
		if pr.result == nil {
			continue
		}
		if pr.result.Context != "" {
			contextParts = append(contextParts, pr.result.Context)
		}
		for _, src := range pr.result.Sources {
			norm := normalizeURL(src)
			if norm != "" && !seen[norm] {
				seen[norm] = true
				sources = append(sources, src)
			}
		}
	}

	if len(sources) == 0 && len(contextParts) == 0 {
		return nil, fmt.Errorf("all providers failed: %w", errors.Join(errs...))
	}

	return &SearchResult{
		Context: strings.Join(contextParts, "\n\n"),
		Sources: sources,
	}, nil
}

package enriche

import (
	"net/http"
	"time"

	"github.com/anatolykoptev/go-enriche/cache"
	"github.com/anatolykoptev/go-enriche/fetch"
	"github.com/anatolykoptev/go-enriche/search"
)

// Option configures an Enricher.
type Option func(*Enricher)

// WithFetcher sets a custom Fetcher.
func WithFetcher(f *fetch.Fetcher) Option {
	return func(e *Enricher) { e.fetcher = f }
}

// WithStealth creates a Fetcher using a stealth HTTP client.
func WithStealth(c *http.Client) Option {
	return func(e *Enricher) {
		e.fetcher = fetch.NewFetcher(fetch.WithClient(c))
	}
}

// WithCache sets a Cache for enrichment results.
func WithCache(c cache.Cache) Option {
	return func(e *Enricher) { e.cache = c }
}

// WithCacheTTL sets the cache TTL for enrichment results.
func WithCacheTTL(d time.Duration) Option {
	return func(e *Enricher) { e.cacheTTL = d }
}

// WithSearch sets a search Provider for external context.
func WithSearch(p search.Provider) Option {
	return func(e *Enricher) { e.search = p }
}

// WithConcurrency sets the max concurrent enrichments in EnrichBatch.
func WithConcurrency(n int) Option {
	return func(e *Enricher) {
		if n > 0 {
			e.concurrency = n
		}
	}
}

package enriche

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/anatolykoptev/go-enriche/cache"
	"github.com/anatolykoptev/go-enriche/extract"
	"github.com/anatolykoptev/go-enriche/fetch"
	"github.com/anatolykoptev/go-enriche/maps"
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

// WithMaxContentLen truncates extracted content to n runes (word-boundary preferred).
// Default: 0 (no truncation).
func WithMaxContentLen(n int) Option {
	return func(e *Enricher) { e.maxContentLen = n }
}

// WithLogger sets a structured logger for debug-level observability.
// If l is nil, the default no-op logger is kept.
func WithLogger(l *slog.Logger) Option {
	return func(e *Enricher) {
		if l != nil {
			e.logger = l
		}
	}
}

// WithMetrics sets callback hooks for counters (cache hit/miss, errors).
func WithMetrics(m *Metrics) Option {
	return func(e *Enricher) { e.metrics = m }
}

// WithMapsChecker sets a maps.Checker for place status verification.
// Only effective for ModePlaces items. If the checker reports a place
// as permanently closed, enrichment short-circuits with StatusClosed.
func WithMapsChecker(c maps.Checker) Option {
	return func(e *Enricher) { e.mapsChecker = c }
}

// WithGeocoder sets a maps.Geocoder for automatic address→coordinates resolution.
// Only effective for ModePlaces items with a non-nil Facts.Address and nil Latitude.
func WithGeocoder(g *maps.Geocoder) Option {
	return func(e *Enricher) { e.geocoder = g }
}

// WithRetryOn403 enables retrying HTTP 403 responses. Use when the underlying
// HTTP client has proxy pool rotation, so each retry uses a different proxy.
// Adds one extra retry attempt (total: initial + 2 retries).
func WithRetryOn403() Option {
	return func(e *Enricher) { e.retryOn403 = true }
}

// WithFormat sets the output format for extracted content.
// Default is extract.FormatText (plain text, current behavior).
// Use extract.FormatMarkdown to preserve links, headings, and formatting.
func WithFormat(f extract.Format) Option {
	return func(e *Enricher) { e.format = f }
}

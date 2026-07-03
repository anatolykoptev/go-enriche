package enriche

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/anatolykoptev/go-enriche/cache"
	"github.com/anatolykoptev/go-enriche/extract"
	"github.com/anatolykoptev/go-enriche/fetch"
	"github.com/anatolykoptev/go-enriche/maps"
	"github.com/anatolykoptev/go-enriche/search"
	"github.com/anatolykoptev/go-kit/httputil"
)

// Option configures an Enricher.
type Option func(*Enricher)

// WithFetcher sets a custom Fetcher.
func WithFetcher(f *fetch.Fetcher) Option {
	return func(e *Enricher) { e.fetcher = f }
}

// WithStealth creates a Fetcher using a stealth HTTP client.
//
// fetch.WithClient REPLACES the Fetcher's client wholesale, which would
// otherwise silently bypass NewFetcher's default SSRF guard — a real escape
// hatch, since a stealth client is this org's production HTTP pattern
// (go-stealth/go-wowa) and the caller-supplied URL it fetches is exactly the
// untrusted input the guard exists for. httputil.NewSSRFGuardedClient
// composes the guard into c's Transport instead (connect-time DialContext
// wrap for a plain *http.Transport, or a request-level pre-check wrap for an
// opaque RoundTripper such as go-stealth's fingerprinting client) without
// touching c's TLS/JA3/proxy/middleware configuration.
func WithStealth(c *http.Client) Option {
	return func(e *Enricher) {
		e.fetcher = fetch.NewFetcher(fetch.WithClient(httputil.NewSSRFGuardedClient(c)))
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
// as permanently or temporarily closed, enrichment short-circuits with
// StatusClosed or StatusTemporaryClosed respectively.
func WithMapsChecker(c maps.Checker) Option {
	return func(e *Enricher) { e.mapsChecker = c }
}

// WithBrowserFetch sets a headless browser fallback for JS-heavy pages.
// When content extracted via HTTP fetch is thin (< 200 chars), the browser
// function re-renders the page and extraction is retried on the rendered HTML.
func WithBrowserFetch(fn func(ctx context.Context, url string) (string, error)) Option {
	return func(e *Enricher) { e.browserFetch = fn }
}

// WithRenderSkipDisabled toggles the ADR-8 render-skip kill-switch. When disabled
// is true, the trust-gated render-skip (rawContactsSufficient anchored-SiteNumber
// escape hatch, go-enriche v1.30.0) is bypassed and the headless render escalates
// exactly as it did pre-v1.30.0 — on thin content OR a missing single-winner Facts
// contact — regardless of any anchored raw SiteNumber. Default (disabled=false)
// keeps the skip ENABLED (the mcmedok 30s->~2s win). This is an OPS revert lever:
// the DATA consequence of a wrong skip is one-way (a number auto-written to a live
// paid card cannot be un-published by a code rollback), so the skip must be
// disable-able without a code change. Intended wiring: go-wp reads a container env
// (e.g. WP_RENDER_SKIP=off) and passes the boolean here — this package stays
// env-free (library hygiene).
func WithRenderSkipDisabled(disabled bool) Option {
	return func(e *Enricher) { e.renderSkipDisabled = disabled }
}

// WithFormat sets the output format for extracted content.
// Default is extract.FormatText (plain text, current behavior).
// Use extract.FormatMarkdown to preserve links, headings, and formatting.
func WithFormat(f extract.Format) Option {
	return func(e *Enricher) { e.format = f }
}

// WithSearchFetchLimit sets the max number of search result URLs to fetch
// when the item has no primary URL. Default: 5.
func WithSearchFetchLimit(n int) Option {
	return func(e *Enricher) {
		if n > 0 {
			e.searchFetchLimit = n
		}
	}
}

// WithOxBrowser enables ox-browser readability as an additional content
// extractor. Runs in parallel with trafilatura; the longer result wins.
func WithOxBrowser(baseURL string) Option {
	return func(e *Enricher) {
		if baseURL != "" {
			e.oxBrowser = fetch.NewOxBrowserClient(baseURL)
		}
	}
}

// WithTargetGuard overrides the SSRF safety check run on a URL before it is
// handed to an external render/extraction delegate (oxBrowser, browserFetch)
// — a hop this package does not control the outbound dial for, so
// fetch.Fetcher's own guarded transport cannot protect it. Defaults to
// go-kit httputil.CheckRawURL, the single, framework-owned SSRF check, which
// refuses loopback, private (RFC1918/RFC4193), link-local (including the
// 169.254.169.254 cloud-metadata address), unspecified, multicast, CGNAT,
// NAT64, 6to4, IPv4-compatible-IPv6, and non-http(s)-scheme targets.
//
// Production callers should not override this — it exists so tests can point
// oxBrowser/browserFetch at a local httptest server. Passing a nil fn is a
// no-op (the current guard, or the New() default, is kept).
func WithTargetGuard(fn func(ctx context.Context, rawURL string) error) Option {
	return func(e *Enricher) {
		if fn != nil {
			e.targetGuard = fn
		}
	}
}

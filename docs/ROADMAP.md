# go-enriche Roadmap

## Vision

Reusable Go library for web content enrichment. Fetch pages with stealth, extract article text (trafilatura-grade), parse structured data (JSON-LD + Microdata), search for context via SearXNG. Production-grade with graceful degradation.

Extracted from go-wp's monolithic `tool_enrich.go`. Three consumers: go-wp, go-nerv
, vaelor.

---

## Phase 0: Infrastructure ✅

**Goal**: Project scaffolding — builds, lints, CI passes on empty packages.

- [x] `go.mod` with all dependencies
- [x] `Makefile` (lint, test, build)
- [x] `.golangci.yml` (v2, from go-imagefy)
- [x] `.pre-commit-config.yaml`
- [x] `.github/workflows/ci.yml` (Go 1.24+1.25, golangci-lint)
- [x] `.gitignore`, `LICENSE.md`
- [x] Empty package stubs: enriche.go, types.go
- [x] Empty sub-packages: fetch/, extract/, structured/, search/, cache/
- [x] README.md (short, API examples)

**Success**: `make lint && make test` pass. CI green. ✅

## Phase 1: Extract ✅

**Goal**: Extract article text, structured facts, og:image, dates from HTML.

- [x] `extract/text.go` — go-trafilatura wrapper, `ExtractText(io.Reader, *url.URL) (*TextResult, error)`
- [x] `structured/parser.go` — astappiev/microdata wrapper, `Parse(io.Reader, contentType, pageURL) (*Data, error)`
- [x] `structured/types.go` — `Place`, `Article`, `Event`, `Organization` structs + converters
- [x] `extract/facts.go` — cascade: structured → regex fallback, pre-compiled patterns
- [x] `extract/ogimage.go` — go-imagefy `ExtractOGImageURL` wrapper
- [x] `extract/date.go` — go-htmldate via trafilatura metadata
- [x] `extract/regex.go` — pre-compiled patterns for address/phone/price (Russian + English)
- [x] 27 tests (6 structured + 21 extract), lint clean

**Success**: Extracts text + facts + image + date from JSON-LD, Microdata, and regex fallback. ✅

## Phase 2: Fetch ✅

**Goal**: HTTP fetch with status detection, stealth, singleflight dedup.

- [x] `fetch/status.go` — PageStatus enum (Active/NotFound/Redirect/Unreachable/WebsiteDown)
- [x] `fetch/fetcher.go` — `Fetcher{}`, `Fetch(ctx, url) (*FetchResult, error)`, singleflight
- [x] `fetch/stealth.go` — go-stealth integration, optional TLS fingerprinting
- [x] Custom `CheckRedirect` for domain-change detection
- [x] Max body bytes (2MB), timeout (15s)
- [x] Tests: httptest.Server — redirects, 404, timeouts, domain changes, singleflight

**Success**: Fetches real pages with correct status detection. Singleflight deduplicates parallel requests. ✅

## Phase 3: Search + Cache ✅

**Goal**: SearXNG context search and multi-layer caching.

- [x] `search/provider.go` — `Provider` interface, `SearchResult` type
- [x] `search/searxng.go` — SearXNG implementation, result aggregation, top N sources, URL dedup
- [x] `search/query.go` — mode-aware query building (news/places/events)
- [x] `cache/cache.go` — `Cache` interface
- [x] `cache/memory.go` — sync.Map L1 with TTL expiry
- [x] `cache/redis.go` — go-redis L2 with TTL
- [x] `cache/tiered.go` — L1 → L2 cascade with promotion
- [x] Tests: httptest for SearXNG, miniredis for Redis, unit for Memory/Tiered
- [x] 27 tests (12 search + 15 cache), lint clean

**Success**: SearXNG returns context + sources. Cache hit avoids re-fetch. Tiered cache works L1 → L2. ✅

## Phase 4: Orchestration ✅

**Goal**: Root Enricher — the public API.

- [x] `types.go` — Item, Result, Facts, ContentMeta, Mode, PageStatus re-export
- [x] `options.go` — functional options (WithFetcher, WithStealth, WithCache, WithCacheTTL, WithSearch, WithConcurrency)
- [x] `enriche.go` — `New(opts...)`, `Enrich(ctx, Item) (*Result, error)`, `EnrichBatch(ctx, []Item) []*Result`
- [x] EnrichBatch: semaphore-bounded concurrency (default 5)
- [x] Graceful degradation: no stealth/cache/search → degrade silently
- [x] Tests: 12 integration tests with mock Provider + httptest
- [x] Pipeline: cache check → fetch+extract → search → cache store

**Success**: `enriche.New(WithStealth(c), WithCache(c), WithSearch(s)).EnrichBatch(ctx, items)` returns enriched results. ✅

## Phase 5: Migration ✅

**Goal**: go-wp uses go-enriche instead of its own implementation.

- [x] Replace `tool_enrich.go` internals with `enriche.Enrich()` calls
- [x] Lazy `enriche.Enricher` init with `WithStealth()` + `WithSearch()` (no adapter needed)
- [x] Remove: `tool_enrich_extract.go` (259 lines), `tool_enrich_fetch.go` (110 lines)
- [x] Keep: `wp_enrich` MCP tool handler (thin wrapper), research store L1, statusWebsiteDown upgrade
- [x] Tests: 3/3 pass, 0 lint issues, full test suite green
- [x] Net: -391 lines removed from go-wp

**Success**: go-wp enrichment works identically via go-enriche. Old code deleted. Tests pass. ✅

---

## Phase 6: Hardening ✅

**Goal**: Fix regression from migration, add observability and robustness.

- [x] Regex facts from search context — `ExtractSnippetFacts()` with plain-text-safe patterns, integrated in `doSearch()`
- [x] `WithMaxContentLen(n)` option — `truncateRunes()` at rune boundaries with word-boundary preference
- [x] Observability: `WithLogger(*slog.Logger)` option — debug logging for cache hit/miss, fetch status, search results; discard handler default
- [x] Observability: `WithMetrics(*Metrics)` callback struct — `OnCacheHit`, `OnCacheMiss`, `OnFetchError`, `OnSearchError`; nil-safe
- [x] `errgroup` in `EnrichBatch` — `errgroup.SetLimit()` replaces WaitGroup+semaphore, context propagation
- [x] Retry with backoff for transient fetch errors — `fetchWithRetry()`, 1 retry with 100-300ms jitter, context-aware; `FetchResult.IsTransient()` classifier
- [x] 107 tests (23 new), lint clean

**Success**: No data loss vs old go-wp. Operators can debug enrichment pipeline. Batch enrichment respects cancellation. ✅

## Phase 7: Search Providers ✅

**Goal**: Pluggable search beyond SearXNG.

- [x] Rate limiter for search providers — `search.NewRateLimited(provider, rps, burst)` token bucket via `golang.org/x/time/rate`
- [x] Brave Search provider — `search.NewBrave(apiKey)` implementing `Provider`
- [x] Google Custom Search provider — `search.NewGoogle(apiKey, cx)` implementing `Provider`
- [x] Multi-provider fallback — `search.NewFallback(primary, fallbacks...)` try in order, `errors.Join` on all-fail
- [x] 126 tests (19 new), lint clean

**Success**: Multiple search backends, rate-limited, with automatic fallover. ✅

## Phase 8: Direct Search Scrapers ✅

**Goal**: Free search without SearXNG or API keys via go-stealth TLS fingerprinting.

- [x] `search/doer.go` — `BrowserDoer` interface + `ChromeHeaders()` helper
- [x] `search/ddg.go` — DDG HTML lite scraper implementing `Provider`, goquery parsing, URL unwrapping
- [x] `search/startpage.go` — Startpage Direct scraper implementing `Provider`, goquery parsing
- [x] Tests: 7 new (4 DDG + 3 Startpage), lint clean
- [x] 130 total tests, race-clean

**Success**: DDG and Startpage as Provider implementations. Works without SearXNG or API keys. ✅

## Phase 9: Proxy Fallback (Tor + ProxyPool) ✅

**Goal**: Tor as free proxy fallback when Webshare (paid) returns 402, with ProxyPool rotation.

- [x] `proxypool.NewStatic(urls ...string)` in go-stealth v1.1.0 — round-robin pool for static URLs
- [x] Tor SOCKS5 Docker service (`socks5://tor:9050`) — free, always available
- [x] `WithDDGProxyPool(pool)` + `WithStartpageProxyPool(pool)` — ProxyPoolProvider options
- [x] `ProxyPoolProvider` interface in `search/doer.go` — compatible with stealth.ProxyPoolProvider
- [x] go-wp wired: DDG → Startpage → SearXNG fallback chain, Webshare → Tor proxy fallback
- [x] 134+ tests, lint clean

**Success**: Direct scrapers use proxy pool with automatic Webshare → Tor fallback. Server IP never exposed. ✅

---

## Future

- go-nerv adapter (Phase 3+ of go-nerv roadmap)
- vaelor integration
- JavaScript rendering via go-rod (optional Fetcher)
- Sitemap-based batch enrichment
- Webhook/callback for async enrichment pipelines

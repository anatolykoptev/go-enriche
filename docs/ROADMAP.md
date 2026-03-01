# go-enriche Roadmap

## Vision

Reusable Go library for web content enrichment. Fetch pages with stealth, extract article text (trafilatura-grade), parse structured data (JSON-LD + Microdata), search for context via SearXNG. Production-grade with graceful degradation.

Extracted from go-wp's monolithic `tool_enrich.go`. Three consumers: go-wp, go-content, vaelor.

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

## Phase 5: Migration

**Goal**: go-wp uses go-enriche instead of its own implementation.

- [ ] `go-wp/internal/enrichadapter/adapter.go` — bridges engine.Cache → cache.Cache
- [ ] Replace `tool_enrich.go` internals with `enriche.Enrich()` calls
- [ ] Remove: `extractArticleText`, `applyLDJSON`, `applyRegexFacts`, `fetchPageWithStatus`, `fetchSearxngContext`
- [ ] Keep: `wp_enrich` MCP tool handler (thin wrapper)
- [ ] Tests: verify go-wp enrichment still works via adapter

**Success**: go-wp enrichment works identically via go-enriche. Old code deleted. Tests pass.

---

## Future

- go-content adapter (Phase 3+ of go-content roadmap)
- vaelor integration
- Additional SearchProviders (Brave, Google)
- JavaScript rendering via go-rod (optional Fetcher)
- Sitemap-based batch enrichment

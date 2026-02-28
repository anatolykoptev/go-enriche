# go-enriche Roadmap

## Vision

Reusable Go library for web content enrichment. Fetch pages with stealth, extract article text (trafilatura-grade), parse structured data (JSON-LD + Microdata), search for context via SearXNG. Production-grade with graceful degradation.

Extracted from go-wp's monolithic `tool_enrich.go`. Three consumers: go-wp, go-content, vaelor.

---

## Phase 0: Infrastructure

**Goal**: Project scaffolding — builds, lints, CI passes on empty packages.

- [ ] `go.mod` with all dependencies
- [ ] `Makefile` (lint, test, build)
- [ ] `.golangci.yml` (v2, from go-imagefy)
- [ ] `.pre-commit-config.yaml`
- [ ] `.github/workflows/ci.yml` (Go 1.24+1.25, golangci-lint)
- [ ] `.gitignore`, `LICENSE.md`
- [ ] Empty package stubs: enriche.go, types.go, options.go
- [ ] Empty sub-packages: fetch/, extract/, structured/, search/, cache/
- [ ] README.md (short, API examples)

**Success**: `make lint && make test` pass. CI green.

## Phase 1: Extract

**Goal**: Extract article text, structured facts, og:image, dates from HTML.

- [ ] `extract/text.go` — go-trafilatura wrapper, `ExtractText(io.Reader, *url.URL) (*TextResult, error)`
- [ ] `structured/parser.go` — astappiev/microdata wrapper, `Parse(html, contentType, pageURL) (*Data, error)`
- [ ] `structured/place.go` — `Place` struct + `FirstPlace()` converter
- [ ] `structured/article.go` — `Article` struct + `FirstArticle()` converter
- [ ] `structured/event.go` — `Event` struct + `FirstEvent()` converter
- [ ] `structured/org.go` — `Organization` struct + `FirstOrganization()` converter
- [ ] `extract/facts.go` — cascade: structured → regex fallback, pre-compiled patterns
- [ ] `extract/ogimage.go` — go-imagefy `ExtractOGImageURL` wrapper
- [ ] `extract/date.go` — go-htmldate wrapper via trafilatura metadata
- [ ] Tests: HTML fixtures in testdata/, table-driven tests per function

**Success**: Given real HTML (fontanka.ru, yandex maps, schema.org page), extracts text + facts + image + date accurately.

## Phase 2: Fetch

**Goal**: HTTP fetch with status detection, stealth, singleflight dedup.

- [ ] `fetch/status.go` — PageStatus enum (Active/NotFound/Redirect/Unreachable/WebsiteDown)
- [ ] `fetch/fetcher.go` — `Fetcher{}`, `Fetch(ctx, url) (*FetchResult, error)`, singleflight
- [ ] `fetch/stealth.go` — go-stealth integration, optional TLS fingerprinting
- [ ] Custom `CheckRedirect` for domain-change detection
- [ ] Max body bytes (2MB), timeout (15s)
- [ ] Tests: httptest.Server — redirects, 404, timeouts, domain changes

**Success**: Fetches real pages with correct status detection. Singleflight deduplicates parallel requests.

## Phase 3: Search + Cache

**Goal**: SearXNG context search and multi-layer caching.

- [ ] `search/provider.go` — `Provider` interface
- [ ] `search/searxng.go` — SearXNG implementation, result aggregation, top N sources
- [ ] `search/query.go` — mode-aware query building (news/places/events)
- [ ] `cache/cache.go` — `Cache` interface
- [ ] `cache/memory.go` — sync.Map L1
- [ ] `cache/redis.go` — go-redis L2 with TTL
- [ ] `cache/tiered.go` — L1 → L2 cascade
- [ ] Tests: httptest for SearXNG, miniredis for Redis, unit for Memory/Tiered

**Success**: SearXNG returns context + sources. Cache hit avoids re-fetch. Tiered cache works L1 → L2.

## Phase 4: Orchestration

**Goal**: Root Enricher — the public API.

- [ ] `types.go` — Item, Result, Facts, ContentMeta, Mode, PageStatus re-export
- [ ] `options.go` — functional options (WithCache, WithStealth, WithSearch, etc.)
- [ ] `enriche.go` — `New(opts...)`, `Enrich(ctx, Item) (*Result, error)`, `EnrichBatch(ctx, []Item) []*Result`
- [ ] EnrichBatch: semaphore-bounded concurrency (default 5)
- [ ] Graceful degradation: no stealth/cache/search → degrade silently
- [ ] Tests: integration with mock Fetcher + mock Provider

**Success**: `enriche.New(WithStealth(c), WithCache(c), WithSearch(s)).EnrichBatch(ctx, items)` returns enriched results.

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

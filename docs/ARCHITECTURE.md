# go-enriche Architecture

## What It Is

Standalone Go library for web content enrichment: fetch pages, extract article text, parse structured data (JSON-LD/Microdata), search for context, cache results. Stealth HTTP support via go-stealth.

Not an HTTP server. Not an MCP tool. A library that consumers (go-wp, go-nerv, vaelor) import and call.

## Three-Layer Model

```
L0 — Pure Logic (no I/O)
    structured/place.go, structured/article.go, extract/regex patterns
    → testable without mocks, goroutine-safe

L1 — HTTP Primitives
    fetch/fetcher.go, extract/text.go, search/searxng.go, cache/redis.go
    → uses net/http or injected clients, no external service knowledge

L2 — Orchestration
    enriche.go — Enricher.Enrich(), EnrichBatch()
    → coordinates L0/L1 via interfaces, dependency injection
```

## Data Flow

```
Item{Name, URL, City, Mode}
    │
    ├─ fetch.Fetch(URL) ──────────────→ FetchResult{HTML, Status, FinalURL}
    │                                         │
    │   ┌─────────────────────────────────────┘
    │   │
    │   ├─ extract.ExtractText(HTML)    → Content + ContentMeta
    │   ├─ extract.ExtractFacts(HTML)   → Facts (microdata → regex cascade)
    │   ├─ extract.ExtractOGImage(HTML) → *Image URL
    │   └─ extract.ExtractDate(HTML)    → *PublishedAt
    │
    ├─ search.Search(query) ──────────→ SearchContext + SearchSources
    │
    └─ Result{Content, Facts, Image, Metadata, SearchContext, Status, ...}
```

## Package Map

| Package | Layer | Purpose | Key dependency |
|---------|-------|---------|---------------|
| `fetch/` | L1 | HTTP fetch + status + stealth + singleflight | go-stealth, x/sync |
| `extract/` | L0/L1 | Text + facts + og:image + date from HTML | go-trafilatura, go-imagefy |
| `structured/` | L0 | Typed schema.org parsing (Place, Article, Event, Org) | astappiev/microdata |
| `search/` | L1 | SearXNG context search + query building | net/http |
| `cache/` | L1 | Cache interface + Memory (L1) + Redis (L2) + Tiered | go-redis |
| Root | L2 | Enricher orchestration + Config + Options | all of above |

## Interfaces

```go
// cache/cache.go
type Cache interface {
    Get(ctx context.Context, key string, dest any) bool
    Set(ctx context.Context, key string, value any, ttl time.Duration)
}

// search/provider.go
type Provider interface {
    Search(ctx context.Context, query string, opts SearchOpts) (*SearchResult, error)
}
```

## Graceful Degradation

Every optional component degrades silently:

| Missing | Behavior |
|---------|----------|
| Stealth client | Falls back to net/http |
| Cache | Full fetch every time |
| SearchProvider | No search context in Result |
| Fetch fails | Status=Unreachable, nil content |
| Extract fails | Empty content, pipeline continues |
| Panic in goroutine | recover + log, skip item |

## Consumer Integration

Adapter pattern — one file per consumer (~50 lines):

```go
// go-wp/internal/enrichadapter/adapter.go
cfg := enriche.New(
    enriche.WithCache(&engineCacheAdapter{}),
    enriche.WithStealth(engine.StealthClient()),
    enriche.WithSearch(enriche.NewSearXNG(engine.Cfg.SearxngURL, engine.Cfg.HTTPClient)),
)
result, _ := cfg.Enrich(ctx, enriche.Item{Name: "...", URL: "...", Mode: enriche.ModePlaces})
```

## Dependencies

| Dependency | Purpose | Optional |
|-----------|---------|----------|
| markusmobius/go-trafilatura | Article text extraction | No |
| astappiev/microdata | JSON-LD + Microdata parsing | No |
| anatolykoptev/go-stealth | TLS fingerprinting | Yes |
| anatolykoptev/go-imagefy | og:image extraction | No |
| redis/go-redis/v9 | L2 cache | Yes |
| golang.org/x/sync | singleflight + semaphore | No |

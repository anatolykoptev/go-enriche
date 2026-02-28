# go-enriche: Architecture Design

> Date: 2026-02-28 | Status: Approved

## Summary

Standalone Go module for web content enrichment — extracted from go-wp's monolithic `tool_enrich.go`. Reusable library with sub-packages for fetch, extract, search, cache. Three consumers: go-wp, go-content, vaelor.

**Module**: `github.com/anatolykoptev/go-enriche`

## Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Consumers | go-wp + go-content + vaelor | Generalized types needed |
| Scope | Full pipeline + Stealth | fetch + extract + search + cache + TLS fingerprinting |
| Structure | Sub-packages | Each usable independently |
| Naming | go-enriche | Consistent with go-imagefy/go-stealth |

## Architecture

Three-layer model (L0 pure logic / L1 HTTP / L2 orchestration) with sub-packages:

```
go-enriche/
├── enriche.go          — Enricher struct, Config, EnrichSingle/EnrichBatch
├── types.go            — Result, Item, Facts, PageStatus, Mode
├── options.go          — functional options: WithCache, WithStealth, etc.
│
├── fetch/              — L1: HTTP fetch + status detection
│   ├── fetcher.go      — Fetcher{}, FetchResult, singleflight dedup
│   ├── status.go       — Active/NotFound/Redirect/Unreachable/WebsiteDown
│   └── stealth.go      — go-stealth integration, TLS profile selection
│
├── extract/            — L0/L1: content extraction from HTML
│   ├── text.go         — go-trafilatura wrapper, ExtractText()
│   ├── facts.go        — JSON-LD + Microdata + regex cascade
│   ├── ogimage.go      — og:image extraction
│   └── date.go         — go-htmldate wrapper, publication date
│
├── structured/         — L0: typed schema.org parsing
│   ├── parser.go       — microdata.ParseHTML wrapper
│   ├── place.go        — Place struct + converter
│   ├── article.go      — Article struct + converter
│   ├── event.go        — Event struct + converter
│   └── org.go          — Organization struct + converter
│
├── search/             — L1: external search context
│   ├── searxng.go      — SearXNG query + aggregation
│   ├── query.go        — mode-aware query building
│   └── provider.go     — SearchProvider interface
│
└── cache/              — L1: caching layer
    ├── cache.go        — Cache interface
    ├── memory.go       — sync.Map L1
    └── redis.go        — Redis L2 (optional)
```

Data flow: `Item` → `Fetcher.Fetch()` → `extract.Text()` + `extract.Facts()` + `search.Context()` → `Result`

## Core Types

### Input

```go
type Item struct {
    Name    string // required
    URL     string // optional — if empty, search-only enrichment
    City    string // optional — for places/events
    Mode    Mode   // News / Places / Events
    Source  string // origin identifier
    Topic   string // classification tag
}

type Mode int // ModeNews, ModePlaces, ModeEvents
```

### Output

```go
type Result struct {
    Name           string
    URL            string
    Status         PageStatus      // Active/NotFound/Redirect/Unreachable/WebsiteDown
    Content        string          // extracted article text (trafilatura)
    Image          *string         // og:image URL
    PublishedAt    *time.Time      // extracted date (go-htmldate)
    Facts          Facts           // structured data
    SearchContext  string          // SearXNG context blob
    SearchSources  []string        // top 3 source URLs
    Metadata       *ContentMeta    // title/author/language/description
}

type Facts struct {
    PlaceName *string
    PlaceType *string
    Address   *string
    Phone     *string
    Price     *string
    Website   *string
    Hours     *string
    EventDate *string
}

type ContentMeta struct {
    Title       string
    Author      string
    Description string
    Language    string
    SiteName    string
}
```

### Enricher

```go
type Enricher struct { cfg Config }

func New(opts ...Option) *Enricher
func (e *Enricher) Enrich(ctx context.Context, item Item) (*Result, error)
func (e *Enricher) EnrichBatch(ctx context.Context, items []Item) []*Result

type Config struct {
    HTTPClient     *http.Client
    StealthClient  *stealth.Client   // optional
    Cache          cache.Cache       // optional
    SearchProvider search.Provider   // optional
    MaxConcurrency int               // default: 5
    MaxBodyBytes   int64             // default: 2MB
    Timeout        time.Duration     // default: 15s
    MaxContentLen  int               // default: 4000 runes
}
```

## Sub-packages

### fetch/

```go
type PageStatus int // Active, NotFound, Redirect, Unreachable, WebsiteDown

type FetchResult struct {
    HTML     string
    Status   PageStatus
    FinalURL string
}

type Fetcher struct {
    client  *http.Client
    stealth *stealth.Client
    sf      singleflight.Group
}

func (f *Fetcher) Fetch(ctx context.Context, url string) (*FetchResult, error)
```

Custom `CheckRedirect` for domain-change detection. `singleflight` dedup for parallel goroutines.

### extract/

```go
func ExtractText(r io.Reader, pageURL *url.URL) (*TextResult, error)  // go-trafilatura
func ExtractFacts(html, pageURL string) Facts                          // microdata → regex cascade
func ExtractOGImage(html string) *string                               // go-imagefy delegate
func ExtractDate(html string, pageURL *url.URL) *time.Time            // go-htmldate
```

Facts cascade: `structured.Parse()` → typed converters → pre-compiled regex fallback.

### structured/

```go
func Parse(html, contentType, pageURL string) (*Data, error)

type Place struct { Name, Type, Address, Phone, Website, Hours, Price *string }
func (d *Data) FirstPlace() *Place

type Article struct { Headline, Author, Description, DatePublished *string }
func (d *Data) FirstArticle() *Article

// Event, Organization — analogous
```

Wrapper over `astappiev/microdata` with typed converters.

### search/

```go
type Provider interface {
    Search(ctx context.Context, query string, opts SearchOpts) (*SearchResult, error)
}

type SearchResult struct {
    Context string
    Sources []string
}

type SearXNG struct { baseURL string; client *http.Client }

func BuildQuery(mode Mode, name, city string) (query, timeRange string)
```

### cache/

```go
type Cache interface {
    Get(ctx context.Context, key string, dest any) bool
    Set(ctx context.Context, key string, value any, ttl time.Duration)
}

type Memory struct { ... }   // sync.Map L1
type Redis struct { ... }    // go-redis L2
type Tiered struct { ... }   // L1 → L2 cascade
```

## Dependencies

```
github.com/markusmobius/go-trafilatura  — article text extraction
github.com/astappiev/microdata          — JSON-LD + Microdata parsing
github.com/anatolykoptev/go-stealth     — TLS fingerprinting
github.com/anatolykoptev/go-imagefy     — og:image extraction
github.com/redis/go-redis/v9            — L2 cache (optional)
golang.org/x/sync                       — singleflight + semaphore
```

go-htmldate comes transitively via go-trafilatura.

## Consumer Integration

Adapter pattern (1 file ~50 lines per consumer):

```
go-wp/internal/enrichadapter/adapter.go     — engine.Cache → cache.Cache
go-content/internal/enrichadapter/adapter.go — store.Cache → cache.Cache
vaelor                                       — direct enriche.New(...)
```

## Graceful Degradation

- No stealth → fallback to net/http
- No cache → full fetch every time
- No SearXNG → skip search context
- Fetch fail → Result with Status=Unreachable, other fields nil
- Extract fail → log + empty content, pipeline continues
- Panic in goroutine → recover + log, skip item

## Migration from go-wp

After go-enriche is ready, `tool_enrich.go` replaces its implementation with `enriche.Enrich()` via adapter. Old code removed: `extractArticleText`, `applyLDJSON`, `fetchPageWithStatus`, `fetchSearxngContext`.

## Testing

| Package | Approach | Mock what |
|---------|----------|-----------|
| `fetch/` | `httptest.Server` | Nothing |
| `extract/` | HTML fixtures in testdata/ | Nothing |
| `structured/` | JSON-LD + Microdata fixtures | Nothing |
| `search/` | `httptest.Server` | Nothing |
| `cache/` | Unit + miniredis | Redis |
| Root | Integration with mock interfaces | HTTP layer |

## Roadmap

| Phase | Goal | Deliverable |
|-------|------|------------|
| 0 | Infrastructure | go.mod, Makefile, CI, linter, empty packages |
| 1 | Extract | extract/ + structured/ — trafilatura, microdata, regex, ogimage, date |
| 2 | Fetch | fetch/ — status detection, singleflight, stealth |
| 3 | Search + Cache | search/ + cache/ — SearXNG, Memory/Redis/Tiered |
| 4 | Orchestration | Root Enricher — Enrich/EnrichBatch, functional options |
| 5 | Migration | go-wp adapter, remove old code |

# go-enriche: Research — Reference Solutions

> Date: 2026-02-28 | Status: Research complete

## Overview

Go-ecosystem for web content enrichment is modular — unlike Python (Trafilatura monolith), Go solutions are specialized and composable. Key discovery: **markusmobius** ported the entire Python Trafilatura ecosystem to Go (go-trafilatura, go-readability, go-domdistiller, go-htmldate), solving ~70% of go-enriche needs.

Current go-wp implementation (`tool_enrich_extract.go`) is a regex-based prototype. Should be replaced with proven libraries.

---

## 1. Article Text Extraction (Readability)

### markusmobius/go-trafilatura

- **GitHub**: https://github.com/markusmobius/go-trafilatura
- **Status**: Actively maintained (2025)
- **Language**: Go

Go port of Python Trafilatura — leading by benchmarks (ScrapingHub, Bevendorff 2023). Multi-stage pipeline:
1. `docCleaning` — remove comments, scripts, styles
2. `pruneHTML` — CSS selector pruning (nav/footer/ads/sidebars)
3. `deleteByLinkDensity` — remove link-heavy blocks
4. `extractContent` — recursive DOM walk via `processNode()` with per-element handlers
5. `extractMetadata` — title/author/date/description from meta tags + JSON-LD
6. Fallback chain: trafilatura → go-readability → go-domdistiller

**Key pattern** — three-tier fallback in `external.go`:
```go
type _FallbackGenerator func() (string, *html.Node)

for _, generator := range createFallbackGenerators(cleanedDoc, opts) {
    candidateTitle, candidateDoc := generator()
    if candidateIsUsable(candidateDoc, extractedDoc, lenCandidate, lenExtracted, opts) {
        extractedDoc = candidateDoc
    }
}
```

**Strengths**:
- Best accuracy among Go solutions
- Thread-safe, stateless `Extract()` — goroutine-safe
- `Options.Focus` (FavorPrecision/FavorRecall/Balanced)
- `Options.MaxTreeSize` — protection against huge documents
- Extracts: TextContent, HTML, Title, Author, Date, Description, SiteName, Language, Categories, Tags
- Built-in language detection (`whatlanggo`)
- LRU dedup cache — catches repeated footer/nav across pages
- `re2go` compiled patterns — 5-10x faster than `regexp.MustCompile` for hot paths

**Weaknesses**:
- Heavy dependencies (go-htmldate, whatlanggo, re2go)
- No built-in HTTP client — needs external fetcher

### go-shiori/go-readability

- **GitHub**: https://github.com/go-shiori/go-readability
- **Status**: Stable; deprecated in favor of `codeberg.org/readeck/go-readability/v2`

Mozilla Readability.js port. Scoring-based: each DOM node scored via `getClassWeight()`, link density, comma count, char count. Scores propagate up.

```go
article, err := readability.FromURL(pageURL, 30*time.Second)
article, err := readability.FromReader(resp.Body, parsedURL)

// Quick check:
isArticle := readability.Check(resp.Body)
```

**Strengths**: 100+ real test pages (NYT, BBC, Guardian, Medium, Wikipedia), `Check()` for fast pre-screening, JSON-LD parsing in metadata extraction.

**Weaknesses**: Worse than trafilatura on edge cases. No comment extraction.

**Recommendation**: Use as fallback after go-trafilatura (which already does this internally).

---

## 2. Structured Data Extraction (JSON-LD / Microdata)

### astappiev/microdata

- **GitHub**: https://github.com/astappiev/microdata
- **Language**: Go

Only Go library handling both **Microdata** (HTML5 itemscope/itemprop) AND **JSON-LD** in one call. Unified `Item` structure:

```go
data, err := microdata.ParseHTML(resp.Body, contentType, pageURL)

place := data.GetFirstOfSchemaType("Place")
item := data.GetFirstOfType("https://schema.org/LocalBusiness", "https://schema.org/Restaurant")

name, _ := place.GetProperty("name")
addr, _ := place.GetNestedItem("address")
```

**Key pattern** — `PropertyMap = map[string][]interface{}`. Values can be primitive or `*Item` (nested).

Uses `github.com/astappiev/fixjson` — survives trailing commas, unquoted keys (common in real sites).

**Strengths**:
- Unified API for JSON-LD + Microdata
- Resilient via fixjson
- `GetFirstOfSchemaType("Place")` — convenient schema.org lookup
- Very small (~9 files), easy to vendor

**Weaknesses**:
- No `@context` resolution
- Returns `interface{}` — needs typed converters on top

**Impact**: Replaces manual `applyLDJSON` in go-wp with `microdata.ParseHTML()` + typed converters for Place/Article/Event/Organization.

### markusmobius/go-htmldate

- **GitHub**: https://github.com/markusmobius/go-htmldate

Multi-layer date extraction (priority order):
1. `<meta name="date">`, `<meta property="article:published_time">` (~20 variants)
2. HTML5 `<time datetime="...">`
3. JSON-LD `datePublished`/`dateModified`
4. URL patterns (`/2024/11/15/slug`)
5. og:image URL dates
6. General regex + `selectCandidate`

Validation: year 1995..current+1, date not >2 days in future.

---

## 3. Web Fetching / Stealth

### go-stealth (own library, already in use)

- **GitHub**: https://github.com/anatolykoptev/go-stealth

Pluggable backend via `HTTPDoer` interface. Default: `tlsClientDoer` (bogdanfinn/tls-client for TLS fingerprinting). Fallback: `stdDoer` (net/http).

```go
client, err := stealth.NewClient(
    stealth.WithProfile(stealth.ProfileChrome131),
    stealth.WithProxy("http://user:pass@proxy:8080"),
    stealth.WithTimeout(15),
)
stdClient := client.StdClient() // → *http.Client compatible
```

Middleware: RetryMiddleware (exponential backoff + jitter), RateLimitMiddleware (per-domain), ClientHintsMiddleware.

**Key pattern**: `BackendFactory` — test with `stdDoer`, deploy with `tlsClientDoer`.

### gocolly/colly (~23k stars)

Too heavy as dependency, but valuable patterns:
- **Proxy rotation**: `atomic.AddUint32` for round-robin (lock-free)
- **Disk cache**: `fnv.New64a()` hash → gob file, expiry via mtime
- **Rate limit per domain**: `LimitRule{DomainGlob, Delay, RandomDelay, Parallelism}`
- **Charset detection**: `golang.org/x/net/html/charset`

### go-rod/rod (~5k stars)

Only for JS-heavy sites. Not for core. Valuable stealth patterns:
- `Mouse.MoveLinear()` — human-like mouse trajectories
- `HijackRouter` — block anti-bot JS scripts at network level
- `EvalOnNewDocument()` — patch `navigator.webdriver = undefined`

---

## 4. Best Patterns for go-enriche

### Pattern 1: Singleflight Fetch Dedup

Current go-wp has cache but no singleflight. Parallel goroutines fetching same URL should collapse:

```go
import "golang.org/x/sync/singleflight"

type Fetcher struct {
    client *http.Client
    cache  Cache
    sf     singleflight.Group
}

func (f *Fetcher) Fetch(ctx context.Context, url string) (*FetchResult, error) {
    result, err, _ := f.sf.Do(url, func() (interface{}, error) {
        if cached := f.cache.Get(ctx, cacheKey(url)); cached != nil {
            return cached, nil
        }
        resp := fetchPageWithStatus(ctx, f.client, url)
        f.cache.Set(ctx, cacheKey(url), resp)
        return resp, nil
    })
    return result.(*FetchResult), err
}
```

### Pattern 2: Functional Options Config

From go-trafilatura `Options` + go-imagefy `Config`:

```go
type Config struct {
    HTTPClient    *http.Client
    StealthClient stealth.BrowserClient // nil → skip TLS fingerprint
    Cache         Cache                 // nil → no-op cache
    SearxngURL    string
    Focus         ExtractionFocus       // Precision / Recall / Balanced
    MaxBodyBytes  int64                 // default 2MB
    Timeout       time.Duration         // default 15s
}

type Option func(*Config)

func WithStealthClient(c stealth.BrowserClient) Option { ... }
func WithCache(c Cache) Option { ... }
func WithSearxng(url string) Option { ... }
```

### Pattern 3: Typed Facts from JSON-LD

Replace manual `applyLDJSON` with microdata + typed converters:

```go
func ExtractFacts(pageHTML, pageURL string) Facts {
    data, err := microdata.ParseHTML(
        strings.NewReader(pageHTML), "text/html", pageURL,
    )
    if err != nil { return regexFallback(pageHTML) }

    for _, schemaType := range []string{"LocalBusiness", "Restaurant", "Place", "Organization"} {
        if item := data.GetFirstOfSchemaType(schemaType); item != nil {
            return itemToPlaceFacts(item)
        }
    }
    if item := data.GetFirstOfSchemaType("Article"); item != nil {
        return itemToArticleFacts(item)
    }
    if item := data.GetFirstOfSchemaType("Event"); item != nil {
        return itemToEventFacts(item)
    }
    return regexFallback(pageHTML)
}
```

### Pattern 4: Bounded Parallel Enrichment

Current go-wp uses unbounded `sync.WaitGroup`. Add semaphore:

```go
import "golang.org/x/sync/semaphore"

func (e *Enricher) EnrichBatch(ctx context.Context, items []Item) []Result {
    sem := semaphore.NewWeighted(int64(e.cfg.MaxConcurrency))
    results := make([]Result, len(items))
    var wg sync.WaitGroup

    for i, item := range items {
        wg.Add(1)
        go func(idx int, it Item) {
            defer wg.Done()
            _ = sem.Acquire(ctx, 1)
            defer sem.Release(1)
            results[idx] = e.EnrichSingle(ctx, it)
        }(i, item)
    }
    wg.Wait()
    return results
}
```

### Pattern 5: Pre-compiled Regex

Current go-wp compiles regex on every call. Fix:

```go
// BAD (current):
func regexExtract(text, pattern string) string {
    re, err := regexp.Compile(pattern) // compiles every call!

// GOOD:
var (
    rePhone   = regexp.MustCompile(`(?:\+7|8)[\s\-]?\(?\d{3}\)?[\s\-]?\d{3}[\s\-]?\d{2}[\s\-]?\d{2}`)
    reAddress = regexp.MustCompile(`(?i)(?:адрес|address)[:\s]+([^\n<]{5,100})`)
    rePrice   = regexp.MustCompile(`(?i)(?:цена|стоимость|price)[:\s]+([^\n<]{2,80})`)
)
```

---

## 5. Comparison Matrix

### Article Text Extraction

| Solution | Accuracy | Metadata | Fallback | Size | Thread-safe |
|----------|----------|----------|----------|------|-------------|
| go-trafilatura | Best | Full (title/author/date/lang) | 3-tier | Medium | Yes |
| go-readability | Good | Partial (title/byline/image) | No | Small | Yes |
| go-domdistiller | Medium | Minimal | No | Small | Yes |
| Current regex (go-wp) | Poor | No | No | 0 | Yes |

### Structured Data Extraction

| Solution | JSON-LD | Microdata | Schema types | Resilience |
|----------|---------|----------|--------------|------------|
| astappiev/microdata | Yes | Yes | `GetFirstOfSchemaType()` | fixjson |
| Current applyLDJSON (go-wp) | Partial | No | Manual | No |
| go-trafilatura metadata | Yes | No | Article/NewsArticle | Yes |

### Fetching / Stealth

| Solution | TLS fingerprint | Proxy | Rate limit | Retry | Browser |
|----------|----------------|-------|------------|-------|---------|
| go-stealth | Yes (tls-client) | Pool | Per-domain | Exp. backoff | No |
| colly | No | Round-robin | Per-domain | No | Via chromedp |
| go-rod | Full | Via hijack | No | No | Yes (CDP) |

---

## 6. Recommendation

### Core Dependencies

| Component | Solution | Why |
|-----------|---------|-----|
| Article text | `markusmobius/go-trafilatura` | Best accuracy, built-in fallback chain, thread-safe |
| Publication date | `markusmobius/go-htmldate` | Multi-layer extraction, re2go perf |
| JSON-LD + Microdata | `astappiev/microdata` | Unified API, fixjson resilience |
| OG image | `go-imagefy.ExtractOGImageURL` | Already exists |
| Stealth HTTP | `go-stealth` | TLS fingerprinting, proxy pool |
| Caching | `tieredCache` from go-wp | Port, don't rewrite |
| Search context | `fetchSearxngContext` from go-wp | Port |
| Regex facts | Pre-compiled patterns | Fix compile-on-call bug |

### go.mod Dependencies

```
github.com/markusmobius/go-trafilatura
github.com/astappiev/microdata
github.com/anatolykoptev/go-stealth       # optional, via interface
github.com/anatolykoptev/go-imagefy        # for og:image
github.com/redis/go-redis/v9               # for L2 cache
golang.org/x/sync                          # singleflight + semaphore
```

go-htmldate pulled transitively via go-trafilatura.

### What NOT to include in core

- `go-rod` / `chromedp` — only for JS-heavy sites, optional via `Fetcher` interface
- `gocolly/colly` — too heavy framework, not a library
- LLM extraction — go-wp/go-content level, not go-enriche

### Proposed Module Structure

```
go-enriche/
├── go.mod                    github.com/anatolykoptev/go-enriche
├── enricher.go               — Enricher{}, Config, Enrich()/EnrichBatch()
├── fetcher.go                — fetchWithStatus, singleflight, status enums
├── extractor.go              — go-trafilatura wrapper, ExtractResult
├── structured.go             — microdata wrapper, typed Place/Article/Event/Org
├── facts.go                  — JSON-LD → typed Facts, fallback cascade
├── regex.go                  — pre-compiled regex, phone/address/price patterns
├── ogimage.go                — OG image extraction (wrap go-imagefy)
├── searxng.go                — query building, result aggregation
├── cache.go                  — Cache interface + tieredCache (from go-wp)
├── types.go                  — Result, Item, Facts, PageStatus, ExtractionFocus
└── options.go                — functional options pattern
```

### Port from go-wp (no changes needed)

1. `engine/cache.go` — full `tieredCache` + L1/L2 + cleanup goroutine
2. `tool_enrich_fetch.go` — `fetchPageWithStatus()` status detection logic
3. `tool_enrich.go` — `fetchSearxngContext()`, `buildSearxngQuery()`, `ItemFacts` struct
4. `engine/stealth.go` — `StealthClient()` singleton pattern

### Replace from go-wp

1. `extractArticleText()` → `trafilatura.Extract()` (go-trafilatura)
2. `applyLDJSON()` / `extractFacts()` → `microdata.ParseHTML()` + typed converters
3. `regexExtract()` → package-level pre-compiled `var rePhone = regexp.MustCompile(...)`
4. Add `singleflight` for parallel fetch dedup
5. Add semaphore for bounded concurrency in `EnrichBatch`

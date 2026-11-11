# go-enriche

Go library that enriches an item (a place, news story or event) from the web: it fetches the
official page, extracts article text and contact facts, parses schema.org data, checks the place
on map services and adds search context. It degrades gracefully, returning a partial result when
a step fails. Built for services that publish content and need verified facts.

## Install

```sh
go get github.com/anatolykoptev/go-enriche
```

Go 1.26 or newer (see `go.mod`).

## Packages

| Package | Purpose |
|---------|---------|
| `enriche` (root) | `Enricher`: `Enrich`, `EnrichBatch`, `Search`; fact resolver, provenance, metrics hooks |
| `extract/` | Article text (trafilatura), facts (structured, then regex cascade), contacts and phone roles, og:image, dates |
| `structured/` | Typed schema.org parsing from JSON-LD and Microdata: Place, Article, Event, Organization |
| `fetch/` | HTTP fetch with status detection, redirect handling, singleflight, SSRF guard, ox-browser client |
| `search/` | Search providers (DDG, Startpage, Brave, Google, ox-browser) and decorators: fallback, parallel, rate limit, circuit breaker |
| `maps/` | Place status and geocoding checkers: 2GIS, Yandex Maps, DaData, with composite, rate-limit and resilience wrappers |
| `cache/` | `Cache` interface with Memory, Redis and Tiered implementations |
| `news/` | News scoring, config and file-based store |

## Usage

```go
import (
    "context"
    "fmt"
    "log"

    "github.com/anatolykoptev/go-enriche"
    "github.com/anatolykoptev/go-enriche/cache"
    "github.com/anatolykoptev/go-enriche/search"
)

ddg, err := search.NewDDG("") // proxy URL; empty means direct
if err != nil {
    log.Fatal(err)
}
e := enriche.New(
    enriche.WithCache(cache.NewMemory()),
    enriche.WithSearch(ddg),
)

res, err := e.Enrich(context.Background(), enriche.Item{
    Name: "Example Cafe",
    URL:  "https://example.com/",
    City: "Springfield",
    Mode: enriche.ModePlaces,
})
if err != nil {
    log.Fatal(err)
}
fmt.Println(res.Status, res.Content, res.Facts.Phone)
```

The `extract` and `structured` packages also work on their own on an HTML reader or string.

## Configuration

Everything is configured in code through functional options (`WithFetcher`, `WithCache`,
`WithCacheTTL`, `WithSearch`, `WithMapsChecker`, `WithConcurrency`, `WithMaxContentLen`,
`WithFormat`, `WithLogger`, `WithMetrics`, `WithBrowserFetch`, `WithOxBrowser`, `WithTargetGuard`,
`WithRenderSkipDisabled`, ...). The library reads no environment variables. By default an
`Enricher` fetches with a fresh `fetch.Fetcher`, caches nothing, runs five items concurrently, caches
results for 24 hours when a cache is set, and checks every outbound target with the go-kit SSRF
guard.

## Status

Released as semver tags (`v1.x`); there is no release automation, tags are cut by hand and
[CHANGELOG.md](CHANGELOG.md) is kept manually. `docs/ROADMAP.md` records the original phases and
`docs/ARCHITECTURE.md` the original design; the code is authoritative where they differ.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md); coding agents start at [CLAUDE.md](CLAUDE.md). Every
document is listed in [docs/INDEX.md](docs/INDEX.md).

## License

Apache-2.0, see [LICENSE.md](LICENSE.md).

# go-enriche

Standalone Go library for web content enrichment: fetch pages, extract article text, parse structured data (JSON-LD/Microdata), search for context.

## Status

Phase 1 complete — extract/ and structured/ packages ready.

## Packages

| Package | Purpose |
|---------|---------|
| `extract/` | Article text (trafilatura), facts (structured→regex cascade), og:image, dates |
| `structured/` | Typed schema.org parsing: Place, Article, Event, Organization |
| `fetch/` | HTTP fetch with status detection, stealth, singleflight (Phase 2) |
| `search/` | Web search context (DDG, Startpage, Brave, Google providers) |
| `cache/` | Cache interface + Memory/Redis/Tiered (Phase 3) |

## Usage

```go
import (
    "github.com/anatolykoptev/go-enriche/extract"
    "github.com/anatolykoptev/go-enriche/structured"
)

// Extract article text
result, _ := extract.ExtractText(reader, pageURL)
fmt.Println(result.Content, result.Title)

// Extract structured facts (JSON-LD → Microdata → regex cascade)
facts := extract.ExtractFacts(html, pageURL)
fmt.Println(facts.PlaceName, facts.Phone)

// Parse schema.org directly
data, _ := structured.Parse(reader, "text/html", pageURL)
if place := data.FirstPlace(); place != nil {
    fmt.Println(place.Name, place.Address)
}

// Extract og:image
img := extract.ExtractOGImage(html)
```

## License

MIT

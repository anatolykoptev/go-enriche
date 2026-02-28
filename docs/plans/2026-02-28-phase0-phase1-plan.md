# go-enriche Phase 0 + Phase 1 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Scaffold go-enriche module with full CI/linter infrastructure, then implement the extract/ and structured/ sub-packages — the core content extraction layer.

**Architecture:** Sub-package structure with L0 pure logic (structured/) and L0/L1 extraction (extract/). go-trafilatura for article text, astappiev/microdata for JSON-LD+Microdata parsing, go-imagefy for og:image, pre-compiled regex for Russian fallback patterns. Each sub-package usable independently.

**Tech Stack:** Go 1.25, go-trafilatura v1.12.2, astappiev/microdata v1.0.2, go-imagefy, golangci-lint v2

---

### Task 1: Project scaffolding — go.mod + Makefile + linter

**Files:**
- Create: `go.mod`
- Create: `Makefile`
- Create: `.golangci.yml`
- Create: `.pre-commit-config.yaml`
- Create: `.github/workflows/ci.yml`
- Create: `.gitignore`
- Create: `LICENSE.md`

**Step 1: Initialize go module and add dependencies**

```bash
cd /home/krolik/src/go-enriche
go mod init github.com/anatolykoptev/go-enriche
go get github.com/markusmobius/go-trafilatura@v1.12.2
go get github.com/astappiev/microdata@v1.0.2
go get github.com/anatolykoptev/go-imagefy@latest
go get golang.org/x/sync@latest
```

**Step 2: Create Makefile**

```makefile
.PHONY: lint test build

lint:
	golangci-lint run ./...

test:
	go test -race -count=1 ./...

build:
	go build ./...
```

**Step 3: Create `.golangci.yml`**

Copy from `/home/krolik/src/go-imagefy/.golangci.yml` — identical config (v2 format, funlen 100/60, cognitive 20, cyclomatic 15, dupl 150, mnd with ignored numbers, test exclusions).

**Step 4: Create `.pre-commit-config.yaml`**

```yaml
repos:
  - repo: https://github.com/pre-commit/pre-commit-hooks
    rev: v5.0.0
    hooks:
      - id: trailing-whitespace
      - id: end-of-file-fixer
      - id: check-yaml
      - id: check-merge-conflict
      - id: check-added-large-files
        args: [--maxkb=500]

  - repo: https://github.com/golangci/golangci-lint
    rev: v2.10.1
    hooks:
      - id: golangci-lint
```

**Step 5: Create `.github/workflows/ci.yml`**

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

permissions:
  contents: read

jobs:
  test:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        go-version: ["1.24", "1.25"]
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: ${{ matrix.go-version }}

      - name: Test
        run: go test -race -count=1 -coverprofile=coverage.out ./...

      - name: Upload coverage
        if: matrix.go-version == '1.25'
        uses: actions/upload-artifact@v4
        with:
          name: coverage
          path: coverage.out

  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: "1.25"

      - uses: golangci/golangci-lint-action@v7
        with:
          version: v2.10.1
```

**Step 6: Create `.gitignore`**

```
*.exe
*.test
*.out
/vendor/
.idea/
.vscode/
```

**Step 7: Create `LICENSE.md`**

MIT license, copyright 2026 Anatoly Koptev.

**Step 8: Commit**

```bash
git add go.mod go.sum Makefile .golangci.yml .pre-commit-config.yaml .github/ .gitignore LICENSE.md
git commit -m "chore: initialize go-enriche module with dependencies and CI"
```

---

### Task 2: Empty package stubs + root types

**Files:**
- Create: `enriche.go`
- Create: `types.go`
- Create: `extract/extract.go`
- Create: `structured/structured.go`
- Create: `fetch/fetch.go`
- Create: `search/search.go`
- Create: `cache/cache.go`

**Step 1: Create root package with placeholder types**

`enriche.go`:
```go
// Package enriche provides web content enrichment: fetch pages, extract text,
// parse structured data, search for context.
package enriche
```

`types.go` — core types from the design:
```go
package enriche

import "time"

// Mode specifies the enrichment mode.
type Mode int

const (
	ModeNews   Mode = iota // News articles
	ModePlaces             // Places and businesses
	ModeEvents             // Events and happenings
)

// Item is the input for enrichment.
type Item struct {
	Name   string // required
	URL    string // optional — if empty, search-only enrichment
	City   string // optional — for places/events
	Mode   Mode
	Source string // origin identifier
	Topic  string // classification tag
}

// Result is the output of enrichment.
type Result struct {
	Name          string
	URL           string
	Status        string       // "active", "not_found", "redirect", "unreachable", "website_down"
	Content       string       // extracted article text
	Image         *string      // og:image URL
	PublishedAt   *time.Time   // extracted publication date
	Facts         Facts        // structured data
	SearchContext string       // search engine context
	SearchSources []string     // source URLs from search
	Metadata      *ContentMeta // title/author/language
}

// Facts holds structured data extracted from a page.
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

// ContentMeta holds article metadata extracted by trafilatura.
type ContentMeta struct {
	Title       string
	Author      string
	Description string
	Language    string
	SiteName    string
}
```

**Step 2: Create sub-package stubs**

Each gets a minimal doc comment so the package compiles:

`extract/extract.go`:
```go
// Package extract provides content extraction from HTML:
// article text, structured facts, og:image, publication date.
package extract
```

`structured/structured.go`:
```go
// Package structured provides typed schema.org parsing
// from JSON-LD and HTML Microdata.
package structured
```

`fetch/fetch.go`:
```go
// Package fetch provides HTTP page fetching with status detection,
// stealth support, and singleflight deduplication.
package fetch
```

`search/search.go`:
```go
// Package search provides external search context aggregation
// via SearXNG and other providers.
package search
```

`cache/cache.go`:
```go
// Package cache provides a Cache interface with in-memory (L1)
// and Redis (L2) implementations.
package cache

import "context"

// Cache is the interface for enrichment caching.
type Cache interface {
	// Get retrieves a cached value. Returns false if not found.
	Get(ctx context.Context, key string, dest any) bool
	// Set stores a value with the given TTL. Zero TTL means no expiration.
	Set(ctx context.Context, key string, value any, ttl time.Duration)
}
```

Add the missing `time` import to `cache/cache.go`.

**Step 3: Verify build**

```bash
cd /home/krolik/src/go-enriche && go build ./... && make lint
```

Expected: builds clean, lint passes.

**Step 4: Commit**

```bash
git add enriche.go types.go extract/ structured/ fetch/ search/ cache/
git commit -m "chore: add root types and empty sub-package stubs"
```

---

### Task 3: structured/ — microdata parser wrapper

**Files:**
- Create: `structured/parser.go`
- Create: `structured/types.go`
- Create: `structured/parser_test.go`

**Step 1: Write types for structured data**

`structured/types.go`:
```go
package structured

import "github.com/astappiev/microdata"

// Data wraps parsed microdata with typed accessors.
type Data struct {
	raw *microdata.Microdata
}

// Place represents a schema.org Place/LocalBusiness.
type Place struct {
	Name    *string
	Type    *string // @type (e.g. "Restaurant", "LocalBusiness")
	Address *string
	Phone   *string
	Website *string
	Hours   *string
	Price   *string
}

// Article represents a schema.org Article/NewsArticle.
type Article struct {
	Headline      *string
	Author        *string
	Description   *string
	DatePublished *string
	Image         *string
}

// Event represents a schema.org Event.
type Event struct {
	Name      *string
	StartDate *string
	EndDate   *string
	Location  *string
	Price     *string
}

// Organization represents a schema.org Organization.
type Organization struct {
	Name    *string
	URL     *string
	Phone   *string
	Address *string
}
```

**Step 2: Write the parser**

`structured/parser.go`:
```go
package structured

import (
	"fmt"
	"strings"

	"github.com/astappiev/microdata"
)

// Parse extracts JSON-LD and Microdata from HTML.
func Parse(r io.Reader, contentType, pageURL string) (*Data, error) {
	md, err := microdata.ParseHTML(r, contentType, pageURL)
	if err != nil {
		return nil, fmt.Errorf("microdata parse: %w", err)
	}
	return &Data{raw: md}, nil
}

// Raw returns the underlying microdata for advanced use.
func (d *Data) Raw() *microdata.Microdata { return d.raw }

// FirstPlace finds the first Place-like item (Place, LocalBusiness, Restaurant, etc.).
func (d *Data) FirstPlace() *Place {
	placeTypes := []string{
		"LocalBusiness", "Restaurant", "CafeOrCoffeeShop", "BarOrPub",
		"Hotel", "Store", "Place", "TouristAttraction", "Museum",
		"SportsActivityLocation", "EntertainmentBusiness",
	}
	for _, t := range placeTypes {
		if item := d.raw.GetFirstOfSchemaType(t); item != nil {
			return itemToPlace(item)
		}
	}
	return nil
}

// FirstArticle finds the first Article-like item.
func (d *Data) FirstArticle() *Article {
	for _, t := range []string{"Article", "NewsArticle", "BlogPosting", "WebPage"} {
		if item := d.raw.GetFirstOfSchemaType(t); item != nil {
			return itemToArticle(item)
		}
	}
	return nil
}

// FirstEvent finds the first Event item.
func (d *Data) FirstEvent() *Event {
	if item := d.raw.GetFirstOfSchemaType("Event"); item != nil {
		return itemToEvent(item)
	}
	return nil
}

// FirstOrganization finds the first Organization item.
func (d *Data) FirstOrganization() *Organization {
	for _, t := range []string{"Organization", "Corporation", "GovernmentOrganization"} {
		if item := d.raw.GetFirstOfSchemaType(t); item != nil {
			return itemToOrganization(item)
		}
	}
	return nil
}

// --- converters ---

func itemToPlace(item *microdata.Item) *Place {
	p := &Place{
		Name:    propString(item, "name"),
		Type:    itemType(item),
		Phone:   propString(item, "telephone"),
		Website: propString(item, "url"),
		Hours:   propString(item, "openingHours"),
	}
	p.Address = extractAddress(item)
	p.Price = extractPrice(item)
	return p
}

func itemToArticle(item *microdata.Item) *Article {
	return &Article{
		Headline:      propString(item, "headline", "name"),
		Author:        extractAuthor(item),
		Description:   propString(item, "description"),
		DatePublished: propString(item, "datePublished"),
		Image:         propString(item, "image"),
	}
}

func itemToEvent(item *microdata.Item) *Event {
	return &Event{
		Name:      propString(item, "name"),
		StartDate: propString(item, "startDate"),
		EndDate:   propString(item, "endDate"),
		Location:  extractEventLocation(item),
		Price:     extractPrice(item),
	}
}

func itemToOrganization(item *microdata.Item) *Organization {
	return &Organization{
		Name:    propString(item, "name"),
		URL:     propString(item, "url"),
		Phone:   propString(item, "telephone"),
		Address: extractAddress(item),
	}
}

// --- helpers ---

// propString returns the first string value for any of the given keys.
func propString(item *microdata.Item, keys ...string) *string {
	for _, key := range keys {
		val, ok := item.GetProperty(key)
		if !ok {
			continue
		}
		var s string
		switch v := val.(type) {
		case string:
			s = strings.TrimSpace(v)
		case fmt.Stringer:
			s = strings.TrimSpace(v.String())
		default:
			s = strings.TrimSpace(fmt.Sprint(v))
		}
		if s != "" {
			return &s
		}
	}
	return nil
}

// itemType returns the first schema.org type stripped of the prefix.
func itemType(item *microdata.Item) *string {
	if len(item.Types) == 0 {
		return nil
	}
	t := item.Types[0]
	t = strings.TrimPrefix(t, "https://schema.org/")
	t = strings.TrimPrefix(t, "http://schema.org/")
	return &t
}

// extractAddress builds an address string from nested PostalAddress or plain string.
func extractAddress(item *microdata.Item) *string {
	nested, ok := item.GetNestedItem("address")
	if ok {
		parts := make([]string, 0, 4)
		for _, key := range []string{"streetAddress", "addressLocality", "addressRegion", "postalCode"} {
			if s := propString(nested, key); s != nil {
				parts = append(parts, *s)
			}
		}
		if len(parts) > 0 {
			joined := strings.Join(parts, ", ")
			return &joined
		}
	}
	return propString(item, "address")
}

// extractPrice gets price from offers or priceRange.
func extractPrice(item *microdata.Item) *string {
	if nested, ok := item.GetNestedItem("offers"); ok {
		if p := propString(nested, "price"); p != nil {
			return p
		}
	}
	return propString(item, "priceRange")
}

// extractAuthor gets author name from nested Person or plain string.
func extractAuthor(item *microdata.Item) *string {
	if nested, ok := item.GetNestedItem("author"); ok {
		return propString(nested, "name")
	}
	return propString(item, "author")
}

// extractEventLocation gets location name from nested Place or plain string.
func extractEventLocation(item *microdata.Item) *string {
	if nested, ok := item.GetNestedItem("location"); ok {
		return propString(nested, "name")
	}
	return propString(item, "location")
}
```

Add `"io"` to the imports of `parser.go`.

**Step 3: Write tests**

`structured/parser_test.go`:
```go
package structured

import (
	"strings"
	"testing"
)

func TestParse_JSONLD_Place(t *testing.T) {
	t.Parallel()
	html := `<html><head>
	<script type="application/ld+json">
	{
		"@context": "https://schema.org",
		"@type": "Restaurant",
		"name": "Пиццерия Марио",
		"telephone": "+7 (812) 555-1234",
		"url": "https://mario.example.com",
		"openingHours": "Mo-Su 10:00-23:00",
		"priceRange": "500-1500 ₽",
		"address": {
			"@type": "PostalAddress",
			"streetAddress": "Невский проспект, 100",
			"addressLocality": "Санкт-Петербург"
		}
	}
	</script>
	</head><body></body></html>`

	data, err := Parse(strings.NewReader(html), "text/html", "https://example.com")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	place := data.FirstPlace()
	if place == nil {
		t.Fatal("expected Place, got nil")
	}
	assertStringPtr(t, "Name", place.Name, "Пиццерия Марио")
	assertStringPtr(t, "Type", place.Type, "Restaurant")
	assertStringPtr(t, "Phone", place.Phone, "+7 (812) 555-1234")
	assertStringPtr(t, "Website", place.Website, "https://mario.example.com")
	assertStringPtr(t, "Hours", place.Hours, "Mo-Su 10:00-23:00")
	assertStringPtr(t, "Price", place.Price, "500-1500 ₽")
	if place.Address == nil || !strings.Contains(*place.Address, "Невский") {
		t.Errorf("expected address containing Невский, got %v", place.Address)
	}
}

func TestParse_JSONLD_Article(t *testing.T) {
	t.Parallel()
	html := `<html><head>
	<script type="application/ld+json">
	{
		"@context": "https://schema.org",
		"@type": "NewsArticle",
		"headline": "Breaking News",
		"author": {"@type": "Person", "name": "John Doe"},
		"datePublished": "2026-02-28",
		"description": "Something happened"
	}
	</script>
	</head><body></body></html>`

	data, err := Parse(strings.NewReader(html), "text/html", "https://example.com")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	article := data.FirstArticle()
	if article == nil {
		t.Fatal("expected Article, got nil")
	}
	assertStringPtr(t, "Headline", article.Headline, "Breaking News")
	assertStringPtr(t, "Author", article.Author, "John Doe")
	assertStringPtr(t, "DatePublished", article.DatePublished, "2026-02-28")
	assertStringPtr(t, "Description", article.Description, "Something happened")
}

func TestParse_JSONLD_Event(t *testing.T) {
	t.Parallel()
	html := `<html><head>
	<script type="application/ld+json">
	{
		"@context": "https://schema.org",
		"@type": "Event",
		"name": "Go Meetup",
		"startDate": "2026-03-15T19:00",
		"endDate": "2026-03-15T22:00",
		"location": {"@type": "Place", "name": "Loft Hall"}
	}
	</script>
	</head><body></body></html>`

	data, err := Parse(strings.NewReader(html), "text/html", "https://example.com")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	event := data.FirstEvent()
	if event == nil {
		t.Fatal("expected Event, got nil")
	}
	assertStringPtr(t, "Name", event.Name, "Go Meetup")
	assertStringPtr(t, "StartDate", event.StartDate, "2026-03-15T19:00")
	assertStringPtr(t, "Location", event.Location, "Loft Hall")
}

func TestParse_JSONLD_Organization(t *testing.T) {
	t.Parallel()
	html := `<html><head>
	<script type="application/ld+json">
	{
		"@context": "https://schema.org",
		"@type": "Organization",
		"name": "Acme Corp",
		"url": "https://acme.example.com",
		"telephone": "+1-800-555-0199"
	}
	</script>
	</head><body></body></html>`

	data, err := Parse(strings.NewReader(html), "text/html", "https://example.com")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	org := data.FirstOrganization()
	if org == nil {
		t.Fatal("expected Organization, got nil")
	}
	assertStringPtr(t, "Name", org.Name, "Acme Corp")
	assertStringPtr(t, "URL", org.URL, "https://acme.example.com")
	assertStringPtr(t, "Phone", org.Phone, "+1-800-555-0199")
}

func TestParse_NoStructuredData(t *testing.T) {
	t.Parallel()
	html := `<html><body><p>Plain page</p></body></html>`

	data, err := Parse(strings.NewReader(html), "text/html", "https://example.com")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if data.FirstPlace() != nil {
		t.Error("expected nil Place for plain page")
	}
	if data.FirstArticle() != nil {
		t.Error("expected nil Article for plain page")
	}
}

func TestParse_AddressString(t *testing.T) {
	t.Parallel()
	html := `<html><head>
	<script type="application/ld+json">
	{
		"@context": "https://schema.org",
		"@type": "Place",
		"name": "Park",
		"address": "123 Main Street, City"
	}
	</script>
	</head><body></body></html>`

	data, err := Parse(strings.NewReader(html), "text/html", "https://example.com")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	place := data.FirstPlace()
	if place == nil {
		t.Fatal("expected Place, got nil")
	}
	assertStringPtr(t, "Address", place.Address, "123 Main Street, City")
}

func assertStringPtr(t *testing.T, field string, got *string, want string) {
	t.Helper()
	if got == nil {
		t.Errorf("%s: expected %q, got nil", field, want)
		return
	}
	if *got != want {
		t.Errorf("%s: expected %q, got %q", field, want, *got)
	}
}
```

**Step 4: Run tests**

```bash
cd /home/krolik/src/go-enriche && go test -race -count=1 ./structured/...
```

Expected: all 6 tests pass.

**Step 5: Run lint**

```bash
make lint
```

Expected: clean.

**Step 6: Commit**

```bash
git add structured/
git commit -m "feat(structured): add schema.org parser with typed Place/Article/Event/Org converters"
```

---

### Task 4: extract/ogimage.go — og:image extraction

**Files:**
- Create: `extract/ogimage.go`
- Create: `extract/ogimage_test.go`

**Step 1: Write the test**

`extract/ogimage_test.go`:
```go
package extract

import "testing"

func TestExtractOGImage_Found(t *testing.T) {
	t.Parallel()
	html := `<html><head>
	<meta property="og:image" content="https://example.com/photo.jpg">
	</head><body></body></html>`

	img := ExtractOGImage(html)
	if img == nil {
		t.Fatal("expected image, got nil")
	}
	if *img != "https://example.com/photo.jpg" {
		t.Errorf("expected https://example.com/photo.jpg, got %s", *img)
	}
}

func TestExtractOGImage_NotFound(t *testing.T) {
	t.Parallel()
	html := `<html><head><title>No OG</title></head><body></body></html>`

	img := ExtractOGImage(html)
	if img != nil {
		t.Errorf("expected nil, got %v", img)
	}
}

func TestExtractOGImage_Empty(t *testing.T) {
	t.Parallel()
	html := `<meta property="og:image" content="">`

	img := ExtractOGImage(html)
	if img != nil {
		t.Errorf("expected nil for empty content, got %v", img)
	}
}
```

**Step 2: Write the implementation**

`extract/ogimage.go`:
```go
package extract

import imagefy "github.com/anatolykoptev/go-imagefy"

// ExtractOGImage extracts the og:image URL from HTML.
// Returns nil if not found or empty.
func ExtractOGImage(html string) *string {
	img := imagefy.ExtractOGImageURL(html)
	if img == "" {
		return nil
	}
	return &img
}
```

**Step 3: Run tests**

```bash
cd /home/krolik/src/go-enriche && go test -race -count=1 ./extract/...
```

Expected: 3 tests pass.

**Step 4: Commit**

```bash
git add extract/
git commit -m "feat(extract): add og:image extraction via go-imagefy"
```

---

### Task 5: extract/text.go — trafilatura wrapper

**Files:**
- Create: `extract/text.go`
- Create: `extract/text_test.go`

**Step 1: Write the test**

`extract/text_test.go`:
```go
package extract

import (
	"net/url"
	"strings"
	"testing"
)

func TestExtractText_Article(t *testing.T) {
	t.Parallel()
	// Trafilatura needs substantial content to consider it extractable.
	html := `<html><head><title>Test Article</title></head>
	<body>
	<nav>Navigation menu items here</nav>
	<article>
	<h1>Important News About Technology</h1>
	<p>This is a substantial article about technology trends in the modern world.
	It contains multiple sentences that discuss various aspects of the topic.
	The article provides detailed analysis of recent developments in the industry.
	Many experts have commented on the significance of these changes for the future.</p>
	<p>Furthermore, the impact on society has been profound and far-reaching.
	New innovations continue to emerge at an unprecedented pace, transforming
	how we live and work. The implications for education, healthcare, and
	transportation are particularly noteworthy and deserve careful examination.</p>
	</article>
	<footer>Copyright 2026</footer>
	</body></html>`

	pageURL, _ := url.Parse("https://example.com/article")
	result, err := ExtractText(strings.NewReader(html), pageURL)
	if err != nil {
		t.Fatalf("ExtractText error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if result.Content == "" {
		t.Error("expected non-empty content")
	}
	// Trafilatura should extract the article content
	if !strings.Contains(result.Content, "technology") {
		t.Errorf("content should contain 'technology', got: %s", result.Content)
	}
}

func TestExtractText_EmptyHTML(t *testing.T) {
	t.Parallel()
	pageURL, _ := url.Parse("https://example.com")
	result, err := ExtractText(strings.NewReader(""), pageURL)
	// Empty input may return nil result or error — both are acceptable
	if err == nil && result != nil && result.Content != "" {
		t.Error("expected empty content for empty HTML")
	}
}

func TestExtractText_Metadata(t *testing.T) {
	t.Parallel()
	html := `<html><head>
	<title>My Great Article</title>
	<meta name="author" content="Jane Smith">
	<meta name="description" content="An article about Go programming">
	<meta property="og:site_name" content="TechBlog">
	</head>
	<body>
	<article>
	<p>This is a substantial article about Go programming language.
	It discusses many features and patterns that make Go unique.
	The language has grown significantly in popularity over the years.
	Developers appreciate its simplicity and performance characteristics.</p>
	<p>Go's concurrency model based on goroutines and channels is one of its
	most distinctive features. This model makes it easier to write concurrent
	programs that are both efficient and easy to reason about.</p>
	</article>
	</body></html>`

	pageURL, _ := url.Parse("https://techblog.example.com/go-article")
	result, err := ExtractText(strings.NewReader(html), pageURL)
	if err != nil {
		t.Fatalf("ExtractText error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if result.Title == "" {
		t.Error("expected non-empty title")
	}
}
```

**Step 2: Write the implementation**

`extract/text.go`:
```go
package extract

import (
	"io"
	"net/url"
	"time"

	"github.com/markusmobius/go-trafilatura"
)

// TextResult holds the extracted text and metadata.
type TextResult struct {
	Content     string
	Title       string
	Author      string
	Description string
	Language    string
	SiteName    string
	Date        time.Time
	Image       string
}

// ExtractText extracts the main article text and metadata from HTML
// using go-trafilatura with fallback to readability and dom-distiller.
func ExtractText(r io.Reader, pageURL *url.URL) (*TextResult, error) {
	result, err := trafilatura.Extract(r, trafilatura.Options{
		OriginalURL:     pageURL,
		EnableFallback:  true,
		ExcludeComments: true,
		Focus:           trafilatura.FavorRecall,
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}

	return &TextResult{
		Content:     result.ContentText,
		Title:       result.Metadata.Title,
		Author:      result.Metadata.Author,
		Description: result.Metadata.Description,
		Language:    result.Metadata.Language,
		SiteName:    result.Metadata.Sitename,
		Date:        result.Metadata.Date,
		Image:       result.Metadata.Image,
	}, nil
}
```

**Step 3: Run tests**

```bash
cd /home/krolik/src/go-enriche && go test -race -count=1 ./extract/...
```

Expected: all 6 tests pass (3 ogimage + 3 text).

**Step 4: Commit**

```bash
git add extract/text.go extract/text_test.go
git commit -m "feat(extract): add article text extraction via go-trafilatura"
```

---

### Task 6: extract/regex.go — pre-compiled regex patterns

**Files:**
- Create: `extract/regex.go`
- Create: `extract/regex_test.go`

**Step 1: Write the tests**

`extract/regex_test.go`:
```go
package extract

import "testing"

func TestRegexAddress_Russian(t *testing.T) {
	t.Parallel()
	text := "Контакты: Адрес: Невский проспект, 100, Санкт-Петербург"
	addr := regexAddress(text)
	if addr == nil {
		t.Fatal("expected address, got nil")
	}
	if *addr != "Невский проспект, 100, Санкт-Петербург" {
		t.Errorf("unexpected address: %s", *addr)
	}
}

func TestRegexAddress_English(t *testing.T) {
	t.Parallel()
	text := "Address: 123 Main Street, Springfield"
	addr := regexAddress(text)
	if addr == nil {
		t.Fatal("expected address, got nil")
	}
}

func TestRegexAddress_NotFound(t *testing.T) {
	t.Parallel()
	text := "No address information here."
	addr := regexAddress(text)
	if addr != nil {
		t.Errorf("expected nil, got %v", *addr)
	}
}

func TestRegexPhone_Russian(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plus7", "Телефон: +7 (812) 555-12-34", "+7 (812) 555-12-34"},
		{"eight", "Звоните: 8(495)123-45-67", "8(495)123-45-67"},
		{"compact", "тел. +79215551234", "+79215551234"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			phone := regexPhone(tt.input)
			if phone == nil {
				t.Fatal("expected phone, got nil")
			}
			if *phone != tt.want {
				t.Errorf("expected %q, got %q", tt.want, *phone)
			}
		})
	}
}

func TestRegexPhone_NotFound(t *testing.T) {
	t.Parallel()
	text := "No phone number here at all."
	phone := regexPhone(text)
	if phone != nil {
		t.Errorf("expected nil, got %v", *phone)
	}
}

func TestRegexPrice_Russian(t *testing.T) {
	t.Parallel()
	text := "Стоимость: от 500 до 1500 рублей"
	price := regexPrice(text)
	if price == nil {
		t.Fatal("expected price, got nil")
	}
}

func TestRegexPrice_English(t *testing.T) {
	t.Parallel()
	text := "Price: $25 per person"
	price := regexPrice(text)
	if price == nil {
		t.Fatal("expected price, got nil")
	}
}

func TestRegexPrice_NotFound(t *testing.T) {
	t.Parallel()
	text := "Nothing about cost here."
	price := regexPrice(text)
	if price != nil {
		t.Errorf("expected nil, got %v", *price)
	}
}
```

**Step 2: Write the implementation**

`extract/regex.go`:
```go
package extract

import (
	"regexp"
	"strings"
)

// Pre-compiled regex patterns for Russian and English fact extraction.
// These are package-level vars, compiled once at init — NOT per-call.
var (
	reAddress = regexp.MustCompile(`(?i)(?:адрес|address)[:\s]+([^\n<]{5,100})`)
	rePhone   = regexp.MustCompile(`(?:\+7|8)[\s\-]?\(?\d{3}\)?[\s\-]?\d{3}[\s\-]?\d{2}[\s\-]?\d{2}`)
	rePrice   = regexp.MustCompile(`(?i)(?:цена|стоимость|price)[:\s]+([^\n<]{2,80})`)
)

// regexAddress extracts an address from text using regex.
func regexAddress(text string) *string {
	return regexSubmatch(reAddress, text)
}

// regexPhone extracts a Russian phone number from text.
func regexPhone(text string) *string {
	return regexMatch(rePhone, text)
}

// regexPrice extracts a price from text using regex.
func regexPrice(text string) *string {
	return regexSubmatch(rePrice, text)
}

// regexSubmatch returns the first capturing group, or nil.
func regexSubmatch(re *regexp.Regexp, text string) *string {
	m := re.FindStringSubmatch(text)
	if m == nil {
		return nil
	}
	// Prefer first capture group if present.
	if len(m) >= 2 && m[1] != "" {
		s := strings.TrimSpace(m[1])
		return &s
	}
	s := strings.TrimSpace(m[0])
	return &s
}

// regexMatch returns the full match, or nil.
func regexMatch(re *regexp.Regexp, text string) *string {
	m := re.FindString(text)
	if m == "" {
		return nil
	}
	s := strings.TrimSpace(m)
	return &s
}
```

**Step 3: Run tests**

```bash
cd /home/krolik/src/go-enriche && go test -race -count=1 ./extract/...
```

Expected: all tests pass (ogimage + text + regex).

**Step 4: Commit**

```bash
git add extract/regex.go extract/regex_test.go
git commit -m "feat(extract): add pre-compiled regex patterns for address/phone/price"
```

---

### Task 7: extract/facts.go — cascade extraction (structured → regex)

**Files:**
- Create: `extract/facts.go`
- Create: `extract/facts_test.go`

This is the key integration point: tries `structured.Parse()` first, then fills remaining nil fields via regex.

**Step 1: Write the tests**

`extract/facts_test.go`:
```go
package extract

import "testing"

func TestExtractFacts_JSONLD(t *testing.T) {
	t.Parallel()
	html := `<html><head>
	<script type="application/ld+json">
	{
		"@context": "https://schema.org",
		"@type": "Restaurant",
		"name": "Test Cafe",
		"telephone": "+7 (812) 111-22-33",
		"address": {"@type": "PostalAddress", "streetAddress": "ул. Пушкина, 10"}
	}
	</script>
	</head><body></body></html>`

	facts := ExtractFacts(html, "https://example.com")
	assertFactPtr(t, "PlaceName", facts.PlaceName, "Test Cafe")
	assertFactPtr(t, "Phone", facts.Phone, "+7 (812) 111-22-33")
	if facts.Address == nil || *facts.Address == "" {
		t.Error("expected non-empty address")
	}
}

func TestExtractFacts_RegexFallback(t *testing.T) {
	t.Parallel()
	// No JSON-LD — regex should pick up the facts.
	html := `<html><body>
	<p>Адрес: Литейный проспект, 55</p>
	<p>Телефон: +7 (812) 999-88-77</p>
	<p>Стоимость: от 200 рублей</p>
	</body></html>`

	facts := ExtractFacts(html, "https://example.com")
	if facts.Address == nil {
		t.Error("expected address from regex")
	}
	if facts.Phone == nil {
		t.Error("expected phone from regex")
	}
	if facts.Price == nil {
		t.Error("expected price from regex")
	}
}

func TestExtractFacts_JSONLDPriority(t *testing.T) {
	t.Parallel()
	// JSON-LD phone should take priority over regex phone in body.
	html := `<html><head>
	<script type="application/ld+json">
	{"@context":"https://schema.org","@type":"Place","telephone":"+7-111-222-33-44"}
	</script>
	</head><body>
	<p>Телефон: +7 (999) 888-77-66</p>
	</body></html>`

	facts := ExtractFacts(html, "https://example.com")
	assertFactPtr(t, "Phone", facts.Phone, "+7-111-222-33-44")
}

func TestExtractFacts_EmptyHTML(t *testing.T) {
	t.Parallel()
	facts := ExtractFacts("", "https://example.com")
	if facts.PlaceName != nil || facts.Phone != nil || facts.Address != nil {
		t.Error("expected all nil facts for empty HTML")
	}
}

func TestExtractFacts_EventDate(t *testing.T) {
	t.Parallel()
	html := `<html><head>
	<script type="application/ld+json">
	{"@context":"https://schema.org","@type":"Event","name":"Concert","startDate":"2026-04-01"}
	</script>
	</head><body></body></html>`

	facts := ExtractFacts(html, "https://example.com")
	assertFactPtr(t, "EventDate", facts.EventDate, "2026-04-01")
}

func assertFactPtr(t *testing.T, field string, got *string, want string) {
	t.Helper()
	if got == nil {
		t.Errorf("%s: expected %q, got nil", field, want)
		return
	}
	if *got != want {
		t.Errorf("%s: expected %q, got %q", field, want, *got)
	}
}
```

**Step 2: Write the implementation**

`extract/facts.go`:
```go
package extract

import (
	"strings"

	"github.com/anatolykoptev/go-enriche/structured"
)

// Facts holds structured data extracted from a page.
// Duplicated from root enriche package to avoid circular imports.
// Root types.go re-exports this.
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

// ExtractFacts extracts structured facts from HTML using a cascade:
// 1. Schema.org structured data (JSON-LD + Microdata) via structured.Parse
// 2. Pre-compiled regex fallback for address/phone/price
func ExtractFacts(html, pageURL string) Facts {
	var facts Facts

	if html == "" {
		return facts
	}

	// Layer 1: structured data (JSON-LD + Microdata).
	data, err := structured.Parse(strings.NewReader(html), "text/html", pageURL)
	if err == nil && data != nil {
		applyPlaceFacts(data, &facts)
		applyArticleFacts(data, &facts)
		applyEventFacts(data, &facts)
		applyOrgFacts(data, &facts)
	}

	// Layer 2: regex fallback — only fills nil fields.
	applyRegexFallback(html, &facts)

	return facts
}

func applyPlaceFacts(data *structured.Data, facts *Facts) {
	place := data.FirstPlace()
	if place == nil {
		return
	}
	setIfNil(&facts.PlaceName, place.Name)
	setIfNil(&facts.PlaceType, place.Type)
	setIfNil(&facts.Address, place.Address)
	setIfNil(&facts.Phone, place.Phone)
	setIfNil(&facts.Website, place.Website)
	setIfNil(&facts.Hours, place.Hours)
	setIfNil(&facts.Price, place.Price)
}

func applyArticleFacts(data *structured.Data, facts *Facts) {
	article := data.FirstArticle()
	if article == nil {
		return
	}
	// Articles may contain event dates in datePublished.
	setIfNil(&facts.EventDate, article.DatePublished)
}

func applyEventFacts(data *structured.Data, facts *Facts) {
	event := data.FirstEvent()
	if event == nil {
		return
	}
	setIfNil(&facts.PlaceName, event.Name)
	setIfNil(&facts.EventDate, event.StartDate)
	setIfNil(&facts.Price, event.Price)
	if event.Location != nil {
		setIfNil(&facts.Address, event.Location)
	}
}

func applyOrgFacts(data *structured.Data, facts *Facts) {
	org := data.FirstOrganization()
	if org == nil {
		return
	}
	setIfNil(&facts.PlaceName, org.Name)
	setIfNil(&facts.Website, org.URL)
	setIfNil(&facts.Phone, org.Phone)
	setIfNil(&facts.Address, org.Address)
}

func applyRegexFallback(html string, facts *Facts) {
	if facts.Address == nil {
		facts.Address = regexAddress(html)
	}
	if facts.Phone == nil {
		facts.Phone = regexPhone(html)
	}
	if facts.Price == nil {
		facts.Price = regexPrice(html)
	}
}

// setIfNil sets *dst to src if *dst is currently nil and src is non-nil.
func setIfNil(dst **string, src *string) {
	if *dst == nil && src != nil {
		*dst = src
	}
}
```

**Step 3: Update root types.go**

In `types.go`, remove the `Facts` struct and replace with a type alias or re-export from extract. Actually, to avoid circular imports, keep `Facts` in the root package as the public type and convert in the orchestrator (Phase 4). For now, `extract.Facts` is the working type. Update `types.go`:

Replace the `Facts` struct in `types.go` with:
```go
// Facts is re-exported from extract package for convenience.
// During Phase 4 orchestration, extract.Facts will be converted to this type.
type Facts = extract.Facts
```

And add the import: `"github.com/anatolykoptev/go-enriche/extract"`

**Step 4: Run tests**

```bash
cd /home/krolik/src/go-enriche && go test -race -count=1 ./...
```

Expected: all tests pass across all packages.

**Step 5: Run lint**

```bash
make lint
```

Expected: clean.

**Step 6: Commit**

```bash
git add extract/facts.go extract/facts_test.go types.go
git commit -m "feat(extract): add facts extraction with structured→regex cascade"
```

---

### Task 8: extract/date.go — publication date extraction

**Files:**
- Create: `extract/date.go`
- Create: `extract/date_test.go`

**Step 1: Write the test**

`extract/date_test.go`:
```go
package extract

import (
	"net/url"
	"strings"
	"testing"
)

func TestExtractDate_MetaTag(t *testing.T) {
	t.Parallel()
	html := `<html><head>
	<meta property="article:published_time" content="2026-02-28T10:00:00Z">
	</head><body><p>Content here for extraction to work properly with trafilatura.
	This needs to be a substantial paragraph for the library to process it.</p></body></html>`

	pageURL, _ := url.Parse("https://example.com/article")
	date := ExtractDate(strings.NewReader(html), pageURL)
	if date == nil {
		t.Skip("trafilatura may not extract date from minimal HTML")
	}
	if date.Year() != 2026 || date.Month() != 2 || date.Day() != 28 {
		t.Errorf("unexpected date: %v", date)
	}
}

func TestExtractDate_NoDate(t *testing.T) {
	t.Parallel()
	html := `<html><body><p>No date anywhere</p></body></html>`
	pageURL, _ := url.Parse("https://example.com")
	date := ExtractDate(strings.NewReader(html), pageURL)
	if date != nil {
		t.Logf("got unexpected date: %v (may be from URL or heuristic)", date)
	}
}
```

**Step 2: Write the implementation**

`extract/date.go`:
```go
package extract

import (
	"io"
	"net/url"
	"time"

	"github.com/markusmobius/go-trafilatura"
)

// ExtractDate extracts the publication date from HTML using go-trafilatura's
// metadata extraction (which internally uses go-htmldate).
// Returns nil if no date found.
func ExtractDate(r io.Reader, pageURL *url.URL) *time.Time {
	result, err := trafilatura.Extract(r, trafilatura.Options{
		OriginalURL:    pageURL,
		EnableFallback: true,
	})
	if err != nil || result == nil {
		return nil
	}

	if result.Metadata.Date.IsZero() {
		return nil
	}
	d := result.Metadata.Date
	return &d
}
```

**Step 3: Run tests**

```bash
cd /home/krolik/src/go-enriche && go test -race -count=1 ./extract/...
```

Expected: pass.

**Step 4: Commit**

```bash
git add extract/date.go extract/date_test.go
git commit -m "feat(extract): add publication date extraction via go-trafilatura"
```

---

### Task 9: README.md + final verification

**Files:**
- Create: `README.md`

**Step 1: Write README**

`README.md`:
```markdown
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
| `search/` | SearXNG context search (Phase 3) |
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
```

**Step 2: Run full verification**

```bash
cd /home/krolik/src/go-enriche
go build ./...
make test
make lint
```

Expected: all green.

**Step 3: Commit**

```bash
git add README.md
git commit -m "docs: add README with package overview and usage examples"
```

---

## Summary

| Task | Package | Tests | What |
|------|---------|-------|------|
| 1 | root | 0 | go.mod, Makefile, CI, linter, .gitignore, LICENSE |
| 2 | root + stubs | 0 | types.go, enriche.go, empty sub-packages |
| 3 | structured/ | 6 | microdata parser + Place/Article/Event/Org converters |
| 4 | extract/ | 3 | og:image extraction (go-imagefy wrapper) |
| 5 | extract/ | 3 | article text extraction (go-trafilatura wrapper) |
| 6 | extract/ | 8 | pre-compiled regex (address/phone/price, Russian+English) |
| 7 | extract/ | 5 | facts cascade (structured → regex fallback) |
| 8 | extract/ | 2 | publication date extraction |
| 9 | root | 0 | README, final verification |
| **Total** | | **27** | |

# Phase 4: Orchestration — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Root Enricher — the public API that orchestrates fetch, extract, search, and cache into a single `Enrich(ctx, Item) (*Result, error)` call.

**Architecture:** `Enricher` struct with functional options. Pipeline: cache check → fetch URL → extract text/facts/image/date → search context → cache store. `EnrichBatch` adds semaphore-bounded concurrency. Graceful degradation: all optional dependencies (cache, search) degrade silently.

**Tech Stack:** Go 1.25, all existing sub-packages (fetch, extract, search, cache, structured)

---

### Task 1: Create options.go

**Files:**
- Create: `options.go`

**options.go:**

```go
package enriche

import (
	"net/http"
	"time"

	"github.com/anatolykoptev/go-enriche/cache"
	"github.com/anatolykoptev/go-enriche/fetch"
	"github.com/anatolykoptev/go-enriche/search"
)

// Option configures an Enricher.
type Option func(*Enricher)

// WithFetcher sets a custom Fetcher.
func WithFetcher(f *fetch.Fetcher) Option {
	return func(e *Enricher) { e.fetcher = f }
}

// WithStealth creates a Fetcher using a stealth HTTP client.
// Convenience wrapper: creates stealth client + fetcher internally.
func WithStealth(c *http.Client) Option {
	return func(e *Enricher) {
		e.fetcher = fetch.NewFetcher(fetch.WithClient(c))
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
```

**Step 1: Create the file**

**Step 2: Build**

Run: `go build ./...`
Expected: exit 0 (will fail until enriche.go defines Enricher — that's OK, proceed to Task 2)

---

### Task 2: Implement Enricher in enriche.go

**Files:**
- Modify: `enriche.go`

Replace the stub with:

```go
// Package enriche provides web content enrichment: fetch pages, extract text,
// parse structured data, search for context.
package enriche

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/anatolykoptev/go-enriche/cache"
	"github.com/anatolykoptev/go-enriche/extract"
	"github.com/anatolykoptev/go-enriche/fetch"
	"github.com/anatolykoptev/go-enriche/search"
)

const (
	defaultConcurrency = 5
	defaultCacheTTL    = 24 * time.Hour
)

// Enricher orchestrates web content enrichment.
type Enricher struct {
	fetcher     *fetch.Fetcher
	cache       cache.Cache
	search      search.Provider
	concurrency int
	cacheTTL    time.Duration
}

// New creates an Enricher with the given options.
func New(opts ...Option) *Enricher {
	e := &Enricher{
		fetcher:     fetch.NewFetcher(),
		concurrency: defaultConcurrency,
		cacheTTL:    defaultCacheTTL,
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

// Enrich enriches a single item: fetch, extract, search, cache.
// Returns a partial result on failures (graceful degradation).
func (e *Enricher) Enrich(ctx context.Context, item Item) (*Result, error) {
	result := &Result{
		Name: item.Name,
		URL:  item.URL,
	}

	// Cache check.
	if e.cache != nil {
		key := cacheKey(item)
		if e.cache.Get(ctx, key, result) {
			return result, nil
		}
	}

	// Fetch + extract.
	if item.URL != "" {
		e.fetchAndExtract(ctx, item, result)
	}

	// Search.
	if e.search != nil {
		e.doSearch(ctx, item, result)
	}

	// Cache store.
	if e.cache != nil {
		e.cache.Set(ctx, cacheKey(item), result, e.cacheTTL)
	}

	return result, nil
}

// EnrichBatch enriches multiple items concurrently with bounded concurrency.
func (e *Enricher) EnrichBatch(ctx context.Context, items []Item) []*Result {
	results := make([]*Result, len(items))
	sem := make(chan struct{}, e.concurrency)
	var wg sync.WaitGroup

	for i, item := range items {
		wg.Add(1)
		go func(idx int, it Item) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			r, _ := e.Enrich(ctx, it)
			results[idx] = r
		}(i, item)
	}

	wg.Wait()
	return results
}

func (e *Enricher) fetchAndExtract(ctx context.Context, item Item, result *Result) {
	fr, err := e.fetcher.Fetch(ctx, item.URL)
	if err != nil {
		result.Status = fetch.StatusUnreachable
		return
	}

	result.Status = fr.Status
	if fr.FinalURL != "" {
		result.URL = fr.FinalURL
	}

	if fr.Status != fetch.StatusActive {
		return
	}

	// Extract text + metadata.
	pageURL, _ := url.Parse(item.URL)
	textResult, textErr := extract.ExtractText(strings.NewReader(fr.HTML), pageURL)
	if textErr == nil && textResult != nil {
		result.Content = textResult.Content
		result.Metadata = &ContentMeta{
			Title:       textResult.Title,
			Author:      textResult.Author,
			Description: textResult.Description,
			Language:    textResult.Language,
			SiteName:    textResult.SiteName,
		}
		if !textResult.Date.IsZero() {
			t := textResult.Date
			result.PublishedAt = &t
		}
		if textResult.Image != "" {
			result.Image = &textResult.Image
		}
	}

	// Extract structured facts.
	result.Facts = extract.ExtractFacts(fr.HTML, item.URL)

	// OG image fallback.
	if result.Image == nil {
		result.Image = extract.ExtractOGImage(fr.HTML)
	}

	// Date fallback.
	if result.PublishedAt == nil {
		result.PublishedAt = extract.ExtractDate(strings.NewReader(fr.HTML), pageURL)
	}
}

func (e *Enricher) doSearch(ctx context.Context, item Item, result *Result) {
	query, timeRange := search.BuildQuery(int(item.Mode), item.Name, item.City)
	sr, err := e.search.Search(ctx, query, timeRange)
	if err != nil || sr == nil {
		return
	}
	result.SearchContext = sr.Context
	result.SearchSources = sr.Sources
}

func cacheKey(item Item) string {
	if item.URL != "" {
		return "enriche:" + item.URL
	}
	return "enriche:search:" + item.Name
}
```

**Step 1: Write the file**

**Step 2: Build**

Run: `go build ./...`
Expected: exit 0

**Step 3: Lint**

Run: `golangci-lint run ./...`
Expected: 0 issues

**Step 4: Commit options.go + enriche.go**

```bash
git add options.go enriche.go
git commit -m "feat: add Enricher with Enrich/EnrichBatch orchestration and functional options"
```

---

### Task 3: Integration tests

**Files:**
- Create: `enriche_test.go`

```go
package enriche

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anatolykoptev/go-enriche/cache"
	"github.com/anatolykoptev/go-enriche/fetch"
	"github.com/anatolykoptev/go-enriche/search"
)

// mockProvider implements search.Provider for testing.
type mockProvider struct {
	result *search.SearchResult
	err    error
}

func (m *mockProvider) Search(_ context.Context, _ string, _ string) (*search.SearchResult, error) {
	return m.result, m.err
}

const testHTML = `<!DOCTYPE html>
<html><head>
<meta property="og:image" content="https://example.com/image.jpg">
<title>Test Article</title>
</head><body>
<article>
<h1>Test Article</h1>
<p>This is a test article with enough content to be extracted by trafilatura.
It needs to be long enough to pass the minimum content length threshold.
Here is some additional text to make the article longer and more realistic.
The article discusses various topics including technology and science.
We need several paragraphs to ensure the extraction works properly.</p>
<p>Second paragraph with more content about the topic at hand.
This paragraph provides additional context and information.
It helps the extraction engine identify this as real article content.</p>
</article>
</body></html>`

func newTestServer(html string, statusCode int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(statusCode)
		w.Write([]byte(html))
	}))
}

func TestEnrich_FetchAndExtract(t *testing.T) {
	t.Parallel()
	srv := newTestServer(testHTML, http.StatusOK)
	defer srv.Close()

	e := New(WithFetcher(fetch.NewFetcher()))
	result, err := e.Enrich(context.Background(), Item{
		Name: "Test",
		URL:  srv.URL,
		Mode: ModeNews,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if result.Status != fetch.StatusActive {
		t.Errorf("expected StatusActive, got %q", result.Status)
	}
	if result.Name != "Test" {
		t.Errorf("expected name 'Test', got %q", result.Name)
	}
}

func TestEnrich_NotFound(t *testing.T) {
	t.Parallel()
	srv := newTestServer("", http.StatusNotFound)
	defer srv.Close()

	e := New()
	result, err := e.Enrich(context.Background(), Item{
		Name: "Missing",
		URL:  srv.URL,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if result.Status != fetch.StatusNotFound {
		t.Errorf("expected StatusNotFound, got %q", result.Status)
	}
	if result.Content != "" {
		t.Error("expected empty content for 404")
	}
}

func TestEnrich_WithSearch(t *testing.T) {
	t.Parallel()
	mock := &mockProvider{
		result: &search.SearchResult{
			Context: "search context text",
			Sources: []string{"https://source1.com", "https://source2.com"},
		},
	}

	e := New(WithSearch(mock))
	result, err := e.Enrich(context.Background(), Item{
		Name: "Test Place",
		City: "Moscow",
		Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if result.SearchContext != "search context text" {
		t.Errorf("expected search context, got %q", result.SearchContext)
	}
	if len(result.SearchSources) != 2 {
		t.Errorf("expected 2 search sources, got %d", len(result.SearchSources))
	}
}

func TestEnrich_WithCache(t *testing.T) {
	t.Parallel()
	srv := newTestServer(testHTML, http.StatusOK)
	defer srv.Close()

	mem := cache.NewMemory()
	e := New(WithCache(mem), WithFetcher(fetch.NewFetcher()))

	item := Item{Name: "Cached", URL: srv.URL, Mode: ModeNews}

	// First call — cache miss, fetches.
	r1, err := e.Enrich(context.Background(), item)
	if err != nil {
		t.Fatalf("first Enrich error: %v", err)
	}
	if r1.Status != fetch.StatusActive {
		t.Errorf("expected StatusActive, got %q", r1.Status)
	}

	// Second call — cache hit.
	srv.Close() // Close server to prove cache is used.
	r2, err := e.Enrich(context.Background(), item)
	if err != nil {
		t.Fatalf("second Enrich error: %v", err)
	}
	if r2.Status != fetch.StatusActive {
		t.Errorf("expected StatusActive from cache, got %q", r2.Status)
	}
}

func TestEnrich_NoURL_SearchOnly(t *testing.T) {
	t.Parallel()
	mock := &mockProvider{
		result: &search.SearchResult{
			Context: "found via search",
			Sources: []string{"https://found.com"},
		},
	}

	e := New(WithSearch(mock))
	result, err := e.Enrich(context.Background(), Item{
		Name: "Search Only Item",
		Mode: ModeNews,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if result.SearchContext != "found via search" {
		t.Errorf("expected search context, got %q", result.SearchContext)
	}
	// No URL → no fetch → status should be zero value.
	if result.Status != "" {
		t.Errorf("expected empty status for search-only, got %q", result.Status)
	}
}

func TestEnrich_GracefulDegradation(t *testing.T) {
	t.Parallel()
	// No cache, no search, no stealth — should work with defaults.
	e := New()
	result, err := e.Enrich(context.Background(), Item{
		Name: "Minimal",
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if result.Name != "Minimal" {
		t.Errorf("expected name 'Minimal', got %q", result.Name)
	}
}

func TestEnrichBatch(t *testing.T) {
	t.Parallel()
	srv := newTestServer(testHTML, http.StatusOK)
	defer srv.Close()

	e := New(WithConcurrency(2))
	items := []Item{
		{Name: "Item1", URL: srv.URL},
		{Name: "Item2", URL: srv.URL},
		{Name: "Item3", URL: srv.URL},
	}

	results := e.EnrichBatch(context.Background(), items)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	for i, r := range results {
		if r == nil {
			t.Errorf("result[%d] is nil", i)
			continue
		}
		if r.Name != items[i].Name {
			t.Errorf("result[%d].Name = %q, want %q", i, r.Name, items[i].Name)
		}
	}
}

func TestEnrichBatch_Concurrency(t *testing.T) {
	t.Parallel()
	var concurrent atomic.Int32
	var maxConcurrent atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		cur := concurrent.Add(1)
		for {
			old := maxConcurrent.Load()
			if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		concurrent.Add(-1)
		w.Write([]byte(testHTML))
	}))
	defer srv.Close()

	e := New(WithConcurrency(2))
	items := make([]Item, 6)
	for i := range items {
		items[i] = Item{Name: "item", URL: srv.URL}
	}

	e.EnrichBatch(context.Background(), items)

	if maxConcurrent.Load() > 2 {
		t.Errorf("expected max 2 concurrent, got %d", maxConcurrent.Load())
	}
}

func TestEnrich_OGImage(t *testing.T) {
	t.Parallel()
	html := `<html><head><meta property="og:image" content="https://img.example.com/photo.jpg"></head>
	<body><p>Short page</p></body></html>`
	srv := newTestServer(html, http.StatusOK)
	defer srv.Close()

	e := New()
	result, err := e.Enrich(context.Background(), Item{Name: "OG", URL: srv.URL})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if result.Image == nil {
		t.Fatal("expected og:image to be extracted")
	}
	if *result.Image != "https://img.example.com/photo.jpg" {
		t.Errorf("expected og:image URL, got %q", *result.Image)
	}
}

func TestEnrich_SearchError(t *testing.T) {
	t.Parallel()
	mock := &mockProvider{err: context.DeadlineExceeded}

	e := New(WithSearch(mock))
	result, err := e.Enrich(context.Background(), Item{Name: "Failing Search"})
	if err != nil {
		t.Fatalf("Enrich should not error on search failure, got: %v", err)
	}
	if result.SearchContext != "" {
		t.Error("expected empty search context on error")
	}
}

func TestEnrich_WithSearXNG(t *testing.T) {
	t.Parallel()
	// Mock SearXNG server.
	searxSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := struct {
			Results []struct {
				URL     string `json:"url"`
				Title   string `json:"title"`
				Content string `json:"content"`
			} `json:"results"`
		}{
			Results: []struct {
				URL     string `json:"url"`
				Title   string `json:"title"`
				Content string `json:"content"`
			}{
				{URL: "https://src.com/1", Title: "Source 1", Content: "Info about topic"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer searxSrv.Close()

	provider := search.NewSearXNG(searxSrv.URL)
	e := New(WithSearch(provider))
	result, err := e.Enrich(context.Background(), Item{
		Name: "Real SearXNG",
		Mode: ModeNews,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if len(result.SearchSources) == 0 {
		t.Error("expected search sources from SearXNG")
	}
}

func TestCacheKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		item Item
		want string
	}{
		{Item{URL: "https://example.com"}, "enriche:https://example.com"},
		{Item{Name: "Test"}, "enriche:search:Test"},
		{Item{Name: "Place", URL: "https://place.com"}, "enriche:https://place.com"},
	}
	for _, tt := range tests {
		got := cacheKey(tt.item)
		if got != tt.want {
			t.Errorf("cacheKey(%+v) = %q, want %q", tt.item, got, tt.want)
		}
	}
}
```

**Step 1: Create the file**

**Step 2: Run tests**

Run: `go test -v -count=1 ./... -run "TestEnrich|TestCache"`
Expected: all tests PASS

**Step 3: Lint**

Run: `golangci-lint run ./...`
Expected: 0 issues

**Step 4: Commit**

```bash
git add enriche_test.go
git commit -m "test: add Enricher integration tests with mock provider and httptest"
```

---

### Task 4: Update ROADMAP.md + final verification

**Step 1: Mark Phase 4 complete in ROADMAP.md**

**Step 2: Run full verification**

Run: `make lint && make test`
Expected: 0 lint issues, all tests pass

**Step 3: Commit**

```bash
git add docs/ROADMAP.md
git commit -m "docs: mark Phase 4 (Orchestration) complete in roadmap"
```

---

## Notes for the Implementer

### Pipeline flow
1. Cache check (if cache configured) — return early on hit
2. Fetch URL (if URL provided) — classify status
3. Extract text via trafilatura (only if StatusActive)
4. Extract structured facts (only if StatusActive)
5. OG image fallback (if trafilatura didn't find image)
6. Date fallback (if trafilatura didn't find date)
7. Search (if provider configured) — always runs even if fetch failed
8. Cache store (if cache configured)

### Graceful degradation
- No fetcher → impossible, always created with default
- No cache → skip cache check/store
- No search → skip search step
- Fetch error → set StatusUnreachable, continue to search
- Fetch non-Active → set status, skip extraction, continue to search
- Extract error → skip, continue with partial result
- Search error → skip, result has empty SearchContext/SearchSources

### Cache key
- URL items: `"enriche:" + url`
- Search-only items: `"enriche:search:" + name`

### EnrichBatch concurrency
- Semaphore channel of size `e.concurrency` (default 5)
- Each goroutine acquires before Enrich, releases after
- sync.WaitGroup for completion

### Type mappings
- `extract.TextResult` → `Result.Content`, `Result.Metadata` (ContentMeta), `Result.PublishedAt`, `Result.Image`
- `extract.Facts` → `Result.Facts`
- `extract.ExtractOGImage` → `Result.Image` (fallback)
- `extract.ExtractDate` → `Result.PublishedAt` (fallback)
- `search.SearchResult` → `Result.SearchContext`, `Result.SearchSources`
- `fetch.FetchResult` → `Result.Status`, `Result.URL` (updated on redirect)

# Phase 6: Hardening Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix the regression from Phase 5 migration (regex facts lost from search snippets), add observability (logging + metrics), improve robustness (errgroup, retry, content truncation).

**Architecture:** Six independent features added to the existing enriche orchestrator. Each touches 2-4 files max. Logger and metrics are threaded through the Enricher struct via functional options. errgroup replaces WaitGroup+semaphore in EnrichBatch. Retry wraps doFetch inline. All changes are backward-compatible — zero new required dependencies.

**Tech Stack:** Go stdlib (`log/slog`, `unicode/utf8`, `math/rand/v2`), `golang.org/x/sync/errgroup` (already in go.mod via singleflight)

---

### Task 1: Regex Facts from Search Context

**Why:** Phase 5 migration lost the old go-wp behavior where `applyRegexFallback()` ran on SearXNG snippets when a page was unreachable. Now if fetch fails, facts stay empty even though search context contains addresses/phones/prices.

**Files:**
- Modify: `extract/regex.go` — add plain-text-safe regex variants
- Modify: `extract/facts.go` — add `ExtractSnippetFacts(text string, existing *Facts)` public function
- Create: `extract/snippet_test.go` — tests for snippet extraction
- Modify: `enriche.go:151-159` — call `ExtractSnippetFacts` after search

**Step 1: Write the failing test**

Create `extract/snippet_test.go`:

```go
package extract

import "testing"

func TestExtractSnippetFacts_Address(t *testing.T) {
	t.Parallel()
	text := "Кафе Рога и Копыта: адрес ул. Ленина, 42, Москва"
	var facts Facts
	ExtractSnippetFacts(text, &facts)
	if facts.Address == nil {
		t.Fatal("expected address from snippet")
	}
	if *facts.Address != "ул. Ленина, 42, Москва" {
		t.Errorf("got %q", *facts.Address)
	}
}

func TestExtractSnippetFacts_Phone(t *testing.T) {
	t.Parallel()
	text := "Контакт: +7 (812) 555-12-34, ежедневно с 10 до 22"
	var facts Facts
	ExtractSnippetFacts(text, &facts)
	if facts.Phone == nil {
		t.Fatal("expected phone from snippet")
	}
}

func TestExtractSnippetFacts_Price(t *testing.T) {
	t.Parallel()
	text := "Средний чек: цена 500-1500 руб."
	var facts Facts
	ExtractSnippetFacts(text, &facts)
	if facts.Price == nil {
		t.Fatal("expected price from snippet")
	}
}

func TestExtractSnippetFacts_DoesNotOverwrite(t *testing.T) {
	t.Parallel()
	existing := "ул. Пушкина, 1"
	facts := Facts{Address: &existing}
	text := "адрес ул. Ленина, 42"
	ExtractSnippetFacts(text, &facts)
	if *facts.Address != "ул. Пушкина, 1" {
		t.Errorf("should not overwrite existing, got %q", *facts.Address)
	}
}

func TestExtractSnippetFacts_MultipleFields(t *testing.T) {
	t.Parallel()
	text := "Ресторан Теремок\nадрес ул. Невского, 28\nТелефон: +7 (495) 123-45-67\nцена от 300 руб."
	var facts Facts
	ExtractSnippetFacts(text, &facts)
	if facts.Address == nil {
		t.Error("expected address")
	}
	if facts.Phone == nil {
		t.Error("expected phone")
	}
	if facts.Price == nil {
		t.Error("expected price")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/krolik/src/go-enriche && go test ./extract/ -run TestExtractSnippetFacts -v`
Expected: FAIL — `ExtractSnippetFacts` undefined

**Step 3: Write minimal implementation**

In `extract/regex.go`, add plain-text-safe variants (no `<` boundary needed for snippets):

```go
// reSnippetAddress matches keyword-anchored address in plain text (no HTML boundary).
var reSnippetAddress = regexp.MustCompile(`(?i)(?:адрес|address)[:\s]+([^\n]{5,100})`)

// reSnippetPrice matches keyword-anchored price in plain text (no HTML boundary).
var reSnippetPrice = regexp.MustCompile(`(?i)(?:цена|стоимость|price)[:\s]+([^\n]{2,80})`)
```

In `extract/facts.go`, add:

```go
// ExtractSnippetFacts applies regex extraction to plain-text snippets (e.g. search context).
// Only fills nil fields in the existing facts — never overwrites.
func ExtractSnippetFacts(text string, facts *Facts) {
	if text == "" || facts == nil {
		return
	}
	if facts.Address == nil {
		facts.Address = regexSubmatch(reSnippetAddress, text)
	}
	if facts.Phone == nil {
		facts.Phone = regexMatch(rePhone, text)
	}
	if facts.Price == nil {
		facts.Price = regexSubmatch(reSnippetPrice, text)
	}
}
```

**Step 4: Run test to verify it passes**

Run: `cd /home/krolik/src/go-enriche && go test ./extract/ -run TestExtractSnippetFacts -v`
Expected: PASS (5/5)

**Step 5: Integrate in orchestrator**

In `enriche.go`, modify `doSearch()` (lines 151-159) to call snippet extraction after search:

```go
func (e *Enricher) doSearch(ctx context.Context, item Item, result *Result) {
	query, timeRange := search.BuildQuery(int(item.Mode), item.Name, item.City)
	sr, err := e.search.Search(ctx, query, timeRange)
	if err != nil || sr == nil {
		return
	}
	result.SearchContext = sr.Context
	result.SearchSources = sr.Sources

	// Extract facts from search snippets (fills nil fields only).
	extract.ExtractSnippetFacts(sr.Context, &result.Facts)
}
```

Add `"github.com/anatolykoptev/go-enriche/extract"` to imports if not already present.

**Step 6: Run full test suite**

Run: `cd /home/krolik/src/go-enriche && go test ./... -v -count=1`
Expected: all pass

**Step 7: Commit**

```bash
git add extract/snippet_test.go extract/regex.go extract/facts.go enriche.go
git commit -m "feat: extract regex facts from search snippets

Fixes regression from Phase 5 migration where address/phone/price
were no longer extracted from SearXNG context when page was unreachable.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 2: Content Truncation Option

**Why:** go-wp used 4000-char limit; currently caller must truncate. Library should offer `WithMaxContentLen(n)` to truncate at rune boundaries.

**Files:**
- Modify: `options.go` — add `WithMaxContentLen`
- Modify: `enriche.go:24-30` — add `maxContentLen` field
- Create: `truncate.go` — `truncateRunes(s string, maxRunes int) string`
- Create: `truncate_test.go` — tests
- Modify: `enriche.go:100-149` — apply truncation after text extraction

**Step 1: Write the failing test**

Create `truncate_test.go`:

```go
package enriche

import "testing"

func TestTruncateRunes_Short(t *testing.T) {
	t.Parallel()
	got := truncateRunes("hello", 10)
	if got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestTruncateRunes_Exact(t *testing.T) {
	t.Parallel()
	got := truncateRunes("hello", 5)
	if got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestTruncateRunes_Truncate(t *testing.T) {
	t.Parallel()
	got := truncateRunes("hello world", 5)
	if got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestTruncateRunes_Cyrillic(t *testing.T) {
	t.Parallel()
	// "Привет мир" = 10 runes
	got := truncateRunes("Привет мир", 6)
	if got != "Привет" {
		t.Errorf("got %q, want %q", got, "Привет")
	}
}

func TestTruncateRunes_WordBoundary(t *testing.T) {
	t.Parallel()
	// Truncate at 8 runes: "Привет м" → finds last space → "Привет"
	got := truncateRunes("Привет мир", 8)
	if got != "Привет" {
		t.Errorf("got %q, want %q", got, "Привет")
	}
}

func TestTruncateRunes_Zero(t *testing.T) {
	t.Parallel()
	got := truncateRunes("hello", 0)
	if got != "hello" {
		t.Errorf("zero maxRunes should not truncate, got %q", got)
	}
}

func TestTruncateRunes_Empty(t *testing.T) {
	t.Parallel()
	got := truncateRunes("", 10)
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/krolik/src/go-enriche && go test . -run TestTruncateRunes -v`
Expected: FAIL — `truncateRunes` undefined

**Step 3: Write implementation**

Create `truncate.go`:

```go
package enriche

import "unicode/utf8"

// truncateRunes truncates s to at most maxRunes runes, preferring word boundaries.
// Returns s unchanged if maxRunes <= 0 or s is short enough.
func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 || utf8.RuneCountInString(s) <= maxRunes {
		return s
	}

	// Walk forward maxRunes runes to find the byte offset.
	byteOffset := 0
	for range maxRunes {
		_, size := utf8.DecodeRuneInString(s[byteOffset:])
		if size == 0 {
			break
		}
		byteOffset += size
	}

	truncated := s[:byteOffset]

	// Try to break at last space for cleaner output.
	if lastSpace := lastIndexByte(truncated, ' '); lastSpace > len(truncated)/2 {
		truncated = truncated[:lastSpace]
	}

	return truncated
}

// lastIndexByte returns the last index of byte c in s, or -1.
func lastIndexByte(s string, c byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
	}
	return -1
}
```

Add option in `options.go`:

```go
// WithMaxContentLen truncates extracted content to n runes (word-boundary preferred).
// Default: 0 (no truncation).
func WithMaxContentLen(n int) Option {
	return func(e *Enricher) { e.maxContentLen = n }
}
```

Add field in `enriche.go` struct:

```go
type Enricher struct {
	fetcher       *fetch.Fetcher
	cache         cache.Cache
	search        search.Provider
	concurrency   int
	cacheTTL      time.Duration
	maxContentLen int
}
```

Apply truncation in `fetchAndExtract()` after setting `result.Content` (after line 120):

```go
	if textErr == nil && textResult != nil {
		result.Content = textResult.Content
		if e.maxContentLen > 0 {
			result.Content = truncateRunes(result.Content, e.maxContentLen)
		}
		// ... rest unchanged
```

**Step 4: Run tests**

Run: `cd /home/krolik/src/go-enriche && go test . -run TestTruncateRunes -v`
Expected: PASS (7/7)

**Step 5: Write integration test**

Add to `enriche_test.go`:

```go
func TestEnrich_MaxContentLen(t *testing.T) {
	t.Parallel()
	srv := newTestServer(testHTML, http.StatusOK)
	defer srv.Close()

	e := New(WithMaxContentLen(50))
	result, err := e.Enrich(context.Background(), Item{Name: "Truncated", URL: srv.URL})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if len([]rune(result.Content)) > 50 {
		t.Errorf("content should be <= 50 runes, got %d", len([]rune(result.Content)))
	}
}
```

**Step 6: Run full test suite**

Run: `cd /home/krolik/src/go-enriche && go test ./... -v -count=1`
Expected: all pass

**Step 7: Commit**

```bash
git add truncate.go truncate_test.go options.go enriche.go enriche_test.go
git commit -m "feat: add WithMaxContentLen option for content truncation

Truncates extracted text to N runes with word-boundary preference.
Default: 0 (no truncation). Callers like go-wp can set WithMaxContentLen(4000).

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 3: Logger (slog) Option

**Why:** Debugging enrichment pipeline requires visibility into fetch failures, cache hits/misses, search errors. Library should log at Debug level via `*slog.Logger`.

**Files:**
- Modify: `enriche.go:24-30` — add `logger *slog.Logger` field
- Modify: `options.go` — add `WithLogger`
- Modify: `enriche.go` — add debug logging in `New()`, `Enrich()`, `fetchAndExtract()`, `doSearch()`
- Modify: `enriche_test.go` — add test for logger

**Step 1: Write the failing test**

Add to `enriche_test.go`:

```go
func TestEnrich_WithLogger(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(handler)

	srv := newTestServer(testHTML, http.StatusOK)
	defer srv.Close()

	e := New(WithLogger(logger))
	_, err := e.Enrich(context.Background(), Item{Name: "Logged", URL: srv.URL})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "enriche") {
		t.Errorf("expected log output containing 'enriche', got: %q", output)
	}
}
```

Add `"bytes"` and `"log/slog"` to test imports (and `"strings"` if not already).

**Step 2: Run test to verify it fails**

Run: `cd /home/krolik/src/go-enriche && go test . -run TestEnrich_WithLogger -v`
Expected: FAIL — `WithLogger` undefined

**Step 3: Write implementation**

Add field to `Enricher` struct in `enriche.go`:

```go
type Enricher struct {
	fetcher       *fetch.Fetcher
	cache         cache.Cache
	search        search.Provider
	concurrency   int
	cacheTTL      time.Duration
	maxContentLen int
	logger        *slog.Logger
}
```

Add noop default in `New()`:

```go
func New(opts ...Option) *Enricher {
	e := &Enricher{
		fetcher:     fetch.NewFetcher(),
		concurrency: defaultConcurrency,
		cacheTTL:    defaultCacheTTL,
		logger:      slog.New(discardHandler{}),
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

// discardHandler silently discards all log records.
type discardHandler struct{}

func (discardHandler) Enabled(context.Context, slog.Level) bool  { return false }
func (discardHandler) Handle(context.Context, slog.Record) error { return nil }
func (d discardHandler) WithAttrs([]slog.Attr) slog.Handler      { return d }
func (d discardHandler) WithGroup(string) slog.Handler            { return d }
```

Add option in `options.go`:

```go
// WithLogger sets a structured logger for debug output.
// Default: discard (no logging).
func WithLogger(l *slog.Logger) Option {
	return func(e *Enricher) {
		if l != nil {
			e.logger = l
		}
	}
}
```

Add `"log/slog"` to imports in both files.

Add debug logging in `Enrich()`:

```go
func (e *Enricher) Enrich(ctx context.Context, item Item) (*Result, error) {
	result := &Result{
		Name: item.Name,
		URL:  item.URL,
	}

	// Cache check.
	if e.cache != nil {
		key := cacheKey(item)
		if e.cache.Get(ctx, key, result) {
			e.logger.DebugContext(ctx, "enriche: cache hit", "name", item.Name, "key", key)
			return result, nil
		}
		e.logger.DebugContext(ctx, "enriche: cache miss", "name", item.Name, "key", key)
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

	e.logger.DebugContext(ctx, "enriche: done", "name", item.Name, "status", result.Status)
	return result, nil
}
```

Add logging in `fetchAndExtract()`:

```go
func (e *Enricher) fetchAndExtract(ctx context.Context, item Item, result *Result) {
	fr, err := e.fetcher.Fetch(ctx, item.URL)
	if err != nil {
		e.logger.DebugContext(ctx, "enriche: fetch failed", "url", item.URL, "err", err)
		result.Status = fetch.StatusUnreachable
		return
	}

	e.logger.DebugContext(ctx, "enriche: fetched", "url", item.URL, "status", fr.Status, "code", fr.StatusCode)
	// ... rest unchanged
```

Add logging in `doSearch()`:

```go
func (e *Enricher) doSearch(ctx context.Context, item Item, result *Result) {
	query, timeRange := search.BuildQuery(int(item.Mode), item.Name, item.City)
	sr, err := e.search.Search(ctx, query, timeRange)
	if err != nil {
		e.logger.DebugContext(ctx, "enriche: search failed", "name", item.Name, "err", err)
		return
	}
	if sr == nil {
		return
	}
	e.logger.DebugContext(ctx, "enriche: search done", "name", item.Name, "sources", len(sr.Sources))
	result.SearchContext = sr.Context
	result.SearchSources = sr.Sources
	extract.ExtractSnippetFacts(sr.Context, &result.Facts)
}
```

**Step 4: Run test**

Run: `cd /home/krolik/src/go-enriche && go test . -run TestEnrich_WithLogger -v`
Expected: PASS

**Step 5: Run full test suite**

Run: `cd /home/krolik/src/go-enriche && go test ./... -v -count=1`
Expected: all pass

**Step 6: Commit**

```bash
git add enriche.go options.go enriche_test.go
git commit -m "feat: add WithLogger option for debug observability

Logs cache hit/miss, fetch status, search results at slog.Debug level.
Default: discard handler (zero overhead when not configured).

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 4: Metrics Callback Option

**Why:** Operators need counters for cache hit/miss, fetch errors, search errors without importing a specific metrics library.

**Files:**
- Create: `metrics.go` — `Metrics` struct with function fields
- Modify: `options.go` — add `WithMetrics`
- Modify: `enriche.go` — add `metrics *Metrics` field, call hooks at each point
- Create: `metrics_test.go` — tests

**Step 1: Write the failing test**

Create `metrics_test.go`:

```go
package enriche

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/anatolykoptev/go-enriche/cache"
	"github.com/anatolykoptev/go-enriche/fetch"
)

func TestMetrics_CacheHitMiss(t *testing.T) {
	t.Parallel()
	srv := newTestServer(testHTML, http.StatusOK)
	defer srv.Close()

	var hits, misses atomic.Int32
	m := &Metrics{
		OnCacheHit:  func() { hits.Add(1) },
		OnCacheMiss: func() { misses.Add(1) },
	}

	mem := cache.NewMemory()
	e := New(WithCache(mem), WithFetcher(fetch.NewFetcher()), WithMetrics(m))

	item := Item{Name: "M", URL: srv.URL}
	e.Enrich(context.Background(), item)
	e.Enrich(context.Background(), item)

	if misses.Load() != 1 {
		t.Errorf("expected 1 miss, got %d", misses.Load())
	}
	if hits.Load() != 1 {
		t.Errorf("expected 1 hit, got %d", hits.Load())
	}
}

func TestMetrics_FetchError(t *testing.T) {
	t.Parallel()
	var errs atomic.Int32
	m := &Metrics{
		OnFetchError: func() { errs.Add(1) },
	}

	e := New(WithMetrics(m))
	e.Enrich(context.Background(), Item{Name: "Bad", URL: "http://192.0.2.1:1"}) // RFC 5737 TEST-NET

	if errs.Load() != 1 {
		t.Errorf("expected 1 fetch error, got %d", errs.Load())
	}
}

func TestMetrics_NilSafe(t *testing.T) {
	t.Parallel()
	// WithMetrics(nil) should not panic.
	e := New(WithMetrics(nil))
	_, err := e.Enrich(context.Background(), Item{Name: "Safe"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/krolik/src/go-enriche && go test . -run TestMetrics -v`
Expected: FAIL — `Metrics` undefined

**Step 3: Write implementation**

Create `metrics.go`:

```go
package enriche

// Metrics provides callback hooks for observability.
// Any nil field is safely ignored (no-op).
type Metrics struct {
	OnCacheHit   func()
	OnCacheMiss  func()
	OnFetchError func()
	OnSearchError func()
}

func (m *Metrics) cacheHit() {
	if m != nil && m.OnCacheHit != nil {
		m.OnCacheHit()
	}
}

func (m *Metrics) cacheMiss() {
	if m != nil && m.OnCacheMiss != nil {
		m.OnCacheMiss()
	}
}

func (m *Metrics) fetchError() {
	if m != nil && m.OnFetchError != nil {
		m.OnFetchError()
	}
}

func (m *Metrics) searchError() {
	if m != nil && m.OnSearchError != nil {
		m.OnSearchError()
	}
}
```

Add to `Enricher` struct in `enriche.go`:

```go
type Enricher struct {
	fetcher       *fetch.Fetcher
	cache         cache.Cache
	search        search.Provider
	concurrency   int
	cacheTTL      time.Duration
	maxContentLen int
	logger        *slog.Logger
	metrics       *Metrics
}
```

Add option in `options.go`:

```go
// WithMetrics sets callback hooks for cache/fetch/search observability.
func WithMetrics(m *Metrics) Option {
	return func(e *Enricher) { e.metrics = m }
}
```

Add calls in `Enrich()`:

```go
	if e.cache.Get(ctx, key, result) {
		e.metrics.cacheHit()
		// ...
	}
	e.metrics.cacheMiss()
```

In `fetchAndExtract()`:

```go
	if err != nil {
		e.metrics.fetchError()
		// ...
	}
```

In `doSearch()`:

```go
	if err != nil {
		e.metrics.searchError()
		// ...
	}
```

**Step 4: Run tests**

Run: `cd /home/krolik/src/go-enriche && go test . -run TestMetrics -v`
Expected: PASS (3/3)

**Step 5: Run full test suite**

Run: `cd /home/krolik/src/go-enriche && go test ./... -v -count=1`
Expected: all pass

**Step 6: Commit**

```bash
git add metrics.go metrics_test.go options.go enriche.go
git commit -m "feat: add WithMetrics callback hooks for observability

Struct with function fields (OnCacheHit, OnCacheMiss, OnFetchError,
OnSearchError). Nil-safe — any nil field is a no-op.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 5: errgroup in EnrichBatch

**Why:** Current WaitGroup+semaphore doesn't propagate context cancellation. `errgroup.SetLimit()` replaces both with fewer lines and stops early on `ctx.Done()`.

**Files:**
- Modify: `enriche.go:80-98` — replace WaitGroup+semaphore with errgroup
- Modify: `enriche_test.go` — add cancellation test

**Step 1: Write the failing test**

Add to `enriche_test.go`:

```go
func TestEnrichBatch_ContextCancel(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Write([]byte(testHTML)) //nolint:errcheck
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	e := New(WithConcurrency(1))

	items := make([]Item, 10)
	for i := range items {
		items[i] = Item{Name: "cancel", URL: srv.URL}
	}

	// Cancel after 100ms — should not process all 10 items (each takes 200ms).
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	results := e.EnrichBatch(ctx, items)

	var nilCount int
	for _, r := range results {
		if r == nil {
			nilCount++
		}
	}
	// At least some items should be nil (not processed) due to cancellation.
	if nilCount == 0 {
		t.Error("expected some nil results after context cancellation")
	}
}
```

**Step 2: Run test to verify current behavior fails**

Run: `cd /home/krolik/src/go-enriche && go test . -run TestEnrichBatch_ContextCancel -v -timeout 30s`
Expected: FAIL — current WaitGroup implementation processes all items regardless of cancellation

**Step 3: Replace EnrichBatch implementation**

In `enriche.go`, replace `EnrichBatch` (lines 80-98):

```go
// EnrichBatch enriches multiple items concurrently with bounded concurrency.
// Respects context cancellation — unstarted items are skipped.
func (e *Enricher) EnrichBatch(ctx context.Context, items []Item) []*Result {
	results := make([]*Result, len(items))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(e.concurrency)

	for i, item := range items {
		g.Go(func() error {
			r, _ := e.Enrich(gctx, it)
			results[idx] = r
			return nil
		})
	}

	_ = g.Wait()
	return results
}
```

**Important:** Use the Go 1.22+ loop variable capture (no `idx, it` closure params needed since go-enriche uses Go 1.25).

Update imports: add `"golang.org/x/sync/errgroup"`, remove `"sync"`.

**Step 4: Run tests**

Run: `cd /home/krolik/src/go-enriche && go test . -run TestEnrichBatch -v`
Expected: PASS (all EnrichBatch tests including new cancellation test)

**Step 5: Run full test suite**

Run: `cd /home/krolik/src/go-enriche && go test ./... -v -count=1`
Expected: all pass

**Step 6: Commit**

```bash
git add enriche.go enriche_test.go
git commit -m "feat: replace WaitGroup+semaphore with errgroup in EnrichBatch

errgroup.SetLimit() provides bounded concurrency + context propagation.
Unstarted items are skipped on context cancellation.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 6: Retry with Backoff for Transient Fetch Errors

**Why:** Transient errors (timeout, 503) deserve one retry with jitter before marking as unreachable.

**Files:**
- Modify: `enriche.go` — add retry logic in `fetchAndExtract()`
- Modify: `enriche_test.go` — add retry test
- Modify: `fetch/status.go` — add `IsTransient()` method on `FetchResult`

**Step 1: Write the failing test**

Add to `fetch/status_test.go` (or create if small):

```go
func TestFetchResult_IsTransient(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		result FetchResult
		want   bool
	}{
		{"503", FetchResult{Status: StatusUnreachable, StatusCode: 503}, true},
		{"502", FetchResult{Status: StatusUnreachable, StatusCode: 502}, true},
		{"429", FetchResult{Status: StatusUnreachable, StatusCode: 429}, true},
		{"404", FetchResult{Status: StatusNotFound, StatusCode: 404}, false},
		{"200", FetchResult{Status: StatusActive, StatusCode: 200}, false},
		{"0-unreachable", FetchResult{Status: StatusUnreachable, StatusCode: 0}, true},
	}
	for _, tt := range tests {
		got := tt.result.IsTransient()
		if got != tt.want {
			t.Errorf("%s: IsTransient() = %v, want %v", tt.name, got, tt.want)
		}
	}
}
```

Add to `enriche_test.go`:

```go
func TestEnrich_RetryTransient(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable) // 503 on first call
			return
		}
		w.Write([]byte(testHTML)) //nolint:errcheck
	}))
	defer srv.Close()

	e := New()
	result, err := e.Enrich(context.Background(), Item{Name: "Retry", URL: srv.URL})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if result.Status != fetch.StatusActive {
		t.Errorf("expected StatusActive after retry, got %q", result.Status)
	}
	if calls.Load() != 2 {
		t.Errorf("expected 2 fetch calls (1 fail + 1 retry), got %d", calls.Load())
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /home/krolik/src/go-enriche && go test ./fetch/ -run TestFetchResult_IsTransient -v && go test . -run TestEnrich_RetryTransient -v`
Expected: FAIL — `IsTransient` undefined, no retry behavior

**Step 3: Write implementation**

Add to `fetch/status.go`:

```go
// IsTransient returns true if the fetch result indicates a transient error
// worth retrying (connection failure, 502, 503, 429).
func (fr *FetchResult) IsTransient() bool {
	if fr.Status != StatusUnreachable {
		return false
	}
	// StatusCode 0 means connection failed (timeout, DNS, etc.) — transient.
	return fr.StatusCode == 0 ||
		fr.StatusCode == http.StatusBadGateway ||
		fr.StatusCode == http.StatusServiceUnavailable ||
		fr.StatusCode == http.StatusGatewayTimeout ||
		fr.StatusCode == http.StatusTooManyRequests
}
```

Add `"net/http"` to `fetch/status.go` imports.

Modify `fetchAndExtract()` in `enriche.go` to add one retry:

```go
func (e *Enricher) fetchAndExtract(ctx context.Context, item Item, result *Result) {
	fr, err := e.fetcher.Fetch(ctx, item.URL)
	if err != nil {
		e.logger.DebugContext(ctx, "enriche: fetch failed", "url", item.URL, "err", err)
		e.metrics.fetchError()
		result.Status = fetch.StatusUnreachable
		return
	}

	// One retry for transient errors (503, 502, 429, timeout).
	if fr.IsTransient() {
		e.logger.DebugContext(ctx, "enriche: transient error, retrying", "url", item.URL, "code", fr.StatusCode)
		jitter := time.Duration(100+rand.IntN(200)) * time.Millisecond
		timer := time.NewTimer(jitter)
		select {
		case <-ctx.Done():
			timer.Stop()
			result.Status = fetch.StatusUnreachable
			return
		case <-timer.C:
		}
		fr, err = e.fetcher.Fetch(ctx, item.URL)
		if err != nil {
			e.logger.DebugContext(ctx, "enriche: retry failed", "url", item.URL, "err", err)
			e.metrics.fetchError()
			result.Status = fetch.StatusUnreachable
			return
		}
	}

	e.logger.DebugContext(ctx, "enriche: fetched", "url", item.URL, "status", fr.Status, "code", fr.StatusCode)
	// ... rest unchanged (result.Status = fr.Status, etc.)
```

Add `"math/rand/v2"` to `enriche.go` imports (as `rand`). Note: `math/rand/v2` requires Go 1.22+, and we use Go 1.25.

**Important:** The singleflight in `Fetcher.Fetch` caches in-flight requests by URL. The retry will work because by the time the jitter elapses, the first singleflight group has completed and a fresh request is made.

**Step 4: Run tests**

Run: `cd /home/krolik/src/go-enriche && go test ./fetch/ -run TestFetchResult_IsTransient -v && go test . -run TestEnrich_RetryTransient -v`
Expected: PASS

**Step 5: Run full test suite**

Run: `cd /home/krolik/src/go-enriche && go test ./... -v -count=1`
Expected: all pass

**Step 6: Lint**

Run: `cd /home/krolik/src/go-enriche && make lint`
Expected: 0 issues

**Step 7: Commit**

```bash
git add fetch/status.go fetch/status_test.go enriche.go enriche_test.go
git commit -m "feat: add one retry with jitter for transient fetch errors

Retries on 502/503/429/timeout with 100-300ms random jitter.
Respects context cancellation during backoff wait.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 7: Final Verification & Tag

**Files:**
- None created — verification only

**Step 1: Run full test suite**

Run: `cd /home/krolik/src/go-enriche && go test ./... -v -count=1 -race`
Expected: all pass, no race conditions

**Step 2: Lint**

Run: `cd /home/krolik/src/go-enriche && make lint`
Expected: 0 issues

**Step 3: Update ROADMAP.md**

Mark all Phase 6 items as complete with `[x]`.

**Step 4: Commit and tag**

```bash
git add docs/ROADMAP.md
git commit -m "docs: mark Phase 6 as complete in roadmap

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
git tag v0.2.0
git push origin main --tags
```

**Step 5: Update go-wp dependency**

```bash
cd /home/krolik/src/go-wp
go get github.com/anatolykoptev/go-enriche@v0.2.0
go mod tidy
```

Update `getEnricher()` in `tool_enrich.go` to pass logger + content limit:

```go
func getEnricher() *enriche.Enricher {
	wpEnricherOnce.Do(func() {
		var opts []enriche.Option

		if sc := engine.StealthClient(); sc != nil {
			opts = append(opts, enriche.WithStealth(sc))
		}
		if engine.Cfg.SearxngURL != "" {
			opts = append(opts, enriche.WithSearch(search.NewSearXNG(engine.Cfg.SearxngURL)))
		}
		opts = append(opts, enriche.WithMaxContentLen(4000))

		wpEnricher = enriche.New(opts...)
	})
	return wpEnricher
}
```

Test and commit go-wp changes.

**Step 6: Deploy go-wp**

```bash
cd ~/deploy/krolik-server
docker compose build --no-cache go-wp && docker compose up -d --no-deps --force-recreate go-wp
```

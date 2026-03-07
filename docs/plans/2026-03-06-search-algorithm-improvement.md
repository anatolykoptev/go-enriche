# Search Algorithm Improvement Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace sequential Fallback search with parallel multi-provider search, fix dead SearXNG default, increase result count, and add ox-browser search fallback.

**Architecture:** `search.Parallel` runs all configured providers concurrently via `errgroup`, merges+deduplicates results from all successful providers (instead of stopping at first success). ox-browser becomes a search provider by fetching DuckDuckGo HTML through its CF-bypass `/fetch` endpoint. go-wp's dead `SEARXNG_URL` default is removed.

**Tech Stack:** Go, `golang.org/x/sync/errgroup`, go-enriche `search` package, go-stealth, ox-browser REST API

---

### Task 1: Fix dead SearXNG default in go-wp

The `SEARXNG_URL` env var defaults to `http://127.0.0.1:8888` but SearXNG was removed. Inside Docker, this is a dead address that causes timeouts in search Fallback chain, YandexMaps checker, and Geocoder.

**Files:**
- Modify: `/home/krolik/src/go-wp/deps_build.go:78`

**Step 1: Fix the default**

Change line 78 from:
```go
SearxngURL:       env.Str("SEARXNG_URL", "http://127.0.0.1:8888"),
```
to:
```go
SearxngURL:       env.Str("SEARXNG_URL", ""),
```

**Step 2: Verify build**

Run: `cd /home/krolik/src/go-wp && go build ./...`
Expected: SUCCESS

**Step 3: Commit**

```bash
cd /home/krolik/src/go-wp
git add deps_build.go
git commit -m "fix: remove dead SearXNG default URL

SearXNG service was removed. The default http://127.0.0.1:8888
caused timeouts in search chain, YandexMaps, and Geocoder."
```

---

### Task 2: Create `search.Parallel` provider in go-enriche

Replace sequential `Fallback` (tries providers one-by-one, returns first success) with `Parallel` (runs all providers concurrently, merges all results).

**Files:**
- Create: `/home/krolik/src/go-enriche/search/parallel.go`
- Create: `/home/krolik/src/go-enriche/search/parallel_test.go`

**Step 1: Write the failing tests**

File: `/home/krolik/src/go-enriche/search/parallel_test.go`

```go
package search

import (
	"context"
	"errors"
	"testing"
	"time"
)

// slowProvider simulates a provider that takes some time.
type slowProvider struct {
	delay  time.Duration
	result *SearchResult
	err    error
}

func (s *slowProvider) Search(ctx context.Context, _ string, _ string) (*SearchResult, error) {
	select {
	case <-time.After(s.delay):
		return s.result, s.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestParallel_MergesResults(t *testing.T) {
	t.Parallel()
	p := NewParallel(
		&okProvider{"ddg"},
		&okProvider{"startpage"},
	)
	result, err := p.Search(context.Background(), "q", "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	// Should have sources from both providers.
	if len(result.Sources) != 2 {
		t.Errorf("expected 2 sources, got %d: %v", len(result.Sources), result.Sources)
	}
	if result.Context == "" {
		t.Error("expected non-empty context")
	}
}

func TestParallel_PartialFailure(t *testing.T) {
	t.Parallel()
	p := NewParallel(
		&failProvider{errors.New("ddg down")},
		&okProvider{"startpage"},
	)
	result, err := p.Search(context.Background(), "q", "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(result.Sources) != 1 {
		t.Errorf("expected 1 source, got %d", len(result.Sources))
	}
	if result.Context != "startpage" {
		t.Errorf("expected 'startpage', got %q", result.Context)
	}
}

func TestParallel_AllFail(t *testing.T) {
	t.Parallel()
	p := NewParallel(
		&failProvider{errors.New("first down")},
		&failProvider{errors.New("second down")},
	)
	_, err := p.Search(context.Background(), "q", "")
	if err == nil {
		t.Error("expected error when all providers fail")
	}
}

func TestParallel_SingleProvider(t *testing.T) {
	t.Parallel()
	p := NewParallel(&okProvider{"only"})
	result, err := p.Search(context.Background(), "q", "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.Context != "only" {
		t.Errorf("expected 'only', got %q", result.Context)
	}
}

func TestParallel_DeduplicatesSources(t *testing.T) {
	t.Parallel()
	// Both providers return the same URL.
	same := &okProvider{"same"}
	p := NewParallel(same, same)
	result, err := p.Search(context.Background(), "q", "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(result.Sources) != 1 {
		t.Errorf("expected 1 deduplicated source, got %d: %v", len(result.Sources), result.Sources)
	}
}

func TestParallel_Empty(t *testing.T) {
	t.Parallel()
	p := NewParallel()
	_, err := p.Search(context.Background(), "q", "")
	if err == nil {
		t.Error("expected error with no providers")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /home/krolik/src/go-enriche && go test ./search/ -run TestParallel -v`
Expected: FAIL (NewParallel not defined)

**Step 3: Implement Parallel provider**

File: `/home/krolik/src/go-enriche/search/parallel.go`

```go
package search

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// Parallel runs all providers concurrently and merges results.
// Unlike Fallback (first success wins), Parallel collects results from
// ALL successful providers, deduplicates URLs, and merges context.
type Parallel struct {
	providers []Provider
}

// NewParallel creates a parallel provider that queries all providers concurrently.
func NewParallel(providers ...Provider) *Parallel {
	return &Parallel{providers: providers}
}

// providerResult holds one provider's output.
type providerResult struct {
	result *SearchResult
	err    error
}

// Search queries all providers in parallel, merges successful results.
// Returns error only if ALL providers fail.
func (p *Parallel) Search(ctx context.Context, query string, timeRange string) (*SearchResult, error) {
	if len(p.providers) == 0 {
		return nil, errors.New("parallel: no providers configured")
	}

	results := make([]providerResult, len(p.providers))
	var wg sync.WaitGroup

	for i, prov := range p.providers {
		wg.Add(1)
		go func(idx int, pr Provider) {
			defer wg.Done()
			r, err := pr.Search(ctx, query, timeRange)
			results[idx] = providerResult{result: r, err: err}
		}(i, prov)
	}
	wg.Wait()

	return p.merge(results)
}

// merge combines all successful results, deduplicating sources.
func (p *Parallel) merge(results []providerResult) (*SearchResult, error) {
	var (
		contextParts []string
		sources      []string
		seen         = make(map[string]bool)
		errs         []error
	)

	for _, pr := range results {
		if pr.err != nil {
			errs = append(errs, pr.err)
			continue
		}
		if pr.result == nil {
			continue
		}
		if pr.result.Context != "" {
			contextParts = append(contextParts, pr.result.Context)
		}
		for _, src := range pr.result.Sources {
			norm := normalizeURL(src)
			if norm != "" && !seen[norm] {
				seen[norm] = true
				sources = append(sources, src)
			}
		}
	}

	if len(sources) == 0 && len(contextParts) == 0 {
		return nil, fmt.Errorf("all providers failed: %w", errors.Join(errs...))
	}

	return &SearchResult{
		Context: strings.Join(contextParts, "\n\n"),
		Sources: sources,
	}, nil
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /home/krolik/src/go-enriche && go test ./search/ -run TestParallel -v`
Expected: ALL PASS

**Step 5: Run full test suite**

Run: `cd /home/krolik/src/go-enriche && go test ./... -count=1`
Expected: ALL PASS

**Step 6: Commit**

```bash
cd /home/krolik/src/go-enriche
git add search/parallel.go search/parallel_test.go
git commit -m "feat(search): add Parallel provider

Runs all providers concurrently and merges results from all
successful ones. Unlike Fallback which returns first success,
Parallel gives more results and is faster (max latency = slowest
provider, not sum of all)."
```

---

### Task 3: Increase maxResults for DDG and Startpage

`defaultMaxResults` is 3 — too few for `fetchSearchSources` which wants 5+ URLs. Increase to 8.

**Files:**
- Modify: `/home/krolik/src/go-enriche/search/searxng.go:14` (the `defaultMaxResults` const)

**Step 1: Change the constant**

In `/home/krolik/src/go-enriche/search/searxng.go`, change line 14:
```go
defaultMaxResults = 3
```
to:
```go
defaultMaxResults = 8
```

**Step 2: Move the constant to search.go**

The constant is in `searxng.go` (deprecated file) but is used by all providers. Move it to `search.go` which is the package root.

In `/home/krolik/src/go-enriche/search/search.go`, add after the import block:
```go
const defaultMaxResults = 8
```

Remove the constant from `/home/krolik/src/go-enriche/search/searxng.go:14`.

**Step 3: Verify build and tests**

Run: `cd /home/krolik/src/go-enriche && go test ./... -count=1`
Expected: ALL PASS

**Step 4: Commit**

```bash
cd /home/krolik/src/go-enriche
git add search/search.go search/searxng.go
git commit -m "feat(search): increase defaultMaxResults from 3 to 8

Moved constant from deprecated searxng.go to search.go.
More results needed for fetchSearchSources (fetches top 5 URLs)."
```

---

### Task 4: Add ox-browser as search provider

ox-browser can fetch any URL with Cloudflare bypass. Use it to fetch DuckDuckGo HTML search and parse results — a fallback when direct DDG/Startpage scrapers fail.

**Files:**
- Create: `/home/krolik/src/go-enriche/search/oxbrowser.go`
- Create: `/home/krolik/src/go-enriche/search/oxbrowser_test.go`

**Step 1: Write the failing tests**

File: `/home/krolik/src/go-enriche/search/oxbrowser_test.go`

```go
package search

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOxBrowser_Search(t *testing.T) {
	t.Parallel()

	// Mock ox-browser /fetch endpoint that returns DDG-like HTML.
	ddgHTML := `<html><body>
		<div class="result results_links results_links_deep web-result">
			<a class="result__a" href="https://example.com/page1">Example Page</a>
			<a class="result__snippet">This is the first result snippet</a>
		</div>
		<div class="result results_links results_links_deep web-result">
			<a class="result__a" href="https://example.com/page2">Second Page</a>
			<a class="result__snippet">This is the second result snippet</a>
		</div>
	</body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// ox-browser /fetch returns JSON with content field.
		resp := `{"content":` + mustJSON(ddgHTML) + `,"status_code":200}`
		w.Write([]byte(resp))
	}))
	defer srv.Close()

	ox := NewOxBrowser(srv.URL, WithOxBrowserMaxResults(5))
	result, err := ox.Search(context.Background(), "test query", "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(result.Sources) == 0 {
		t.Error("expected at least one source")
	}
}

func TestOxBrowser_FetchError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ox := NewOxBrowser(srv.URL)
	_, err := ox.Search(context.Background(), "test", "")
	if err == nil {
		t.Error("expected error on 500 response")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /home/krolik/src/go-enriche && go test ./search/ -run TestOxBrowser -v`
Expected: FAIL (NewOxBrowser not defined)

**Step 3: Implement OxBrowser search provider**

File: `/home/krolik/src/go-enriche/search/oxbrowser.go`

```go
package search

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/anatolykoptev/go-stealth/websearch"
)

const oxBrowserSearchTimeout = 30 * time.Second

// OxBrowser implements Provider by fetching DuckDuckGo HTML through
// ox-browser's /fetch endpoint (which handles Cloudflare bypass).
// Use as a fallback when direct DDG/Startpage scrapers fail.
type OxBrowser struct {
	baseURL    string
	client     *http.Client
	maxResults int
}

// OxBrowserOption configures OxBrowser.
type OxBrowserOption func(*OxBrowser)

// WithOxBrowserMaxResults sets the max results to aggregate.
func WithOxBrowserMaxResults(n int) OxBrowserOption {
	return func(o *OxBrowser) { o.maxResults = n }
}

// NewOxBrowser creates an ox-browser search provider.
// baseURL is the ox-browser service URL (e.g. "http://ox-browser:8901").
func NewOxBrowser(baseURL string, opts ...OxBrowserOption) *OxBrowser {
	o := &OxBrowser{
		baseURL:    strings.TrimRight(baseURL, "/"),
		client:     &http.Client{Timeout: oxBrowserSearchTimeout},
		maxResults: defaultMaxResults,
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// oxFetchResponse is the JSON response from ox-browser /fetch.
type oxFetchResponse struct {
	Content    string `json:"content"`
	StatusCode int    `json:"status_code"`
	Error      string `json:"error,omitempty"`
}

// Search fetches DuckDuckGo HTML via ox-browser and parses results.
func (o *OxBrowser) Search(ctx context.Context, query string, _ string) (*SearchResult, error) {
	ddgURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)

	body, err := json.Marshal(map[string]string{"url": ddgURL})
	if err != nil {
		return nil, fmt.Errorf("oxbrowser search: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		o.baseURL+"/fetch", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("oxbrowser search: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oxbrowser search: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oxbrowser search: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("oxbrowser search: read body: %w", err)
	}

	var fr oxFetchResponse
	if err := json.Unmarshal(data, &fr); err != nil {
		return nil, fmt.Errorf("oxbrowser search: parse JSON: %w", err)
	}
	if fr.Error != "" {
		return nil, fmt.Errorf("oxbrowser search: %s", fr.Error)
	}

	// Parse DDG HTML using go-stealth websearch parser.
	wsResults, err := websearch.ParseDDGHTML([]byte(fr.Content))
	if err != nil {
		return nil, fmt.Errorf("oxbrowser search: parse DDG HTML: %w", err)
	}

	return aggregateResults(toSearchResults(wsResults), o.maxResults), nil
}

// mustJSON is a test helper — marshals string to JSON string literal.
func mustJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
```

**Step 4: Run tests**

Run: `cd /home/krolik/src/go-enriche && go test ./search/ -run TestOxBrowser -v`
Expected: PASS (or adjust DDG HTML fixture if parser expects different structure)

**Step 5: Run full test suite**

Run: `cd /home/krolik/src/go-enriche && go test ./... -count=1`
Expected: ALL PASS

**Step 6: Commit**

```bash
cd /home/krolik/src/go-enriche
git add search/oxbrowser.go search/oxbrowser_test.go
git commit -m "feat(search): add OxBrowser provider

Fetches DuckDuckGo HTML through ox-browser /fetch endpoint
(Cloudflare bypass). Parses results with go-stealth websearch.
Use as fallback when direct DDG/Startpage scrapers fail."
```

---

### Task 5: Wire Parallel + OxBrowser in go-wp enricher

Replace `NewFallback` with `NewParallel` and add ox-browser as a search provider.

**Files:**
- Modify: `/home/krolik/src/go-wp/internal/wptools/content/enrich.go:89-147` (`getEnricher` function)

**Step 1: Update go-enriche dependency in go-wp**

```bash
cd /home/krolik/src/go-wp
go get github.com/anatolykoptev/go-enriche@latest
go mod tidy
```

**Step 2: Replace Fallback with Parallel and add ox-browser provider**

In `getEnricher()` function, change lines 97-117 from:

```go
// Build search provider chain: DDG -> Startpage -> SearXNG fallback.
var searchProviders []search.Provider

// Direct scrapers need a proxy (data center IPs are blocked).
// Try Webshare (paid) -> fallback to Tor (free).
if proxyPool != nil {
    if ddg, err := search.NewDDG("", search.WithDDGProxyPool(proxyPool)); err == nil {
        searchProviders = append(searchProviders, ddg)
    }
    if sp, err := search.NewStartpage("", search.WithStartpageProxyPool(proxyPool)); err == nil {
        searchProviders = append(searchProviders, sp)
    }
}

if searxngURL != "" {
    searchProviders = append(searchProviders, search.NewSearXNG(searxngURL))
}

if len(searchProviders) > 0 {
    opts = append(opts, enriche.WithSearch(search.NewFallback(searchProviders...)))
}
```

to:

```go
// Build search provider chain: DDG + Startpage + OxBrowser (parallel).
var searchProviders []search.Provider

// Direct scrapers need a proxy (data center IPs are blocked).
if proxyPool != nil {
    if ddg, err := search.NewDDG("", search.WithDDGProxyPool(proxyPool)); err == nil {
        searchProviders = append(searchProviders, ddg)
    }
    if sp, err := search.NewStartpage("", search.WithStartpageProxyPool(proxyPool)); err == nil {
        searchProviders = append(searchProviders, sp)
    }
}

// ox-browser as search fallback (fetches DDG HTML with CF bypass).
if oxBrowserURL != "" {
    searchProviders = append(searchProviders, search.NewOxBrowser(oxBrowserURL))
}

if len(searchProviders) > 0 {
    opts = append(opts, enriche.WithSearch(search.NewParallel(searchProviders...)))
}
```

**Step 3: Verify build**

Run: `cd /home/krolik/src/go-wp && go build ./...`
Expected: SUCCESS

**Step 4: Commit**

```bash
cd /home/krolik/src/go-wp
git add internal/wptools/content/enrich.go go.mod go.sum
go mod vendor
git add vendor/
git commit -m "feat(enrich): parallel search + ox-browser fallback

- Replace sequential Fallback with Parallel provider
- Add ox-browser as search provider (DDG via CF bypass)
- Remove dead SearXNG from search chain
- All providers run concurrently, results merged"
```

---

### Task 6: Lint, test, deploy

**Step 1: Lint go-enriche**

Run: `cd /home/krolik/src/go-enriche && golangci-lint run ./...`
Expected: 0 issues (fix any that appear)

**Step 2: Lint go-wp**

Run: `cd /home/krolik/src/go-wp && golangci-lint run ./...`
Expected: 0 issues

**Step 3: Tag go-enriche release**

```bash
cd /home/krolik/src/go-enriche
git tag v1.3.0
git push origin main --tags
```

**Step 4: Deploy go-wp**

```bash
cd ~/deploy/krolik-server
docker compose build --no-cache go-wp && docker compose up -d --no-deps --force-recreate go-wp
```

**Step 5: Smoke test — enrich a place with no URL**

Use `wp_enrich` tool with:
```json
{"mode": "places", "data": "[{\"name\": \"Маньпупунёр\"}]"}
```

Expected: non-empty `content` and `search_sources` in response (previously returned empty).

---

## Dependency Graph

```
Task 1 (fix SearXNG default)     — independent
Task 2 (Parallel provider)       — independent
Task 3 (increase maxResults)     — independent
Task 4 (OxBrowser provider)      — depends on Task 3 (uses defaultMaxResults)
Task 5 (wire in go-wp)           — depends on Tasks 1, 2, 3, 4
Task 6 (lint, deploy)            — depends on Task 5
```

Parallelizable: Tasks 1, 2, 3 can run simultaneously. Task 4 after Task 3. Task 5 after all. Task 6 last.

# Phase 7: Search Providers Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Pluggable search providers beyond SearXNG: Brave Search, Google Custom Search, rate limiting decorator, and multi-provider fallback.

**Architecture:** All providers implement `search.Provider` interface (`Search(ctx, query, timeRange) (*SearchResult, error)`). Rate limiter and fallback are decorators wrapping any Provider. Each provider does minimal HTTP: build URL, parse JSON, aggregate into SearchResult via the shared `aggregate()` helper. `golang.org/x/time/rate` for token bucket rate limiting. No new heavy dependencies.

**Tech Stack:** `net/http`, `encoding/json`, `golang.org/x/time/rate` (new dep), existing `search.Provider` interface

---

### Task 1: Rate-Limited Provider Decorator

**Why:** Search APIs have QPS limits (Brave: 1 QPS free, Google: ~2 QPS practical). Need to throttle requests without changing provider implementations.

**Files:**
- Create: `search/ratelimit.go`
- Create: `search/ratelimit_test.go`

**Step 1: Write the failing test**

Create `search/ratelimit_test.go`:

```go
package search

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type callCounter struct {
	calls atomic.Int32
}

func (c *callCounter) Search(_ context.Context, _ string, _ string) (*SearchResult, error) {
	c.calls.Add(1)
	return &SearchResult{Context: "ok"}, nil
}

func TestRateLimited_Throttles(t *testing.T) {
	t.Parallel()
	counter := &callCounter{}
	limited := NewRateLimited(counter, 2, 1) // 2 req/s, burst 1

	ctx := context.Background()

	// First call — immediate (burst token).
	_, err := limited.Search(ctx, "q1", "")
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}

	// Rapid second call — should block briefly.
	start := time.Now()
	_, err = limited.Search(ctx, "q2", "")
	if err != nil {
		t.Fatalf("second call error: %v", err)
	}
	elapsed := time.Since(start)

	// Should have waited ~500ms (1/2 req/s).
	if elapsed < 300*time.Millisecond {
		t.Errorf("expected throttle delay, got %v", elapsed)
	}

	if counter.calls.Load() != 2 {
		t.Errorf("expected 2 calls, got %d", counter.calls.Load())
	}
}

func TestRateLimited_ContextCancel(t *testing.T) {
	t.Parallel()
	counter := &callCounter{}
	limited := NewRateLimited(counter, 0.1, 1) // very slow: 1 per 10s

	ctx := context.Background()
	// Consume burst token.
	limited.Search(ctx, "q1", "")

	// Cancel context — should error, not block.
	ctx2, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := limited.Search(ctx2, "q2", "")
	if err == nil {
		t.Error("expected context cancellation error")
	}
}

func TestRateLimited_PassesThrough(t *testing.T) {
	t.Parallel()
	inner := &callCounter{}
	limited := NewRateLimited(inner, 100, 10) // generous limit

	result, err := limited.Search(context.Background(), "test", "week")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.Context != "ok" {
		t.Errorf("expected 'ok', got %q", result.Context)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/krolik/src/go-enriche && go test ./search/ -run TestRateLimited -v`
Expected: FAIL — `NewRateLimited` undefined

**Step 3: Write implementation**

Create `search/ratelimit.go`:

```go
package search

import (
	"context"
	"fmt"

	"golang.org/x/time/rate"
)

// RateLimited wraps a Provider with a token bucket rate limiter.
type RateLimited struct {
	inner   Provider
	limiter *rate.Limiter
}

// NewRateLimited creates a rate-limited provider.
// rps: requests per second, burst: max burst size.
func NewRateLimited(p Provider, rps float64, burst int) *RateLimited {
	return &RateLimited{
		inner:   p,
		limiter: rate.NewLimiter(rate.Limit(rps), burst),
	}
}

// Search waits for a rate limit token, then delegates to the inner provider.
func (r *RateLimited) Search(ctx context.Context, query string, timeRange string) (*SearchResult, error) {
	if err := r.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit: %w", err)
	}
	return r.inner.Search(ctx, query, timeRange)
}
```

Run: `go get golang.org/x/time` to add the dependency.

**Step 4: Run test to verify it passes**

Run: `cd /home/krolik/src/go-enriche && go test ./search/ -run TestRateLimited -v`
Expected: PASS (3/3)

**Step 5: Run full test suite + lint**

Run: `cd /home/krolik/src/go-enriche && go test ./... -count=1 && golangci-lint run ./...`
Expected: all pass, 0 issues

**Step 6: Commit**

```bash
git add search/ratelimit.go search/ratelimit_test.go go.mod go.sum
git commit -m "feat: add rate-limited provider decorator

NewRateLimited(provider, rps, burst) wraps any Provider with
golang.org/x/time/rate token bucket. Context-aware blocking.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 2: Brave Search Provider

**Why:** SearXNG requires self-hosting. Brave Search API is a commercial alternative with a free tier (~1000 req/month).

**Files:**
- Create: `search/brave.go`
- Create: `search/brave_test.go`

**Step 1: Write the failing test**

Create `search/brave_test.go`:

```go
package search

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBrave_Search(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth header.
		if r.Header.Get("X-Subscription-Token") != "test-key" {
			t.Errorf("missing auth header")
		}
		if r.URL.Query().Get("q") != "test query" {
			t.Errorf("missing query param, got %q", r.URL.Query().Get("q"))
		}

		resp := braveResponse{
			Web: &braveWebResults{
				Results: []braveResult{
					{URL: "https://example.com/1", Title: "Result 1", Description: "First result content"},
					{URL: "https://example.com/2", Title: "Result 2", Description: "Second result content"},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	brave := NewBrave("test-key", WithBraveBaseURL(srv.URL))
	result, err := brave.Search(context.Background(), "test query", "")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(result.Sources) != 2 {
		t.Errorf("expected 2 sources, got %d", len(result.Sources))
	}
	if result.Context == "" {
		t.Error("expected non-empty context")
	}
}

func TestBrave_Freshness(t *testing.T) {
	t.Parallel()
	var gotFreshness string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotFreshness = r.URL.Query().Get("freshness")
		resp := braveResponse{Web: &braveWebResults{}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	brave := NewBrave("key", WithBraveBaseURL(srv.URL))
	brave.Search(context.Background(), "q", "week")

	if gotFreshness != "pw" {
		t.Errorf("expected freshness 'pw' for week, got %q", gotFreshness)
	}
}

func TestBrave_ErrorStatus(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	brave := NewBrave("key", WithBraveBaseURL(srv.URL))
	_, err := brave.Search(context.Background(), "q", "")
	if err == nil {
		t.Error("expected error on 429")
	}
}

func TestBrave_MaxResults(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		results := make([]braveResult, 10)
		for i := range results {
			results[i] = braveResult{URL: "https://example.com/" + string(rune('a'+i)), Title: "R", Description: "D"}
		}
		resp := braveResponse{Web: &braveWebResults{Results: results}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	brave := NewBrave("key", WithBraveBaseURL(srv.URL), WithBraveMaxResults(3))
	result, _ := brave.Search(context.Background(), "q", "")
	if len(result.Sources) > 3 {
		t.Errorf("expected max 3 sources, got %d", len(result.Sources))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/krolik/src/go-enriche && go test ./search/ -run TestBrave -v`
Expected: FAIL — `NewBrave` undefined

**Step 3: Write implementation**

Create `search/brave.go`:

```go
package search

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

const braveDefaultBaseURL = "https://api.search.brave.com/res/v1/web/search"

// Brave implements Provider using the Brave Search API.
type Brave struct {
	apiKey     string
	baseURL    string
	client     *http.Client
	maxResults int
}

// BraveOption configures Brave.
type BraveOption func(*Brave)

// WithBraveBaseURL overrides the API endpoint (for testing).
func WithBraveBaseURL(u string) BraveOption {
	return func(b *Brave) { b.baseURL = u }
}

// WithBraveHTTPClient sets a custom HTTP client.
func WithBraveHTTPClient(c *http.Client) BraveOption {
	return func(b *Brave) { b.client = c }
}

// WithBraveMaxResults sets the max results to aggregate.
func WithBraveMaxResults(n int) BraveOption {
	return func(b *Brave) { b.maxResults = n }
}

// NewBrave creates a Brave Search provider.
func NewBrave(apiKey string, opts ...BraveOption) *Brave {
	b := &Brave{
		apiKey:     apiKey,
		baseURL:    braveDefaultBaseURL,
		client:     http.DefaultClient,
		maxResults: defaultMaxResults,
	}
	for _, o := range opts {
		o(b)
	}
	return b
}

// braveResponse is the top-level JSON response from Brave Search API.
type braveResponse struct {
	Web *braveWebResults `json:"web"`
}

type braveWebResults struct {
	Results []braveResult `json:"results"`
}

type braveResult struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// timeRangeToBraveFreshness maps enriche time ranges to Brave freshness values.
func timeRangeToBraveFreshness(timeRange string) string {
	switch timeRange {
	case "week":
		return "pw"
	case "month":
		return "pm"
	case "day":
		return "pd"
	case "year":
		return "py"
	default:
		return ""
	}
}

// Search queries Brave Search and returns aggregated context.
func (b *Brave) Search(ctx context.Context, query string, timeRange string) (*SearchResult, error) {
	params := url.Values{}
	params.Set("q", query)
	params.Set("count", "10")
	if freshness := timeRangeToBraveFreshness(timeRange); freshness != "" {
		params.Set("freshness", freshness)
	}

	reqURL := b.baseURL + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("brave: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", b.apiKey)

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("brave: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("brave: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("brave: read body: %w", err)
	}

	var data braveResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("brave: parse JSON: %w", err)
	}

	if data.Web == nil {
		return &SearchResult{}, nil
	}

	return b.aggregate(data.Web.Results), nil
}

func (b *Brave) aggregate(results []braveResult) *SearchResult {
	generic := make([]searxngResult, 0, len(results))
	for _, r := range results {
		generic = append(generic, searxngResult{
			URL:     r.URL,
			Title:   r.Title,
			Content: r.Description,
		})
	}
	// Reuse SearXNG aggregation logic — same dedup + context building.
	s := &SearXNG{maxResults: b.maxResults}
	return s.aggregate(generic)
}
```

**Step 4: Run tests**

Run: `cd /home/krolik/src/go-enriche && go test ./search/ -run TestBrave -v`
Expected: PASS (4/4)

**Step 5: Run full test suite + lint**

Run: `cd /home/krolik/src/go-enriche && go test ./... -count=1 && golangci-lint run ./...`

**Step 6: Commit**

```bash
git add search/brave.go search/brave_test.go
git commit -m "feat: add Brave Search provider

search.NewBrave(apiKey) implements Provider using the Brave Search API.
Supports freshness mapping (week→pw, month→pm), configurable max results.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 3: Google Custom Search Provider

**Why:** Google CSE is widely used, 100 free queries/day. Alternative to Brave/SearXNG.

**Files:**
- Create: `search/google.go`
- Create: `search/google_test.go`

**Step 1: Write the failing test**

Create `search/google_test.go`:

```go
package search

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGoogle_Search(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "test-key" {
			t.Errorf("missing key param")
		}
		if r.URL.Query().Get("cx") != "test-cx" {
			t.Errorf("missing cx param")
		}
		if r.URL.Query().Get("q") != "test query" {
			t.Errorf("wrong query, got %q", r.URL.Query().Get("q"))
		}

		resp := googleResponse{
			Items: []googleResult{
				{Link: "https://example.com/1", Title: "Result 1", Snippet: "First snippet"},
				{Link: "https://example.com/2", Title: "Result 2", Snippet: "Second snippet"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	google := NewGoogle("test-key", "test-cx", WithGoogleBaseURL(srv.URL))
	result, err := google.Search(context.Background(), "test query", "")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(result.Sources) != 2 {
		t.Errorf("expected 2 sources, got %d", len(result.Sources))
	}
	if result.Context == "" {
		t.Error("expected non-empty context")
	}
}

func TestGoogle_DateRestrict(t *testing.T) {
	t.Parallel()
	var gotDateRestrict string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotDateRestrict = r.URL.Query().Get("dateRestrict")
		resp := googleResponse{}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	google := NewGoogle("k", "cx", WithGoogleBaseURL(srv.URL))
	google.Search(context.Background(), "q", "week")

	if gotDateRestrict != "w1" {
		t.Errorf("expected 'w1' for week, got %q", gotDateRestrict)
	}
}

func TestGoogle_ErrorStatus(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	google := NewGoogle("k", "cx", WithGoogleBaseURL(srv.URL))
	_, err := google.Search(context.Background(), "q", "")
	if err == nil {
		t.Error("expected error on 403")
	}
}

func TestGoogle_EmptyItems(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := googleResponse{}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	google := NewGoogle("k", "cx", WithGoogleBaseURL(srv.URL))
	result, err := google.Search(context.Background(), "q", "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(result.Sources) != 0 {
		t.Errorf("expected 0 sources, got %d", len(result.Sources))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/krolik/src/go-enriche && go test ./search/ -run TestGoogle -v`
Expected: FAIL — `NewGoogle` undefined

**Step 3: Write implementation**

Create `search/google.go`:

```go
package search

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

const googleDefaultBaseURL = "https://www.googleapis.com/customsearch/v1"

// Google implements Provider using the Google Custom Search JSON API.
type Google struct {
	apiKey     string
	cx         string
	baseURL    string
	client     *http.Client
	maxResults int
}

// GoogleOption configures Google.
type GoogleOption func(*Google)

// WithGoogleBaseURL overrides the API endpoint (for testing).
func WithGoogleBaseURL(u string) GoogleOption {
	return func(g *Google) { g.baseURL = u }
}

// WithGoogleHTTPClient sets a custom HTTP client.
func WithGoogleHTTPClient(c *http.Client) GoogleOption {
	return func(g *Google) { g.client = c }
}

// WithGoogleMaxResults sets the max results to aggregate.
func WithGoogleMaxResults(n int) GoogleOption {
	return func(g *Google) { g.maxResults = n }
}

// NewGoogle creates a Google Custom Search provider.
func NewGoogle(apiKey, cx string, opts ...GoogleOption) *Google {
	g := &Google{
		apiKey:     apiKey,
		cx:         cx,
		baseURL:    googleDefaultBaseURL,
		client:     http.DefaultClient,
		maxResults: defaultMaxResults,
	}
	for _, o := range opts {
		o(g)
	}
	return g
}

// googleResponse is the JSON structure returned by Google CSE API.
type googleResponse struct {
	Items []googleResult `json:"items"`
}

type googleResult struct {
	Title   string `json:"title"`
	Link    string `json:"link"`
	Snippet string `json:"snippet"`
}

// timeRangeToGoogleDateRestrict maps enriche time ranges to Google dateRestrict values.
func timeRangeToGoogleDateRestrict(timeRange string) string {
	switch timeRange {
	case "week":
		return "w1"
	case "month":
		return "m1"
	case "day":
		return "d1"
	case "year":
		return "y1"
	default:
		return ""
	}
}

// Search queries Google Custom Search and returns aggregated context.
func (g *Google) Search(ctx context.Context, query string, timeRange string) (*SearchResult, error) {
	params := url.Values{}
	params.Set("key", g.apiKey)
	params.Set("cx", g.cx)
	params.Set("q", query)
	params.Set("num", "10")
	if dr := timeRangeToGoogleDateRestrict(timeRange); dr != "" {
		params.Set("dateRestrict", dr)
	}

	reqURL := g.baseURL + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("google: build request: %w", err)
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("google: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("google: read body: %w", err)
	}

	var data googleResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("google: parse JSON: %w", err)
	}

	return g.aggregate(data.Items), nil
}

func (g *Google) aggregate(items []googleResult) *SearchResult {
	generic := make([]searxngResult, 0, len(items))
	for _, r := range items {
		generic = append(generic, searxngResult{
			URL:     r.Link,
			Title:   r.Title,
			Content: r.Snippet,
		})
	}
	s := &SearXNG{maxResults: g.maxResults}
	return s.aggregate(generic)
}
```

**Step 4: Run tests**

Run: `cd /home/krolik/src/go-enriche && go test ./search/ -run TestGoogle -v`
Expected: PASS (4/4)

**Step 5: Run full test suite + lint**

**Step 6: Commit**

```bash
git add search/google.go search/google_test.go
git commit -m "feat: add Google Custom Search provider

search.NewGoogle(apiKey, cx) implements Provider using the Google CSE API.
Supports dateRestrict mapping (week→w1, month→m1), configurable max results.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 4: Multi-Provider Fallback

**Why:** If primary search provider fails (rate limit, downtime), fall back to secondary providers automatically.

**Files:**
- Create: `search/fallback.go`
- Create: `search/fallback_test.go`

**Step 1: Write the failing test**

Create `search/fallback_test.go`:

```go
package search

import (
	"context"
	"errors"
	"testing"
)

type failProvider struct {
	err error
}

func (f *failProvider) Search(_ context.Context, _ string, _ string) (*SearchResult, error) {
	return nil, f.err
}

type okProvider struct {
	name string
}

func (o *okProvider) Search(_ context.Context, _ string, _ string) (*SearchResult, error) {
	return &SearchResult{Context: o.name, Sources: []string{"https://" + o.name}}, nil
}

func TestFallback_PrimarySucceeds(t *testing.T) {
	t.Parallel()
	fb := NewFallback(&okProvider{"primary"}, &okProvider{"secondary"})
	result, err := fb.Search(context.Background(), "q", "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.Context != "primary" {
		t.Errorf("expected primary, got %q", result.Context)
	}
}

func TestFallback_PrimaryFails(t *testing.T) {
	t.Parallel()
	fb := NewFallback(
		&failProvider{errors.New("primary down")},
		&okProvider{"secondary"},
	)
	result, err := fb.Search(context.Background(), "q", "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.Context != "secondary" {
		t.Errorf("expected secondary, got %q", result.Context)
	}
}

func TestFallback_AllFail(t *testing.T) {
	t.Parallel()
	fb := NewFallback(
		&failProvider{errors.New("first down")},
		&failProvider{errors.New("second down")},
	)
	_, err := fb.Search(context.Background(), "q", "")
	if err == nil {
		t.Error("expected error when all providers fail")
	}
}

func TestFallback_ThreeProviders(t *testing.T) {
	t.Parallel()
	fb := NewFallback(
		&failProvider{errors.New("first down")},
		&failProvider{errors.New("second down")},
		&okProvider{"third"},
	)
	result, err := fb.Search(context.Background(), "q", "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.Context != "third" {
		t.Errorf("expected third, got %q", result.Context)
	}
}

func TestFallback_SingleProvider(t *testing.T) {
	t.Parallel()
	fb := NewFallback(&okProvider{"only"})
	result, err := fb.Search(context.Background(), "q", "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.Context != "only" {
		t.Errorf("expected 'only', got %q", result.Context)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/krolik/src/go-enriche && go test ./search/ -run TestFallback -v`
Expected: FAIL — `NewFallback` undefined

**Step 3: Write implementation**

Create `search/fallback.go`:

```go
package search

import (
	"context"
	"errors"
	"fmt"
)

// Fallback tries providers in order, returning the first successful result.
type Fallback struct {
	providers []Provider
}

// NewFallback creates a fallback provider chain.
// The first provider is primary; subsequent providers are tried on error.
func NewFallback(providers ...Provider) *Fallback {
	return &Fallback{providers: providers}
}

// Search tries each provider in order until one succeeds.
func (f *Fallback) Search(ctx context.Context, query string, timeRange string) (*SearchResult, error) {
	var errs []error
	for _, p := range f.providers {
		result, err := p.Search(ctx, query, timeRange)
		if err == nil {
			return result, nil
		}
		errs = append(errs, err)
	}
	return nil, fmt.Errorf("all providers failed: %w", errors.Join(errs...))
}
```

**Step 4: Run tests**

Run: `cd /home/krolik/src/go-enriche && go test ./search/ -run TestFallback -v`
Expected: PASS (5/5)

**Step 5: Run full test suite + lint**

**Step 6: Commit**

```bash
git add search/fallback.go search/fallback_test.go
git commit -m "feat: add multi-provider fallback

search.NewFallback(primary, fallbacks...) tries providers in order.
Returns first success. errors.Join on all-fail.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 5: Final Verification, Tag, Deploy

**Step 1: Run full test suite with race detector**

Run: `cd /home/krolik/src/go-enriche && go test ./... -v -count=1 -race`

**Step 2: Lint**

Run: `cd /home/krolik/src/go-enriche && make lint`

**Step 3: Update ROADMAP.md**

Mark all Phase 7 items as complete with `[x]`.

**Step 4: Commit and tag**

```bash
git add docs/ROADMAP.md
git commit -m "docs: mark Phase 7 as complete in roadmap"
git tag v0.3.0
git push origin main --tags
```

**Step 5: Update go-wp dependency**

```bash
cd /home/krolik/src/go-wp
go get github.com/anatolykoptev/go-enriche@v0.3.0
go mod tidy
```

Test and commit go-wp changes. Deploy.

# Phase 2: Fetch — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** HTTP fetch with status detection (Active/NotFound/Redirect/Unreachable/WebsiteDown), stealth TLS fingerprinting via go-stealth, and singleflight deduplication for parallel requests.

**Architecture:** `fetch/` package with three files: `status.go` (PageStatus type + constants), `fetcher.go` (Fetcher struct with singleflight, custom CheckRedirect for domain-change detection, io.LimitReader for max body), `stealth.go` (thin helper to create stealth-configured *http.Client). The Fetcher accepts a standard `*http.Client` — stealth or plain — so consumers choose their own client. Singleflight deduplicates concurrent requests to the same URL.

**Tech Stack:** Go 1.25, `golang.org/x/sync/singleflight`, `github.com/anatolykoptev/go-stealth`, `net/http/httptest`

---

### Task 1: Add dependencies

**Files:**
- Modify: `go.mod`

**Step 1: Add golang.org/x/sync and go-stealth**

```bash
cd /home/krolik/src/go-enriche
go get golang.org/x/sync
go get github.com/anatolykoptev/go-stealth
```

**Step 2: Verify go.mod**

Run: `go mod tidy && grep -E 'x/sync|go-stealth' go.mod`
Expected: Both appear in `require` block.

**Step 3: Verify build**

Run: `go build ./...`
Expected: exit 0

**Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add golang.org/x/sync and go-stealth dependencies"
```

---

### Task 2: PageStatus type + FetchResult struct

**Files:**
- Create: `fetch/status.go`
- Modify: `fetch/fetch.go` — remove stub content (replace with import marker if needed)
- Test: `fetch/status_test.go`

**Step 1: Write the test**

Create `fetch/status_test.go`:

```go
package fetch

import "testing"

func TestPageStatus_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status PageStatus
		want   string
	}{
		{StatusActive, "active"},
		{StatusNotFound, "not_found"},
		{StatusRedirect, "redirect"},
		{StatusUnreachable, "unreachable"},
		{StatusWebsiteDown, "website_down"},
	}
	for _, tt := range tests {
		if string(tt.status) != tt.want {
			t.Errorf("PageStatus %q != %q", tt.status, tt.want)
		}
	}
}

func TestFetchResult_Zero(t *testing.T) {
	t.Parallel()
	var r FetchResult
	if r.Status != "" || r.HTML != "" || r.FinalURL != "" || r.StatusCode != 0 {
		t.Error("zero FetchResult should have empty fields")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./fetch/ -run TestPageStatus -v`
Expected: FAIL — `PageStatus` undefined

**Step 3: Write the implementation**

Create `fetch/status.go`:

```go
package fetch

// PageStatus represents the availability status of a fetched page.
type PageStatus string

const (
	StatusActive      PageStatus = "active"
	StatusNotFound    PageStatus = "not_found"
	StatusRedirect    PageStatus = "redirect"
	StatusUnreachable PageStatus = "unreachable"
	StatusWebsiteDown PageStatus = "website_down"
)

// FetchResult is the output of a page fetch.
type FetchResult struct {
	HTML       string
	Status     PageStatus
	FinalURL   string
	StatusCode int
}
```

Update `fetch/fetch.go` — keep only the package doc:

```go
// Package fetch provides HTTP page fetching with status detection,
// stealth support, and singleflight deduplication.
package fetch
```

**Step 4: Run tests**

Run: `go test ./fetch/ -v`
Expected: PASS (2 tests)

**Step 5: Commit**

```bash
git add fetch/status.go fetch/status_test.go fetch/fetch.go
git commit -m "feat(fetch): add PageStatus type and FetchResult struct"
```

---

### Task 3: Fetcher with redirect detection and status classification

This is the core task. The Fetcher:
- Accepts `*http.Client` (stealth or plain)
- Uses custom `CheckRedirect` to detect cross-domain redirects
- Classifies HTTP status codes into PageStatus values
- Reads body with `io.LimitReader` for max body bytes
- Uses `singleflight.Group` to deduplicate parallel requests

**Files:**
- Create: `fetch/fetcher.go`
- Create: `fetch/fetcher_test.go`

**Step 1: Write the tests**

Create `fetch/fetcher_test.go`:

```go
package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFetch_Success(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body>Hello</body></html>"))
	}))
	defer srv.Close()

	f := NewFetcher()
	result, err := f.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != StatusActive {
		t.Errorf("expected active, got %s", result.Status)
	}
	if result.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", result.StatusCode)
	}
	if !strings.Contains(result.HTML, "Hello") {
		t.Errorf("expected body containing Hello, got %s", result.HTML)
	}
}

func TestFetch_NotFound(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	f := NewFetcher()
	result, err := f.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != StatusNotFound {
		t.Errorf("expected not_found, got %s", result.Status)
	}
	if result.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", result.StatusCode)
	}
}

func TestFetch_ServerError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	f := NewFetcher()
	result, err := f.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != StatusUnreachable {
		t.Errorf("expected unreachable, got %s", result.Status)
	}
}

func TestFetch_DomainRedirect(t *testing.T) {
	t.Parallel()

	// Target server (different domain in effect).
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("redirected"))
	}))
	defer target.Close()

	// Origin server redirects to target.
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/new-page", http.StatusMovedPermanently)
	}))
	defer origin.Close()

	f := NewFetcher()
	result, err := f.Fetch(context.Background(), origin.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != StatusRedirect {
		t.Errorf("expected redirect, got %s", result.Status)
	}
	if result.FinalURL == "" {
		t.Error("expected non-empty FinalURL for redirect")
	}
}

func TestFetch_SameDomainRedirect(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/page", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("final page"))
	}))
	defer srv.Close()

	f := NewFetcher()
	result, err := f.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != StatusActive {
		t.Errorf("same-domain redirect should be active, got %s", result.Status)
	}
	if !strings.Contains(result.HTML, "final page") {
		t.Error("expected body from final redirect destination")
	}
}

func TestFetch_Timeout(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := NewFetcher(WithTimeout(100 * time.Millisecond))
	result, err := f.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != StatusUnreachable {
		t.Errorf("expected unreachable on timeout, got %s", result.Status)
	}
}

func TestFetch_MaxBodyBytes(t *testing.T) {
	t.Parallel()
	bigBody := strings.Repeat("X", 1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(bigBody))
	}))
	defer srv.Close()

	f := NewFetcher(WithMaxBodyBytes(100))
	result, err := f.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.HTML) > 100 {
		t.Errorf("expected body truncated to 100 bytes, got %d", len(result.HTML))
	}
}

func TestFetch_EmptyURL(t *testing.T) {
	t.Parallel()
	f := NewFetcher()
	_, err := f.Fetch(context.Background(), "")
	if err == nil {
		t.Error("expected error for empty URL")
	}
}

func TestFetch_InvalidURL(t *testing.T) {
	t.Parallel()
	f := NewFetcher()
	result, err := f.Fetch(context.Background(), "not-a-url")
	if err != nil {
		// Error is acceptable.
		return
	}
	if result.Status != StatusUnreachable {
		t.Errorf("expected unreachable for invalid URL, got %s", result.Status)
	}
}

func TestFetch_Singleflight(t *testing.T) {
	t.Parallel()
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	f := NewFetcher()
	var wg sync.WaitGroup
	const concurrency = 10
	results := make([]*FetchResult, concurrency)
	errors := make([]error, concurrency)

	for i := range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errors[i] = f.Fetch(context.Background(), srv.URL)
		}()
	}
	wg.Wait()

	for i := range concurrency {
		if errors[i] != nil {
			t.Errorf("goroutine %d error: %v", i, errors[i])
		}
		if results[i] == nil || results[i].Status != StatusActive {
			t.Errorf("goroutine %d: expected active status", i)
		}
	}

	if got := callCount.Load(); got != 1 {
		t.Errorf("singleflight: expected 1 server hit, got %d", got)
	}
}

func TestFetch_ContextCanceled(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	f := NewFetcher()
	result, err := f.Fetch(ctx, srv.URL)
	if err != nil {
		return // Error for canceled context is acceptable.
	}
	if result.Status != StatusUnreachable {
		t.Errorf("expected unreachable for canceled context, got %s", result.Status)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./fetch/ -run TestFetch -v 2>&1 | head -5`
Expected: FAIL — `NewFetcher` undefined

**Step 3: Write the implementation**

Create `fetch/fetcher.go`:

```go
package fetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"
)

// Default configuration values.
const (
	DefaultMaxBodyBytes = 2 << 20     // 2 MB
	DefaultTimeout      = 15 * time.Second
	maxRedirects        = 5
)

// Fetcher performs HTTP page fetches with status detection and singleflight dedup.
type Fetcher struct {
	client       *http.Client
	maxBodyBytes int64
	sf           singleflight.Group
}

// Option configures a Fetcher.
type Option func(*Fetcher)

// WithClient sets a custom HTTP client (e.g., stealth-configured).
func WithClient(c *http.Client) Option {
	return func(f *Fetcher) { f.client = c }
}

// WithMaxBodyBytes sets the maximum response body size.
func WithMaxBodyBytes(n int64) Option {
	return func(f *Fetcher) { f.maxBodyBytes = n }
}

// WithTimeout sets the HTTP client timeout.
func WithTimeout(d time.Duration) Option {
	return func(f *Fetcher) { f.client.Timeout = d }
}

// NewFetcher creates a Fetcher with the given options.
func NewFetcher(opts ...Option) *Fetcher {
	f := &Fetcher{
		client: &http.Client{
			Timeout: DefaultTimeout,
		},
		maxBodyBytes: DefaultMaxBodyBytes,
	}
	for _, o := range opts {
		o(f)
	}
	return f
}

// Fetch retrieves a page and classifies its status.
// Concurrent calls for the same URL are deduplicated via singleflight.
func (f *Fetcher) Fetch(ctx context.Context, rawURL string) (*FetchResult, error) {
	if rawURL == "" {
		return nil, fmt.Errorf("fetch: empty URL")
	}

	v, err, _ := f.sf.Do(rawURL, func() (any, error) {
		return f.doFetch(ctx, rawURL)
	})
	if err != nil {
		return nil, err
	}
	result := v.(*FetchResult)
	return result, nil
}

func (f *Fetcher) doFetch(ctx context.Context, rawURL string) (*FetchResult, error) {
	origHost := extractHost(rawURL)

	// Clone client with custom redirect policy for domain-change detection.
	client := *f.client
	var finalURL string
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		finalURL = req.URL.String()
		if len(via) >= maxRedirects {
			return http.ErrUseLastResponse
		}
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return &FetchResult{Status: StatusUnreachable}, nil
	}

	resp, err := client.Do(req)
	if err != nil {
		return &FetchResult{Status: StatusUnreachable}, nil
	}
	defer resp.Body.Close() //nolint:errcheck

	// Detect cross-domain redirect.
	if finalURL != "" && extractHost(finalURL) != origHost {
		return &FetchResult{
			Status:     StatusRedirect,
			FinalURL:   finalURL,
			StatusCode: resp.StatusCode,
		}, nil
	}

	if resp.StatusCode == http.StatusNotFound {
		return &FetchResult{
			Status:     StatusNotFound,
			StatusCode: resp.StatusCode,
		}, nil
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return &FetchResult{
			Status:     StatusUnreachable,
			StatusCode: resp.StatusCode,
		}, nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, f.maxBodyBytes))
	if err != nil {
		return &FetchResult{
			Status:     StatusUnreachable,
			StatusCode: resp.StatusCode,
		}, nil
	}

	return &FetchResult{
		HTML:       string(body),
		Status:     StatusActive,
		FinalURL:   finalURL,
		StatusCode: resp.StatusCode,
	}, nil
}

// extractHost returns the lowercase host from a URL string.
func extractHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Host)
}
```

**Step 4: Run tests**

Run: `go test ./fetch/ -v -count=1`
Expected: PASS (all tests including singleflight)

**Step 5: Run lint**

Run: `golangci-lint run ./fetch/`
Expected: 0 issues

**Step 6: Commit**

```bash
git add fetch/fetcher.go fetch/fetcher_test.go
git commit -m "feat(fetch): add Fetcher with redirect detection, singleflight dedup, and status classification"
```

---

### Task 4: Stealth client helper

Thin helper for creating a stealth-configured `*http.Client` from go-stealth. The Fetcher doesn't know about stealth — it just takes an `*http.Client`. This helper makes it easy for consumers.

**Files:**
- Create: `fetch/stealth.go`
- Create: `fetch/stealth_test.go`

**Step 1: Write the test**

Create `fetch/stealth_test.go`:

```go
package fetch

import "testing"

func TestNewStealthClient_Default(t *testing.T) {
	t.Parallel()
	client, err := NewStealthClient()
	if err != nil {
		t.Fatalf("NewStealthClient error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.Transport == nil {
		t.Error("expected non-nil transport (stealth roundtripper)")
	}
}

func TestNewStealthClient_WithTimeout(t *testing.T) {
	t.Parallel()
	client, err := NewStealthClient(StealthWithTimeout(30))
	if err != nil {
		t.Fatalf("NewStealthClient error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewStealthClient_StdHTTP(t *testing.T) {
	t.Parallel()
	// WithStdHTTP uses stdlib backend (no CGO/TLS fingerprint).
	client, err := NewStealthClient(StealthWithStdHTTP())
	if err != nil {
		t.Fatalf("NewStealthClient error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./fetch/ -run TestNewStealthClient -v`
Expected: FAIL — `NewStealthClient` undefined

**Step 3: Write the implementation**

Create `fetch/stealth.go`:

```go
package fetch

import (
	"net/http"

	stealth "github.com/anatolykoptev/go-stealth"
)

// StealthOption configures the stealth client.
type StealthOption func(*[]stealth.ClientOption)

// StealthWithTimeout sets the request timeout in seconds.
func StealthWithTimeout(seconds int) StealthOption {
	return func(opts *[]stealth.ClientOption) {
		*opts = append(*opts, stealth.WithTimeout(seconds))
	}
}

// StealthWithProxy sets an HTTP/SOCKS5 proxy URL.
func StealthWithProxy(proxyURL string) StealthOption {
	return func(opts *[]stealth.ClientOption) {
		*opts = append(*opts, stealth.WithProxy(proxyURL))
	}
}

// StealthWithProfile sets the TLS fingerprint profile.
func StealthWithProfile(profile stealth.TLSProfile) StealthOption {
	return func(opts *[]stealth.ClientOption) {
		*opts = append(*opts, stealth.WithProfile(profile))
	}
}

// StealthWithStdHTTP uses the stdlib net/http backend (no TLS fingerprinting).
func StealthWithStdHTTP() StealthOption {
	return func(opts *[]stealth.ClientOption) {
		*opts = append(*opts, stealth.WithStdHTTP())
	}
}

const defaultStealthTimeoutSec = 15

// NewStealthClient creates an *http.Client with TLS fingerprinting via go-stealth.
// The returned client can be passed to NewFetcher via WithClient.
func NewStealthClient(opts ...StealthOption) (*http.Client, error) {
	stealthOpts := []stealth.ClientOption{
		stealth.WithTimeout(defaultStealthTimeoutSec),
	}
	for _, o := range opts {
		o(&stealthOpts)
	}

	bc, err := stealth.NewClient(stealthOpts...)
	if err != nil {
		return nil, err
	}
	return bc.StdClient(), nil
}
```

**Step 4: Run tests**

Run: `go test ./fetch/ -v -count=1`
Expected: PASS (all fetch tests)

**Step 5: Run lint**

Run: `golangci-lint run ./fetch/`
Expected: 0 issues

**Step 6: Commit**

```bash
git add fetch/stealth.go fetch/stealth_test.go
git commit -m "feat(fetch): add stealth client helper for go-stealth integration"
```

---

### Task 5: Update root types.go to use PageStatus

The root `types.go` currently has `Status string` in Result. Update it to use `fetch.PageStatus` for type safety.

**Files:**
- Modify: `types.go:32` — change `Status string` to `Status fetch.PageStatus`

**Step 1: Update types.go**

Change the import and the Status field:

```go
package enriche

import (
	"time"

	"github.com/anatolykoptev/go-enriche/extract"
	"github.com/anatolykoptev/go-enriche/fetch"
)

// ...existing code...

// Result is the output of enrichment.
type Result struct {
	Name          string
	URL           string
	Status        fetch.PageStatus // Active/NotFound/Redirect/Unreachable/WebsiteDown
	Content       string           // extracted article text
	// ...rest unchanged...
}
```

**Step 2: Build**

Run: `go build ./...`
Expected: exit 0

**Step 3: Run all tests**

Run: `make test`
Expected: PASS

**Step 4: Run lint**

Run: `make lint`
Expected: 0 issues

**Step 5: Commit**

```bash
git add types.go
git commit -m "refactor: use fetch.PageStatus in Result instead of raw string"
```

---

### Task 6: Update ROADMAP.md + run final verification

**Files:**
- Modify: `docs/ROADMAP.md` — mark Phase 2 items as done

**Step 1: Update ROADMAP.md**

Change Phase 2 section to mark all items complete:

```markdown
## Phase 2: Fetch ✅

**Goal**: HTTP fetch with status detection, stealth, singleflight dedup.

- [x] `fetch/status.go` — PageStatus enum (Active/NotFound/Redirect/Unreachable/WebsiteDown)
- [x] `fetch/fetcher.go` — `Fetcher{}`, `Fetch(ctx, url) (*FetchResult, error)`, singleflight
- [x] `fetch/stealth.go` — go-stealth integration, optional TLS fingerprinting
- [x] Custom `CheckRedirect` for domain-change detection
- [x] Max body bytes (2MB), timeout (15s)
- [x] Tests: httptest.Server — redirects, 404, timeouts, domain changes, singleflight

**Success**: Fetches real pages with correct status detection. Singleflight deduplicates parallel requests. ✅
```

**Step 2: Final verification**

Run: `make lint && make test`
Expected: 0 lint issues, all tests pass (29 extract/structured + ~14 fetch = ~43 total)

**Step 3: Commit**

```bash
git add docs/ROADMAP.md
git commit -m "docs: mark Phase 2 complete in roadmap"
```

---

## Notes for the Implementer

### go-stealth API quirks
- `stealth.WithTimeout(seconds)` takes `int`, not `time.Duration`.
- `BrowserClient.StdClient()` returns `*http.Client` with BrowserClient as Transport.
- StdClient downloads full body into memory via `Do()` before returning `*http.Response`. The `io.LimitReader` limits reading from the in-memory buffer, NOT the actual download. This is a known limitation — document it if needed.
- `stealth.WithFollowRedirects()` enables redirect following in the stealth backend. BUT we use our own `CheckRedirect` for domain detection, so do NOT pass `WithFollowRedirects()` — let the stdlib redirect policy handle it.

### singleflight behavior
- `singleflight.Do(key, fn)` returns the same result for all concurrent callers with the same key.
- The key is the raw URL string.
- After the in-flight call completes, subsequent calls create new requests (no caching).
- The `FetchResult` is shared via pointer — callers must not mutate it.

### Testing with httptest
- `httptest.NewServer` creates a server on `127.0.0.1:PORT`. Two different httptest servers have different ports → different hosts → domain change detected.
- For same-domain redirect tests, redirect within the same server.
- For domain-change redirect tests, redirect from one server to another.

### Linter compliance
- Magic numbers: use named constants (`DefaultMaxBodyBytes`, `DefaultTimeout`, `maxRedirects`).
- Don't use raw HTTP status codes — use `http.StatusNotFound`, `http.StatusBadRequest`, etc.
- `resp.Body.Close()` must have `//nolint:errcheck` or assign to `_`.

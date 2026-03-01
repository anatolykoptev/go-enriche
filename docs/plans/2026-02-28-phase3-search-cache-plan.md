# Phase 3: Search + Cache — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** SearXNG context search with mode-aware query building, and multi-layer caching (in-memory L1, Redis L2, tiered cascade).

**Architecture:** `search/` package with Provider interface, SearXNG implementation, and query builder. `cache/` package extends existing Cache interface with Memory (sync.Map), Redis (go-redis), and Tiered (L1→L2) implementations. SearXNG returns context text + source URLs. Cache implementations are composable — consumers pick what they need.

**Tech Stack:** Go 1.25, `github.com/redis/go-redis/v9`, `github.com/alicebob/miniredis/v2` (test), `net/http/httptest`

---

### Task 1: Add dependencies

**Files:**
- Modify: `go.mod`

**Step 1: Add go-redis and miniredis**

```bash
cd /home/krolik/src/go-enriche
go get github.com/redis/go-redis/v9
go get github.com/alicebob/miniredis/v2
```

**Step 2: Tidy**

Run: `go mod tidy && go build ./...`
Expected: exit 0

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add go-redis and miniredis dependencies"
```

---

### Task 2: Search Provider interface + query builder

**Files:**
- Create: `search/provider.go`
- Create: `search/query.go`
- Create: `search/query_test.go`
- Modify: `search/search.go` — keep package doc only

**search/provider.go:**

```go
package search

import "context"

// SearchResult holds search context and source URLs.
type SearchResult struct {
	Context string   // concatenated title+content from top results
	Sources []string // deduplicated source URLs
}

// Provider searches for external context about a topic.
type Provider interface {
	Search(ctx context.Context, query string, timeRange string) (*SearchResult, error)
}
```

**search/query.go:**

```go
package search

import (
	"fmt"
	"time"
)

// Time range constants for SearXNG.
const (
	TimeRangeWeek  = "week"
	TimeRangeMonth = "month"
)

// Mode constants matching root enriche.Mode values.
const (
	modeNews   = 0
	modePlaces = 1
	modeEvents = 2
)

// BuildQuery constructs a search query and time range based on enrichment mode.
// mode: 0=news, 1=places, 2=events (matches enriche.Mode iota values).
func BuildQuery(mode int, name, city string) (query, timeRange string) {
	switch mode {
	case modePlaces:
		if city != "" {
			return fmt.Sprintf("%s %s", name, city), ""
		}
		return name, ""
	case modeEvents:
		year := time.Now().Format("2006")
		if city != "" {
			return fmt.Sprintf("%s %s %s", name, city, year), TimeRangeMonth
		}
		return fmt.Sprintf("%s %s", name, year), TimeRangeMonth
	default: // news
		return name, TimeRangeWeek
	}
}
```

**search/query_test.go:**

```go
package search

import (
	"strings"
	"testing"
	"time"
)

func TestBuildQuery_News(t *testing.T) {
	t.Parallel()
	query, timeRange := BuildQuery(modeNews, "Go language", "")
	if query != "Go language" {
		t.Errorf("expected 'Go language', got %q", query)
	}
	if timeRange != TimeRangeWeek {
		t.Errorf("expected 'week', got %q", timeRange)
	}
}

func TestBuildQuery_Places(t *testing.T) {
	t.Parallel()
	query, timeRange := BuildQuery(modePlaces, "Cafe Nora", "Moscow")
	if query != "Cafe Nora Moscow" {
		t.Errorf("expected 'Cafe Nora Moscow', got %q", query)
	}
	if timeRange != "" {
		t.Errorf("expected empty timeRange for places, got %q", timeRange)
	}
}

func TestBuildQuery_PlacesNoCity(t *testing.T) {
	t.Parallel()
	query, _ := BuildQuery(modePlaces, "Museum", "")
	if query != "Museum" {
		t.Errorf("expected 'Museum', got %q", query)
	}
}

func TestBuildQuery_Events(t *testing.T) {
	t.Parallel()
	query, timeRange := BuildQuery(modeEvents, "Jazz Fest", "Berlin")
	year := time.Now().Format("2006")
	if !strings.Contains(query, "Jazz Fest Berlin "+year) {
		t.Errorf("expected query with city and year, got %q", query)
	}
	if timeRange != TimeRangeMonth {
		t.Errorf("expected 'month', got %q", timeRange)
	}
}

func TestBuildQuery_EventsNoCity(t *testing.T) {
	t.Parallel()
	query, _ := BuildQuery(modeEvents, "Conference", "")
	year := time.Now().Format("2006")
	if query != "Conference "+year {
		t.Errorf("expected 'Conference %s', got %q", year, query)
	}
}
```

**Step 1: Run tests to verify they fail**

Run: `go test ./search/ -v`
Expected: FAIL — types undefined

**Step 2: Create all three files**

**Step 3: Run tests**

Run: `go test ./search/ -v`
Expected: 5 tests PASS

**Step 4: Lint**

Run: `golangci-lint run ./search/`
Expected: 0 issues

**Step 5: Commit**

```bash
git add search/
git commit -m "feat(search): add Provider interface and mode-aware query builder"
```

---

### Task 3: SearXNG implementation

**Files:**
- Create: `search/searxng.go`
- Create: `search/searxng_test.go`

**search/searxng.go:**

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
)

const (
	defaultMaxResults = 3
	maxResponseBytes  = 2 << 20 // 2 MB
)

// SearXNG implements Provider using a SearXNG instance.
type SearXNG struct {
	baseURL    string
	client     *http.Client
	maxResults int
}

// SearXNGOption configures SearXNG.
type SearXNGOption func(*SearXNG)

// WithHTTPClient sets a custom HTTP client for SearXNG requests.
func WithHTTPClient(c *http.Client) SearXNGOption {
	return func(s *SearXNG) { s.client = c }
}

// WithMaxResults sets the maximum number of results to return.
func WithMaxResults(n int) SearXNGOption {
	return func(s *SearXNG) { s.maxResults = n }
}

// NewSearXNG creates a SearXNG provider.
func NewSearXNG(baseURL string, opts ...SearXNGOption) *SearXNG {
	s := &SearXNG{
		baseURL:    strings.TrimRight(baseURL, "/"),
		client:     http.DefaultClient,
		maxResults: defaultMaxResults,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// searxngResponse is the JSON structure returned by SearXNG API.
type searxngResponse struct {
	Results []searxngResult `json:"results"`
}

type searxngResult struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

// Search queries SearXNG and returns aggregated context and source URLs.
func (s *SearXNG) Search(ctx context.Context, query string, timeRange string) (*SearchResult, error) {
	params := url.Values{}
	params.Set("q", query)
	params.Set("format", "json")
	params.Set("categories", "general")
	if timeRange != "" {
		params.Set("time_range", timeRange)
	}

	reqURL := s.baseURL + "/search?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("searxng: build request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("searxng: request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("searxng: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("searxng: read body: %w", err)
	}

	var data searxngResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("searxng: parse JSON: %w", err)
	}

	return s.aggregate(data.Results), nil
}

func (s *SearXNG) aggregate(results []searxngResult) *SearchResult {
	var (
		contextParts []string
		sources      []string
		seen         = make(map[string]bool)
	)

	for _, r := range results {
		if len(sources) >= s.maxResults {
			break
		}

		norm := normalizeURL(r.URL)
		if norm == "" || seen[norm] {
			continue
		}
		seen[norm] = true

		sources = append(sources, r.URL)
		if r.Title != "" && r.Content != "" {
			contextParts = append(contextParts, r.Title+": "+r.Content)
		} else if r.Content != "" {
			contextParts = append(contextParts, r.Content)
		} else if r.Title != "" {
			contextParts = append(contextParts, r.Title)
		}
	}

	return &SearchResult{
		Context: strings.Join(contextParts, "\n\n"),
		Sources: sources,
	}
}

// normalizeURL strips fragment, lowercases host/scheme, removes trailing slash.
func normalizeURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	u.Fragment = ""
	u.Host = strings.ToLower(u.Host)
	u.Scheme = strings.ToLower(u.Scheme)
	result := u.String()
	return strings.TrimRight(result, "/")
}
```

**search/searxng_test.go:**

```go
package search

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSearXNG_Search(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Errorf("expected /search path, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("format") != "json" {
			t.Error("expected format=json")
		}
		if r.URL.Query().Get("categories") != "general" {
			t.Error("expected categories=general")
		}
		resp := searxngResponse{
			Results: []searxngResult{
				{URL: "https://example.com/1", Title: "Result 1", Content: "Content 1"},
				{URL: "https://example.com/2", Title: "Result 2", Content: "Content 2"},
				{URL: "https://example.com/3", Title: "Result 3", Content: "Content 3"},
				{URL: "https://example.com/4", Title: "Result 4", Content: "Content 4"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	s := NewSearXNG(srv.URL)
	result, err := s.Search(context.Background(), "test query", TimeRangeWeek)
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(result.Sources) != defaultMaxResults {
		t.Errorf("expected %d sources, got %d", defaultMaxResults, len(result.Sources))
	}
	if result.Context == "" {
		t.Error("expected non-empty context")
	}
}

func TestSearXNG_TimeRange(t *testing.T) {
	t.Parallel()
	var gotTimeRange string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTimeRange = r.URL.Query().Get("time_range")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(searxngResponse{})
	}))
	defer srv.Close()

	s := NewSearXNG(srv.URL)
	_, _ = s.Search(context.Background(), "test", TimeRangeMonth)
	if gotTimeRange != TimeRangeMonth {
		t.Errorf("expected time_range=month, got %q", gotTimeRange)
	}
}

func TestSearXNG_EmptyTimeRange(t *testing.T) {
	t.Parallel()
	var hasTimeRange bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hasTimeRange = r.URL.Query().Has("time_range")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(searxngResponse{})
	}))
	defer srv.Close()

	s := NewSearXNG(srv.URL)
	_, _ = s.Search(context.Background(), "test", "")
	if hasTimeRange {
		t.Error("time_range should not be sent when empty")
	}
}

func TestSearXNG_Dedup(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := searxngResponse{
			Results: []searxngResult{
				{URL: "https://example.com/page", Title: "A", Content: "Content A"},
				{URL: "https://example.com/page", Title: "B", Content: "Content B"},
				{URL: "https://EXAMPLE.COM/page/", Title: "C", Content: "Content C"},
				{URL: "https://other.com/x", Title: "D", Content: "Content D"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	s := NewSearXNG(srv.URL)
	result, err := s.Search(context.Background(), "test", "")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	// First 3 URLs normalize to the same; only 2 unique sources expected.
	if len(result.Sources) != 2 {
		t.Errorf("expected 2 unique sources after dedup, got %d: %v", len(result.Sources), result.Sources)
	}
}

func TestSearXNG_ServerError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	s := NewSearXNG(srv.URL)
	_, err := s.Search(context.Background(), "test", "")
	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestSearXNG_CustomMaxResults(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := searxngResponse{
			Results: []searxngResult{
				{URL: "https://a.com", Title: "A", Content: "CA"},
				{URL: "https://b.com", Title: "B", Content: "CB"},
				{URL: "https://c.com", Title: "C", Content: "CC"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	s := NewSearXNG(srv.URL, WithMaxResults(1))
	result, err := s.Search(context.Background(), "test", "")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(result.Sources) != 1 {
		t.Errorf("expected 1 source with WithMaxResults(1), got %d", len(result.Sources))
	}
}

func TestNormalizeURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"https://Example.COM/page#section", "https://example.com/page"},
		{"https://example.com/page/", "https://example.com/page"},
		{"HTTP://EXAMPLE.COM", "http://example.com"},
		{"", ""},
	}
	for _, tt := range tests {
		got := normalizeURL(tt.input)
		if got != tt.want {
			t.Errorf("normalizeURL(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
```

**Step 1: Run tests to verify they fail**

Run: `go test ./search/ -v 2>&1 | head -5`
Expected: FAIL — `SearXNG` undefined

**Step 2: Create both files**

**Step 3: Run tests**

Run: `go test ./search/ -v -count=1`
Expected: 12 tests PASS (5 query + 7 searxng)

**Step 4: Lint**

Run: `golangci-lint run ./search/`
Expected: 0 issues

**Step 5: Commit**

```bash
git add search/
git commit -m "feat(search): add SearXNG implementation with dedup and httptest tests"
```

---

### Task 4: Memory cache (L1)

**Files:**
- Create: `cache/memory.go`
- Create: `cache/memory_test.go`

**cache/memory.go:**

```go
package cache

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// Memory is an in-memory Cache backed by sync.Map.
type Memory struct {
	data sync.Map
}

type memoryEntry struct {
	value     []byte
	expiresAt time.Time
}

// NewMemory creates an in-memory cache.
func NewMemory() *Memory {
	return &Memory{}
}

// Get retrieves a cached value. Returns false if not found or expired.
func (m *Memory) Get(_ context.Context, key string, dest any) bool {
	raw, ok := m.data.Load(key)
	if !ok {
		return false
	}
	entry := raw.(memoryEntry)
	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		m.data.Delete(key)
		return false
	}
	return json.Unmarshal(entry.value, dest) == nil
}

// Set stores a value with the given TTL. Zero TTL means no expiration.
func (m *Memory) Set(_ context.Context, key string, value any, ttl time.Duration) {
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl)
	}
	m.data.Store(key, memoryEntry{value: data, expiresAt: expiresAt})
}
```

**cache/memory_test.go:**

```go
package cache

import (
	"context"
	"testing"
	"time"
)

func TestMemory_SetGet(t *testing.T) {
	t.Parallel()
	m := NewMemory()
	ctx := context.Background()

	m.Set(ctx, "key1", "hello", time.Minute)
	var got string
	if !m.Get(ctx, "key1", &got) {
		t.Fatal("expected cache hit")
	}
	if got != "hello" {
		t.Errorf("expected 'hello', got %q", got)
	}
}

func TestMemory_Miss(t *testing.T) {
	t.Parallel()
	m := NewMemory()
	var got string
	if m.Get(context.Background(), "nonexistent", &got) {
		t.Error("expected cache miss")
	}
}

func TestMemory_Expiry(t *testing.T) {
	t.Parallel()
	m := NewMemory()
	ctx := context.Background()

	m.Set(ctx, "exp", "value", time.Millisecond)
	time.Sleep(5 * time.Millisecond)

	var got string
	if m.Get(ctx, "exp", &got) {
		t.Error("expected cache miss after TTL expiry")
	}
}

func TestMemory_NoExpiry(t *testing.T) {
	t.Parallel()
	m := NewMemory()
	ctx := context.Background()

	m.Set(ctx, "forever", "value", 0)
	var got string
	if !m.Get(ctx, "forever", &got) {
		t.Fatal("expected cache hit with zero TTL")
	}
}

func TestMemory_Struct(t *testing.T) {
	t.Parallel()
	type item struct {
		Name  string
		Count int
	}
	m := NewMemory()
	ctx := context.Background()

	m.Set(ctx, "item", item{Name: "test", Count: 42}, time.Minute)
	var got item
	if !m.Get(ctx, "item", &got) {
		t.Fatal("expected cache hit")
	}
	if got.Name != "test" || got.Count != 42 {
		t.Errorf("unexpected struct: %+v", got)
	}
}

func TestMemory_Overwrite(t *testing.T) {
	t.Parallel()
	m := NewMemory()
	ctx := context.Background()

	m.Set(ctx, "k", "v1", time.Minute)
	m.Set(ctx, "k", "v2", time.Minute)
	var got string
	if !m.Get(ctx, "k", &got) {
		t.Fatal("expected cache hit")
	}
	if got != "v2" {
		t.Errorf("expected 'v2' after overwrite, got %q", got)
	}
}
```

**Step 1: Run tests**

Run: `go test ./cache/ -v -count=1`
Expected: 6 tests PASS

**Step 2: Lint**

Run: `golangci-lint run ./cache/`
Expected: 0 issues

**Step 3: Commit**

```bash
git add cache/memory.go cache/memory_test.go
git commit -m "feat(cache): add in-memory L1 cache with TTL expiry"
```

---

### Task 5: Redis cache (L2) + Tiered cache

**Files:**
- Create: `cache/redis.go`
- Create: `cache/tiered.go`
- Create: `cache/redis_test.go`
- Create: `cache/tiered_test.go`

**cache/redis.go:**

```go
package cache

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis is a Cache backed by Redis.
type Redis struct {
	client *redis.Client
}

// NewRedis creates a Redis cache.
func NewRedis(client *redis.Client) *Redis {
	return &Redis{client: client}
}

// Get retrieves a cached value from Redis. Returns false if not found.
func (r *Redis) Get(ctx context.Context, key string, dest any) bool {
	data, err := r.client.Get(ctx, key).Bytes()
	if err != nil {
		return false
	}
	return json.Unmarshal(data, dest) == nil
}

// Set stores a value in Redis with the given TTL. Zero TTL means no expiration.
func (r *Redis) Set(ctx context.Context, key string, value any, ttl time.Duration) {
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	r.client.Set(ctx, key, data, ttl) //nolint:errcheck
}
```

**cache/tiered.go:**

```go
package cache

import (
	"context"
	"time"
)

// Tiered is a Cache that checks L1 first, then L2.
// On L2 hit, the value is promoted to L1.
type Tiered struct {
	l1 Cache
	l2 Cache
}

// NewTiered creates a tiered cache with L1 (fast) and L2 (durable).
func NewTiered(l1, l2 Cache) *Tiered {
	return &Tiered{l1: l1, l2: l2}
}

// Get checks L1 first, then L2. On L2 hit, promotes to L1.
func (t *Tiered) Get(ctx context.Context, key string, dest any) bool {
	if t.l1.Get(ctx, key, dest) {
		return true
	}
	if t.l2.Get(ctx, key, dest) {
		// Promote to L1 with a short TTL.
		t.l1.Set(ctx, key, dest, promotionTTL)
		return true
	}
	return false
}

const promotionTTL = 5 * time.Minute

// Set stores in both L1 and L2.
func (t *Tiered) Set(ctx context.Context, key string, value any, ttl time.Duration) {
	t.l1.Set(ctx, key, value, ttl)
	t.l2.Set(ctx, key, value, ttl)
}
```

**cache/redis_test.go:**

```go
package cache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestRedis(t *testing.T) (*Redis, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return NewRedis(client), mr
}

func TestRedis_SetGet(t *testing.T) {
	t.Parallel()
	r, _ := newTestRedis(t)
	ctx := context.Background()

	r.Set(ctx, "key1", "hello", time.Minute)
	var got string
	if !r.Get(ctx, "key1", &got) {
		t.Fatal("expected cache hit")
	}
	if got != "hello" {
		t.Errorf("expected 'hello', got %q", got)
	}
}

func TestRedis_Miss(t *testing.T) {
	t.Parallel()
	r, _ := newTestRedis(t)
	var got string
	if r.Get(context.Background(), "nonexistent", &got) {
		t.Error("expected cache miss")
	}
}

func TestRedis_Expiry(t *testing.T) {
	t.Parallel()
	r, mr := newTestRedis(t)
	ctx := context.Background()

	r.Set(ctx, "exp", "value", time.Second)
	mr.FastForward(2 * time.Second)

	var got string
	if r.Get(ctx, "exp", &got) {
		t.Error("expected cache miss after TTL expiry")
	}
}

func TestRedis_Struct(t *testing.T) {
	t.Parallel()
	type item struct {
		Name  string
		Count int
	}
	r, _ := newTestRedis(t)
	ctx := context.Background()

	r.Set(ctx, "item", item{Name: "test", Count: 42}, time.Minute)
	var got item
	if !r.Get(ctx, "item", &got) {
		t.Fatal("expected cache hit")
	}
	if got.Name != "test" || got.Count != 42 {
		t.Errorf("unexpected struct: %+v", got)
	}
}
```

**cache/tiered_test.go:**

```go
package cache

import (
	"context"
	"testing"
	"time"
)

func TestTiered_L1Hit(t *testing.T) {
	t.Parallel()
	l1 := NewMemory()
	l2 := NewMemory()
	tc := NewTiered(l1, l2)
	ctx := context.Background()

	l1.Set(ctx, "k", "from-l1", time.Minute)

	var got string
	if !tc.Get(ctx, "k", &got) {
		t.Fatal("expected hit from L1")
	}
	if got != "from-l1" {
		t.Errorf("expected 'from-l1', got %q", got)
	}
}

func TestTiered_L2Hit_Promotes(t *testing.T) {
	t.Parallel()
	l1 := NewMemory()
	l2 := NewMemory()
	tc := NewTiered(l1, l2)
	ctx := context.Background()

	// Only in L2.
	l2.Set(ctx, "k", "from-l2", time.Minute)

	var got string
	if !tc.Get(ctx, "k", &got) {
		t.Fatal("expected hit from L2")
	}
	if got != "from-l2" {
		t.Errorf("expected 'from-l2', got %q", got)
	}

	// Should now be in L1.
	var promoted string
	if !l1.Get(ctx, "k", &promoted) {
		t.Error("expected value promoted to L1 after L2 hit")
	}
}

func TestTiered_Miss(t *testing.T) {
	t.Parallel()
	tc := NewTiered(NewMemory(), NewMemory())
	var got string
	if tc.Get(context.Background(), "nonexistent", &got) {
		t.Error("expected cache miss from both tiers")
	}
}

func TestTiered_SetStoresBoth(t *testing.T) {
	t.Parallel()
	l1 := NewMemory()
	l2 := NewMemory()
	tc := NewTiered(l1, l2)
	ctx := context.Background()

	tc.Set(ctx, "k", "value", time.Minute)

	var got1, got2 string
	if !l1.Get(ctx, "k", &got1) {
		t.Error("expected value in L1 after Tiered.Set")
	}
	if !l2.Get(ctx, "k", &got2) {
		t.Error("expected value in L2 after Tiered.Set")
	}
}

func TestTiered_WithRedis(t *testing.T) {
	t.Parallel()
	l1 := NewMemory()
	r, _ := newTestRedis(t)
	tc := NewTiered(l1, r)
	ctx := context.Background()

	tc.Set(ctx, "k", "redis-value", time.Minute)

	// Clear L1 to force L2 read.
	l1new := NewMemory()
	tc2 := NewTiered(l1new, r)

	var got string
	if !tc2.Get(ctx, "k", &got) {
		t.Fatal("expected hit from Redis L2")
	}
	if got != "redis-value" {
		t.Errorf("expected 'redis-value', got %q", got)
	}
}
```

**Step 1: Run tests**

Run: `go test ./cache/ -v -count=1`
Expected: 15 tests PASS (6 memory + 4 redis + 5 tiered)

**Step 2: Lint**

Run: `golangci-lint run ./cache/`
Expected: 0 issues

**Step 3: Commit**

```bash
git add cache/redis.go cache/tiered.go cache/redis_test.go cache/tiered_test.go
git commit -m "feat(cache): add Redis L2 and Tiered L1→L2 cache implementations"
```

---

### Task 6: Update ROADMAP.md + final verification

**Files:**
- Modify: `docs/ROADMAP.md`

**Step 1: Update ROADMAP.md**

Mark Phase 3 section as complete.

**Step 2: Final verification**

Run: `make lint && make test`
Expected: 0 lint issues, all tests pass

**Step 3: Commit**

```bash
git add docs/ROADMAP.md
git commit -m "docs: mark Phase 3 complete in roadmap"
```

---

## Notes for the Implementer

### SearXNG API
- Endpoint: `{baseURL}/search?q=...&format=json&categories=general`
- Returns JSON with `results` array, each element has `url`, `title`, `content`
- Time range values: `"week"`, `"month"`, or omit param entirely
- Response body capped at 2MB via `io.LimitReader`

### Cache serialization
- Both Memory and Redis use `encoding/json` for serialization
- This means the `dest` parameter in `Get` must be a pointer to a JSON-deserializable type
- `Set` silently fails on unmarshalable values (graceful degradation)

### Tiered promotion
- On L2 hit, value is promoted to L1 with `promotionTTL` (5 min)
- This uses the _deserialized_ value (via dest), so types must be JSON round-trip stable

### miniredis for testing
- `miniredis.RunT(t)` creates an in-memory Redis compatible server
- `mr.FastForward(d)` simulates time passing for TTL expiry tests
- No real Redis needed for tests

### Query builder + Mode coupling
- `BuildQuery` takes `int` (not `enriche.Mode`) to avoid circular imports
- The int values match `enriche.Mode` iota: 0=news, 1=places, 2=events
- Constants `modeNews`, `modePlaces`, `modeEvents` are unexported — used internally only

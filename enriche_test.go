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
		w.Write([]byte(html)) //nolint:errcheck
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
		w.Write([]byte(testHTML)) //nolint:errcheck
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
	type searxResult struct {
		URL     string `json:"url"`
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	type searxResponse struct {
		Results []searxResult `json:"results"`
	}

	searxSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := searxResponse{
			Results: []searxResult{
				{URL: "https://src.com/1", Title: "Source 1", Content: "Info about topic"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
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

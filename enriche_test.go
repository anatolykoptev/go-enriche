package enriche

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anatolykoptev/go-enriche/cache"
	"github.com/anatolykoptev/go-enriche/fetch"
	"github.com/anatolykoptev/go-enriche/maps"
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

// testFetcher builds a fetch.Fetcher whose client skips the default SSRF
// guard (go-kit httputil.NewSSRFGuardedClient, wired in fetch.NewFetcher),
// for tests that point item.URL at a local
// httptest.Server (loopback) — a legitimate target in a test, but one the
// guarded default correctly refuses. Uses the pre-existing WithClient escape
// hatch, same as any other caller opting out of the default guard. Shared
// across every *_test.go file in this package.
func testFetcher() *fetch.Fetcher {
	return fetch.NewFetcher(fetch.WithClient(&http.Client{Timeout: fetch.DefaultTimeout}))
}

// allowAllTargets is a permissive Enricher.targetGuard override (see
// WithTargetGuard) for tests that drive the oxBrowser/browserFetch render
// delegates against a local httptest.Server. The REAL default guard
// (httputil.CheckRawURL, go-kit) would refuse a loopback target, same reasoning as
// testFetcher — this is the render-delegate-gate analogue of that escape
// hatch, not a production code path.
func allowAllTargets(context.Context, string) error { return nil }

// newTestEnricher builds an Enricher defaulted for tests that point item.URL
// (or a discovered contacts/search-source URL) at a local httptest.Server:
// the SSRF guard — both fetch.Fetcher default transport and the
// Enricher-level targetGuard (see go-kit httputil, options.go) — correctly
// refuses a loopback target, so every such test needs an explicit opt-out.
// Caller opts are applied AFTER these defaults, so a test can still override
// either (e.g. to exercise the real guard, pass New() directly instead).
func newTestEnricher(opts ...Option) *Enricher {
	base := []Option{WithFetcher(testFetcher()), WithTargetGuard(allowAllTargets)}
	return New(append(base, opts...)...)
}

func TestEnrich_FetchAndExtract(t *testing.T) {
	t.Parallel()
	srv := newTestServer(testHTML, http.StatusOK)
	defer srv.Close()

	e := newTestEnricher(WithFetcher(testFetcher()))
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

	e := newTestEnricher()
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

	e := newTestEnricher(WithSearch(mock))
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
	e := newTestEnricher(WithCache(mem), WithFetcher(testFetcher()))

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

	// Source server to be fetched from search results.
	srcSrv := newTestServer(testHTML, http.StatusOK)
	defer srcSrv.Close()

	mock := &mockProvider{
		result: &search.SearchResult{
			Context: "found via search",
			Sources: []string{srcSrv.URL},
		},
	}

	e := newTestEnricher(WithSearch(mock))
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
	// No URL → search sources fetched → status should be active.
	if result.Status != StatusActive {
		t.Errorf("expected StatusActive from search sources, got %q", result.Status)
	}
	if result.Content == "" {
		t.Error("expected content from search source fetch, got empty")
	}
}

func TestEnrich_NoURL_NoSearch(t *testing.T) {
	t.Parallel()
	// No search provider → no sources → status stays empty.
	e := newTestEnricher()
	result, err := e.Enrich(context.Background(), Item{
		Name: "Minimal NoURL",
		Mode: ModeNews,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if result.Status != "" {
		t.Errorf("expected empty status, got %q", result.Status)
	}
}

func TestEnrich_GracefulDegradation(t *testing.T) {
	t.Parallel()
	// No cache, no search, no stealth — should work with defaults.
	e := newTestEnricher()
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

	e := newTestEnricher(WithConcurrency(2))
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

	e := newTestEnricher(WithConcurrency(2))
	items := make([]Item, 6)
	for i := range items {
		items[i] = Item{Name: "item", URL: srv.URL}
	}

	e.EnrichBatch(context.Background(), items)

	if maxConcurrent.Load() > 2 {
		t.Errorf("expected max 2 concurrent, got %d", maxConcurrent.Load())
	}
}

func TestEnrichBatch_ContextCancel(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		// Slow handler — should be interrupted by context cancellation.
		select {
		case <-r.Context().Done():
			w.WriteHeader(http.StatusServiceUnavailable)
		case <-time.After(2 * time.Second):
			w.Write([]byte(testHTML)) //nolint:errcheck
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	e := newTestEnricher(WithConcurrency(2))
	items := make([]Item, 4)
	for i := range items {
		items[i] = Item{Name: "cancel", URL: srv.URL}
	}

	start := time.Now()
	results := e.EnrichBatch(ctx, items)
	elapsed := time.Since(start)

	// Should complete much faster than 4*2s = 8s due to context cancellation.
	if elapsed > 2*time.Second {
		t.Errorf("expected fast completion due to cancel, took %v", elapsed)
	}

	// All results should be non-nil (graceful degradation), but with failed status.
	for i, r := range results {
		if r == nil {
			t.Errorf("result[%d] is nil", i)
		}
	}
}

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

	e := newTestEnricher()
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

func TestEnrich_NoRetryFor404(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	e := newTestEnricher()
	result, _ := e.Enrich(context.Background(), Item{Name: "NoRetry", URL: srv.URL})
	if result.Status != fetch.StatusNotFound {
		t.Errorf("expected StatusNotFound, got %q", result.Status)
	}
	if calls.Load() != 1 {
		t.Errorf("404 should not retry, got %d calls", calls.Load())
	}
}

func TestEnrich_OGImage(t *testing.T) {
	t.Parallel()
	html := `<html><head><meta property="og:image" content="https://img.example.com/photo.jpg"></head>
<body><p>Short page</p></body></html>`
	srv := newTestServer(html, http.StatusOK)
	defer srv.Close()

	e := newTestEnricher()
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

	e := newTestEnricher(WithSearch(mock))
	result, err := e.Enrich(context.Background(), Item{Name: "Failing Search"})
	if err != nil {
		t.Fatalf("Enrich should not error on search failure, got: %v", err)
	}
	if result.SearchContext != "" {
		t.Error("expected empty search context on error")
	}
}

func TestEnrich_WithSearchProvider(t *testing.T) {
	t.Parallel()
	provider := &mockProvider{
		result: &search.SearchResult{
			Context: "Source 1: Info about topic",
			Sources: []string{"https://src.com/1"},
		},
	}
	e := newTestEnricher(WithSearch(provider))
	result, err := e.Enrich(context.Background(), Item{
		Name: "Test Search",
		Mode: ModeNews,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if len(result.SearchSources) == 0 {
		t.Error("expected search sources")
	}
}

func TestEnrich_MaxContentLen(t *testing.T) {
	t.Parallel()
	srv := newTestServer(testHTML, http.StatusOK)
	defer srv.Close()

	e := newTestEnricher(WithMaxContentLen(50))
	result, err := e.Enrich(context.Background(), Item{Name: "Truncated", URL: srv.URL})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if result.Content != "" && len([]rune(result.Content)) > 50 {
		t.Errorf("content should be <= 50 runes, got %d", len([]rune(result.Content)))
	}
}

func TestEnrich_WithLogger(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(handler)

	srv := newTestServer(testHTML, http.StatusOK)
	defer srv.Close()

	e := newTestEnricher(WithLogger(logger))
	_, err := e.Enrich(context.Background(), Item{Name: "Logged", URL: srv.URL})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "enriche") {
		t.Errorf("expected log output containing 'enriche', got: %q", output)
	}
}

func TestCacheKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		item Item
		want string
	}{
		{Item{URL: "https://example.com"}, "enriche:v2:https://example.com:seed:none"},
		{Item{Name: "Test"}, "enriche:search:v2:Test:seed:none"},
		{Item{Name: "Place", URL: "https://place.com"}, "enriche:v2:https://place.com:seed:none"},
	}
	for _, tt := range tests {
		got := cacheKey(tt.item)
		if got != tt.want {
			t.Errorf("cacheKey(%+v) = %q, want %q", tt.item, got, tt.want)
		}
	}
}

// --- Source coords seeding tests ---

// mockMapsChecker is a maps.Checker stub returning configurable OrgData coords.
type mockMapsChecker struct {
	lat float64
	lon float64
}

func (m *mockMapsChecker) Check(_ context.Context, _, _, _ string) (*maps.CheckResult, error) {
	return &maps.CheckResult{
		Status: maps.PlaceOpen,
		OrgData: &maps.OrgData{
			Name:      "Some Place",
			Latitude:  m.lat,
			Longitude: m.lon,
		},
	}, nil
}

// TestEnrich_SourceCoords_WinsOverMapsChecker_NoURL covers the no-URL path
// (no fetchAndExtract, no ExtractFacts wholesale assignment).
// Source coords must survive mergeOrgDataToFacts fill-nil-only guard.
func TestEnrich_SourceCoords_WinsOverMapsChecker_NoURL(t *testing.T) {
	t.Parallel()

	srcLat, srcLon := 59.9390, 30.3158   // source-authoritative (e.g. KudaGo)
	mapsLat, mapsLon := 55.7558, 37.6176 // maps-checker returns Moscow coords

	checker := &mockMapsChecker{lat: mapsLat, lon: mapsLon}
	e := newTestEnricher(WithMapsChecker(checker))

	result, err := e.Enrich(context.Background(), Item{
		Name:      "Кафе Singer",
		City:      "Санкт-Петербург",
		Mode:      ModePlaces,
		Latitude:  &srcLat,
		Longitude: &srcLon,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if result.Facts.Latitude == nil {
		t.Fatal("expected Facts.Latitude to be set")
	}
	if *result.Facts.Latitude != srcLat {
		t.Errorf("Latitude = %v, want source %v (maps checker returned %v)",
			*result.Facts.Latitude, srcLat, mapsLat)
	}
	if result.Facts.Longitude == nil {
		t.Fatal("expected Facts.Longitude to be set")
	}
	if *result.Facts.Longitude != srcLon {
		t.Errorf("Longitude = %v, want source %v (maps checker returned %v)",
			*result.Facts.Longitude, srcLon, mapsLon)
	}
}

// TestEnrich_SourceCoords_WinsOverMapsChecker_WithURL covers the URL path where
// fetchAndExtract runs and extract.ExtractFacts overwrites Facts wholesale.
// Source coords must survive the wholesale assignment and win over the maps checker.
func TestEnrich_SourceCoords_WinsOverMapsChecker_WithURL(t *testing.T) {
	t.Parallel()

	srcLat, srcLon := 59.9390, 30.3158   // source-authoritative
	mapsLat, mapsLon := 55.7558, 37.6176 // maps-checker would return these

	srv := newTestServer(testHTML, http.StatusOK)
	defer srv.Close()

	checker := &mockMapsChecker{lat: mapsLat, lon: mapsLon}
	e := newTestEnricher(WithMapsChecker(checker))

	result, err := e.Enrich(context.Background(), Item{
		Name:      "Кафе Singer",
		URL:       srv.URL,
		City:      "Санкт-Петербург",
		Mode:      ModePlaces,
		Latitude:  &srcLat,
		Longitude: &srcLon,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if result.Facts.Latitude == nil {
		t.Fatal("expected Facts.Latitude to be set after fetchAndExtract path")
	}
	if *result.Facts.Latitude != srcLat {
		t.Errorf("Latitude = %v, want source %v (maps checker returned %v, ExtractFacts may have clobbered)",
			*result.Facts.Latitude, srcLat, mapsLat)
	}
	if result.Facts.Longitude == nil {
		t.Fatal("expected Facts.Longitude to be set after fetchAndExtract path")
	}
	if *result.Facts.Longitude != srcLon {
		t.Errorf("Longitude = %v, want source %v", *result.Facts.Longitude, srcLon)
	}
}

// TestEnrich_NoSourceCoords_MapsCheckerFills verifies that when Item carries no
// coords the maps checker still fills them (no regression on existing behaviour).
func TestEnrich_NoSourceCoords_MapsCheckerFills(t *testing.T) {
	t.Parallel()

	mapsLat, mapsLon := 55.7558, 37.6176
	checker := &mockMapsChecker{lat: mapsLat, lon: mapsLon}
	e := newTestEnricher(WithMapsChecker(checker))

	result, err := e.Enrich(context.Background(), Item{
		Name: "Some Cafe",
		City: "Москва",
		Mode: ModePlaces,
		// Latitude and Longitude deliberately absent (nil)
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if result.Facts.Latitude == nil {
		t.Fatal("expected Facts.Latitude from maps checker when item has no coords")
	}
	if *result.Facts.Latitude != mapsLat {
		t.Errorf("Latitude = %v, want maps %v", *result.Facts.Latitude, mapsLat)
	}
}

// TestEnrich_SourceCoords_PairGuard verifies that a lone Latitude with nil Longitude
// is treated as absent (not seeded), so it doesn't produce a half-coord in Facts.
func TestEnrich_SourceCoords_PairGuard(t *testing.T) {
	t.Parallel()

	srcLat := 59.9390
	mapsLat, mapsLon := 55.7558, 37.6176
	checker := &mockMapsChecker{lat: mapsLat, lon: mapsLon}
	e := newTestEnricher(WithMapsChecker(checker))

	result, err := e.Enrich(context.Background(), Item{
		Name:      "Half Coord Place",
		City:      "Санкт-Петербург",
		Mode:      ModePlaces,
		Latitude:  &srcLat, // only lat, lon is nil
		Longitude: nil,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	// Pair guard must treat this as absent → maps checker fills instead.
	if result.Facts.Latitude == nil {
		t.Fatal("expected Facts.Latitude to be filled by maps checker (pair guard rejected lone lat)")
	}
	if *result.Facts.Latitude != mapsLat {
		t.Errorf("Latitude = %v, want maps %v (lone lat should be ignored, maps should fill)",
			*result.Facts.Latitude, mapsLat)
	}
}

// mockMapsCheckerError is a maps.Checker stub that always returns an error.
type mockMapsCheckerError struct{}

func (m *mockMapsCheckerError) Check(_ context.Context, _, _, _ string) (*maps.CheckResult, error) {
	return nil, errors.New("2GIS transient error")
}

// TestEnrich_SourceCoords_PlacesNoURL_MapsError covers the regression: Places mode,
// no URL, maps checker returns a transient error. Before the fix, checkMapsStatus
// early-returned without seeding, so source coords were silently dropped.
func TestEnrich_SourceCoords_PlacesNoURL_MapsError(t *testing.T) {
	t.Parallel()

	srcLat, srcLon := 59.9390, 30.3158
	checker := &mockMapsCheckerError{}
	e := newTestEnricher(WithMapsChecker(checker))

	result, err := e.Enrich(context.Background(), Item{
		Name:      "Кафе на Невском",
		City:      "Санкт-Петербург",
		Mode:      ModePlaces,
		Latitude:  &srcLat,
		Longitude: &srcLon,
		// No URL — fetchAndExtract never runs.
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if result.Facts.Latitude == nil {
		t.Fatal("expected Facts.Latitude to be set; maps error must not drop source coords")
	}
	if *result.Facts.Latitude != srcLat {
		t.Errorf("Latitude = %v, want source %v", *result.Facts.Latitude, srcLat)
	}
	if result.Facts.Longitude == nil {
		t.Fatal("expected Facts.Longitude to be set")
	}
	if *result.Facts.Longitude != srcLon {
		t.Errorf("Longitude = %v, want source %v", *result.Facts.Longitude, srcLon)
	}
}

// TestEnrich_SourceCoords_EventsNoURL covers ModeEvents items (e.g. KudaGo) that
// carry source coords but have no URL. checkMapsStatus is skipped for events entirely
// and fetchAndExtract never runs, so the unconditional up-front seed is the only guard.
func TestEnrich_SourceCoords_EventsNoURL(t *testing.T) {
	t.Parallel()

	srcLat, srcLon := 59.9390, 30.3158
	// maps checker is wired but must not affect events — add it to prove isolation.
	checker := &mockMapsChecker{lat: 55.7558, lon: 37.6176}
	e := newTestEnricher(WithMapsChecker(checker))

	result, err := e.Enrich(context.Background(), Item{
		Name:      "Фестиваль на Дворцовой",
		City:      "Санкт-Петербург",
		Mode:      ModeEvents,
		Latitude:  &srcLat,
		Longitude: &srcLon,
		// No URL — events platform (KudaGo) use case.
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if result.Facts.Latitude == nil {
		t.Fatal("expected Facts.Latitude to survive for ModeEvents no-URL item")
	}
	if *result.Facts.Latitude != srcLat {
		t.Errorf("Latitude = %v, want source %v", *result.Facts.Latitude, srcLat)
	}
	if result.Facts.Longitude == nil {
		t.Fatal("expected Facts.Longitude to survive")
	}
	if *result.Facts.Longitude != srcLon {
		t.Errorf("Longitude = %v, want source %v", *result.Facts.Longitude, srcLon)
	}
}

// --- Maps temporary-closed status tests (F14) ---

// stubMapsChecker is a configurable maps.Checker stub for closure-status tests.
type stubMapsChecker struct {
	res *maps.CheckResult
	err error
}

func (s stubMapsChecker) Check(_ context.Context, _, _, _ string) (*maps.CheckResult, error) {
	return s.res, s.err
}

// TestCheckMapsStatus_TemporaryClosed_MapsToTemporaryStatus verifies that
// PlaceTemporaryClosed short-circuits enrichment with StatusTemporaryClosed
// instead of falling through to fetchAndExtract (which would set StatusActive).
func TestCheckMapsStatus_TemporaryClosed_MapsToTemporaryStatus(t *testing.T) {
	t.Parallel()
	e := newTestEnricher(WithMapsChecker(stubMapsChecker{
		res: &maps.CheckResult{Status: maps.PlaceTemporaryClosed, MapURL: "https://2gis.ru/x"},
	}))
	result, err := e.Enrich(context.Background(), Item{Name: "Закрытое кафе", Mode: ModePlaces, City: "СПб"})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if result.Status != fetch.StatusTemporaryClosed {
		t.Errorf("Status = %q, want %q", result.Status, fetch.StatusTemporaryClosed)
	}
}

// TestCheckMapsStatus_PermanentClosed_StillMapsToClosed verifies that the existing
// PlacePermanentClosed → StatusClosed mapping is not broken by the F14 change.
func TestCheckMapsStatus_PermanentClosed_StillMapsToClosed(t *testing.T) {
	t.Parallel()
	e := newTestEnricher(WithMapsChecker(stubMapsChecker{res: &maps.CheckResult{Status: maps.PlacePermanentClosed}}))
	result, err := e.Enrich(context.Background(), Item{Name: "X", Mode: ModePlaces})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if result.Status != fetch.StatusClosed {
		t.Errorf("Status = %q, want %q", result.Status, fetch.StatusClosed)
	}
}

// TestEnrich_SourceCoords_PairGuard_LonOnly is the reverse of TestEnrich_SourceCoords_PairGuard:
// a lone Longitude with nil Latitude must also be treated as absent so it does not
// produce a half-coord. The maps checker fills instead.
func TestEnrich_SourceCoords_PairGuard_LonOnly(t *testing.T) {
	t.Parallel()

	srcLon := 30.3158
	mapsLat, mapsLon := 55.7558, 37.6176
	checker := &mockMapsChecker{lat: mapsLat, lon: mapsLon}
	e := newTestEnricher(WithMapsChecker(checker))

	result, err := e.Enrich(context.Background(), Item{
		Name:      "Half Coord Place Lon",
		City:      "Санкт-Петербург",
		Mode:      ModePlaces,
		Latitude:  nil, // only lon, lat is nil
		Longitude: &srcLon,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	// Pair guard must treat this as absent → maps checker fills instead.
	if result.Facts.Latitude == nil {
		t.Fatal("expected Facts.Latitude to be filled by maps checker (pair guard rejected lone lon)")
	}
	if *result.Facts.Latitude != mapsLat {
		t.Errorf("Latitude = %v, want maps %v (lone lon should be ignored, maps should fill)",
			*result.Facts.Latitude, mapsLat)
	}
}

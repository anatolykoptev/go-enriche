package enriche

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestMetrics_PhaseTiming proves the OnPhaseTiming hook fires for the always-run
// legs of a URL-bearing Enrich: homepage_fetch (the raw homepage fetch) and
// total (the whole-call wall-clock, deferred at the top of Enrich). Durations
// must be non-negative. Revert either instrumentation call site (the
// e.metrics.phaseTiming lines in enriche_fetch.go / enriche.go) and the matching
// assertion goes RED.
func TestMetrics_PhaseTiming(t *testing.T) {
	t.Parallel()
	srv := newTestServer(testHTML, http.StatusOK)
	defer srv.Close()

	var mu sync.Mutex
	seen := map[string]int{}
	m := &Metrics{
		OnPhaseTiming: func(phase string, seconds float64) {
			if seconds < 0 {
				t.Errorf("phase %q reported negative seconds %v", phase, seconds)
			}
			mu.Lock()
			seen[phase]++
			mu.Unlock()
		},
	}

	e := newTestEnricher(WithFetcher(testFetcher()), WithMetrics(m))
	if _, err := e.Enrich(context.Background(), Item{Name: "P", URL: srv.URL, Mode: ModePlaces}); err != nil {
		t.Fatalf("Enrich: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if seen[PhaseHomepageFetch] == 0 {
		t.Errorf("OnPhaseTiming never fired for %q; seen=%v", PhaseHomepageFetch, seen)
	}
	if seen[PhaseTotal] == 0 {
		t.Errorf("OnPhaseTiming never fired for %q; seen=%v", PhaseTotal, seen)
	}
}

// TestMetrics_PhaseTiming_OxBrowser proves the ox-browser leg emits its own
// phase timing (homepage_ox_browser) when an ox-browser is configured — the leg
// that was previously the only fetch leg with no timing. It runs in parallel
// with homepage_fetch, but Enrich consumes its result before returning, so the
// emission is deterministic by the time Enrich returns. Revert the
// e.metrics.phaseTiming(PhaseHomepageOxBrowser, ...) line in enriche_fetch.go
// and this goes RED.
func TestMetrics_PhaseTiming_OxBrowser(t *testing.T) {
	t.Parallel()
	srv := newTestServer(testHTML, http.StatusOK)
	defer srv.Close()

	// Minimal ox-browser /read endpoint returning a readability result.
	oxSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":"ox readability body text","title":"T"}`))
	}))
	defer oxSrv.Close()

	var mu sync.Mutex
	seen := map[string]int{}
	m := &Metrics{
		OnPhaseTiming: func(phase string, seconds float64) {
			if seconds < 0 {
				t.Errorf("phase %q reported negative seconds %v", phase, seconds)
			}
			mu.Lock()
			seen[phase]++
			mu.Unlock()
		},
	}

	e := newTestEnricher(WithFetcher(testFetcher()), WithMetrics(m), WithOxBrowser(oxSrv.URL))
	if _, err := e.Enrich(context.Background(), Item{Name: "P", URL: srv.URL, Mode: ModePlaces}); err != nil {
		t.Fatalf("Enrich: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if seen[PhaseHomepageOxBrowser] == 0 {
		t.Errorf("OnPhaseTiming never fired for %q; seen=%v", PhaseHomepageOxBrowser, seen)
	}
}

// TestMetrics_PhaseTiming_NilHookSafe guards the nil-safe path: an Enrich with no
// OnPhaseTiming hook (the default for every existing caller) must not panic.
func TestMetrics_PhaseTiming_NilHookSafe(t *testing.T) {
	t.Parallel()
	srv := newTestServer(testHTML, http.StatusOK)
	defer srv.Close()

	e := newTestEnricher(WithFetcher(testFetcher()), WithMetrics(&Metrics{}))
	if _, err := e.Enrich(context.Background(), Item{Name: "P", URL: srv.URL, Mode: ModePlaces}); err != nil {
		t.Fatalf("Enrich: %v", err)
	}
}

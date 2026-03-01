package enriche

import (
	"context"
	"net/http"
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
	e.Enrich(context.Background(), item) //nolint:errcheck
	e.Enrich(context.Background(), item) //nolint:errcheck

	if misses.Load() != 1 {
		t.Errorf("expected 1 miss, got %d", misses.Load())
	}
	if hits.Load() != 1 {
		t.Errorf("expected 1 hit, got %d", hits.Load())
	}
}

func TestMetrics_FetchError(t *testing.T) {
	t.Parallel()
	srv := newTestServer("", http.StatusInternalServerError)
	defer srv.Close()

	var errs atomic.Int32
	m := &Metrics{
		OnFetchError: func() { errs.Add(1) },
	}

	e := New(WithMetrics(m))
	e.Enrich(context.Background(), Item{Name: "Bad", URL: srv.URL}) //nolint:errcheck

	if errs.Load() != 1 {
		t.Errorf("expected 1 fetch error, got %d", errs.Load())
	}
}

func TestMetrics_NilSafe(t *testing.T) {
	t.Parallel()
	e := New(WithMetrics(nil))
	_, err := e.Enrich(context.Background(), Item{Name: "Safe"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

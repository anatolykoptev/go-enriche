package enriche

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/anatolykoptev/go-enriche/cache"
	"github.com/anatolykoptev/go-enriche/maps"
)

// TestEnrich_Cache_SeedNotBypassed locks the cache/seed interaction: a value
// cached by an UNSEEDED enrich must NOT be served to a later SEEDED enrich. The
// seed is part of the cache identity (cacheKey folds a seed fingerprint), so a
// fresh operator seed misses the stale blob and re-resolves with the operator
// value at the top. Without this, a blob cached before the operator verified a
// value would silently bypass the override-precedence guarantee.
func TestEnrich_Cache_SeedNotBypassed(t *testing.T) {
	t.Parallel()

	html := goldenFixture(t, "royal-wed.html")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(html)) //nolint:errcheck
	}))
	defer srv.Close()

	const mapsPhone = "+7 (812) 956-18-40"
	const operatorPhone = "+7 (921) 956-18-40"
	checker := stubMapsChecker{res: &maps.CheckResult{
		Status:  maps.PlaceOpen,
		OrgData: &maps.OrgData{Name: "Royal Wedding", Phone: mapsPhone},
	}}

	mem := cache.NewMemory()
	e := newTestEnricher(
		WithFetcher(testFetcher()),
		WithMapsChecker(checker),
		WithCache(mem),
		WithCacheTTL(time.Hour),
	)

	base := Item{Name: "Royal Wedding", URL: srv.URL, City: spbCity, Mode: ModePlaces}

	// 1) Unseeded enrich populates the cache with the enrich-derived phone.
	r1, err := e.Enrich(context.Background(), base)
	if err != nil {
		t.Fatalf("unseeded Enrich: %v", err)
	}
	unseeded := derefStr(r1.Facts.Phone)

	// 2) Seeded re-enrich MUST NOT serve the cached unseeded blob — the operator
	//    value wins.
	seeded := base
	seeded.Seed = SeedFacts{Phone: operatorPhone}
	r2, err := e.Enrich(context.Background(), seeded)
	if err != nil {
		t.Fatalf("seeded Enrich: %v", err)
	}
	if got := derefStr(r2.Facts.Phone); got != operatorPhone {
		t.Errorf("seeded re-enrich served stale/non-operator phone %q (unseeded was %q); want operator %q",
			got, unseeded, operatorPhone)
	}
	if r2.Provenance.Phone.Source != "operator_verified" {
		t.Errorf("seeded provenance = %q, want operator_verified", r2.Provenance.Phone.Source)
	}

	// 3) A second seeded call with the SAME seed SHOULD hit the cache (same key).
	counts := 0
	memCounting := newTestEnricher(
		WithFetcher(testFetcher()),
		WithMapsChecker(checker),
		WithCache(mem),
		WithCacheTTL(time.Hour),
		WithMetrics(&Metrics{OnCacheHit: func() { counts++ }}),
	)
	if _, err := memCounting.Enrich(context.Background(), seeded); err != nil {
		t.Fatalf("repeat seeded Enrich: %v", err)
	}
	if counts != 1 {
		t.Errorf("same-seed re-enrich cache hits = %d, want 1 (seed key must be stable)", counts)
	}
}

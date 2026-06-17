package enriche

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/anatolykoptev/go-enriche/fetch"
	"github.com/anatolykoptev/go-enriche/maps"
	"github.com/anatolykoptev/go-enriche/search"
)

// Orchestration-level golden regression set (Phase 2). The extract-layer golden
// set (extract/golden_test.go) proves ExtractFactsForCity returns the right
// phone from raw HTML. THIS set proves the enriche.Enrich ORCHESTRATION keeps
// that value end-to-end when a maps card disagrees — the gap that let the live
// Royal Wedding case ship the maps (812) instead of the social-link +7 921.
//
// Each case wires a real fetcher against an httptest server serving a golden
// fixture, plus a stubMapsChecker returning a CONFLICTING maps card, then
// asserts the resolver's official_site > maps precedence holds.

const spbCity = "Санкт-Петербург"

// goldenFixture reads a fixture from the extract package's testdata so the two
// golden sets share one source of truth for the HTML.
func goldenFixture(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("extract", "testdata", "golden", name)
	b, err := os.ReadFile(path) //nolint:gosec // test-only fixed path
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(b)
}

// countingMetrics captures the Phase-2 telemetry callbacks for assertions.
type countingMetrics struct {
	phoneSources map[string]int
	siteResolved int
	conflicts    map[string]int
}

func newCountingMetrics() (*countingMetrics, *Metrics) {
	c := &countingMetrics{
		phoneSources: map[string]int{},
		conflicts:    map[string]int{},
	}
	m := &Metrics{
		OnPhoneSource:  func(s string) { c.phoneSources[s]++ },
		OnSiteResolved: func() { c.siteResolved++ },
		OnConflict:     func(f string) { c.conflicts[f]++ },
	}
	return c, m
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// searchResult builds a search.SearchResult with the given context + source URLs.
func searchResult(ctx string, sources []string) search.SearchResult {
	return search.SearchResult{Context: ctx, Sources: sources}
}

// TestEnrich_Orchestration_SiteBeatsMapsPhone is the HEADLINE Phase-2 guard:
// the Royal Wedding class. maps returns the (812) card; the official site's
// own HTML carries the DNI-immune social-link +7 921 956-18-40. The resolver
// MUST return the social-link number end-to-end (not the maps 812), fire
// enrich_conflict_total{phone} (site overrode a present, differing maps phone),
// and attribute the phone to source=official_site.
func TestEnrich_Orchestration_SiteBeatsMapsPhone(t *testing.T) {
	t.Parallel()

	html := goldenFixture(t, "royal-wed.html")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(html)) //nolint:errcheck
	}))
	defer srv.Close()

	// maps card disagrees with the site: it carries the rotating (812) proxy.
	const mapsPhone = "+7 (812) 956-18-40"
	checker := stubMapsChecker{res: &maps.CheckResult{
		Status:  maps.PlaceOpen,
		OrgData: &maps.OrgData{Name: "Royal Wedding", Phone: mapsPhone},
	}}
	counts, metrics := newCountingMetrics()

	e := New(WithFetcher(fetch.NewFetcher()), WithMapsChecker(checker), WithMetrics(metrics))
	result, err := e.Enrich(context.Background(), Item{
		Name: "Royal Wedding", URL: srv.URL, City: spbCity, Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}

	const wantPhone = "+79219561840" // DNI-immune social link, the live-article value
	got := derefStr(result.Facts.Phone)
	if got != wantPhone {
		t.Errorf("phone = %q, want social-link %q (must NOT be maps %q)", got, wantPhone, mapsPhone)
	}
	if got == mapsPhone {
		t.Errorf("phone = maps card %q — official site did NOT win (the live-prod bug)", mapsPhone)
	}
	if counts.phoneSources["official_site"] != 1 {
		t.Errorf("phone_source official_site = %d, want 1 (sources=%v)", counts.phoneSources["official_site"], counts.phoneSources)
	}
	if counts.conflicts["phone"] != 1 {
		t.Errorf("conflict{phone} = %d, want 1 (site overrode maps phone)", counts.conflicts["phone"])
	}
	if counts.siteResolved != 1 {
		t.Errorf("site_resolved = %d, want 1", counts.siteResolved)
	}
}

// TestEnrich_Orchestration_IgoraNoRegression guards the clean non-DNI 812 case:
// site tel: is local 812 615-70-00, maps card carries an 8-800 call-tracking
// line. Site wins; phone attributed to official_site.
func TestEnrich_Orchestration_IgoraNoRegression(t *testing.T) {
	t.Parallel()

	html := goldenFixture(t, "igora-drive.html")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(html)) //nolint:errcheck
	}))
	defer srv.Close()

	const mapsPhone = "+7 (800) 555-35-35" // call-tracking 8-800 on the maps card
	checker := stubMapsChecker{res: &maps.CheckResult{
		Status:  maps.PlaceOpen,
		OrgData: &maps.OrgData{Name: "Игора Драйв", Phone: mapsPhone},
	}}
	counts, metrics := newCountingMetrics()

	e := New(WithFetcher(fetch.NewFetcher()), WithMapsChecker(checker), WithMetrics(metrics))
	result, err := e.Enrich(context.Background(), Item{
		Name: "Игора Драйв", URL: srv.URL, City: spbCity, Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}

	got := derefStr(result.Facts.Phone)
	if got == mapsPhone {
		t.Errorf("phone = maps 8-800 %q — site 812 did not win", mapsPhone)
	}
	if got == "" {
		t.Fatal("phone empty — site extraction lost")
	}
	if counts.phoneSources["official_site"] != 1 {
		t.Errorf("phone_source official_site = %d, want 1", counts.phoneSources["official_site"])
	}
}

// TestEnrich_Orchestration_FalseClosed_SiteRefutes is the Карт-Ленд class: maps
// flags the venue permanently closed (wrong card), but the official site is
// reachable and active. The live site MUST refute the closed verdict
// (Status=Active) AND its contact facts must still be collected — the closed
// short-circuit no longer bails before the site is fetched.
func TestEnrich_Orchestration_FalseClosed_SiteRefutes(t *testing.T) {
	t.Parallel()

	html := goldenFixture(t, "igora-drive.html")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(html)) //nolint:errcheck
	}))
	defer srv.Close()

	checker := stubMapsChecker{res: &maps.CheckResult{
		Status:  maps.PlacePermanentClosed, // WRONG card flags it closed
		MapURL:  "https://2gis.ru/wrong-card",
		OrgData: &maps.OrgData{Name: "Карт-Ленд", Phone: "+7 (800) 000-00-00"},
	}}

	e := New(WithFetcher(fetch.NewFetcher()), WithMapsChecker(checker))
	result, err := e.Enrich(context.Background(), Item{
		Name: "Карт-Ленд", URL: srv.URL, City: spbCity, Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}

	if result.Status == fetch.StatusClosed {
		t.Errorf("Status = closed — live official site did NOT refute the stale maps closed-status")
	}
	if result.Status != fetch.StatusActive {
		t.Errorf("Status = %q, want active (live site refutes false-closed)", result.Status)
	}
	if derefStr(result.Facts.Phone) == "" {
		t.Error("phone empty — closed short-circuit bailed before the site was fetched (the false-closed bug)")
	}
}

// TestEnrich_Orchestration_ClosedNoSite_MapsStands is the inverse guard: when
// maps reports closed AND there is no reachable official site (no URL), the
// maps closed-status must STAND — the site cannot refute what it cannot prove.
// Wires a search provider (the production go-wp config ALWAYS has one) to ensure
// the doSearch/fetchSearchSources path does not overturn the closed verdict.
func TestEnrich_Orchestration_ClosedNoSite_MapsStands(t *testing.T) {
	t.Parallel()

	checker := stubMapsChecker{res: &maps.CheckResult{
		Status: maps.PlacePermanentClosed,
		MapURL: "https://2gis.ru/x",
	}}
	// Search provider present but returning no fetchable sources.
	sr := searchResult("закрытое кафе", nil)
	prov := &mockProvider{result: &sr}
	e := New(WithMapsChecker(checker), WithSearch(prov))
	result, err := e.Enrich(context.Background(), Item{
		Name: "Закрытое навсегда", City: spbCity, Mode: ModePlaces, // no URL
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if result.Status != fetch.StatusClosed {
		t.Errorf("Status = %q, want closed (no site to refute)", result.Status)
	}
}

// TestEnrich_Orchestration_ClosedNoSite_SearchDoesNotResurrect is the regression
// guard for the search-fallback resurrection class (caught in PR #14 review):
// maps reports the venue closed, there is no official site, AND a search
// provider returns a fetchable third-party page (a stale aggregator listing).
// A search-discovered page is NOT authority to refute a maps closed-status, so
// the venue must STAY closed — it must not be resurrected to StatusActive by the
// search-source content fetch.
func TestEnrich_Orchestration_ClosedNoSite_SearchDoesNotResurrect(t *testing.T) {
	t.Parallel()

	// A live third-party page (e.g. a stale 2gis/zoon listing) that fetches 200.
	thirdParty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`<!DOCTYPE html><html><body><article>` + //nolint:errcheck
			`<h1>Кафе на Невском</h1><p>Описание заведения из стороннего каталога, ` +
			`достаточно длинное чтобы пройти порог извлечения текста трафилатурой. ` +
			`Этот листинг устарел и не отражает реальный статус заведения.</p>` +
			`</article></body></html>`))
	}))
	defer thirdParty.Close()

	checker := stubMapsChecker{res: &maps.CheckResult{
		Status: maps.PlacePermanentClosed,
		MapURL: "https://2gis.ru/closed-card",
	}}
	// Search returns the third-party URL as a fetchable source.
	sr := searchResult("кафе на невском отзывы", []string{thirdParty.URL})
	prov := &mockProvider{result: &sr}

	e := New(WithFetcher(fetch.NewFetcher()), WithMapsChecker(checker), WithSearch(prov))
	result, err := e.Enrich(context.Background(), Item{
		Name: "Кафе на Невском", City: spbCity, Mode: ModePlaces, // no URL
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if result.Status == fetch.StatusActive {
		t.Errorf("Status = active — a stale search-discovered page resurrected a maps-closed venue (the PR-review MAJOR)")
	}
	if result.Status != fetch.StatusClosed {
		t.Errorf("Status = %q, want closed (search page is not authority to refute closed)", result.Status)
	}
}

// TestEnrich_Orchestration_ClosedDeadSite_MapsStands: maps says closed and the
// official site is unreachable (server down). The maps closed-status stands —
// only a reachable, ACTIVE site refutes.
func TestEnrich_Orchestration_ClosedDeadSite_MapsStands(t *testing.T) {
	t.Parallel()

	// Server that immediately closes connections → unreachable fetch.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := srv.URL
	srv.Close() // close so the URL is now unreachable

	checker := stubMapsChecker{res: &maps.CheckResult{
		Status: maps.PlaceTemporaryClosed,
		MapURL: "https://2gis.ru/x",
	}}
	e := New(WithFetcher(fetch.NewFetcher()), WithMapsChecker(checker))
	result, err := e.Enrich(context.Background(), Item{
		Name: "Временно закрыто", URL: deadURL, City: spbCity, Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if result.Status != fetch.StatusTemporaryClosed {
		t.Errorf("Status = %q, want temporary_closed (dead site cannot refute)", result.Status)
	}
}

// TestEnrich_Orchestration_MapsFillsGap proves graceful degradation: the
// resolver lets a LOWER-priority maps value FILL a field the official site left
// empty, while the site still wins the field it does provide. Here the site has
// a phone but no address; maps supplies the address.
func TestEnrich_Orchestration_MapsFillsGap(t *testing.T) {
	t.Parallel()

	// Minimal site: a contacts-region tel: but no address markup.
	siteHTML := `<!DOCTYPE html><html><body>
<header><a href="tel:+78121234567">+7 (812) 123-45-67</a></header>
<p>Описание площадки без адреса в разметке.</p></body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(siteHTML)) //nolint:errcheck
	}))
	defer srv.Close()

	const mapsAddr = "Санкт-Петербург, Невский проспект, 28"
	const mapsPhone = "+7 (800) 555-35-35"
	checker := stubMapsChecker{res: &maps.CheckResult{
		Status:  maps.PlaceOpen,
		OrgData: &maps.OrgData{Name: "Площадка", Address: mapsAddr, Phone: mapsPhone},
	}}

	e := New(WithFetcher(fetch.NewFetcher()), WithMapsChecker(checker))
	result, err := e.Enrich(context.Background(), Item{
		Name: "Площадка", URL: srv.URL, City: spbCity, Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}

	// Phone: site wins.
	if got := derefStr(result.Facts.Phone); got == mapsPhone {
		t.Errorf("phone = maps %q — site phone should have won", mapsPhone)
	}
	// Address: site had none → maps fills the gap.
	if got := derefStr(result.Facts.Address); got != mapsAddr {
		t.Errorf("address = %q, want maps fallback %q (site left it empty)", got, mapsAddr)
	}
}

// TestResolver_OrderIndependence is the source-priority invariant (Fitness #2):
// the resolved winner depends only on source priority, never on the order the
// sources were merged. Merging maps-then-site and site-then-maps must yield the
// same winner (the site value).
func TestResolver_OrderIndependence(t *testing.T) {
	t.Parallel()

	const sitePhone = "+79219561840"
	const mapsPhone = "+7 (812) 956-18-40"

	build := func() (*Facts, *resolver, *countingMetrics) {
		f := &Facts{}
		c, m := newCountingMetrics()
		return f, &resolver{facts: f, prov: &factProvenance{}, m: m}, c
	}

	// Order A: maps first, then site (the production order).
	fA, rA, cA := build()
	rA.set(&fA.Phone, &rA.prov.phone, mapsPhone, sourceMaps, "phone")
	rA.set(&fA.Phone, &rA.prov.phone, sitePhone, sourceOfficialSite, "phone")

	// Order B: site first, then maps (reverse).
	fB, rB, cB := build()
	rB.set(&fB.Phone, &rB.prov.phone, sitePhone, sourceOfficialSite, "phone")
	rB.set(&fB.Phone, &rB.prov.phone, mapsPhone, sourceMaps, "phone")

	// Resolved VALUE is order-independent.
	if derefStr(fA.Phone) != sitePhone || derefStr(fB.Phone) != sitePhone {
		t.Errorf("order-dependence: A=%q B=%q, both want %q", derefStr(fA.Phone), derefStr(fB.Phone), sitePhone)
	}
	if rA.prov.phone != sourceOfficialSite || rB.prov.phone != sourceOfficialSite {
		t.Errorf("owner not official_site: A=%v B=%v", rA.prov.phone, rB.prov.phone)
	}
	// Conflict COUNT is also order-independent: both orders adjudicated exactly
	// one site-vs-maps phone conflict (PR-review LOW finding).
	if cA.conflicts["phone"] != 1 || cB.conflicts["phone"] != 1 {
		t.Errorf("conflict count order-dependent: A=%d B=%d, both want 1", cA.conflicts["phone"], cB.conflicts["phone"])
	}
}

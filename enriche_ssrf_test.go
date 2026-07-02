package enriche

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anatolykoptev/go-enriche/fetch"
	"github.com/anatolykoptev/go-enriche/search"
)

// These tests exercise the 4 call sites where a fetched URL is handed to an
// EXTERNAL render/extraction delegate (oxBrowser.Extract, browserFetch) that
// fetch.Fetcher's dial-time guard cannot protect — see checkTarget in
// enriche.go. Unlike every other test in this package, they deliberately do
// NOT use testFetcher()/newTestEnricher()/allowAllTargets: the REAL default
// guards (fetch.NewFetcher(), built on go-kit httputil.NewSSRFGuardedClient, and
// httputil.CheckRawURL, both wired by New())
// are exactly what's under test here.
//
// The complete render-delegate call-site set (verified by grepping every
// e.browserFetch( and e.oxBrowser.Extract( call site in the non-test,
// non-vendor tree — see the PR description for the grep output):
//
//  1. enriche_fetch.go:        oxBrowser.Extract(item.URL)   — TestFetchAndExtract_OxBrowser_BlocksInternalItemURL
//  2. enriche_search_fetch.go: oxBrowser.Extract(srcURL)     — TestFetchOneSource_OxBrowser_BlocksInternalSearchSourceURL
//  3. enriche_contacts.go:     browserFetch(contactsURL)     — TestFetchContactsHTML_BlocksInternalRenderTarget
//  4. enriche_fetch.go:        browserFetch(item.URL)        — TestEnrich_HomepageRender_BlocksInternalTarget

// TestFetchAndExtract_OxBrowser_BlocksInternalItemURL covers call site #1
// (enriche_fetch.go): the ox-browser leg fires in a goroutine BEFORE the raw
// fetch's own guard has a chance to short-circuit the function, so it needs
// its own checkTarget gate. item.URL is an address the default guard must
// refuse (see go-kit httputil.IsBlockedIP); the fake ox-browser server must
// never see a request.
func TestFetchAndExtract_OxBrowser_BlocksInternalItemURL(t *testing.T) {
	t.Parallel()
	var oxHits atomic.Int32
	oxSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		oxHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"title":"t","content":"should never be reached"}`))
	}))
	defer oxSrv.Close()

	e := New(WithOxBrowser(oxSrv.URL)) // real default fetcher + real default targetGuard

	result, err := e.Enrich(context.Background(), Item{
		Name: "Internal Target", URL: "http://169.254.169.254/place", Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if got := oxHits.Load(); got != 0 {
		t.Fatalf("ox-browser server hit %d times for a blocked internal target — checkTarget failed to gate oxBrowser.Extract(item.URL)", got)
	}
	if result.Content != "" {
		t.Errorf("expected no content from a blocked target, got %q", result.Content)
	}
}

// TestFetchOneSource_OxBrowser_BlocksInternalSearchSourceURL covers call site
// #2 (enriche_search_fetch.go): a search-discovered source URL is likewise
// fired at ox-browser in an ungated goroutine.
func TestFetchOneSource_OxBrowser_BlocksInternalSearchSourceURL(t *testing.T) {
	t.Parallel()
	var oxHits atomic.Int32
	oxSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		oxHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"title":"t","content":"should never be reached"}`))
	}))
	defer oxSrv.Close()

	mock := &mockProvider{
		result: &search.SearchResult{
			Context: "found via search",
			Sources: []string{"http://169.254.169.254/src"},
		},
	}
	e := New(WithSearch(mock), WithOxBrowser(oxSrv.URL))

	_, err := e.Enrich(context.Background(), Item{Name: "Search Only", Mode: ModeNews})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if got := oxHits.Load(); got != 0 {
		t.Fatalf("ox-browser server hit %d times for a blocked internal search-source target — checkTarget failed to gate oxBrowser.Extract(srcURL)", got)
	}
}

// TestFetchContactsHTML_BlocksInternalRenderTarget covers call site #3
// (enriche_contacts.go): fetchContactsHTML's browserFetch call is NOT gated
// on its own raw-fetch outcome (a contactless raw fetch — including one
// refused by fetch.Fetcher's own guard — still falls through to a render
// attempt), so it needs the same explicit checkTarget gate. Called directly
// (bypassing the same-origin contacts-page discovery in resolveContactsPage)
// to unit-test the guard at this call site independent of how contactsURL
// was obtained.
func TestFetchContactsHTML_BlocksInternalRenderTarget(t *testing.T) {
	t.Parallel()
	var rendered atomic.Bool
	e := New(WithBrowserFetch(func(_ context.Context, _ string) (string, error) {
		rendered.Store(true)
		return renderedWithContacts, nil // would succeed if the guard failed to block
	}))

	html, poisoned := e.fetchContactsHTML(context.Background(), "http://169.254.169.254/contacts", Item{City: "Санкт-Петербург"})
	if rendered.Load() {
		t.Fatal("browserFetch fired for an internal contactsURL target — checkTarget failed to gate it")
	}
	if html != "" || poisoned {
		t.Errorf("expected degraded empty result for a blocked target, got html=%q poisoned=%v", html, poisoned)
	}
}

// TestEnrich_HomepageRender_BlocksInternalTarget covers call site #4
// (enriche_fetch.go): browserFetch(item.URL) fires for the homepage render
// (the thin-content/absent-contacts trigger — common for SPA/Tilda/Bitrix/
// React venue homepages, so attacker-craftable, not a corner case) and is
// reached WITHOUT any guarantee from Guard A: Guard A only protects the RAW
// fetch when e.fetcher happens to be the guarded default, but WithFetcher /
// WithStealth let a caller supply an unguarded client (a real, common
// configuration — go-wp's WithStealth call is exactly this shape). Once the
// raw fetch of an internal item.URL "succeeds" via an unguarded fetcher, the
// render trigger fires just like it would for any other thin page.
//
// Uses WithFetcher(testFetcher()) (deliberately unguarded, simulating
// WithStealth/any custom fetcher) WITHOUT newTestEnricher() (which would
// also set WithTargetGuard(allowAllTargets) and mask the bug): the REAL
// default targetGuard (httputil.CheckRawURL, go-kit) is exactly what's under test.
func TestEnrich_HomepageRender_BlocksInternalTarget(t *testing.T) {
	t.Parallel()
	srv := newTestServer(`<html><body><div>x</div></body></html>`, http.StatusOK) // thin: triggers the render
	defer srv.Close()

	var rendered atomic.Bool
	e := New(
		WithFetcher(testFetcher()), // unguarded raw fetch — simulates WithStealth/any custom fetcher
		WithBrowserFetch(func(_ context.Context, _ string) (string, error) {
			rendered.Store(true)
			return renderedWithContacts, nil // would succeed if the guard failed to block
		}),
	)

	_, err := e.Enrich(context.Background(), Item{
		Name: "Loopback Homepage", URL: srv.URL, City: "Санкт-Петербург", Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if rendered.Load() {
		t.Fatal("browserFetch fired for item.URL pointing at a loopback target — checkTarget failed to gate the homepage render")
	}
}

// TestWithStealth_GuardsAgainstInternalTarget is the integration-level proof
// for WithStealth (options.go): fetch.WithClient normally replaces the
// Fetcher's client wholesale, which would otherwise bypass Guard A entirely
// for any stealth-configured Enricher (go-wp's production pattern — see
// internal/wptools/content/enrich.go's enriche.WithStealth(d.stealthClient)
// call). WithStealth must route the caller's client through
// httputil.NewSSRFGuardedClient (go-kit; see its own TestNewSSRFGuardedClient_*
// for the composition-mechanism proof) rather than passing it through raw.
//
// A bare *http.Client stands in for the stealth client here (the
// *http.Transport/nil-Transport branch — the custom-RoundTripper branch,
// which is what go-stealth's actual client shape hits, is proven directly in
// go-kit's own httputil/ssrf_test.go). The point under test is END-TO-END: that Enrich()
// never reaches the loopback test server through a WithStealth-built
// Enricher.
func TestWithStealth_GuardsAgainstInternalTarget(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("should never be reached"))
	}))
	defer srv.Close()

	stealthClient := &http.Client{Timeout: 5 * time.Second}
	e := New(WithStealth(stealthClient))

	result, err := e.Enrich(context.Background(), Item{
		Name: "Loopback via stealth", URL: srv.URL, Mode: ModeNews,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if got := hits.Load(); got != 0 {
		t.Fatalf("loopback test server hit %d times through a WithStealth-configured fetcher — Guard A was bypassed", got)
	}
	if result.Status != fetch.StatusUnreachable {
		t.Errorf("expected the loopback target refused as unreachable, got %s", result.Status)
	}
}

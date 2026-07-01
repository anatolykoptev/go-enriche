package enriche

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/anatolykoptev/go-enriche/search"
)

// These tests exercise the 3 call sites where a fetched URL is handed to an
// EXTERNAL render/extraction delegate (oxBrowser.Extract, browserFetch) that
// fetch.Fetcher's dial-time guard cannot protect — see checkTarget in
// enriche.go. Unlike every other test in this package, they deliberately do
// NOT use testFetcher()/newTestEnricher()/allowAllTargets: the REAL default
// guards (fetch.NewFetcher() and fetch.CheckSSRFSafe, both wired by New())
// are exactly what's under test here.

// TestFetchAndExtract_OxBrowser_BlocksInternalItemURL covers call site #1
// (enriche_fetch.go): the ox-browser leg fires in a goroutine BEFORE the raw
// fetch's own guard has a chance to short-circuit the function, so it needs
// its own checkTarget gate. item.URL is an address the default guard must
// refuse (see fetch/ssrf.go's isBlockedIP); the fake ox-browser server must
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

package maps

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mustJSONStr marshals a string to a JSON string literal (including quotes).
func mustJSONStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// oxDDGHTML is a minimal DDG HTML structure that websearch.ParseDDGHTML parses.
const oxDDGHTML = `<html><body>
	<div class="result">
		<a class="result__a" href="https://example.com/page1">Example Page</a>
		<span class="result__snippet">A useful snippet about the topic.</span>
	</div>
</body></html>`

func TestOxBrowserOrgFetcher_SendsInternalSecret(t *testing.T) {
	t.Setenv("INTERNAL_SERVICE_SECRET", "s3cret")

	var gotSecret string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSecret = r.Header.Get("X-Internal-Secret")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"body":"<html>org</html>","status":200}`))
	}))
	defer srv.Close()

	fetch := OxBrowserOrgFetcher(srv.URL)
	if _, err := fetch(context.Background(), "https://yandex.ru/maps/org/x"); err != nil {
		t.Fatalf("OrgFetcher: %v", err)
	}
	if gotSecret != "s3cret" {
		t.Errorf("X-Internal-Secret = %q, want %q", gotSecret, "s3cret")
	}
}

func TestOxBrowserSearch_SendsInternalSecret(t *testing.T) {
	t.Setenv("INTERNAL_SERVICE_SECRET", "s3cret")

	var gotSecret string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSecret = r.Header.Get("X-Internal-Secret")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"body":` + mustJSONStr(oxDDGHTML) + `,"status":200}`))
	}))
	defer srv.Close()

	search := OxBrowserSearch(srv.URL)
	results, err := search(context.Background(), "test query")
	if err != nil {
		t.Fatalf("OxBrowserSearch: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	if gotSecret != "s3cret" {
		t.Errorf("X-Internal-Secret = %q, want %q", gotSecret, "s3cret")
	}
}

func TestYandexMapsSearch_SendsInternalSecret(t *testing.T) {
	t.Setenv("INTERNAL_SERVICE_SECRET", "s3cret")

	var gotSecret string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSecret = r.Header.Get("X-Internal-Secret")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"body":"<a href=\"https://yandex.ru/maps/org/foo/123\">x</a>","status":200}`))
	}))
	defer srv.Close()

	search := YandexMapsSearch(srv.URL)
	results, err := search(context.Background(), "test query")
	if err != nil {
		t.Fatalf("YandexMapsSearch: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	if gotSecret != "s3cret" {
		t.Errorf("X-Internal-Secret = %q, want %q", gotSecret, "s3cret")
	}
}

// A redirect off the ox-browser origin must not carry the secret to the
// second origin.
func TestOxBrowserOrgFetcher_RedirectStripsSecret(t *testing.T) {
	t.Setenv("INTERNAL_SERVICE_SECRET", "s3cret")

	var secondSecret string
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondSecret = r.Header.Get("X-Internal-Secret")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"body":"<html>org</html>","status":200}`))
	}))
	defer second.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, second.URL+"/fetch", http.StatusFound)
	}))
	defer srv.Close()

	fetch := OxBrowserOrgFetcher(srv.URL)
	if _, err := fetch(context.Background(), "https://yandex.ru/maps/org/x"); err != nil {
		t.Fatalf("OrgFetcher: %v", err)
	}
	if secondSecret != "" {
		t.Errorf("X-Internal-Secret leaked to second origin: %q", secondSecret)
	}
}

// With INTERNAL_SERVICE_SECRET empty no header is sent and the request is
// still made.
func TestOxBrowserOrgFetcher_NoSecretNoHeader(t *testing.T) {
	t.Setenv("INTERNAL_SERVICE_SECRET", "")

	var gotSecret string
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		gotSecret = r.Header.Get("X-Internal-Secret")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"body":"<html>org</html>","status":200}`))
	}))
	defer srv.Close()

	fetch := OxBrowserOrgFetcher(srv.URL)
	if _, err := fetch(context.Background(), "https://yandex.ru/maps/org/x"); err != nil {
		t.Fatalf("OrgFetcher: %v", err)
	}
	if !called {
		t.Fatal("server was not called")
	}
	if gotSecret != "" {
		t.Errorf("X-Internal-Secret = %q, want empty", gotSecret)
	}
}

package search

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mustJSON marshals a string to a JSON string literal (including quotes).
func mustJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// minimalDDGHTML is a minimal DDG HTML structure that ParseDDGHTML can parse.
const minimalDDGHTML = `<html><body>
	<div class="result">
		<a class="result__a" href="https://example.com/page1">Example Page</a>
		<span class="result__snippet">A useful snippet about the topic.</span>
	</div>
	<div class="result">
		<a class="result__a" href="https://other.org/article">Other Article</a>
		<span class="result__snippet">Another relevant snippet.</span>
	</div>
</body></html>`

func TestOxBrowser_Search(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/fetch" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"body":` + mustJSON(minimalDDGHTML) + `,"status":200}`))
	}))
	defer srv.Close()

	p := NewOxBrowser(srv.URL)
	result, err := p.Search(t.Context(), "golang testing", "")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Sources) == 0 {
		t.Fatal("expected at least one source, got none")
	}
	if result.Context == "" {
		t.Fatal("expected non-empty context")
	}
	// Verify first source URL is correctly parsed.
	if result.Sources[0] != "https://example.com/page1" {
		t.Errorf("Sources[0] = %q, want https://example.com/page1", result.Sources[0])
	}
}

func TestOxBrowser_FetchError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := NewOxBrowser(srv.URL)
	_, err := p.Search(t.Context(), "golang testing", "")
	if err == nil {
		t.Fatal("expected error on HTTP 500, got nil")
	}
}

func TestOxBrowser_OxBrowserError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"body":"","status":403,"error":"blocked by Cloudflare"}`))
	}))
	defer srv.Close()

	p := NewOxBrowser(srv.URL)
	_, err := p.Search(t.Context(), "golang testing", "")
	if err == nil {
		t.Fatal("expected error when ox-browser returns error field, got nil")
	}
}

// With INTERNAL_SERVICE_SECRET set, requests to the configured ox-browser
// base URL carry X-Internal-Secret.
func TestOxBrowser_SendsInternalSecret(t *testing.T) {
	t.Setenv("INTERNAL_SERVICE_SECRET", "s3cret")

	var gotSecret string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSecret = r.Header.Get("X-Internal-Secret")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"body":` + mustJSON(minimalDDGHTML) + `,"status":200}`))
	}))
	defer srv.Close()

	p := NewOxBrowser(srv.URL)
	if _, err := p.Search(t.Context(), "golang testing", ""); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if gotSecret != "s3cret" {
		t.Errorf("X-Internal-Secret = %q, want %q", gotSecret, "s3cret")
	}
}

// A redirect off the ox-browser origin must not carry the secret to the
// second origin.
func TestOxBrowser_RedirectStripsSecret(t *testing.T) {
	t.Setenv("INTERNAL_SERVICE_SECRET", "s3cret")

	var secondSecret string
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondSecret = r.Header.Get("X-Internal-Secret")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"body":` + mustJSON(minimalDDGHTML) + `,"status":200}`))
	}))
	defer second.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, second.URL+"/fetch", http.StatusFound)
	}))
	defer srv.Close()

	p := NewOxBrowser(srv.URL)
	if _, err := p.Search(t.Context(), "golang testing", ""); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if secondSecret != "" {
		t.Errorf("X-Internal-Secret leaked to second origin: %q", secondSecret)
	}
}

// With INTERNAL_SERVICE_SECRET empty no header is sent and the request is
// still made.
func TestOxBrowser_NoSecretNoHeader(t *testing.T) {
	t.Setenv("INTERNAL_SERVICE_SECRET", "")

	var gotSecret string
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		gotSecret = r.Header.Get("X-Internal-Secret")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"body":` + mustJSON(minimalDDGHTML) + `,"status":200}`))
	}))
	defer srv.Close()

	p := NewOxBrowser(srv.URL)
	if _, err := p.Search(t.Context(), "golang testing", ""); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !called {
		t.Fatal("server was not called")
	}
	if gotSecret != "" {
		t.Errorf("X-Internal-Secret = %q, want empty", gotSecret)
	}
}

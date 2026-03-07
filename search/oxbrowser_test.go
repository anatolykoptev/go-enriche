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
		_, _ = w.Write([]byte(`{"content":` + mustJSON(minimalDDGHTML) + `,"status_code":200}`))
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
		_, _ = w.Write([]byte(`{"content":"","status_code":403,"error":"blocked by Cloudflare"}`))
	}))
	defer srv.Close()

	p := NewOxBrowser(srv.URL)
	_, err := p.Search(t.Context(), "golang testing", "")
	if err == nil {
		t.Fatal("expected error when ox-browser returns error field, got nil")
	}
}

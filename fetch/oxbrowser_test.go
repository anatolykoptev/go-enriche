package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOxBrowserExtract(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/read" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"title":"Test Article","content":"Hello world content here","author":"John","excerpt":"A test","length":25,"elapsed_ms":42}`))
	}))
	defer srv.Close()

	client := NewOxBrowserClient(srv.URL)
	result, err := client.Extract(context.Background(), "https://example.com/article")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Title != "Test Article" {
		t.Errorf("title = %q, want %q", result.Title, "Test Article")
	}
	if result.Content != "Hello world content here" {
		t.Errorf("content = %q, want %q", result.Content, "Hello world content here")
	}
	if result.Author != "John" {
		t.Errorf("author = %q, want %q", result.Author, "John")
	}
}

func TestOxBrowserExtractError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"title":"","content":"","error":"readability: could not extract article"}`))
	}))
	defer srv.Close()

	client := NewOxBrowserClient(srv.URL)
	_, err := client.Extract(context.Background(), "https://example.com/broken")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestOxBrowserExtractHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"fetch failed"}`))
	}))
	defer srv.Close()

	client := NewOxBrowserClient(srv.URL)
	_, err := client.Extract(context.Background(), "https://example.com/down")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// With INTERNAL_SERVICE_SECRET set, requests to the configured ox-browser
// base URL carry X-Internal-Secret.
func TestOxBrowserExtract_SendsInternalSecret(t *testing.T) {
	t.Setenv("INTERNAL_SERVICE_SECRET", "s3cret")

	var gotSecret string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSecret = r.Header.Get("X-Internal-Secret")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"title":"t","content":"c","length":1}`))
	}))
	defer srv.Close()

	client := NewOxBrowserClient(srv.URL)
	if _, err := client.Extract(context.Background(), "https://example.com/x"); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if gotSecret != "s3cret" {
		t.Errorf("X-Internal-Secret = %q, want %q", gotSecret, "s3cret")
	}
}

// A redirect off the ox-browser origin must not carry the secret to the
// second origin.
func TestOxBrowserExtract_RedirectStripsSecret(t *testing.T) {
	t.Setenv("INTERNAL_SERVICE_SECRET", "s3cret")

	var secondSecret string
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondSecret = r.Header.Get("X-Internal-Secret")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"title":"t","content":"c","length":1}`))
	}))
	defer second.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, second.URL+"/read", http.StatusFound)
	}))
	defer srv.Close()

	client := NewOxBrowserClient(srv.URL)
	if _, err := client.Extract(context.Background(), "https://example.com/x"); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if secondSecret != "" {
		t.Errorf("X-Internal-Secret leaked to second origin: %q", secondSecret)
	}
}

// With INTERNAL_SERVICE_SECRET empty no header is sent and the request is
// still made.
func TestOxBrowserExtract_NoSecretNoHeader(t *testing.T) {
	t.Setenv("INTERNAL_SERVICE_SECRET", "")

	var gotSecret string
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		gotSecret = r.Header.Get("X-Internal-Secret")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"title":"t","content":"c","length":1}`))
	}))
	defer srv.Close()

	client := NewOxBrowserClient(srv.URL)
	if _, err := client.Extract(context.Background(), "https://example.com/x"); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !called {
		t.Fatal("server was not called")
	}
	if gotSecret != "" {
		t.Errorf("X-Internal-Secret = %q, want empty", gotSecret)
	}
}

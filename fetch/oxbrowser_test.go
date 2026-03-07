package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOxBrowserExtract(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/readability" {
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

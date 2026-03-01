package search

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGoogle_Search(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "test-key" {
			t.Errorf("missing key param")
		}
		if r.URL.Query().Get("cx") != "test-cx" {
			t.Errorf("missing cx param")
		}
		if r.URL.Query().Get("q") != "test query" {
			t.Errorf("wrong query, got %q", r.URL.Query().Get("q"))
		}

		resp := googleResponse{
			Items: []googleResult{
				{Link: "https://example.com/1", Title: "Result 1", Snippet: "First snippet"},
				{Link: "https://example.com/2", Title: "Result 2", Snippet: "Second snippet"},
			},
		}
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer srv.Close()

	google := NewGoogle("test-key", "test-cx", WithGoogleBaseURL(srv.URL))
	result, err := google.Search(context.Background(), "test query", "")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(result.Sources) != 2 {
		t.Errorf("expected 2 sources, got %d", len(result.Sources))
	}
	if result.Context == "" {
		t.Error("expected non-empty context")
	}
}

func TestGoogle_DateRestrict(t *testing.T) {
	t.Parallel()
	var gotDateRestrict string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotDateRestrict = r.URL.Query().Get("dateRestrict")
		resp := googleResponse{}
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer srv.Close()

	google := NewGoogle("k", "cx", WithGoogleBaseURL(srv.URL))
	_, err := google.Search(context.Background(), "q", "week")
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	if gotDateRestrict != "w1" {
		t.Errorf("expected 'w1' for week, got %q", gotDateRestrict)
	}
}

func TestGoogle_ErrorStatus(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	google := NewGoogle("k", "cx", WithGoogleBaseURL(srv.URL))
	_, err := google.Search(context.Background(), "q", "")
	if err == nil {
		t.Error("expected error on 403")
	}
}

func TestGoogle_EmptyItems(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := googleResponse{}
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer srv.Close()

	google := NewGoogle("k", "cx", WithGoogleBaseURL(srv.URL))
	result, err := google.Search(context.Background(), "q", "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(result.Sources) != 0 {
		t.Errorf("expected 0 sources, got %d", len(result.Sources))
	}
}

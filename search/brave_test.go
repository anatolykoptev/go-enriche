package search

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBrave_Search(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth header.
		if r.Header.Get("X-Subscription-Token") != "test-key" {
			t.Errorf("missing auth header")
		}
		if r.URL.Query().Get("q") != "test query" {
			t.Errorf("missing query param, got %q", r.URL.Query().Get("q"))
		}

		resp := braveResponse{
			Web: &braveWebResults{
				Results: []braveResult{
					{URL: "https://example.com/1", Title: "Result 1", Description: "First result content"},
					{URL: "https://example.com/2", Title: "Result 2", Description: "Second result content"},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer srv.Close()

	brave := NewBrave("test-key", WithBraveBaseURL(srv.URL))
	result, err := brave.Search(context.Background(), "test query", "")
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

func TestBrave_Freshness(t *testing.T) {
	t.Parallel()
	var gotFreshness string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotFreshness = r.URL.Query().Get("freshness")
		resp := braveResponse{Web: &braveWebResults{}}
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer srv.Close()

	brave := NewBrave("key", WithBraveBaseURL(srv.URL))
	_, err := brave.Search(context.Background(), "q", "week")
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	if gotFreshness != "pw" {
		t.Errorf("expected freshness 'pw' for week, got %q", gotFreshness)
	}
}

func TestBrave_ErrorStatus(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	brave := NewBrave("key", WithBraveBaseURL(srv.URL))
	_, err := brave.Search(context.Background(), "q", "")
	if err == nil {
		t.Error("expected error on 429")
	}
}

func TestBrave_MaxResults(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		results := make([]braveResult, 10)
		for i := range results {
			results[i] = braveResult{URL: "https://example.com/" + string(rune('a'+i)), Title: "R", Description: "D"}
		}
		resp := braveResponse{Web: &braveWebResults{Results: results}}
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer srv.Close()

	brave := NewBrave("key", WithBraveBaseURL(srv.URL), WithBraveMaxResults(3))
	result, err := brave.Search(context.Background(), "q", "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(result.Sources) > 3 {
		t.Errorf("expected max 3 sources, got %d", len(result.Sources))
	}
}

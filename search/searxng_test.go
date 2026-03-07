package search

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSearXNG_Search(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Errorf("expected /search path, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("format") != "json" {
			t.Error("expected format=json")
		}
		if r.URL.Query().Get("categories") != "general" {
			t.Error("expected categories=general")
		}
		resp := searxngAPIResponse{
			Results: []searxngAPIResult{
				{URL: "https://example.com/1", Title: "Result 1", Content: "Content 1"},
				{URL: "https://example.com/2", Title: "Result 2", Content: "Content 2"},
				{URL: "https://example.com/3", Title: "Result 3", Content: "Content 3"},
				{URL: "https://example.com/4", Title: "Result 4", Content: "Content 4"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer srv.Close()

	s := NewSearXNG(srv.URL)
	result, err := s.Search(context.Background(), "test query", TimeRangeWeek)
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(result.Sources) != defaultMaxResults {
		t.Errorf("expected %d sources, got %d", defaultMaxResults, len(result.Sources))
	}
	if result.Context == "" {
		t.Error("expected non-empty context")
	}
}

func TestSearXNG_TimeRange(t *testing.T) {
	t.Parallel()
	var gotTimeRange string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTimeRange = r.URL.Query().Get("time_range")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(searxngAPIResponse{}) //nolint:errcheck
	}))
	defer srv.Close()

	s := NewSearXNG(srv.URL)
	_, _ = s.Search(context.Background(), "test", TimeRangeMonth)
	if gotTimeRange != TimeRangeMonth {
		t.Errorf("expected time_range=month, got %q", gotTimeRange)
	}
}

func TestSearXNG_EmptyTimeRange(t *testing.T) {
	t.Parallel()
	var hasTimeRange bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hasTimeRange = r.URL.Query().Has("time_range")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(searxngAPIResponse{}) //nolint:errcheck
	}))
	defer srv.Close()

	s := NewSearXNG(srv.URL)
	_, _ = s.Search(context.Background(), "test", "")
	if hasTimeRange {
		t.Error("time_range should not be sent when empty")
	}
}

func TestSearXNG_Dedup(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := searxngAPIResponse{
			Results: []searxngAPIResult{
				{URL: "https://example.com/page", Title: "A", Content: "Content A"},
				{URL: "https://example.com/page", Title: "B", Content: "Content B"},
				{URL: "https://EXAMPLE.COM/page/", Title: "C", Content: "Content C"},
				{URL: "https://other.com/x", Title: "D", Content: "Content D"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer srv.Close()

	s := NewSearXNG(srv.URL)
	result, err := s.Search(context.Background(), "test", "")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	// First 3 URLs normalize to the same; only 2 unique sources expected.
	if len(result.Sources) != 2 {
		t.Errorf("expected 2 unique sources after dedup, got %d: %v", len(result.Sources), result.Sources)
	}
}

func TestSearXNG_ServerError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	s := NewSearXNG(srv.URL)
	_, err := s.Search(context.Background(), "test", "")
	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestSearXNG_CustomMaxResults(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := searxngAPIResponse{
			Results: []searxngAPIResult{
				{URL: "https://a.com", Title: "A", Content: "CA"},
				{URL: "https://b.com", Title: "B", Content: "CB"},
				{URL: "https://c.com", Title: "C", Content: "CC"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer srv.Close()

	s := NewSearXNG(srv.URL, WithMaxResults(1))
	result, err := s.Search(context.Background(), "test", "")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(result.Sources) != 1 {
		t.Errorf("expected 1 source with WithMaxResults(1), got %d", len(result.Sources))
	}
}

func TestNormalizeURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"https://Example.COM/page#section", "https://example.com/page"},
		{"https://example.com/page/", "https://example.com/page"},
		{"HTTP://EXAMPLE.COM", "http://example.com"},
		{"", ""},
	}
	for _, tt := range tests {
		got := normalizeURL(tt.input)
		if got != tt.want {
			t.Errorf("normalizeURL(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

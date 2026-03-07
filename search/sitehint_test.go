package search

import (
	"context"
	"testing"
)

// recordingProvider records the query it receives.
type recordingProvider struct {
	lastQuery string
}

func (r *recordingProvider) Search(_ context.Context, query string, _ string) (*SearchResult, error) {
	r.lastQuery = query
	return &SearchResult{Context: "result", Sources: []string{"https://example.com"}}, nil
}

func TestSiteHint_AppendsRestriction(t *testing.T) {
	rec := &recordingProvider{}
	sh := NewSiteHint(rec, []string{"fontanka.ru", "sobaka.ru"})

	_, err := sh.Search(t.Context(), "кафе Петербург", "")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	want := "кафе Петербург site:fontanka.ru OR site:sobaka.ru"
	if rec.lastQuery != want {
		t.Errorf("query = %q, want %q", rec.lastQuery, want)
	}
}

func TestSiteHint_EmptySites(t *testing.T) {
	rec := &recordingProvider{}
	sh := NewSiteHint(rec, nil)

	_, err := sh.Search(t.Context(), "test query", "")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if rec.lastQuery != "test query" {
		t.Errorf("query = %q, want %q", rec.lastQuery, "test query")
	}
}

func TestSiteHint_PropagatesResults(t *testing.T) {
	rec := &recordingProvider{}
	sh := NewSiteHint(rec, []string{"example.com"})

	result, err := sh.Search(t.Context(), "test", "")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if result.Context != "result" {
		t.Errorf("Context = %q, want %q", result.Context, "result")
	}
	if len(result.Sources) != 1 {
		t.Errorf("Sources = %d, want 1", len(result.Sources))
	}
}

package search

import (
	"context"
	"errors"
	"testing"
)

func TestParallel_MergesResults(t *testing.T) {
	t.Parallel()
	p := NewParallel(&okProvider{"alpha"}, &okProvider{"beta"})
	result, err := p.Search(context.Background(), "q", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Sources) != 2 {
		t.Errorf("expected 2 sources, got %d", len(result.Sources))
	}
	if result.Context == "" {
		t.Error("expected non-empty context")
	}
}

func TestParallel_PartialFailure(t *testing.T) {
	t.Parallel()
	p := NewParallel(
		&failProvider{errors.New("provider down")},
		&okProvider{"beta"},
	)
	result, err := p.Search(context.Background(), "q", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Sources) != 1 {
		t.Errorf("expected 1 source, got %d", len(result.Sources))
	}
	if result.Context != "beta" {
		t.Errorf("expected context 'beta', got %q", result.Context)
	}
}

func TestParallel_AllFail(t *testing.T) {
	t.Parallel()
	p := NewParallel(
		&failProvider{errors.New("first down")},
		&failProvider{errors.New("second down")},
	)
	_, err := p.Search(context.Background(), "q", "")
	if err == nil {
		t.Error("expected error when all providers fail")
	}
}

func TestParallel_SingleProvider(t *testing.T) {
	t.Parallel()
	p := NewParallel(&okProvider{"only"})
	result, err := p.Search(context.Background(), "q", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Context != "only" {
		t.Errorf("expected context 'only', got %q", result.Context)
	}
	if len(result.Sources) != 1 {
		t.Errorf("expected 1 source, got %d", len(result.Sources))
	}
}

func TestParallel_DeduplicatesSources(t *testing.T) {
	t.Parallel()
	// Both providers return the same URL — expect exactly 1 source after dedup.
	dup := &dupProvider{url: "https://example.com"}
	p := NewParallel(dup, dup)
	result, err := p.Search(context.Background(), "q", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Sources) != 1 {
		t.Errorf("expected 1 deduplicated source, got %d: %v", len(result.Sources), result.Sources)
	}
}

func TestParallel_Empty(t *testing.T) {
	t.Parallel()
	p := NewParallel()
	_, err := p.Search(context.Background(), "q", "")
	if err == nil {
		t.Error("expected error for empty provider list")
	}
}

// dupProvider returns the same URL regardless of name.
type dupProvider struct {
	url string
}

func (d *dupProvider) Search(_ context.Context, _ string, _ string) (*SearchResult, error) {
	return &SearchResult{Context: "dup", Sources: []string{d.url}}, nil
}

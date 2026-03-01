package search

import (
	"context"
	"errors"
	"testing"
)

type failProvider struct {
	err error
}

func (f *failProvider) Search(_ context.Context, _ string, _ string) (*SearchResult, error) {
	return nil, f.err
}

type okProvider struct {
	name string
}

func (o *okProvider) Search(_ context.Context, _ string, _ string) (*SearchResult, error) {
	return &SearchResult{Context: o.name, Sources: []string{"https://" + o.name}}, nil
}

func TestFallback_PrimarySucceeds(t *testing.T) {
	t.Parallel()
	fb := NewFallback(&okProvider{"primary"}, &okProvider{"secondary"})
	result, err := fb.Search(context.Background(), "q", "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.Context != "primary" {
		t.Errorf("expected primary, got %q", result.Context)
	}
}

func TestFallback_PrimaryFails(t *testing.T) {
	t.Parallel()
	fb := NewFallback(
		&failProvider{errors.New("primary down")},
		&okProvider{"secondary"},
	)
	result, err := fb.Search(context.Background(), "q", "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.Context != "secondary" {
		t.Errorf("expected secondary, got %q", result.Context)
	}
}

func TestFallback_AllFail(t *testing.T) {
	t.Parallel()
	fb := NewFallback(
		&failProvider{errors.New("first down")},
		&failProvider{errors.New("second down")},
	)
	_, err := fb.Search(context.Background(), "q", "")
	if err == nil {
		t.Error("expected error when all providers fail")
	}
}

func TestFallback_ThreeProviders(t *testing.T) {
	t.Parallel()
	fb := NewFallback(
		&failProvider{errors.New("first down")},
		&failProvider{errors.New("second down")},
		&okProvider{"third"},
	)
	result, err := fb.Search(context.Background(), "q", "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.Context != "third" {
		t.Errorf("expected third, got %q", result.Context)
	}
}

func TestFallback_SingleProvider(t *testing.T) {
	t.Parallel()
	fb := NewFallback(&okProvider{"only"})
	result, err := fb.Search(context.Background(), "q", "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.Context != "only" {
		t.Errorf("expected 'only', got %q", result.Context)
	}
}

package search

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type callCounter struct {
	calls atomic.Int32
}

func (c *callCounter) Search(_ context.Context, _ string, _ string) (*SearchResult, error) {
	c.calls.Add(1)
	return &SearchResult{Context: "ok"}, nil
}

func TestRateLimited_Throttles(t *testing.T) {
	t.Parallel()
	counter := &callCounter{}
	limited := NewRateLimited(counter, 2, 1) // 2 req/s, burst 1

	ctx := context.Background()

	// First call — immediate (burst token).
	_, err := limited.Search(ctx, "q1", "")
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}

	// Rapid second call — should block briefly.
	start := time.Now()
	_, err = limited.Search(ctx, "q2", "")
	if err != nil {
		t.Fatalf("second call error: %v", err)
	}
	elapsed := time.Since(start)

	// Should have waited ~500ms (1/2 req/s).
	if elapsed < 300*time.Millisecond {
		t.Errorf("expected throttle delay, got %v", elapsed)
	}

	if counter.calls.Load() != 2 {
		t.Errorf("expected 2 calls, got %d", counter.calls.Load())
	}
}

func TestRateLimited_ContextCancel(t *testing.T) {
	t.Parallel()
	counter := &callCounter{}
	limited := NewRateLimited(counter, 0.1, 1) // very slow: 1 per 10s

	ctx := context.Background()
	// Consume burst token.
	_, _ = limited.Search(ctx, "q1", "")

	// Cancel context — should error, not block.
	ctx2, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := limited.Search(ctx2, "q2", "")
	if err == nil {
		t.Error("expected context cancellation error")
	}
}

func TestRateLimited_PassesThrough(t *testing.T) {
	t.Parallel()
	inner := &callCounter{}
	limited := NewRateLimited(inner, 100, 10) // generous limit

	result, err := limited.Search(context.Background(), "test", "week")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.Context != "ok" {
		t.Errorf("expected 'ok', got %q", result.Context)
	}
}

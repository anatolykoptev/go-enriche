package search

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCircuitBreaker_SuspendsAfterError(t *testing.T) {
	t.Parallel()
	cb := &CircuitBreaker{}
	if cb.IsSuspended() {
		t.Fatal("should not be suspended initially")
	}

	cb.RecordError()
	if !cb.IsSuspended() {
		t.Fatal("should be suspended after error")
	}
}

func TestCircuitBreaker_ResetsOnSuccess(t *testing.T) {
	t.Parallel()
	cb := &CircuitBreaker{}
	cb.RecordError()
	cb.RecordSuccess()
	if cb.IsSuspended() {
		t.Fatal("should not be suspended after success")
	}
}

func TestCircuitBreaker_ExponentialBackoff(t *testing.T) {
	t.Parallel()
	cb := &CircuitBreaker{}

	cb.RecordError() // 30s
	cb.mu.Lock()
	dur1 := time.Until(cb.suspendUntil)
	cb.mu.Unlock()

	cb.RecordError() // 60s
	cb.mu.Lock()
	dur2 := time.Until(cb.suspendUntil)
	cb.mu.Unlock()

	if dur2 <= dur1 {
		t.Errorf("expected increasing backoff, got dur1=%v dur2=%v", dur1, dur2)
	}
}

func TestCircuitBreaker_CapsAtMax(t *testing.T) {
	t.Parallel()
	cb := &CircuitBreaker{}
	for range 20 {
		cb.RecordError()
	}
	cb.mu.Lock()
	dur := time.Until(cb.suspendUntil)
	cb.mu.Unlock()
	if dur > maxSuspendDuration+time.Second {
		t.Errorf("expected capped at %v, got %v", maxSuspendDuration, dur)
	}
}

type failingProvider struct{ err error }

func (f *failingProvider) Search(context.Context, string, string) (*SearchResult, error) {
	return nil, f.err
}

type successProvider struct{ result *SearchResult }

func (s *successProvider) Search(context.Context, string, string) (*SearchResult, error) {
	return s.result, nil
}

func TestResilient_SkipsWhenSuspended(t *testing.T) {
	t.Parallel()
	inner := &failingProvider{err: errors.New("fail")}
	r := NewResilient(inner)

	// First call fails and suspends.
	_, err := r.Search(context.Background(), "test", "")
	if err == nil {
		t.Fatal("expected error")
	}

	// Second call should be ErrSuspended (inner not called).
	_, err = r.Search(context.Background(), "test", "")
	if !errors.Is(err, ErrSuspended) {
		t.Fatalf("expected ErrSuspended, got %v", err)
	}
}

func TestResilient_PassesThrough(t *testing.T) {
	t.Parallel()
	want := &SearchResult{Context: "ok", Sources: []string{"https://a.com"}}
	inner := &successProvider{result: want}
	r := NewResilient(inner)

	got, err := r.Search(context.Background(), "test", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Context != want.Context {
		t.Errorf("expected context %q, got %q", want.Context, got.Context)
	}
}

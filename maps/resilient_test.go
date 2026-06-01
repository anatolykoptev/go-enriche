package maps

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// mockChecker is a controllable Checker for testing Resilient.
type mockChecker struct {
	calls  atomic.Int32
	result *CheckResult
	err    error
	// block causes the mock to wait for ctx.Done before returning.
	block bool
}

func (m *mockChecker) Check(ctx context.Context, _, _ string) (*CheckResult, error) {
	m.calls.Add(1)
	if m.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return m.result, m.err
}

var errBackend = errors.New("backend failure")

// openResult is a convenience fixture.
var openResult = &CheckResult{Status: PlaceOpen}

// TestResilient_SuccessPassThrough verifies a healthy call goes through unchanged.
func TestResilient_SuccessPassThrough(t *testing.T) {
	inner := &mockChecker{result: openResult}
	r := NewResilient(inner, 0)

	got, err := r.Check(context.Background(), "Place", "City")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Status != PlaceOpen {
		t.Errorf("status = %q, want %q", got.Status, PlaceOpen)
	}
	if inner.calls.Load() != 1 {
		t.Errorf("inner calls = %d, want 1", inner.calls.Load())
	}
}

// TestResilient_PlaceNotFoundIsSuccess verifies PlaceNotFound from inner does NOT trip the breaker.
func TestResilient_PlaceNotFoundIsSuccess(t *testing.T) {
	inner := &mockChecker{result: &CheckResult{Status: PlaceNotFound}}
	r := NewResilient(inner, 0)

	got, err := r.Check(context.Background(), "Nowhere", "City")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Status != PlaceNotFound {
		t.Errorf("status = %q, want PlaceNotFound", got.Status)
	}
	// Breaker must remain closed — next call still hits inner.
	got2, _ := r.Check(context.Background(), "Nowhere", "City")
	if got2.Status != PlaceNotFound {
		t.Errorf("second call status = %q, want PlaceNotFound", got2.Status)
	}
	if inner.calls.Load() != 2 { //nolint:mnd
		t.Errorf("inner calls = %d, want 2 (breaker must not open on PlaceNotFound)", inner.calls.Load())
	}
}

// TestResilient_BreakerOpensAfterError verifies one error suspends the backend.
func TestResilient_BreakerOpensAfterError(t *testing.T) {
	inner := &mockChecker{err: errBackend}
	r := NewResilient(inner, 0)

	// First call: hits inner, triggers error, opens breaker.
	got, err := r.Check(context.Background(), "P", "C")
	if err != nil {
		t.Fatalf("expected nil error (fall-through), got %v", err)
	}
	if got.Status != PlaceNotFound {
		t.Errorf("fall-through status = %q, want PlaceNotFound", got.Status)
	}

	// Breaker is now open — subsequent calls must short-circuit.
	before := inner.calls.Load()
	for range 3 {
		got2, err2 := r.Check(context.Background(), "P", "C")
		if err2 != nil {
			t.Fatalf("suspended call returned error: %v", err2)
		}
		if got2.Status != PlaceNotFound {
			t.Errorf("suspended call status = %q, want PlaceNotFound", got2.Status)
		}
	}
	after := inner.calls.Load()
	if after != before {
		t.Errorf("inner called %d extra times while suspended, want 0", after-before)
	}
}

// TestResilient_HalfOpenProbeOnExpiry verifies that after the suspend window,
// the next call probes the inner (half-open) and a success resets the breaker.
func TestResilient_HalfOpenProbeOnExpiry(t *testing.T) {
	inner := &mockChecker{err: errBackend}
	r := NewResilient(inner, 0)

	// Trip the breaker.
	_, _ = r.Check(context.Background(), "P", "C")

	// Fast-forward past the suspend window by backdating suspendUntil.
	r.cb.mu.Lock()
	r.cb.suspendUntil = time.Now().Add(-time.Second)
	r.cb.mu.Unlock()

	// Swap inner to a healthy one.
	inner2 := &mockChecker{result: openResult}
	r.inner = inner2

	got, err := r.Check(context.Background(), "P", "C")
	if err != nil {
		t.Fatalf("half-open probe error: %v", err)
	}
	if got.Status != PlaceOpen {
		t.Errorf("half-open result status = %q, want PlaceOpen", got.Status)
	}
	if inner2.calls.Load() != 1 {
		t.Errorf("inner2 calls = %d, want 1", inner2.calls.Load())
	}

	// Breaker must be reset — consecutiveErrs == 0 and suspendUntil zero.
	r.cb.mu.Lock()
	errs := r.cb.consecutiveErrs
	until := r.cb.suspendUntil
	r.cb.mu.Unlock()
	if errs != 0 {
		t.Errorf("consecutiveErrs = %d after success, want 0", errs)
	}
	if !until.IsZero() {
		t.Errorf("suspendUntil = %v after success, want zero", until)
	}
}

// TestResilient_TimeoutTreatedAsFailure verifies a slow inner is timed out and falls through.
func TestResilient_TimeoutTreatedAsFailure(t *testing.T) {
	inner := &mockChecker{block: true}
	r := NewResilient(inner, 10*time.Millisecond)

	start := time.Now()
	got, err := r.Check(context.Background(), "Slow", "City")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("expected nil error (fall-through), got %v", err)
	}
	if got.Status != PlaceNotFound {
		t.Errorf("timeout fall-through status = %q, want PlaceNotFound", got.Status)
	}
	// Should complete well within 1s; 10ms timeout + overhead.
	if elapsed > time.Second {
		t.Errorf("Check() took %v, should complete near 10ms timeout", elapsed)
	}
	// Breaker must be open after timeout.
	before := inner.calls.Load()
	r.Check(context.Background(), "Slow", "City") //nolint:errcheck
	if inner.calls.Load() != before {
		t.Error("inner was called again while suspended after timeout")
	}
}

// TestResilient_NilResultFallsThrough verifies (nil, nil) from inner is treated as failure.
func TestResilient_NilResultFallsThrough(t *testing.T) {
	inner := &mockChecker{result: nil, err: nil}
	r := NewResilient(inner, 0)

	got, err := r.Check(context.Background(), "P", "C")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Status != PlaceNotFound {
		t.Errorf("nil-result status = %q, want PlaceNotFound", got.Status)
	}
}

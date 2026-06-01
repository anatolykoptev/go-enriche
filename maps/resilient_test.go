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
	// Breaker must remain closed -- next call still hits inner.
	got2, _ := r.Check(context.Background(), "Nowhere", "City")
	if got2.Status != PlaceNotFound {
		t.Errorf("second call status = %q, want PlaceNotFound", got2.Status)
	}
	if inner.calls.Load() != 2 { //nolint:mnd
		t.Errorf("inner calls = %d, want 2 (breaker must not open on PlaceNotFound)", inner.calls.Load())
	}
}

// TestResilient_BreakerOpensAfterError verifies one error suspends the backend.
// After opening, Check returns (nil, ErrSuspended) and inner is NOT called.
func TestResilient_BreakerOpensAfterError(t *testing.T) {
	inner := &mockChecker{err: errBackend}
	r := NewResilient(inner, 0)

	// First call: hits inner, triggers error, opens breaker.
	got, err := r.Check(context.Background(), "P", "C")
	if err == nil {
		t.Fatalf("expected error from failed backend, got nil (result=%v)", got)
	}
	if got != nil {
		t.Errorf("expected nil result on error, got %v", got)
	}

	// Breaker is now open -- subsequent calls must short-circuit with ErrSuspended.
	before := inner.calls.Load()
	for range 3 {
		got2, err2 := r.Check(context.Background(), "P", "C")
		if !errors.Is(err2, ErrSuspended) {
			t.Errorf("suspended call error = %v, want ErrSuspended", err2)
		}
		if got2 != nil {
			t.Errorf("suspended call result = %v, want nil", got2)
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

	// Breaker must be reset -- consecutiveErrs == 0 and suspendUntil zero.
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

// TestResilient_TimeoutTreatedAsFailure verifies a slow inner is timed out and
// the per-call timeout is treated as a backend failure (breaker is tripped).
func TestResilient_TimeoutTreatedAsFailure(t *testing.T) {
	inner := &mockChecker{block: true}
	r := NewResilient(inner, 10*time.Millisecond)

	start := time.Now()
	got, err := r.Check(context.Background(), "Slow", "City")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected error from timeout, got nil (result=%v)", got)
	}
	if got != nil {
		t.Errorf("expected nil result on timeout, got %v", got)
	}
	// Should complete well within 1s; 10ms timeout + overhead.
	if elapsed > time.Second {
		t.Errorf("Check() took %v, should complete near 10ms timeout", elapsed)
	}
	// Breaker must be open after per-call timeout.
	before := inner.calls.Load()
	_, err2 := r.Check(context.Background(), "Slow", "City")
	if !errors.Is(err2, ErrSuspended) {
		t.Errorf("expected ErrSuspended after timeout, got %v", err2)
	}
	if inner.calls.Load() != before {
		t.Error("inner was called again while suspended after timeout")
	}
}

// TestResilient_NilResultFallsThrough verifies (nil, nil) from inner is treated as failure.
func TestResilient_NilResultFallsThrough(t *testing.T) {
	inner := &mockChecker{result: nil, err: nil}
	r := NewResilient(inner, 0)

	got, err := r.Check(context.Background(), "P", "C")
	if err == nil {
		t.Fatalf("expected error for nil result, got nil (result=%v)", got)
	}
	if got != nil {
		t.Errorf("expected nil result, got %v", got)
	}
	if !errors.Is(err, errNilResult) {
		t.Errorf("err = %v, want errNilResult", err)
	}
}

// TestResilient_ParentCancelNotChargedToBreaker verifies that cancellation of the
// PARENT context does not count as a backend fault. After a parent-cancel failure,
// the next call with a live context must still reach the inner (breaker not opened).
func TestResilient_ParentCancelNotChargedToBreaker(t *testing.T) {
	inner := &mockChecker{block: true}
	r := NewResilient(inner, 0) // no per-call timeout -- parent ctx governs

	parentCtx, parentCancel := context.WithCancel(context.Background())

	// Cancel the parent while the inner is blocked.
	done := make(chan struct{})
	go func() {
		defer close(done)
		parentCancel()
	}()

	got, err := r.Check(parentCtx, "P", "C")
	<-done

	if err == nil {
		t.Fatalf("expected error from parent cancel, got nil (result=%v)", got)
	}
	if got != nil {
		t.Errorf("expected nil result on cancel, got %v", got)
	}

	// Breaker must NOT be open -- the failure was the caller's fault.
	callsBefore := inner.calls.Load()
	inner.block = false
	inner.result = openResult
	got2, err2 := r.Check(context.Background(), "P", "C")
	if err2 != nil {
		t.Fatalf("post-cancel call error: %v (breaker opened when it should not have)", err2)
	}
	if got2 == nil || got2.Status != PlaceOpen {
		t.Errorf("post-cancel result = %v, want PlaceOpen", got2)
	}
	if inner.calls.Load() == callsBefore {
		t.Error("inner was not called after parent-cancel -- breaker incorrectly opened")
	}
}

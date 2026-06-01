package maps

import (
	"context"
	"sync"
	"time"
)

const (
	resilientBaseSuspend = 30 * time.Second
	resilientMaxSuspend  = 10 * time.Minute
	resilientMaxExpShift = 8
)

// circuitBreaker tracks consecutive errors and temporarily suspends a backend.
// After each error the suspend duration doubles (exponential backoff, capped at 10m).
// A single successful call resets the counter.
type circuitBreaker struct {
	mu              sync.Mutex
	suspendUntil    time.Time
	consecutiveErrs int
}

// isSuspended returns true if the backend is currently suspended.
func (cb *circuitBreaker) isSuspended() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return time.Now().Before(cb.suspendUntil)
}

// recordError increments the error counter and extends the suspend window.
func (cb *circuitBreaker) recordError() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.consecutiveErrs++
	shift := cb.consecutiveErrs - 1
	if shift > resilientMaxExpShift {
		shift = resilientMaxExpShift
	}
	dur := resilientBaseSuspend * time.Duration(1<<shift)
	if dur > resilientMaxSuspend {
		dur = resilientMaxSuspend
	}
	cb.suspendUntil = time.Now().Add(dur)
}

// recordSuccess resets the error counter and clears the suspend window.
func (cb *circuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.consecutiveErrs = 0
	cb.suspendUntil = time.Time{}
}

// Resilient wraps a Checker with:
//   - a per-call timeout (context.WithTimeout) so a slow backend can't eat the entire budget.
//   - a circuit breaker that suspends the backend after consecutive errors.
//
// When suspended or on timeout/error, Resilient returns PlaceNotFound+nil so
// CompositeChecker continues to the next backend rather than aborting the chain.
//
// PlaceNotFound from a healthy inner call is treated as SUCCESS (backend is up,
// place simply not found there); the circuit breaker is NOT tripped.
type Resilient struct {
	inner   Checker
	timeout time.Duration
	cb      *circuitBreaker
}

// NewResilient wraps a Checker with circuit-breaker and per-call timeout.
// timeout is applied per Check call; 0 means no extra timeout (context deadline governs).
func NewResilient(inner Checker, timeout time.Duration) *Resilient {
	return &Resilient{
		inner:   inner,
		timeout: timeout,
		cb:      &circuitBreaker{},
	}
}

// Check delegates to the inner Checker unless the circuit breaker is open.
// On breaker-open, timeout, or error: returns PlaceNotFound+nil (fall-through for CompositeChecker).
// On success (any status including PlaceNotFound): records success, returns result as-is.
func (r *Resilient) Check(ctx context.Context, name, city string) (*CheckResult, error) {
	if r.cb.isSuspended() {
		return &CheckResult{Status: PlaceNotFound}, nil
	}

	callCtx := ctx
	var cancel context.CancelFunc
	if r.timeout > 0 {
		callCtx, cancel = context.WithTimeout(ctx, r.timeout)
		defer cancel()
	}

	result, err := r.inner.Check(callCtx, name, city)
	if err != nil {
		r.cb.recordError()
		return &CheckResult{Status: PlaceNotFound}, nil
	}
	if result == nil {
		// Guard against a misbehaving inner that returns (nil, nil).
		r.cb.recordError()
		return &CheckResult{Status: PlaceNotFound}, nil
	}

	r.cb.recordSuccess()
	return result, nil
}

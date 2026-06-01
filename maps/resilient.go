package maps

import (
	"context"
	"errors"
	"sync"
	"time"
)

const (
	resilientBaseSuspend = 30 * time.Second
	resilientMaxSuspend  = 10 * time.Minute
	resilientMaxExpShift = 8
)

// ErrSuspended is returned by Resilient.Check when the circuit breaker is open.
// CompositeChecker treats any non-nil error as a transient backend failure:
// it logs the error and continues to the next backend. Returning ErrSuspended
// (rather than a synthetic PlaceNotFound result) preserves that log entry
// and increments the mapsCheckError metric in enriche.go.
//
// Wrap each backend individually inside CompositeChecker:
//
//	NewResilient(NewTwoGIS(...), timeout)     // good -- per-backend breaker
//	NewResilient(NewCompositeChecker(...), t) // bad  -- one breaker hides which backend failed
var ErrSuspended = errors.New("maps: backend suspended (circuit breaker open)")

// errNilResult is returned when the inner Checker returns (nil, nil),
// which violates the Checker contract. Treated as a backend failure.
var errNilResult = errors.New("maps: inner checker returned nil result and nil error")

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
//   - a per-call timeout (context.WithTimeout) so a slow backend cannot eat the entire budget.
//   - a circuit breaker that suspends the backend after consecutive errors.
//
// On breaker-open, timeout, or inner error, Resilient returns (nil, err) so
// CompositeChecker logs the failure and continues to the next backend.
// This preserves error visibility: the slog.Debug in composite.go fires, and
// enriche.go increments mapsCheckError via the err != nil gate.
//
// A genuine PlaceNotFound from a HEALTHY inner call is treated as SUCCESS --
// the backend is up, the place simply is not there -- and is returned as
// (*CheckResult{Status: PlaceNotFound}, nil) without tripping the breaker.
//
// Wrap each backend individually (see ErrSuspended doc).
type Resilient struct {
	inner   Checker
	timeout time.Duration
	cb      *circuitBreaker
}

// NewResilient wraps a Checker with circuit-breaker and per-call timeout.
// timeout is applied per Check call; 0 means no extra timeout (context deadline governs).
// Wrap each backend individually inside CompositeChecker -- see ErrSuspended.
func NewResilient(inner Checker, timeout time.Duration) *Resilient {
	return &Resilient{
		inner:   inner,
		timeout: timeout,
		cb:      &circuitBreaker{},
	}
}

// Check delegates to the inner Checker unless the circuit breaker is open.
//
// Return contract:
//   - breaker open:          (nil, ErrSuspended)
//   - inner returns error:   (nil, err) -- parent-ctx cancel is NOT recorded as backend failure
//   - inner returns nil,nil: (nil, errNilResult) -- contract violation, treated as failure
//   - inner returns result:  (result, nil) -- including PlaceNotFound (healthy response)
func (r *Resilient) Check(ctx context.Context, name, city string) (*CheckResult, error) {
	if r.cb.isSuspended() {
		return nil, ErrSuspended
	}

	callCtx := ctx
	var cancel context.CancelFunc
	if r.timeout > 0 {
		callCtx, cancel = context.WithTimeout(ctx, r.timeout)
		defer cancel()
	}

	result, err := r.inner.Check(callCtx, name, city)
	if err != nil {
		// Do NOT charge this against the breaker if the CALLER's context is already
		// done -- a parent cancellation or deadline is not the backend's fault.
		if ctx.Err() == nil {
			r.cb.recordError()
		}
		return nil, err
	}
	if result == nil {
		// Guard against a misbehaving inner that returns (nil, nil).
		r.cb.recordError()
		return nil, errNilResult
	}

	r.cb.recordSuccess()
	return result, nil
}

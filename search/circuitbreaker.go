package search

import (
	"context"
	"errors"
	"sync"
	"time"
)

const (
	baseSuspendDuration  = 30 * time.Second
	maxSuspendDuration   = 10 * time.Minute
	maxBackoffExponent   = 8
)

// CircuitBreaker tracks consecutive errors and temporarily suspends a provider.
// After each error the suspend duration doubles (exponential backoff).
// A single success resets the counter.
type CircuitBreaker struct {
	mu              sync.Mutex
	suspendUntil    time.Time
	consecutiveErrs int
}

// IsSuspended returns true if the provider is currently suspended.
func (cb *CircuitBreaker) IsSuspended() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return time.Now().Before(cb.suspendUntil)
}

// RecordError increments the error counter and extends the suspend window.
func (cb *CircuitBreaker) RecordError() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.consecutiveErrs++
	dur := baseSuspendDuration * time.Duration(1<<min(cb.consecutiveErrs-1, maxBackoffExponent))
	if dur > maxSuspendDuration {
		dur = maxSuspendDuration
	}
	cb.suspendUntil = time.Now().Add(dur)
}

// RecordSuccess resets the error counter and clears the suspend window.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.consecutiveErrs = 0
	cb.suspendUntil = time.Time{}
}

// Resilient wraps a Provider with a CircuitBreaker.
// While the provider is suspended, Search returns ErrSuspended immediately.
type Resilient struct {
	inner Provider
	cb    *CircuitBreaker
}

// ErrSuspended is returned when a provider is temporarily suspended.
var ErrSuspended = errors.New("provider suspended")

// NewResilient wraps a provider with circuit breaker protection.
func NewResilient(p Provider) *Resilient {
	return &Resilient{inner: p, cb: &CircuitBreaker{}}
}

// Search delegates to the inner provider unless it is suspended.
func (r *Resilient) Search(ctx context.Context, query string, timeRange string) (*SearchResult, error) {
	if r.cb.IsSuspended() {
		return nil, ErrSuspended
	}
	result, err := r.inner.Search(ctx, query, timeRange)
	if err != nil {
		r.cb.RecordError()
		return nil, err
	}
	r.cb.RecordSuccess()
	return result, nil
}

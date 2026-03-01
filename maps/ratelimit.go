package maps

import (
	"context"
	"fmt"

	"golang.org/x/time/rate"
)

// RateLimited wraps a Checker with a token bucket rate limiter.
type RateLimited struct {
	inner   Checker
	limiter *rate.Limiter
}

// NewRateLimited creates a rate-limited checker.
// rps: requests per second, burst: max burst size.
func NewRateLimited(c Checker, rps float64, burst int) *RateLimited {
	return &RateLimited{
		inner:   c,
		limiter: rate.NewLimiter(rate.Limit(rps), burst),
	}
}

// Check waits for a rate limit token, then delegates to the inner checker.
func (r *RateLimited) Check(ctx context.Context, name, city string) (*CheckResult, error) {
	if err := r.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit: %w", err)
	}
	return r.inner.Check(ctx, name, city)
}

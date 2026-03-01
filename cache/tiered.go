package cache

import (
	"context"
	"time"
)

const promotionTTL = 5 * time.Minute

// Tiered is a Cache that checks L1 first, then L2.
// On L2 hit, the value is promoted to L1.
type Tiered struct {
	l1 Cache
	l2 Cache
}

// NewTiered creates a tiered cache with L1 (fast) and L2 (durable).
func NewTiered(l1, l2 Cache) *Tiered {
	return &Tiered{l1: l1, l2: l2}
}

// Get checks L1 first, then L2. On L2 hit, promotes to L1.
func (t *Tiered) Get(ctx context.Context, key string, dest any) bool {
	if t.l1.Get(ctx, key, dest) {
		return true
	}
	if t.l2.Get(ctx, key, dest) {
		// Promote to L1 with a short TTL.
		t.l1.Set(ctx, key, dest, promotionTTL)
		return true
	}
	return false
}

// Set stores in both L1 and L2.
func (t *Tiered) Set(ctx context.Context, key string, value any, ttl time.Duration) {
	t.l1.Set(ctx, key, value, ttl)
	t.l2.Set(ctx, key, value, ttl)
}

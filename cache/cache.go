// Package cache provides a Cache interface with in-memory (L1)
// and Redis (L2) implementations.
package cache

import (
	"context"
	"time"
)

// Cache is the interface for enrichment caching.
type Cache interface {
	// Get retrieves a cached value. Returns false if not found.
	Get(ctx context.Context, key string, dest any) bool
	// Set stores a value with the given TTL. Zero TTL means no expiration.
	Set(ctx context.Context, key string, value any, ttl time.Duration)
}

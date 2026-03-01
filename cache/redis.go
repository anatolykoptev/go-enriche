package cache

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis is a Cache backed by Redis.
type Redis struct {
	client *redis.Client
}

// NewRedis creates a Redis cache.
func NewRedis(client *redis.Client) *Redis {
	return &Redis{client: client}
}

// Get retrieves a cached value from Redis. Returns false if not found.
func (r *Redis) Get(ctx context.Context, key string, dest any) bool {
	data, err := r.client.Get(ctx, key).Bytes()
	if err != nil {
		return false
	}
	return json.Unmarshal(data, dest) == nil
}

// Set stores a value in Redis with the given TTL. Zero TTL means no expiration.
func (r *Redis) Set(ctx context.Context, key string, value any, ttl time.Duration) {
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	r.client.Set(ctx, key, data, ttl) //nolint:errcheck
}

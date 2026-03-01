package cache

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// Memory is an in-memory Cache backed by sync.Map.
type Memory struct {
	data sync.Map
}

type memoryEntry struct {
	value     []byte
	expiresAt time.Time
}

// NewMemory creates an in-memory cache.
func NewMemory() *Memory {
	return &Memory{}
}

// Get retrieves a cached value. Returns false if not found or expired.
func (m *Memory) Get(_ context.Context, key string, dest any) bool {
	raw, ok := m.data.Load(key)
	if !ok {
		return false
	}
	entry := raw.(memoryEntry)
	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		m.data.Delete(key)
		return false
	}
	return json.Unmarshal(entry.value, dest) == nil
}

// Set stores a value with the given TTL. Zero TTL means no expiration.
func (m *Memory) Set(_ context.Context, key string, value any, ttl time.Duration) {
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl)
	}
	m.data.Store(key, memoryEntry{value: data, expiresAt: expiresAt})
}

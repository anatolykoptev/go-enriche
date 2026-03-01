package cache

import (
	"context"
	"testing"
	"time"
)

func TestTiered_L1Hit(t *testing.T) {
	t.Parallel()
	l1 := NewMemory()
	l2 := NewMemory()
	tc := NewTiered(l1, l2)
	ctx := context.Background()

	l1.Set(ctx, "k", "from-l1", time.Minute)

	var got string
	if !tc.Get(ctx, "k", &got) {
		t.Fatal("expected hit from L1")
	}
	if got != "from-l1" {
		t.Errorf("expected 'from-l1', got %q", got)
	}
}

func TestTiered_L2Hit_Promotes(t *testing.T) {
	t.Parallel()
	l1 := NewMemory()
	l2 := NewMemory()
	tc := NewTiered(l1, l2)
	ctx := context.Background()

	// Only in L2.
	l2.Set(ctx, "k", "from-l2", time.Minute)

	var got string
	if !tc.Get(ctx, "k", &got) {
		t.Fatal("expected hit from L2")
	}
	if got != "from-l2" {
		t.Errorf("expected 'from-l2', got %q", got)
	}

	// Should now be in L1.
	var promoted string
	if !l1.Get(ctx, "k", &promoted) {
		t.Error("expected value promoted to L1 after L2 hit")
	}
}

func TestTiered_Miss(t *testing.T) {
	t.Parallel()
	tc := NewTiered(NewMemory(), NewMemory())
	var got string
	if tc.Get(context.Background(), "nonexistent", &got) {
		t.Error("expected cache miss from both tiers")
	}
}

func TestTiered_SetStoresBoth(t *testing.T) {
	t.Parallel()
	l1 := NewMemory()
	l2 := NewMemory()
	tc := NewTiered(l1, l2)
	ctx := context.Background()

	tc.Set(ctx, "k", "value", time.Minute)

	var got1, got2 string
	if !l1.Get(ctx, "k", &got1) {
		t.Error("expected value in L1 after Tiered.Set")
	}
	if !l2.Get(ctx, "k", &got2) {
		t.Error("expected value in L2 after Tiered.Set")
	}
}

func TestTiered_WithRedis(t *testing.T) {
	t.Parallel()
	l1 := NewMemory()
	r, _ := newTestRedis(t)
	tc := NewTiered(l1, r)
	ctx := context.Background()

	tc.Set(ctx, "k", "redis-value", time.Minute)

	// Clear L1 to force L2 read.
	l1new := NewMemory()
	tc2 := NewTiered(l1new, r)

	var got string
	if !tc2.Get(ctx, "k", &got) {
		t.Fatal("expected hit from Redis L2")
	}
	if got != "redis-value" {
		t.Errorf("expected 'redis-value', got %q", got)
	}
}

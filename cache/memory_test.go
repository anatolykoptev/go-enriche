package cache

import (
	"context"
	"testing"
	"time"
)

func TestMemory_SetGet(t *testing.T) {
	t.Parallel()
	m := NewMemory()
	ctx := context.Background()

	m.Set(ctx, "key1", "hello", time.Minute)
	var got string
	if !m.Get(ctx, "key1", &got) {
		t.Fatal("expected cache hit")
	}
	if got != "hello" {
		t.Errorf("expected 'hello', got %q", got)
	}
}

func TestMemory_Miss(t *testing.T) {
	t.Parallel()
	m := NewMemory()
	var got string
	if m.Get(context.Background(), "nonexistent", &got) {
		t.Error("expected cache miss")
	}
}

func TestMemory_Expiry(t *testing.T) {
	t.Parallel()
	m := NewMemory()
	ctx := context.Background()

	m.Set(ctx, "exp", "value", time.Millisecond)
	time.Sleep(5 * time.Millisecond)

	var got string
	if m.Get(ctx, "exp", &got) {
		t.Error("expected cache miss after TTL expiry")
	}
}

func TestMemory_NoExpiry(t *testing.T) {
	t.Parallel()
	m := NewMemory()
	ctx := context.Background()

	m.Set(ctx, "forever", "value", 0)
	var got string
	if !m.Get(ctx, "forever", &got) {
		t.Fatal("expected cache hit with zero TTL")
	}
}

func TestMemory_Struct(t *testing.T) {
	t.Parallel()
	type item struct {
		Name  string
		Count int
	}
	m := NewMemory()
	ctx := context.Background()

	m.Set(ctx, "item", item{Name: "test", Count: 42}, time.Minute)
	var got item
	if !m.Get(ctx, "item", &got) {
		t.Fatal("expected cache hit")
	}
	if got.Name != "test" || got.Count != 42 {
		t.Errorf("unexpected struct: %+v", got)
	}
}

func TestMemory_Overwrite(t *testing.T) {
	t.Parallel()
	m := NewMemory()
	ctx := context.Background()

	m.Set(ctx, "k", "v1", time.Minute)
	m.Set(ctx, "k", "v2", time.Minute)
	var got string
	if !m.Get(ctx, "k", &got) {
		t.Fatal("expected cache hit")
	}
	if got != "v2" {
		t.Errorf("expected 'v2' after overwrite, got %q", got)
	}
}

package cache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestRedis(t *testing.T) (*Redis, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return NewRedis(client), mr
}

func TestRedis_SetGet(t *testing.T) {
	t.Parallel()
	r, _ := newTestRedis(t)
	ctx := context.Background()

	r.Set(ctx, "key1", "hello", time.Minute)
	var got string
	if !r.Get(ctx, "key1", &got) {
		t.Fatal("expected cache hit")
	}
	if got != "hello" {
		t.Errorf("expected 'hello', got %q", got)
	}
}

func TestRedis_Miss(t *testing.T) {
	t.Parallel()
	r, _ := newTestRedis(t)
	var got string
	if r.Get(context.Background(), "nonexistent", &got) {
		t.Error("expected cache miss")
	}
}

func TestRedis_Expiry(t *testing.T) {
	t.Parallel()
	r, mr := newTestRedis(t)
	ctx := context.Background()

	r.Set(ctx, "exp", "value", time.Second)
	mr.FastForward(2 * time.Second)

	var got string
	if r.Get(ctx, "exp", &got) {
		t.Error("expected cache miss after TTL expiry")
	}
}

func TestRedis_Struct(t *testing.T) {
	t.Parallel()
	type item struct {
		Name  string
		Count int
	}
	r, _ := newTestRedis(t)
	ctx := context.Background()

	r.Set(ctx, "item", item{Name: "test", Count: 42}, time.Minute)
	var got item
	if !r.Get(ctx, "item", &got) {
		t.Fatal("expected cache hit")
	}
	if got.Name != "test" || got.Count != 42 {
		t.Errorf("unexpected struct: %+v", got)
	}
}

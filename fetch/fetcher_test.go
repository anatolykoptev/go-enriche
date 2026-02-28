package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFetch_Success(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><body>Hello</body></html>"))
	}))
	defer srv.Close()

	f := NewFetcher()
	result, err := f.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != StatusActive {
		t.Errorf("expected active, got %s", result.Status)
	}
	if result.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", result.StatusCode)
	}
	if !strings.Contains(result.HTML, "Hello") {
		t.Errorf("expected body containing Hello, got %s", result.HTML)
	}
}

func TestFetch_NotFound(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	f := NewFetcher()
	result, err := f.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != StatusNotFound {
		t.Errorf("expected not_found, got %s", result.Status)
	}
	if result.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", result.StatusCode)
	}
}

func TestFetch_ServerError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	f := NewFetcher()
	result, err := f.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != StatusUnreachable {
		t.Errorf("expected unreachable, got %s", result.Status)
	}
}

func TestFetch_DomainRedirect(t *testing.T) {
	t.Parallel()

	// Target server (different domain in effect).
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("redirected"))
	}))
	defer target.Close()

	// Origin server redirects to target.
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/new-page", http.StatusMovedPermanently)
	}))
	defer origin.Close()

	f := NewFetcher()
	result, err := f.Fetch(context.Background(), origin.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != StatusRedirect {
		t.Errorf("expected redirect, got %s", result.Status)
	}
	if result.FinalURL == "" {
		t.Error("expected non-empty FinalURL for redirect")
	}
}

func TestFetch_SameDomainRedirect(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/page", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("final page"))
	}))
	defer srv.Close()

	f := NewFetcher()
	result, err := f.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != StatusActive {
		t.Errorf("same-domain redirect should be active, got %s", result.Status)
	}
	if !strings.Contains(result.HTML, "final page") {
		t.Error("expected body from final redirect destination")
	}
}

func TestFetch_Timeout(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := NewFetcher(WithTimeout(100 * time.Millisecond))
	result, err := f.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != StatusUnreachable {
		t.Errorf("expected unreachable on timeout, got %s", result.Status)
	}
}

func TestFetch_MaxBodyBytes(t *testing.T) {
	t.Parallel()
	bigBody := strings.Repeat("X", 1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(bigBody))
	}))
	defer srv.Close()

	f := NewFetcher(WithMaxBodyBytes(100))
	result, err := f.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.HTML) > 100 {
		t.Errorf("expected body truncated to 100 bytes, got %d", len(result.HTML))
	}
}

func TestFetch_EmptyURL(t *testing.T) {
	t.Parallel()
	f := NewFetcher()
	_, err := f.Fetch(context.Background(), "")
	if err == nil {
		t.Error("expected error for empty URL")
	}
}

func TestFetch_InvalidURL(t *testing.T) {
	t.Parallel()
	f := NewFetcher()
	result, err := f.Fetch(context.Background(), "not-a-url")
	if err != nil {
		return // Error is acceptable.
	}
	if result.Status != StatusUnreachable {
		t.Errorf("expected unreachable for invalid URL, got %s", result.Status)
	}
}

func TestFetch_Singleflight(t *testing.T) {
	t.Parallel()
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount.Add(1)
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	f := NewFetcher()
	var wg sync.WaitGroup
	const concurrency = 10
	results := make([]*FetchResult, concurrency)
	errors := make([]error, concurrency)

	for i := range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errors[i] = f.Fetch(context.Background(), srv.URL)
		}()
	}
	wg.Wait()

	for i := range concurrency {
		if errors[i] != nil {
			t.Errorf("goroutine %d error: %v", i, errors[i])
		}
		if results[i] == nil || results[i].Status != StatusActive {
			t.Errorf("goroutine %d: expected active status", i)
		}
	}

	if got := callCount.Load(); got != 1 {
		t.Errorf("singleflight: expected 1 server hit, got %d", got)
	}
}

func TestFetch_ContextCanceled(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	f := NewFetcher()
	result, err := f.Fetch(ctx, srv.URL)
	if err != nil {
		return // Error for canceled context is acceptable.
	}
	if result.Status != StatusUnreachable {
		t.Errorf("expected unreachable for canceled context, got %s", result.Status)
	}
}

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

// newUnguardedFetcher builds a Fetcher whose client skips the default SSRF
// guard (see go-kit httputil.NewSSRFGuardedClient, wired in NewFetcher /
// fetcher.go, and ssrf_test.go), for tests exercising fetcher behavior
// (redirects, timeouts, singleflight, body limits) against a local httptest
// server — a legitimate loopback target in a test, but one the guarded
// default correctly refuses. Uses the pre-existing WithClient escape hatch,
// same as any other caller opting out of the default guard.
func newUnguardedFetcher(opts ...Option) *Fetcher {
	base := []Option{WithClient(&http.Client{Timeout: DefaultTimeout})}
	return NewFetcher(append(base, opts...)...)
}

func TestFetch_Success(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><body>Hello</body></html>"))
	}))
	defer srv.Close()

	f := newUnguardedFetcher()
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

	f := newUnguardedFetcher()
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

	f := newUnguardedFetcher()
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

	f := newUnguardedFetcher()
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

	f := newUnguardedFetcher()
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

	f := newUnguardedFetcher(WithTimeout(100 * time.Millisecond))
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

	f := newUnguardedFetcher(WithMaxBodyBytes(100))
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
	f := newUnguardedFetcher()
	_, err := f.Fetch(context.Background(), "")
	if err == nil {
		t.Error("expected error for empty URL")
	}
}

func TestFetch_InvalidURL(t *testing.T) {
	t.Parallel()
	f := newUnguardedFetcher()
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

	f := newUnguardedFetcher()
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

	f := newUnguardedFetcher()
	result, err := f.Fetch(ctx, srv.URL)
	if err != nil {
		return // Error for canceled context is acceptable.
	}
	if result.Status != StatusUnreachable {
		t.Errorf("expected unreachable for canceled context, got %s", result.Status)
	}
}

// TestFetch_SetsUserAgent proves WithUserAgent's header lands on the actual
// outbound request, using the same unguarded-fetcher-over-httptest pattern as
// every other fetcher-behavior test in this file.
func TestFetch_SetsUserAgent(t *testing.T) {
	t.Parallel()
	const ua = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36"
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := newUnguardedFetcher(WithUserAgent(ua))
	if _, err := f.Fetch(context.Background(), srv.URL); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotUA != ua {
		t.Errorf("User-Agent = %q, want %q", gotUA, ua)
	}
}

// TestFetch_NoUserAgent_DefaultGoUA pins the pre-WithUserAgent behavior this
// package has always had: absent WithUserAgent, doFetch sets no explicit
// User-Agent header at all, so net/http falls back to its own default
// ("Go-http-client/1.1"). This is the exact gap the go-wp #190 review found
// (a bare Go UA can make some sites serve degraded/blocked content, breaking
// verdict-neutrality against a browser-UA baseline) — this test documents
// the default so a future accidental change to it is caught here, not
// discovered downstream.
func TestFetch_NoUserAgent_DefaultGoUA(t *testing.T) {
	t.Parallel()
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := newUnguardedFetcher()
	if _, err := f.Fetch(context.Background(), srv.URL); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(gotUA, "Go-http-client/") {
		t.Errorf("User-Agent = %q, want the net/http default (Go-http-client/*) — WithUserAgent not set", gotUA)
	}
}

// TestNewFetcher_WithUserAgent_KeepsDefaultSSRFGuard proves WithUserAgent is
// a pure request-header option that does NOT disturb NewFetcher's default
// client construction (httputil.NewSSRFGuardedClient(&http.Client{...}),
// the STRONG connect-time-guarded tier — see NewFetcher's and WithUserAgent's
// doc comments). Mirrors ssrf_test.go's TestFetcher_RefusesBlockedTargets
// exactly, with WithUserAgent added, on the REAL guarded default (no
// WithClient escape hatch here, unlike every other test in this file) — a
// regression where a future WithUserAgent implementation wrapped f.client in
// a RoundTripper instead of setting a request header would either break this
// (if it replaced the guarded client) or pass vacuously; combined with
// TestFetch_SetsUserAgent above (which proves the header actually lands),
// the pair proves the option is additive-only.
func TestNewFetcher_WithUserAgent_KeepsDefaultSSRFGuard(t *testing.T) {
	t.Parallel()
	f := NewFetcher(WithUserAgent("Mozilla/5.0 test-probe"))
	result, err := f.Fetch(context.Background(), "http://127.0.0.1:1/x")
	if err != nil {
		t.Fatalf("unexpected Fetch error: %v", err)
	}
	if result.Status != StatusUnreachable {
		t.Errorf("Fetch(loopback) = status %s, want %s (blocked) — WithUserAgent must not weaken or bypass the default SSRF guard", result.Status, StatusUnreachable)
	}
}

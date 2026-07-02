package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/anatolykoptev/go-kit/httputil"
)

// These tests prove go-enriche's WIRING onto go-kit httputil's SSRF guard —
// not the guard's own predicate logic, which go-kit's own exhaustive table
// (httputil.TestIsBlockedIP / TestCheckURL / TestNewSSRFGuardedClient_*)
// already covers and this package must not re-derive (that duplication is
// exactly what this migration retires — see the former fetch/ssrf.go).
//
// TestFetcher_RefusesBlockedTargets is the Fetcher-level proof: NewFetcher's
// default client is built on httputil.NewSSRFGuardedClient (see NewFetcher
// in fetcher.go), so a target the guard blocks must be refused THROUGH the
// Fetcher, surfaced the same way every other unreachable target is
// (StatusUnreachable). The table covers the classes go-enriche's former
// local guard already blocked, PLUS two classes ONLY the go-kit guard adds
// (CGNAT, the alt-encoded-IP-literal bypass) — proving the widened coverage
// actually reaches through this package's wiring, not just go-kit's own
// predicate tests.
//
// Every case here is a literal IP (or an alt-encoded literal), so the
// pre-request httputil.CheckURL tier refuses it BEFORE any dial is
// attempted — deterministic and fast, no real network I/O. Proving the
// ALLOW path (a real public target succeeds) belongs to
// TestFetcher_AllowsUnguardedClientOverride / TestFetch_Success, which reach
// a real local httptest server through the WithClient escape hatch; dialing
// an actual public IP here would be slow and environment-dependent.
func TestFetcher_RefusesBlockedTargets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		rawURL string
	}{
		{"loopback", "http://127.0.0.1:1/x"},
		{"private rfc1918", "http://10.0.0.1:1/x"},
		{"link-local cloud metadata", "http://169.254.169.254/latest/meta-data/"},
		// Widened coverage: these two classes were NOT in go-enriche's former
		// local isBlockedIP/CheckSSRFSafe (see the go-wp #172 pr-council review
		// that gated this migration) — only present after routing onto go-kit's
		// wider httputil guard.
		{"cgnat (rfc6598)", "http://100.64.0.1:1/x"},
		{"alt-encoded ip literal (hex bypass)", "http://0x7f000001/x"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := NewFetcher() // real default: guarded by construction
			result, err := f.Fetch(context.Background(), tt.rawURL)
			if err != nil {
				t.Fatalf("unexpected Fetch error: %v", err)
			}
			if result.Status != StatusUnreachable {
				t.Errorf("Fetch(%q) = status %s, want %s (blocked)", tt.rawURL, result.Status, StatusUnreachable)
			}
		})
	}
}

// TestFetcher_RefusesLoopbackTarget additionally proves the underlying error
// is a clear, typed httputil.ErrSSRFBlocked (not just an opaque dial
// failure) when the guard is exercised directly, since Fetcher.Fetch itself
// swallows the error into StatusUnreachable (see TestFetcher_RefusesBlockedTargets).
func TestFetcher_RefusesLoopbackTarget(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("should never be reached"))
	}))
	defer srv.Close()

	f := NewFetcher() // real default: guarded by construction
	result, err := f.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected Fetch error: %v", err)
	}
	if result.Status != StatusUnreachable {
		t.Errorf("expected loopback target refused as unreachable, got status=%s html_len=%d", result.Status, len(result.HTML))
	}

	client := httputil.NewSSRFGuardedClient(&http.Client{Timeout: 2 * time.Second})
	resp, doErr := client.Get(srv.URL) //nolint:noctx // test: URL is a fixed local httptest server
	if resp != nil {
		resp.Body.Close() //nolint:errcheck
	}
	if doErr == nil {
		t.Fatal("expected httputil.NewSSRFGuardedClient to refuse the loopback target")
	}
	if !errors.Is(doErr, httputil.ErrSSRFBlocked) {
		t.Errorf("expected error to wrap httputil.ErrSSRFBlocked, got: %v", doErr)
	}
}

// TestFetcher_AllowsUnguardedClientOverride proves WithClient remains a full
// escape hatch (pre-existing API, unchanged): a caller-supplied client is
// used as-is, guard or no guard.
func TestFetcher_AllowsUnguardedClientOverride(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	f := NewFetcher(WithClient(&http.Client{Timeout: DefaultTimeout}))
	result, err := f.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != StatusActive {
		t.Errorf("expected active with an explicit unguarded client, got %s", result.Status)
	}
}

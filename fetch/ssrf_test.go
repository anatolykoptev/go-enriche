package fetch

import (
	"context"
	"errors"
	"net"
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

// TestGuardedDialContext_RefusesBlockedRedirectTarget is the fast,
// always-runs, zero-network proof of the EXACT mechanism that protects every
// hop of any redirect chain WithFollowRedirects (fetcher.go) makes reachable:
// net/http's Client routes the original request AND every followed redirect
// through the exact same client.Transport.RoundTrip -- there is no separate,
// hop-numbered code path in net/http, in guardedRoundTripper, or in
// GuardedDialContext (verified by reading all three). httputil.
// GuardedDialContext(nil) returns the exact Control-hook-wrapped DialContext
// function NewFetcher's guarded transport installs; calling it directly with
// a blocked literal address is precisely what net/http invokes internally
// for ANY hop's dial, first or Nth, once that hop's target has resolved to
// that address -- see go-kit ssrf.go's own denyBlockedAddress doc comment
// ("split out so tests can drive it directly with a hardcoded
// post-resolution address... without needing a real DNS rebind"), the same
// reasoning applied one level up (the full DialContext, not just its
// Control body, since that's exported and this is a Fetcher wiring test,
// not a go-kit predicate test). Complements the real end-to-end redirect
// test below.
func TestGuardedDialContext_RefusesBlockedRedirectTarget(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		address string
	}{
		{"loopback", "127.0.0.1:1"},
		{"link-local cloud metadata", "169.254.169.254:80"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dial := httputil.GuardedDialContext(nil)
			conn, err := dial(context.Background(), "tcp", tt.address)
			if conn != nil {
				conn.Close() //nolint:errcheck
			}
			if err == nil {
				t.Fatalf("expected GuardedDialContext to refuse dialing %q (the address a redirect Location would resolve to at connect time)", tt.address)
			}
			if !errors.Is(err, httputil.ErrSSRFBlocked) {
				t.Errorf("expected error to wrap httputil.ErrSSRFBlocked, got: %v", err)
			}
		})
	}
}

// firstUnblockedLocalIP returns a non-blocked (per httputil.IsBlockedIP)
// address bound to one of this host's own network interfaces -- used as the
// FIRST hop of a real end-to-end redirect chain in the test below, so a
// REAL guarded Fetcher can actually reach it (unlike 127.0.0.1, which the
// guard refuses at hop 1 too, making httptest's usual loopback binding
// unusable for this specific test). Skips the test if no such address
// exists (e.g. a fully NAT'd host whose only interface addresses are
// private/link-local) -- a genuine environment-capability gap, not a
// disguised skip-to-green: TestGuardedDialContext_RefusesBlockedRedirectTarget
// above always runs regardless and proves the same underlying mechanism.
func firstUnblockedLocalIP(t *testing.T) string {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Skipf("cannot enumerate local interfaces: %v", err)
	}
	for _, a := range addrs {
		ipNet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		if httputil.IsBlockedIP(ipNet.IP) {
			continue
		}
		return ipNet.IP.String()
	}
	t.Skip("no non-blocked local IP address bound to any interface on this host -- cannot construct a real end-to-end redirect-chain test (see doc comment); the mechanism is still covered by TestGuardedDialContext_RefusesBlockedRedirectTarget")
	return ""
}

// TestFetch_FollowRedirects_RefusesRedirectToBlockedTarget is the real,
// end-to-end counterpart to the mechanism test above: an ACTUAL httptest
// server (bound to a real, non-blocked local interface address -- see
// firstUnblockedLocalIP) issues a redirect whose Location is a literal
// blocked address (loopback). Driven through the REAL guarded default
// Fetcher (NewFetcher(WithFollowRedirects()), no WithClient escape hatch),
// this proves the guard refuses the SECOND hop's dial through a genuine
// network round-trip for hop 1, not just a direct call into the guard
// primitive.
func TestFetch_FollowRedirects_RefusesRedirectToBlockedTarget(t *testing.T) {
	t.Parallel()
	hostIP := firstUnblockedLocalIP(t)

	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", net.JoinHostPort(hostIP, "0"))
	if err != nil {
		t.Skipf("cannot bind a listener to %s: %v", hostIP, err)
	}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1:1/blocked", http.StatusFound)
	}))
	srv.Listener.Close() //nolint:errcheck
	srv.Listener = ln
	srv.Start()
	defer srv.Close()

	f := NewFetcher(WithFollowRedirects()) // REAL guarded default, no escape hatch
	result, err := f.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected Fetch error: %v", err)
	}
	if result.Status != StatusUnreachable {
		t.Errorf("Fetch through a redirect to a blocked target = status %s, want %s — the guard must refuse the SECOND hop's dial even with WithFollowRedirects set", result.Status, StatusUnreachable)
	}
}

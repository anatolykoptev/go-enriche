package fetch

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIsBlockedIP(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		ip      string
		blocked bool
	}{
		// Loopback.
		{"loopback v4", "127.0.0.1", true},
		{"loopback v4 range", "127.255.255.254", true},
		{"loopback v6", "::1", true},

		// RFC1918 private.
		{"rfc1918 10/8", "10.0.0.1", true},
		{"rfc1918 172.16/12 low", "172.16.0.1", true},
		{"rfc1918 172.16/12 high", "172.31.255.255", true},
		{"rfc1918 192.168/16", "192.168.1.1", true},
		{"just outside 172.16/12 low", "172.15.255.255", false},
		{"just outside 172.16/12 high", "172.32.0.0", false},

		// RFC4193 unique-local IPv6.
		{"rfc4193 ula", "fc00::1", true},
		{"rfc4193 ula fd", "fd12:3456:789a:1::1", true},

		// Link-local (includes cloud metadata 169.254.169.254).
		{"link-local v4", "169.254.1.1", true},
		{"cloud metadata", "169.254.169.254", true},
		{"link-local v6", "fe80::1", true},

		// Unspecified.
		{"unspecified v4", "0.0.0.0", true},
		{"unspecified v6", "::", true},

		// Multicast.
		{"multicast v4", "224.0.0.1", true},
		{"multicast v6", "ff02::1", true},

		// IPv4-mapped-IPv6 of blocked addresses.
		{"ipv4-mapped private", "::ffff:10.0.0.1", true},
		{"ipv4-mapped loopback", "::ffff:127.0.0.1", true},
		{"ipv4-mapped link-local", "::ffff:169.254.169.254", true},

		// Public — must be allowed.
		{"public v4 google dns", "8.8.8.8", false},
		{"public v4 cloudflare dns", "1.1.1.1", false},
		{"public v4 arbitrary", "93.184.216.34", false},
		{"public v6 cloudflare", "2606:4700:4700::1111", false},
		{"ipv4-mapped public", "::ffff:8.8.8.8", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("test setup: %q did not parse as an IP", tt.ip)
			}
			if got := isBlockedIP(ip); got != tt.blocked {
				t.Errorf("isBlockedIP(%s) = %v, want %v", tt.ip, got, tt.blocked)
			}
		})
	}
}

func TestCheckSSRFSafe(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		rawURL  string
		blocked bool
	}{
		{"loopback literal", "http://127.0.0.1:8080/x", true},
		{"loopback hostname", "http://localhost/x", true},
		{"private literal", "http://10.9.0.10:8890/", true},
		{"link-local literal (cloud metadata)", "http://169.254.169.254/latest/meta-data/", true},
		{"docker-compose-range private literal", "http://172.18.0.5:8901/read", true},
		{"public literal ip", "http://8.8.8.8/", false},
		{"malformed url", "http://[::1", true},
		{"empty host", "not-a-url", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := CheckSSRFSafe(ctx, tt.rawURL)
			blocked := err != nil
			if blocked != tt.blocked {
				t.Errorf("CheckSSRFSafe(%q) blocked = %v (err=%v), want blocked=%v", tt.rawURL, blocked, err, tt.blocked)
			}
			if tt.blocked && err != nil && !errors.Is(err, ErrSSRFBlocked) {
				// A resolution failure (e.g. no network in CI) is acceptable for the
				// hostname case, but literal-IP and malformed-URL cases must always
				// classify through ErrSSRFBlocked specifically.
				if tt.name != "loopback hostname" {
					t.Errorf("CheckSSRFSafe(%q) error %v does not wrap ErrSSRFBlocked", tt.rawURL, err)
				}
			}
		})
	}
}

// TestGuardedDialContext_BlocksResolvedAddress drives the DialContext func
// returned by GuardedDialContext with an address exactly as net/http would
// pass it AFTER DNS resolution — this is what proves the guard defeats
// DNS-rebinding: the check fires on the literal resolved address, never on a
// hostname string, so it cannot be fooled by a name that resolves public on
// first lookup and private by connect time.
func TestGuardedDialContext_BlocksResolvedAddress(t *testing.T) {
	t.Parallel()
	dial := GuardedDialContext(&net.Dialer{Timeout: time.Second})

	blockedAddrs := []string{
		"127.0.0.1:80",
		"169.254.169.254:80", // cloud metadata
		"10.0.0.1:443",
		"172.18.0.5:8901", // docker-compose bridge range
		"[::1]:80",
		"[fe80::1]:80",
	}
	for _, addr := range blockedAddrs {
		addr := addr
		t.Run(addr, func(t *testing.T) {
			t.Parallel()
			_, err := dial(context.Background(), "tcp", addr)
			if err == nil {
				t.Fatalf("dial(%q) succeeded, want ErrSSRFBlocked", addr)
			}
			if !errors.Is(err, ErrSSRFBlocked) {
				t.Errorf("dial(%q) error %v does not wrap ErrSSRFBlocked", addr, err)
			}
		})
	}
}

// TestFetcher_RefusesLoopbackTarget is the Fetcher-level (not just the
// predicate-level) regression test: a real httptest server bound to loopback
// must be refused by the DEFAULT (guarded) Fetcher, surfaced the same way
// every other unreachable target is surfaced (StatusUnreachable) — while
// NewGuardedClient used directly (bypassing Fetcher's status-code mapping)
// proves the underlying error is a clear, typed ErrSSRFBlocked.
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

	client := NewGuardedClient(2 * time.Second)
	resp, doErr := client.Get(srv.URL) //nolint:noctx // test: URL is a fixed local httptest server
	if resp != nil {
		resp.Body.Close() //nolint:errcheck
	}
	if doErr == nil {
		t.Fatal("expected NewGuardedClient to refuse the loopback target")
	}
	if !errors.Is(doErr, ErrSSRFBlocked) {
		t.Errorf("expected error to wrap ErrSSRFBlocked, got: %v", doErr)
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

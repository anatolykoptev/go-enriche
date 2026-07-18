package fetch

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anatolykoptev/go-kit/httputil"
)

// These tests prove NewStealthClient's WIRING onto go-kit/httputil's
// SSRFGuards() -- the fleet SSRF fix step 3a/4 (see
// internal SSRF remediation plan,
// blast-radius row 7: this is the DIRECT, no-proxy stealth client, the
// highest-risk row -- any caller that omits StealthWithProxy dials the
// target straight from this container).
//
// go-stealth v1.18.0 ships its OWN stdlib-only default-deny floor (loopback /
// private / link-local -- see go-stealth's ssrf.go isBlockedIP), so a
// BrowserClient is fail-closed BY CONSTRUCTION even with zero guard options.
// That means a bare "redirect to 127.0.0.1 gets blocked" assertion is
// VACUOUS here: it would pass identically whether or not this package wires
// anything at all, because go-stealth's own floor already catches loopback.
// The genuine differentiator -- and what actually proves THIS package's
// wiring fired, not just go-stealth's fallback -- is the ERROR IDENTITY:
// go-stealth's built-in guards wrap stealth.ErrSSRFBlocked; go-kit's guards
// (wired below via SSRFGuards()/CheckURL) wrap httputil.ErrSSRFBlocked.
// Reverting NewStealthClient's wiring still blocks the loopback redirect
// (go-stealth's floor remains) but the error stops being
// httputil.ErrSSRFBlocked -- this assertion goes RED without a client
// ever reaching an actually-vulnerable state, which is exactly why the
// error-identity check (not just "an error occurred") is the correct proof.
func TestNewStealthClient_DefaultGuardsRefuseRedirectToLoopback(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		opts []StealthOption
	}{
		{"std backend", []StealthOption{StealthWithStdHTTP()}},
		{"tls-client backend (default, production)", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			hostIP := firstUnblockedLocalIP(t)

			var lc net.ListenConfig
			ln, err := lc.Listen(context.Background(), "tcp", net.JoinHostPort(hostIP, "0"))
			if err != nil {
				t.Skipf("cannot bind a listener to %s: %v", hostIP, err)
			}

			srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "http://127.0.0.1:1/pwn", http.StatusFound)
			}))
			srv.Listener.Close() //nolint:errcheck
			srv.Listener = ln
			srv.Start()
			defer srv.Close()

			client, err := NewStealthClient(tt.opts...)
			if err != nil {
				t.Fatalf("NewStealthClient error: %v", err)
			}

			resp, doErr := client.Get(srv.URL) //nolint:noctx,gosec // test: URL is a fixed local httptest server
			if resp != nil {
				resp.Body.Close() //nolint:errcheck
			}
			if doErr == nil {
				t.Fatal("expected the redirect to a loopback target to be refused")
			}
			if !errors.Is(doErr, httputil.ErrSSRFBlocked) {
				t.Errorf("expected error to wrap httputil.ErrSSRFBlocked (proving go-kit's SSRFGuards() wiring fired, not just go-stealth's own default-deny floor), got: %v", doErr)
			}
		})
	}
}

// TestNewStealthClient_WithoutSSRFGuard_AllowsLoopback is the positive
// control paired with the test above: it proves StealthWithoutSSRFGuard
// actually reaches through to go-stealth's WithoutSSRFGuard (not a no-op),
// and -- by contrast with the default-guarded case blocking the exact same
// kind of target above -- confirms the default path's block is the guard
// firing, not some unrelated failure (e.g. a bad test server).
func TestNewStealthClient_WithoutSSRFGuard_AllowsLoopback(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	client, err := NewStealthClient(StealthWithStdHTTP(), StealthWithoutSSRFGuard())
	if err != nil {
		t.Fatalf("NewStealthClient error: %v", err)
	}

	resp, doErr := client.Get(srv.URL) //nolint:noctx,gosec // test: fixed local httptest server, guard intentionally disabled
	if doErr != nil {
		t.Fatalf("expected StealthWithoutSSRFGuard to allow a loopback fetch, got error: %v", doErr)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

package fetch

import (
	"net/http"

	"github.com/anatolykoptev/go-kit/httputil"
	stealth "github.com/anatolykoptev/go-stealth"
)

// StealthOption configures the stealth client.
type StealthOption func(*[]stealth.ClientOption)

// StealthWithTimeout sets the request timeout in seconds.
func StealthWithTimeout(seconds int) StealthOption {
	return func(opts *[]stealth.ClientOption) {
		*opts = append(*opts, stealth.WithTimeout(seconds))
	}
}

// StealthWithProxy sets an HTTP/SOCKS5 proxy URL.
func StealthWithProxy(proxyURL string) StealthOption {
	return func(opts *[]stealth.ClientOption) {
		*opts = append(*opts, stealth.WithProxy(proxyURL))
	}
}

// StealthWithProfile sets the TLS fingerprint profile.
func StealthWithProfile(profile stealth.TLSProfile) StealthOption {
	return func(opts *[]stealth.ClientOption) {
		*opts = append(*opts, stealth.WithProfile(profile))
	}
}

// StealthWithStdHTTP uses the stdlib net/http backend (no TLS fingerprinting).
func StealthWithStdHTTP() StealthOption {
	return func(opts *[]stealth.ClientOption) {
		*opts = append(*opts, stealth.WithStdHTTP())
	}
}

// StealthWithoutSSRFGuard disables all three SSRF guard tiers NewStealthClient
// wires by default (dial, redirect, request-URL). FOR TESTS ONLY: it restores
// pre-guard behavior so an httptest (loopback) suite can fetch a local
// server. Never call this from production code — it is applied LAST, after
// any other options, so it always wins over the default wiring. The fleet
// fitness function (see the multi-stealth-ssrf plan) forbids this outside
// _test.go.
func StealthWithoutSSRFGuard() StealthOption {
	return func(opts *[]stealth.ClientOption) {
		*opts = append(*opts, stealth.WithoutSSRFGuard())
	}
}

const defaultStealthTimeoutSec = 15

// NewStealthClient creates an *http.Client with TLS fingerprinting via go-stealth.
// The returned client can be passed to NewFetcher via WithClient.
//
// Guarded by default: this is go-enriche's DIRECT (no-proxy-required)
// stealth client — the highest-risk fetch path in the fleet SSRF review
// (multi-stealth-ssrf plan, blast-radius row 7), since any caller that omits
// StealthWithProxy dials the target straight from this container. go-stealth
// itself is fail-closed by construction (a stdlib-only floor: loopback /
// private / link-local), but that floor omits CGNAT, NAT64/6to4, and
// alt-encoded-IP literals — so NewStealthClient wires go-kit/httputil's
// fuller SSRFGuards() policy onto all three of go-stealth's guard tiers:
// dial (connect-time, rebind-proof — closes the redirect-swallow gap since
// go-stealth's backends follow redirects internally without the outer
// *http.Client ever re-observing a 3xx), redirect (per-hop, defense in
// depth), and request-URL (pre-request — the only tier that guards a
// PROXIED fetch's initial target, since dial control there only ever sees
// the proxy). See StealthWithoutSSRFGuard for the test-only escape hatch.
func NewStealthClient(opts ...StealthOption) (*http.Client, error) {
	redirect, dial := httputil.SSRFGuards()
	stealthOpts := []stealth.ClientOption{
		stealth.WithTimeout(defaultStealthTimeoutSec),
		stealth.WithDialControl(dial),
		stealth.WithRedirectGuard(redirect),
		stealth.WithRequestURLGuard(httputil.CheckURL),
	}
	for _, o := range opts {
		o(&stealthOpts)
	}

	bc, err := stealth.NewClient(stealthOpts...)
	if err != nil {
		return nil, err
	}
	return bc.StdClient(), nil
}

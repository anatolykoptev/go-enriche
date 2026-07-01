package fetch

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"syscall"
	"time"
)

// ErrSSRFBlocked wraps every error returned when a fetch target is loopback,
// private (RFC1918 / RFC4193 ULA), link-local (including the cloud-metadata
// address 169.254.169.254), unspecified, or multicast — the address classes
// an SSRF payload targets to reach internal infrastructure that must never be
// reachable from a caller-supplied fetch target (e.g. an advertiser-provided
// website URL).
var ErrSSRFBlocked = errors.New("fetch: SSRF-blocked address")

const (
	guardedDialTimeout   = 10 * time.Second
	guardedDialKeepAlive = 30 * time.Second
)

// isBlockedIP reports whether ip must never be dialed as a fetch target.
// Go's net.IP predicates already unwrap IPv4-mapped-IPv6 addresses (e.g.
// ::ffff:10.0.0.1 or ::ffff:127.0.0.1) to their IPv4 form before matching, so
// no separate normalization step is needed here — this predicate is correct
// for both address families as-is.
func isBlockedIP(ip net.IP) bool {
	return ip.IsLoopback() || // 127.0.0.0/8, ::1
		ip.IsPrivate() || // RFC1918 (10/8, 172.16/12, 192.168/16) + RFC4193 ULA (fc00::/7)
		ip.IsLinkLocalUnicast() || // 169.254.0.0/16 (incl. cloud metadata 169.254.169.254), fe80::/10
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || // 0.0.0.0, ::
		ip.IsMulticast()
}

// GuardedDialContext wraps base with a Control hook that refuses to connect
// to a blocked address (see isBlockedIP). The check runs on the
// ALREADY-RESOLVED address at connect time — after DNS lookup, immediately
// before the connect(2) syscall — which is what defeats DNS-rebinding: a
// hostname that resolves to a public IP when net/http first looks it up but
// resolves to a private IP by the time this fires is still caught, because
// the check inspects the literal address about to be dialed, never the
// hostname string. Any pre-existing Control hook on base still runs first.
func GuardedDialContext(base *net.Dialer) func(ctx context.Context, network, address string) (net.Conn, error) {
	d := *base // shallow copy: never mutate the caller's *net.Dialer
	prevControl := d.Control
	d.Control = func(network, address string, c syscall.RawConn) error {
		if prevControl != nil {
			if err := prevControl(network, address, c); err != nil {
				return err
			}
		}
		return denyBlockedAddress(network, address)
	}
	return d.DialContext
}

// denyBlockedAddress is the Control-hook body, split out so tests can drive
// it directly with a hardcoded post-resolution address (simulating exactly
// what net/http passes after DNS lookup) without needing a real DNS rebind.
func denyBlockedAddress(network, address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address // no port present — shouldn't happen for a tcp/udp dial target
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// A resolved dial target that isn't a literal IP is unexpected; fail
		// closed rather than let an unparseable address through.
		return fmt.Errorf("%w: cannot parse dial address %q (%s)", ErrSSRFBlocked, address, network)
	}
	if isBlockedIP(ip) {
		return fmt.Errorf("%w: %s (%s)", ErrSSRFBlocked, ip, network)
	}
	return nil
}

// guardedTransport returns an *http.Transport cloned from
// http.DefaultTransport (preserving its proxy / idle-conn / HTTP2 defaults)
// with DialContext replaced by GuardedDialContext, so every connection made
// through it is SSRF-safe by default.
func guardedTransport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	dialer := &net.Dialer{Timeout: guardedDialTimeout, KeepAlive: guardedDialKeepAlive}
	t.DialContext = GuardedDialContext(dialer)
	return t
}

// NewGuardedClient returns an *http.Client whose Transport refuses to connect
// to loopback / private / link-local / unspecified / multicast addresses
// (see isBlockedIP), suitable for fetching a caller-supplied, potentially
// untrusted URL.
func NewGuardedClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: guardedTransport()}
}

// CheckSSRFSafe resolves rawURL's host and returns an error wrapping
// ErrSSRFBlocked if ANY resolved address is blocked (see isBlockedIP).
//
// Use this to gate a URL BEFORE handing it to a delegate this package does
// not control the outbound dial for (e.g. an external headless-browser
// render service reached over its own HTTP client) — GuardedDialContext
// cannot protect that hop, because the delegate performs its own dial in a
// different process. This check is necessarily weaker than
// GuardedDialContext against DNS-rebinding — DNS can change between this
// resolution and the delegate's own dial — so call it as close as possible
// to the point of dispatch to minimize that window.
func CheckSSRFSafe(ctx context.Context, rawURL string) error {
	host, err := hostOf(rawURL)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrSSRFBlocked, err)
	}
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return fmt.Errorf("%w: %s", ErrSSRFBlocked, ip)
		}
		return nil
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("fetch: resolve %q: %w", host, err)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("%w: %q resolved to no addresses", ErrSSRFBlocked, host)
	}
	for _, a := range addrs {
		if isBlockedIP(a.IP) {
			return fmt.Errorf("%w: %s resolves to %s", ErrSSRFBlocked, host, a.IP)
		}
	}
	return nil
}

func hostOf(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse %q: %w", rawURL, err)
	}
	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("no host in %q", rawURL)
	}
	return host, nil
}

// GuardClient returns a shallow copy of c with the SSRF guard composed into
// its Transport, WITHOUT replacing or disturbing any of the Transport's
// existing configuration (TLS ClientHelloID/JA3 fingerprint, proxy,
// middleware, connection pooling, etc.) — safe to apply to a
// stealth/fingerprint-evasion client (see WithStealth in the enriche
// package), which is exactly what this exists for: WithClient (and
// WithStealth, which is built on it) REPLACES f.client wholesale, bypassing
// NewFetcher's default guardedTransport entirely.
//
// Two composition tiers, chosen by what c.Transport actually is:
//
//   - nil or *http.Transport: cloned (Clone() preserves TLSClientConfig,
//     proxy, HTTP2 settings, etc.) with ONLY DialContext wrapped by
//     GuardedDialContext — the STRONG, connect-time, DNS-rebind-proof tier,
//     identical to NewFetcher's own default guard.
//   - any other http.RoundTripper (e.g. a stealth client whose Transport
//     performs its own dial via a bespoke TLS-fingerprinting backend, with
//     no DialContext/net.Dialer hook exposed at all): wrapped with a
//     RoundTripper that runs CheckSSRFSafe on the outbound request's URL
//     before delegating — the same, necessarily WEAKER pre-resolve tier as
//     CheckSSRFSafe/checkTarget (a DNS-rebind can still occur between this
//     check and the delegate's own, separate resolution), but the best
//     guarantee available without reaching into a dial mechanism this
//     package does not own. This is still real protection: it refuses the
//     request outright for a target that resolves blocked at request time.
//
// c == nil returns nil unchanged (nothing to guard).
func GuardClient(c *http.Client) *http.Client {
	if c == nil {
		return nil
	}
	cc := *c
	switch t := c.Transport.(type) {
	case nil:
		cc.Transport = guardedTransport()
	case *http.Transport:
		tc := t.Clone()
		tc.DialContext = GuardedDialContext(&net.Dialer{Timeout: guardedDialTimeout, KeepAlive: guardedDialKeepAlive})
		cc.Transport = tc
	default:
		cc.Transport = &guardedRoundTripper{next: t}
	}
	return &cc
}

// guardedRoundTripper wraps an arbitrary http.RoundTripper with a
// pre-request SSRF check (see CheckSSRFSafe) on the outbound request's URL.
// This composes with ANY RoundTripper implementation — it never touches the
// wrapped one's internal dial mechanics, so a stealth/fingerprint-evasion
// implementation is untouched on the allow path.
type guardedRoundTripper struct {
	next http.RoundTripper
}

func (g *guardedRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := CheckSSRFSafe(req.Context(), req.URL.String()); err != nil {
		return nil, err
	}
	return g.next.RoundTrip(req)
}

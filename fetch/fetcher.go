package fetch

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/anatolykoptev/go-kit/httputil"
)

// Default configuration values.
const (
	DefaultMaxBodyBytes = 2 << 20 // 2 MB
	DefaultTimeout      = 15 * time.Second
	maxRedirects        = 5
)

// Fetcher performs HTTP page fetches with status detection and singleflight dedup.
type Fetcher struct {
	client       *http.Client
	maxBodyBytes int64
	sf           singleflight.Group
	// userAgent, when non-empty, is sent as the request's User-Agent header
	// (see WithUserAgent). Empty (the zero value) sends no explicit header at
	// all, so net/http falls back to its own default ("Go-http-client/1.1").
	userAgent string
	// followRedirects, when true, makes doFetch follow a cross-domain
	// redirect through to its final destination instead of aborting with
	// StatusRedirect + an empty body (see WithFollowRedirects). Default
	// false preserves the pre-existing behavior byte-for-byte.
	followRedirects bool
}

// Option configures a Fetcher.
type Option func(*Fetcher)

// WithClient sets a custom HTTP client (e.g., stealth-configured).
// When using WithClient, set the timeout on the provided client directly
// rather than via WithTimeout, as option order affects which client is mutated.
func WithClient(c *http.Client) Option {
	return func(f *Fetcher) { f.client = c }
}

// WithMaxBodyBytes sets the maximum response body size.
func WithMaxBodyBytes(n int64) Option {
	return func(f *Fetcher) { f.maxBodyBytes = n }
}

// WithTimeout sets the HTTP client timeout.
func WithTimeout(d time.Duration) Option {
	return func(f *Fetcher) { f.client.Timeout = d }
}

// WithUserAgent sets the User-Agent header doFetch sends on every request.
// NewFetcher's default (no WithUserAgent) sets NO explicit header, so
// net/http sends its own default ("Go-http-client/1.1") — distinguishable
// from a real browser, which some sites serve degraded or blocked content
// to. This is a REQUEST-level header set inside doFetch, deliberately not a
// client/RoundTripper wrap: NewFetcher's default client must stay exactly
// `httputil.NewSSRFGuardedClient(&http.Client{...})` (a real, nil-then-cloned
// *http.Transport under the hood) so the guard's strong, connect-time,
// DNS-rebind-proof tier applies (see NewFetcher's doc comment and go-kit
// httputil.NewSSRFGuardedClient's own doc comment on the two composition
// tiers) — wrapping the client in a UA-setting http.RoundTripper would make
// its Transport an opaque, non-*http.Transport type, which a caller
// composing this Fetcher's client through NewSSRFGuardedClient a second time
// would fall into the WEAKER pre-request-only tier for. Setting the header
// on the *http.Request instead has zero effect on transport/guard
// composition.
func WithUserAgent(ua string) Option {
	return func(f *Fetcher) { f.userAgent = ua }
}

// WithFollowRedirects makes doFetch follow a cross-domain redirect through
// to its final destination (up to maxRedirects hops, browser-like) instead
// of aborting with StatusRedirect and an empty body — the pre-existing,
// still-default (this option unset) behavior. FinalURL is still populated
// from the last hop either way.
//
// Why this exists (go-wp#190 live regression, 2026-07): a caller's raw
// fetch of an operator-supplied website (e.g. wp_verify) may legitimately
// redirect cross-domain for canonicalization (bare-domain -> www, http ->
// https, or a city/locale-prefixed canonical host) — mcmedok.ru and
// excimerclinic.ru both do this. Without this option, doFetch's default
// cross-domain-redirect short-circuit discarded the (already fetched) final
// page's body entirely, which silently degraded a caller relying on content
// extraction (wp_verify fell back to a weaker source tier and regressed a
// live verdict). The default stays OFF because a cross-domain redirect is
// ALSO a meaningful signal on its own for some callers (e.g. detecting a
// defunct site parked/redirected to an unrelated domain) — this option is
// an explicit opt-in for callers that want content over that signal.
//
// SSRF: the guard is UNCHANGED and un-weakened by this option. doFetch's
// client is still built by NewFetcher's default construction
// (httputil.NewSSRFGuardedClient), and net/http's Client routes EVERY
// request it issues -- the original AND every followed redirect hop --
// through that SAME client.Transport.RoundTrip; there is no separate,
// unguarded code path for a redirect target. Concretely: the strong tier's
// GuardedDialContext Control hook (go-kit httputil/ssrf.go) re-runs at
// connect time for EACH hop's already-DNS-resolved address, so a redirect
// to a loopback / private / link-local / cloud-metadata target is refused
// at that hop exactly as the original URL would be -- this option makes
// redirects reach further, it does not make any one of them less guarded.
func WithFollowRedirects() Option {
	return func(f *Fetcher) { f.followRedirects = true }
}

// NewFetcher creates a Fetcher with the given options. The default client is
// SSRF-guarded via go-kit httputil.NewSSRFGuardedClient (the single,
// framework-owned SSRF block-list): it refuses to connect to loopback,
// private, link-local, unspecified, multicast, CGNAT, NAT64, 6to4, or
// IPv4-compatible-IPv6 addresses, since rawURL passed to Fetch is
// caller-supplied by design (e.g. an advertiser-provided website field) and
// must never be able to reach internal infrastructure. Pass WithClient to
// opt out (e.g. for a test hitting a local httptest server).
func NewFetcher(opts ...Option) *Fetcher {
	f := &Fetcher{
		client:       httputil.NewSSRFGuardedClient(&http.Client{Timeout: DefaultTimeout}),
		maxBodyBytes: DefaultMaxBodyBytes,
	}
	for _, o := range opts {
		o(f)
	}
	return f
}

// Fetch retrieves a page and classifies its status.
// Concurrent calls for the same URL are deduplicated via singleflight.
// Note: the winning goroutine's context governs the in-flight request;
// if that context is canceled, all waiters receive StatusUnreachable.
func (f *Fetcher) Fetch(ctx context.Context, rawURL string) (*FetchResult, error) {
	if rawURL == "" {
		return nil, errors.New("fetch: empty URL")
	}

	v, err, _ := f.sf.Do(rawURL, func() (any, error) {
		return f.doFetch(ctx, rawURL)
	})
	if err != nil {
		return nil, err
	}
	result := v.(*FetchResult)
	return result, nil
}

func (f *Fetcher) doFetch(ctx context.Context, rawURL string) (*FetchResult, error) {
	origHost := extractHost(rawURL)

	// Clone client with custom redirect policy for domain-change detection.
	client := *f.client
	var finalURL string
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		finalURL = req.URL.String()
		if len(via) >= maxRedirects {
			return http.ErrUseLastResponse
		}
		return nil
	}

	resp, err := f.doRequest(ctx, &client, rawURL)
	if err != nil {
		// A TLS CERTIFICATE error (hostname mismatch, untrusted CA, or
		// another x509 chain error -- isCertError) is DISTINCT from an
		// opaque connection failure (refused, DNS, timeout): the origin may
		// still be genuinely reachable behind a broken cert (real case:
		// p45.su serves a cert issued for a different host). Retry ONCE via
		// httpFallbackURL's plain-HTTP downgrade, through the exact SAME
		// `client` (same *http.Client, same already-guarded Transport, same
		// CheckRedirect closure) -- never a fresh unguarded client, so the
		// SSRF guard's connect-time Control hook re-runs for this hop
		// exactly as it does for every other dial (see NewFetcher's and
		// WithFollowRedirects' doc comments on why that hook, not the
		// client instance, is what enforces the guard).
		//
		// Note: httpFallbackURL retries the ORIGINAL rawURL (this function's
		// argument), not whichever hop's cert actually failed -- if the
		// primary request already followed one or more redirects before the
		// error surfaced, the fallback re-starts from rawURL's host, not the
		// failing redirect target's. Safe (still fully guarded either way),
		// but non-obvious.
		if fallbackURL, ok := httpFallbackURL(rawURL); ok && isCertError(err) {
			finalURL = "" // fresh redirect-chain tracking for the fallback's own hops
			if fresp, ferr := f.doRequest(ctx, &client, fallbackURL); ferr == nil {
				defer fresp.Body.Close() //nolint:errcheck
				fr := f.processResponse(fresp, origHost, finalURL)
				fr.TLSFallbackUsed = true
				return fr, nil
			}
			// Fallback itself failed (blocked by the guard, refused,
			// timed out, another cert error, ...) -- report the DISTINCT
			// TLS-specific status instead of collapsing back to the
			// generic StatusUnreachable every other failure gets, so a
			// downstream consumer can tell "cert is broken" apart from
			// "site is down" (see StatusTLSInvalid's doc comment).
			return &FetchResult{Status: StatusTLSInvalid}, nil
		}
		return &FetchResult{Status: StatusUnreachable}, nil
	}
	defer resp.Body.Close() //nolint:errcheck

	return f.processResponse(resp, origHost, finalURL), nil
}

// doRequest issues a single GET through client, applying the same
// User-Agent policy as every fetch (see WithUserAgent). Shared by doFetch's
// primary request and its TLS-cert-error fallback retry so both attempts
// build the request identically and share the exact same client/Transport
// (and therefore the exact same SSRF guard).
func (f *Fetcher) doRequest(ctx context.Context, client *http.Client, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if f.userAgent != "" {
		req.Header.Set("User-Agent", f.userAgent)
	}
	return client.Do(req) //nolint:gosec // URL is user-provided by design; guarded against internal targets by NewFetcher's default transport (see go-kit httputil.NewSSRFGuardedClient). The TLS-cert-error fallback (httpFallbackURL, doFetch) reuses this SAME client/transport, so the guard applies identically to both attempts.
}

// processResponse classifies an already-obtained *http.Response into a
// FetchResult -- the shared tail of doFetch's primary request and its
// TLS-cert-error fallback retry (see httpFallbackURL). finalURL is the last
// hop's URL as captured by the client's CheckRedirect closure (empty if no
// redirect occurred).
func (f *Fetcher) processResponse(resp *http.Response, origHost, finalURL string) *FetchResult {
	// Detect cross-domain redirect. Skipped when followRedirects is set --
	// the request has already been followed through to resp by client.Do
	// (CheckRedirect returns nil until maxRedirects), so falling through
	// below reads resp/body from the FINAL hop, not this one.
	if finalURL != "" && extractHost(finalURL) != origHost && !f.followRedirects {
		return &FetchResult{
			Status:     StatusRedirect,
			FinalURL:   finalURL,
			StatusCode: resp.StatusCode,
		}
	}

	if resp.StatusCode == http.StatusNotFound {
		return &FetchResult{
			Status:     StatusNotFound,
			StatusCode: resp.StatusCode,
		}
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return &FetchResult{
			Status:     StatusUnreachable,
			StatusCode: resp.StatusCode,
		}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, f.maxBodyBytes))
	if err != nil {
		return &FetchResult{
			Status:     StatusUnreachable,
			StatusCode: resp.StatusCode,
		}
	}

	return &FetchResult{
		HTML:       string(body),
		Status:     StatusActive,
		FinalURL:   finalURL,
		StatusCode: resp.StatusCode,
	}
}

// isCertError reports whether err is a TLS CERTIFICATE validation failure --
// hostname mismatch, untrusted/unknown CA, or another x509 chain-validation
// error -- as DISTINCT from a connection-level failure (refused, DNS,
// timeout) or a non-cert TLS transport error (e.g. a garbled handshake).
// Only a cert error is eligible for doFetch's one-retry cert-tolerant
// fallback (httpFallbackURL); every other error keeps returning
// StatusUnreachable exactly as before this change.
//
// Checks four error types via errors.As, which walks the FULL wrapped chain
// (net/http wraps in *url.Error, which wraps *net.OpError, which wraps the
// TLS error), so nesting depth doesn't matter:
//   - x509.HostnameError -- cert valid, but for a different host (the p45.su
//     ground-truth case: a cert issued for 000h01.westcall.spb.ru).
//   - x509.UnknownAuthorityError -- self-signed / untrusted CA.
//   - x509.CertificateInvalidError -- expired, not-yet-valid, or another
//     chain-validation failure (covers x509.Expired and friends).
//   - *tls.CertificateVerificationError -- the Go 1.20+ wrapper crypto/tls
//     puts around ANY certificate-verification failure (its Unwrap returns
//     the underlying x509 error, so the three checks above already catch
//     most cases through it; this direct check is defense-in-depth for an
//     x509 error type not explicitly named above).
func isCertError(err error) bool {
	if err == nil {
		return false
	}
	var hostErr x509.HostnameError
	var authErr x509.UnknownAuthorityError
	var certErr x509.CertificateInvalidError
	var verifyErr *tls.CertificateVerificationError
	return errors.As(err, &hostErr) ||
		errors.As(err, &authErr) ||
		errors.As(err, &certErr) ||
		errors.As(err, &verifyErr)
}

// httpFallbackURL returns rawURL with its scheme swapped from "https" to
// "http" -- host, port, path, and query are untouched -- for doFetch's
// one-retry TLS-cert-error fallback (see isCertError). Deliberately a pure
// URL rewrite reusing the SAME host:port the primary request already
// resolved to: it does NOT invent a new target, so it changes nothing about
// which address the SSRF guard has to evaluate. Returns ok=false for a
// non-https URL (nothing to fall back from) or an unparseable one.
func httpFallbackURL(rawURL string) (string, bool) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" {
		return "", false
	}
	fallback := *u
	fallback.Scheme = "http"
	return fallback.String(), true
}

// extractHost returns the lowercase host from a URL string.
func extractHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Host)
}

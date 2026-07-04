package fetch

import (
	"context"
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return &FetchResult{Status: StatusUnreachable}, nil
	}
	if f.userAgent != "" {
		req.Header.Set("User-Agent", f.userAgent)
	}

	resp, err := client.Do(req) //nolint:gosec // URL is user-provided by design; guarded against internal targets by NewFetcher's default transport (see go-kit httputil.NewSSRFGuardedClient)
	if err != nil {
		return &FetchResult{Status: StatusUnreachable}, nil
	}
	defer resp.Body.Close() //nolint:errcheck

	// Detect cross-domain redirect.
	if finalURL != "" && extractHost(finalURL) != origHost {
		return &FetchResult{
			Status:     StatusRedirect,
			FinalURL:   finalURL,
			StatusCode: resp.StatusCode,
		}, nil
	}

	if resp.StatusCode == http.StatusNotFound {
		return &FetchResult{
			Status:     StatusNotFound,
			StatusCode: resp.StatusCode,
		}, nil
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return &FetchResult{
			Status:     StatusUnreachable,
			StatusCode: resp.StatusCode,
		}, nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, f.maxBodyBytes))
	if err != nil {
		return &FetchResult{
			Status:     StatusUnreachable,
			StatusCode: resp.StatusCode,
		}, nil
	}

	return &FetchResult{
		HTML:       string(body),
		Status:     StatusActive,
		FinalURL:   finalURL,
		StatusCode: resp.StatusCode,
	}, nil
}

// extractHost returns the lowercase host from a URL string.
func extractHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Host)
}

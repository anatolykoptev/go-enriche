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
}

// Option configures a Fetcher.
type Option func(*Fetcher)

// WithClient sets a custom HTTP client (e.g., stealth-configured).
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

// NewFetcher creates a Fetcher with the given options.
func NewFetcher(opts ...Option) *Fetcher {
	f := &Fetcher{
		client: &http.Client{
			Timeout: DefaultTimeout,
		},
		maxBodyBytes: DefaultMaxBodyBytes,
	}
	for _, o := range opts {
		o(f)
	}
	return f
}

// Fetch retrieves a page and classifies its status.
// Concurrent calls for the same URL are deduplicated via singleflight.
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

	resp, err := client.Do(req) //nolint:gosec // URL is user-provided by design
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

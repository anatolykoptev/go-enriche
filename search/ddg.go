package search

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	stealth "github.com/anatolykoptev/go-stealth"
	"github.com/anatolykoptev/go-stealth/websearch"
)

// DDG implements Provider using DuckDuckGo HTML lite endpoint with browser TLS fingerprinting.
//
// Proxy is mandatory — data center IPs are blocked by DuckDuckGo.
// All requests are routed through the configured proxy; the server IP is never exposed.
type DDG struct {
	bc         BrowserDoer
	proxyPool  ProxyPoolProvider
	maxResults int
}

// DDGOption configures DDG.
type DDGOption func(*DDG)

// WithDDGMaxResults sets the max results to aggregate.
func WithDDGMaxResults(n int) DDGOption {
	return func(d *DDG) { d.maxResults = n }
}

// WithDDGDoer overrides the default BrowserDoer (for testing).
func WithDDGDoer(bc BrowserDoer) DDGOption {
	return func(d *DDG) { d.bc = bc }
}

// WithDDGProxyPool enables per-request proxy rotation.
// When set, the proxyURL argument in NewDDG can be empty.
func WithDDGProxyPool(pool ProxyPoolProvider) DDGOption {
	return func(d *DDG) { d.proxyPool = pool }
}

// NewDDG creates a DuckDuckGo Direct provider with proxy or proxy pool.
// Either proxyURL or WithDDGProxyPool must be provided — data center IPs are blocked.
//
// Example:
//
//	ddg, err := search.NewDDG("socks5://user:pass@proxy:1080")
//	ddg, err := search.NewDDG("", search.WithDDGProxyPool(pool))
func NewDDG(proxyURL string, opts ...DDGOption) (*DDG, error) {
	d := &DDG{maxResults: defaultMaxResults}
	for _, o := range opts {
		o(d)
	}

	// Custom doer already set (testing).
	if d.bc != nil {
		return d, nil
	}

	// Need at least a proxy URL or a proxy pool.
	if proxyURL == "" && d.proxyPool == nil {
		return nil, errors.New("ddg: proxy URL or pool is required (data center IPs are blocked)")
	}

	var stealthOpts []stealth.ClientOption
	stealthOpts = append(stealthOpts, stealth.WithTimeout(defaultStealthTimeout))
	if d.proxyPool != nil {
		stealthOpts = append(stealthOpts, stealth.WithProxyPool(d.proxyPool))
	} else {
		stealthOpts = append(stealthOpts, stealth.WithProxy(proxyURL))
	}

	bc, err := stealth.NewClient(stealthOpts...)
	if err != nil {
		return nil, fmt.Errorf("ddg: stealth client: %w", err)
	}
	d.bc = bc
	return d, nil
}

// Search queries DuckDuckGo HTML lite and returns aggregated context.
func (d *DDG) Search(ctx context.Context, query string, timeRange string) (*SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	formBody := fmt.Sprintf("q=%s&df=", url.QueryEscape(query))

	headers := ChromeHeaders()
	headers["referer"] = "https://html.duckduckgo.com/"
	headers["content-type"] = "application/x-www-form-urlencoded"

	data, _, status, err := d.bc.Do(
		http.MethodPost,
		"https://html.duckduckgo.com/html/",
		headers,
		strings.NewReader(formBody),
	)
	if err != nil {
		return nil, fmt.Errorf("ddg: request: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("ddg: HTTP %d", status)
	}

	// Delegate HTML parsing to go-stealth websearch.
	wsResults, err := websearch.ParseDDGHTML(data)
	if err != nil {
		return nil, fmt.Errorf("ddg: parse: %w", err)
	}

	return d.aggregate(wsResults), nil
}

func (d *DDG) aggregate(results []websearch.Result) *SearchResult {
	internal := wsToInternal(results)
	s := &SearXNG{maxResults: d.maxResults}
	return s.aggregate(internal)
}

// ddgUnwrapURL delegates to websearch.DDGUnwrapURL for backward compatibility.
func ddgUnwrapURL(href string) string {
	return websearch.DDGUnwrapURL(href)
}

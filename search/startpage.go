package search

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	stealth "github.com/anatolykoptev/go-stealth"
)

// Startpage implements Provider using Startpage POST form with browser TLS fingerprinting.
//
// Proxy is mandatory — data center IPs are blocked by Startpage (307 → CAPTCHA).
// All requests are routed through the configured proxy; the server IP is never exposed.
type Startpage struct {
	bc         BrowserDoer
	proxyPool  ProxyPoolProvider
	maxResults int
}

// StartpageOption configures Startpage.
type StartpageOption func(*Startpage)

// WithStartpageMaxResults sets the max results to aggregate.
func WithStartpageMaxResults(n int) StartpageOption {
	return func(sp *Startpage) { sp.maxResults = n }
}

// WithStartpageDoer overrides the default BrowserDoer (for testing).
func WithStartpageDoer(bc BrowserDoer) StartpageOption {
	return func(sp *Startpage) { sp.bc = bc }
}

// WithStartpageProxyPool enables per-request proxy rotation.
// When set, the proxyURL argument in NewStartpage can be empty.
func WithStartpageProxyPool(pool ProxyPoolProvider) StartpageOption {
	return func(sp *Startpage) { sp.proxyPool = pool }
}

// NewStartpage creates a Startpage Direct provider with proxy or proxy pool.
// Either proxyURL or WithStartpageProxyPool must be provided — data center IPs are blocked.
//
// Example:
//
//	sp, err := search.NewStartpage("socks5://user:pass@proxy:1080")
//	sp, err := search.NewStartpage("", search.WithStartpageProxyPool(pool))
func NewStartpage(proxyURL string, opts ...StartpageOption) (*Startpage, error) {
	sp := &Startpage{maxResults: defaultMaxResults}
	for _, o := range opts {
		o(sp)
	}

	// Custom doer already set (testing).
	if sp.bc != nil {
		return sp, nil
	}

	// Need at least a proxy URL or a proxy pool.
	if proxyURL == "" && sp.proxyPool == nil {
		return nil, errors.New("startpage: proxy URL or pool is required (data center IPs are blocked)")
	}

	var stealthOpts []stealth.ClientOption
	stealthOpts = append(stealthOpts, stealth.WithTimeout(defaultStealthTimeout))
	if sp.proxyPool != nil {
		stealthOpts = append(stealthOpts, stealth.WithProxyPool(sp.proxyPool))
	} else {
		stealthOpts = append(stealthOpts, stealth.WithProxy(proxyURL))
	}

	bc, err := stealth.NewClient(stealthOpts...)
	if err != nil {
		return nil, fmt.Errorf("startpage: stealth client: %w", err)
	}
	sp.bc = bc
	return sp, nil
}

// Search queries Startpage via POST form and returns aggregated context.
func (sp *Startpage) Search(ctx context.Context, query string, timeRange string) (*SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	formBody := fmt.Sprintf("query=%s&cat=web&language=english", url.QueryEscape(query))

	headers := ChromeHeaders()
	headers["referer"] = "https://www.startpage.com/"
	headers["content-type"] = "application/x-www-form-urlencoded"
	headers["accept"] = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"

	data, _, status, err := sp.bc.Do(
		http.MethodPost,
		"https://www.startpage.com/sp/search",
		headers,
		strings.NewReader(formBody),
	)
	if err != nil {
		return nil, fmt.Errorf("startpage: request: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("startpage: HTTP %d", status)
	}

	results, err := parseStartpageHTML(data)
	if err != nil {
		return nil, fmt.Errorf("startpage: parse: %w", err)
	}

	return sp.aggregate(results), nil
}

func (sp *Startpage) aggregate(results []searxngResult) *SearchResult {
	// Reuse SearXNG aggregation logic — same dedup + context building.
	s := &SearXNG{maxResults: sp.maxResults}
	return s.aggregate(results)
}

// parseStartpageHTML extracts search results from Startpage HTML response.
func parseStartpageHTML(data []byte) ([]searxngResult, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("goquery parse: %w", err)
	}

	var results []searxngResult

	// Startpage result blocks: <div class="w-gl__result"> or <div class="result">
	doc.Find(".w-gl__result, .result").Each(func(_ int, s *goquery.Selection) {
		// Title + URL from <a> inside heading.
		link := s.Find("a.w-gl__result-title, h3 a, a.result-link").First()
		title := strings.TrimSpace(link.Text())
		href, exists := link.Attr("href")
		if !exists || title == "" {
			return
		}

		// Description.
		desc := s.Find("p.w-gl__description, .w-gl__description, p.result-description").First()
		content := strings.TrimSpace(desc.Text())

		// Skip empty/ad results.
		if href == "" || strings.Contains(href, "startpage.com/do/") {
			return
		}

		results = append(results, searxngResult{
			URL:     href,
			Title:   title,
			Content: content,
		})
	})

	return results, nil
}

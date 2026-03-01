package search

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// DDG implements Provider using DuckDuckGo HTML lite endpoint with browser TLS fingerprinting.
type DDG struct {
	bc         BrowserDoer
	maxResults int
}

// DDGOption configures DDG.
type DDGOption func(*DDG)

// WithDDGMaxResults sets the max results to aggregate.
func WithDDGMaxResults(n int) DDGOption {
	return func(d *DDG) { d.maxResults = n }
}

// NewDDG creates a DuckDuckGo Direct provider.
func NewDDG(bc BrowserDoer, opts ...DDGOption) *DDG {
	d := &DDG{
		bc:         bc,
		maxResults: defaultMaxResults,
	}
	for _, o := range opts {
		o(d)
	}
	return d
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

	results, err := parseDDGHTML(data)
	if err != nil {
		return nil, fmt.Errorf("ddg: parse: %w", err)
	}

	return d.aggregate(results), nil
}

func (d *DDG) aggregate(results []searxngResult) *SearchResult {
	// Reuse SearXNG aggregation logic — same dedup + context building.
	s := &SearXNG{maxResults: d.maxResults}
	return s.aggregate(results)
}

// parseDDGHTML extracts search results from DDG HTML lite response.
func parseDDGHTML(data []byte) ([]searxngResult, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("goquery parse: %w", err)
	}

	var results []searxngResult

	doc.Find(".result, .web-result").Each(func(_ int, s *goquery.Selection) {
		link := s.Find("a.result__a, .result__title a, a.result-link").First()
		title := strings.TrimSpace(link.Text())
		href, exists := link.Attr("href")
		if !exists || title == "" {
			return
		}

		href = ddgUnwrapURL(href)
		if href == "" {
			return
		}

		snippet := s.Find(".result__snippet, .result__body").First()
		content := strings.TrimSpace(snippet.Text())

		results = append(results, searxngResult{
			URL:     href,
			Title:   title,
			Content: content,
		})
	})

	return results, nil
}

// ddgUnwrapURL extracts the actual URL from DDG redirect wrappers.
// DDG HTML wraps links as: //duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com&rut=...
func ddgUnwrapURL(href string) string {
	if strings.Contains(href, "duckduckgo.com/l/") || strings.Contains(href, "uddg=") {
		if u, err := url.Parse(href); err == nil {
			if uddg := u.Query().Get("uddg"); uddg != "" {
				return uddg
			}
		}
	}
	if strings.HasPrefix(href, "http") {
		return href
	}
	return ""
}

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

// Startpage implements Provider using Startpage POST form with browser TLS fingerprinting.
//
// IMPORTANT: Requires a residential/mobile proxy. Data center IPs (AWS, GCP, OCI, etc.)
// are blocked by Startpage (307 → CAPTCHA). Configure the proxy on the BrowserDoer:
//
//	bc, _ := stealth.NewClient(stealth.WithProxy("socks5://proxy:1080"))
//	sp := search.NewStartpage(bc)
type Startpage struct {
	bc         BrowserDoer
	maxResults int
}

// StartpageOption configures Startpage.
type StartpageOption func(*Startpage)

// WithStartpageMaxResults sets the max results to aggregate.
func WithStartpageMaxResults(n int) StartpageOption {
	return func(sp *Startpage) { sp.maxResults = n }
}

// NewStartpage creates a Startpage Direct provider.
func NewStartpage(bc BrowserDoer, opts ...StartpageOption) *Startpage {
	sp := &Startpage{
		bc:         bc,
		maxResults: defaultMaxResults,
	}
	for _, o := range opts {
		o(sp)
	}
	return sp
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

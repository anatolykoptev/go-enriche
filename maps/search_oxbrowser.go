package maps

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/anatolykoptev/go-engine/websearch"
)

const oxSearchTimeout = 20 * time.Second

// OxBrowserSearch returns a SearchFunc that uses ox-browser's /fetch endpoint
// to query DuckDuckGo HTML and parse results. This replaces SearXNG dependency.
// Requests carry X-Internal-Secret from INTERNAL_SERVICE_SECRET when set
// (scoped to the ox-browser origin by go-kit svcauth).
func OxBrowserSearch(oxBrowserURL string) SearchFunc {
	baseURL := strings.TrimRight(oxBrowserURL, "/")
	client := oxHTTPClient(oxSearchTimeout, baseURL)

	return func(ctx context.Context, query string) ([]SearchResult, error) {
		ddgURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)

		html, err := oxFetch(ctx, client, baseURL, ddgURL)
		if err != nil {
			return nil, fmt.Errorf("oxbrowser search: %w", err)
		}

		wsResults, err := websearch.ParseDDGHTML([]byte(html))
		if err != nil {
			return nil, fmt.Errorf("oxbrowser search: parse DDG: %w", err)
		}

		results := make([]SearchResult, 0, len(wsResults))
		for _, r := range wsResults {
			results = append(results, SearchResult{URL: r.URL, Title: r.Title})
		}
		return results, nil
	}
}

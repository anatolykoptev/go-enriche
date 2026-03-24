package maps

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const goSearchTimeout = 20 * time.Second

// goSearchResponse is the JSON response from go-search /api/search.
type goSearchResponse struct {
	Results []goSearchResult `json:"results"`
}

type goSearchResult struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// GoSearchSearch returns a SearchFunc that queries go-search's REST API.
// go-search aggregates DDG + Startpage + Brave + ox-browser in parallel,
// providing much better coverage than any single scraper.
func GoSearchSearch(goSearchURL string) SearchFunc {
	baseURL := strings.TrimRight(goSearchURL, "/")
	client := &http.Client{Timeout: goSearchTimeout}

	return func(ctx context.Context, query string) ([]SearchResult, error) {
		reqURL := baseURL + "/api/search?q=" + url.QueryEscape(query) + "&lang=ru"

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return nil, fmt.Errorf("go-search: request: %w", err)
		}

		resp, err := client.Do(req) //nolint:gosec
		if err != nil {
			return nil, fmt.Errorf("go-search: fetch: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("go-search: HTTP %d", resp.StatusCode)
		}

		data, err := io.ReadAll(io.LimitReader(resp.Body, oxMaxResponseBytes))
		if err != nil {
			return nil, fmt.Errorf("go-search: read: %w", err)
		}

		var sr goSearchResponse
		if err := json.Unmarshal(data, &sr); err != nil {
			return nil, fmt.Errorf("go-search: parse: %w", err)
		}

		results := make([]SearchResult, 0, len(sr.Results))
		for _, r := range sr.Results {
			results = append(results, SearchResult{URL: r.URL, Title: r.Title})
		}
		return results, nil
	}
}

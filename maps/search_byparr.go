package maps

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const byparrSearchTimeout = 45 * time.Second

// byparrResponse is the FlareSolverr-compatible JSON response.
type byparrResponse struct {
	Status   string          `json:"status"`
	Solution *byparrSolution `json:"solution"`
	Message  string          `json:"message"`
}

type byparrSolution struct {
	URL      string `json:"url"`
	Response string `json:"response"`
}

// ByparrSearch returns a SearchFunc that uses byparr (FlareSolverr) headless
// browser to search Yandex Maps. Byparr renders the SPA and follows redirects,
// returning the final org URL in solution.url.
// Slow (~6s per request) but reliable for SPA sites.
func ByparrSearch(byparrURL string) SearchFunc {
	base := strings.TrimRight(byparrURL, "/")
	client := &http.Client{Timeout: byparrSearchTimeout}

	return func(ctx context.Context, query string) ([]SearchResult, error) {
		searchQuery := cleanMapsQuery(query)
		mapsURL := "https://yandex.ru/maps/search/" +
			url.PathEscape(searchQuery) + "/"

		br, err := callByparr(ctx, client, base, mapsURL)
		if err != nil {
			return nil, err
		}
		return extractByparrResults(br), nil
	}
}

// callByparr sends a request to the byparr API and returns parsed response.
func callByparr(ctx context.Context, client *http.Client, base, targetURL string) (*byparrResponse, error) {
	body, err := json.Marshal(map[string]any{
		"cmd":        "request.get",
		"url":        targetURL,
		"maxTimeout": 30000, //nolint:mnd
	})
	if err != nil {
		return nil, fmt.Errorf("byparr search: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		base+"/v1", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("byparr search: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("byparr search: fetch: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("byparr search: read: %w", err)
	}

	var br byparrResponse
	if err := json.Unmarshal(data, &br); err != nil {
		return nil, fmt.Errorf("byparr search: parse: %w", err)
	}
	if br.Status != "ok" {
		return nil, fmt.Errorf("byparr search: %s", br.Message)
	}
	if br.Solution == nil {
		return nil, errors.New("byparr search: no solution")
	}
	return &br, nil
}

// extractByparrResults extracts org URLs from byparr solution (redirect URL + HTML).
func extractByparrResults(br *byparrResponse) []SearchResult {
	var results []SearchResult
	seen := map[string]bool{}

	// The final URL after SPA render often contains the org URL directly.
	if isYandexMapsOrgURL(br.Solution.URL) {
		results = append(results, SearchResult{URL: br.Solution.URL})
		seen[br.Solution.URL] = true
	}

	// Also parse rendered HTML for additional org URLs.
	for _, m := range yandexMapsOrgRe.FindAllString(br.Solution.Response, -1) {
		fullURL := "https://" + m
		if !seen[fullURL] {
			seen[fullURL] = true
			results = append(results, SearchResult{URL: fullURL})
		}
	}
	return results
}

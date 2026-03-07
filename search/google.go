package search

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

const googleDefaultBaseURL = "https://www.googleapis.com/customsearch/v1"

// Google implements Provider using the Google Custom Search JSON API.
type Google struct {
	apiKey     string
	cx         string
	baseURL    string
	client     *http.Client
	maxResults int
}

// GoogleOption configures Google.
type GoogleOption func(*Google)

// WithGoogleBaseURL overrides the API endpoint (for testing).
func WithGoogleBaseURL(u string) GoogleOption {
	return func(g *Google) { g.baseURL = u }
}

// WithGoogleHTTPClient sets a custom HTTP client.
func WithGoogleHTTPClient(c *http.Client) GoogleOption {
	return func(g *Google) { g.client = c }
}

// WithGoogleMaxResults sets the max results to aggregate.
func WithGoogleMaxResults(n int) GoogleOption {
	return func(g *Google) { g.maxResults = n }
}

// NewGoogle creates a Google Custom Search provider.
func NewGoogle(apiKey, cx string, opts ...GoogleOption) *Google {
	g := &Google{
		apiKey:     apiKey,
		cx:         cx,
		baseURL:    googleDefaultBaseURL,
		client:     http.DefaultClient,
		maxResults: defaultMaxResults,
	}
	for _, o := range opts {
		o(g)
	}
	return g
}

// googleResponse is the JSON structure returned by Google CSE API.
type googleResponse struct {
	Items []googleResult `json:"items"`
}

type googleResult struct {
	Title   string `json:"title"`
	Link    string `json:"link"`
	Snippet string `json:"snippet"`
}

// timeRangeToGoogleDateRestrict maps enriche time ranges to Google dateRestrict values.
func timeRangeToGoogleDateRestrict(timeRange string) string {
	switch timeRange {
	case "week":
		return "w1"
	case "month":
		return "m1"
	case "day":
		return "d1"
	case "year":
		return "y1"
	default:
		return ""
	}
}

// Search queries Google Custom Search and returns aggregated context.
func (g *Google) Search(ctx context.Context, query string, timeRange string) (*SearchResult, error) {
	params := url.Values{}
	params.Set("key", g.apiKey)
	params.Set("cx", g.cx)
	params.Set("q", query)
	params.Set("num", "10")
	if dr := timeRangeToGoogleDateRestrict(timeRange); dr != "" {
		params.Set("dateRestrict", dr)
	}

	reqURL := g.baseURL + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("google: build request: %w", err)
	}

	resp, err := g.client.Do(req) //nolint:gosec // G704: baseURL is configured by the caller, not user input
	if err != nil {
		return nil, fmt.Errorf("google: request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("google: read body: %w", err)
	}

	var data googleResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("google: parse JSON: %w", err)
	}

	return g.aggregate(data.Items), nil
}

func (g *Google) aggregate(items []googleResult) *SearchResult {
	generic := make([]searchResult, 0, len(items))
	for _, r := range items {
		generic = append(generic, searchResult{
			URL:     r.Link,
			Title:   r.Title,
			Content: r.Snippet,
		})
	}
	return aggregateResults(generic, g.maxResults)
}

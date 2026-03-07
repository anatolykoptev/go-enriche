package search

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

const braveDefaultBaseURL = "https://api.search.brave.com/res/v1/web/search"

// Brave implements Provider using the Brave Search API.
type Brave struct {
	apiKey     string
	baseURL    string
	client     *http.Client
	maxResults int
}

// BraveOption configures Brave.
type BraveOption func(*Brave)

// WithBraveBaseURL overrides the API endpoint (for testing).
func WithBraveBaseURL(u string) BraveOption {
	return func(b *Brave) { b.baseURL = u }
}

// WithBraveHTTPClient sets a custom HTTP client.
func WithBraveHTTPClient(c *http.Client) BraveOption {
	return func(b *Brave) { b.client = c }
}

// WithBraveMaxResults sets the max results to aggregate.
func WithBraveMaxResults(n int) BraveOption {
	return func(b *Brave) { b.maxResults = n }
}

// NewBrave creates a Brave Search provider.
func NewBrave(apiKey string, opts ...BraveOption) *Brave {
	b := &Brave{
		apiKey:     apiKey,
		baseURL:    braveDefaultBaseURL,
		client:     http.DefaultClient,
		maxResults: defaultMaxResults,
	}
	for _, o := range opts {
		o(b)
	}
	return b
}

// braveResponse is the top-level JSON response from Brave Search API.
type braveResponse struct {
	Web *braveWebResults `json:"web"`
}

type braveWebResults struct {
	Results []braveResult `json:"results"`
}

type braveResult struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// timeRangeToBraveFreshness maps enriche time ranges to Brave freshness values.
func timeRangeToBraveFreshness(timeRange string) string {
	switch timeRange {
	case TimeRangeWeek:
		return "pw"
	case TimeRangeMonth:
		return "pm"
	case TimeRangeDay:
		return "pd"
	case TimeRangeYear:
		return "py"
	default:
		return ""
	}
}

// Search queries Brave Search and returns aggregated context.
func (b *Brave) Search(ctx context.Context, query string, timeRange string) (*SearchResult, error) {
	params := url.Values{}
	params.Set("q", query)
	params.Set("count", "10")
	if freshness := timeRangeToBraveFreshness(timeRange); freshness != "" {
		params.Set("freshness", freshness)
	}

	reqURL := b.baseURL + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("brave: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", b.apiKey)

	resp, err := b.client.Do(req) //nolint:gosec // G704: baseURL is configured by the caller, not user input
	if err != nil {
		return nil, fmt.Errorf("brave: request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("brave: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("brave: read body: %w", err)
	}

	var data braveResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("brave: parse JSON: %w", err)
	}

	if data.Web == nil {
		return &SearchResult{}, nil
	}

	return b.aggregate(data.Web.Results), nil
}

func (b *Brave) aggregate(results []braveResult) *SearchResult {
	generic := make([]searchResult, 0, len(results))
	for _, r := range results {
		generic = append(generic, searchResult{
			URL:     r.URL,
			Title:   r.Title,
			Content: r.Description,
		})
	}
	return aggregateResults(generic, b.maxResults)
}

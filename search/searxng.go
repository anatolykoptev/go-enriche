package search

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	defaultMaxResults = 3
	maxResponseBytes  = 2 << 20 // 2 MB
)

// SearXNG implements Provider using a SearXNG instance.
type SearXNG struct {
	baseURL    string
	client     *http.Client
	maxResults int
}

// SearXNGOption configures SearXNG.
type SearXNGOption func(*SearXNG)

// WithHTTPClient sets a custom HTTP client for SearXNG requests.
func WithHTTPClient(c *http.Client) SearXNGOption {
	return func(s *SearXNG) { s.client = c }
}

// WithMaxResults sets the maximum number of results to return.
func WithMaxResults(n int) SearXNGOption {
	return func(s *SearXNG) { s.maxResults = n }
}

// NewSearXNG creates a SearXNG provider.
func NewSearXNG(baseURL string, opts ...SearXNGOption) *SearXNG {
	s := &SearXNG{
		baseURL:    strings.TrimRight(baseURL, "/"),
		client:     http.DefaultClient,
		maxResults: defaultMaxResults,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// searxngResponse is the JSON structure returned by SearXNG API.
type searxngResponse struct {
	Results []searxngResult `json:"results"`
}

type searxngResult struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

// Search queries SearXNG and returns aggregated context and source URLs.
func (s *SearXNG) Search(ctx context.Context, query string, timeRange string) (*SearchResult, error) {
	params := url.Values{}
	params.Set("q", query)
	params.Set("format", "json")
	params.Set("categories", "general")
	if timeRange != "" {
		params.Set("time_range", timeRange)
	}

	reqURL := s.baseURL + "/search?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("searxng: build request: %w", err)
	}

	resp, err := s.client.Do(req) //nolint:gosec // G704: baseURL is configured by the caller, not user input
	if err != nil {
		return nil, fmt.Errorf("searxng: request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("searxng: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("searxng: read body: %w", err)
	}

	var data searxngResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("searxng: parse JSON: %w", err)
	}

	return s.aggregate(data.Results), nil
}

func (s *SearXNG) aggregate(results []searxngResult) *SearchResult {
	var (
		contextParts []string
		sources      []string
		seen         = make(map[string]bool)
	)

	for _, r := range results {
		if len(sources) >= s.maxResults {
			break
		}

		norm := normalizeURL(r.URL)
		if norm == "" || seen[norm] {
			continue
		}
		seen[norm] = true

		sources = append(sources, r.URL)
		switch {
		case r.Title != "" && r.Content != "":
			contextParts = append(contextParts, r.Title+": "+r.Content)
		case r.Content != "":
			contextParts = append(contextParts, r.Content)
		case r.Title != "":
			contextParts = append(contextParts, r.Title)
		}
	}

	return &SearchResult{
		Context: strings.Join(contextParts, "\n\n"),
		Sources: sources,
	}
}

// normalizeURL strips fragment, lowercases host/scheme, removes trailing slash.
func normalizeURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	u.Fragment = ""
	u.Host = strings.ToLower(u.Host)
	u.Scheme = strings.ToLower(u.Scheme)
	result := u.String()
	return strings.TrimRight(result, "/")
}

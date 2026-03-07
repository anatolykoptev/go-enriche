package search

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/anatolykoptev/go-stealth/websearch"
)

const oxBrowserSearchTimeout = 30 * time.Second

// OxBrowser implements Provider by fetching DuckDuckGo HTML through
// ox-browser's /fetch endpoint (which handles Cloudflare bypass).
// Use as a fallback when direct DDG/Startpage scrapers fail.
type OxBrowser struct {
	baseURL    string
	client     *http.Client
	maxResults int
}

// OxBrowserOption configures OxBrowser.
type OxBrowserOption func(*OxBrowser)

// WithOxBrowserMaxResults sets the max results to aggregate.
func WithOxBrowserMaxResults(n int) OxBrowserOption {
	return func(o *OxBrowser) { o.maxResults = n }
}

// NewOxBrowser creates an ox-browser search provider.
// baseURL is the ox-browser service URL (e.g. "http://ox-browser:8901").
func NewOxBrowser(baseURL string, opts ...OxBrowserOption) *OxBrowser {
	o := &OxBrowser{
		baseURL:    strings.TrimRight(baseURL, "/"),
		client:     &http.Client{Timeout: oxBrowserSearchTimeout},
		maxResults: defaultMaxResults,
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// oxFetchResponse is the JSON response from ox-browser /fetch.
type oxFetchResponse struct {
	Content    string `json:"content"`
	StatusCode int    `json:"status_code"`
	Error      string `json:"error,omitempty"`
}

// Search fetches DuckDuckGo HTML via ox-browser and parses results.
func (o *OxBrowser) Search(ctx context.Context, query string, _ string) (*SearchResult, error) {
	ddgURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)

	body, err := json.Marshal(map[string]string{"url": ddgURL})
	if err != nil {
		return nil, fmt.Errorf("oxbrowser search: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		o.baseURL+"/fetch", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("oxbrowser search: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req) //nolint:gosec // G704: baseURL is configured by the caller, not user input
	if err != nil {
		return nil, fmt.Errorf("oxbrowser search: request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oxbrowser search: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("oxbrowser search: read body: %w", err)
	}

	var fr oxFetchResponse
	if err := json.Unmarshal(data, &fr); err != nil {
		return nil, fmt.Errorf("oxbrowser search: parse JSON: %w", err)
	}
	if fr.Error != "" {
		return nil, fmt.Errorf("oxbrowser search: %s", fr.Error)
	}

	// Parse DDG HTML using go-stealth websearch parser.
	wsResults, err := websearch.ParseDDGHTML([]byte(fr.Content))
	if err != nil {
		return nil, fmt.Errorf("oxbrowser search: parse DDG HTML: %w", err)
	}

	return aggregateResults(toSearchResults(wsResults), o.maxResults), nil
}

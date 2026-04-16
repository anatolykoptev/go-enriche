package maps

import (
	"bytes"
	"errors"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const goSearchTimeout = 20 * time.Second

// restBridgeResponse is the JSON response from RESTBridge /api/tools/raw_web_search.
type restBridgeResponse struct {
	Structured struct {
		Results []goSearchResult `json:"results"`
	} `json:"structured"`
	IsError bool `json:"is_error"`
}

type goSearchResult struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// restBridgeRequest is the JSON body for the RESTBridge search endpoint.
type restBridgeRequest struct {
	Query    string `json:"query"`
	Language string `json:"language"`
}

// GoSearchSearch returns a SearchFunc that queries the RESTBridge API.
// RESTBridge aggregates DDG + Startpage + Brave + ox-browser in parallel,
// providing much better coverage than any single scraper.
func GoSearchSearch(goSearchURL string) SearchFunc {
	baseURL := strings.TrimRight(goSearchURL, "/")
	client := &http.Client{Timeout: goSearchTimeout}

	return func(ctx context.Context, query string) ([]SearchResult, error) {
		reqBody := restBridgeRequest{
			Query:    query,
			Language: "ru",
		}

		bodyBytes, err := json.Marshal(reqBody)
		if err != nil {
			return nil, fmt.Errorf("go-search: marshal: %w", err)
		}

		reqURL := baseURL + "/api/tools/raw_web_search"

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, fmt.Errorf("go-search: request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req) //nolint:gosec
		if err != nil {
			return nil, fmt.Errorf("go-search: fetch: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("go-search: HTTP %d", resp.StatusCode)
		}

		data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		if err != nil {
			return nil, fmt.Errorf("go-search: read: %w", err)
		}

		var sr restBridgeResponse
		if err := json.Unmarshal(data, &sr); err != nil {
			return nil, fmt.Errorf("go-search: parse: %w", err)
		}

		if sr.IsError {
			return nil, errors.New("go-search: API returned error")
		}

		results := make([]SearchResult, 0, len(sr.Structured.Results))
		for _, r := range sr.Structured.Results {
			results = append(results, SearchResult{URL: r.URL, Title: r.Title})
		}
		return results, nil
	}
}

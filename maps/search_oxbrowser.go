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

	"github.com/anatolykoptev/go-stealth/websearch"
)

const (
	oxSearchTimeout    = 20 * time.Second
	oxMaxResponseBytes = 512 * 1024 // 512KB limit for DDG HTML
)

// OxBrowserSearch returns a SearchFunc that uses ox-browser's /fetch endpoint
// to query DuckDuckGo HTML and parse results. This replaces SearXNG dependency.
func OxBrowserSearch(oxBrowserURL string) SearchFunc {
	baseURL := strings.TrimRight(oxBrowserURL, "/")
	client := &http.Client{Timeout: oxSearchTimeout}

	return func(ctx context.Context, query string) ([]SearchResult, error) {
		ddgURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)

		body, err := json.Marshal(map[string]string{"url": ddgURL})
		if err != nil {
			return nil, fmt.Errorf("maps oxbrowser: marshal: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			baseURL+"/fetch", strings.NewReader(string(body)))
		if err != nil {
			return nil, fmt.Errorf("maps oxbrowser: request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req) //nolint:gosec
		if err != nil {
			return nil, fmt.Errorf("maps oxbrowser: fetch: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("maps oxbrowser: HTTP %d", resp.StatusCode)
		}

		data, err := io.ReadAll(io.LimitReader(resp.Body, oxMaxResponseBytes))
		if err != nil {
			return nil, fmt.Errorf("maps oxbrowser: read: %w", err)
		}

		var fr struct {
			Body   string `json:"body"`
			Status int    `json:"status"`
			Error  string `json:"error,omitempty"`
		}
		if err := json.Unmarshal(data, &fr); err != nil {
			return nil, fmt.Errorf("maps oxbrowser: parse: %w", err)
		}
		if fr.Error != "" {
			return nil, fmt.Errorf("maps oxbrowser: %s", fr.Error)
		}

		wsResults, err := websearch.ParseDDGHTML([]byte(fr.Body))
		if err != nil {
			return nil, fmt.Errorf("maps oxbrowser: parse DDG: %w", err)
		}

		results := make([]SearchResult, 0, len(wsResults))
		for _, r := range wsResults {
			results = append(results, SearchResult{URL: r.URL, Title: r.Title})
		}
		return results, nil
	}
}

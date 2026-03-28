package maps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const goBrowserSearchTimeout = 45 * time.Second

// gbRenderReq is the JSON body for go-browser POST /render.
type gbRenderReq struct {
	URL         string `json:"url"`
	TimeoutSecs int    `json:"timeout_secs,omitempty"`
}

// gbRenderResp is the JSON response from go-browser POST /render.
type gbRenderResp struct {
	URL   string `json:"url"`
	HTML  string `json:"html"`
	Error string `json:"error,omitempty"`
}

// GoBrowserSearch returns a SearchFunc that renders Yandex Maps search pages
// via go-browser /render (headless Chrome). Drop-in replacement for ByparrSearch.
// Slower than ox-browser /fetch but renders full SPA content.
func GoBrowserSearch(goBrowserURL string) SearchFunc {
	base := strings.TrimRight(goBrowserURL, "/")
	client := &http.Client{Timeout: goBrowserSearchTimeout + 5*time.Second}

	return func(ctx context.Context, query string) ([]SearchResult, error) {
		searchQuery := cleanMapsQuery(query)
		mapsURL := "https://yandex.ru/maps/search/" +
			url.PathEscape(searchQuery) + "/"

		html, err := renderViaGoBrowser(ctx, client, base, mapsURL)
		if err != nil {
			return nil, fmt.Errorf("go-browser maps: %w", err)
		}

		return extractOrgURLsFromHTML(html), nil
	}
}

// GoBrowserOrgFetcher returns an OrgFetcher that renders Yandex Maps org pages
// via go-browser /render. Drop-in replacement for ByparrOrgFetcher.
func GoBrowserOrgFetcher(goBrowserURL string) OrgFetcher {
	base := strings.TrimRight(goBrowserURL, "/")
	client := &http.Client{Timeout: goBrowserSearchTimeout + 5*time.Second}

	return func(ctx context.Context, orgURL string) (string, error) {
		return renderViaGoBrowser(ctx, client, base, orgURL)
	}
}

// renderViaGoBrowser sends a URL to go-browser /render and returns rendered HTML.
func renderViaGoBrowser(
	ctx context.Context,
	client *http.Client,
	base, targetURL string,
) (string, error) {
	body, err := json.Marshal(gbRenderReq{
		URL:         targetURL,
		TimeoutSecs: int(goBrowserSearchTimeout.Seconds()),
	})
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		base+"/render", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req) //nolint:gosec
	if err != nil {
		return "", fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}

	var rr gbRenderResp
	if err := json.Unmarshal(data, &rr); err != nil {
		return "", fmt.Errorf("parse: %w", err)
	}
	if rr.Error != "" {
		return "", fmt.Errorf("render: %s", rr.Error)
	}
	if rr.HTML == "" {
		return "", fmt.Errorf("empty HTML for %s", targetURL)
	}
	return rr.HTML, nil
}

// extractOrgURLsFromHTML extracts unique Yandex Maps org URLs from rendered HTML.
func extractOrgURLsFromHTML(html string) []SearchResult {
	matches := yandexMapsOrgRe.FindAllString(html, -1)
	seen := make(map[string]bool, len(matches))
	var results []SearchResult
	for _, m := range matches {
		fullURL := "https://" + m
		if !seen[fullURL] {
			seen[fullURL] = true
			results = append(results, SearchResult{URL: fullURL})
		}
	}
	return results
}

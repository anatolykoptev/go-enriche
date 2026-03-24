package maps

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const ymSearchTimeout = 15 * time.Second

// yandexMapsOrgRe matches org URLs in Yandex Maps HTML.
var yandexMapsOrgRe = regexp.MustCompile(`yandex\.ru/maps/org/[^"<>,\s\\]+`)

// YandexMapsSearch returns a SearchFunc that queries Yandex Maps search
// page directly via ox-browser /fetch. No API key needed.
// This is more reliable than DDG for finding org pages.
func YandexMapsSearch(oxBrowserURL string) SearchFunc {
	baseURL := strings.TrimRight(oxBrowserURL, "/")
	client := &http.Client{Timeout: ymSearchTimeout}

	return func(ctx context.Context, query string) ([]SearchResult, error) {
		// Extract place name from the maps-specific query format.
		// Input: `site:yandex.ru/maps/org "PlaceName" City`
		// We only need "PlaceName City" for Yandex Maps search.
		searchQuery := cleanMapsQuery(query)

		mapsURL := "https://yandex.ru/maps/2/saint-petersburg/search/" +
			url.PathEscape(searchQuery) + "/"

		body, err := json.Marshal(map[string]string{"url": mapsURL})
		if err != nil {
			return nil, fmt.Errorf("yandex maps search: marshal: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			baseURL+"/fetch", strings.NewReader(string(body)))
		if err != nil {
			return nil, fmt.Errorf("yandex maps search: request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req) //nolint:gosec
		if err != nil {
			return nil, fmt.Errorf("yandex maps search: fetch: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("yandex maps search: HTTP %d", resp.StatusCode)
		}

		data, err := io.ReadAll(io.LimitReader(resp.Body, oxMaxResponseBytes))
		if err != nil {
			return nil, fmt.Errorf("yandex maps search: read: %w", err)
		}

		var fr struct {
			Body   string `json:"body"`
			Status int    `json:"status"`
			Error  string `json:"error,omitempty"`
		}
		if err := json.Unmarshal(data, &fr); err != nil {
			return nil, fmt.Errorf("yandex maps search: parse: %w", err)
		}
		if fr.Error != "" {
			return nil, fmt.Errorf("yandex maps search: %s", fr.Error)
		}

		// Extract unique org URLs from the HTML.
		matches := yandexMapsOrgRe.FindAllString(fr.Body, -1)
		seen := make(map[string]bool, len(matches))
		var results []SearchResult
		for _, m := range matches {
			fullURL := "https://" + m
			if seen[fullURL] {
				continue
			}
			seen[fullURL] = true
			results = append(results, SearchResult{URL: fullURL})
		}
		return results, nil
	}
}

// cleanMapsQuery strips the site:yandex.ru/maps/org prefix and quotes
// from a maps search query, leaving just the place name and city.
func cleanMapsQuery(q string) string {
	q = strings.ReplaceAll(q, `site:yandex.ru/maps/org`, "")
	q = strings.ReplaceAll(q, `"`, "")
	return strings.TrimSpace(q)
}

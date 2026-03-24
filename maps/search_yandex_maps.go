package maps

import (
	"context"
	"fmt"
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
// Note: Yandex Maps is SPA — this only works when org URLs leak into SSR HTML.
func YandexMapsSearch(oxBrowserURL string) SearchFunc {
	baseURL := strings.TrimRight(oxBrowserURL, "/")
	client := &http.Client{Timeout: ymSearchTimeout}

	return func(ctx context.Context, query string) ([]SearchResult, error) {
		searchQuery := cleanMapsQuery(query)
		mapsURL := "https://yandex.ru/maps/search/" + url.PathEscape(searchQuery) + "/"

		html, err := oxFetch(ctx, client, baseURL, mapsURL)
		if err != nil {
			return nil, fmt.Errorf("yandex maps search: %w", err)
		}

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

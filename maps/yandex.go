package maps

import (
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

const (
	defaultHTTPTimeout = 15 * time.Second
	maxOrgResults      = 3
)

// YandexMaps checks place status by:
//  1. Searching SearXNG for Yandex Maps org links.
//  2. Fetching the org page and parsing the embedded JSON status.
//
// No API keys, proxies, or browser fingerprinting required.
type YandexMaps struct {
	searxngURL string
	httpClient *http.Client
}

// YandexOption configures YandexMaps.
type YandexOption func(*YandexMaps)

// WithYandexHTTPClient overrides the default HTTP client (for testing).
func WithYandexHTTPClient(c *http.Client) YandexOption {
	return func(y *YandexMaps) { y.httpClient = c }
}

// NewYandexMaps creates a Yandex Maps checker.
// searxngURL is the base URL of the SearXNG instance (e.g., "http://searxng:8080").
func NewYandexMaps(searxngURL string, opts ...YandexOption) (*YandexMaps, error) {
	if searxngURL == "" {
		return nil, errors.New("yandex: searxng URL is required")
	}
	y := &YandexMaps{
		searxngURL: strings.TrimRight(searxngURL, "/"),
		httpClient: &http.Client{
			Timeout: defaultHTTPTimeout,
			CheckRedirect: func(_ *http.Request, via []*http.Request) error {
				if len(via) > 3 { //nolint:mnd
					return http.ErrUseLastResponse
				}
				return nil
			},
		},
	}
	for _, o := range opts {
		o(y)
	}
	return y, nil
}

// Check queries SearXNG for Yandex Maps org listings, then fetches each org page
// to read the embedded status JSON ("permanent-closed", "temporary-closed", "open").
func (y *YandexMaps) Check(ctx context.Context, name, city string) (*CheckResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	orgURLs, err := y.findOrgURLs(ctx, name, city)
	if err != nil {
		return nil, fmt.Errorf("yandex: search: %w", err)
	}
	if len(orgURLs) == 0 {
		return &CheckResult{Status: PlaceNotFound}, nil
	}

	// Check each org page for status.
	for _, orgURL := range orgURLs {
		status, err := y.fetchOrgStatus(ctx, orgURL)
		if err != nil {
			continue
		}
		result := &CheckResult{MapURL: orgURL}
		switch status {
		case "permanent-closed":
			result.Status = PlacePermanentClosed
		case "temporary-closed":
			result.Status = PlaceTemporaryClosed
		default:
			result.Status = PlaceOpen
		}
		return result, nil
	}

	return &CheckResult{Status: PlaceUnknown}, nil
}

// searxngResult is a single SearXNG JSON search result.
type searxngResult struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

// findOrgURLs searches SearXNG for Yandex Maps org links.
func (y *YandexMaps) findOrgURLs(ctx context.Context, name, city string) ([]string, error) {
	query := fmt.Sprintf(`site:yandex.ru/maps/org "%s" %s`, name, city)
	params := url.Values{}
	params.Set("q", query)
	params.Set("format", "json")
	params.Set("language", "ru")

	reqURL := y.searxngURL + "/search?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := y.httpClient.Do(req) //nolint:gosec // searxngURL is configured by the caller, not user input
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var data struct {
		Results []searxngResult `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	var urls []string
	for _, r := range data.Results {
		if isYandexMapsOrgURL(r.URL) && len(urls) < maxOrgResults {
			urls = append(urls, r.URL)
		}
	}
	return urls, nil
}

// fetchOrgStatus fetches a Yandex Maps org page and extracts the status field.
func (y *YandexMaps) fetchOrgStatus(ctx context.Context, orgURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, orgURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) "+
		"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Accept-Language", "ru-RU,ru;q=0.9")

	resp, err := y.httpClient.Do(req) //nolint:gosec // org URLs from SearXNG, not user input
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return parseOrgStatus(body), nil
}

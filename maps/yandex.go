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

// SearchResult is a single web search result (URL + Title).
type SearchResult struct {
	URL   string
	Title string
}

// SearchFunc searches the web and returns results. Used as a pluggable
// alternative to SearXNG for finding Yandex Maps org links.
type SearchFunc func(ctx context.Context, query string) ([]SearchResult, error)

// YandexMaps checks place status by:
//  1. Searching for Yandex Maps org links (via SearXNG or SearchFunc).
//  2. Fetching the org page and parsing the embedded JSON status.
//
// When an OrgFetcher is set, uses it to render SPA pages and extract full business data.
type YandexMaps struct {
	searxngURL string
	searchFunc SearchFunc
	httpClient *http.Client
	orgFetcher OrgFetcher
}

// YandexOption configures YandexMaps.
type YandexOption func(*YandexMaps)

// WithYandexHTTPClient overrides the default HTTP client (for testing).
func WithYandexHTTPClient(c *http.Client) YandexOption {
	return func(y *YandexMaps) { y.httpClient = c }
}

// WithOrgFetcher sets a browser-based fetcher for rendering SPA org pages.
// When set, full business data (phone, address, hours, etc.) is extracted.
func WithOrgFetcher(f OrgFetcher) YandexOption {
	return func(y *YandexMaps) { y.orgFetcher = f }
}

// WithSearchFunc sets a custom search function instead of SearXNG.
// When set, searxngURL is not required.
func WithSearchFunc(f SearchFunc) YandexOption {
	return func(y *YandexMaps) { y.searchFunc = f }
}

// NewYandexMaps creates a Yandex Maps checker.
// Either searxngURL or WithSearchFunc option must be provided.
func NewYandexMaps(searxngURL string, opts ...YandexOption) (*YandexMaps, error) {
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
	if y.searxngURL == "" && y.searchFunc == nil {
		return nil, errors.New("yandex: either searxng URL or search func is required")
	}
	return y, nil
}

// Check queries for Yandex Maps org listings, then fetches each org page
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

	// Check each org page for status (and business data if browser available).
	// Prefer open/temporary-closed branches over permanently closed ones,
	// since search may rank a closed branch higher than the active one.
	var closedResult *CheckResult
	for _, orgURL := range orgURLs {
		result, err := y.fetchAndParse(ctx, orgURL)
		if err != nil {
			continue
		}
		if result.Status != PlacePermanentClosed {
			return result, nil
		}
		if closedResult == nil {
			closedResult = result
		}
	}
	if closedResult != nil {
		return closedResult, nil
	}

	return &CheckResult{Status: PlaceUnknown}, nil
}

// searxngResult is a single SearXNG JSON search result.
type searxngResult struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

// findOrgURLs searches for Yandex Maps org links using SearchFunc or SearXNG.
func (y *YandexMaps) findOrgURLs(ctx context.Context, name, city string) ([]string, error) {
	query := fmt.Sprintf(`site:yandex.ru/maps/org "%s" %s`, name, city)

	if y.searchFunc != nil {
		return y.findOrgURLsViaFunc(ctx, query)
	}
	return y.findOrgURLsViaSearXNG(ctx, query)
}

// findOrgURLsViaFunc uses the pluggable SearchFunc.
func (y *YandexMaps) findOrgURLsViaFunc(ctx context.Context, query string) ([]string, error) {
	results, err := y.searchFunc(ctx, query)
	if err != nil {
		return nil, err
	}
	var urls []string
	for _, r := range results {
		if isYandexMapsOrgURL(r.URL) && len(urls) < maxOrgResults {
			urls = append(urls, r.URL)
		}
	}
	return urls, nil
}

// findOrgURLsViaSearXNG searches SearXNG JSON API (legacy path).
func (y *YandexMaps) findOrgURLsViaSearXNG(ctx context.Context, query string) ([]string, error) {
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

// fetchAndParse fetches an org page and returns a CheckResult with status and optional OrgData.
func (y *YandexMaps) fetchAndParse(ctx context.Context, orgURL string) (*CheckResult, error) {
	// Browser path: render SPA, extract full business data.
	if y.orgFetcher != nil {
		html, err := y.orgFetcher(ctx, orgURL)
		if err == nil && html != "" {
			od := parseOrgPage([]byte(html))
			od.MapURL = orgURL
			result := &CheckResult{
				MapURL:  orgURL,
				OrgData: od,
				Status:  od.Status,
			}
			if result.Status == "" {
				result.Status = PlaceUnknown
			}
			return result, nil
		}
		// Fall through to plain HTTP on browser failure.
	}

	status, err := y.fetchOrgStatus(ctx, orgURL)
	if err != nil {
		return nil, err
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

// fetchOrgStatus fetches a Yandex Maps org page and extracts the status field.
func (y *YandexMaps) fetchOrgStatus(ctx context.Context, orgURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, orgURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) "+
		"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Accept-Language", "ru-RU,ru;q=0.9")

	resp, err := y.httpClient.Do(req) //nolint:gosec // org URLs from search, not user input
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

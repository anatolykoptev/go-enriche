package maps

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Geocoder resolves addresses to coordinates via SearXNG map search.
type Geocoder struct {
	searxngURL string
	httpClient *http.Client
}

// GeocoderOption configures a Geocoder.
type GeocoderOption func(*Geocoder)

// WithGeoHTTPClient sets a custom HTTP client.
func WithGeoHTTPClient(c *http.Client) GeocoderOption {
	return func(g *Geocoder) { g.httpClient = c }
}

// NewGeocoder creates a Geocoder using the given SearXNG base URL.
func NewGeocoder(searxngURL string, opts ...GeocoderOption) (*Geocoder, error) {
	if searxngURL == "" {
		return nil, errors.New("geocoder: searxng URL is required")
	}
	g := &Geocoder{
		searxngURL: strings.TrimRight(searxngURL, "/"),
		httpClient: http.DefaultClient,
	}
	for _, o := range opts {
		o(g)
	}
	return g, nil
}

// geoResponse is the SearXNG JSON response for map queries.
type geoResponse struct {
	Results []geoResult `json:"results"`
}

// geoResult is a single SearXNG map result with coordinates.
type geoResult struct {
	Title     string  `json:"title"`
	URL       string  `json:"url"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// Geocode resolves an address+city to lat/lon via SearXNG categories=map.
// Returns ok=false if no valid coordinates found.
func (g *Geocoder) Geocode(ctx context.Context, address, city string) (lat, lon float64, ok bool) {
	query := address
	if city != "" {
		query = address + ", " + city
	}

	u, err := url.Parse(g.searxngURL + "/search")
	if err != nil {
		return 0, 0, false
	}

	q := u.Query()
	q.Set("q", query)
	q.Set("categories", "map")
	q.Set("format", "json")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, 0, false
	}

	resp, err := g.httpClient.Do(req) //nolint:gosec // searxngURL is configured by the caller, not user input
	if err != nil {
		return 0, 0, false
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, 0, false
	}

	var geo geoResponse
	if err := json.Unmarshal(body, &geo); err != nil {
		return 0, 0, false
	}

	if len(geo.Results) == 0 {
		return 0, 0, false
	}

	r := geo.Results[0]
	if r.Latitude == 0 && r.Longitude == 0 {
		return 0, 0, false
	}

	return r.Latitude, r.Longitude, true
}

// GeocoderOrNil is a convenience — returns nil on empty URL (no-op safe).
func GeocoderOrNil(searxngURL string, opts ...GeocoderOption) *Geocoder {
	g, err := NewGeocoder(searxngURL, opts...)
	if err != nil {
		return nil
	}
	return g
}

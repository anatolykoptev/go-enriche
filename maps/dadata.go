package maps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const daDataTimeout = 5 * time.Second

// daDataCleanURL is the DaData clean/address endpoint.
// Overridable in tests via package-level assignment (mirrors twoGISGeocodeURL pattern).
var daDataCleanURL = "https://cleaner.dadata.ru/api/v1/clean/address" //nolint:gochecknoglobals

// DaDataConfig configures the DaData address geocoder.
type DaDataConfig struct {
	APIKey     string       // DaData API token
	Secret     string       // DaData X-Secret header value
	HTTPClient *http.Client // optional; defaults to a client with daDataTimeout
}

// daDataAddress is one element of the DaData clean/address JSON response.
type daDataAddress struct {
	Result string `json:"result"`
	GeoLat string `json:"geo_lat"`
	GeoLon string `json:"geo_lon"`
	QcGeo  int    `json:"qc_geo"`
}

// DaDataChecker resolves a RU address to coordinates via DaData clean/address
// (FIAS/ГАР-backed). Implements maps.Checker.
//
// Address-gated: empty address → PlaceNotFound, no HTTP call.
// Credential-gated: empty APIKey or Secret → PlaceNotFound, no HTTP call (safe
// to wire before the key is provisioned; composite falls through).
// Precision-gated: only qc_geo ∈ {0, 1} (house-level) accepted; street/settlement/
// city/undefined → PlaceNotFound so CompositeChecker falls through to another
// checker. Bad-key/quota HTTP errors (non-2xx) surface as errors, not
// not-found, so callers can observe them.
//
// ToS: DaData permits storing coordinates derived from clean/address.
type DaDataChecker struct {
	apiKey string
	secret string
	client *http.Client
}

// NewDaData creates a DaDataChecker. HTTPClient in cfg is optional; if nil a
// default client with daDataTimeout is created.
func NewDaData(cfg DaDataConfig) *DaDataChecker {
	cl := cfg.HTTPClient
	if cl == nil {
		cl = &http.Client{Timeout: daDataTimeout}
	}
	return &DaDataChecker{
		apiKey: cfg.APIKey,
		secret: cfg.Secret,
		client: cl,
	}
}

// Check geocodes address to house-level coordinates via DaData clean/address.
// name is ignored (DaData is address-only).
func (c *DaDataChecker) Check(ctx context.Context, _, city, address string) (*CheckResult, error) {
	if strings.TrimSpace(address) == "" {
		return &CheckResult{Status: PlaceNotFound}, nil
	}
	if c.apiKey == "" || c.secret == "" {
		return &CheckResult{Status: PlaceNotFound}, nil
	}

	// Prepend city if provided.
	var q string
	if city != "" {
		q = strings.TrimSpace(city + ", " + address)
	} else {
		q = strings.TrimSpace(address)
	}

	body, err := json.Marshal([]string{q})
	if err != nil {
		return nil, fmt.Errorf("dadata: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, daDataCleanURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("dadata: %w", err)
	}
	req.Header.Set("Authorization", "Token "+c.apiKey)
	req.Header.Set("X-Secret", c.secret)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Non-2xx is actionable (401/403 = bad key, 429 = quota) — return error,
		// not PlaceNotFound. This prevents silent swallowing of misconfiguration.
		return nil, fmt.Errorf("dadata: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("dadata: read: %w", err)
	}

	return parseDaDataResponse(data)
}

// parseDaDataResponse converts raw DaData clean/address JSON into a CheckResult.
// Precision gate: only qc_geo ∈ {0, 1} (house-level) accepted; anything coarser
// or unparseable → PlaceNotFound, nil so the composite falls through.
func parseDaDataResponse(data []byte) (*CheckResult, error) {
	var items []daDataAddress
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("dadata: parse: %w", err)
	}

	if len(items) == 0 {
		return &CheckResult{Status: PlaceNotFound}, nil
	}

	item := items[0]

	// qc_geo 0 = exact house, 1 = nearest house — both accepted.
	// qc_geo 2 = street, 3 = settlement, 4 = city, 5 = undefined — too coarse.
	if item.QcGeo > 1 {
		return &CheckResult{Status: PlaceNotFound}, nil
	}

	if item.GeoLat == "" || item.GeoLon == "" {
		return &CheckResult{Status: PlaceNotFound}, nil
	}

	lat, errLat := strconv.ParseFloat(item.GeoLat, 64)
	lon, errLon := strconv.ParseFloat(item.GeoLon, 64)
	if errLat != nil || errLon != nil {
		return &CheckResult{Status: PlaceNotFound}, nil
	}

	return &CheckResult{
		Status: PlaceOpen,
		OrgData: &OrgData{
			Status:    PlaceOpen,
			Latitude:  lat,
			Longitude: lon,
			Address:   item.Result,
		},
	}, nil
}

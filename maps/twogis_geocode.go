package maps

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// twoGISGeocodeURL is the base URL for the 2GIS Geocoder endpoint.
// Overridable in tests via package-level assignment.
var twoGISGeocodeURL = "https://catalog.api.2gis.com/3.0/items/geocode" //nolint:gochecknoglobals

// TwoGISGeocoder resolves a street address to building-level coordinates via
// the 2GIS /items/geocode endpoint. Unlike TwoGISChecker (which searches by
// name and produces wrong-venue errors ~53% of the time), TwoGISGeocoder
// anchors the lookup on the address and returns the first matching building's
// point.
//
// Address-gated: if the caller passes an empty address, Check returns
// PlaceNotFound immediately so a composite checker can fall through to a
// name-based checker. Transport errors are returned as errors; API-level
// failures (non-200, empty items, malformed JSON) return PlaceNotFound with
// nil error to avoid tripping circuit breakers.
type TwoGISGeocoder struct {
	apiKey string
	client *http.Client
}

// NewTwoGISGeocoder creates a 2GIS geocoder. Empty APIKey defaults to "demo".
// Reuses TwoGISConfig and the same timeout as TwoGISChecker.
func NewTwoGISGeocoder(cfg TwoGISConfig) *TwoGISGeocoder {
	key := cfg.APIKey
	if key == "" {
		key = twoGISDemoKey
	}
	return &TwoGISGeocoder{
		apiKey: key,
		client: &http.Client{Timeout: twoGISTimeout},
	}
}

// Check geocodes the given address to building-level coordinates.
//
// When address is empty, returns PlaceNotFound immediately without an HTTP
// call — a geocoder requires an address; the caller should use a name-based
// checker instead.
//
// When the geocoder finds a building, returns PlaceOpen with coords in
// OrgData.Latitude / OrgData.Longitude. On API errors (non-200, empty result,
// malformed JSON) returns PlaceNotFound with nil error (fall-through). On
// transport errors (network failure) returns the error.
func (g *TwoGISGeocoder) Check(ctx context.Context, _, city, address string) (*CheckResult, error) {
	if address == "" {
		return &CheckResult{Status: PlaceNotFound}, nil
	}

	var q string
	if city != "" {
		q = strings.TrimSpace(city + ", " + address)
	} else {
		q = strings.TrimSpace(address)
	}

	params := url.Values{
		"q": {q},
		// items.type is required for the building-level precision gate: 2GIS
		// returns meta.code=200 with coarse fallback items (adm_div, street,
		// settlement) when no exact building is found. Without this field the
		// gate cannot distinguish building from centroid.
		"fields": {"items.point,items.type"},
		"key":    {g.apiKey},
	}

	reqURL := twoGISGeocodeURL + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("2gis geocode: %w", err)
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("2gis geocode: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &CheckResult{Status: PlaceNotFound}, nil
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return &CheckResult{Status: PlaceNotFound}, nil //nolint:nilerr
	}

	var gr twoGISResponse
	if err := json.Unmarshal(data, &gr); err != nil {
		return &CheckResult{Status: PlaceNotFound}, nil //nolint:nilerr
	}

	if len(gr.Result.Items) == 0 {
		return &CheckResult{Status: PlaceNotFound}, nil
	}

	// Take the first item that has a non-nil point AND building-level precision.
	//
	// Accept-set (verified via 2GIS /items/geocode with key=demo):
	//   "building" — exact address resolution to a building polygon/point.
	//   "branch"   — business branch tied to a building; carries a real building
	//                point (not a centroid).
	//
	// Reject-set (coarse fallbacks — confident-WRONG for a specific address):
	//   "adm_div"    — administrative division centroid (city, district, etc.)
	//   "street"     — street midpoint/centroid
	//   "settlement" — settlement centroid
	//   ""           — unknown / field not returned; reject for safety
	//
	// Rejected items are skipped; if no building-level item is found the
	// composite falls through to the by-name anchored checker.
	for _, item := range gr.Result.Items {
		if item.Point == nil {
			continue
		}
		if item.Type != "building" && item.Type != "branch" {
			continue
		}
		od := &OrgData{
			Status:    PlaceOpen,
			Name:      item.Name,
			Address:   item.AddressName,
			Latitude:  item.Point.Lat,
			Longitude: item.Point.Lon,
		}
		return &CheckResult{
			Status:   PlaceOpen,
			RawTitle: item.Name,
			OrgData:  od,
		}, nil
	}

	// All items have no point — treat as not-found.
	return &CheckResult{Status: PlaceNotFound}, nil
}

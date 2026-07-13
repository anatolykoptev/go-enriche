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

// twoGISItemTypeBuilding is the 2GIS /items/geocode item.type value for
// exact building-level address resolution (see the accept-set comment on
// the precision gate below).
const twoGISItemTypeBuilding = "building"

// twoGISGeocodeURL is the base URL for the 2GIS Geocoder endpoint.
// Overridable in tests via package-level assignment.
var twoGISGeocodeURL = "https://catalog.api.2gis.com/3.0/items/geocode" //nolint:gochecknoglobals

// TwoGISGeocoder resolves a street address to building-level coordinates via
// the 2GIS /items/geocode endpoint. Unlike TwoGISChecker (which searches by
// name and produces wrong-venue errors ~53% of the time), TwoGISGeocoder
// anchors the lookup on the address and returns the first matching building's
// point.
//
// Address-gated: if the caller passes an empty or all-whitespace address, Check
// returns PlaceNotFound immediately so a composite checker can fall through to
// a name-based checker. Transport errors are returned as errors; API-level
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
// When address is empty or all-whitespace, returns PlaceNotFound immediately
// without an HTTP call — a geocoder requires an address; the caller should use
// a name-based checker instead.
//
// When the geocoder finds a building, returns PlaceOpen with coords in
// OrgData.Latitude / OrgData.Longitude. Also populates OrgData.Name and
// OrgData.Address from the response when 2GIS returns them.
//
// Error semantics mirror TwoGISChecker.Check:
//   - Transport errors (network failure, read failure) → error.
//   - meta.code != 200 (quota/auth failure, e.g. 403/429) → error so the
//     Resilient wrapper / circuit breaker can trip. A 429 must NOT look like
//     "address not found" — over 1000 places that would silently drop every
//     remaining result.
//   - HTTP non-200 (server error) → PlaceNotFound, nil (fall-through).
//   - Empty items or all-zero-coord items → PlaceNotFound, nil (fall-through).
func (g *TwoGISGeocoder) Check(ctx context.Context, _, city, address string) (*CheckResult, error) {
	if strings.TrimSpace(address) == "" {
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
		// items.address_name and items.name populate OrgData for downstream
		// consumers; items.point provides coords (the primary output).
		"fields": {"items.point,items.type,items.address_name,items.name"},
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
		return nil, fmt.Errorf("2gis geocode: read: %w", err)
	}

	var gr twoGISResponse
	if err := json.Unmarshal(data, &gr); err != nil {
		return &CheckResult{Status: PlaceNotFound}, nil //nolint:nilerr
	}

	// Mirror TwoGISChecker: non-200 meta.code signals quota exhaustion (429)
	// or auth failure (403). These arrive with HTTP 200 + empty items, so they
	// would otherwise silently degrade to PlaceNotFound. Return an error so
	// the Resilient wrapper records it and the breaker can trip.
	if gr.Meta.Code != http.StatusOK {
		return nil, fmt.Errorf("2gis geocode: meta code %d", gr.Meta.Code)
	}

	if len(gr.Result.Items) == 0 {
		return &CheckResult{Status: PlaceNotFound}, nil
	}

	if od := selectBestGeocodeItem(gr.Result.Items); od != nil {
		return &CheckResult{
			Status:   PlaceOpen,
			RawTitle: od.Name,
			OrgData:  od,
		}, nil
	}

	// All items have nil or zero-coord points, or no building-level item.
	return &CheckResult{Status: PlaceNotFound}, nil
}

// selectBestGeocodeItem returns the first 2GIS geocode item that has a
// non-zero point and building-level precision.
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
// 2GIS can also return point:{lat:0,lon:0} (Gulf of Guinea) when the
// address resolves to a structural zero — those are skipped first.
func selectBestGeocodeItem(items []twoGISItem) *OrgData {
	for _, item := range items {
		if item.Point == nil || (item.Point.Lat == 0 && item.Point.Lon == 0) {
			continue
		}
		if item.Type != twoGISItemTypeBuilding && item.Type != "branch" {
			continue
		}
		return &OrgData{
			Status:    PlaceOpen,
			Name:      item.Name,
			Address:   item.AddressName,
			Latitude:  item.Point.Lat,
			Longitude: item.Point.Lon,
		}
	}
	return nil
}

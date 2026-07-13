package maps

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// makeTwoGISGeocodeResponse builds a twoGISResponse with the given items,
// reusing the same struct as the catalog checker.
func makeTwoGISGeocodeResponse(items []twoGISItem) twoGISResponse {
	return twoGISResponse{
		Meta: struct{ Code int `json:"code"` }{Code: http.StatusOK},
		Result: struct {
			Items []twoGISItem `json:"items"`
			Total int          `json:"total"`
		}{Items: items, Total: len(items)},
	}
}

// newTwoGISGeocodeMockServer returns a test server that records the last
// incoming request and serves the given JSON response.
func newTwoGISGeocodeMockServer(t *testing.T, resp twoGISResponse, hits *atomic.Int32, lastQ *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if lastQ != nil {
			q := r.URL.Query().Get("q")
			*lastQ = q
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

// newTwoGISGeocodeCheckerWithURL creates a TwoGISGeocoder that targets the
// given base URL (test server). It overrides the package-level variable so
// the checker talks to the mock server.
func newTwoGISGeocodeCheckerWithURL(baseURL string) *TwoGISGeocoder {
	twoGISGeocodeURL = baseURL
	return NewTwoGISGeocoder(TwoGISConfig{APIKey: "demo"})
}

// --- Tests ---

// TestTwoGISGeocoder_AddressPresent_ReturnsCoords verifies the happy path:
// when a street address is provided and the geocoder returns a building point,
// CheckResult has the correct coords and a non-NotFound status.
//
// Regression guard: if NewTwoGISGeocoder or Check are removed/broken, or
// if Point parsing regresses, this test goes RED.
func TestTwoGISGeocoder_AddressPresent_ReturnsCoords(t *testing.T) {
	resp := makeTwoGISGeocodeResponse([]twoGISItem{
		{
			Name:        "Дом Зингера",
			AddressName: "Невский проспект, 28 / набережная канала Грибоедова, 21",
			Point:       &twoGISPoint{Lat: 59.935858, Lon: 30.325908},
			Type:        "building",
		},
	})

	var hits atomic.Int32
	var lastQ string
	srv := newTwoGISGeocodeMockServer(t, resp, &hits, &lastQ)
	defer srv.Close()

	g := newTwoGISGeocodeCheckerWithURL(srv.URL)

	result, err := g.Check(context.Background(), "Зингер", "Санкт-Петербург", "Невский проспект, 28")
	if err != nil {
		t.Fatalf("Check() error: %v", err)
	}
	if result.Status == PlaceNotFound {
		t.Errorf("status = %q, want non-NotFound (address present + point returned)", result.Status)
	}
	if result.OrgData == nil {
		t.Fatal("OrgData is nil, want populated coords")
	}
	if result.OrgData.Latitude != 59.935858 {
		t.Errorf("Latitude = %f, want 59.935858", result.OrgData.Latitude)
	}
	if result.OrgData.Longitude != 30.325908 {
		t.Errorf("Longitude = %f, want 30.325908", result.OrgData.Longitude)
	}
	if hits.Load() == 0 {
		t.Error("HTTP server was never hit — Check() must make a request when address is non-empty")
	}
}

// TestTwoGISGeocoder_EmptyAddress_PlaceNotFoundNoHTTP verifies the
// address-gated behaviour: when address is empty, Check returns PlaceNotFound
// immediately without making any HTTP call.
//
// Regression guard: if the early-return guard is removed, hits > 0 → RED.
func TestTwoGISGeocoder_EmptyAddress_PlaceNotFoundNoHTTP(t *testing.T) {
	// Server should never be called; use a sentinel response to detect leaks.
	resp := makeTwoGISGeocodeResponse([]twoGISItem{
		{Name: "should not be hit", Point: &twoGISPoint{Lat: 1, Lon: 1}},
	})

	var hits atomic.Int32
	srv := newTwoGISGeocodeMockServer(t, resp, &hits, nil)
	defer srv.Close()

	g := newTwoGISGeocodeCheckerWithURL(srv.URL)

	result, err := g.Check(context.Background(), "Зингер", "Санкт-Петербург", "")
	if err != nil {
		t.Fatalf("Check() error: %v", err)
	}
	if result.Status != PlaceNotFound {
		t.Errorf("status = %q, want PlaceNotFound (empty address must short-circuit)", result.Status)
	}
	if hits.Load() != 0 {
		t.Errorf("HTTP server was hit %d times, want 0 (no request on empty address)", hits.Load())
	}
}

// TestTwoGISGeocoder_EmptyItems_PlaceNotFound verifies that when the geocoder
// returns an empty items array, Check returns PlaceNotFound with nil error
// (fall-through semantics — not a transport error).
//
// Regression guard: if empty-items check is removed, status won't be NotFound → RED.
func TestTwoGISGeocoder_EmptyItems_PlaceNotFound(t *testing.T) {
	resp := makeTwoGISGeocodeResponse(nil)

	var hits atomic.Int32
	srv := newTwoGISGeocodeMockServer(t, resp, &hits, nil)
	defer srv.Close()

	g := newTwoGISGeocodeCheckerWithURL(srv.URL)

	result, err := g.Check(context.Background(), "Несуществующее место", "Санкт-Петербург", "Выдуманная ул., 999")
	if err != nil {
		t.Fatalf("Check() error = %v, want nil (empty items is not a transport error)", err)
	}
	if result.Status != PlaceNotFound {
		t.Errorf("status = %q, want PlaceNotFound (empty items → fall-through)", result.Status)
	}
}

// TestTwoGISGeocoder_Non200_PlaceNotFound verifies that a non-200 HTTP status
// code from the geocoder returns PlaceNotFound with nil error (fall-through,
// not a hard error that would trip a circuit breaker).
//
// Regression guard: if non-200 handling returns an error instead, tests
// exercising CompositeChecker fall-through would break → RED.
func TestTwoGISGeocoder_Non200_PlaceNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"meta":{"code":503}}`))
	}))
	defer srv.Close()

	g := newTwoGISGeocodeCheckerWithURL(srv.URL)

	result, err := g.Check(context.Background(), "Тест", "Москва", "Тверская, 1")
	if err != nil {
		t.Fatalf("Check() error = %v, want nil (non-200 must fall-through, not hard-error)", err)
	}
	if result.Status != PlaceNotFound {
		t.Errorf("status = %q, want PlaceNotFound (non-200 → fall-through)", result.Status)
	}
}

// TestTwoGISGeocoder_MalformedJSON_PlaceNotFound verifies that malformed JSON
// returns PlaceNotFound with nil error (same fall-through semantics as non-200).
func TestTwoGISGeocoder_MalformedJSON_PlaceNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{broken json`))
	}))
	defer srv.Close()

	g := newTwoGISGeocodeCheckerWithURL(srv.URL)

	result, err := g.Check(context.Background(), "Тест", "Москва", "Арбат, 5")
	if err != nil {
		t.Fatalf("Check() error = %v, want nil (malformed JSON must fall-through)", err)
	}
	if result.Status != PlaceNotFound {
		t.Errorf("status = %q, want PlaceNotFound (malformed JSON → fall-through)", result.Status)
	}
}

// TestTwoGISGeocoder_CityPrependedToAddress verifies that the outgoing "q"
// parameter contains both the city and the address, in that order.
//
// Regression guard: if city prepend is removed or swapped, query won't have
// the city prefix → result disambiguation breaks → RED.
func TestTwoGISGeocoder_CityPrependedToAddress(t *testing.T) {
	resp := makeTwoGISGeocodeResponse([]twoGISItem{
		{Name: "Кафе", Point: &twoGISPoint{Lat: 59.9, Lon: 30.3}},
	})

	var hits atomic.Int32
	var lastQ string
	srv := newTwoGISGeocodeMockServer(t, resp, &hits, &lastQ)
	defer srv.Close()

	g := newTwoGISGeocodeCheckerWithURL(srv.URL)

	_, err := g.Check(context.Background(), "Кафе", "Санкт-Петербург", "Садовая, 7")
	if err != nil {
		t.Fatalf("Check() error: %v", err)
	}
	if !strings.Contains(lastQ, "Санкт-Петербург") {
		t.Errorf("q param %q does not contain city 'Санкт-Петербург'", lastQ)
	}
	if !strings.Contains(lastQ, "Садовая") {
		t.Errorf("q param %q does not contain address 'Садовая'", lastQ)
	}
	// City must precede address in the q string.
	cityIdx := strings.Index(lastQ, "Санкт-Петербург")
	addrIdx := strings.Index(lastQ, "Садовая")
	if cityIdx > addrIdx {
		t.Errorf("q param %q: city index %d > address index %d, want city first", lastQ, cityIdx, addrIdx)
	}
}

// TestTwoGISGeocoder_BuildingType_ReturnsCoords verifies that an item with
// type "building" is accepted by the precision gate.
//
// Regression guard: if the type gate is removed or changed to reject
// "building", this test → RED.
func TestTwoGISGeocoder_BuildingType_ReturnsCoords(t *testing.T) {
	resp := makeTwoGISGeocodeResponse([]twoGISItem{
		{
			Name:        "Дом Зингера",
			AddressName: "Невский проспект, 28",
			Point:       &twoGISPoint{Lat: 59.935858, Lon: 30.325908},
			Type:        "building",
		},
	})

	var hits atomic.Int32
	srv := newTwoGISGeocodeMockServer(t, resp, &hits, nil)
	defer srv.Close()

	g := newTwoGISGeocodeCheckerWithURL(srv.URL)

	result, err := g.Check(context.Background(), "Дом Зингера", "Санкт-Петербург", "Невский проспект, 28")
	if err != nil {
		t.Fatalf("Check() error: %v", err)
	}
	if result.Status == PlaceNotFound {
		t.Errorf("status = PlaceNotFound, want non-NotFound (building type must be accepted)")
	}
	if result.OrgData == nil {
		t.Fatal("OrgData is nil, want populated coords for building-type item")
	}
	if result.OrgData.Latitude != 59.935858 {
		t.Errorf("Latitude = %f, want 59.935858", result.OrgData.Latitude)
	}
}

// TestTwoGISGeocoder_AdmDivType_PlaceNotFound verifies that an item with
// type "adm_div" (administrative division / city centroid) is REJECTED by
// the precision gate even though it carries a point.
//
// Regression guard: reverting the type gate makes this pass vacuously because
// the point is present — run RED to confirm it fails without the gate → GREEN
// only after the gate is in place.
func TestTwoGISGeocoder_AdmDivType_PlaceNotFound(t *testing.T) {
	resp := makeTwoGISGeocodeResponse([]twoGISItem{
		{
			Name:  "Санкт-Петербург",
			Point: &twoGISPoint{Lat: 59.939095, Lon: 30.315868},
			Type:  "adm_div",
		},
	})

	var hits atomic.Int32
	srv := newTwoGISGeocodeMockServer(t, resp, &hits, nil)
	defer srv.Close()

	g := newTwoGISGeocodeCheckerWithURL(srv.URL)

	result, err := g.Check(context.Background(), "Тест", "Санкт-Петербург", "Невский проспект, 1")
	if err != nil {
		t.Fatalf("Check() error: %v", err)
	}
	if result.Status != PlaceNotFound {
		t.Errorf("status = %q, want PlaceNotFound (adm_div centroid must be rejected)", result.Status)
	}
}

// TestTwoGISGeocoder_StreetType_PlaceNotFound verifies that an item with
// type "street" (street centroid) is REJECTED by the precision gate.
func TestTwoGISGeocoder_StreetType_PlaceNotFound(t *testing.T) {
	resp := makeTwoGISGeocodeResponse([]twoGISItem{
		{
			Name:  "Невский проспект",
			Point: &twoGISPoint{Lat: 59.930, Lon: 30.360},
			Type:  "street",
		},
	})

	var hits atomic.Int32
	srv := newTwoGISGeocodeMockServer(t, resp, &hits, nil)
	defer srv.Close()

	g := newTwoGISGeocodeCheckerWithURL(srv.URL)

	result, err := g.Check(context.Background(), "Кафе", "Санкт-Петербург", "Невский проспект, 10")
	if err != nil {
		t.Fatalf("Check() error: %v", err)
	}
	if result.Status != PlaceNotFound {
		t.Errorf("status = %q, want PlaceNotFound (street centroid must be rejected)", result.Status)
	}
}

// TestTwoGISGeocoder_MixedItems_FirstAdmDivSecondBuilding verifies that when
// the first item is coarse (adm_div) and the second is "building", the
// selector skips the coarse item and returns the building one.
//
// This is the mixed-items regression: the old code took item[0] unconditionally.
func TestTwoGISGeocoder_MixedItems_FirstAdmDivSecondBuilding(t *testing.T) {
	resp := makeTwoGISGeocodeResponse([]twoGISItem{
		{
			Name:  "Санкт-Петербург",
			Point: &twoGISPoint{Lat: 59.939095, Lon: 30.315868},
			Type:  "adm_div",
		},
		{
			Name:        "Дом 28",
			AddressName: "Невский проспект, 28",
			Point:       &twoGISPoint{Lat: 59.935858, Lon: 30.325908},
			Type:        "building",
		},
	})

	var hits atomic.Int32
	srv := newTwoGISGeocodeMockServer(t, resp, &hits, nil)
	defer srv.Close()

	g := newTwoGISGeocodeCheckerWithURL(srv.URL)

	result, err := g.Check(context.Background(), "Дом 28", "Санкт-Петербург", "Невский проспект, 28")
	if err != nil {
		t.Fatalf("Check() error: %v", err)
	}
	if result.Status == PlaceNotFound {
		t.Errorf("status = PlaceNotFound, want non-NotFound (building item present in list)")
	}
	if result.OrgData == nil {
		t.Fatal("OrgData is nil, want populated coords from building item")
	}
	if result.OrgData.Latitude != 59.935858 {
		t.Errorf("Latitude = %f, want 59.935858 (second/building item, not first/adm_div)", result.OrgData.Latitude)
	}
}

// TestTwoGISGeocoder_FieldsIncludeItemsType verifies that the outgoing HTTP
// request includes "items.type" in the fields parameter.
//
// Regression guard: if items.type is removed from fields, 2GIS won't return
// the type field → gate can't function → this test → RED.
func TestTwoGISGeocoder_FieldsIncludeItemsType(t *testing.T) {
	resp := makeTwoGISGeocodeResponse([]twoGISItem{
		{Name: "Кафе", Point: &twoGISPoint{Lat: 55.0, Lon: 37.0}, Type: "building"},
	})

	var hits atomic.Int32
	var lastFields string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		lastFields = r.URL.Query().Get("fields")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	g := newTwoGISGeocodeCheckerWithURL(srv.URL)

	_, err := g.Check(context.Background(), "Кафе", "Москва", "Арбат, 1")
	if err != nil {
		t.Fatalf("Check() error: %v", err)
	}
	if !strings.Contains(lastFields, "items.type") {
		t.Errorf("fields param %q does not contain 'items.type', want it present for type gate", lastFields)
	}
}

// TestTwoGISGeocoder_EmptyCityUsesAddressOnly verifies that when city is empty,
// the q parameter is just the address (no leading comma/space junk).
func TestTwoGISGeocoder_EmptyCityUsesAddressOnly(t *testing.T) {
	resp := makeTwoGISGeocodeResponse([]twoGISItem{
		{Name: "Кафе", Point: &twoGISPoint{Lat: 55.0, Lon: 37.0}, Type: "building"},
	})

	var hits atomic.Int32
	var lastQ string
	srv := newTwoGISGeocodeMockServer(t, resp, &hits, &lastQ)
	defer srv.Close()

	g := newTwoGISGeocodeCheckerWithURL(srv.URL)

	_, err := g.Check(context.Background(), "Кафе", "", "Арбат, 5")
	if err != nil {
		t.Fatalf("Check() error: %v", err)
	}
	if strings.HasPrefix(lastQ, ",") || strings.HasPrefix(lastQ, " ") {
		t.Errorf("q param %q starts with junk when city is empty, want clean address", lastQ)
	}
	if !strings.Contains(lastQ, "Арбат") {
		t.Errorf("q param %q does not contain address 'Арбат'", lastQ)
	}
}

// TestTwoGISGeocoder_MetaCodeNon200_ReturnsError guards FIX 1 (MAJOR): 2GIS
// returns HTTP 200 with meta.code=429 (quota) or meta.code=403 (bad key).
// Previously the geocoder ignored meta.code and silently returned PlaceNotFound —
// over a 1000-place run a single 429 would silently drop every remaining place.
// Now it must return an ERROR so Resilient/circuit-breaker sees the failure.
//
// Regression guard: revert the meta.code check → this test returns nil error
// (or PlaceNotFound) instead of an error → RED.
func TestTwoGISGeocoder_MetaCodeNon200_ReturnsError(t *testing.T) {
	cases := []struct {
		name     string
		metaCode int
	}{
		{"quota_exhausted_429", http.StatusTooManyRequests},
		{"bad_key_403", http.StatusForbidden},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// HTTP transport returns 200; only meta.code signals the failure.
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				// Encode a response where meta.code is non-200 but items is empty.
				resp := twoGISResponse{}
				resp.Meta.Code = tc.metaCode
				_ = json.NewEncoder(w).Encode(resp)
			}))
			defer srv.Close()

			g := newTwoGISGeocodeCheckerWithURL(srv.URL)

			result, err := g.Check(context.Background(), "Тест", "Москва", "Тверская, 1")
			if err == nil {
				t.Errorf("Check() error = nil, want non-nil error for meta.code=%d (quota/auth failure must not look like PlaceNotFound)", tc.metaCode)
			}
			// result may be nil or non-nil; just confirm we did not silently succeed.
			if result != nil && result.Status != PlaceNotFound {
				t.Errorf("result.Status = %q, want PlaceNotFound or nil result when meta.code=%d returns error", result.Status, tc.metaCode)
			}
		})
	}
}

// TestTwoGISGeocoder_ZeroCoordPoint_PlaceNotFound guards FIX 3: 2GIS can return
// point:{lat:0,lon:0} (Gulf of Guinea) when the address resolves to a structural
// zero. Previously the first item with a non-nil point was accepted, so a
// zero-coord item was returned as PlaceOpen at 0,0.
// Now zero-coord items must be skipped; if all items have zero/nil coords →
// PlaceNotFound.
//
// Regression guard: remove the zero-coord skip → this test returns PlaceOpen
// with 0,0 → RED.
func TestTwoGISGeocoder_ZeroCoordPoint_PlaceNotFound(t *testing.T) {
	resp := makeTwoGISGeocodeResponse([]twoGISItem{
		{
			Name:  "Пустая точка",
			Point: &twoGISPoint{Lat: 0, Lon: 0},
		},
	})

	var hits atomic.Int32
	srv := newTwoGISGeocodeMockServer(t, resp, &hits, nil)
	defer srv.Close()

	g := newTwoGISGeocodeCheckerWithURL(srv.URL)

	result, err := g.Check(context.Background(), "Тест", "Москва", "Арбат, 5")
	if err != nil {
		t.Fatalf("Check() error = %v, want nil (zero-coord is not a transport error)", err)
	}
	if result.Status != PlaceNotFound {
		t.Errorf("status = %q (Lat=%v Lon=%v), want PlaceNotFound (zero-coord point must be skipped)",
			result.Status, result.OrgData, result.OrgData)
	}
}

// TestTwoGISGeocoder_WhitespaceAddress_PlaceNotFoundNoHTTP guards FIX 2: an
// all-whitespace address passed the old "" gate and burned a 2GIS call.
// Now strings.TrimSpace check must reject it without any HTTP call.
//
// Regression guard: revert to address=="" → whitespace hits the server → RED.
func TestTwoGISGeocoder_WhitespaceAddress_PlaceNotFoundNoHTTP(t *testing.T) {
	resp := makeTwoGISGeocodeResponse([]twoGISItem{
		{Name: "should not be hit", Point: &twoGISPoint{Lat: 1, Lon: 1}},
	})

	var hits atomic.Int32
	srv := newTwoGISGeocodeMockServer(t, resp, &hits, nil)
	defer srv.Close()

	g := newTwoGISGeocodeCheckerWithURL(srv.URL)

	result, err := g.Check(context.Background(), "Тест", "Москва", "   ")
	if err != nil {
		t.Fatalf("Check() error = %v, want nil", err)
	}
	if result.Status != PlaceNotFound {
		t.Errorf("status = %q, want PlaceNotFound (whitespace address must short-circuit)", result.Status)
	}
	if hits.Load() != 0 {
		t.Errorf("HTTP server was hit %d times, want 0 (no request for whitespace address)", hits.Load())
	}
}

// TestTwoGISGeocoder_MultiItemFirstZeroSecondValid guards FIX 3 multi-item path:
// when the first item has a zero coord and the second has valid coords, Check
// must skip the first and return the second's coords.
//
// Regression guard: remove zero-coord skip → first item (0,0) is returned → RED.
func TestTwoGISGeocoder_MultiItemFirstZeroSecondValid(t *testing.T) {
	resp := makeTwoGISGeocodeResponse([]twoGISItem{
		{Name: "Нулевая", Point: &twoGISPoint{Lat: 0, Lon: 0}},
		{Name: "Дом Зингера", AddressName: "Невский проспект, 28", Point: &twoGISPoint{Lat: 59.935858, Lon: 30.325908}, Type: "building"},
	})

	var hits atomic.Int32
	srv := newTwoGISGeocodeMockServer(t, resp, &hits, nil)
	defer srv.Close()

	g := newTwoGISGeocodeCheckerWithURL(srv.URL)

	result, err := g.Check(context.Background(), "Зингер", "Санкт-Петербург", "Невский проспект, 28")
	if err != nil {
		t.Fatalf("Check() error = %v, want nil", err)
	}
	if result.Status == PlaceNotFound {
		t.Errorf("status = PlaceNotFound, want PlaceOpen (second item has valid coords)")
	}
	if result.OrgData == nil {
		t.Fatal("OrgData is nil, want populated coords from second item")
	}
	if result.OrgData.Latitude != 59.935858 {
		t.Errorf("Latitude = %f, want 59.935858 (second item's coords)", result.OrgData.Latitude)
	}
}

// TestTwoGISGeocoder_AddressNamePopulated guards FIX 5: when fields includes
// items.address_name and items.name, OrgData.Address and OrgData.Name must be
// populated from the response.
//
// Regression guard: remove items.address_name from fields (or stop populating
// OrgData.Address) → Address/Name empty → RED.
func TestTwoGISGeocoder_AddressNamePopulated(t *testing.T) {
	resp := makeTwoGISGeocodeResponse([]twoGISItem{
		{
			Name:        "Дом Зингера",
			AddressName: "Невский проспект, 28",
			Point:       &twoGISPoint{Lat: 59.935858, Lon: 30.325908},
			Type:        "building",
		},
	})

	var hits atomic.Int32
	var lastFields string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		lastFields = r.URL.Query().Get("fields")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	g := newTwoGISGeocodeCheckerWithURL(srv.URL)

	result, err := g.Check(context.Background(), "Зингер", "Санкт-Петербург", "Невский проспект, 28")
	if err != nil {
		t.Fatalf("Check() error = %v, want nil", err)
	}
	if result.OrgData == nil {
		t.Fatal("OrgData is nil")
	}
	if result.OrgData.Address != "Невский проспект, 28" {
		t.Errorf("OrgData.Address = %q, want 'Невский проспект, 28' (address_name must be populated)", result.OrgData.Address)
	}
	if result.OrgData.Name != "Дом Зингера" {
		t.Errorf("OrgData.Name = %q, want 'Дом Зингера' (name must be populated)", result.OrgData.Name)
	}
	// Verify the fields param actually requested address_name and name.
	if !strings.Contains(lastFields, "items.address_name") {
		t.Errorf("fields param %q does not contain 'items.address_name' — 2GIS won't return address", lastFields)
	}
	if !strings.Contains(lastFields, "items.name") {
		t.Errorf("fields param %q does not contain 'items.name' — 2GIS won't return name", lastFields)
	}
}

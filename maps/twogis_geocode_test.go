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

// TestTwoGISGeocoder_EmptyCityUsesAddressOnly verifies that when city is empty,
// the q parameter is just the address (no leading comma/space junk).
func TestTwoGISGeocoder_EmptyCityUsesAddressOnly(t *testing.T) {
	resp := makeTwoGISGeocodeResponse([]twoGISItem{
		{Name: "Кафе", Point: &twoGISPoint{Lat: 55.0, Lon: 37.0}},
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

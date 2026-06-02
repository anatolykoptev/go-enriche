package maps

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func makeTwoGISResponse(items []twoGISItem) twoGISResponse {
	return twoGISResponse{
		Meta:   struct{ Code int `json:"code"` }{Code: http.StatusOK},
		Result: struct {
			Items []twoGISItem `json:"items"`
			Total int          `json:"total"`
		}{Items: items, Total: len(items)},
	}
}

// twoGISTransport redirects all requests to a local test server, stripping
// the test server prefix so the 2GIS handler path logic still works.
type twoGISTransport struct {
	target string
	// lastQuery captures the "q" query parameter of the most recent request.
	lastQuery string
}

func (rt *twoGISTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.lastQuery = req.URL.Query().Get("q")
	newURL := rt.target + "?" + req.URL.RawQuery
	newReq, err := http.NewRequestWithContext(req.Context(), req.Method, newURL, req.Body)
	if err != nil {
		return nil, err
	}
	newReq.Header = req.Header
	return http.DefaultTransport.RoundTrip(newReq)
}

func newTwoGISMockServer(t *testing.T, resp twoGISResponse, checkFields bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if checkFields {
			fields := r.URL.Query().Get("fields")
			if !strings.Contains(fields, "items.point") {
				t.Errorf("fields %q does not include items.point", fields)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestTwoGIS_CoordsPopulated(t *testing.T) {
	resp := makeTwoGISResponse([]twoGISItem{
		{
			Name:        "Кофейня Центр",
			AddressName: "Невский пр., 1",
			Point:       &twoGISPoint{Lat: 59.9343, Lon: 30.3351},
		},
	})
	srv := newTwoGISMockServer(t, resp, true)
	defer srv.Close()

	checker := &TwoGISChecker{
		apiKey: "demo",
		client: &http.Client{Transport: &twoGISTransport{target: srv.URL}},
	}

	result, err := checker.Check(context.Background(), "Кофейня Центр", "Санкт-Петербург", "")
	if err != nil {
		t.Fatalf("Check() error: %v", err)
	}
	if result.Status != PlaceOpen {
		t.Errorf("status = %q, want %q", result.Status, PlaceOpen)
	}
	if result.OrgData == nil {
		t.Fatal("OrgData is nil")
	}
	if result.OrgData.Latitude != 59.9343 {
		t.Errorf("Latitude = %f, want 59.9343", result.OrgData.Latitude)
	}
	if result.OrgData.Longitude != 30.3351 {
		t.Errorf("Longitude = %f, want 30.3351", result.OrgData.Longitude)
	}
}

func TestTwoGIS_NoPointLeavesCoordsZero(t *testing.T) {
	resp := makeTwoGISResponse([]twoGISItem{
		{
			Name:        "Без координат",
			AddressName: "ул. Пушкина, 1",
			Point:       nil,
		},
	})
	srv := newTwoGISMockServer(t, resp, false)
	defer srv.Close()

	checker := &TwoGISChecker{
		apiKey: "demo",
		client: &http.Client{Transport: &twoGISTransport{target: srv.URL}},
	}

	result, err := checker.Check(context.Background(), "Без координат", "Москва", "")
	if err != nil {
		t.Fatalf("Check() error: %v", err)
	}
	if result.OrgData == nil {
		t.Fatal("OrgData is nil")
	}
	if result.OrgData.Latitude != 0 {
		t.Errorf("Latitude = %f, want 0", result.OrgData.Latitude)
	}
	if result.OrgData.Longitude != 0 {
		t.Errorf("Longitude = %f, want 0", result.OrgData.Longitude)
	}
}

// TestTwoGIS_AddressAnchoredQuery_HitWithMatchingAddress verifies that when an
// address is provided and the 2GIS result's address_name overlaps it, the result
// is accepted and coordinates are returned.
func TestTwoGIS_AddressAnchoredQuery_HitWithMatchingAddress(t *testing.T) {
	resp := makeTwoGISResponse([]twoGISItem{
		{
			Name:        "Кофейня Зингеръ",
			AddressName: "Невский проспект, 28",
			Point:       &twoGISPoint{Lat: 59.9357, Lon: 30.3261},
		},
	})
	rt := &twoGISTransport{target: ""}
	srv := newTwoGISMockServer(t, resp, false)
	defer srv.Close()
	rt.target = srv.URL

	checker := &TwoGISChecker{
		apiKey: "demo",
		client: &http.Client{Transport: rt},
	}

	result, err := checker.Check(context.Background(), "Кафе Singer", "Санкт-Петербург", "Невский пр., 28")
	if err != nil {
		t.Fatalf("Check() error: %v", err)
	}
	if result.Status != PlaceOpen {
		t.Errorf("status = %q, want PlaceOpen", result.Status)
	}
	if result.OrgData == nil {
		t.Fatal("OrgData is nil")
	}
	if result.OrgData.Latitude != 59.9357 {
		t.Errorf("Latitude = %f, want 59.9357", result.OrgData.Latitude)
	}
	// Query must include the address tokens.
	if !strings.Contains(rt.lastQuery, "Невский") {
		t.Errorf("query %q does not contain address token 'Невский'", rt.lastQuery)
	}
}

// TestTwoGIS_AddressAnchoredQuery_RejectedOnMismatch is the regression guard for
// the Singer→Giovanni class: when the 2GIS result's address_name does NOT overlap
// the query address (totally different street), Check returns PlaceNotFound so
// CompositeChecker falls through to Yandex rather than using the wrong coordinates.
func TestTwoGIS_AddressAnchoredQuery_RejectedOnMismatch(t *testing.T) {
	// Simulates 2GIS returning "Giovanni medici" on "улица Чайковского, 83/7"
	// when the actual query was anchored on "Невский, 28".
	resp := makeTwoGISResponse([]twoGISItem{
		{
			Name:        "Giovanni medici, итальянский ресторан",
			AddressName: "улица Чайковского, 83/7",
			Point:       &twoGISPoint{Lat: 59.9440, Lon: 30.3600},
		},
	})
	srv := newTwoGISMockServer(t, resp, false)
	defer srv.Close()

	checker := &TwoGISChecker{
		apiKey: "demo",
		client: &http.Client{Transport: &twoGISTransport{target: srv.URL}},
	}

	result, err := checker.Check(context.Background(), "Кафе Singer", "Санкт-Петербург", "Невский пр., 28")
	if err != nil {
		t.Fatalf("Check() error: %v (must not error — PlaceNotFound is a healthy signal)", err)
	}
	if result.Status != PlaceNotFound {
		t.Errorf("status = %q, want PlaceNotFound (address mismatch must be rejected)", result.Status)
	}
}

// TestTwoGIS_EmptyAddress_NoValidation verifies that when no address is provided
// the legacy behaviour is preserved: items[0] is accepted without validation.
func TestTwoGIS_EmptyAddress_NoValidation(t *testing.T) {
	// Return a result with a completely different address — without an anchor
	// address, we cannot validate, so it is accepted as-is.
	resp := makeTwoGISResponse([]twoGISItem{
		{
			Name:        "Некое место",
			AddressName: "улица Неизвестная, 99",
			Point:       &twoGISPoint{Lat: 55.7558, Lon: 37.6176},
		},
	})
	srv := newTwoGISMockServer(t, resp, false)
	defer srv.Close()

	checker := &TwoGISChecker{
		apiKey: "demo",
		client: &http.Client{Transport: &twoGISTransport{target: srv.URL}},
	}

	result, err := checker.Check(context.Background(), "Некое место", "Москва", "")
	if err != nil {
		t.Fatalf("Check() error: %v", err)
	}
	if result.Status != PlaceOpen {
		t.Errorf("status = %q, want PlaceOpen (no address = no validation)", result.Status)
	}
}

// TestTwoGIS_AddressAbbreviationNormalised verifies that common Russian address
// abbreviations ("пр." / "проспект") are treated as the same street so a
// legitimate match is not rejected.
func TestTwoGIS_AddressAbbreviationNormalised(t *testing.T) {
	resp := makeTwoGISResponse([]twoGISItem{
		{
			Name:        "Кафе Зингеръ",
			AddressName: "Невский проспект, 28",
			Point:       &twoGISPoint{Lat: 59.9357, Lon: 30.3261},
		},
	})
	srv := newTwoGISMockServer(t, resp, false)
	defer srv.Close()

	checker := &TwoGISChecker{
		apiKey: "demo",
		client: &http.Client{Transport: &twoGISTransport{target: srv.URL}},
	}

	// Query uses abbreviated form "пр.", result uses full form "проспект" —
	// "Невский" is the shared street-name token after noise removal.
	result, err := checker.Check(context.Background(), "Кафе Зингеръ", "Санкт-Петербург", "Невский пр., 28")
	if err != nil {
		t.Fatalf("Check() error: %v", err)
	}
	if result.Status != PlaceOpen {
		t.Errorf("status = %q, want PlaceOpen (abbreviation variance must not reject)", result.Status)
	}
}

// --- addressTokensOverlap unit tests ---

func TestAddressTokensOverlap(t *testing.T) {
	tests := []struct {
		name   string
		query  string
		result string
		want   bool
	}{
		{
			name:   "same street abbreviated vs full",
			query:  "Невский пр., 28",
			result: "Невский проспект, 28",
			want:   true, // "невский" shared
		},
		{
			name:   "entirely different streets",
			query:  "Невский пр., 28",
			result: "улица Чайковского, 83/7",
			want:   false,
		},
		{
			name:   "shared city token should not count (city stripped by caller)",
			query:  "Невский пр., 28",
			result: "Большая Морская, 18",
			want:   false,
		},
		{
			name:   "abbreviation variants for переулок",
			query:  "Столярный пер., 3",
			result: "Столярный переулок, 3",
			want:   true,
		},
		{
			name:   "house number variance only — different streets",
			query:  "Лиговский пр., 28",
			result: "Лиговский пр., 14",
			want:   true, // street name "лиговский" matches; house differs but still same street
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := addressTokensOverlap(tt.query, tt.result)
			if got != tt.want {
				t.Errorf("addressTokensOverlap(%q, %q) = %v, want %v",
					tt.query, tt.result, got, tt.want)
			}
		})
	}
}

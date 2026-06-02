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
	lastQuery  string
	lastFields string
}

func (rt *twoGISTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.lastQuery = req.URL.Query().Get("q")
	rt.lastFields = req.URL.Query().Get("fields")
	newURL := rt.target + "?" + req.URL.RawQuery
	newReq, err := http.NewRequestWithContext(req.Context(), req.Method, newURL, req.Body)
	if err != nil {
		return nil, err
	}
	newReq.Header = req.Header
	return http.DefaultTransport.RoundTrip(newReq)
}

func newTwoGISMockServer(t *testing.T, resp twoGISResponse) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

// newTwoGISCheckerWithTransport builds a test TwoGISChecker wired to the mock server.
func newTwoGISCheckerWithTransport(srv *httptest.Server, rt *twoGISTransport, cfg TwoGISConfig) *TwoGISChecker {
	if cfg.APIKey == "" {
		cfg.APIKey = "demo"
	}
	return &TwoGISChecker{
		apiKey:            cfg.APIKey,
		client:            &http.Client{Transport: rt},
		onAddressRejected: cfg.OnAddressRejected,
	}
}

func TestTwoGIS_FieldsContainAddressName(t *testing.T) {
	resp := makeTwoGISResponse([]twoGISItem{
		{
			Name:        "Кофейня Центр",
			AddressName: "Невский пр., 1",
			Point:       &twoGISPoint{Lat: 59.9343, Lon: 30.3351},
		},
	})
	rt := &twoGISTransport{}
	srv := newTwoGISMockServer(t, resp)
	defer srv.Close()
	rt.target = srv.URL

	checker := newTwoGISCheckerWithTransport(srv, rt, TwoGISConfig{})
	_, err := checker.Check(context.Background(), "Кофейня Центр", "Санкт-Петербург", "Невский пр., 1")
	if err != nil {
		t.Fatalf("Check() error: %v", err)
	}
	// address_name must be in fields so validation has data to work with.
	if !strings.Contains(rt.lastFields, "items.address_name") {
		t.Errorf("fields %q does not include items.address_name (required for validation)", rt.lastFields)
	}
	if !strings.Contains(rt.lastFields, "items.point") {
		t.Errorf("fields %q does not include items.point", rt.lastFields)
	}
}

func TestTwoGIS_CoordsPopulated(t *testing.T) {
	resp := makeTwoGISResponse([]twoGISItem{
		{
			Name:        "Кофейня Центр",
			AddressName: "Невский пр., 1",
			Point:       &twoGISPoint{Lat: 59.9343, Lon: 30.3351},
		},
	})
	srv := newTwoGISMockServer(t, resp)
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
	srv := newTwoGISMockServer(t, resp)
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
// address is provided and the 2GIS result's address_name overlaps it on a
// distinctive token, the result is accepted and coordinates are returned.
func TestTwoGIS_AddressAnchoredQuery_HitWithMatchingAddress(t *testing.T) {
	resp := makeTwoGISResponse([]twoGISItem{
		{
			Name:        "Кофейня Зингеръ",
			AddressName: "Невский проспект, 28",
			Point:       &twoGISPoint{Lat: 59.9357, Lon: 30.3261},
		},
	})
	rt := &twoGISTransport{}
	srv := newTwoGISMockServer(t, resp)
	defer srv.Close()
	rt.target = srv.URL

	checker := newTwoGISCheckerWithTransport(srv, rt, TwoGISConfig{})

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
	resp := makeTwoGISResponse([]twoGISItem{
		{
			Name:        "Giovanni medici, итальянский ресторан",
			AddressName: "улица Чайковского, 83/7",
			Point:       &twoGISPoint{Lat: 59.9440, Lon: 30.3600},
		},
	})
	srv := newTwoGISMockServer(t, resp)
	defer srv.Close()

	var rejected atomic.Int32
	checker := &TwoGISChecker{
		apiKey:            "demo",
		client:            &http.Client{Transport: &twoGISTransport{target: srv.URL}},
		onAddressRejected: func() { rejected.Add(1) },
	}

	result, err := checker.Check(context.Background(), "Кафе Singer", "Санкт-Петербург", "Невский пр., 28")
	if err != nil {
		t.Fatalf("Check() error: %v (must not error — PlaceNotFound is a healthy signal)", err)
	}
	if result.Status != PlaceNotFound {
		t.Errorf("status = %q, want PlaceNotFound (address mismatch must be rejected)", result.Status)
	}
	if rejected.Load() != 1 {
		t.Errorf("OnAddressRejected fired %d times, want 1", rejected.Load())
	}
}

// TestTwoGIS_EmptyAddress_NoValidation verifies that when no address is provided
// the legacy behaviour is preserved: items[0] is accepted without validation.
func TestTwoGIS_EmptyAddress_NoValidation(t *testing.T) {
	resp := makeTwoGISResponse([]twoGISItem{
		{
			Name:        "Некое место",
			AddressName: "улица Неизвестная, 99",
			Point:       &twoGISPoint{Lat: 55.7558, Lon: 37.6176},
		},
	})
	srv := newTwoGISMockServer(t, resp)
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
	srv := newTwoGISMockServer(t, resp)
	defer srv.Close()

	checker := &TwoGISChecker{
		apiKey: "demo",
		client: &http.Client{Transport: &twoGISTransport{target: srv.URL}},
	}

	// "Невский" is the shared distinctive token after noise removal.
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
		city   string
		want   bool
	}{
		{
			name:   "same street abbreviated vs full",
			query:  "Невский пр., 28",
			result: "Невский проспект, 28",
			city:   "Санкт-Петербург",
			want:   true, // "невский" shared
		},
		{
			name:   "entirely different streets",
			query:  "Невский пр., 28",
			result: "улица Чайковского, 83/7",
			city:   "Санкт-Петербург",
			want:   false,
		},
		{
			name:   "Большая Морская vs Большая Конюшенная — generic adjective must not match",
			query:  "Большая Морская, 18",
			result: "Большая Конюшенная, 12",
			city:   "Санкт-Петербург",
			want:   false, // "большая" stripped; "морская" vs "конюшенная" — no overlap
		},
		{
			name:   "наб реки Мойки vs наб реки Фонтанки — generic geo-noun must not match",
			query:  "наб. реки Мойки, 12",
			result: "набережная реки Фонтанки, 90",
			city:   "Санкт-Петербург",
			want:   false, // "реки" stripped; "мойки" vs "фонтанки" — no overlap
		},
		{
			name:   "Тверская Москва vs Арбат Москва — city token must not match",
			query:  "Тверская, 5, Москва",
			result: "Арбат, 10, Москва",
			city:   "Москва",
			want:   false, // "москва" stripped; "тверская" vs "арбат" — no overlap
		},
		{
			name:   "Ленина 28а vs Мира 28а — house number with Cyrillic suffix must not match",
			query:  "Ленина, 28а",
			result: "Мира, 28а",
			city:   "",
			want:   false, // "28а" starts with digit → house number; "ленина" vs "мира" — no overlap
		},
		{
			name:   "Малая Садовая vs Малая Конюшенная — generic adjective must not match",
			query:  "Малая Садовая, 3",
			result: "Малая Конюшенная, 5",
			city:   "Санкт-Петербург",
			want:   false, // "малая" stripped; "садовая" vs "конюшенная" — no overlap
		},
		{
			name:   "ул Рубинштейна 5 vs улица Рубинштейна д 5 — legit formatting variant",
			query:  "ул. Рубинштейна, 5",
			result: "улица Рубинштейна, д. 5",
			city:   "Санкт-Петербург",
			want:   true, // "рубинштейна" shared
		},
		{
			name:   "Невский пр 28 vs проспект Невский 28 — reordering accepted",
			query:  "Невский пр., 28",
			result: "проспект Невский, 28",
			city:   "Санкт-Петербург",
			want:   true, // "невский" shared
		},
		{
			name:   "abbreviation variants for переулок",
			query:  "Столярный пер., 3",
			result: "Столярный переулок, 3",
			city:   "Санкт-Петербург",
			want:   true, // "столярный" shared
		},
		{
			name:   "house number variance — same street",
			query:  "Лиговский пр., 28",
			result: "Лиговский пр., 14",
			city:   "Санкт-Петербург",
			want:   true, // "лиговский" shared; house numbers differ but street matches
		},
		{
			name:   "city abbreviation спб stripped — different streets",
			query:  "Садовая, 5, спб",
			result: "Литейный пр., 5, спб",
			city:   "Санкт-Петербург",
			want:   false, // "спб" stripped as city synonym; "садовая" vs "литейный" — no overlap
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := addressTokensOverlap(tt.query, tt.result, tt.city)
			if got != tt.want {
				t.Errorf("addressTokensOverlap(%q, %q, city=%q) = %v, want %v",
					tt.query, tt.result, tt.city, got, tt.want)
			}
		})
	}
}

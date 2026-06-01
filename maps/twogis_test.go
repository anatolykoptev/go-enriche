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
}

func (rt *twoGISTransport) RoundTrip(req *http.Request) (*http.Response, error) {
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

	result, err := checker.Check(context.Background(), "Кофейня Центр", "Санкт-Петербург")
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

	result, err := checker.Check(context.Background(), "Без координат", "Москва")
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

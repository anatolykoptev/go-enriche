package maps

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// daDataResponse mirrors the DaData clean/address response shape for test fixtures.
type daDataTestItem struct {
	Result string `json:"result"`
	GeoLat string `json:"geo_lat"`
	GeoLon string `json:"geo_lon"`
	QcGeo  int    `json:"qc_geo"`
}

func makeDaDataServer(t *testing.T, status int, items []daDataTestItem, captureReq func(*http.Request, []byte)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if captureReq != nil {
			var body [4096]byte
			n, _ := r.Body.Read(body[:])
			captureReq(r, body[:n])
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if err := json.NewEncoder(w).Encode(items); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
}

// TestDaData_QcGeo0_ValidCoords: exact house-level result → PlaceOpen with coords.
func TestDaData_QcGeo0_ValidCoords(t *testing.T) {
	var gotReq *http.Request
	var gotBody []byte
	srv := makeDaDataServer(t, http.StatusOK, []daDataTestItem{
		{Result: "г Санкт-Петербург, Невский пр-кт, д 28", GeoLat: "59.934280", GeoLon: "30.328429", QcGeo: 0},
	}, func(r *http.Request, b []byte) {
		gotReq = r
		gotBody = b
	})
	defer srv.Close()

	daDataCleanURL = srv.URL

	c := NewDaData(DaDataConfig{APIKey: "testkey", Secret: "testsecret"})
	result, err := c.Check(context.Background(), "whatever", "Санкт-Петербург", "Невский пр-кт, д 28")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != PlaceOpen {
		t.Errorf("status = %q, want PlaceOpen", result.Status)
	}
	if result.OrgData == nil {
		t.Fatal("OrgData is nil")
	}
	if result.OrgData.Latitude == 0 || result.OrgData.Longitude == 0 {
		t.Errorf("coords = (%v, %v), want non-zero", result.OrgData.Latitude, result.OrgData.Longitude)
	}
	if result.OrgData.Address == "" {
		t.Errorf("OrgData.Address is empty, want normalised address")
	}

	// Assert outgoing headers carry auth credentials.
	if gotReq == nil {
		t.Fatal("server never received a request")
	}
	if got := gotReq.Header.Get("Authorization"); got != "Token testkey" {
		t.Errorf("Authorization = %q, want %q", got, "Token testkey")
	}
	if got := gotReq.Header.Get("X-Secret"); got != "testsecret" {
		t.Errorf("X-Secret = %q, want %q", got, "testsecret")
	}

	// Assert body is JSON array containing city + address.
	var sentAddrs []string
	if err := json.Unmarshal(gotBody, &sentAddrs); err != nil {
		t.Fatalf("body parse: %v (body=%s)", err, gotBody)
	}
	if len(sentAddrs) != 1 {
		t.Fatalf("body array len = %d, want 1", len(sentAddrs))
	}
	want := "Санкт-Петербург, Невский пр-кт, д 28"
	if sentAddrs[0] != want {
		t.Errorf("body[0] = %q, want %q", sentAddrs[0], want)
	}
}

// TestDaData_QcGeo1_Accepted: nearest-house precision → accepted (PlaceOpen).
func TestDaData_QcGeo1_Accepted(t *testing.T) {
	srv := makeDaDataServer(t, http.StatusOK, []daDataTestItem{
		{Result: "г Санкт-Петербург, Невский пр-кт, д 30", GeoLat: "59.934000", GeoLon: "30.328000", QcGeo: 1},
	}, nil)
	defer srv.Close()
	daDataCleanURL = srv.URL

	c := NewDaData(DaDataConfig{APIKey: "k", Secret: "s"})
	result, err := c.Check(context.Background(), "", "Санкт-Петербург", "Невский пр-кт, д 30")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != PlaceOpen {
		t.Errorf("status = %q, want PlaceOpen (qc_geo=1 must be accepted)", result.Status)
	}
}

// TestDaData_QcGeo2_Street_Rejected: street-level precision → PlaceNotFound.
func TestDaData_QcGeo2_Street_Rejected(t *testing.T) {
	srv := makeDaDataServer(t, http.StatusOK, []daDataTestItem{
		{Result: "г Санкт-Петербург, Невский пр-кт", GeoLat: "59.934100", GeoLon: "30.328100", QcGeo: 2},
	}, nil)
	defer srv.Close()
	daDataCleanURL = srv.URL

	c := NewDaData(DaDataConfig{APIKey: "k", Secret: "s"})
	result, err := c.Check(context.Background(), "", "Санкт-Петербург", "Невский пр-кт")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != PlaceNotFound {
		t.Errorf("status = %q, want PlaceNotFound (qc_geo=2 street is too coarse)", result.Status)
	}
}

// TestDaData_QcGeo5_Undefined_Rejected: undefined precision → PlaceNotFound.
func TestDaData_QcGeo5_Undefined_Rejected(t *testing.T) {
	srv := makeDaDataServer(t, http.StatusOK, []daDataTestItem{
		{Result: "", GeoLat: "", GeoLon: "", QcGeo: 5},
	}, nil)
	defer srv.Close()
	daDataCleanURL = srv.URL

	c := NewDaData(DaDataConfig{APIKey: "k", Secret: "s"})
	result, err := c.Check(context.Background(), "", "Москва", "ул Тверская")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != PlaceNotFound {
		t.Errorf("status = %q, want PlaceNotFound (qc_geo=5 undefined must be rejected)", result.Status)
	}
}

// TestDaData_EmptyAddress_NoHTTPCall: address-gated — no address → PlaceNotFound, server never called.
func TestDaData_EmptyAddress_NoHTTPCall(t *testing.T) {
	serverCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverCalled = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()
	daDataCleanURL = srv.URL

	c := NewDaData(DaDataConfig{APIKey: "k", Secret: "s"})
	result, err := c.Check(context.Background(), "SomeName", "Москва", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != PlaceNotFound {
		t.Errorf("status = %q, want PlaceNotFound", result.Status)
	}
	if serverCalled {
		t.Error("server was called despite empty address — must be address-gated")
	}
}

// TestDaData_EmptyCredentials_NoHTTPCall: credential-gated — no key → PlaceNotFound, server never called.
func TestDaData_EmptyCredentials_NoHTTPCall(t *testing.T) {
	serverCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverCalled = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()
	daDataCleanURL = srv.URL

	c := NewDaData(DaDataConfig{}) // no APIKey, no Secret
	result, err := c.Check(context.Background(), "SomeName", "Москва", "ул Тверская, д 1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != PlaceNotFound {
		t.Errorf("status = %q, want PlaceNotFound", result.Status)
	}
	if serverCalled {
		t.Error("server was called despite empty credentials — must be credential-gated")
	}
}

// TestDaData_HTTP403_ReturnsError: bad credentials → error, not PlaceNotFound.
func TestDaData_HTTP403_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()
	daDataCleanURL = srv.URL

	c := NewDaData(DaDataConfig{APIKey: "badkey", Secret: "badsecret"})
	result, err := c.Check(context.Background(), "", "Москва", "ул Тверская, д 1")
	if err == nil {
		t.Fatalf("expected error for HTTP 403, got nil (result=%v)", result)
	}
}

// TestDaData_EmptyArray_PlaceNotFound: 200 OK with empty array → PlaceNotFound.
func TestDaData_EmptyArray_PlaceNotFound(t *testing.T) {
	srv := makeDaDataServer(t, http.StatusOK, []daDataTestItem{}, nil)
	defer srv.Close()
	daDataCleanURL = srv.URL

	c := NewDaData(DaDataConfig{APIKey: "k", Secret: "s"})
	result, err := c.Check(context.Background(), "", "Москва", "ул Тверская, д 1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != PlaceNotFound {
		t.Errorf("status = %q, want PlaceNotFound", result.Status)
	}
}

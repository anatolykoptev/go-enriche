package maps

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGeocode_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("categories") != "map" {
			t.Errorf("categories = %q, want map", q.Get("categories"))
		}
		resp := geoResponse{
			Results: []geoResult{
				{Latitude: 59.9343, Longitude: 30.3351},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	g, err := NewGeocoder(srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	lat, lon, ok := g.Geocode(context.Background(), "Невский пр. 1", "Санкт-Петербург")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if lat != 59.9343 {
		t.Errorf("lat = %f, want 59.9343", lat)
	}
	if lon != 30.3351 {
		t.Errorf("lon = %f, want 30.3351", lon)
	}
}

func TestGeocode_NoResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := geoResponse{}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	g, _ := NewGeocoder(srv.URL)
	_, _, ok := g.Geocode(context.Background(), "Несуществующий адрес", "Нигде")
	if ok {
		t.Error("expected ok=false for empty results")
	}
}

func TestGeocode_ZeroCoords(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := geoResponse{
			Results: []geoResult{{Latitude: 0, Longitude: 0}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	g, _ := NewGeocoder(srv.URL)
	_, _, ok := g.Geocode(context.Background(), "Test", "Test")
	if ok {
		t.Error("expected ok=false for zero coords")
	}
}

func TestGeocode_EmptyURL(t *testing.T) {
	_, err := NewGeocoder("")
	if err == nil {
		t.Error("expected error for empty URL")
	}
}

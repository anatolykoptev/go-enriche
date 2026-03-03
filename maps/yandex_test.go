package maps

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestYandexMaps_PermanentClosed(t *testing.T) {
	env := newTestEnv(t, `<html>{"status":"permanent-closed","name":"Test"}</html>`)

	r, err := env.checker.Check(context.Background(), "Test Place", "Москва")
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != PlacePermanentClosed {
		t.Errorf("got %s, want %s", r.Status, PlacePermanentClosed)
	}
	if !r.IsClosed() {
		t.Error("IsClosed() should be true")
	}
}

func TestYandexMaps_TemporaryClosed(t *testing.T) {
	env := newTestEnv(t, `<html>{"status":"temporary-closed","name":"Test"}</html>`)

	r, err := env.checker.Check(context.Background(), "Test Cafe", "СПб")
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != PlaceTemporaryClosed {
		t.Errorf("got %s, want %s", r.Status, PlaceTemporaryClosed)
	}
}

func TestYandexMaps_Open(t *testing.T) {
	env := newTestEnv(t, `<html>{"status":"open","name":"Good"}</html>`)

	r, err := env.checker.Check(context.Background(), "Good Place", "СПб")
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != PlaceOpen {
		t.Errorf("got %s, want %s", r.Status, PlaceOpen)
	}
}

func TestYandexMaps_NotFound(t *testing.T) {
	searxng := newMockSearXNG(t, nil)
	defer searxng.Close()

	ym, _ := NewYandexMaps(searxng.URL)
	r, err := ym.Check(context.Background(), "Nonexistent", "Нигде")
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != PlaceNotFound {
		t.Errorf("got %s, want %s", r.Status, PlaceNotFound)
	}
}

func TestParseOrgStatus(t *testing.T) {
	tests := []struct {
		name string
		html string
		want string
	}{
		{"permanent", `"status":"permanent-closed"`, "permanent-closed"},
		{"temporary", `"status":"temporary-closed"`, "temporary-closed"},
		{"open", `"status":"open"`, "open"},
		{"closed_hours", `"status":"closed"`, ""},
		{"empty", `no status here`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseOrgStatus([]byte(tt.html))
			if got != tt.want {
				t.Errorf("parseOrgStatus(%q) = %q, want %q", tt.html, got, tt.want)
			}
		})
	}
}

func TestIsYandexMapsOrgURL(t *testing.T) {
	if !isYandexMapsOrgURL("https://yandex.ru/maps/org/test/123/") {
		t.Error("should match yandex.ru")
	}
	if !isYandexMapsOrgURL("https://yandex.com/maps/org/test/123/") {
		t.Error("should match yandex.com")
	}
	if isYandexMapsOrgURL("https://google.com/maps/place/123/") {
		t.Error("should not match google")
	}
}

// --- test helpers ---

type testEnv struct {
	checker *YandexMaps
	searxng *httptest.Server
	org     *httptest.Server
}

// newTestEnv creates a mock SearXNG that returns a fake yandex.ru/maps/org URL,
// and a mock org page server. A custom HTTP client redirects yandex.ru requests
// to the local org server.
func newTestEnv(t *testing.T, orgHTML string) *testEnv {
	t.Helper()

	org := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(orgHTML))
	}))
	t.Cleanup(org.Close)

	// SearXNG returns a proper yandex.ru URL.
	searxng := newMockSearXNG(t, []mockResult{{
		URL:   "https://yandex.ru/maps/org/test_place/12345/",
		Title: "Test Place",
	}})
	t.Cleanup(searxng.Close)

	// Custom transport redirects yandex.ru → local org server.
	transport := &rewriteTransport{
		target:    org.URL,
		transport: http.DefaultTransport,
	}
	client := &http.Client{Transport: transport}

	ym, err := NewYandexMaps(searxng.URL, WithYandexHTTPClient(client))
	if err != nil {
		t.Fatal(err)
	}

	return &testEnv{checker: ym, searxng: searxng, org: org}
}

// rewriteTransport redirects yandex.ru requests to a local test server.
type rewriteTransport struct {
	target    string
	transport http.RoundTripper
}

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.Contains(req.URL.Host, "yandex.ru") || strings.Contains(req.URL.Host, "yandex.com") {
		newURL := rt.target + req.URL.Path
		newReq, _ := http.NewRequestWithContext(req.Context(), req.Method, newURL, req.Body)
		newReq.Header = req.Header
		return rt.transport.RoundTrip(newReq)
	}
	return rt.transport.RoundTrip(req)
}

type mockResult struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

func newMockSearXNG(t *testing.T, results []mockResult) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := struct {
			Results []mockResult `json:"results"`
		}{Results: results}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

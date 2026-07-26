package maps

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	stealth "github.com/anatolykoptev/go-stealth"
)

// TestYandexMaps_UserAgentMatchesTLSProfile is the invariant this refactor
// exists to establish: the User-Agent sent by fetchOrgStatus must have the
// same Chrome major as the TLS profile the UA is derived from
// (stealth.ProfileChrome131 — the library default). A hand-written UA literal
// can drift from the profile (a Chrome/120 UA over a Chrome_131 JA3 is a
// stronger bot signal than a merely outdated one); deriving the UA from
// stealth.UserAgentForProfile makes the two agree by contract.
func TestYandexMaps_UserAgentMatchesTLSProfile(t *testing.T) {
	t.Parallel()

	var gotUA string
	org := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`<html>{"status":"open","name":"Test"}</html>`))
	}))
	t.Cleanup(org.Close)

	searxng := newMockSearXNG(t, []mockResult{{
		URL:   "https://yandex.ru/maps/org/test_place/12345/",
		Title: "Test Place",
	}})
	t.Cleanup(searxng.Close)

	transport := &rewriteTransport{target: org.URL, transport: http.DefaultTransport}
	ym, err := NewYandexMaps(searxng.URL, WithYandexHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := ym.Check(context.Background(), "Test Place", "Москва", ""); err != nil {
		t.Fatal(err)
	}

	wantUA := stealth.UserAgentForProfile(stealth.ProfileChrome131)
	gotMajor := chromeMajor(t, gotUA)
	wantMajor := chromeMajor(t, wantUA)
	if gotMajor != wantMajor {
		t.Errorf("UA Chrome major = %q, want %q (must match ProfileChrome131; UA=%q want=%q)",
			gotMajor, wantMajor, gotUA, wantUA)
	}
	if gotUA != wantUA {
		t.Errorf("User-Agent = %q, want %q (must equal stealth.UserAgentForProfile(ProfileChrome131), not a hardcoded literal)",
			gotUA, wantUA)
	}
}

// chromeMajor extracts the integer Chrome major version from a User-Agent
// string (e.g. "Chrome/131.0.0.0" → "131"). Fatals if no Chrome token is
// found — a UA without one is itself a defect this test should surface.
func chromeMajor(t *testing.T, ua string) string {
	t.Helper()
	idx := strings.Index(ua, "Chrome/")
	if idx < 0 {
		t.Fatalf("no Chrome/ token in UA %q", ua)
	}
	rest := ua[idx+len("Chrome/"):]
	end := strings.IndexByte(rest, '.')
	if end < 0 {
		t.Fatalf("no major-version delimiter in UA %q", ua)
	}
	major := rest[:end]
	if _, err := strconv.Atoi(major); err != nil {
		t.Fatalf("non-numeric Chrome major %q in UA %q", major, ua)
	}
	return major
}

package extract

import (
	"os"
	"path/filepath"
	"testing"
)

// Golden regression set (Fitness #1) — the load-bearing guard against the
// wrong-fact class returning. Each case is a saved real or representative HTML
// fixture with the operator-verified ground truth.
//
// Provenance per fixture:
//   - igora-drive.html      REAL (live drive-igora.ru capture 2026-06-17)
//   - royal-wedding.html    REAL (live royal-wedding.ru capture 2026-06-17) —
//     multi-city: Moscow HQ tel:/og:, SPb branch only in microdata.
//   - vzaimno.html          REPRESENTATIVE (domain unresolved at capture)
//   - easyweddingday.html   REPRESENTATIVE (live site 200 but price JS-rendered)
//   - no-site.html          REPRESENTATIVE (VK/2GIS-only, no own domain)
//   - embedded-widget.html  REPRESENTATIVE (booking-iframe + widget tel: vector)
//
// The `city` column drives the local-area-code tiebreaker (operator Decision 2,
// 2026-06-17): among ALL official-site phone candidates (tel: href + microdata
// + JSON-LD telephone + og:), prefer the one whose area code matches the
// article's target city; fall back to tel:-wins ordering only when no candidate
// matches the city's area code.
func TestGoldenRegression_ExtractFacts(t *testing.T) {
	t.Parallel()

	const spb = "Санкт-Петербург"

	cases := []struct {
		name         string
		fixture      string
		city         string // article target city (drives local-area-code rule)
		wantPhone    string // exact phone ExtractFactsForCity must return ("" = expect nil)
		wantNotPhone string // phone it must NOT return (the known-wrong value)
		wantAddr     string // substring the returned address must contain ("" = skip)
		wantPrice    string // substring the returned price must contain ("" = skip)
	}{
		{
			// REAL capture: venue's only site phone is the SPb 812 tel: href in
			// header + footer; no 8-800, no 793-86-16 (that is a maps artifact
			// absent from the site). 812 is already local, so tel:-wins and the
			// area-code rule agree; this guards against a regression.
			name:         "igora_drive_spb_tel_812_no_regression",
			fixture:      "igora-drive.html",
			city:         spb,
			wantPhone:    "+7 (812) 615 70 00",
			wantNotPhone: "+7 (800) 555-35-35",
		},
		{
			// REAL capture, the headline Decision-2 case. Multi-city venue: the
			// tel: href + og: are the MOSCOW 925 line; the SPb branch number
			// 956-18-40 (812) exists ONLY as one microdata itemprop=telephone.
			// For an SPb guide the 812 microdata candidate must beat the 925
			// tel: href. Plain tel:-wins returns the WRONG 925.
			name:         "royal_wedding_spb_local_area_code_picks_812_microdata",
			fixture:      "royal-wedding.html",
			city:         spb,
			wantPhone:    "+7 (812) 956-18-40",
			wantNotPhone: "+7-925-580-81-18",
		},
		{
			// Same fixture, NO city hint, so the area-code rule cannot fire and
			// the resolver falls back to tel:-wins: the Moscow 925 tel: href.
			// This is the documented Phase-1 fallback (do NOT fabricate the SPb
			// number when there is no city signal).
			name:         "royal_wedding_no_city_falls_back_to_tel_wins_925",
			fixture:      "royal-wedding.html",
			city:         "",
			wantPhone:    "+7-925-580-81-18",
			wantNotPhone: "+7 (812) 956-18-40",
		},
		{
			// Sole site candidate is an 812 tel: href; with an SPb city it is
			// trivially the local match.
			name:      "vzaimno_site_address_litejnyj_12",
			fixture:   "vzaimno.html",
			city:      spb,
			wantPhone: "+7 (812) 244-55-66",
			wantAddr:  "Литейный",
		},
		{
			name:      "easyweddingday_site_price_120k",
			fixture:   "easyweddingday.html",
			city:      spb,
			wantPhone: "+7 (812) 123-45-67",
			wantPrice: "120",
		},
		{
			name:      "no_site_no_verified_phone",
			fixture:   "no-site.html",
			city:      spb,
			wantPhone: "", // no tel:/JSON-LD/regex phone on a VK-only placeholder
		},
		{
			name:      "embedded_widget_own_contacts_tel_wins",
			fixture:   "embedded-widget.html",
			city:      spb,
			wantPhone: "+7 (812) 333-44-55",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			html := readFixture(t, tc.fixture)
			facts := ExtractFactsForCity(html, "https://example.com", tc.city)

			if tc.wantPhone == "" {
				if facts.Phone != nil {
					t.Errorf("phone: want nil, got %q", *facts.Phone)
				}
			} else {
				if facts.Phone == nil {
					t.Fatalf("phone: want %q, got nil", tc.wantPhone)
				}
				if *facts.Phone != tc.wantPhone {
					t.Errorf("phone: want %q, got %q", tc.wantPhone, *facts.Phone)
				}
			}
			if tc.wantNotPhone != "" && facts.Phone != nil && *facts.Phone == tc.wantNotPhone {
				t.Errorf("phone: must NOT be the known-wrong %q", tc.wantNotPhone)
			}

			if tc.wantAddr != "" {
				if facts.Address == nil {
					t.Fatalf("address: want substring %q, got nil", tc.wantAddr)
				}
				if !contains(*facts.Address, tc.wantAddr) {
					t.Errorf("address: want substring %q, got %q", tc.wantAddr, *facts.Address)
				}
			}

			if tc.wantPrice != "" {
				if facts.Price == nil {
					t.Fatalf("price: want substring %q, got nil", tc.wantPrice)
				}
				if !contains(*facts.Price, tc.wantPrice) {
					t.Errorf("price: want substring %q, got %q", tc.wantPrice, *facts.Price)
				}
			}
		})
	}
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "golden", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(b)
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

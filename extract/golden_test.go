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
//   - igora-drive.html      REAL (live drive-igora.ru capture 2026-06-16)
//   - easyweddingday.html   REPRESENTATIVE (live site 200 but price JS-rendered)
//   - royal-wedding.html    REPRESENTATIVE (site 443-refused at capture)
//   - vzaimno.html          REPRESENTATIVE (domain unresolved at capture)
//   - no-site.html          REPRESENTATIVE (VK/2GIS-only, no own domain)
//   - embedded-widget.html  REPRESENTATIVE (booking-iframe + widget tel: vector)
//
// Assertion: site tel: beats maps/widget, aggregator price never silently
// wins, no-site venue yields no site-verified phone.
func TestGoldenRegression_ExtractFacts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		fixture      string
		wantPhone    string // exact phone ExtractFacts must return ("" = expect nil)
		wantNotPhone string // phone ExtractFacts must NOT return (the known-wrong value)
		wantAddr     string // substring the returned address must contain ("" = skip)
		wantPrice    string // substring the returned price must contain ("" = skip)
	}{
		{
			// Fixture carries a WRONG JSON-LD telephone (8-800 tracking number)
			// and a demoted calltouch widget tel:; the correct number exists
			// ONLY as the contacts-region tel: href. The old cascade returned
			// the 8-800 — this case fails unless applyContactOverride wins.
			name:         "igora_drive_contacts_tel_wins_over_jsonld_and_widget_8800",
			fixture:      "igora-drive.html",
			wantPhone:    "+7 (812) 615 70 00",
			wantNotPhone: "+7 (800) 555-35-35",
		},
		{
			// Representative fixture (site 443-refused at capture). Tests the
			// structural rule address-block tel: > body tel: > widget tel:; the
			// specific numbers are illustrative, not operator-verified ground
			// truth.
			name:         "royal_wedding_contacts_tel_beats_body_and_widget",
			fixture:      "royal-wedding.html",
			wantPhone:    "+7 (812) 956-18-40",
			wantNotPhone: "+7 (812) 704-85-45",
		},
		{
			name:      "vzaimno_site_address_litejnyj_12",
			fixture:   "vzaimno.html",
			wantPhone: "+7 (812) 244-55-66",
			wantAddr:  "Литейный",
		},
		{
			name:      "easyweddingday_site_price_120k",
			fixture:   "easyweddingday.html",
			wantPhone: "+7 (812) 123-45-67",
			wantPrice: "120",
		},
		{
			name:      "no_site_no_verified_phone",
			fixture:   "no-site.html",
			wantPhone: "", // no tel:/JSON-LD/regex phone on a VK-only placeholder
		},
		{
			name:      "embedded_widget_own_contacts_tel_wins",
			fixture:   "embedded-widget.html",
			wantPhone: "+7 (812) 333-44-55",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			html := readFixture(t, tc.fixture)
			facts := ExtractFacts(html, "https://example.com")

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

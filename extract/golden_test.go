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
//   - royal-wed.html         REAL (live royal-wed.ru static capture 2026-06-17) —
//     the venue's REAL SPb official site (article 56564 CTA target). Runs
//     Roistat DNI: the visible (812) tel: ROTATES (956-18-40 / 439-18-55 /
//     704-85-45), so the only stable phone is the hard-coded WhatsApp social
//     link +7 (921) 956-18-40. The Moscow sibling royal-wedding.ru was DROPPED
//     (different city entity: t.me/royalweddingmsk, +7 925 ...).
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
			// REAL capture, the headline DNI case. royal-wed.ru runs Roistat
			// dynamic-number-insertion: the static (812) 956-18-40 in the tel:
			// href + 8x itemprop=telephone + og: meta is a ROTATING proxy slot
			// (operator witnessed 956-18-40 / 439-18-55 / 704-85-45). The only
			// stable, owned, DNI-immune phone is the hard-coded WhatsApp social
			// link +7 (921) 956-18-40 (the value that shipped to the live
			// article). The resolver MUST return the social-link number and
			// MUST NOT assert any rotating (812) proxy as the venue's phone.
			name:         "royal_wed_spb_social_link_beats_dni_rotating_812",
			fixture:      "royal-wed.html",
			city:         spb,
			wantPhone:    "+79219561840",
			wantNotPhone: "+7 (812) 956-18-40",
		},
		{
			// Same fixture, NO city hint: the social-link phone is still chosen,
			// so the rotating (812) is never asserted regardless of city signal.
			// This fixture carries a DNI vendor script (Roistat), so the win
			// comes from resolvePhoneForCityDNI's DNI branch (socialLinkIndex +
			// dniTrustworthy), which picks the social-link candidate
			// unconditionally — it is the only DNI-immune source (see
			// pickPhoneCandidate's doc comment, issue #55). On a CLEAN page
			// (no DNI vendor) a labeled contacts-region tel: would instead win
			// over a social-link number — see
			// TestResolvePhoneForCity_SocialLinkDoesNotBeatLabeledTel.
			name:         "royal_wed_no_city_social_link_still_wins",
			fixture:      "royal-wed.html",
			city:         "",
			wantPhone:    "+79219561840",
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

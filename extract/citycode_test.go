package extract

import "testing"

func TestExpectedAreaCodes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		city string
		want []int
	}{
		{"Санкт-Петербург", []int{812}},
		{"г. Санкт-Петербург", []int{812}},
		{"  спб ", []int{812}},
		{"SPb", []int{812}},
		{"Saint Petersburg", []int{812}},
		{"Москва", []int{495, 499}},
		{"", nil},
		{"Воронеж", nil}, // unknown city -> no tiebreaker
	}
	for _, tc := range cases {
		got := expectedAreaCodes(tc.city)
		if len(got) != len(tc.want) {
			t.Errorf("expectedAreaCodes(%q) = %v, want %v", tc.city, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("expectedAreaCodes(%q) = %v, want %v", tc.city, got, tc.want)
				break
			}
		}
	}
}

func TestPhoneAreaCode(t *testing.T) {
	t.Parallel()
	cases := []struct {
		phone string
		want  int
	}{
		{"+7 (812) 956-18-40", 812},
		{"+7-925-580-81-18", 925},
		{"8 (800) 555-35-35", 800},
		{"8(495)1234567", 495},
		{"not a phone", -1},
		{"+7 812 12", -1}, // too short
	}
	for _, tc := range cases {
		if got := phoneAreaCode(tc.phone); got != tc.want {
			t.Errorf("phoneAreaCode(%q) = %d, want %d", tc.phone, got, tc.want)
		}
	}
}

func TestMatchesCity(t *testing.T) {
	t.Parallel()
	spb := expectedAreaCodes("Санкт-Петербург")
	if !matchesCity("+7 (812) 956-18-40", spb) {
		t.Error("812 should match SPb")
	}
	if matchesCity("+7-925-580-81-18", spb) {
		t.Error("925 must not match SPb")
	}
	if matchesCity("+7 (812) 956-18-40", nil) {
		t.Error("nil expected set must never match")
	}
}

// The headline multi-candidate case in isolation: an SPb article over a venue
// whose tel: href + og: are Moscow 925 and whose only 812 number is a microdata
// telephone. The 812 microdata must win for the SPb city.
func TestResolvePhoneForCity_MultiCityPicksLocalMicrodata(t *testing.T) {
	t.Parallel()
	html := `<html><body>
	<header class="header"><a href="tel:+79255808118">+7-925-580-81-18</a></header>
	<div itemscope><meta itemprop="telephone" content="+7-925-580-81-18"><meta itemprop="address" content="г. Москва"></div>
	<div itemscope><meta itemprop="telephone" content="+7 (812) 956-18-40"><meta itemprop="address" content="г. Санкт-Петербург"></div>
	</body></html>`
	doc, err := documentFromHTML(html)
	if err != nil || doc == nil {
		t.Fatalf("doc parse: %v", err)
	}
	phone, region, ok := resolvePhoneForCity(doc, "Санкт-Петербург")
	if !ok {
		t.Fatal("want a resolved phone")
	}
	if phone != "+7 (812) 956-18-40" {
		t.Errorf("want SPb microdata 956-18-40, got %q (region %s)", phone, region)
	}
}

// No city hint -> fall back to source-order (the contacts-region tel: wins),
// proving the resolver does not fabricate an SPb number when the article gives
// no city signal.
func TestResolvePhoneForCity_NoCityFallsBackToTier(t *testing.T) {
	t.Parallel()
	html := `<html><body>
	<header class="header"><a href="tel:+79255808118">+7-925-580-81-18</a></header>
	<div itemscope><meta itemprop="telephone" content="+7 (812) 956-18-40"></div>
	</body></html>`
	doc, _ := documentFromHTML(html)
	phone, _, ok := resolvePhoneForCity(doc, "")
	if !ok || phone != "+7-925-580-81-18" {
		t.Errorf("no city -> contacts-region tel: must win; got %q ok=%v", phone, ok)
	}
}

// A city whose only local candidate is a JSON-LD/og: phone (seeded via prior /
// og: meta), not a tel: href, still resolves to the local number.
func TestResolvePhoneForCity_LocalOnlyInOgMeta(t *testing.T) {
	t.Parallel()
	html := `<html><head>
	<meta property="business:contact_data:phone_number" content="+7 (812) 244-55-66"/>
	</head><body>
	<header class="header"><a href="tel:+79991112233">+7 (999) 111-22-33</a></header>
	</body></html>`
	doc, _ := documentFromHTML(html)
	phone, _, ok := resolvePhoneForCity(doc, "Санкт-Петербург")
	if !ok || phone != "+7 (812) 244-55-66" {
		t.Errorf("local og: phone must win for SPb; got %q ok=%v", phone, ok)
	}
}

// Regression guard (reviewer MAJOR, PR #13 round 3): on the no-city / no-local
// fallback path a human-facing body tel: must outrank a microdata telephone —
// the venue's own tel: is the top phone authority, and an empty/unknown city
// must NOT reorder that. Locks the fix for the tier-order regression.
func TestResolvePhoneForCity_NoCityBodyTelBeatsMicrodata(t *testing.T) {
	t.Parallel()
	html := `<html><body>
	<p>звоните <a href="tel:+78121112233">+7 (812) 111-22-33</a></p>
	<span itemprop="telephone">+7 (812) 999-88-77</span>
	</body></html>`
	doc, _ := documentFromHTML(html)
	phone, region, ok := resolvePhoneForCity(doc, "")
	if !ok {
		t.Fatal("want a resolved phone")
	}
	if phone != "+7 (812) 111-22-33" {
		t.Errorf("no-city: body tel: must beat microdata; want +7 (812) 111-22-33, got %q (region %s)", phone, region)
	}
}

// 8-800 toll-free / call-tracking numbers must be demoted: a bare body 8-800
// tel: must not beat a real local microdata number, and must not win over a
// real body tel:. Guards the reviewer's 8-800-fallback finding.
func TestResolvePhoneForCity_TollFreeDemoted(t *testing.T) {
	t.Parallel()
	// Body 8-800 tel: (tierBody) vs a NON-local (Moscow 495) microdata number
	// (tierMicrodata). With the shipped body>microdata order the only reason
	// the 495 microdata wins for city="" is that the 8-800 is demoted to
	// tierDemoted — so this isolates the demotion as the sole cause and stays
	// robust if the body/microdata tiers are ever reordered.
	html := `<html><body>
	<p><a href="tel:+78005553535">8 (800) 555-35-35</a></p>
	<span itemprop="telephone">+7 (495) 999-88-77</span>
	</body></html>`
	doc, _ := documentFromHTML(html)
	for _, city := range []string{"Москва", ""} {
		phone, _, ok := resolvePhoneForCity(doc, city)
		if !ok || phone != "+7 (495) 999-88-77" {
			t.Errorf("city=%q: 8-800 must be demoted below the real number; got %q ok=%v", city, phone, ok)
		}
	}
}

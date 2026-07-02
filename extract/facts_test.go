package extract

import "testing"

func TestExtractFacts_JSONLD(t *testing.T) {
	t.Parallel()
	html := `<html><head>
	<script type="application/ld+json">
	{
		"@context": "https://schema.org",
		"@type": "Restaurant",
		"name": "Test Cafe",
		"telephone": "+7 (812) 111-22-33",
		"address": {"@type": "PostalAddress", "streetAddress": "ул. Пушкина, 10"}
	}
	</script>
	</head><body></body></html>`

	facts := ExtractFacts(html, "https://example.com")
	assertFactPtr(t, "PlaceName", facts.PlaceName, "Test Cafe")
	assertFactPtr(t, "Phone", facts.Phone, "+7 (812) 111-22-33")
	if facts.Address == nil || *facts.Address == "" {
		t.Error("expected non-empty address")
	}
}

func TestExtractFacts_RegexFallback(t *testing.T) {
	t.Parallel()
	html := `<html><body>
	<p>Адрес: Литейный проспект, 55</p>
	<p>Телефон: +7 (812) 999-88-77</p>
	<p>Стоимость: от 200 рублей</p>
	</body></html>`

	facts := ExtractFacts(html, "https://example.com")
	if facts.Address == nil {
		t.Error("expected address from regex")
	}
	if facts.Phone == nil {
		t.Error("expected phone from regex")
	}
	if facts.Price == nil {
		t.Error("expected price from regex")
	}
}

// TestExtractFacts_RegexFallbackSkipsBoilerplateJunk guards the Novoclinic bug
// (novoclinicspb.ru, REAL capture 2026-07-02): applyRegexFallback used to run
// regexPhone over RAW HTML, so a CSS letter-spacing decimal inside a <style>
// block ("letter-spacing: 0.06153846153846154em") read as the digit run
// 84615384615 — which ValidatePhone accepts (area code 461 falls in the
// Rossvyaz 401-499 landline range) — and won the page's ONLY phone slot
// (structured data, tel: hrefs and microdata are all absent on this fixture,
// so the venue's real phone is reachable only via the Layer-2 regex pass).
// The fallback must scope to boilerplate-stripped text (style/script/...
// removed — see stripBoilerplate in goquery.go) so the CSS junk never
// surfaces, while the real visible phone is still recovered.
func TestExtractFacts_RegexFallbackSkipsBoilerplateJunk(t *testing.T) {
	t.Parallel()
	html := readFixture(t, "novoclinic.html")

	facts := ExtractFacts(html, "https://novoclinicspb.ru")

	if facts.Phone == nil {
		t.Fatal("expected the real site phone to be recovered, got nil")
	}
	if *facts.Phone == "84615384615" {
		t.Errorf("CSS-decimal junk phone leaked through: %q", *facts.Phone)
	}
	if !contains(*facts.Phone, "331-52-55") {
		t.Errorf("expected the real site phone (+7 (921) 331-52-55), got %q", *facts.Phone)
	}
}

func TestExtractFacts_JSONLDPriority(t *testing.T) {
	t.Parallel()
	html := `<html><head>
	<script type="application/ld+json">
	{"@context":"https://schema.org","@type":"Place","telephone":"+7-812-222-33-44"}
	</script>
	</head><body>
	<p>Телефон: +7 (921) 888-77-66</p>
	</body></html>`

	facts := ExtractFacts(html, "https://example.com")
	assertFactPtr(t, "Phone", facts.Phone, "+7-812-222-33-44")
}

func TestExtractFacts_EmptyHTML(t *testing.T) {
	t.Parallel()
	facts := ExtractFacts("", "https://example.com")
	if facts.PlaceName != nil || facts.Phone != nil || facts.Address != nil {
		t.Error("expected all nil facts for empty HTML")
	}
}

func TestExtractFacts_EventDate(t *testing.T) {
	t.Parallel()
	html := `<html><head>
	<script type="application/ld+json">
	{"@context":"https://schema.org","@type":"Event","name":"Concert","startDate":"2026-04-01"}
	</script>
	</head><body></body></html>`

	facts := ExtractFacts(html, "https://example.com")
	assertFactPtr(t, "EventDate", facts.EventDate, "2026-04-01")
}

func TestExtractFacts_RejectsGarbagePhone(t *testing.T) {
	t.Parallel()
	html := `<html><head>
	<script type="application/ld+json">
	{"@context":"https://schema.org","@type":"Restaurant","telephone":"81063196745"}
	</script>
	</head><body></body></html>`
	facts := ExtractFacts(html, "https://example.com")
	if facts.Phone != nil {
		t.Errorf("expected nil phone for garbage number, got %q", *facts.Phone)
	}
}

func TestExtractFacts_RejectsGarbagePrice(t *testing.T) {
	t.Parallel()
	html := `<html><head>
	<script type="application/ld+json">
	{"@context":"https://schema.org","@type":"Restaurant","priceRange":"not(:empty){margin-top:4px}"}
	</script>
	</head><body></body></html>`
	facts := ExtractFacts(html, "https://example.com")
	if facts.Price != nil {
		t.Errorf("expected nil price for CSS garbage, got %q", *facts.Price)
	}
}

func TestExtractFacts_RejectsGarbageAddress(t *testing.T) {
	t.Parallel()
	html := `<html><head>
	<script type="application/ld+json">
	{"@context":"https://schema.org","@type":"Place","address":"и т. д. на официальном сайте Культура.РФ"}
	</script>
	</head><body></body></html>`
	facts := ExtractFacts(html, "https://example.com")
	if facts.Address != nil {
		t.Errorf("expected nil address for junk, got %q", *facts.Address)
	}
}

func TestExtractFacts_AcceptsValidStructuredData(t *testing.T) {
	t.Parallel()
	html := `<html><head>
	<script type="application/ld+json">
	{
		"@context":"https://schema.org",
		"@type":"Restaurant",
		"name":"Good Cafe",
		"telephone":"+7 (812) 555-12-34",
		"address":{"@type":"PostalAddress","streetAddress":"ул. Рубинштейна, 10","addressLocality":"Санкт-Петербург"},
		"priceRange":"1500-2500 ₽"
	}
	</script>
	</head><body></body></html>`
	facts := ExtractFacts(html, "https://example.com")
	if facts.Phone == nil {
		t.Error("expected valid phone")
	}
	if facts.Address == nil {
		t.Error("expected valid address")
	}
	if facts.Price == nil {
		t.Error("expected valid price")
	}
}

func assertFactPtr(t *testing.T, field string, got *string, want string) {
	t.Helper()
	if got == nil {
		t.Errorf("%s: expected %q, got nil", field, want)
		return
	}
	if *got != want {
		t.Errorf("%s: expected %q, got %q", field, want, *got)
	}
}

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

func TestExtractFacts_JSONLDPriority(t *testing.T) {
	t.Parallel()
	html := `<html><head>
	<script type="application/ld+json">
	{"@context":"https://schema.org","@type":"Place","telephone":"+7-111-222-33-44"}
	</script>
	</head><body>
	<p>Телефон: +7 (999) 888-77-66</p>
	</body></html>`

	facts := ExtractFacts(html, "https://example.com")
	assertFactPtr(t, "Phone", facts.Phone, "+7-111-222-33-44")
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

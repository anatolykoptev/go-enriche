package structured

import (
	"strings"
	"testing"
)

func TestParse_JSONLD_Place(t *testing.T) {
	t.Parallel()
	html := `<html><head>
	<script type="application/ld+json">
	{
		"@context": "https://schema.org",
		"@type": "Restaurant",
		"name": "Пиццерия Марио",
		"telephone": "+7 (812) 555-1234",
		"url": "https://mario.example.com",
		"openingHours": "Mo-Su 10:00-23:00",
		"priceRange": "500-1500 ₽",
		"address": {
			"@type": "PostalAddress",
			"streetAddress": "Невский проспект, 100",
			"addressLocality": "Санкт-Петербург"
		}
	}
	</script>
	</head><body></body></html>`

	data, err := Parse(strings.NewReader(html), "text/html", "https://example.com")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	place := data.FirstPlace()
	if place == nil {
		t.Fatal("expected Place, got nil")
	}
	assertStringPtr(t, "Name", place.Name, "Пиццерия Марио")
	assertStringPtr(t, "Type", place.Type, "Restaurant")
	assertStringPtr(t, "Phone", place.Phone, "+7 (812) 555-1234")
	assertStringPtr(t, "Website", place.Website, "https://mario.example.com")
	assertStringPtr(t, "Hours", place.Hours, "Mo-Su 10:00-23:00")
	assertStringPtr(t, "Price", place.Price, "500-1500 ₽")
	if place.Address == nil || !strings.Contains(*place.Address, "Невский") {
		t.Errorf("expected address containing Невский, got %v", place.Address)
	}
}

func TestParse_JSONLD_Article(t *testing.T) {
	t.Parallel()
	html := `<html><head>
	<script type="application/ld+json">
	{
		"@context": "https://schema.org",
		"@type": "NewsArticle",
		"headline": "Breaking News",
		"author": {"@type": "Person", "name": "John Doe"},
		"datePublished": "2026-02-28",
		"description": "Something happened"
	}
	</script>
	</head><body></body></html>`

	data, err := Parse(strings.NewReader(html), "text/html", "https://example.com")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	article := data.FirstArticle()
	if article == nil {
		t.Fatal("expected Article, got nil")
	}
	assertStringPtr(t, "Headline", article.Headline, "Breaking News")
	assertStringPtr(t, "Author", article.Author, "John Doe")
	assertStringPtr(t, "DatePublished", article.DatePublished, "2026-02-28")
	assertStringPtr(t, "Description", article.Description, "Something happened")
}

func TestParse_JSONLD_Event(t *testing.T) {
	t.Parallel()
	html := `<html><head>
	<script type="application/ld+json">
	{
		"@context": "https://schema.org",
		"@type": "Event",
		"name": "Go Meetup",
		"startDate": "2026-03-15T19:00",
		"endDate": "2026-03-15T22:00",
		"location": {"@type": "Place", "name": "Loft Hall"}
	}
	</script>
	</head><body></body></html>`

	data, err := Parse(strings.NewReader(html), "text/html", "https://example.com")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	event := data.FirstEvent()
	if event == nil {
		t.Fatal("expected Event, got nil")
	}
	assertStringPtr(t, "Name", event.Name, "Go Meetup")
	assertStringPtr(t, "StartDate", event.StartDate, "2026-03-15T19:00")
	assertStringPtr(t, "Location", event.Location, "Loft Hall")
}

func TestParse_JSONLD_Organization(t *testing.T) {
	t.Parallel()
	html := `<html><head>
	<script type="application/ld+json">
	{
		"@context": "https://schema.org",
		"@type": "Organization",
		"name": "Acme Corp",
		"url": "https://acme.example.com",
		"telephone": "+1-800-555-0199"
	}
	</script>
	</head><body></body></html>`

	data, err := Parse(strings.NewReader(html), "text/html", "https://example.com")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	org := data.FirstOrganization()
	if org == nil {
		t.Fatal("expected Organization, got nil")
	}
	assertStringPtr(t, "Name", org.Name, "Acme Corp")
	assertStringPtr(t, "URL", org.URL, "https://acme.example.com")
	assertStringPtr(t, "Phone", org.Phone, "+1-800-555-0199")
}

func TestParse_NoStructuredData(t *testing.T) {
	t.Parallel()
	html := `<html><body><p>Plain page</p></body></html>`

	data, err := Parse(strings.NewReader(html), "text/html", "https://example.com")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if data.FirstPlace() != nil {
		t.Error("expected nil Place for plain page")
	}
	if data.FirstArticle() != nil {
		t.Error("expected nil Article for plain page")
	}
}

func TestParse_AddressString(t *testing.T) {
	t.Parallel()
	html := `<html><head>
	<script type="application/ld+json">
	{
		"@context": "https://schema.org",
		"@type": "Place",
		"name": "Park",
		"address": "123 Main Street, City"
	}
	</script>
	</head><body></body></html>`

	data, err := Parse(strings.NewReader(html), "text/html", "https://example.com")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	place := data.FirstPlace()
	if place == nil {
		t.Fatal("expected Place, got nil")
	}
	assertStringPtr(t, "Address", place.Address, "123 Main Street, City")
}

func TestPlaces_Multiple(t *testing.T) {
	t.Parallel()
	html := `<html><head><script type="application/ld+json">
	{"@context":"https://schema.org","@graph":[
		{"@type":"Restaurant","name":"Счастье","address":"ул. Рубинштейна, 15"},
		{"@type":"Restaurant","name":"Frank","address":"ул. Рубинштейна, 29"}
	]}</script></head></html>`

	data, err := Parse(strings.NewReader(html), "text/html", "https://example.com")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	places := data.Places()
	if len(places) != 2 {
		t.Fatalf("expected 2 places, got %d", len(places))
	}
	if places[0].Name == nil || *places[0].Name != "Счастье" {
		t.Errorf("expected Счастье, got %v", places[0].Name)
	}
	if places[1].Name == nil || *places[1].Name != "Frank" {
		t.Errorf("expected Frank, got %v", places[1].Name)
	}
}

func TestPlaces_Empty(t *testing.T) {
	t.Parallel()
	html := `<html><head></head><body>no structured data</body></html>`
	data, err := Parse(strings.NewReader(html), "text/html", "https://example.com")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	places := data.Places()
	if len(places) != 0 {
		t.Errorf("expected 0 places, got %d", len(places))
	}
}

func assertStringPtr(t *testing.T, field string, got *string, want string) {
	t.Helper()
	if got == nil {
		t.Errorf("%s: expected %q, got nil", field, want)
		return
	}
	if *got != want {
		t.Errorf("%s: expected %q, got %q", field, want, *got)
	}
}

// TestParse_Organization_LegalSignals verifies the corroborant signals the
// extract layer reads to decide whether an Organization streetAddress is a legal
// seat (HasLegalID / LegalName). A bare Organization with NO such signal exposes
// neither — that is the case extract.orgAddressIsLegal must NOT treat as legal.
func TestParse_Organization_LegalSignals(t *testing.T) {
	t.Parallel()

	t.Run("taxID property sets HasLegalID", func(t *testing.T) {
		t.Parallel()
		html := `<html><body><div itemscope itemtype="http://schema.org/Organization">
<meta itemprop="name" content="ООО Игора Драйв"/>
<meta itemprop="taxID" content="7801321150"/>
<div itemprop="address" itemscope itemtype="http://schema.org/PostalAddress">
<span itemprop="streetAddress">11-я В.О. линия, 38</span></div></div></body></html>`
		org := parseOrg(t, html)
		if !org.HasLegalID {
			t.Errorf("HasLegalID = false, want true (taxID property present)")
		}
	})

	t.Run("ИНН token inside a nested item sets HasLegalID", func(t *testing.T) {
		t.Parallel()
		html := `<html><body><div itemscope itemtype="http://schema.org/Organization">
<meta itemprop="name" content="Студия"/>
<div itemprop="address" itemscope itemtype="http://schema.org/PostalAddress">
<span itemprop="streetAddress">ул. Ленина, 5, ИНН 7813045678</span></div></div></body></html>`
		org := parseOrg(t, html)
		if !org.HasLegalID {
			t.Errorf("HasLegalID = false, want true (ИНН token in nested streetAddress)")
		}
	})

	t.Run("legalName property is exposed", func(t *testing.T) {
		t.Parallel()
		html := `<html><body><div itemscope itemtype="http://schema.org/Organization">
<meta itemprop="name" content="Волна"/>
<meta itemprop="legalName" content="ООО «Волна»"/>
<div itemprop="address" itemscope itemtype="http://schema.org/PostalAddress">
<span itemprop="streetAddress">Литейный, 55</span></div></div></body></html>`
		org := parseOrg(t, html)
		if org.LegalName == nil || !strings.Contains(*org.LegalName, "Волна") {
			t.Errorf("LegalName = %v, want the legalName value", org.LegalName)
		}
	})

	t.Run("bare Organization exposes NO legal signal", func(t *testing.T) {
		t.Parallel()
		html := `<html><body><div itemscope itemtype="http://schema.org/Organization">
<meta itemprop="name" content="Кафе Уют"/>
<div itemprop="address" itemscope itemtype="http://schema.org/PostalAddress">
<span itemprop="streetAddress">Невский проспект, 28</span></div></div></body></html>`
		org := parseOrg(t, html)
		if org.HasLegalID {
			t.Errorf("HasLegalID = true, want false (no ИНН/ОГРН/taxID anywhere)")
		}
		if org.LegalName != nil {
			t.Errorf("LegalName = %q, want nil (no legalName property)", *org.LegalName)
		}
	})
}

func parseOrg(t *testing.T, html string) *Organization {
	t.Helper()
	data, err := Parse(strings.NewReader(html), "text/html", "https://example.com")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	org := data.FirstOrganization()
	if org == nil {
		t.Fatal("expected Organization, got nil")
	}
	return org
}

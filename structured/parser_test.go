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

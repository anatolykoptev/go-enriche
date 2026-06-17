package extract

import "testing"

func TestExtractSiteContacts_ContactsRegionBeatsWidget(t *testing.T) {
	t.Parallel()
	html := `<html><body>
	<div class="widget"><a href="tel:+78005553535">8 (800) 555-35-35</a></div>
	<footer class="footer"><a href="tel:+78126157000">+7 (812) 615 70 00</a></footer>
	</body></html>`
	c := ExtractSiteContacts(html)
	if c.Phone == nil || *c.Phone != "+7 (812) 615 70 00" {
		t.Fatalf("want contacts-region phone, got %v", c.Phone)
	}
	if c.PhoneRegion != "contacts" {
		t.Errorf("want region=contacts, got %q", c.PhoneRegion)
	}
}

func TestExtractSiteContacts_BodyTelIsOther(t *testing.T) {
	t.Parallel()
	html := `<html><body><p>Звоните <a href="tel:+78129998877">+7 (812) 999-88-77</a></p></body></html>`
	c := ExtractSiteContacts(html)
	if c.Phone == nil || *c.Phone != "+7 (812) 999-88-77" {
		t.Fatalf("want body phone, got %v", c.Phone)
	}
	if c.PhoneRegion != "other" {
		t.Errorf("want region=other, got %q", c.PhoneRegion)
	}
}

func TestExtractSiteContacts_MicrodataFallback(t *testing.T) {
	t.Parallel()
	// No tel: href; itemprop=telephone outside a Place/Org scope.
	html := `<html><body><span itemprop="telephone">+7 (812) 244-55-66</span></body></html>`
	c := ExtractSiteContacts(html)
	if c.Phone == nil || *c.Phone != "+7 (812) 244-55-66" {
		t.Fatalf("want microdata phone, got %v", c.Phone)
	}
	if c.PhoneRegion != "microdata" {
		t.Errorf("want region=microdata, got %q", c.PhoneRegion)
	}
}

func TestExtractSiteContacts_RejectsInvalidTel(t *testing.T) {
	t.Parallel()
	// tel: with a non-Rossvyaz code must be skipped by ValidatePhone.
	html := `<html><body><footer><a href="tel:+71231234567">+7 (123) 123-45-67</a></footer></body></html>`
	c := ExtractSiteContacts(html)
	if c.Phone != nil {
		t.Errorf("want nil for invalid area code, got %q", *c.Phone)
	}
}

func TestExtractSiteContacts_Mailto(t *testing.T) {
	t.Parallel()
	html := `<html><body><a href="mailto:info@example.ru?subject=Hi">write</a></body></html>`
	c := ExtractSiteContacts(html)
	if c.Email == nil || *c.Email != "info@example.ru" {
		t.Fatalf("want info@example.ru, got %v", c.Email)
	}
}

func TestExtractSiteContacts_Empty(t *testing.T) {
	t.Parallel()
	c := ExtractSiteContacts("")
	if c.Phone != nil || c.Email != nil {
		t.Error("want all-nil for empty html")
	}
}

// applyContactOverride: a contacts-region tel: overrides a JSON-LD telephone
// (the call-tracking-injected-JSON-LD vs human tel: case).
func TestApplyContactOverride_TelBeatsJSONLD(t *testing.T) {
	t.Parallel()
	html := `<html><head>
	<script type="application/ld+json">
	{"@context":"https://schema.org","@type":"Restaurant","telephone":"+7 (800) 555-35-35"}
	</script></head><body>
	<footer class="footer"><a href="tel:+78126157000">+7 (812) 615 70 00</a></footer>
	</body></html>`
	facts := ExtractFacts(html, "https://example.com")
	if facts.Phone == nil || *facts.Phone != "+7 (812) 615 70 00" {
		t.Fatalf("contacts tel: must override JSON-LD telephone, got %v", facts.Phone)
	}
}

// A body-only tel: must NOT override a JSON-LD telephone (structured wins over
// a weak body tel:); it only fills when JSON-LD/regex left phone nil.
func TestApplyContactOverride_BodyTelDoesNotBeatJSONLD(t *testing.T) {
	t.Parallel()
	html := `<html><head>
	<script type="application/ld+json">
	{"@context":"https://schema.org","@type":"Restaurant","telephone":"+7 (812) 222-33-44"}
	</script></head><body>
	<p>звоните <a href="tel:+79218887766">+7 (921) 888-77-66</a></p>
	</body></html>`
	facts := ExtractFacts(html, "https://example.com")
	if facts.Phone == nil || *facts.Phone != "+7 (812) 222-33-44" {
		t.Fatalf("JSON-LD must win over body tel:, got %v", facts.Phone)
	}
}

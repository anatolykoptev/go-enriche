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

// Regression for the reviewer-reproduced wrong-phone vector (PR #13 review):
// a real contacts-region tel: wrapped in a GENERIC widget container
// (WordPress .widget / Bitrix #bx-widget-area) must NOT be demoted, so it
// still beats a call-tracking JSON-LD telephone. The old narrow logic used
// [class*=widget]/[id*=widget] which falsely demoted these and let the 8-800
// JSON-LD win.
func TestApplyContactOverride_GenericWidgetWrapperNotDemoted(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		html string
	}{
		{
			name: "wordpress_footer_widget_aside",
			html: `<html><head>
			<script type="application/ld+json">
			{"@context":"https://schema.org","@type":"LocalBusiness","telephone":"+7 (800) 555-35-35"}
			</script></head><body>
			<footer class="footer"><aside class="widget widget_text">
			  <a href="tel:+78126157000">+7 (812) 615 70 00</a>
			</aside></footer></body></html>`,
		},
		{
			name: "bitrix_header_bx_widget_area",
			html: `<html><head>
			<script type="application/ld+json">
			{"@context":"https://schema.org","@type":"Organization","telephone":"+7 (800) 555-35-35"}
			</script></head><body>
			<header class="header"><div id="bx-widget-area">
			  <a href="tel:+78126157000">+7 (812) 615 70 00</a>
			</div></header></body></html>`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			facts := ExtractFacts(tc.html, "https://example.com")
			if facts.Phone == nil {
				t.Fatal("want contacts-region phone, got nil")
			}
			if *facts.Phone != "+7 (812) 615 70 00" {
				t.Errorf("generic widget wrapper must not demote real contacts tel:; want +7 (812) 615 70 00, got %q", *facts.Phone)
			}
		})
	}
}

// Conversely: a NAMED call-tracking widget nested INSIDE the contacts region
// must still be demoted, so its tracking number does not win.
func TestApplyContactOverride_CallTrackingNestedInContactsIsDemoted(t *testing.T) {
	t.Parallel()
	// Footer contains both the real tel: and a comagic tracking tel: nested
	// inside the footer. The real one (not call-tracking) must win.
	html := `<html><body>
	<footer class="footer">
	  <div class="comagic-phone"><a href="tel:+78005553535">8 (800) 555-35-35</a></div>
	  <address><a href="tel:+78123334455">+7 (812) 333-44-55</a></address>
	</footer></body></html>`
	facts := ExtractFacts(html, "https://example.com")
	if facts.Phone == nil || *facts.Phone != "+7 (812) 333-44-55" {
		t.Fatalf("nested call-tracking tel: must be demoted; want +7 (812) 333-44-55, got %v", facts.Phone)
	}
}

// --- social-link (DNI-immune) phone extraction (2026-06-17 reconciliation) ---

// The headline DNI regression: a Roistat-style site whose visible (812) tel:
// href + microdata is a rotating proxy must resolve to its hard-coded WhatsApp
// social-link number, NOT the rotating (812). Asserts both the SPb-city path
// (where the area-code rule would otherwise pick the local (812)) and the
// no-city path.
func TestApplyContactOverride_SocialLinkBeatsRotatingTel(t *testing.T) {
	t.Parallel()
	// Mirrors royal-wed.ru: a contacts-region (812) tel: (the DNI slot) plus a
	// matching 8x microdata (812), plus the stable WhatsApp social link.
	html := `<html><body>
	<header class="header">
	  <a href="tel:+78129561840">+7 (812) 956-18-40</a>
	  <a href="https://api.whatsapp.com/send?phone=79219561840">WhatsApp</a>
	  <a href="https://t.me/royal_wedding_spb">Telegram</a>
	</header>
	<div itemscope itemtype="https://schema.org/LocalBusiness">
	  <meta itemprop="telephone" content="+7 (812) 956-18-40">
	</div></body></html>`

	for _, city := range []string{"Санкт-Петербург", ""} {
		facts := ExtractFactsForCity(html, "https://royal-wed.ru", city)
		if facts.Phone == nil {
			t.Fatalf("city=%q: want social-link phone, got nil", city)
		}
		if *facts.Phone != "+79219561840" {
			t.Errorf("city=%q: want stable social link +79219561840, got %q", city, *facts.Phone)
		}
		if *facts.Phone == "+7 (812) 956-18-40" {
			t.Errorf("city=%q: must NOT assert the rotating DNI (812) proxy", city)
		}
	}
}

// With NO social link present, the existing behavior is unchanged: a clean
// non-DNI 812 site (Игора-Драйв class) still resolves to its (812) tel:.
func TestApplyContactOverride_NoSocialLinkKeepsTel(t *testing.T) {
	t.Parallel()
	html := `<html><body><footer class="footer">
	  <a href="tel:+78126157000">+7 (812) 615 70 00</a>
	</footer></body></html>`
	facts := ExtractFactsForCity(html, "https://drive-igora.ru", "Санкт-Петербург")
	if facts.Phone == nil || *facts.Phone != "+7 (812) 615 70 00" {
		t.Fatalf("no social link: want +7 (812) 615 70 00, got %v", facts.Phone)
	}
}

// A social-link 8-800 (unheard of, but guard it) must be demoted, not treated
// as the top authority — toll-free is never a venue's owned local line.
func TestSocialLink_TollFreeStillDemoted(t *testing.T) {
	t.Parallel()
	html := `<html><body>
	<a href="https://wa.me/78005553535">WhatsApp</a>
	<footer class="footer"><a href="tel:+78126157000">+7 (812) 615 70 00</a></footer>
	</body></html>`
	facts := ExtractFactsForCity(html, "https://example.com", "Санкт-Петербург")
	if facts.Phone == nil || *facts.Phone != "+7 (812) 615 70 00" {
		t.Fatalf("toll-free social link must be demoted, real 812 tel: must win; got %v", facts.Phone)
	}
}

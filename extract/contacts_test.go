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
// social-link number, NOT the rotating (812). The DNI vendor script MUST be
// present so detectDNIVendor triggers the DNI branch in resolvePhoneForCityDNI,
// which picks the social-link candidate unconditionally (it is the only
// DNI-immune source). Without the DNI script the page is clean and the
// labeled contacts-region tel: wins (issue #55). Asserts both the SPb-city
// path (where the area-code rule would otherwise pick the local (812)) and
// the no-city path.
func TestApplyContactOverride_SocialLinkBeatsRotatingTel(t *testing.T) {
	t.Parallel()
	// Mirrors royal-wed.ru: a contacts-region (812) tel: (the DNI slot) plus a
	// matching 8x microdata (812), plus the stable WhatsApp social link. The
	// Roistat loader script triggers the DNI branch.
	html := `<html><head>
	<script src="//cloud.roistat.com/api/site/1.0/abc/init"></script>
	</head><body>
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

// TestResolvePhoneForCity_SocialLinkDoesNotBeatLabeledTel is the issue #55
// regression: a CLEAN (no DNI vendor) page that carries BOTH a labeled
// contacts-region tel: (the venue's own office phone) AND a WhatsApp
// click-to-chat link embedding a DIFFERENT number (often a manager's personal
// mobile). The labeled contacts-region phone MUST win — a social-link number
// is DNI-immune but is not necessarily the number a listing should display;
// it must not unconditionally override the page's own labeled phone.
//
// Reproduces the live spb03.com case: header + footer both label
// +7(812)-214-61-41 as "Телефон", while a WhatsApp link embeds the unrelated
// 79219551631. Before the fix, pickPhoneCandidate's unconditional social-link
// pre-check (rule 1) picked the WhatsApp number and the real labeled phone
// was flagged "wrong" downstream.
func TestResolvePhoneForCity_SocialLinkDoesNotBeatLabeledTel(t *testing.T) {
	t.Parallel()
	// Clean page (no DNI vendor script): a labeled contacts-region tel: plus
	// a WhatsApp link with a DIFFERENT number — the spb03.com shape.
	html := `<html><body>
	<header class="header">
	  Телефон: <a href="tel:+78122146141">+7 (812) 214-61-41</a>
	  <a href="https://api.whatsapp.com/send?phone=79219551631">WhatsApp</a>
	</header>
	<footer class="footer">
	  Телефон: <a href="tel:+78122146141">+7 (812) 214-61-41</a>
	</footer>
	</body></html>`

	// Unit-level: resolvePhoneForCity must pick the labeled 812, not the
	// WhatsApp 921.
	doc, err := documentFromHTML(html)
	if err != nil || doc == nil {
		t.Fatalf("parse: %v", err)
	}
	phone, _, ok := resolvePhoneForCity(doc, "Санкт-Петербург")
	if !ok {
		t.Fatal("want a resolved phone, got !ok")
	}
	if phone != "+7 (812) 214-61-41" {
		t.Errorf("resolvePhoneForCity: want labeled +7 (812) 214-61-41, got %q (the WhatsApp number must NOT override the labeled contacts-region tel: on a clean page)", phone)
	}

	// End-to-end: ExtractFactsForCity must commit the labeled 812 as
	// Facts.Phone.
	facts := ExtractFactsForCity(html, "https://spb03.com", "Санкт-Петербург")
	if facts.Phone == nil {
		t.Fatal("Facts.Phone = nil, want the labeled contacts-region phone")
	}
	if *facts.Phone != "+7 (812) 214-61-41" {
		t.Errorf("Facts.Phone = %q, want labeled +7 (812) 214-61-41 (not the WhatsApp +79219551631)", *facts.Phone)
	}
}

// TestResolvePhoneForCity_SocialLinkStillWinsWhenOnlyCandidate guards the
// flip side of issue #55: when a social-link number is the ONLY phone
// candidate on the page (no labeled contacts-region tel:), it must still be
// picked — the DNI-immunity value is preserved as a fallback-when-nothing-
// else-exists. This is the case where a social-link number IS the right
// answer.
func TestResolvePhoneForCity_SocialLinkStillWinsWhenOnlyCandidate(t *testing.T) {
	t.Parallel()
	html := `<html><body>
	<header class="header">
	  <a href="https://api.whatsapp.com/send?phone=79219551631">WhatsApp</a>
	</header>
	</body></html>`
	doc, err := documentFromHTML(html)
	if err != nil || doc == nil {
		t.Fatalf("parse: %v", err)
	}
	phone, _, ok := resolvePhoneForCity(doc, "Санкт-Петербург")
	if !ok {
		t.Fatal("want a resolved phone, got !ok")
	}
	if phone != "+79219551631" {
		t.Errorf("want social-link +79219551631 (only candidate), got %q", phone)
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
// TestDNITrustworthy_AgreesBetweenResolverAndSiteNumbers is the FIX-3
// integration guard: resolvePhoneForCityDNI's DNI-branch pick/omit decision
// and CollectSiteNumbers' per-candidate Trustworthy verdict (sitenumbers.go)
// are both driven through the SAME shared predicate (dniTrustworthy,
// contacts.go) and must never disagree on a DNI-active page — the exact
// fail-OPEN risk the shared predicate exists to close (an unlisted vendor
// causing a rotating proxy to be marked Trustworthy where the resolver would
// have failed closed). Table-driven across {tier x dni} via single-candidate
// synthetic docs so each case is isolated.
func TestDNITrustworthy_AgreesBetweenResolverAndSiteNumbers(t *testing.T) {
	t.Parallel()
	const mangoScript = `<script src="https://widgets.mango-office.ru/widgets/mango.js"></script>`

	cases := []struct {
		name string
		html string
		// wantTrusted is CollectSiteNumbers' expected Trustworthy verdict for
		// the fixture's sole candidate.
		wantTrusted bool
		// checkResolver, when true, additionally asserts
		// resolvePhoneForCityDNI's pick/omit decision agrees with
		// wantTrusted. Scoped to DNI-active pages only: on a clean page
		// pickPhoneCandidate always returns its best-available candidate
		// regardless of anchoring (a display "best guess", not a trust
		// gate) — a different, deliberately looser semantic than the
		// SiteNumbers sidecar's Trustworthy bar, so comparing them there
		// would be apples-to-oranges. The DNI branch is the one place both
		// paths make the identical omit-or-trust decision.
		checkResolver bool
	}{
		{
			name:        "contacts_tier_no_dni_is_trustworthy",
			html:        `<html><body><div class="contacts"><a href="tel:+78120001111">+7 (812) 000-11-11</a></div></body></html>`,
			wantTrusted: true,
		},
		{
			name:        "body_tier_no_dni_is_not_trustworthy",
			html:        `<html><body><p>Звоните <a href="tel:+78120002222">+7 (812) 000-22-22</a></p></body></html>`,
			wantTrusted: false,
		},
		{
			name:          "contacts_tier_dni_active_is_not_trustworthy",
			html:          `<html><head>` + mangoScript + `</head><body><div class="contacts"><a href="tel:+78120003333">+7 (812) 000-33-33</a></div></body></html>`,
			wantTrusted:   false,
			checkResolver: true,
		},
		{
			name:          "social_link_dni_active_is_trustworthy",
			html:          `<html><head>` + mangoScript + `</head><body><a href="https://wa.me/78120004444">WhatsApp</a></body></html>`,
			wantTrusted:   true,
			checkResolver: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, err := documentFromHTML(tc.html)
			if err != nil || doc == nil {
				t.Fatalf("parse: %v", err)
			}

			nums := CollectSiteNumbers(doc, false) // no separate raw-fetch stage in this fixture
			if len(nums) != 1 {
				t.Fatalf("len(SiteNumbers) = %d, want exactly 1 candidate on this fixture; got %+v", len(nums), nums)
			}
			if nums[0].Trustworthy != tc.wantTrusted {
				t.Errorf("CollectSiteNumbers Trustworthy = %v, want %v", nums[0].Trustworthy, tc.wantTrusted)
			}

			if !tc.checkResolver {
				return
			}
			_, _, ok, dniOmit := resolvePhoneForCityDNI(doc, "")
			gotPicked := ok && !dniOmit
			if gotPicked != tc.wantTrusted {
				t.Errorf("resolvePhoneForCityDNI picked=%v (ok=%v dniOmit=%v), want picked=%v — DISAGREES with CollectSiteNumbers' Trustworthy=%v for the SAME candidate", gotPicked, ok, dniOmit, tc.wantTrusted, nums[0].Trustworthy)
			}
		})
	}
}

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

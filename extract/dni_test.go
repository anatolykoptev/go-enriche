package extract

import "testing"

// TestDetectDNIVendor covers the call-tracking / dynamic-number-insertion (DNI)
// vendor signatures the resolver uses to distrust an injected/rotating tel:.
// Detection is over already-fetched HTML only (script-src / global config token
// / widget data-attr) — no network I/O, no cross-fetch rotation, no screenshot.
func TestDetectDNIVendor(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		html       string
		wantVendor string
		wantDNI    bool
	}{
		{
			name:       "roistat script-src",
			html:       `<html><head><script src="//cloud.roistat.com/api/site/1.0/abc/init"></script></head><body></body></html>`,
			wantVendor: "roistat",
			wantDNI:    true,
		},
		{
			name:       "roistat global config token",
			html:       `<html><body><script>window.roistatProjectId="12345";</script></body></html>`,
			wantVendor: "roistat",
			wantDNI:    true,
		},
		{
			name:       "calltouch script-src",
			html:       `<html><head><script src="https://mod.calltouch.ru/init.js?id=xyz"></script></head></html>`,
			wantVendor: "calltouch",
			wantDNI:    true,
		},
		{
			name:       "comagic script-src",
			html:       `<html><head><script src="//app.comagic.ru/static/cs.min.js"></script></head></html>`,
			wantVendor: "comagic",
			wantDNI:    true,
		},
		{
			name:       "mango office widget",
			html:       `<html><head><script src="https://widgets.mango-office.ru/widgets/mango.js"></script></head></html>`,
			wantVendor: "mango",
			wantDNI:    true,
		},
		{
			name:       "callibri script-src",
			html:       `<html><head><script src="https://cdn.callibri.ru/callibri.js"></script></head></html>`,
			wantVendor: "callibri",
			wantDNI:    true,
		},
		{
			name:       "uis comagic (uiscom) script-src",
			html:       `<html><head><script src="//app.uiscom.ru/static/cs.min.js"></script></head></html>`,
			wantVendor: "uis",
			wantDNI:    true,
		},
		{
			name:    "clean site no DNI vendor",
			html:    `<html><head><script src="https://www.googletagmanager.com/gtag/js"></script></head><body><footer><a href="tel:+78126157000">+7 (812) 615 70 00</a></footer></body></html>`,
			wantDNI: false,
		},
		{
			name:    "generic widget word is not a DNI signal",
			html:    `<html><body><div class="widget-area"><a href="tel:+78126157000">+7 (812) 615 70 00</a></div></body></html>`,
			wantDNI: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, err := documentFromHTML(tc.html)
			if err != nil {
				t.Fatalf("doc: %v", err)
			}
			vendor, dni := detectDNIVendor(doc)
			if dni != tc.wantDNI {
				t.Errorf("detectDNIVendor dni=%v want %v (vendor=%q)", dni, tc.wantDNI, vendor)
			}
			if tc.wantDNI && vendor != tc.wantVendor {
				t.Errorf("detectDNIVendor vendor=%q want %q", vendor, tc.wantVendor)
			}
		})
	}
}

// TestApplyContactOverride_DNINoSocialOmitsRotatingTel is the headline PR-4
// fixture: a DNI venue (Roistat present) with NO social link must OMIT the
// rotating contacts-region tel: rather than ship it as authoritative. The
// proxy (812) the vendor injected is exactly the wrong-phone vector — with no
// DNI-immune alternative, the honest result is no phone («уточняйте»).
func TestApplyContactOverride_DNINoSocialOmitsRotatingTel(t *testing.T) {
	t.Parallel()
	html := `<html><head>
	  <script src="//cloud.roistat.com/api/site/1.0/abc/init"></script>
	</head><body>
	  <header class="header"><a href="tel:+78124391855">+7 (812) 439-18-55</a></header>
	  <div itemscope itemtype="https://schema.org/LocalBusiness">
	    <meta itemprop="telephone" content="+7 (812) 439-18-55">
	  </div>
	</body></html>`
	facts := ExtractFactsForCity(html, "https://dni-venue.ru", "Санкт-Петербург")
	if facts.Phone != nil {
		t.Errorf("DNI + no social link: phone must be omitted (got %q — a rotating proxy)", *facts.Phone)
	}
}

// TestApplyContactOverride_DNIWithSocialStillWins guards that PR-4 does NOT
// regress the Royal Wedding headline: with Roistat present AND a stable social
// link, the social-link phone still wins (it is DNI-immune).
func TestApplyContactOverride_DNIWithSocialStillWins(t *testing.T) {
	t.Parallel()
	html := `<html><head>
	  <script src="//cloud.roistat.com/api/site/1.0/abc/init"></script>
	</head><body>
	  <header class="header">
	    <a href="tel:+78129561840">+7 (812) 956-18-40</a>
	    <a href="https://api.whatsapp.com/send?phone=79219561840">WhatsApp</a>
	  </header>
	</body></html>`
	facts := ExtractFactsForCity(html, "https://royal-wed.ru", "Санкт-Петербург")
	if facts.Phone == nil || *facts.Phone != "+79219561840" {
		t.Fatalf("DNI + social link: want stable social +79219561840, got %v", facts.Phone)
	}
}

// TestApplyContactOverride_NonDNINoSocialKeepsTel guards the contrasting case:
// a clean NON-DNI site with no social link still keeps its (812) tel: (the
// Игора-Драйв class). Only a DNI signal triggers the omit. This is the
// discriminator that prevents PR-4 from over-omitting every social-less site.
func TestApplyContactOverride_NonDNINoSocialKeepsTel(t *testing.T) {
	t.Parallel()
	html := `<html><head>
	  <script src="https://www.googletagmanager.com/gtag/js"></script>
	</head><body><footer class="footer">
	  <a href="tel:+78126157000">+7 (812) 615 70 00</a>
	</footer></body></html>`
	facts := ExtractFactsForCity(html, "https://drive-igora.ru", "Санкт-Петербург")
	if facts.Phone == nil || *facts.Phone != "+7 (812) 615 70 00" {
		t.Fatalf("non-DNI no social: want +7 (812) 615 70 00 kept, got %v", facts.Phone)
	}
}

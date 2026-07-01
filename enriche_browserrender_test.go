package enriche

import (
	"context"
	"strings"
	"testing"
)

// richTextNoContacts is a text-rich article page (passes minExtractChars) that
// carries NO structured contact fact (no tel:, no address microdata, no hours).
// The contacts/hours block is JS-injected and therefore invisible to a raw fetch.
const richTextNoContacts = `<!DOCTYPE html><html lang="ru"><head><title>Студия</title></head>
<body><article><h1>Наша студия</h1>
<p>Мы проводим занятия для детей и взрослых уже более десяти лет в самом сердце города.
Опытные преподаватели, уютные залы и индивидуальный подход к каждому ученику.
Записывайтесь на пробное занятие и убедитесь сами в качестве нашей работы.</p>
<div id="contacts-root"><!-- contacts injected by JS at runtime --></div>
</article></body></html>`

// renderedWithContacts is what a full-JS render produces for the same page:
// the JS has populated the contacts block with a tel: link, a PostalAddress
// microdata block, and visible working hours via openingHours JSON-LD.
const renderedWithContacts = `<!DOCTYPE html><html lang="ru"><head><title>Студия</title>
<script type="application/ld+json">{"@context":"https://schema.org","@type":"LocalBusiness",
"name":"Студия","telephone":"+7 812 700 11 22",
"address":{"@type":"PostalAddress","streetAddress":"Невский проспект, 28","addressLocality":"Санкт-Петербург"},
"openingHours":"Mo-Su 10:00-22:00"}</script></head>
<body><article><h1>Наша студия</h1>
<p>Мы проводим занятия для детей и взрослых уже более десяти лет в самом сердце города.
Опытные преподаватели, уютные залы и индивидуальный подход к каждому ученику.
Записывайтесь на пробное занятие и убедитесь сами в качестве нашей работы.</p>
<div id="contacts-root"><a href="tel:+78127001122">+7 812 700-11-22</a>
<address>Невский проспект, 28</address></div>
</article></body></html>`

// TestEnrich_BrowserRender_AbsentContacts_SurfacesJSInjected is the headline
// Phase-A test: a text-rich page whose raw HTML has NO contacts triggers the
// full-JS render (reason absent_contacts), and the rendered DOM (Mo-Su hours,
// address, phone) is what the resolver merges — the JS-injected contacts that a
// raw fetch would have lost are now surfaced. Runs the FULL Enrich orchestration
// with a maps checker PRESENT (the synthetic-green discipline: the maps-merge-
// before-site path must be exercised, not just the leaf extractor).
func TestEnrich_BrowserRender_AbsentContacts_SurfacesJSInjected(t *testing.T) {
	t.Parallel()
	srv := newTestServer(richTextNoContacts, 200)
	defer srv.Close()

	var renderURL string
	var renderReasons []string
	e := newTestEnricher(
		WithFetcher(testFetcher()),
		WithMapsChecker(&mockMapsChecker{lat: 59.93, lon: 30.33}),
		WithBrowserFetch(func(_ context.Context, url string) (string, error) {
			renderURL = url
			return renderedWithContacts, nil
		}),
		WithMetrics(&Metrics{
			OnBrowserRender: func(reason string) { renderReasons = append(renderReasons, reason) },
		}),
	)

	result, err := e.Enrich(context.Background(), Item{
		Name: "Студия", URL: srv.URL, City: "Санкт-Петербург", Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}

	if renderURL != srv.URL {
		t.Fatalf("expected browser render of %q, got %q (render did not fire)", srv.URL, renderURL)
	}
	if len(renderReasons) != 1 || renderReasons[0] != "absent_contacts" {
		t.Fatalf("expected OnBrowserRender(absent_contacts) once, got %v", renderReasons)
	}
	if result.Facts.Hours == nil || !strings.Contains(*result.Facts.Hours, "10:00") {
		t.Fatalf("expected JS-injected hours surfaced, got %v", derefOrNil(result.Facts.Hours))
	}
	if result.Facts.Address == nil || !strings.Contains(*result.Facts.Address, "Невский") {
		t.Fatalf("expected JS-injected address surfaced, got %v", derefOrNil(result.Facts.Address))
	}
	if result.Facts.Phone == nil || !strings.Contains(*result.Facts.Phone, "700") {
		t.Fatalf("expected JS-injected phone surfaced, got %v", derefOrNil(result.Facts.Phone))
	}
}

// TestEnrich_BrowserRender_ContactsPresent_NoRender verifies the no-over-render
// guard: a raw page that ALREADY carries a contact fact (tel:) and rich content
// must NOT trigger a render — saving the cost and avoiding any render-only
// override of a raw contact.
func TestEnrich_BrowserRender_ContactsPresent_NoRender(t *testing.T) {
	t.Parallel()
	rawWithPhone := strings.Replace(richTextNoContacts,
		`<div id="contacts-root"><!-- contacts injected by JS at runtime --></div>`,
		`<div id="contacts-root"><a href="tel:+78120000000">+7 812 000-00-00</a></div>`, 1)
	srv := newTestServer(rawWithPhone, 200)
	defer srv.Close()

	rendered := false
	e := newTestEnricher(
		WithFetcher(testFetcher()),
		WithMapsChecker(&mockMapsChecker{lat: 59.93, lon: 30.33}),
		WithBrowserFetch(func(_ context.Context, _ string) (string, error) {
			rendered = true
			return renderedWithContacts, nil
		}),
	)
	if _, err := e.Enrich(context.Background(), Item{
		Name: "Студия", URL: srv.URL, City: "Санкт-Петербург", Mode: ModePlaces,
	}); err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if rendered {
		t.Fatal("render fired despite a contact fact present in raw HTML (over-render)")
	}
}

// TestEnrich_BrowserRender_ThinContent_Reason verifies the thin-content trigger
// is preserved and reported as thin_content (not absent_contacts).
func TestEnrich_BrowserRender_ThinContent_Reason(t *testing.T) {
	t.Parallel()
	srv := newTestServer(`<html><body><div>x</div></body></html>`, 200)
	defer srv.Close()

	var reasons []string
	e := newTestEnricher(
		WithFetcher(testFetcher()),
		WithBrowserFetch(func(_ context.Context, _ string) (string, error) {
			return renderedWithContacts, nil
		}),
		WithMetrics(&Metrics{OnBrowserRender: func(r string) { reasons = append(reasons, r) }}),
	)
	if _, err := e.Enrich(context.Background(), Item{
		Name: "Студия", URL: srv.URL, City: "Санкт-Петербург", Mode: ModePlaces,
	}); err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if len(reasons) != 1 || reasons[0] != "thin_content" {
		t.Fatalf("expected OnBrowserRender(thin_content) once, got %v", reasons)
	}
}

// TestEnrich_BrowserRender_NoNewFacts_KeepsRaw verifies that when the render
// surfaces NO additional contact facts, the raw extraction is retained (the
// render must not displace raw facts with an empty rendered DOM).
func TestEnrich_BrowserRender_NoNewFacts_KeepsRaw(t *testing.T) {
	t.Parallel()
	// Raw page is text-rich with no contacts → render fires (absent_contacts),
	// but the render returns the SAME contact-less HTML → no new facts.
	srv := newTestServer(richTextNoContacts, 200)
	defer srv.Close()

	e := newTestEnricher(
		WithFetcher(testFetcher()),
		WithBrowserFetch(func(_ context.Context, _ string) (string, error) {
			return richTextNoContacts, nil // render surfaces nothing new
		}),
	)
	result, err := e.Enrich(context.Background(), Item{
		Name: "Студия", URL: srv.URL, City: "Санкт-Петербург", Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if result.Facts.Hours != nil || result.Facts.Phone != nil {
		t.Fatalf("expected no contact facts (render added nothing), got hours=%v phone=%v",
			derefOrNil(result.Facts.Hours), derefOrNil(result.Facts.Phone))
	}
}

func derefOrNil(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

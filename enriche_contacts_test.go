package enriche

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-enriche/fetch"
	"github.com/anatolykoptev/go-enriche/maps"
)

// newMultiPathServer serves distinct HTML per request path. Used to model a
// venue whose homepage links a /contacts/ subpage that carries the richer
// contact set. A path absent from the map returns 404.
func newMultiPathServer(pages map[string]string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := pages[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(body)) //nolint:errcheck
	}))
}

// homeLinksContacts is a homepage with enough text to pass minExtractChars but
// NO contact facts — it only links the /contacts/ subpage.
const homeLinksContacts = `<!DOCTYPE html><html lang="ru"><head><title>Фабрика</title></head>
<body><article><h1>О фабрике</h1>
<p>Мы производим фрески и фотообои высочайшего качества уже более двадцати лет.
Наша продукция украшает интерьеры по всей стране, а опытные дизайнеры помогут
подобрать решение под любой проект и бюджет. Доставка по всем регионам.</p>
<nav><a href="/contacts/">Контакты</a></nav>
</article></body></html>`

// contactsPageRich is the /contacts/ subpage: it carries the email, address and
// hours the homepage omits, plus a city-local phone.
const contactsPageRich = `<!DOCTYPE html><html lang="ru"><head><title>Контакты</title></head>
<body><div class="contacts">
<a href="tel:+78124391100">+7 (812) 439-11-00</a>
<a href="mailto:salon@fabrika.ru">salon@fabrika.ru</a>
<address>Невский проспект, 28</address>
<div><span>Часы работы</span><span>Пн-Пт 10:00-21:00</span></div>
</div></body></html>`

// TestEnrich_ContactsPage_SurfacesEmailHoursHomepageLacks is the headline
// Phase-B orchestration test: a homepage with NO contacts but a /contacts/ link
// must trigger discovery, the contacts page must be fetched, and its email +
// hours + address + phone must surface through the FULL Enrich run with a maps
// checker PRESENT (synthetic-green discipline — the maps-merge-before-site path
// is exercised, not just the leaf extractor).
func TestEnrich_ContactsPage_SurfacesEmailHoursHomepageLacks(t *testing.T) {
	t.Parallel()
	srv := newMultiPathServer(map[string]string{
		"/":          homeLinksContacts,
		"/contacts/": contactsPageRich,
	})
	defer srv.Close()

	var discovered, resolved int
	e := New(
		WithFetcher(fetch.NewFetcher()),
		WithMapsChecker(&mockMapsChecker{lat: 59.93, lon: 30.33}),
		WithMetrics(&Metrics{
			OnContactsPageDiscovered: func() { discovered++ },
			OnContactsPageResolved:   func() { resolved++ },
		}),
	)
	result, err := e.Enrich(context.Background(), Item{
		Name: "Фабрика", URL: srv.URL + "/", City: "Санкт-Петербург", Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if discovered != 1 {
		t.Fatalf("OnContactsPageDiscovered = %d, want 1", discovered)
	}
	if resolved != 1 {
		t.Fatalf("OnContactsPageResolved = %d, want 1", resolved)
	}
	if result.Facts.Email == nil || *result.Facts.Email != "salon@fabrika.ru" {
		t.Fatalf("Email = %v, want salon@fabrika.ru", derefOrNil(result.Facts.Email))
	}
	if result.Facts.Hours == nil || !strings.Contains(*result.Facts.Hours, "10:00") {
		t.Fatalf("Hours = %v, want a 10:00 range from the contacts page", derefOrNil(result.Facts.Hours))
	}
	if result.Facts.Address == nil || !strings.Contains(*result.Facts.Address, "Невский") {
		t.Fatalf("Address = %v, want the contacts-page address", derefOrNil(result.Facts.Address))
	}
	if result.Facts.Phone == nil || !strings.Contains(*result.Facts.Phone, "439-11-00") {
		t.Fatalf("Phone = %v, want the contacts-page 812 phone", derefOrNil(result.Facts.Phone))
	}
	// Provenance must attribute the email to the official site (high confidence).
	if got := result.Provenance.Email.Source; got != "official_site" {
		t.Fatalf("Email provenance source = %q, want official_site", got)
	}
}

// contactsPageDNI is a /contacts/ subpage running a Mango call-tracking widget
// with a rotating tel: and NO wa.me social link — its phone must be OMITTED
// (DNI-poisoned), but its email and hours must still surface (DNI poisons only
// the phone).
const contactsPageDNI = `<!DOCTYPE html><html lang="ru"><head><title>Контакты</title>
<script src="https://widgets.mango-office.ru/widgets/mango.js"></script>
<script>var MangoObject="mango-office";</script></head>
<body><div class="contacts">
<a href="tel:+78137938615">+7 (813) 793-86-15</a>
<a href="mailto:info@igora.ru">info@igora.ru</a>
<div><span>Режим работы</span><span>ежедневно 09:00-23:00</span></div>
</div></body></html>`

// TestEnrich_ContactsPage_DNIOmitsPhoneKeepsEmailHours verifies the contacts
// page inherits the DNI anti-fab guarantee: a Mango-DNI contacts page with no
// social link omits the rotating phone, but its email and hours still surface.
// A maps checker returns a phone (the same rotating-class number) which must
// NOT survive the poison verdict.
func TestEnrich_ContactsPage_DNIOmitsPhoneKeepsEmailHours(t *testing.T) {
	t.Parallel()
	srv := newMultiPathServer(map[string]string{
		"/":          homeLinksContacts,
		"/contacts/": contactsPageDNI,
	})
	defer srv.Close()

	e := New(
		WithFetcher(fetch.NewFetcher()),
		WithMapsChecker(&mockMapsCheckerPhone{phone: "+7 813 793 86 15"}),
	)
	result, err := e.Enrich(context.Background(), Item{
		Name: "Игора", URL: srv.URL + "/", City: "Санкт-Петербург", Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if result.Facts.Phone != nil {
		t.Fatalf("Phone = %q, want nil (DNI contacts page, no social link → omit, and the maps proxy must not survive)", *result.Facts.Phone)
	}
	if result.Facts.Email == nil || *result.Facts.Email != "info@igora.ru" {
		t.Fatalf("Email = %v, want info@igora.ru (email survives DNI)", derefOrNil(result.Facts.Email))
	}
	if result.Facts.Hours == nil || !strings.Contains(*result.Facts.Hours, "09:00") {
		t.Fatalf("Hours = %v, want the 09:00 visible-hours block (hours survive DNI)", derefOrNil(result.Facts.Hours))
	}
}

// TestEnrich_ContactsPage_RenderErrorShellDegradesToRaw verifies that when the
// raw contacts page is contactless and the render returns a too-short error
// shell, the shell is NOT adopted, the render-error metric fires, and the
// pipeline degrades to the raw fetch (no crash, no shell-derived junk).
func TestEnrich_ContactsPage_RenderErrorShellDegradesToRaw(t *testing.T) {
	t.Parallel()
	// A contacts page whose raw HTML is a thin JS shell with no contacts.
	contactlessShell := `<!DOCTYPE html><html><head><title>Контакты</title></head><body><div id="app"></div></body></html>`
	srv := newMultiPathServer(map[string]string{
		"/":          homeLinksContacts,
		"/contacts/": contactlessShell,
	})
	defer srv.Close()

	var renderErrors int
	e := New(
		WithFetcher(fetch.NewFetcher()),
		WithMapsChecker(&mockMapsChecker{lat: 59.93, lon: 30.33}),
		WithBrowserFetch(func(_ context.Context, _ string) (string, error) {
			// A 200-byte bot-protection error shell — below minRenderShellBytes.
			return strings.Repeat("x", 200), nil
		}),
		WithMetrics(&Metrics{
			OnBrowserRenderError: func() { renderErrors++ },
		}),
	)
	result, err := e.Enrich(context.Background(), Item{
		Name: "X", URL: srv.URL + "/", City: "Санкт-Петербург", Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if renderErrors < 1 {
		t.Fatalf("OnBrowserRenderError = %d, want >=1 (shell render must register as a failure)", renderErrors)
	}
	// The contacts page yielded nothing; no shell-derived contact must appear.
	if result.Facts.Email != nil {
		t.Fatalf("Email = %q, want nil (error shell must not produce facts)", *result.Facts.Email)
	}
}

// TestEnrich_ContactsPage_NoDiscoveryWhenNoLink verifies the no-op path: a
// homepage with contacts and no /contacts/ link never triggers discovery.
func TestEnrich_ContactsPage_NoDiscoveryWhenNoLink(t *testing.T) {
	t.Parallel()
	homeWithContacts := strings.Replace(homeLinksContacts,
		`<nav><a href="/contacts/">Контакты</a></nav>`,
		`<div class="contacts"><a href="tel:+78120000000">+7 812 000-00-00</a></div>`, 1)
	srv := newMultiPathServer(map[string]string{"/": homeWithContacts})
	defer srv.Close()

	var discovered int
	e := New(
		WithFetcher(fetch.NewFetcher()),
		WithMapsChecker(&mockMapsChecker{lat: 59.93, lon: 30.33}),
		WithMetrics(&Metrics{OnContactsPageDiscovered: func() { discovered++ }}),
	)
	if _, err := e.Enrich(context.Background(), Item{
		Name: "X", URL: srv.URL + "/", City: "Санкт-Петербург", Mode: ModePlaces,
	}); err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if discovered != 0 {
		t.Fatalf("OnContactsPageDiscovered = %d, want 0 (no contacts link on homepage)", discovered)
	}
}

// mockMapsCheckerPhone returns a maps card carrying a phone (the rotating-class
// number a DNI venue's maps entry often holds) so the DNI-poison test can prove
// the maps phone does NOT survive a contacts-page poison verdict.
type mockMapsCheckerPhone struct {
	phone string
}

func (m *mockMapsCheckerPhone) Check(_ context.Context, _, _, _ string) (*maps.CheckResult, error) {
	return &maps.CheckResult{
		Status:  maps.PlaceOpen,
		OrgData: &maps.OrgData{Name: "Игора", Phone: m.phone},
	}, nil
}

// dniRawCleanRender models a homepage whose RAW HTML carries a Mango DNI widget
// and a rotating tel: (no social link) — but whose post-render DOM is "clean"
// (the widget rewrote itself away) and shows a plausible phone + address. The
// poison-OR must carry the raw DNI verdict forward so the rendered phone is
// still refused.
const dniRawShell = `<!DOCTYPE html><html lang="ru"><head><title>Клуб</title>
<script src="https://widgets.mango-office.ru/widgets/mango.js"></script>
<script>var MangoObject="mango-office";</script></head>
<body><div id="app"></div>
<a href="tel:+78137938615">+7 (813) 793-86-15</a></body></html>`

const dniCleanRendered = `<!DOCTYPE html><html lang="ru"><head><title>Клуб</title></head>
<body><article><p>Клуб приглашает гостей каждый день. У нас просторные залы,
внимательный персонал и удобное расположение в самом центре города рядом с метро.
Приходите целыми семьями — мы рады каждому посетителю и всегда готовы помочь.</p></article>
<div class="contacts">
<a href="tel:+78126157000">+7 (812) 615-70-00</a>
<address>Невский проспект, 28</address></div></body></html>`

// TestEnrich_PoisonOR_RawDNISurvivesCleanRender verifies the poison-OR: a raw
// DNI homepage whose rendered DOM looks clean must STILL omit the phone — the
// render must not launder a rotating proxy by replacing the poisoned raw facts
// with a clean-looking rendered set.
func TestEnrich_PoisonOR_RawDNISurvivesCleanRender(t *testing.T) {
	t.Parallel()
	srv := newMultiPathServer(map[string]string{"/": dniRawShell})
	defer srv.Close()

	e := New(
		WithFetcher(fetch.NewFetcher()),
		WithMapsChecker(&mockMapsChecker{lat: 59.93, lon: 30.33}),
		WithBrowserFetch(func(_ context.Context, _ string) (string, error) {
			return dniCleanRendered, nil
		}),
	)
	result, err := e.Enrich(context.Background(), Item{
		Name: "Клуб", URL: srv.URL + "/", City: "Санкт-Петербург", Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if result.Facts.Phone != nil {
		t.Fatalf("Phone = %q, want nil (raw DNI verdict must survive a clean render — poison-OR)", *result.Facts.Phone)
	}
	// The clean render's address is a non-phone fact and may still surface.
	if result.Facts.Address == nil || !strings.Contains(*result.Facts.Address, "Невский") {
		t.Fatalf("Address = %v, want the rendered address (only the phone is poisoned)", derefOrNil(result.Facts.Address))
	}
}

package enriche

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	e := newTestEnricher(
		WithFetcher(testFetcher()),
		// Guard-B (checkTarget) defaults to the real httputil.CheckRawURL (go-kit), which
		// refuses a loopback target — allow it here since contactsURL points at
		// the local httptest server in these tests (see allowAllTargets).
		WithTargetGuard(allowAllTargets),
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

	e := newTestEnricher(
		WithFetcher(testFetcher()),
		// Guard-B (checkTarget) defaults to the real httputil.CheckRawURL (go-kit), which
		// refuses a loopback target — allow it here since contactsURL points at
		// the local httptest server in these tests (see allowAllTargets).
		WithTargetGuard(allowAllTargets),
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
	// PhonePoisoned must propagate onto the public Result so a consumer (e.g.
	// wp_verify_contacts' omitted_dni verdict) can tell "DNI/call-tracking,
	// trust as scraped" apart from "genuinely no phone found" — a bare nil
	// Phone says nothing on its own. Before the dropPhone fix this was always
	// false: the resolver dropped the phone but never set the flag.
	if !result.Facts.PhonePoisoned {
		t.Fatalf("PhonePoisoned = false, want true (DNI verdict must propagate onto Result.Facts, not just drop the phone silently)")
	}
	// The poison-lock verdict is itself resolved provenance, not an absent
	// field — snapshot() must surface it even though Phone is nil.
	if got := result.Provenance.Phone.Source; got != "poison_locked" {
		t.Fatalf("Phone provenance source = %q, want poison_locked (the DNI verdict, not an empty/absent provenance)", got)
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
	e := newTestEnricher(
		WithFetcher(testFetcher()),
		// Guard-B (checkTarget) defaults to the real httputil.CheckRawURL (go-kit), which
		// refuses a loopback target — allow it here since contactsURL points at
		// the local httptest server in these tests (see allowAllTargets).
		WithTargetGuard(allowAllTargets),
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
	// HIGH#1: a render ATTEMPTED-BUT-FAILED (shell) degrades to raw-only with no
	// successful render corroboration, so the go-wp Correctable gate must see
	// RenderSkipped=true — a degrade is not render-confirmed data. (homeLinksContacts
	// also fails its homepage render here, so either leg suffices to set the flag.)
	if !result.RenderSkipped {
		t.Fatal("RenderSkipped = false, want true (render-error degrade rests on raw-only, must be marked)")
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
	e := newTestEnricher(
		WithFetcher(testFetcher()),
		// Guard-B (checkTarget) defaults to the real httputil.CheckRawURL (go-kit), which
		// refuses a loopback target — allow it here since contactsURL points at
		// the local httptest server in these tests (see allowAllTargets).
		WithTargetGuard(allowAllTargets),
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

// homeRichNoPhoneLinksContacts is a homepage complete on hours/email/address
// but with ZERO phone at all — homepageMissingRichField's anchored-phone leg
// fires regardless of the other fields, so /contacts discovery still runs.
const homeRichNoPhoneLinksContacts = `<!DOCTYPE html><html lang="ru"><head><title>Кафе</title></head>
<body><article><h1>О кафе</h1>
<p>Уютное кафе в центре города. Мы готовим вкусные блюда из свежих продуктов
каждый день и рады гостям с самого утра до позднего вечера. Большой выбор
напитков, десертов и сезонное меню. Приходите всей семьёй — у нас уютно.</p>
<a href="mailto:hello@cafe.ru">hello@cafe.ru</a>
<address>Невский проспект, 28</address>
<div><span>Часы работы</span><span>Пн-Вс 10:00-22:00</span></div>
<nav><a href="/contacts/">Контакты</a></nav>
</article></body></html>`

// contactsPagePhoneOnlyAnchored is a /contacts page whose ONLY fact is a
// single anchored (contacts-region) tel: — strictly FEWER structured facts
// (1) than homeRichNoPhoneLinksContacts (3: address+hours+email), so the
// richness gate (contactFactCount) must skip the Facts MERGE entirely.
const contactsPagePhoneOnlyAnchored = `<!DOCTYPE html><html lang="ru"><head><title>Контакты</title></head>
<body><div class="contacts">
<a href="tel:+78120000099">+7 (812) 000-00-99</a>
</div></body></html>`

// TestEnrich_ContactsPage_SiteNumbersSurviveRichnessGate is the FIX-1
// headline: a homepage complete on hours/email/address but with NO phone
// triggers /contacts discovery (homepageMissingRichField's anchored-phone
// leg). The /contacts page carries ONLY an anchored tel: — fewer structured
// facts than the homepage — so contactFactCount's richness gate must skip
// the Facts MERGE (Facts.Phone stays nil, additive-only guarantee: the
// single-winner path is untouched). But the full candidate-set sidecar
// (Result.SiteNumbers) must still pick up that anchored phone: it
// accumulates from EVERY page actually fetched+parsed, independent of the
// richness gate that governs only the single-winner merge. Before the fix,
// addSiteNumbers sat AFTER the early return and this candidate was silently
// dropped — the feature's own headline case doing nothing.
func TestEnrich_ContactsPage_SiteNumbersSurviveRichnessGate(t *testing.T) {
	t.Parallel()
	srv := newMultiPathServer(map[string]string{
		"/":          homeRichNoPhoneLinksContacts,
		"/contacts/": contactsPagePhoneOnlyAnchored,
	})
	defer srv.Close()

	var discovered, resolved int
	e := newTestEnricher(
		WithFetcher(testFetcher()),
		// Guard-B (checkTarget) defaults to the real httputil.CheckRawURL (go-kit), which
		// refuses a loopback target — allow it here since contactsURL points at
		// the local httptest server in these tests (see allowAllTargets).
		WithTargetGuard(allowAllTargets),
		WithMapsChecker(&mockMapsChecker{lat: 59.93, lon: 30.33}),
		WithMetrics(&Metrics{
			OnContactsPageDiscovered: func() { discovered++ },
			OnContactsPageResolved:   func() { resolved++ },
		}),
	)
	result, err := e.Enrich(context.Background(), Item{
		Name: "Кафе", URL: srv.URL + "/", City: "Санкт-Петербург", Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if discovered != 1 {
		t.Fatalf("OnContactsPageDiscovered = %d, want 1 (homepage has zero phone, must still discover /contacts)", discovered)
	}
	// The richness gate must skip the Facts MERGE: the /contacts page (1
	// fact: the phone) is not strictly richer than the homepage (3 facts:
	// address+hours+email), so resolved must stay 0 and Facts.Phone nil.
	if resolved != 0 {
		t.Fatalf("OnContactsPageResolved = %d, want 0 (richness gate must skip the Facts merge for a no-richer contacts page)", resolved)
	}
	if result.Facts.Phone != nil {
		t.Fatalf("Facts.Phone = %q, want nil (richness gate skipped the merge — Facts.Phone must stay byte-stable)", *result.Facts.Phone)
	}
	found := false
	for _, n := range result.SiteNumbers {
		if strings.Contains(n.Value, "000-00-99") {
			found = true
			if !n.Anchored || !n.Trustworthy {
				t.Errorf("SiteNumbers entry for 000-00-99 = %+v, want Anchored=true Trustworthy=true", n)
			}
		}
	}
	if !found {
		t.Fatalf("SiteNumbers missing the /contacts anchored phone (richness-gate-skipped page); got %+v", result.SiteNumbers)
	}
}

// homeWeakPhoneLinksContacts carries a phone as a WEAK, plain body tel:
// (tierBody — not anchored, not Trustworthy) and links /contacts. Missing
// hours/email/address, so /contacts discovery fires regardless of the
// anchored-phone leg.
const homeWeakPhoneLinksContacts = `<!DOCTYPE html><html lang="ru"><head><title>Студия</title></head>
<body><article><h1>О студии</h1>
<p>Студия работает уже много лет и помогает клиентам с самыми разными задачами.
Мы гордимся качеством работы и вниманием к каждому проекту, большим и малым.
Обращайтесь в любое время — всегда рады новым клиентам и их идеям.</p>
Звоните: <a href="tel:+78120001122">+7 (812) 000-11-22</a>
<nav><a href="/contacts/">Контакты</a></nav>
</article></body></html>`

// contactsPageStrongSamePhone carries the SAME digits as an ANCHORED,
// Trustworthy contacts-region tel: (tierContacts) plus an email — strictly
// richer than homeWeakPhoneLinksContacts, so the richness gate does not
// skip the merge.
const contactsPageStrongSamePhone = `<!DOCTYPE html><html lang="ru"><head><title>Контакты</title></head>
<body><div class="contacts">
<a href="tel:+78120001122">+7 (812) 000-11-22</a>
<a href="mailto:studio@example.ru">studio@example.ru</a>
</div></body></html>`

// TestEnrich_SiteNumbersSnapshot_HigherRankReadingWins is the FIX-5
// headline: the SAME phone number appears on BOTH the homepage (a weak,
// unanchored body tel:) and the /contacts page (an anchored, Trustworthy
// contacts-region tel:). siteNumbersSnapshot's dedup must keep the
// STRONGER (Trustworthy) reading, not whichever page happened to merge
// first or last.
func TestEnrich_SiteNumbersSnapshot_HigherRankReadingWins(t *testing.T) {
	t.Parallel()
	srv := newMultiPathServer(map[string]string{
		"/":          homeWeakPhoneLinksContacts,
		"/contacts/": contactsPageStrongSamePhone,
	})
	defer srv.Close()

	e := newTestEnricher(
		WithFetcher(testFetcher()),
		// Guard-B (checkTarget) defaults to the real httputil.CheckRawURL (go-kit), which
		// refuses a loopback target — allow it here since contactsURL points at
		// the local httptest server in these tests (see allowAllTargets).
		WithTargetGuard(allowAllTargets),
		WithMapsChecker(&mockMapsChecker{lat: 59.93, lon: 30.33}),
	)
	result, err := e.Enrich(context.Background(), Item{
		Name: "Студия", URL: srv.URL + "/", City: "Санкт-Петербург", Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}

	var matches int
	for _, n := range result.SiteNumbers {
		if !strings.Contains(n.Value, "000-11-22") {
			continue
		}
		matches++
		if !n.Anchored || !n.Trustworthy {
			t.Errorf("SiteNumbers entry for 000-11-22 = %+v, want the STRONGER (Anchored=true Trustworthy=true) contacts-page reading to survive the dedup, not the weak homepage body-tel: reading", n)
		}
	}
	if matches != 1 {
		t.Fatalf("SiteNumbers has %d entries for 000-11-22, want exactly 1 (deduped across homepage + /contacts)", matches)
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

	e := newTestEnricher(
		WithFetcher(testFetcher()),
		// Guard-B (checkTarget) defaults to the real httputil.CheckRawURL (go-kit), which
		// refuses a loopback target — allow it here since contactsURL points at
		// the local httptest server in these tests (see allowAllTargets).
		WithTargetGuard(allowAllTargets),
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

// dniRawShellZeroPhone models a homepage whose RAW HTML runs a Mango DNI
// widget and carries ZERO phone at all (no tel:, no social link) —
// contactless, which forces the render (hasContactFacts is false, same gate
// as dniRawShell above). Because there is no phone candidate on the raw
// page, resolvePhoneForCityDNI never even reaches its DNI check (it
// short-circuits on an empty candidate set), so rawFacts.PhonePoisoned
// alone would be false despite the vendor being active — exactly the gap
// homeRawPoisoned (enriche_fetch.go) closes via extract.HasDNIVendor.
const dniRawShellZeroPhone = `<!DOCTYPE html><html lang="ru"><head><title>Клуб</title>
<script src="https://widgets.mango-office.ru/widgets/mango.js"></script>
<script>var MangoObject="mango-office";</script></head>
<body><div id="app"></div></body></html>`

// dniCleanRenderedInjectsTel models the SELF-REMOVING-WIDGET case the
// Poison-OR mechanism exists to defeat: the RENDERED DOM OMITS the DNI
// script entirely (the widget rewrote/removed itself at runtime) and shows
// a plausible tel: in the contacts region — exactly what a rotating proxy
// looks like once the vendor loader is gone from the post-render markup.
const dniCleanRenderedInjectsTel = `<!DOCTYPE html><html lang="ru"><head><title>Клуб</title></head>
<body><article><p>Клуб приглашает гостей каждый день. У нас просторные залы,
внимательный персонал и удобное расположение в самом центре города рядом с метро.
Приходите целыми семьями — мы рады каждому посетителю и всегда готовы помочь.</p></article>
<div class="contacts">
<a href="tel:+78137938615">+7 (813) 793-86-15</a></div></body></html>`

// TestEnrich_SiteNumbers_PoisonOR_RawDNISurvivesCleanRender is the Critical
// regression guard (pr-review-council re-review of PR #27, commit 8709dae):
// a homepage whose RAW fetch runs a DNI vendor but has NO phone at all
// forces a render (contactless raw). The RENDERED DOM omits the vendor
// script and injects a tel: — the exact self-removing-widget case the
// Facts.Phone Poison-OR (enriche_fetch.go:165) already defeats. Before this
// fix, CollectSiteNumbersHTML re-ran detectDNIVendor independently on ONLY
// the rendered HTML, saw no vendor, and marked the injected (rotating-proxy)
// tel: Trustworthy=true in the exported, cache-persisted Result.SiteNumbers
// — laundering it past the same anti-fab guarantee Facts.Phone already
// enforces. The fix ORs the raw-fetch poison signal (homeRawPoisoned) into
// the page-level DNI the Trustworthy gate uses, so the candidate fails
// closed regardless of what the rendered DOM looks like.
func TestEnrich_SiteNumbers_PoisonOR_RawDNISurvivesCleanRender(t *testing.T) {
	t.Parallel()
	srv := newMultiPathServer(map[string]string{"/": dniRawShellZeroPhone})
	defer srv.Close()

	e := newTestEnricher(
		WithFetcher(testFetcher()),
		// Guard-B (checkTarget) defaults to the real httputil.CheckRawURL (go-kit), which
		// refuses a loopback target — allow it here since contactsURL points at
		// the local httptest server in these tests (see allowAllTargets).
		WithTargetGuard(allowAllTargets),
		WithMapsChecker(&mockMapsChecker{lat: 59.93, lon: 30.33}),
		WithBrowserFetch(func(_ context.Context, _ string) (string, error) {
			return dniCleanRenderedInjectsTel, nil
		}),
	)
	result, err := e.Enrich(context.Background(), Item{
		Name: "Клуб", URL: srv.URL + "/", City: "Санкт-Петербург", Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}

	found := false
	for _, n := range result.SiteNumbers {
		if !strings.Contains(n.Value, "793-86-15") {
			continue
		}
		found = true
		if !n.DNI {
			t.Errorf("SiteNumbers entry for 793-86-15 DNI = false, want true (the raw-fetch poison verdict must survive the clean render)")
		}
		if n.Trustworthy {
			t.Errorf("SiteNumbers entry for 793-86-15 Trustworthy = true, want false (pre-fix bug: an injected rotating-proxy tel: laundered as trustworthy because the RENDERED DOM alone looked clean)")
		}
	}
	if !found {
		t.Fatalf("SiteNumbers missing the rendered 793-86-15 candidate; got %+v", result.SiteNumbers)
	}
}

// TestEnrich_PoisonOR_ZeroCandidateDNI_FactsPhoneSurvivesCleanRender is the
// Facts.Phone twin of TestEnrich_SiteNumbers_PoisonOR_RawDNISurvivesCleanRender
// above: a homepage whose RAW fetch runs a DNI vendor but carries ZERO phone
// candidates forces a render (contactless raw); the RENDERED DOM omits the
// vendor script and injects a tel: — the self-removing-widget case.
//
// Before this fix, the Facts.Phone carry-forward in enriche_fetch.go (the
// "if rawFacts.PhonePoisoned && !renderedFacts.PhonePoisoned" guard) used
// rawFacts.PhonePoisoned ALONE: resolvePhoneForCityDNI never even checks for
// a DNI vendor when the raw page has zero phone candidates (it short-circuits
// on the empty candidate set first), so rawFacts.PhonePoisoned stayed false
// despite the Mango loader being present in the raw HTML — the injected
// rendered tel: then surfaced as the authoritative, UNPOISONED Facts.Phone.
// The fix ORs extract.HasDNIVendor(fr.HTML) into homeRawPoisoned and uses
// THAT in the carry-forward, so the candidate fails closed regardless of
// what the rendered DOM alone looks like — mirroring the SiteNumbers fix
// from PR #27 (391833f) that this test's sibling above already guards.
func TestEnrich_PoisonOR_ZeroCandidateDNI_FactsPhoneSurvivesCleanRender(t *testing.T) {
	t.Parallel()
	srv := newMultiPathServer(map[string]string{"/": dniRawShellZeroPhone})
	defer srv.Close()

	e := newTestEnricher(
		WithFetcher(testFetcher()),
		// Guard-B (checkTarget) defaults to the real httputil.CheckRawURL (go-kit), which
		// refuses a loopback target — allow it here since contactsURL points at
		// the local httptest server in these tests (see allowAllTargets).
		WithTargetGuard(allowAllTargets),
		WithMapsChecker(&mockMapsChecker{lat: 59.93, lon: 30.33}),
		WithBrowserFetch(func(_ context.Context, _ string) (string, error) {
			return dniCleanRenderedInjectsTel, nil
		}),
	)
	result, err := e.Enrich(context.Background(), Item{
		Name: "Клуб", URL: srv.URL + "/", City: "Санкт-Петербург", Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}

	if result.Facts.Phone != nil {
		t.Fatalf("Facts.Phone = %q, want nil (the injected rendered tel: is a rotating-proxy candidate from a raw DNI page with zero phone candidates — must not surface as authoritative)",
			*result.Facts.Phone)
	}
	if !result.Facts.PhonePoisoned {
		t.Errorf("Facts.PhonePoisoned = false, want true (zero-candidate raw DNI must still poison the rendered phone)")
	}
	if result.Provenance.Phone.Source != srcStrPoisonLocked {
		t.Errorf("Provenance.Phone.Source = %q, want %q (poison-lock verdict, not a laundered clean phone)",
			result.Provenance.Phone.Source, srcStrPoisonLocked)
	}
}

// homeCleanPhoneLinksContacts is a homepage carrying a CLEAN city-local tel:
// (official_site) but NO email/hours/address, linking a /contacts/ subpage. It
// models the FIX-1 recall-regression case: the stable homepage phone must
// survive even when the /contacts page runs a DNI widget.
const homeCleanPhoneLinksContacts = `<!DOCTYPE html><html lang="ru"><head><title>Студия</title></head>
<body><article><h1>Студия</h1>
<p>Свадебная студия с многолетним опытом. Мы помогаем парам организовать
торжество мечты под ключ: площадка, декор, ведущий и фотограф. Десятки счастливых
пар каждый сезон доверяют нам свой самый важный день. Звоните, всё расскажем.</p>
<a href="tel:+78129561840">+7 (812) 956-18-40</a>
<nav><a href="/contacts/">Контакты</a></nav>
</article></body></html>`

// TestEnrich_ContactsPageDNI_PreservesCleanHomepagePhone is the FIX-1 headline:
// a venue whose HOMEPAGE carries a clean official_site phone but whose /contacts
// page runs a Mango DNI widget (no social link) must KEEP the homepage phone —
// the contacts-page DNI suppresses only the contacts page's own rotating number,
// never the homepage's stable one. (Before the fix, dropPhone() niled it.)
func TestEnrich_ContactsPageDNI_PreservesCleanHomepagePhone(t *testing.T) {
	t.Parallel()
	srv := newMultiPathServer(map[string]string{
		"/":          homeCleanPhoneLinksContacts,
		"/contacts/": contactsPageDNI,
	})
	defer srv.Close()

	e := newTestEnricher(
		WithFetcher(testFetcher()),
		// Guard-B (checkTarget) defaults to the real httputil.CheckRawURL (go-kit), which
		// refuses a loopback target — allow it here since contactsURL points at
		// the local httptest server in these tests (see allowAllTargets).
		WithTargetGuard(allowAllTargets),
		// maps returns the rotating-class proxy — it must not survive either, but
		// the homepage clean phone is what must remain.
		WithMapsChecker(&mockMapsCheckerPhone{phone: "+7 813 793 86 15"}),
	)
	result, err := e.Enrich(context.Background(), Item{
		Name: "Студия", URL: srv.URL + "/", City: "Санкт-Петербург", Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if result.Facts.Phone == nil || !strings.Contains(*result.Facts.Phone, "956-18-40") {
		t.Fatalf("Phone = %v, want the homepage 812 956-18-40 to SURVIVE the /contacts DNI verdict", derefOrNil(result.Facts.Phone))
	}
	if got := result.Provenance.Phone.Source; got != "official_site" {
		t.Fatalf("Phone provenance source = %q, want official_site (homepage phone, not poison-locked)", got)
	}
	// dropPhone's early-return guard must ALSO keep PhonePoisoned false: this
	// call site refused to drop anything (a clean official-site phone already
	// resolved), so it must not claim a poison verdict happened.
	if result.Facts.PhonePoisoned {
		t.Fatalf("PhonePoisoned = true, want false (dropPhone's early-return guard must not set the flag when it declines to drop the phone)")
	}
	// The contacts page still contributes its email/hours (DNI poisons only phone).
	if result.Facts.Email == nil || *result.Facts.Email != "info@igora.ru" {
		t.Fatalf("Email = %v, want the contacts-page email (multi-field win)", derefOrNil(result.Facts.Email))
	}
}

// TestEnrich_HomepageIsDNI_NoCleanPhoneAnywhere_StillOmits is the FIX-1
// regression guard for the drive-igora behaviour: when the HOMEPAGE itself is
// the DNI site and no clean phone exists anywhere, the phone must STILL be
// omitted (the new dropPhone() gate must not accidentally keep a maps proxy).
func TestEnrich_HomepageIsDNI_NoCleanPhoneAnywhere_StillOmits(t *testing.T) {
	t.Parallel()
	// Homepage runs Mango DNI with a rotating tel: and no social link, and has
	// enough text to avoid the thin-content render trigger.
	homeDNI := `<!DOCTYPE html><html lang="ru"><head><title>Клуб</title>
<script src="https://widgets.mango-office.ru/widgets/mango.js"></script>
<script>var MangoObject="mango-office";</script></head>
<body><article><p>Клуб приглашает гостей каждый день: просторные залы, внимательный
персонал и удобное расположение в самом центре города рядом с метро. Приходите
целыми семьями — мы рады каждому посетителю и всегда готовы помочь с выбором.</p>
<a href="tel:+78137938615">+7 (813) 793-86-15</a></article></body></html>`
	srv := newMultiPathServer(map[string]string{"/": homeDNI})
	defer srv.Close()

	e := newTestEnricher(
		WithFetcher(testFetcher()),
		// Guard-B (checkTarget) defaults to the real httputil.CheckRawURL (go-kit), which
		// refuses a loopback target — allow it here since contactsURL points at
		// the local httptest server in these tests (see allowAllTargets).
		WithTargetGuard(allowAllTargets),
		WithMapsChecker(&mockMapsCheckerPhone{phone: "+7 813 793 86 15"}),
	)
	result, err := e.Enrich(context.Background(), Item{
		Name: "Клуб", URL: srv.URL + "/", City: "Санкт-Петербург", Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if result.Facts.Phone != nil {
		t.Fatalf("Phone = %q, want nil (homepage IS DNI, no clean phone — drop+lock must still fire)", *result.Facts.Phone)
	}
}

// contactsPageDNIShellRender models a /contacts page whose RAW HTML carries a
// Mango DNI widget and a contactless body (so a render fires), and whose
// RENDERED DOM is "clean" (the widget rewrote itself away) showing a plausible
// tel:. The FIX-2 poison-OR must carry the raw DNI verdict forward so the
// rendered phone is still refused — a render must not launder a rotating proxy.
const contactsPageDNIRawShell = `<!DOCTYPE html><html lang="ru"><head><title>Контакты</title>
<script src="https://widgets.mango-office.ru/widgets/mango.js"></script>
<script>var MangoObject="mango-office";</script></head>
<body><div id="app"></div></body></html>`

const contactsPageCleanRendered = `<!DOCTYPE html><html lang="ru"><head><title>Контакты</title></head>
<body><main><article><h1>Как нас найти</h1>
<p>Мы всегда рады гостям и подробно расскажем, как до нас добраться на любом виде
транспорта. Ниже вы найдёте наш адрес, телефон и электронную почту для связи.
Если у вас остались вопросы, напишите нам — мы отвечаем в течение рабочего дня и
поможем подобрать удобное время визита. Парковка для гостей доступна рядом.</p>
</article>
<div class="contacts">
<a href="tel:+78126157000">+7 (812) 615-70-00</a>
<a href="mailto:info@club.ru">info@club.ru</a>
<address>Невский проспект, 28</address></div></main></body></html>`

// TestEnrich_ContactsPagePoisonOR_RawDNISurvivesCleanRender is the FIX-2
// headline: a /contacts page whose RAW carries a DNI widget but whose RENDERED
// DOM looks clean must STILL omit the phone — the contacts-page render must not
// launder a rotating proxy. (Mirrors the homepage TestEnrich_PoisonOR test, on
// the /contacts render path the fix adds.)
func TestEnrich_ContactsPagePoisonOR_RawDNISurvivesCleanRender(t *testing.T) {
	t.Parallel()
	srv := newMultiPathServer(map[string]string{
		"/":          homeLinksContacts, // contactless homepage → contacts discovery fires
		"/contacts/": contactsPageDNIRawShell,
	})
	defer srv.Close()

	e := newTestEnricher(
		WithFetcher(testFetcher()),
		// Guard-B (checkTarget) defaults to the real httputil.CheckRawURL (go-kit), which
		// refuses a loopback target — allow it here since contactsURL points at
		// the local httptest server in these tests (see allowAllTargets).
		WithTargetGuard(allowAllTargets),
		WithMapsChecker(&mockMapsChecker{lat: 59.93, lon: 30.33}),
		WithBrowserFetch(func(_ context.Context, url string) (string, error) {
			// Render only the /contacts page to the clean DOM. (The homepage is
			// text-rich and contactless, so it may also render; return the clean
			// contacts DOM for the contacts URL, and a thin shell otherwise so the
			// homepage render adds nothing.)
			if strings.Contains(url, "/contacts") {
				return contactsPageCleanRendered, nil
			}
			return homeLinksContacts, nil
		}),
	)
	result, err := e.Enrich(context.Background(), Item{
		Name: "Клуб", URL: srv.URL + "/", City: "Санкт-Петербург", Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if result.Facts.Phone != nil {
		t.Fatalf("Phone = %q, want nil (raw /contacts DNI verdict must survive a clean render — contacts-page poison-OR)", *result.Facts.Phone)
	}
	// Non-phone facts from the clean render may still surface (only phone poisoned).
	if result.Facts.Email == nil || *result.Facts.Email != "info@club.ru" {
		t.Fatalf("Email = %v, want the rendered contacts email (only the phone is poisoned)", derefOrNil(result.Facts.Email))
	}
}

// homeCompleteContacts is a homepage that already carries email + hours +
// address + an ANCHORED (contacts-region) phone — all the RICH fields a
// /contacts page would supply — AND links a /contacts/ page. The FIX-3 perf
// gate (widened in P2 to also require an anchored phone member — see
// homepageMissingRichField) must SKIP discovery: nothing to gain.
const homeCompleteContacts = `<!DOCTYPE html><html lang="ru"><head><title>Кафе</title></head>
<body><article><h1>О кафе</h1>
<p>Уютное кафе в центре города. Мы готовим вкусные блюда из свежих продуктов
каждый день и рады гостям с самого утра до позднего вечера. Большой выбор
напитков, десертов и сезонное меню. Приходите всей семьёй — у нас уютно.</p>
<a href="mailto:hello@cafe.ru">hello@cafe.ru</a>
<address>Невский проспект, 28</address>
<div class="contacts"><a href="tel:+78120000001">+7 (812) 000-00-01</a></div>
<div><span>Часы работы</span><span>Пн-Вс 10:00-22:00</span></div>
<nav><a href="/contacts/">Контакты</a></nav>
</article></body></html>`

// homeCompleteNoAnchoredPhone is homeCompleteContacts WITHOUT any phone at
// all: complete on hours/email/address (the pre-P2 gate), but carrying ZERO
// anchored phone member. The P2-widened FIX-3 gate must NOT skip discovery
// for this homepage — see TestEnrich_ContactsPageGate_NoAnchoredPhoneStillFetches.
const homeCompleteNoAnchoredPhone = `<!DOCTYPE html><html lang="ru"><head><title>Кафе</title></head>
<body><article><h1>О кафе</h1>
<p>Уютное кафе в центре города. Мы готовим вкусные блюда из свежих продуктов
каждый день и рады гостям с самого утра до позднего вечера. Большой выбор
напитков, десертов и сезонное меню. Приходите всей семьёй — у нас уютно.</p>
<a href="mailto:hello@cafe.ru">hello@cafe.ru</a>
<address>Невский проспект, 28</address>
<div><span>Часы работы</span><span>Пн-Вс 10:00-22:00</span></div>
<nav><a href="/contacts/">Контакты</a></nav>
</article></body></html>`

// homePhoneOnlyLinksContacts is a homepage with a phone but NO email/hours/
// address, linking /contacts/. The FIX-3 gate must STILL fetch /contacts (the
// «часы»/email we are after are not on the homepage). NOT a blunt !hasContactFacts.
const homePhoneOnlyLinksContacts = `<!DOCTYPE html><html lang="ru"><head><title>Кафе</title></head>
<body><article><h1>О кафе</h1>
<p>Уютное кафе в центре города. Мы готовим вкусные блюда из свежих продуктов
каждый день и рады гостям с самого утра до позднего вечера. Большой выбор
напитков, десертов и сезонное меню. Приходите всей семьёй — у нас уютно.</p>
<a href="tel:+78120000000">+7 (812) 000-00-00</a>
<nav><a href="/contacts/">Контакты</a></nav>
</article></body></html>`

// TestEnrich_ContactsPageGate_CompleteHomepageSkipsFetch verifies the FIX-3 perf
// gate: a homepage already carrying hours+email+address never discovers/fetches
// the /contacts page (no round-trip to re-supply what we already have).
func TestEnrich_ContactsPageGate_CompleteHomepageSkipsFetch(t *testing.T) {
	t.Parallel()
	srv := newMultiPathServer(map[string]string{
		"/":          homeCompleteContacts,
		"/contacts/": contactsPageRich,
	})
	defer srv.Close()

	var discovered int
	e := newTestEnricher(
		WithFetcher(testFetcher()),
		// Guard-B (checkTarget) defaults to the real httputil.CheckRawURL (go-kit), which
		// refuses a loopback target — allow it here since contactsURL points at
		// the local httptest server in these tests (see allowAllTargets).
		WithTargetGuard(allowAllTargets),
		WithMapsChecker(&mockMapsChecker{lat: 59.93, lon: 30.33}),
		WithMetrics(&Metrics{OnContactsPageDiscovered: func() { discovered++ }}),
	)
	if _, err := e.Enrich(context.Background(), Item{
		Name: "Кафе", URL: srv.URL + "/", City: "Санкт-Петербург", Mode: ModePlaces,
	}); err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if discovered != 0 {
		t.Fatalf("OnContactsPageDiscovered = %d, want 0 (complete homepage must skip the contacts fetch)", discovered)
	}
}

// TestEnrich_ContactsPageGate_NoAnchoredPhoneStillFetches is the P2 golden:
// a homepage that is "complete" under the PRE-P2 gate (hours+email+address
// all present) but carries ZERO anchored phone member must still fetch
// /contacts — a phone-only-on-/contacts venue must not be silently skipped
// just because the OTHER rich fields happen to already be on the homepage.
// The contacts-page number must surface both in Facts.Phone and in the
// additive Result.SiteNumbers set.
func TestEnrich_ContactsPageGate_NoAnchoredPhoneStillFetches(t *testing.T) {
	t.Parallel()
	srv := newMultiPathServer(map[string]string{
		"/":          homeCompleteNoAnchoredPhone,
		"/contacts/": contactsPageRich, // carries the +7 (812) 439-11-00 anchored tel:
	})
	defer srv.Close()

	var discovered, resolved int
	e := newTestEnricher(
		WithFetcher(testFetcher()),
		// Guard-B (checkTarget) defaults to the real httputil.CheckRawURL (go-kit), which
		// refuses a loopback target — allow it here since contactsURL points at
		// the local httptest server in these tests (see allowAllTargets).
		WithTargetGuard(allowAllTargets),
		WithMapsChecker(&mockMapsChecker{lat: 59.93, lon: 30.33}),
		WithMetrics(&Metrics{
			OnContactsPageDiscovered: func() { discovered++ },
			OnContactsPageResolved:   func() { resolved++ },
		}),
	)
	result, err := e.Enrich(context.Background(), Item{
		Name: "Кафе", URL: srv.URL + "/", City: "Санкт-Петербург", Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if discovered != 1 {
		t.Fatalf("OnContactsPageDiscovered = %d, want 1 (a homepage with zero anchored phone member must still fetch /contacts)", discovered)
	}
	if resolved != 1 {
		t.Fatalf("OnContactsPageResolved = %d, want 1", resolved)
	}
	if result.Facts.Phone == nil || !strings.Contains(*result.Facts.Phone, "439-11-00") {
		t.Fatalf("Phone = %v, want the contacts-page 812 phone", derefOrNil(result.Facts.Phone))
	}
	found := false
	for _, n := range result.SiteNumbers {
		if strings.Contains(n.Value, "439-11-00") {
			found = true
			if !n.Anchored || !n.Trustworthy {
				t.Errorf("SiteNumbers entry for 439-11-00 = %+v, want Anchored=true Trustworthy=true", n)
			}
		}
	}
	if !found {
		t.Fatalf("SiteNumbers missing the contacts-page 439-11-00 candidate; got %+v", result.SiteNumbers)
	}
}

// TestEnrich_ContactsPageGate_PhoneOnlyHomepageStillFetches verifies the FIX-3
// gate is NOT a blunt !hasContactFacts: a homepage that has only a phone is
// still missing hours/email/address, so it must keep fetching /contacts.
func TestEnrich_ContactsPageGate_PhoneOnlyHomepageStillFetches(t *testing.T) {
	t.Parallel()
	srv := newMultiPathServer(map[string]string{
		"/":          homePhoneOnlyLinksContacts,
		"/contacts/": contactsPageRich,
	})
	defer srv.Close()

	var discovered, resolved int
	e := newTestEnricher(
		WithFetcher(testFetcher()),
		// Guard-B (checkTarget) defaults to the real httputil.CheckRawURL (go-kit), which
		// refuses a loopback target — allow it here since contactsURL points at
		// the local httptest server in these tests (see allowAllTargets).
		WithTargetGuard(allowAllTargets),
		WithMapsChecker(&mockMapsChecker{lat: 59.93, lon: 30.33}),
		WithMetrics(&Metrics{
			OnContactsPageDiscovered: func() { discovered++ },
			OnContactsPageResolved:   func() { resolved++ },
		}),
	)
	result, err := e.Enrich(context.Background(), Item{
		Name: "Кафе", URL: srv.URL + "/", City: "Санкт-Петербург", Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if discovered != 1 {
		t.Fatalf("OnContactsPageDiscovered = %d, want 1 (phone-only homepage must still fetch /contacts for hours/email)", discovered)
	}
	if resolved != 1 {
		t.Fatalf("OnContactsPageResolved = %d, want 1", resolved)
	}
	// The «часы» goal: the contacts page hours must surface for a phone-only homepage.
	if result.Facts.Hours == nil || !strings.Contains(*result.Facts.Hours, "10:00") {
		t.Fatalf("Hours = %v, want the contacts-page hours (the «часы» goal for a phone-only homepage)", derefOrNil(result.Facts.Hours))
	}
}

// TestEnrich_ContactsPage_EqualSourceOverwrite verifies the NIT coverage case:
// when the homepage and /contacts page both supply the SAME field at the SAME
// source (official_site), the contacts-page value wins (later equal-source pass)
// and the provenance source stays official_site.
func TestEnrich_ContactsPage_EqualSourceOverwrite(t *testing.T) {
	t.Parallel()
	// Homepage email A; contacts page email B (same field, both official_site).
	homeEmailA := strings.Replace(homeLinksContacts,
		`<nav><a href="/contacts/">Контакты</a></nav>`,
		`<a href="mailto:old@fabrika.ru">old@fabrika.ru</a><nav><a href="/contacts/">Контакты</a></nav>`, 1)
	srv := newMultiPathServer(map[string]string{
		"/":          homeEmailA,
		"/contacts/": contactsPageRich, // carries salon@fabrika.ru
	})
	defer srv.Close()

	e := newTestEnricher(
		WithFetcher(testFetcher()),
		// Guard-B (checkTarget) defaults to the real httputil.CheckRawURL (go-kit), which
		// refuses a loopback target — allow it here since contactsURL points at
		// the local httptest server in these tests (see allowAllTargets).
		WithTargetGuard(allowAllTargets),
		WithMapsChecker(&mockMapsChecker{lat: 59.93, lon: 30.33}),
	)
	result, err := e.Enrich(context.Background(), Item{
		Name: "Фабрика", URL: srv.URL + "/", City: "Санкт-Петербург", Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if result.Facts.Email == nil || *result.Facts.Email != "salon@fabrika.ru" {
		t.Fatalf("Email = %v, want the contacts-page email salon@fabrika.ru (equal-source later pass wins)", derefOrNil(result.Facts.Email))
	}
	if got := result.Provenance.Email.Source; got != "official_site" {
		t.Fatalf("Email provenance source = %q, want official_site (equal-source overwrite keeps source)", got)
	}
}

// contactsPageShorterAddress is a /contacts page whose address is a LESS-precise
// version of the homepage's. The advisory longer-address-wins rule must keep the
// more-complete homepage address on an equal-source (official_site) overwrite.
const contactsPageShorterAddress = `<!DOCTYPE html><html lang="ru"><head><title>Контакты</title></head>
<body><div class="contacts">
<a href="mailto:salon@fabrika.ru">salon@fabrika.ru</a>
<address>Невский проспект, 28</address>
<div><span>Часы работы</span><span>Пн-Пт 10:00-21:00</span></div>
</div></body></html>`

// TestEnrich_ContactsPage_LongerAddressWinsEqualSource verifies the advisory:
// a homepage carrying a more-complete address must NOT be clobbered to a bare
// less-precise /contacts address at equal official_site source.
func TestEnrich_ContactsPage_LongerAddressWinsEqualSource(t *testing.T) {
	t.Parallel()
	// Homepage with the FULL address; contacts page has the bare one.
	homeFullAddr := strings.Replace(homeLinksContacts,
		`<nav><a href="/contacts/">Контакты</a></nav>`,
		`<address>Невский проспект, 28, корпус 2, офис 5</address><nav><a href="/contacts/">Контакты</a></nav>`, 1)
	srv := newMultiPathServer(map[string]string{
		"/":          homeFullAddr,
		"/contacts/": contactsPageShorterAddress,
	})
	defer srv.Close()

	e := newTestEnricher(
		WithFetcher(testFetcher()),
		// Guard-B (checkTarget) defaults to the real httputil.CheckRawURL (go-kit), which
		// refuses a loopback target — allow it here since contactsURL points at
		// the local httptest server in these tests (see allowAllTargets).
		WithTargetGuard(allowAllTargets),
		WithMapsChecker(&mockMapsChecker{lat: 59.93, lon: 30.33}),
	)
	result, err := e.Enrich(context.Background(), Item{
		Name: "Фабрика", URL: srv.URL + "/", City: "Санкт-Петербург", Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if result.Facts.Address == nil || !strings.Contains(*result.Facts.Address, "корпус 2") {
		t.Fatalf("Address = %v, want the more-complete homepage address (longer-wins on equal source)", derefOrNil(result.Facts.Address))
	}
	// The contacts page still wins on OTHER fields it is richer on (email/hours).
	if result.Facts.Email == nil || *result.Facts.Email != "salon@fabrika.ru" {
		t.Fatalf("Email = %v, want the contacts-page email (still adopted)", derefOrNil(result.Facts.Email))
	}
}

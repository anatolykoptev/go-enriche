package enriche

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/anatolykoptev/go-enriche/extract"
)

// renderSpy is a browserFetch delegate that RECORDS whether (and for which
// URLs) the headless render was actually invoked — so a render-skip golden can
// assert on render-HAPPENED, not merely on the merged output. body is what a
// (non-skipped) render returns.
type renderSpy struct {
	mu   sync.Mutex
	urls []string
	body string
}

func (s *renderSpy) fetch(_ context.Context, url string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.urls = append(s.urls, url)
	return s.body, nil
}

func (s *renderSpy) called() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.urls) > 0
}

func (s *renderSpy) calledForPath(path string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.urls {
		if strings.Contains(u, path) {
			return true
		}
	}
	return false
}

func findSiteNumber(nums []extract.PhoneNumberFact, substr string) *extract.PhoneNumberFact {
	for i := range nums {
		if strings.Contains(nums[i].Value, substr) {
			return &nums[i]
		}
	}
	return nil
}

// --- Fixtures (mcmedok-class: the real SPb branch phone lives ONLY in an
// inline-script branch-locator JSON, invisible to the single-winner Facts.Phone
// and thus to hasContactFacts, so the OLD gate rendered needlessly). ---

// rsHomeBranchJSONAnchored is a text-rich homepage whose ONLY phone signal is a
// branch-JSON anchored SPb number (Facts carries NO contact fact); no DNI
// vendor. The old gate rendered (absent_contacts); the trust gate must SKIP.
const rsHomeBranchJSONAnchored = `<!DOCTYPE html><html lang="ru"><head><title>Клиника</title>
<script>var marker_data = '[{"phone":"+7 (812) 767-36-61"}]';</script></head>
<body><article><h1>О клинике</h1>
<p>Наша клиника работает в городе уже более пятнадцати лет и объединяет опытных
специалистов самых разных направлений. Мы бережно относимся к каждому пациенту,
применяем современное оборудование и постоянно расширяем спектр услуг для всей
семьи, чтобы забота о здоровье была удобной и доступной каждый день недели.</p>
</article></body></html>`

// rsHomeBranchJSONPoisoned is rsHomeBranchJSONAnchored PLUS an active (listed)
// Mango DNI vendor: homeRawPoisoned=true, so the anchored number is NOT
// trustworthy and the render must still fire (fail-closed).
const rsHomeBranchJSONPoisoned = `<!DOCTYPE html><html lang="ru"><head><title>Клиника</title>
<script src="https://widgets.mango-office.ru/widgets/mango.js"></script>
<script>var marker_data = '[{"phone":"+7 (812) 767-36-61"}]';</script></head>
<body><article><h1>О клинике</h1>
<p>Наша клиника работает в городе уже более пятнадцати лет и объединяет опытных
специалистов самых разных направлений. Мы бережно относимся к каждому пациенту,
применяем современное оборудование и постоянно расширяем спектр услуг для всей
семьи, чтобы забота о здоровье была удобной и доступной каждый день недели.</p>
</article></body></html>`

// rsHomeBranchJSONUnlistedDNI is rsHomeBranchJSONAnchored PLUS an UNLISTED
// call-tracking vendor (absent from detectDNIVendor's signatures), so
// homeRawPoisoned stays false and the number surfaces Trustworthy — the
// documented residual gap (Golden D).
const rsHomeBranchJSONUnlistedDNI = `<!DOCTYPE html><html lang="ru"><head><title>Клиника</title>
<script src="https://cdn.tracktastic.io/dni.js"></script>
<script>window.tracktasticInit({id:42});var marker_data = '[{"phone":"+7 (812) 767-36-61"}]';</script></head>
<body><article><h1>О клинике</h1>
<p>Наша клиника работает в городе уже более пятнадцати лет и объединяет опытных
специалистов самых разных направлений. Мы бережно относимся к каждому пациенту,
применяем современное оборудование и постоянно расширяем спектр услуг для всей
семьи, чтобы забота о здоровье была удобной и доступной каждый день недели.</p>
</article></body></html>`

// rsHomeWithPhoneLinksContacts is a homepage that ALREADY carries a (valid,
// anchored) phone — so it never renders — but is missing hours/email/address,
// so contacts-subpage discovery fires. Used to isolate the CONTACTS render leg.
const rsHomeWithPhoneLinksContacts = `<!DOCTYPE html><html lang="ru"><head><title>Сеть клиник</title></head>
<body><article><h1>О сети</h1>
<p>Сеть клиник объединяет несколько филиалов и предлагает широкий спектр услуг
для пациентов всех возрастов. Опытные врачи, современное оборудование и удобное
расположение филиалов помогают заботиться о здоровье всей семьи каждый день.</p>
<div class="contacts"><a href="tel:+78123334455">+7 (812) 333-44-55</a></div>
<nav><a href="/contacts/">Контакты</a></nav>
</article></body></html>`

// rsContactsBranchJSONAnchored is a /contacts subpage whose only phone is a
// branch-JSON anchored SPb number, no Facts contact, no DNI — the contacts leg
// must SKIP the render.
const rsContactsBranchJSONAnchored = `<!DOCTYPE html><html lang="ru"><head><title>Контакты</title>
<script>var marker_data = '[{"phone":"+7 (812) 767-36-61"}]';</script></head>
<body><article><p>Мы всегда рады помочь вам с выбором услуги и ответить на любые
вопросы о работе наших филиалов, ценах и специалистах. Свяжитесь удобным способом,
и мы подберём для вас подходящее время визита и нужного специалиста.</p></article>
</body></html>`

// rsContactsNoAnchored is a contactless /contacts subpage with NO anchored
// number — the contacts leg must still render.
const rsContactsNoAnchored = `<!DOCTYPE html><html lang="ru"><head><title>Контакты</title></head>
<body><article><p>Свяжитесь с нами удобным способом — мы всегда рады помочь с выбором
услуги, расскажем о ценах и специалистах и подберём подходящее время для визита в
любой из наших филиалов, где вас встретят внимательные администраторы.</p>
<div id="c-root"><!-- contacts injected by JS --></div></article></body></html>`

// rsContactsBranchJSONPoisoned is rsContactsBranchJSONAnchored PLUS a listed
// Mango DNI vendor — the contacts leg must still render (fail-closed).
const rsContactsBranchJSONPoisoned = `<!DOCTYPE html><html lang="ru"><head><title>Контакты</title>
<script src="https://widgets.mango-office.ru/widgets/mango.js"></script>
<script>var marker_data = '[{"phone":"+7 (812) 767-36-61"}]';</script></head>
<body><article><p>Мы всегда рады помочь вам с выбором услуги и ответить на любые
вопросы о работе наших филиалов, ценах и специалистах. Свяжитесь удобным способом,
и мы подберём для вас подходящее время визита и нужного специалиста.</p></article>
</body></html>`

// --- Golden A (homepage): the win — a mcmedok-class page skips the render. ---
func TestEnrich_RenderSkip_HomepageAnchoredRawSufficient_SkipsRender(t *testing.T) {
	t.Parallel()
	srv := newTestServer(rsHomeBranchJSONAnchored, 200)
	defer srv.Close()

	spy := &renderSpy{body: rsHomeBranchJSONAnchored}
	var skips [][2]string
	e := newTestEnricher(
		WithFetcher(testFetcher()),
		WithMapsChecker(&mockMapsChecker{lat: 59.93, lon: 30.33}),
		WithBrowserFetch(spy.fetch),
		WithMetrics(&Metrics{
			OnBrowserRenderSkipped: func(leg, reason string) { skips = append(skips, [2]string{leg, reason}) },
		}),
	)
	result, err := e.Enrich(context.Background(), Item{
		Name: "Клиника", URL: srv.URL, City: "Санкт-Петербург", Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if spy.called() {
		t.Fatalf("browser render fired (%v) — a trustworthy anchored raw SiteNumber must SKIP the render", spy.urls)
	}
	if len(skips) != 1 || skips[0] != [2]string{"homepage", "raw_sufficient"} {
		t.Fatalf("OnBrowserRenderSkipped = %v, want exactly [(homepage, raw_sufficient)]", skips)
	}
	if !result.RenderSkipped {
		t.Fatal("Result.RenderSkipped = false, want true (the render was trust-skipped)")
	}
	n := findSiteNumber(result.SiteNumbers, "767-36-61")
	if n == nil {
		t.Fatalf("SiteNumbers missing the branch-JSON 812 number the skip preserved; got %+v", result.SiteNumbers)
	}
	if !n.Anchored || !n.Trustworthy || n.Source != "branch_json" {
		t.Fatalf("preserved number = %+v, want Anchored+Trustworthy+branch_json", *n)
	}
}

// --- Golden B (homepage): a page with NO anchored raw number still renders. ---
func TestEnrich_RenderSkip_HomepageNoAnchored_StillRenders(t *testing.T) {
	t.Parallel()
	srv := newTestServer(richTextNoContacts, 200)
	defer srv.Close()

	spy := &renderSpy{body: renderedWithContacts}
	var skips int
	e := newTestEnricher(
		WithFetcher(testFetcher()),
		WithMapsChecker(&mockMapsChecker{lat: 59.93, lon: 30.33}),
		WithBrowserFetch(spy.fetch),
		WithMetrics(&Metrics{OnBrowserRenderSkipped: func(_, _ string) { skips++ }}),
	)
	result, err := e.Enrich(context.Background(), Item{
		Name: "Студия", URL: srv.URL, City: "Санкт-Петербург", Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if !spy.called() {
		t.Fatal("render did NOT fire — a page with NO anchored raw number must still render (skip must not starve it)")
	}
	if skips != 0 {
		t.Fatalf("OnBrowserRenderSkipped fired %d times, want 0 (a render actually happened)", skips)
	}
	if result.RenderSkipped {
		t.Fatal("Result.RenderSkipped = true, want false (a render happened)")
	}
}

// --- Golden C (homepage): DNI-poisoned + anchored still renders (fail-closed). ---
func TestEnrich_RenderSkip_HomepagePoisonedAnchored_StillRenders(t *testing.T) {
	t.Parallel()
	srv := newTestServer(rsHomeBranchJSONPoisoned, 200)
	defer srv.Close()

	spy := &renderSpy{body: rsHomeBranchJSONPoisoned}
	var skips int
	e := newTestEnricher(
		WithFetcher(testFetcher()),
		WithMapsChecker(&mockMapsChecker{lat: 59.93, lon: 30.33}),
		WithBrowserFetch(spy.fetch),
		WithMetrics(&Metrics{OnBrowserRenderSkipped: func(_, _ string) { skips++ }}),
	)
	result, err := e.Enrich(context.Background(), Item{
		Name: "Клиника", URL: srv.URL, City: "Санкт-Петербург", Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if !spy.called() {
		t.Fatal("render did NOT fire on a DNI-poisoned page — the skip must NEVER fire when poisoned (fail-closed)")
	}
	if skips != 0 {
		t.Fatalf("OnBrowserRenderSkipped fired %d times on a poisoned page, want 0", skips)
	}
	if result.RenderSkipped {
		t.Fatal("Result.RenderSkipped = true on a poisoned page, want false")
	}
	n := findSiteNumber(result.SiteNumbers, "767-36-61")
	if n == nil {
		t.Fatalf("SiteNumbers missing the branch-JSON number; got %+v", result.SiteNumbers)
	}
	if !n.DNI || n.Trustworthy {
		t.Fatalf("poisoned number = %+v, want DNI=true Trustworthy=false (poison protection holds)", *n)
	}
}

// --- Golden D (homepage): the DOCUMENTED residual gap — an UNLISTED DNI vendor
// with an anchored number leaves the page un-poisoned, so the render is skipped
// and the number surfaces Trustworthy. NOT caught today by design; backstopped
// in prod by the Phase-1.5 canary re-render sampler + go-wp Correctable gate.
// This pins the CURRENT (gap) behavior so a future change to it is deliberate. ---
func TestEnrich_RenderSkip_HomepageUnlistedDNIAnchored_DocumentsResidualGap(t *testing.T) {
	t.Parallel()
	srv := newTestServer(rsHomeBranchJSONUnlistedDNI, 200)
	defer srv.Close()

	spy := &renderSpy{body: rsHomeBranchJSONUnlistedDNI}
	e := newTestEnricher(
		WithFetcher(testFetcher()),
		WithMapsChecker(&mockMapsChecker{lat: 59.93, lon: 30.33}),
		WithBrowserFetch(spy.fetch),
	)
	result, err := e.Enrich(context.Background(), Item{
		Name: "Клиника", URL: srv.URL, City: "Санкт-Петербург", Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if spy.called() {
		t.Fatalf("render fired (%v); an unlisted DNI vendor is NOT detected, so the skip DOES fire today (the gap)", spy.urls)
	}
	if !result.RenderSkipped {
		t.Fatal("Result.RenderSkipped = false, want true (the render was skipped — the residual gap)")
	}
	n := findSiteNumber(result.SiteNumbers, "767-36-61")
	if n == nil {
		t.Fatalf("SiteNumbers missing the branch-JSON number; got %+v", result.SiteNumbers)
	}
	if n.DNI || !n.Trustworthy {
		t.Fatalf("number = %+v; today an UNLISTED vendor leaves it DNI=false Trustworthy=true (the documented gap)", *n)
	}
}

// --- Contacts-leg variant A: the /contacts subpage skips the render. ---
func TestEnrich_RenderSkip_ContactsAnchoredRawSufficient_SkipsRender(t *testing.T) {
	t.Parallel()
	srv := newMultiPathServer(map[string]string{
		"/":          rsHomeWithPhoneLinksContacts,
		"/contacts/": rsContactsBranchJSONAnchored,
	})
	defer srv.Close()

	spy := &renderSpy{body: rsContactsBranchJSONAnchored}
	var skips [][2]string
	e := newTestEnricher(
		WithFetcher(testFetcher()),
		WithTargetGuard(allowAllTargets),
		WithMapsChecker(&mockMapsChecker{lat: 59.93, lon: 30.33}),
		WithBrowserFetch(spy.fetch),
		WithMetrics(&Metrics{
			OnBrowserRenderSkipped: func(leg, reason string) { skips = append(skips, [2]string{leg, reason}) },
		}),
	)
	result, err := e.Enrich(context.Background(), Item{
		Name: "Сеть клиник", URL: srv.URL + "/", City: "Санкт-Петербург", Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if spy.called() {
		t.Fatalf("render fired (%v) — the contacts page's trustworthy anchored raw number must SKIP the render", spy.urls)
	}
	if len(skips) != 1 || skips[0] != [2]string{"contacts", "raw_sufficient"} {
		t.Fatalf("OnBrowserRenderSkipped = %v, want exactly [(contacts, raw_sufficient)]", skips)
	}
	if !result.RenderSkipped {
		t.Fatal("Result.RenderSkipped = false, want true (contacts render trust-skipped)")
	}
	if findSiteNumber(result.SiteNumbers, "767-36-61") == nil {
		t.Fatalf("SiteNumbers missing the contacts-page branch-JSON number; got %+v", result.SiteNumbers)
	}
}

// --- Contacts-leg variant B: a contactless /contacts with no anchored number
// still renders. ---
func TestEnrich_RenderSkip_ContactsNoAnchored_StillRenders(t *testing.T) {
	t.Parallel()
	srv := newMultiPathServer(map[string]string{
		"/":          rsHomeWithPhoneLinksContacts,
		"/contacts/": rsContactsNoAnchored,
	})
	defer srv.Close()

	spy := &renderSpy{body: contactsPageRich}
	var skips int
	e := newTestEnricher(
		WithFetcher(testFetcher()),
		WithTargetGuard(allowAllTargets),
		WithMapsChecker(&mockMapsChecker{lat: 59.93, lon: 30.33}),
		WithBrowserFetch(spy.fetch),
		WithMetrics(&Metrics{OnBrowserRenderSkipped: func(_, _ string) { skips++ }}),
	)
	if _, err := e.Enrich(context.Background(), Item{
		Name: "Сеть клиник", URL: srv.URL + "/", City: "Санкт-Петербург", Mode: ModePlaces,
	}); err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if !spy.calledForPath("/contacts/") {
		t.Fatalf("contacts render did NOT fire (%v) — a contactless /contacts page with no anchored number must still render", spy.urls)
	}
	if skips != 0 {
		t.Fatalf("OnBrowserRenderSkipped fired %d times, want 0 (contacts render happened)", skips)
	}
}

// --- Contacts-leg variant C: a DNI-poisoned /contacts with an anchored number
// still renders (fail-closed). ---
func TestEnrich_RenderSkip_ContactsPoisonedAnchored_StillRenders(t *testing.T) {
	t.Parallel()
	srv := newMultiPathServer(map[string]string{
		"/":          rsHomeWithPhoneLinksContacts,
		"/contacts/": rsContactsBranchJSONPoisoned,
	})
	defer srv.Close()

	spy := &renderSpy{body: rsContactsBranchJSONPoisoned}
	var skips int
	e := newTestEnricher(
		WithFetcher(testFetcher()),
		WithTargetGuard(allowAllTargets),
		WithMapsChecker(&mockMapsChecker{lat: 59.93, lon: 30.33}),
		WithBrowserFetch(spy.fetch),
		WithMetrics(&Metrics{OnBrowserRenderSkipped: func(_, _ string) { skips++ }}),
	)
	result, err := e.Enrich(context.Background(), Item{
		Name: "Сеть клиник", URL: srv.URL + "/", City: "Санкт-Петербург", Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if !spy.calledForPath("/contacts/") {
		t.Fatalf("contacts render did NOT fire (%v) — a DNI-poisoned contacts page must always render (fail-closed)", spy.urls)
	}
	if skips != 0 {
		t.Fatalf("OnBrowserRenderSkipped fired %d times on a poisoned contacts page, want 0", skips)
	}
	n := findSiteNumber(result.SiteNumbers, "767-36-61")
	if n == nil {
		t.Fatalf("SiteNumbers missing the contacts branch-JSON number; got %+v", result.SiteNumbers)
	}
	if !n.DNI || n.Trustworthy {
		t.Fatalf("poisoned contacts number = %+v, want DNI=true Trustworthy=false", *n)
	}
}

// --- HIGH#1 homepage degrade: a render ATTEMPTED-BUT-FAILED rests on raw-only,
// so RenderSkipped must be TRUE even though the render was tried (not skipped).
// A NON-poisoned rich page renders (absent_contacts), the render returns a
// sub-minRenderShellBytes shell → degrade to raw → RenderSkipped=true. ---
func TestEnrich_RenderSkip_HomepageRenderError_MarksRawOnly(t *testing.T) {
	t.Parallel()
	srv := newTestServer(richTextNoContacts, 200)
	defer srv.Close()

	var renderErrors int
	e := newTestEnricher(
		WithFetcher(testFetcher()),
		WithMapsChecker(&mockMapsChecker{lat: 59.93, lon: 30.33}),
		WithBrowserFetch(func(_ context.Context, _ string) (string, error) {
			return strings.Repeat("x", 200), nil // sub-minRenderShellBytes shell → degrade
		}),
		WithMetrics(&Metrics{OnBrowserRenderError: func() { renderErrors++ }}),
	)
	result, err := e.Enrich(context.Background(), Item{
		Name: "Студия", URL: srv.URL, City: "Санкт-Петербург", Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if renderErrors < 1 {
		t.Fatalf("OnBrowserRenderError = %d, want >=1 (the render was attempted and failed)", renderErrors)
	}
	if !result.RenderSkipped {
		t.Fatal("RenderSkipped = false, want true (render-error degrade rests on raw-only, must be marked)")
	}
}

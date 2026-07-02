package enriche

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-enriche/extract"
)

// nationalChainContactsHTML models the Эксимер-style national-chain page: one
// official site listing branch phone numbers for several cities (only ONE of
// which — 812 — is seeded in extract's cityAreaCodes; 383/831/863 are
// deliberately NOT seeded), plus a mobile line and an 8-800 toll-free line.
// All candidates sit in the contacts region so every one is Anchored — the
// city-membership tag is orthogonal to the existing tier/trust machinery.
const nationalChainContactsHTML = `<!DOCTYPE html><html lang="ru"><head><title>Клиника</title></head>
<body><div class="contacts">
<a href="tel:+78121111111">+7 (812) 111-11-11</a>
<a href="tel:+74952222222">+7 (495) 222-22-22</a>
<a href="tel:+73833333333">+7 (383) 333-33-33</a>
<a href="tel:+78314444444">+7 (831) 444-44-44</a>
<a href="tel:+78635555555">+7 (863) 555-55-55</a>
<a href="tel:+79210006677">+7 (921) 000-66-77</a>
<a href="tel:+78005553535">8 (800) 555-35-35</a>
</div></body></html>`

// siteNumberByValue finds the SiteNumbers entry whose Value contains needle,
// failing the test if it's missing (every case names an unambiguous trailing
// digit group, e.g. "111-11-11").
func siteNumberByValue(t *testing.T, nums []extract.PhoneNumberFact, needle string) extract.PhoneNumberFact {
	t.Helper()
	for _, n := range nums {
		if strings.Contains(n.Value, needle) {
			return n
		}
	}
	t.Fatalf("SiteNumbers missing entry containing %q; got %+v", needle, nums)
	return extract.PhoneNumberFact{}
}

// TestEnrich_SiteNumbers_CityMembership_NationalChain is the headline P1
// end-to-end case: resolve.go's addSiteNumbers tags each accumulated
// PhoneNumberFact via extract.ClassifyCityMembership, reached through the
// real Enrich() orchestration (not a hand-called helper). Under
// Item.City="Санкт-Петербург", the 812 branch must be CityMatch=true; the
// Moscow (seeded) AND the Новосибирск/Нижний Новгород/Ростов (UNSEEDED)
// branches must all be CityForeign=true — proving seed-independence end to
// end. The mobile and 8-800 lines must be neutral (both flags false).
func TestEnrich_SiteNumbers_CityMembership_NationalChain(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(nationalChainContactsHTML)) //nolint:errcheck
	}))
	defer srv.Close()

	e := newTestEnricher(WithFetcher(testFetcher()))
	result, err := e.Enrich(context.Background(), Item{
		Name: "Клиника", URL: srv.URL, City: "Санкт-Петербург", Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}

	cases := []struct {
		name        string
		needle      string
		wantMatch   bool
		wantForeign bool
	}{
		{"812 spb branch — local match", "111-11-11", true, false},
		{"495 moscow branch — seeded foreign", "222-22-22", false, true},
		{"383 novosibirsk branch — UNSEEDED foreign", "333-33-33", false, true},
		{"831 nizhny novgorod branch — UNSEEDED foreign", "444-44-44", false, true},
		{"863 rostov branch — UNSEEDED foreign", "555-55-55", false, true},
		{"921 mobile — neutral", "000-66-77", false, false},
		{"800 toll-free — neutral", "555-35-35", false, false},
	}
	for _, tc := range cases {
		n := siteNumberByValue(t, result.SiteNumbers, tc.needle)
		if n.CityMatch != tc.wantMatch || n.CityForeign != tc.wantForeign {
			t.Errorf("%s: %q -> CityMatch=%v CityForeign=%v, want CityMatch=%v CityForeign=%v",
				tc.name, n.Value, n.CityMatch, n.CityForeign, tc.wantMatch, tc.wantForeign)
		}
	}
}

// TestEnrich_SiteNumbers_CityMembership_EmptyCity_Neutral proves the
// byte-identical no-op: when Item.City is unset (hully / non-RU / any
// project with no configured city), EVERY SiteNumbers candidate — including
// the geographic-landline branches — must tag neutral (CityMatch=false,
// CityForeign=false), matching pre-city-membership behavior exactly.
func TestEnrich_SiteNumbers_CityMembership_EmptyCity_Neutral(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(nationalChainContactsHTML)) //nolint:errcheck
	}))
	defer srv.Close()

	e := newTestEnricher(WithFetcher(testFetcher()))
	result, err := e.Enrich(context.Background(), Item{
		Name: "Клиника", URL: srv.URL, City: "", Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if len(result.SiteNumbers) == 0 {
		t.Fatal("SiteNumbers is empty, want the 7 fixture candidates")
	}
	for _, n := range result.SiteNumbers {
		if n.CityMatch || n.CityForeign {
			t.Errorf("empty Item.City: %q -> CityMatch=%v CityForeign=%v, want (false, false) for every candidate",
				n.Value, n.CityMatch, n.CityForeign)
		}
	}
}

// TestEnrich_SiteNumbers_CityMembership_SurvivesDedup locks addSiteNumbers'
// own doc-comment invariant end-to-end (resolve.go): the SAME phone number
// is tagged at TWO different tiers across two addSiteNumbers calls — a
// weak, unanchored homepage body tel: (homeWeakPhoneLinksContacts) then a
// stronger, anchored+Trustworthy /contacts-page reading of the IDENTICAL
// digits (contactsPageStrongSamePhone, both defined in
// enriche_contacts_test.go and reused here for the exact same fixture
// TestEnrich_SiteNumbersSnapshot_HigherRankReadingWins already exercises
// for Anchored/Trustworthy). DedupeKeepStronger keeps the stronger
// occurrence; its CityMatch tag — computed on EACH occurrence before the
// merge, since ClassifyCityMembership is a pure function of the digits —
// must survive intact on the winner, not silently reset to the
// merge-losing occurrence's (zero-value) tag or dropped.
func TestEnrich_SiteNumbers_CityMembership_SurvivesDedup(t *testing.T) {
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

	n := siteNumberByValue(t, result.SiteNumbers, "000-11-22")
	if !n.Anchored || !n.Trustworthy {
		t.Fatalf("SiteNumbers entry for 000-11-22 = %+v, want the STRONGER contacts-page reading (Anchored=true Trustworthy=true) to survive the dedup", n)
	}
	if !n.CityMatch || n.CityForeign {
		t.Errorf("SiteNumbers entry for 000-11-22 = %+v, want CityMatch=true CityForeign=false to survive on the dedup winner (812 is local to Санкт-Петербург)", n)
	}
}

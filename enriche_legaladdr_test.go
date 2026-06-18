package enriche

import (
	"context"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-enriche/fetch"
	"github.com/anatolykoptev/go-enriche/maps"
)

// mockMapsCheckerAddr returns a maps card carrying a VENUE (geo) address — the
// geo-correct visiting address whose authority the venue Address slot must hold.
// It models the 2GIS/Yandex card a city-guide venue resolves to.
type mockMapsCheckerAddr struct {
	name string
	addr string
	lat  float64
	lon  float64
}

func (m *mockMapsCheckerAddr) Check(_ context.Context, _, _, _ string) (*maps.CheckResult, error) {
	return &maps.CheckResult{
		Status: maps.PlaceOpen,
		OrgData: &maps.OrgData{
			Name:      m.name,
			Address:   m.addr,
			Latitude:  m.lat,
			Longitude: m.lon,
		},
	}, nil
}

// homeLinksContactsNoAddr is a homepage with enough text to pass minExtractChars
// and NO contact facts (no phone/address/hours/email) — it only links /contacts/.
// (The homepage MUST be address-less so the contacts-page fetch fires: the
// homepage-complete perf gate skips the second fetch when address+hours+email are
// all already present.)
const homeLinksContactsNoAddr = `<!DOCTYPE html><html lang="ru"><head><title>Игора Драйв</title></head>
<body><article><h1>Игора Драйв</h1>
<p>Современный автоспортивный комплекс с трассами для картинга, дрифта и
кольцевых гонок. У нас регулярно проходят соревнования и корпоративные
мероприятия. Опытные инструкторы проведут обучение для любого уровня подготовки.</p>
<nav><a href="/contacts/">Контакты</a></nav>
</article></body></html>`

// contactsPageLegalOnlyWithFields is the /contacts subpage that carries a legal
// registered seat (В.О. линия, литера/помещение, ИНН) — but NO venue visiting
// address — plus an email and hours. This is the live drive-igora shape: the
// extractor picked the legal seat for the address slot while the venue's geo
// address came from the maps card. Before the field split the legal seat
// (official_site/high) overwrote the maps venue address (maps/low) and the card's
// map link pointed at the city-center office instead of the venue.
const contactsPageLegalOnlyWithFields = `<!DOCTYPE html><html lang="ru"><head><title>Контакты</title></head>
<body><div class="contacts">
<address>11-я В.О. линия, дом № 38, литера А, помещение 91, Санкт-Петербург, 199178</address>
<a href="mailto:info@drive-igora.ru">info@drive-igora.ru</a>
<div><span>Режим работы</span><span>ежедневно 10:00-21:00</span></div>
</div>
<p>ИНН 7801321150 ООО «Игора Драйв»</p></body></html>`

// TestEnrich_LegalAddressSplit_VenueMapsHoldsSlot is THE headline Phase-C test
// (the Игора case): the /contacts page's LEGAL seat must route to LegalAddress
// (provenance official_site), while the maps VENUE address keeps the Address
// slot — so the card's map link points at the venue, not the legal office. Runs
// the FULL Enrich orchestration with the maps checker PRESENT (synthetic-green
// discipline: the maps-merge-before-site path is exercised). The legal-vs-venue
// conflict counter must fire (the previously-silent wrong-map-link signal).
func TestEnrich_LegalAddressSplit_VenueMapsHoldsSlot(t *testing.T) {
	t.Parallel()
	srv := newMultiPathServer(map[string]string{
		"/":          homeLinksContactsNoAddr,
		"/contacts/": contactsPageLegalOnlyWithFields,
	})
	defer srv.Close()

	var legalVsVenue int
	e := New(
		WithFetcher(fetch.NewFetcher()),
		WithMapsChecker(&mockMapsCheckerAddr{
			name: "Игора Драйв",
			addr: "Приозерское шоссе, 3, к. 1", // the maps VENUE (geo) address
			lat:  60.66, lon: 30.15,
		}),
		WithMetrics(&Metrics{OnLegalVsVenueAddress: func() { legalVsVenue++ }}),
	)
	result, err := e.Enrich(context.Background(), Item{
		Name: "Игора Драйв", URL: srv.URL + "/", City: "Санкт-Петербург", Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}

	// Address slot = the maps VENUE address (drives the card's map link).
	if result.Facts.Address == nil || !strings.Contains(*result.Facts.Address, "Приозерское") {
		t.Fatalf("Address = %v, want the maps VENUE address (Приозерское) in the map slot", derefOrNil(result.Facts.Address))
	}
	if strings.Contains(derefStr(result.Facts.Address), "В.О. линия") {
		t.Fatalf("Address = %q must NOT be the legal В.О. линия seat — the map link would point at the office", *result.Facts.Address)
	}
	// LegalAddress sidecar = the В.О. линия legal seat, official_site provenance.
	if result.Facts.LegalAddress == nil || !strings.Contains(*result.Facts.LegalAddress, "В.О. линия") {
		t.Fatalf("LegalAddress = %v, want the В.О. линия legal seat", derefOrNil(result.Facts.LegalAddress))
	}
	if got := result.Provenance.LegalAddress.Source; got != "official_site" {
		t.Fatalf("LegalAddress provenance source = %q, want official_site", got)
	}
	if got := result.Provenance.Address.Source; got != "maps" {
		t.Fatalf("Address provenance source = %q, want maps (the venue slot stays with the geo address)", got)
	}
	// Email + hours from the contacts page still surface (the multi-field win).
	if result.Facts.Email == nil || *result.Facts.Email != "info@drive-igora.ru" {
		t.Fatalf("Email = %v, want info@drive-igora.ru", derefOrNil(result.Facts.Email))
	}
	if result.Facts.Hours == nil || !strings.Contains(*result.Facts.Hours, "10:00") {
		t.Fatalf("Hours = %v, want the 10:00 range", derefOrNil(result.Facts.Hours))
	}
	// The previously-silent wrong-map-link class now has a signal.
	if legalVsVenue != 1 {
		t.Fatalf("OnLegalVsVenueAddress = %d, want 1 (legal arrived while maps venue owned the slot)", legalVsVenue)
	}
}

// contactsPageVenueOnly is a /contacts page with ONLY a real venue address (no
// legal markers) plus an email + hours the homepage lacks.
const contactsPageVenueOnly = `<!DOCTYPE html><html lang="ru"><head><title>Контакты</title></head>
<body><div class="contacts">
<address>Лиговский проспект, 50, корпус 9</address>
<a href="mailto:hello@venue.ru">hello@venue.ru</a>
<div><span>Часы работы</span><span>Пн-Вс 11:00-23:00</span></div>
</div></body></html>`

// TestEnrich_VenueOnlyContactsAddress_WinsVenueSlot is the NEGATIVE CONTROL the
// reviewer flagged: a /contacts page carrying ONLY a real venue address (no legal
// markers) must STILL win the venue Address slot over the maps address — it must
// NOT be false-demoted to LegalAddress. Without this guard the classifier could
// over-trigger and strip every real venue address off the map slot.
func TestEnrich_VenueOnlyContactsAddress_WinsVenueSlot(t *testing.T) {
	t.Parallel()
	srv := newMultiPathServer(map[string]string{
		"/":          homeLinksContactsNoAddr,
		"/contacts/": contactsPageVenueOnly,
	})
	defer srv.Close()

	var legalVsVenue int
	e := New(
		WithFetcher(fetch.NewFetcher()),
		WithMapsChecker(&mockMapsCheckerAddr{
			name: "Кафе", addr: "Невский проспект, 100", lat: 59.93, lon: 30.36,
		}),
		WithMetrics(&Metrics{OnLegalVsVenueAddress: func() { legalVsVenue++ }}),
	)
	result, err := e.Enrich(context.Background(), Item{
		Name: "Кафе", URL: srv.URL + "/", City: "Санкт-Петербург", Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	// The venue address from /contacts wins the venue slot over maps (official_site
	// > maps); it is NOT demoted to LegalAddress.
	if result.Facts.Address == nil || !strings.Contains(*result.Facts.Address, "Лиговский") {
		t.Fatalf("Address = %v, want the /contacts venue address Лиговский (official_site > maps, NOT demoted)", derefOrNil(result.Facts.Address))
	}
	if got := result.Provenance.Address.Source; got != "official_site" {
		t.Fatalf("Address provenance source = %q, want official_site (the venue /contacts address wins)", got)
	}
	if result.Facts.LegalAddress != nil {
		t.Fatalf("LegalAddress = %q, want nil — a plain venue address must NOT be classified legal", *result.Facts.LegalAddress)
	}
	// No legal/venue conflict because there is no legal address at all.
	if legalVsVenue != 0 {
		t.Fatalf("OnLegalVsVenueAddress = %d, want 0 (no legal address present)", legalVsVenue)
	}
}

// contactsPageLegalOnly is a /contacts page whose ONLY address is a legal seat —
// no venue visiting address anywhere on the page.
const contactsPageLegalOnly = `<!DOCTYPE html><html lang="ru"><head><title>Контакты</title></head>
<body><div class="contacts">
<address>ул. Профессора Попова, 37, литера Щ, помещение 14-Н, Санкт-Петербург</address>
<a href="mailto:office@studio.ru">office@studio.ru</a>
<div><span>Режим работы</span><span>Пн-Пт 09:00-18:00</span></div>
</div>
<p>ИНН 7813045678 ООО «Студия»</p></body></html>`

// TestEnrich_LegalOnlyContacts_NoMapsAddress_OmitsMapSlot is the no-maps-address
// case (f): when ONLY a legal address exists anywhere (no maps venue address, no
// venue address on /contacts), the venue Address slot stays nil so the card omits
// the map link — omit-for-map beats point-at-office. The legal address still
// surfaces as LegalAddress for «Реквизиты». This is a second legal-address venue
// distinct from the Игора (maps-present) case.
func TestEnrich_LegalOnlyContacts_NoMapsAddress_OmitsMapSlot(t *testing.T) {
	t.Parallel()
	srv := newMultiPathServer(map[string]string{
		"/":          homeLinksContactsNoAddr,
		"/contacts/": contactsPageLegalOnly,
	})
	defer srv.Close()

	var legalVsVenue int
	e := New(
		WithFetcher(fetch.NewFetcher()),
		// Maps returns NO address (only coords) — the no-maps-venue-address case.
		WithMapsChecker(&mockMapsChecker{lat: 59.97, lon: 30.31}),
		WithMetrics(&Metrics{OnLegalVsVenueAddress: func() { legalVsVenue++ }}),
	)
	result, err := e.Enrich(context.Background(), Item{
		Name: "Студия", URL: srv.URL + "/", City: "Санкт-Петербург", Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	// Address slot stays nil → the card omits the map link (omit-for-map beats
	// pointing the map at the legal office).
	if result.Facts.Address != nil {
		t.Fatalf("Address = %q, want nil (legal-only → omit the map slot, never point at the office)", *result.Facts.Address)
	}
	// Legal address still surfaces for «Реквизиты».
	if result.Facts.LegalAddress == nil || !strings.Contains(*result.Facts.LegalAddress, "Попова") {
		t.Fatalf("LegalAddress = %v, want the legal seat on Попова", derefOrNil(result.Facts.LegalAddress))
	}
	if got := result.Provenance.LegalAddress.Source; got != "official_site" {
		t.Fatalf("LegalAddress provenance source = %q, want official_site", got)
	}
	// No conflict: there was never a venue address to be overwritten.
	if legalVsVenue != 0 {
		t.Fatalf("OnLegalVsVenueAddress = %d, want 0 (no venue address ever owned the slot)", legalVsVenue)
	}
	// The email still surfaces (multi-field win on a legal-only page).
	if result.Facts.Email == nil || *result.Facts.Email != "office@studio.ru" {
		t.Fatalf("Email = %v, want office@studio.ru", derefOrNil(result.Facts.Email))
	}
}

package enriche

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anatolykoptev/go-enriche/maps"
	"github.com/anatolykoptev/go-enriche/search"
)

// dniPhoneMapsChecker returns an OrgData carrying a maps phone — the rotating
// tracking number the real Игора maps card ships — so the orchestration tests
// exercise the production path where maps merges a phone BEFORE the site fetch.
type dniPhoneMapsChecker struct{ phone string }

func (m dniPhoneMapsChecker) Check(_ context.Context, _, _, _ string) (*maps.CheckResult, error) {
	return &maps.CheckResult{
		Status:  maps.PlaceOpen,
		OrgData: &maps.OrgData{Name: "Игора Драйв", Phone: m.phone},
	}, nil
}

// liveDNIHTML mirrors the live drive-igora.ru shape: an inline Mango async
// loader (no <script src>), a contacts-region tel: (the rotating proxy), and NO
// wa.me social link — the exact maps-present DNI case the prior probe never
// exercised. The Mango loader is inline (constructs the loader URL as a string
// literal) to match production, not a <script src> attribute.
const liveDNIHTML = `<!DOCTYPE html><html><head>
<script>(function(w,d,o){w['MangoObject']=o;var s=d.createElement('script');
s.src='//widgets.mango-office.ru/widgets/mango.js';d.body.appendChild(s);})
(window,document,'mango');</script>
</head><body>
<footer class="footer"><a href="tel:+78126157000">+7 (812) 615 70 00</a></footer>
</body></html>`

func newDNISite(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(liveDNIHTML))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestEnrich_DNISite_MapsPhone_Omitted is the FIX-1 headline / missing test: the
// official site is DNI-poisoned (Mango, no social link) so it omits its own
// phone, and the maps card ALSO returns a phone (merged earlier at sourceMaps).
// The resolver MUST surface the DNI omit as a first-class verdict that outranks
// the maps phone — final Phone is nil, NOT the rotating maps proxy.
//
// This is the exact production path the prior probe failed to exercise: the
// extract-layer DNI test had no maps phone, so it could not catch the survival
// of the maps phone through the resolver (synthetic green).
func TestEnrich_DNISite_MapsPhone_Omitted(t *testing.T) {
	t.Parallel()
	srv := newDNISite(t)

	e := newTestEnricher(WithMapsChecker(dniPhoneMapsChecker{phone: "+7 813 793 86 16"}))
	result, err := e.Enrich(context.Background(), Item{
		Name: "Игора Драйв", URL: srv.URL, City: "Санкт-Петербург", Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if result.Facts.Phone != nil {
		t.Fatalf("DNI site + maps phone: phone must be OMITTED, got %q (provenance=%+v) — the rotating maps proxy survived the resolver",
			*result.Facts.Phone, result.Provenance.Phone)
	}
	// PhonePoisoned must propagate onto Result.Facts on the real production
	// path (maps phone merged BEFORE the DNI-poisoned site fetch) too, not just
	// the /contacts-page orchestration covered in enriche_contacts_test.go.
	if !result.Facts.PhonePoisoned {
		t.Errorf("PhonePoisoned = false, want true (DNI verdict must propagate even when a maps phone was merged first)")
	}
	// Provenance for an omitted phone carries the poison-lock verdict — it is a
	// resolved refuse, not an absence, so it must NOT come back empty on the
	// wire (was the pre-fix behaviour: dropPhone niled the phone but the
	// resolver's snapshot() only serialized present values).
	if result.Provenance.Phone.Source != srcStrPoisonLocked {
		t.Errorf("omitted phone provenance source=%q want %q (poison-lock verdict, not an absent field)", result.Provenance.Phone.Source, srcStrPoisonLocked)
	}
}

// TestEnrich_DNISite_MapsPhone_SearchDoesNotRefill guards that a DNI-poison
// drop LOCKS the phone: a later lower-priority source (a search snippet
// carrying a phone-shaped string) must NOT resurrect a number for the field.
func TestEnrich_DNISite_MapsPhone_SearchDoesNotRefill(t *testing.T) {
	t.Parallel()
	srv := newDNISite(t)

	e := newTestEnricher(
		WithMapsChecker(dniPhoneMapsChecker{phone: "+7 813 793 86 16"}),
		WithSearch(&mockProvider{result: &search.SearchResult{
			Context: "Телефон: +7 (812) 111 22 33 для записи на картинг",
		}}),
	)
	result, err := e.Enrich(context.Background(), Item{
		Name: "Игора Драйв", URL: srv.URL, City: "Санкт-Петербург", Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if result.Facts.Phone != nil {
		t.Fatalf("DNI poison-lock: a later search snippet must NOT refill the phone, got %q", *result.Facts.Phone)
	}
}

// TestEnrich_DNISite_OperatorSeed_Wins guards that an operator-verified phone is
// SACROSANCT even on a DNI site: the poison verdict drops a maps/site phone but
// must NEVER drop a hand-verified operator pin.
func TestEnrich_DNISite_OperatorSeed_Wins(t *testing.T) {
	t.Parallel()
	srv := newDNISite(t)

	e := newTestEnricher(WithMapsChecker(dniPhoneMapsChecker{phone: "+7 813 793 86 16"}))
	result, err := e.Enrich(context.Background(), Item{
		Name: "Игора Драйв", URL: srv.URL, City: "Санкт-Петербург", Mode: ModePlaces,
		Seed: SeedFacts{Phone: "+7 (812) 615 70 00"},
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if result.Facts.Phone == nil || *result.Facts.Phone != "+7 (812) 615 70 00" {
		t.Fatalf("operator seed must survive DNI poison: want +7 (812) 615 70 00, got %v", result.Facts.Phone)
	}
	if result.Provenance.Phone.Source != srcStrOperatorVerified {
		t.Errorf("operator phone provenance source=%q want operator_verified", result.Provenance.Phone.Source)
	}
}

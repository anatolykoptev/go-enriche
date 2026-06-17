package enriche

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anatolykoptev/go-enriche/fetch"
	"github.com/anatolykoptev/go-enriche/maps"
)

// TestEnrich_OperatorVerified_SurvivesReEnrich is the headline override-precedence
// proof at the full-orchestration level: an operator-verified phone supplied on
// the input MUST survive a re-enrich whose site fetch AND maps card both yield a
// DIFFERENT phone. Mirrors the live monetized-content case (article 56564): the
// hand-verified +7 (921) 956-18-40 must not be silently replaced by a rotating
// (812) DNI proxy or a maps card.
func TestEnrich_OperatorVerified_SurvivesReEnrich(t *testing.T) {
	t.Parallel()

	html := goldenFixture(t, "royal-wed.html") // a real site that yields its own phone
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(html)) //nolint:errcheck
	}))
	defer srv.Close()

	const mapsPhone = "+7 (812) 956-18-40"     // rotating maps card
	const operatorPhone = "+7 (921) 956-18-40" // the operator-verified, hand-shipped value
	checker := stubMapsChecker{res: &maps.CheckResult{
		Status:  maps.PlaceOpen,
		OrgData: &maps.OrgData{Name: "Royal Wedding", Phone: mapsPhone},
	}}

	e := New(WithFetcher(fetch.NewFetcher()), WithMapsChecker(checker))
	result, err := e.Enrich(context.Background(), Item{
		Name: "Royal Wedding", URL: srv.URL, City: spbCity, Mode: ModePlaces,
		Seed: SeedFacts{Phone: operatorPhone}, // operator override
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}

	if got := derefStr(result.Facts.Phone); got != operatorPhone {
		t.Errorf("phone = %q, want operator-verified %q (operator value was overwritten by enrich)", got, operatorPhone)
	}
	if result.Provenance.Phone.Source != "operator_verified" {
		t.Errorf("phone provenance source = %q, want operator_verified", result.Provenance.Phone.Source)
	}
	if result.Provenance.Phone.Confidence != "high" {
		t.Errorf("phone provenance confidence = %q, want high", result.Provenance.Phone.Confidence)
	}
}

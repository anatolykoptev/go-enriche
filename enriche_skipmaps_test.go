package enriche

import (
	"context"
	"testing"

	"github.com/anatolykoptev/go-enriche/maps"
)

// countingMapsChecker records how many times Check is invoked and returns a
// distinct maps phone via OrgData. A SkipMapsCheck test uses the call count to
// prove the checker was never consulted, and the distinct phone to prove the
// maps OrgData never reached the facts. No existing double counts calls, so
// this is the minimal test seam the SkipMapsCheck gate needs.
type countingMapsChecker struct {
	phone string
	calls int
}

func (c *countingMapsChecker) Check(_ context.Context, _, _, _ string) (*maps.CheckResult, error) {
	c.calls++
	return &maps.CheckResult{
		Status:  maps.PlaceOpen,
		OrgData: &maps.OrgData{Name: "Some Place", Phone: c.phone},
	}, nil
}

// TestEnrich_SkipMapsCheck_SuppressesMapsChecker is the headline guard: a
// ModePlaces item with a NON-nil maps checker and SkipMapsCheck=true must not
// invoke the checker at all, and the maps OrgData phone must never surface in
// facts. Revert the "|| item.SkipMapsCheck" gate clause in checkMapsStatus and
// this goes RED (calls==1, Facts.Phone==the maps phone).
func TestEnrich_SkipMapsCheck_SuppressesMapsChecker(t *testing.T) {
	t.Parallel()
	checker := &countingMapsChecker{phone: "+7 (495) 000-00-00"}
	e := newTestEnricher(WithMapsChecker(checker))

	result, err := e.Enrich(context.Background(), Item{
		Name:          "Кафе Пример",
		City:          "Санкт-Петербург",
		Mode:          ModePlaces,
		SkipMapsCheck: true,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if checker.calls != 0 {
		t.Errorf("maps checker Check called %d times, want 0 — SkipMapsCheck must suppress checkMapsStatus entirely", checker.calls)
	}
	if result.Facts.Phone != nil {
		t.Errorf("Facts.Phone = %q, want nil — the maps OrgData phone must never reach facts when SkipMapsCheck=true", *result.Facts.Phone)
	}
}

// TestEnrich_SkipMapsCheck_DefaultFalse_StillChecksMaps is the anti-vacuous
// control: with SkipMapsCheck left at its zero value (false) the very same
// checker MUST be called exactly once and its phone MUST merge into facts.
// This proves the checker is genuinely wired on this path, so the suppression
// asserted above is a real behavioural change and not a vacuously-green test.
func TestEnrich_SkipMapsCheck_DefaultFalse_StillChecksMaps(t *testing.T) {
	t.Parallel()
	checker := &countingMapsChecker{phone: "+7 (495) 000-00-00"}
	e := newTestEnricher(WithMapsChecker(checker))

	result, err := e.Enrich(context.Background(), Item{
		Name: "Кафе Пример",
		City: "Санкт-Петербург",
		Mode: ModePlaces,
		// SkipMapsCheck omitted -> false (default): maps-check must still run.
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if checker.calls != 1 {
		t.Errorf("maps checker Check called %d times, want 1 — default (SkipMapsCheck=false) must still run the maps check", checker.calls)
	}
	if result.Facts.Phone == nil {
		t.Error("Facts.Phone = nil, want the maps OrgData phone — default path must merge the maps phone (proves the checker is wired and only SkipMapsCheck gates it)")
	}
}

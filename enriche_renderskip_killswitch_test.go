package enriche

import (
	"context"
	"testing"
)

// TestEnrich_RenderSkip_KillSwitchDisabled_RendersAgain proves the ADR-8 ops
// kill-switch (WithRenderSkipDisabled): with the switch ON, an mcmedok-class
// page whose ONLY phone signal is a trustworthy anchored branch-JSON SiteNumber
// — the exact page the default (skip-enabled) path SKIPS the render for (see
// TestEnrich_RenderSkip_HomepageAnchoredRawSufficient_SkipsRender) — RENDERS
// again, exactly as it did pre-v1.30.0. This is the revert-without-code-change
// lever for the one-way data consequence of a wrong skip.
//
// RED-on-revert: drop WithRenderSkipDisabled or the renderSkipDisabled guard in
// rawContactsSufficient and the anchored raw SiteNumber suppresses the render
// again, so spy.called() is false and this test fails.
func TestEnrich_RenderSkip_KillSwitchDisabled_RendersAgain(t *testing.T) {
	t.Parallel()
	srv := newTestServer(rsHomeBranchJSONAnchored, 200)
	defer srv.Close()

	spy := &renderSpy{body: rsHomeBranchJSONAnchored}
	var skips int
	e := newTestEnricher(
		WithFetcher(testFetcher()),
		WithMapsChecker(&mockMapsChecker{lat: 59.93, lon: 30.33}),
		WithBrowserFetch(spy.fetch),
		WithRenderSkipDisabled(true), // ops kill-switch ON: the render must fire
		WithMetrics(&Metrics{OnBrowserRenderSkipped: func(_, _ string) { skips++ }}),
	)
	result, err := e.Enrich(context.Background(), Item{
		Name: "Клиника", URL: srv.URL, City: "Санкт-Петербург", Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if !spy.called() {
		t.Fatal("kill-switch ON but the render did NOT fire — the anchored raw SiteNumber still suppressed it (WithRenderSkipDisabled not wired into rawContactsSufficient)")
	}
	if skips != 0 {
		t.Fatalf("OnBrowserRenderSkipped fired %d times with the kill-switch ON, want 0 (no skip may be recorded)", skips)
	}
	if result.RenderSkipped {
		t.Fatal("Result.RenderSkipped = true with the kill-switch ON, want false (a render actually happened)")
	}
}

// TestEnrich_RenderSkip_KillSwitchDefault_StillSkips is the paired control: the
// SAME fixture with the DEFAULT enricher (no WithRenderSkipDisabled → skip
// enabled) still SKIPS the render, so the test above isolates the switch, not
// the fixture. WithRenderSkipDisabled(false) is the explicit default and must
// behave identically to omitting the option.
func TestEnrich_RenderSkip_KillSwitchDefault_StillSkips(t *testing.T) {
	t.Parallel()
	srv := newTestServer(rsHomeBranchJSONAnchored, 200)
	defer srv.Close()

	spy := &renderSpy{body: rsHomeBranchJSONAnchored}
	e := newTestEnricher(
		WithFetcher(testFetcher()),
		WithMapsChecker(&mockMapsChecker{lat: 59.93, lon: 30.33}),
		WithBrowserFetch(spy.fetch),
		WithRenderSkipDisabled(false), // explicit default = skip enabled
	)
	result, err := e.Enrich(context.Background(), Item{
		Name: "Клиника", URL: srv.URL, City: "Санкт-Петербург", Mode: ModePlaces,
	})
	if err != nil {
		t.Fatalf("Enrich error: %v", err)
	}
	if spy.called() {
		t.Fatalf("default enricher rendered (%v) — a trustworthy anchored raw SiteNumber must SKIP by default", spy.urls)
	}
	if !result.RenderSkipped {
		t.Fatal("Result.RenderSkipped = false by default, want true (the render was trust-skipped)")
	}
}

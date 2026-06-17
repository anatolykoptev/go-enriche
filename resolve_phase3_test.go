package enriche

import "testing"

// TestResolver_OperatorVerified_BeatsOfficialSite proves the override-precedence
// invariant: an operator-verified seed wins over an official-site value AND is
// never downgraded — regardless of merge order (the resolver is the single
// authority, not call order). This is the load-bearing protection for the
// monetized-content case (the +7 (921) 956-18-40 shipped to article 56564 must
// survive a re-enrich that would otherwise produce a rotating (812) proxy).
func TestResolver_OperatorVerified_BeatsOfficialSite(t *testing.T) {
	t.Parallel()

	const operatorPhone = "+7 (921) 956-18-40"
	const sitePhone = "+7 (812) 439-18-55" // a rotating DNI proxy the site would yield

	build := func() (*Facts, *resolver) {
		f := &Facts{}
		_, m := newCountingMetrics()
		return f, &resolver{facts: f, prov: &factProvenance{}, m: m}
	}

	// Order A: operator seed first (production order), then site.
	fA, rA := build()
	rA.seedOperatorValues(SeedFacts{Phone: operatorPhone})
	rA.mergeSite(Facts{Phone: ptr(sitePhone)})

	// Order B: site first, then operator seed (reverse — must still pin operator).
	fB, rB := build()
	rB.mergeSite(Facts{Phone: ptr(sitePhone)})
	rB.seedOperatorValues(SeedFacts{Phone: operatorPhone})

	if got := derefStr(fA.Phone); got != operatorPhone {
		t.Errorf("order A: operator value lost: got %q want %q", got, operatorPhone)
	}
	if got := derefStr(fB.Phone); got != operatorPhone {
		t.Errorf("order B: operator value lost: got %q want %q", got, operatorPhone)
	}
	if rA.prov.phone.source != sourceOperatorVerified {
		t.Errorf("order A: phone owner = %v, want operator_verified", rA.prov.phone.source)
	}
	if rA.prov.phone.conf != confHigh {
		t.Errorf("order A: phone confidence = %v, want high", rA.prov.phone.conf)
	}
}

// TestResolver_SourcePriority_FullOrder asserts the complete priority ladder
// operator_verified > official_site > aggregator > maps > search is monotone:
// for every adjacent pair, the higher source's value wins when both are present,
// independent of which is merged first.
func TestResolver_SourcePriority_FullOrder(t *testing.T) {
	t.Parallel()

	type tier struct {
		name string
		src  fieldSource
		val  string
	}
	ladder := []tier{
		{"search", sourceSearch, "search-val"},
		{"maps", sourceMaps, "maps-val"},
		{"aggregator", sourceAggregator, "agg-val"},
		{"official_site", sourceOfficialSite, "site-val"},
		{"operator_verified", sourceOperatorVerified, "operator-val"},
	}

	for i := 0; i < len(ladder); i++ {
		for j := i + 1; j < len(ladder); j++ {
			lo, hi := ladder[i], ladder[j]

			// hi merged after lo.
			f1 := &Facts{}
			_, m1 := newCountingMetrics()
			r1 := &resolver{facts: f1, prov: &factProvenance{}, m: m1}
			r1.set(&f1.Phone, &r1.prov.phone, lo.val, lo.src, "phone")
			r1.set(&f1.Phone, &r1.prov.phone, hi.val, hi.src, "phone")

			// hi merged before lo.
			f2 := &Facts{}
			_, m2 := newCountingMetrics()
			r2 := &resolver{facts: f2, prov: &factProvenance{}, m: m2}
			r2.set(&f2.Phone, &r2.prov.phone, hi.val, hi.src, "phone")
			r2.set(&f2.Phone, &r2.prov.phone, lo.val, lo.src, "phone")

			if got := derefStr(f1.Phone); got != hi.val {
				t.Errorf("%s>%s (lo-first): got %q want %q", hi.name, lo.name, got, hi.val)
			}
			if got := derefStr(f2.Phone); got != hi.val {
				t.Errorf("%s>%s (hi-first): got %q want %q", hi.name, lo.name, got, hi.val)
			}
			if r1.prov.phone.source != hi.src || r2.prov.phone.source != hi.src {
				t.Errorf("%s>%s: owner not %s (lo-first=%v hi-first=%v)",
					hi.name, lo.name, hi.name, r1.prov.phone.source, r2.prov.phone.source)
			}
		}
	}
}

// TestResolver_Snapshot_ProvenanceExported proves the resolved per-field
// {source, confidence} is exported onto the public Provenance sidecar, and that
// an absent field stays zero-valued (omitempty on the wire).
func TestResolver_Snapshot_ProvenanceExported(t *testing.T) {
	t.Parallel()

	f := &Facts{}
	_, m := newCountingMetrics()
	r := &resolver{facts: f, prov: &factProvenance{}, m: m}

	r.set(&f.Phone, &r.prov.phone, "+79219561840", sourceOfficialSite, "phone")
	r.set(&f.Address, &r.prov.address, "Фурштатская ул.", sourceMaps, "address")
	// Price left absent.

	p := r.snapshot()
	if p.Phone.Source != "official_site" || p.Phone.Confidence != "high" {
		t.Errorf("phone provenance = %+v, want {official_site, high}", p.Phone)
	}
	if p.Address.Source != "maps" || p.Address.Confidence != "low" {
		t.Errorf("address provenance = %+v, want {maps, low}", p.Address)
	}
	if p.Price.Source != "" || p.Price.Confidence != "" {
		t.Errorf("absent price provenance = %+v, want zero-value", p.Price)
	}
}

// TestConfidenceFor_Table locks the ADR-006 base confidence mapping.
func TestConfidenceFor_Table(t *testing.T) {
	t.Parallel()
	cases := map[fieldSource]confidence{
		sourceOperatorVerified: confHigh,
		sourceOfficialSite:     confHigh,
		sourceAggregator:       confLow,
		sourceMaps:             confLow,
		sourceSearch:           confLow,
		sourceNone:             confLow,
	}
	for src, want := range cases {
		if got := confidenceFor(src); got != want {
			t.Errorf("confidenceFor(%v) = %v, want %v", src, got, want)
		}
	}
}

func ptr(s string) *string { return &s }

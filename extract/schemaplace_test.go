package extract

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// TestCollectSiteNumbers_SchemaPlace_MultiLocationSurfacesBranches is the P2
// REPRESENTATIVE golden (testdata/golden/schemaplace-multilocation.html) —
// no live failing card exists yet for this class (see the P2 plan's Open
// Questions). A schema.org @graph marks up TWO LocalBusiness branches, each
// its own city + telephone: an 812 (СПб) branch and a 495 (Moscow) branch.
// Before schemaPlaceCandidates existed, CollectSiteNumbers never read
// schema.org Place data at all — structured.Data.Places() was dormant for
// SiteNumbers, feeding only the unrelated FirstPlace()->Facts.Phone path.
// RED-on-revert: stubbing schemaPlaceCandidates to return nil makes the 812
// assertion below fail outright (SiteNumbers missing the branch candidate).
func TestCollectSiteNumbers_SchemaPlace_MultiLocationSurfacesBranches(t *testing.T) {
	t.Parallel()
	doc := docFromFixture(t, "schemaplace-multilocation.html")
	nums := CollectSiteNumbers(doc, false)

	spb := findByValueSubstring(nums, "222-33-44")
	if spb == nil {
		t.Fatalf("SiteNumbers missing the schema.org SPb 812 branch candidate (222-33-44); got %+v", nums)
	}
	if spb.Source != numSourceSchemaPlace {
		t.Errorf("812 branch Source = %q, want %q", spb.Source, numSourceSchemaPlace)
	}
	if !spb.Anchored {
		t.Errorf("812 branch Anchored = false, want true")
	}
	if spb.DNI {
		t.Errorf("812 branch DNI = true, want false (fixture trips no known DNI vendor)")
	}
	if !spb.Trustworthy {
		t.Errorf("812 branch Trustworthy = false, want true (anchored, no DNI vendor active)")
	}
	if cityMatch, cityForeign := ClassifyCityMembership(spb.Value, ExpectedAreaCodes("Санкт-Петербург")); !cityMatch || cityForeign {
		t.Errorf("812 branch ClassifyCityMembership = (%v,%v), want (true,false) for Санкт-Петербург", cityMatch, cityForeign)
	}

	moscow := findByValueSubstring(nums, "111-22-33")
	if moscow == nil {
		t.Fatalf("SiteNumbers missing the schema.org Moscow 495 branch candidate (111-22-33); got %+v", nums)
	}
	if moscow.Source != numSourceSchemaPlace {
		t.Errorf("495 branch Source = %q, want %q", moscow.Source, numSourceSchemaPlace)
	}
	if cm, cf := ClassifyCityMembership(moscow.Value, ExpectedAreaCodes("Санкт-Петербург")); cm || !cf {
		t.Errorf("495 branch ClassifyCityMembership = (%v,%v), want (false,true) for Санкт-Петербург", cm, cf)
	}
}

// TestSchemaPlaceCandidates_SingleLocationNoOp proves the multi-location
// no-op guard (minMultiLocationPlaces): a page with FEWER than 2 schema.org
// Place entries yields ZERO schemaPlaceCandidates. A single Place's
// telephone is already read by the unrelated FirstPlace()->Facts.Phone
// path (facts.go), and every existing single-location golden fixture
// depends on this finder staying silent — otherwise DedupeKeepStronger's
// higher-tier-wins rule would reclassify their Source out from under them
// (see schemaplace.go's minMultiLocationPlaces doc comment).
//
// Non-vacuous: the two-location case (want=2) proves the finder actually
// runs and returns candidates when the gate IS satisfied, so a
// stubbed-out/always-nil schemaPlaceCandidates could not trivially "pass"
// the zero/one-location cases too for the wrong reason.
func TestSchemaPlaceCandidates_SingleLocationNoOp(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		html string
		want int
	}{
		{"no places at all", `<html><body>no structured data</body></html>`, 0},
		{
			"exactly one LocalBusiness",
			`<html><head><script type="application/ld+json">` +
				`{"@context":"https://schema.org","@type":"LocalBusiness","telephone":"+7 (812) 111-22-33"}` +
				`</script></head><body></body></html>`,
			0,
		},
		{
			"two LocalBusiness branches",
			`<html><head><script type="application/ld+json">` +
				`{"@context":"https://schema.org","@graph":[` +
				`{"@type":"LocalBusiness","telephone":"+7 (812) 111-22-33"},` +
				`{"@type":"LocalBusiness","telephone":"+7 (495) 222-33-44"}]}` +
				`</script></head><body></body></html>`,
			2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tc.html))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			got := schemaPlaceCandidates(doc)
			if len(got) != tc.want {
				t.Fatalf("schemaPlaceCandidates() = %d candidates, want %d; got %+v", len(got), tc.want, got)
			}
		})
	}
}

// TestCollectSiteNumbers_SchemaPlace_ExistingSingleLocationGoldenUnaffected
// proves the no-op guard holds on a REAL existing P1 fixture
// (testdata/golden/branchjson-jsonld-skip.html, one JSON-LD LocalBusiness):
// CollectSiteNumbers must never emit a Source=schema_place entry for it,
// and — since that fixture carries no other DOM phone candidate either —
// the set stays empty, exactly its pre-P2 shape.
func TestCollectSiteNumbers_SchemaPlace_ExistingSingleLocationGoldenUnaffected(t *testing.T) {
	t.Parallel()
	doc := docFromFixture(t, "branchjson-jsonld-skip.html")
	if got := schemaPlaceCandidates(doc); len(got) != 0 {
		t.Fatalf("schemaPlaceCandidates(doc) = %+v, want empty (single-location no-op guard)", got)
	}
	nums := CollectSiteNumbers(doc, false)
	if len(nums) != 0 {
		t.Fatalf("CollectSiteNumbers(doc, false) = %+v, want empty (unchanged from pre-P2 shape)", nums)
	}
}

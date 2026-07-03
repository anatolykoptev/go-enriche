package extract

import (
	"log/slog"

	"github.com/PuerkitoBio/goquery"
	"github.com/anatolykoptev/go-enriche/structured"
)

// minMultiLocationPlaces is the no-op guard for schemaPlaceCandidates: a
// page whose schema.org structured data marks up FEWER than this many Place
// entities is a single-location (or no) schema.org page, and
// schemaPlaceCandidates deliberately adds NOTHING for it.
//
// This is the load-bearing anti-regression gate for this finder. A single
// Place's telephone is ALREADY read by the unrelated
// structured.Data.FirstPlace() -> Facts.Phone path (facts.go), which every
// existing golden fixture already exercises at tierMicrodata/tierContacts.
// Without this gate, a page like
// testdata/golden/branchjson-jsonld-skip.html (one JSON-LD LocalBusiness)
// would gain a NEW tierSchemaPlace reading of the SAME digits an existing
// candidate already covers, and DedupeKeepStronger's higher-tier-wins rule
// would silently reclassify that existing candidate's Source — breaking
// every single-location golden's byte-identical SiteNumbers output. Only a
// page that genuinely marks up MULTIPLE branches (the actual P2 gap: a
// national chain's @graph with one LocalBusiness per branch, each its own
// city + telephone) is in scope for this finder.
const minMultiLocationPlaces = 2

// schemaPlaceCandidates finds phone numbers in a page's schema.org
// structured data (JSON-LD + Microdata) that marks up MULTIPLE
// Place/LocalBusiness entries — e.g. a national chain's @graph listing one
// LocalBusiness per branch, each with its own city + telephone. It reuses
// the package's existing multi-location-capable parser,
// structured.Data.Places() (structured/parser.go), which already walks
// @graph arrays; this finder was the only piece missing to feed that output
// into SiteNumbers.
//
// Distinct from branchJSONCandidates (branchjson.go), which reads a
// national chain's BESPOKE inline-script branch-locator JSON — a
// non-standard JS string-literal assignment. schemaPlaceCandidates instead
// reads STANDARD schema.org markup astappiev/microdata already parses into
// structured.Place values. The two finders cover disjoint source classes
// and are deliberately kept in separate files with separate Source labels
// — see tierSchemaPlace's doc comment (contacts.go) for the pr-review-
// council finding that mislabeled a schema.org reading as branch_json on
// P1; a schema.org reading must never reuse that label.
//
// Anti-fabrication is the SAME hard boundary every finder in this package
// obeys:
//  1. the multi-location no-op guard above — a single Place is out of
//     scope, never even inspected for a phone;
//  2. the ONLY input read per surviving Place is the parser's own typed
//     Phone field — no recursion, no free-text scan of the page, no
//     fallback to any other structured.Place property;
//  3. every surviving value is gated through the SAME
//     makeCandidate/ValidatePhone Rossvyaz-numbering-plan ceiling every
//     other finder in this package obeys.
func schemaPlaceCandidates(doc *goquery.Document) []phoneCandidate {
	if doc == nil || len(doc.Nodes) == 0 {
		return nil
	}
	// Reuse doc's own already-parsed root node: structured.ParseNode walks
	// the SAME tree goquery already built, instead of re-serializing doc
	// back to a string and having astappiev/microdata re-parse it from
	// scratch. pageURL is not needed — Place.Phone is a plain string
	// property, never a URL that needs base-URL resolution.
	data, err := structured.ParseNode(doc.Nodes[0], "")
	if err != nil || data == nil {
		return nil
	}
	places := data.Places()
	if len(places) < minMultiLocationPlaces {
		return nil
	}
	var out []phoneCandidate
	for _, place := range places {
		if place == nil || place.Phone == nil || *place.Phone == "" {
			continue
		}
		c, ok := makeCandidate(*place.Phone, tierSchemaPlace)
		if !ok {
			continue
		}
		out = append(out, c)
		// maxBranchCandidates (branchjson.go) is the package's existing
		// per-page candidate-cap convention; reused as-is rather than a
		// second constant of the same value, per the same 4-core-box
		// resource-bound reasoning branchJSONCandidates documents.
		if len(out) >= maxBranchCandidates {
			slog.Warn("extract: schemaPlaceCandidates candidate cap tripped", "cap", maxBranchCandidates)
			break
		}
	}
	return out
}

package extract

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// docFromFixture parses a golden fixture into a *goquery.Document, failing
// the test on a parse error. Kept local to this file since CollectSiteNumbers
// takes a doc, not raw HTML.
func docFromFixture(t *testing.T, name string) *goquery.Document {
	t.Helper()
	html := readFixture(t, name)
	doc, err := documentFromHTML(html)
	if err != nil || doc == nil {
		t.Fatalf("parse fixture %s: %v", name, err)
	}
	return doc
}

// findByValueSubstring returns the first PhoneNumberFact whose Value contains
// sub, or nil.
func findByValueSubstring(nums []PhoneNumberFact, sub string) *PhoneNumberFact {
	for i := range nums {
		if strings.Contains(nums[i].Value, sub) {
			return &nums[i]
		}
	}
	return nil
}

// TestCollectSiteNumbers_Eksimer_SocialLinkTrustworthyDespiteCalltouch is the
// headline P2 calibration golden: the site runs Calltouch DNI, so its header
// tel: is a rotating proxy and must NOT be Trustworthy, while the hard-coded
// WhatsApp number (325-55-35) is DNI-immune and MUST be Trustworthy — the
// fail-closed page-level gate applied per-candidate, reused verbatim from
// resolvePhoneForCityDNI. The maps-card number 561-13-62 must never appear —
// it is not part of the site's own HTML at all.
func TestCollectSiteNumbers_Eksimer_SocialLinkTrustworthyDespiteCalltouch(t *testing.T) {
	t.Parallel()
	doc := docFromFixture(t, "eksimer.html")
	nums := CollectSiteNumbers(doc, false)

	if got := findByValueSubstring(nums, "5611362"); got != nil {
		t.Fatalf("SiteNumbers must never contain the maps-card number 561-13-62 (not on the site's own page): got %+v", *got)
	}

	social := findByValueSubstring(nums, "3255535")
	if social == nil {
		t.Fatalf("SiteNumbers missing the WhatsApp 325-55-35 candidate; got %+v", nums)
	}
	if social.Source != "social_link" {
		t.Errorf("325-55-35 Source = %q, want social_link", social.Source)
	}
	if !social.Anchored {
		t.Errorf("325-55-35 Anchored = false, want true (social-link tier)")
	}
	if !social.DNI {
		t.Errorf("325-55-35 DNI = false, want true (Calltouch is active on this page)")
	}
	if !social.Trustworthy {
		t.Errorf("325-55-35 Trustworthy = false, want true (DNI-immune social-link candidate survives the Calltouch page-level gate)")
	}

	header := findByValueSubstring(nums, "300-11-99")
	if header == nil {
		t.Fatalf("SiteNumbers missing the header tel: candidate; got %+v", nums)
	}
	if header.Source != regionContacts {
		t.Errorf("header tel: Source = %q, want %q", header.Source, regionContacts)
	}
	if !header.Anchored {
		t.Errorf("header tel: Anchored = false, want true (contacts-region tier)")
	}
	if header.Trustworthy {
		t.Errorf("header tel: Trustworthy = true, want false (DNI active — only the social-link candidate survives the fail-closed gate)")
	}
}

// TestCollectSiteNumbers_Lazermed_AnchoredTelTrustworthyNoDNI is the second P2
// calibration golden: a CLEAN (no DNI vendor) site whose pickPhoneCandidate
// winner is the WhatsApp number, but whose real contacts-region tel:
// (571-46-12) — a valid, DIFFERENT site number pickPhoneCandidate discards —
// must still surface as Trustworthy in the SET (no DNI vendor demotes it).
func TestCollectSiteNumbers_Lazermed_AnchoredTelTrustworthyNoDNI(t *testing.T) {
	t.Parallel()
	doc := docFromFixture(t, "lazermed.html")
	nums := CollectSiteNumbers(doc, false)

	tel := findByValueSubstring(nums, "571-46-12")
	if tel == nil {
		t.Fatalf("SiteNumbers missing the 571-46-12 contacts-region tel: candidate; got %+v", nums)
	}
	if tel.Source != regionContacts {
		t.Errorf("571-46-12 Source = %q, want %q", tel.Source, regionContacts)
	}
	if !tel.Anchored {
		t.Errorf("571-46-12 Anchored = false, want true (contacts-region tier)")
	}
	if tel.DNI {
		t.Errorf("571-46-12 DNI = true, want false (no DNI vendor on this fixture)")
	}
	if !tel.Trustworthy {
		t.Errorf("571-46-12 Trustworthy = false, want true (anchored, no DNI vendor active)")
	}

	// Calibration cross-check: pickPhoneCandidate/Facts.Phone still commits to
	// the WhatsApp number (rule 1 — social-link wins unconditionally), NOT
	// 571-46-12 — proving 571-46-12 is exactly the class of valid-but-
	// different number the single-winner resolver discards.
	facts := ExtractFactsForCity(readFixture(t, "lazermed.html"), "https://example.com", "")
	if facts.Phone == nil || !strings.Contains(*facts.Phone, "9998877") {
		t.Fatalf("Facts.Phone = %v, want the WhatsApp number (571-46-12 must NOT be the single-winner pick)", derefOrNilExtract(facts.Phone))
	}
}

// TestCollectSiteNumbers_DedupesRepeatedMicrodataKeepsHighestTier guards the
// intra-page dedup: royal-wed.html carries the SAME (812) number 9x (1 header
// tel: + 8 itemprop duplicates) plus one distinct WhatsApp number. Without
// dedup this would leak 9 near-duplicate entries; without the tier-wins
// tiebreak the collapsed entry could lose its correct (higher) contacts-tier
// classification to a lower-tier microdata duplicate.
func TestCollectSiteNumbers_DedupesRepeatedMicrodataKeepsHighestTier(t *testing.T) {
	t.Parallel()
	doc := docFromFixture(t, "royal-wed.html")
	nums := CollectSiteNumbers(doc, false)

	if len(nums) != 2 {
		t.Fatalf("len(SiteNumbers) = %d, want 2 (deduped: one 812 number + one WhatsApp number), got %+v", len(nums), nums)
	}

	tel := findByValueSubstring(nums, "956-18-40")
	if tel == nil {
		t.Fatalf("SiteNumbers missing the deduped 812 candidate; got %+v", nums)
	}
	if tel.Source != regionContacts {
		t.Errorf("deduped 812 candidate Source = %q, want %q (the header tel: tier, not the demoted microdata tier)", tel.Source, regionContacts)
	}
}

// TestCollectSiteNumbers_Deterministic proves repeated calls over the same
// doc yield byte-identical output (ordering + content), independent of the
// internal map used for dedup.
func TestCollectSiteNumbers_Deterministic(t *testing.T) {
	t.Parallel()
	doc := docFromFixture(t, "royal-wed.html")

	first := CollectSiteNumbers(doc, false)
	for i := 0; i < 5; i++ {
		got := CollectSiteNumbers(doc, false)
		if len(got) != len(first) {
			t.Fatalf("run %d: len = %d, want %d", i, len(got), len(first))
		}
		for j := range got {
			if got[j] != first[j] {
				t.Fatalf("run %d: entry %d = %+v, want %+v (non-deterministic ordering)", i, j, got[j], first[j])
			}
		}
	}
}

// TestCollectSiteNumbers_NilDoc guards the nil-safety contract.
func TestCollectSiteNumbers_NilDoc(t *testing.T) {
	t.Parallel()
	if got := CollectSiteNumbers(nil, false); got != nil {
		t.Errorf("CollectSiteNumbers(nil, false) = %+v, want nil", got)
	}
}

// TestCollectSiteNumbersHTML_EmptyInput guards the HTML-string entry point's
// empty-input contract (mirrors ExtractSiteContacts/documentFromHTML).
func TestCollectSiteNumbersHTML_EmptyInput(t *testing.T) {
	t.Parallel()
	if got := CollectSiteNumbersHTML("", false); got != nil {
		t.Errorf("CollectSiteNumbersHTML(\"\") = %+v, want nil", got)
	}
}

// derefOrNilExtract mirrors the enriche package's derefOrNil test helper
// (unexported there, so duplicated minimally here for a readable failure
// message).
func derefOrNilExtract(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

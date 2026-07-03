package extract

import (
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// TestCollectSiteNumbers_MCMedok_BranchJSONSurfacesSPbNumber is the P0
// headline REAL golden (testdata/golden/mcmedok.html): the site's ONLY
// visible tel: is a Moscow 499 number; the real SPb branch phone (812)
// lives ONLY inside the marker_data inline-script JSON. Before
// branchJSONCandidates existed, CollectSiteNumbers never saw the 812 at
// all — the false wp_verify "wrong" this arc fixes. After: the 812 branch
// must surface as Source=branch_json, Anchored=true, Trustworthy=true (no
// DNI vendor trips on this page) and — run through the SAME
// ClassifyCityMembership addSiteNumbers (resolve.go) calls in production —
// CityMatch=true for a Санкт-Петербург project. Both Moscow numbers (the
// DOM header tel: AND the marker_data Moscow branch) must classify
// CityForeign=true.
func TestCollectSiteNumbers_MCMedok_BranchJSONSurfacesSPbNumber(t *testing.T) {
	t.Parallel()
	doc := docFromFixture(t, "mcmedok.html")
	nums := CollectSiteNumbers(doc, false)

	spb := findByValueSubstring(nums, "767-36-61")
	if spb == nil {
		t.Fatalf("SiteNumbers missing the branch-JSON SPb 812 candidate (767-36-61); got %+v", nums)
	}
	if spb.Source != numSourceBranchJSON {
		t.Errorf("812 branch Source = %q, want %q", spb.Source, numSourceBranchJSON)
	}
	if !spb.Anchored {
		t.Errorf("812 branch Anchored = false, want true")
	}
	if spb.DNI {
		t.Errorf("812 branch DNI = true, want false (mcmedok.html trips no known DNI vendor)")
	}
	if !spb.Trustworthy {
		t.Errorf("812 branch Trustworthy = false, want true (anchored, no DNI vendor active)")
	}
	if cityMatch, cityForeign := ClassifyCityMembership(spb.Value, ExpectedAreaCodes("Санкт-Петербург")); !cityMatch || cityForeign {
		t.Errorf("812 branch ClassifyCityMembership = (%v,%v), want (true,false) for Санкт-Петербург", cityMatch, cityForeign)
	}

	domMoscow := findByValueSubstring(nums, "653-80-70")
	if domMoscow == nil {
		t.Fatalf("SiteNumbers missing the DOM header Moscow tel: candidate (653-80-70); got %+v", nums)
	}
	if cm, cf := ClassifyCityMembership(domMoscow.Value, ExpectedAreaCodes("Санкт-Петербург")); cm || !cf {
		t.Errorf("DOM Moscow tel: ClassifyCityMembership = (%v,%v), want (false,true) for Санкт-Петербург", cm, cf)
	}

	branchMoscow := findByValueSubstring(nums, "653-82-20")
	if branchMoscow == nil {
		t.Fatalf("SiteNumbers missing the branch-JSON Moscow candidate (653-82-20); got %+v", nums)
	}
	if branchMoscow.Source != numSourceBranchJSON {
		t.Errorf("branch-JSON Moscow Source = %q, want %q", branchMoscow.Source, numSourceBranchJSON)
	}
	if cm, cf := ClassifyCityMembership(branchMoscow.Value, ExpectedAreaCodes("Санкт-Петербург")); cm || !cf {
		t.Errorf("branch-JSON Moscow ClassifyCityMembership = (%v,%v), want (false,true) for Санкт-Петербург", cm, cf)
	}
}

// TestCollectSiteNumbers_BranchJSON_AntiFab_JunkFieldsNotHarvested is the P0
// anti-fabrication golden (testdata/golden/branchjson-junk.html): unlabeled
// numeric fields (id/lat/lng/timestamp) — one of which would itself pass
// ValidatePhone if read as a phone value — must never enter SiteNumbers,
// and a phone-KEYED but invalid value must still be dropped by
// ValidatePhone. A THIRD, legitimate phone-keyed value (555-66-77) on the
// SAME branchJSONCandidates-scanned script MUST surface as Source=
// branch_json — this positive assertion is what makes the test non-vacuous:
// without it, a stubbed-out/reverted branchJSONCandidates would trivially
// "pass" this test too, since the junk would be absent for the wrong
// reason (the finder never ran at all, not because it correctly excluded
// it). The separate legitimate contacts-region tel: on the page must also
// surface unaffected.
func TestCollectSiteNumbers_BranchJSON_AntiFab_JunkFieldsNotHarvested(t *testing.T) {
	t.Parallel()
	doc := docFromFixture(t, "branchjson-junk.html")
	nums := CollectSiteNumbers(doc, false)

	for _, junk := range []string{"84615384615", "999", "1782286360180753", "12345"} {
		if got := findByValueSubstring(nums, junk); got != nil {
			t.Errorf("SiteNumbers must never contain the non-phone-keyed/invalid value %q; got %+v", junk, *got)
		}
	}
	legit := findByValueSubstring(nums, "555-66-77")
	if legit == nil {
		t.Fatalf("SiteNumbers missing the legitimate branch-JSON candidate (555-66-77) — branchJSONCandidates must have run over this fixture; got %+v", nums)
	}
	if legit.Source != numSourceBranchJSON {
		t.Errorf("555-66-77 Source = %q, want %q", legit.Source, numSourceBranchJSON)
	}
	if findByValueSubstring(nums, "123-45-67") == nil {
		t.Fatalf("SiteNumbers missing the legitimate contacts tel: candidate; got %+v", nums)
	}
	if len(nums) != 2 {
		t.Fatalf("len(SiteNumbers) = %d, want 2 (the contacts tel: + the one legitimate branch_json entry, zero junk harvested); got %+v", len(nums), nums)
	}
}

// TestCollectSiteNumbers_SingleCityNoOp_BranchJSONAddsNothing is the P0
// regression golden: on igora-drive.html (a real single-city page with no
// inline-script branch JSON at all — already covered by
// TestGoldenRegression_ExtractFacts), branchJSONCandidates must contribute
// ZERO candidates, so CollectSiteNumbers' output is byte-identical to its
// pre-branchJSON shape: one deduped, anchored, trustworthy 812 entry.
func TestCollectSiteNumbers_SingleCityNoOp_BranchJSONAddsNothing(t *testing.T) {
	t.Parallel()
	doc := docFromFixture(t, "igora-drive.html")
	nums := CollectSiteNumbers(doc, false)

	if len(nums) != 1 {
		t.Fatalf("len(SiteNumbers) = %d, want 1 (single deduped 812 tel:, unchanged by branchJSONCandidates); got %+v", len(nums), nums)
	}
	n := nums[0]
	if n.Source != regionContacts || !n.Anchored || n.DNI || !n.Trustworthy {
		t.Errorf("SiteNumbers[0] = %+v, want Source=%q Anchored=true DNI=false Trustworthy=true (unchanged from pre-branchJSON)", n, regionContacts)
	}
	if findByValueSubstring(nums, "615 70 00") == nil {
		t.Fatalf("SiteNumbers missing the 812 candidate; got %+v", nums)
	}
}

// TestCollectSiteNumbers_SocialBranchDNICollision_KeepsSocialTierImmunity is
// the P0 tier-collision golden (testdata/golden/branchjson-collision.html):
// the same digits appear as both a hard-coded social-link candidate
// (tierSocialLink, DNI-immune) and a weaker branch-JSON reading
// (tierBranchJSON, NOT DNI-immune) on a page that actively runs a DNI
// vendor. DedupeKeepStronger must keep the SOCIAL reading — Source=
// social_link, Trustworthy=true — never letting the branch_json reading of
// the identical digits demote or shadow the DNI-immune classification.
func TestCollectSiteNumbers_SocialBranchDNICollision_KeepsSocialTierImmunity(t *testing.T) {
	t.Parallel()
	doc := docFromFixture(t, "branchjson-collision.html")

	// Non-vacuousness guard: prove branchJSONCandidates itself actually
	// produced the colliding branch_json reading of these digits BEFORE
	// asking whether the dedup picked the right winner — otherwise a
	// stubbed-out/reverted branchJSONCandidates would trivially "pass" the
	// assertions below too (there would simply be nothing to collide with).
	branchOnly := branchJSONCandidates(doc)
	foundBranchReading := false
	for _, c := range branchOnly {
		if DigitsOnly(c.value) == "79210001122" && c.tier == tierBranchJSON {
			foundBranchReading = true
			break
		}
	}
	if !foundBranchReading {
		t.Fatalf("branchJSONCandidates(doc) did not produce a tierBranchJSON candidate for 79210001122; got %+v", branchOnly)
	}

	nums := CollectSiteNumbers(doc, false)

	var n *PhoneNumberFact
	for i := range nums {
		if DigitsOnly(nums[i].Value) == "79210001122" {
			n = &nums[i]
			break
		}
	}
	if n == nil {
		t.Fatalf("SiteNumbers missing the collision candidate (79210001122); got %+v", nums)
	}
	if n.Source != numSourceSocialLink {
		t.Errorf("collision candidate Source = %q, want %q (social must win the tier collision, not %q)", n.Source, numSourceSocialLink, numSourceBranchJSON)
	}
	if !n.DNI {
		t.Errorf("collision candidate DNI = false, want true (window.roistatProjectId is active on this page)")
	}
	if !n.Trustworthy {
		t.Errorf("collision candidate Trustworthy = false, want true (social-link tier survives the DNI fail-closed gate even though a weaker branch_json reading of the same digits exists)")
	}

	// Only ONE entry for these digits must survive — proving the collision
	// was actually deduped, not merely that the social reading happens to
	// rank first among two surviving entries.
	count := 0
	for i := range nums {
		if DigitsOnly(nums[i].Value) == "79210001122" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("digits 79210001122 appear %d times in SiteNumbers, want 1 (deduped to the stronger social-link reading); got %+v", count, nums)
	}
}

// --- Supplementary unit coverage for branchJSONCandidates' resource bounds
// (not part of the 4 required goldens above, but the cap logic is new code
// that needs its own falsification). ---

// TestBranchJSONCandidates_OversizedScriptSkipped proves the 256KB
// per-script byte cap fails CLOSED: a script whose text exceeds the cap is
// skipped WHOLESALE, even though it opens with an otherwise-valid
// phone-keyed object. A SECOND, under-cap script on the same page carries
// its own valid candidate (999-88-77) that MUST still surface — the
// positive control that makes this test non-vacuous: without it, a
// stubbed-out/reverted branchJSONCandidates would trivially "pass" too
// (empty output either way).
func TestBranchJSONCandidates_OversizedScriptSkipped(t *testing.T) {
	t.Parallel()
	filler := strings.Repeat("x", maxBranchScriptBytes+1024)
	html := `<html><body>` +
		`<script>var branches = [{"phone":"+78121234567","note":"` + filler + `"}];</script>` +
		`<script>var other = [{"phone":"+7 812 999-88-77"}];</script>` +
		`</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := branchJSONCandidates(doc)
	for _, c := range got {
		if DigitsOnly(c.value) == "78121234567" {
			t.Errorf("branchJSONCandidates() unexpectedly harvested the oversized script's candidate; got %+v", got)
		}
	}
	found := false
	for _, c := range got {
		if DigitsOnly(c.value) == "78129998877" {
			found = true
		}
	}
	if !found {
		t.Fatalf("branchJSONCandidates() = %+v, want the under-cap sibling script's 999-88-77 candidate present", got)
	}
}

// TestBranchJSONCandidates_CandidateCapTripped proves the 200-candidate
// per-page cap actually bounds output on a page carrying more than 200
// distinct valid phone-keyed branch objects.
func TestBranchJSONCandidates_CandidateCapTripped(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString(`<html><body><script>var branches = [`)
	const total = 250
	for i := 0; i < total; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		// 7-digit suffix keeps every number well-formed (7 + area 812 + 7 = 11 digits).
		b.WriteString(`{"phone":"+7 812 ` + zeroPad(i, 7) + `"}`)
	}
	b.WriteString(`];</script></body></html>`)
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(b.String()))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := branchJSONCandidates(doc)
	if len(got) != maxBranchCandidates {
		t.Fatalf("branchJSONCandidates() returned %d candidates, want exactly %d (page carries %d valid candidates, cap must bound it)", len(got), maxBranchCandidates, total)
	}
}

// zeroPad renders i as a zero-padded decimal string of the given width.
func zeroPad(i, width int) string {
	s := strconv.Itoa(i)
	for len(s) < width {
		s = "0" + s
	}
	return s
}

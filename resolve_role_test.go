package enriche

import (
	"testing"

	"github.com/anatolykoptev/go-enriche/extract"
)

// roleGeneralPageHTML is a page whose ONLY phone reading for the shared
// digits is a low-rank general one: a bare body tel: with no contacts
// region and no role/department context (naturalTier=tierBody -> Anchored
// AND Trustworthy both false under siteNumberRank -> rank 0). Deliberately
// LOW rank so it LOSES resolve.go's addSiteNumbers tier/trust merge against
// roleDepartmentalPageHTML below — proving the Role OR-reduce fix survives
// independently of which reading wins the other fields.
const roleGeneralPageHTML = `<!doctype html><html><body>
<article><p>Иногда до нас можно дозвониться по номеру <a href="tel:+78121234567">+7 (812) 123-45-67</a> в рабочее время.</p></article>
</body></html>`

// roleDepartmentalPageHTML is a SECOND page (e.g. a discovered /contacts
// subpage) printing the SAME digits under a preceding department heading,
// inside a contacts region — naturalTier=tierContacts -> Anchored AND
// Trustworthy both true under siteNumberRank -> rank 3, the HIGHEST
// possible, so this reading WINS resolve.go's addSiteNumbers tier/trust
// merge for Value/Source/Anchored/Trustworthy.
const roleDepartmentalPageHTML = `<!doctype html><html><body>
<header>
  <div class="h6">Отдел закупок</div>
  <a href="tel:+78121234567">+7 (812) 123-45-67</a>
</header>
</body></html>`

// TestResolver_AddSiteNumbers_RoleGeneralWinsAcrossPages is the MAJOR
// (review round 1, #3) cross-page regression golden for resolve.go's
// addSiteNumbers: the SAME digits are found on TWO SEPARATE official-site
// pages (the homepage general reading, then a discovered /contacts subpage
// departmental reading) merged via TWO SEPARATE addSiteNumbers calls — the
// real production shape (enriche_fetch.go / enriche_contacts.go call sites,
// see addSiteNumbers' own doc comment). The higher-rank departmental page
// legitimately wins the tier/trust merge (Anchored/Trustworthy), but Role
// must still resolve to general — the demotion must not "re-enter at the
// resolver" even when extract.CollectSiteNumbers' OWN single-page dedup
// (already regression-locked separately) never sees both readings at once.
func TestResolver_AddSiteNumbers_RoleGeneralWinsAcrossPages(t *testing.T) {
	t.Parallel()

	generalNums := extract.CollectSiteNumbersHTML(roleGeneralPageHTML, false)
	if len(generalNums) != 1 {
		t.Fatalf("fixture setup: roleGeneralPageHTML yielded %d SiteNumbers, want 1; got %+v", len(generalNums), generalNums)
	}
	if generalNums[0].Role.IsDepartmental() {
		t.Fatalf("fixture setup: roleGeneralPageHTML reading is already departmental (%+v) — fixture does not isolate the case", generalNums[0])
	}
	if generalNums[0].Anchored || generalNums[0].Trustworthy {
		t.Fatalf("fixture setup: roleGeneralPageHTML reading must be LOW rank (Anchored=false, Trustworthy=false) to prove the fix independently of the tier/trust merge; got %+v", generalNums[0])
	}

	deptNums := extract.CollectSiteNumbersHTML(roleDepartmentalPageHTML, false)
	if len(deptNums) != 1 {
		t.Fatalf("fixture setup: roleDepartmentalPageHTML yielded %d SiteNumbers, want 1; got %+v", len(deptNums), deptNums)
	}
	if !deptNums[0].Role.IsDepartmental() {
		t.Fatalf("fixture setup: roleDepartmentalPageHTML reading is not departmental (%+v) — fixture does not isolate the case", deptNums[0])
	}
	if !deptNums[0].Anchored || !deptNums[0].Trustworthy {
		t.Fatalf("fixture setup: roleDepartmentalPageHTML reading must be HIGH rank (Anchored=true, Trustworthy=true) so it wins the tier/trust merge; got %+v", deptNums[0])
	}
	if extract.DigitsOnly(generalNums[0].Value) != extract.DigitsOnly(deptNums[0].Value) {
		t.Fatalf("fixture setup: the two pages must share the SAME digits; got %q vs %q", generalNums[0].Value, deptNums[0].Value)
	}

	f := &Facts{}
	_, m := newCountingMetrics()
	r := &resolver{facts: f, prov: &factProvenance{}, m: m}

	// Homepage first (general, low rank), then the /contacts subpage
	// (departmental, high rank) — the real mergeSite call order.
	r.addSiteNumbers(generalNums, spbCity)
	r.addSiteNumbers(deptNums, spbCity)

	snap := r.siteNumbersSnapshot()
	if len(snap) != 1 {
		t.Fatalf("siteNumbersSnapshot len = %d, want 1 (same digits deduped across pages); got %+v", len(snap), snap)
	}
	if snap[0].Role.IsDepartmental() {
		t.Errorf("siteNumbersSnapshot()[0] = %+v, want Role=general — the departmental page's higher tier/trust rank must not override the general reading found on the OTHER page", snap[0])
	}
	// Sanity: the departmental page's reading really did win the OTHER
	// fields (Anchored/Trustworthy), proving this isn't vacuously passing
	// because the general reading already won everything.
	if !snap[0].Anchored || !snap[0].Trustworthy {
		t.Errorf("siteNumbersSnapshot()[0] = %+v, want Anchored=true Trustworthy=true (the higher-rank departmental-page reading should still win the non-Role fields)", snap[0])
	}
}

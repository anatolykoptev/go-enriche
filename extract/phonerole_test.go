package extract

import (
	"strings"
	"testing"
)

// TestClassifyPhoneRole_Golden is the classifyPhoneRole precision table:
// unambiguous departmental hits (Tier A/B), the false-demote guard for a
// lookalike EXCLUDED word, and the hard raw=="" -> general invariant.
func TestClassifyPhoneRole_Golden(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
		want PhoneRole
	}{
		{
			name: "leasing_desk_frame_departmental",
			raw:  "по вопросам аренды торговых мест",
			want: roleDepartmental,
		},
		{
			name: "spravochnaya_excluded_stays_general",
			raw:  "справочная",
			want: roleGeneral,
		},
		{
			name: "sales_dept_false_demote_guard",
			raw:  "отдел продаж",
			want: roleGeneral,
		},
		{
			name: "empty_label_unlabeled_never_demotes",
			raw:  "",
			want: roleGeneral,
		},
		{
			name: "fax_tier_a_departmental",
			raw:  "факс",
			want: roleDepartmental,
		},
		// Extra coverage beyond the FF4 golden, for anti-vacuous falsification.
		{
			name: "fax_case_insensitive_uppercase",
			raw:  "ФАКС: +7 (812) 111-11-11",
			want: roleDepartmental,
		},
		{
			name: "accounting_tier_a_departmental",
			raw:  "бухгалтерия",
			want: roleDepartmental,
		},
		{
			name: "co_occurring_general_word_does_not_rescue_departmental",
			// "Отдел закупок" (Tier A) co-occurs with the general-sounding
			// word "Телефон" in the SAME label — the departmental match
			// must win; there is no positive-general override that could
			// let "Телефон" rescue it back to general.
			raw:  "Отдел закупок. Телефон:",
			want: roleDepartmental,
		},
		{
			name: "bare_arenda_stem_alone_stays_general",
			// The bare stem "аренда" (no "по вопросам"/"мест"/"помещен"/
			// "площад"/"торгов" frame) must NOT demote — a rental
			// BUSINESS's own general line legitimately carries it.
			raw:  "Аренда оборудования",
			want: roleGeneral,
		},
		{
			name: "reception_desk_stays_general",
			raw:  "приёмная",
			want: roleGeneral,
		},
		{
			name: "booking_line_stays_general",
			raw:  "бронирование столиков",
			want: roleGeneral,
		},
		// Lexicon substring false-demote regression guards (review round 1,
		// MAJOR-2): бare Tier-A stems that are real substrings of common
		// unrelated venue-type/business words must NOT fire.
		{
			name: "optometry_clinic_stays_general",
			raw:  "Оптометрия",
			want: roleGeneral,
		},
		{
			name: "optometry_center_stays_general",
			raw:  "Центр оптометрии",
			want: roleGeneral,
		},
		{
			name: "water_utility_stays_general",
			raw:  "Водоснабжение",
			want: roleGeneral,
		},
		{
			name: "electricity_utility_stays_general",
			raw:  "Электроснабжение",
			want: roleGeneral,
		},
		{
			name: "heating_utility_stays_general",
			raw:  "Теплоснабжение",
			want: roleGeneral,
		},
		{
			name: "state_procurement_stays_general",
			raw:  "Госзакупки",
			want: roleGeneral,
		},
		{
			name: "facsimile_word_stays_general",
			// «факс» is a real PREFIX of «факсимиле» — must not fire via
			// plain substring Contains; needs letter-boundary matching.
			raw:  "Факсимильная связь",
			want: roleGeneral,
		},
		{
			name: "bare_fax_word_still_departmental",
			// The bare, letter-bounded word "факс" must still match — the
			// boundary fix must not become a false-negative regression.
			raw:  "Факс",
			want: roleDepartmental,
		},
		{
			name: "fax_with_colon_still_departmental",
			raw:  "Факс:",
			want: roleDepartmental,
		},
		{
			name: "procurement_dept_still_departmental",
			// The FRAMED "отдел закупок" form is kept (only the bare stem
			// "закупки" was dropped) — must still classify departmental.
			raw:  "Отдел закупок",
			want: roleDepartmental,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := classifyPhoneRole(tc.raw); got != tc.want {
				t.Errorf("classifyPhoneRole(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestPhoneRoleLabelText_PrecedingHeadingSibling proves the core PRECEDING-
// context scan: a heading with no phone token of its own, sitting BEFORE the
// phone-bearing node as a sibling, is picked up as the role label — the
// p45.su leasing-desk DOM shape phoneValueScope's FOLLOWING-only walk cannot
// reach.
func TestPhoneRoleLabelText_PrecedingHeadingSibling(t *testing.T) {
	t.Parallel()
	const html = `<!doctype html><html><body>
<div class="rent-info">
  <div class="rent-heading">По вопросам аренды торговых мест</div>
  <a itemprop="telephone" href="#">+7 (921) 956-18-40</a>
</div>
</body></html>`
	doc := docFromHTML(t, html)
	node := doc.Find(`a[itemprop="telephone"]`).First()
	if node.Length() == 0 {
		t.Fatalf("fixture setup: itemprop anchor not found")
	}
	got := phoneRoleLabelText(node)
	if !strings.Contains(got, "аренд") {
		t.Errorf("phoneRoleLabelText = %q, want it to contain %q", got, "аренд")
	}
}

// TestPhoneRoleLabelText_NeverReadsOwnPhoneDigitsAtDepthZero proves the node's
// OWN text is never read at depth 0 for an anchor whose own text IS the
// phone's display digits — reading it would leak the phone number itself as
// a "role label", which is not a role at all and would never departmental-
// match anyway but corrupts the field's intent (verbatim role text for a
// downstream LLM).
func TestPhoneRoleLabelText_NeverReadsOwnPhoneDigitsAtDepthZero(t *testing.T) {
	t.Parallel()
	const html = `<!doctype html><html><body>
<header><a href="tel:+78121234567">+7 (812) 123-45-67</a></header>
</body></html>`
	doc := docFromHTML(t, html)
	node := doc.Find(`a[href^="tel:"]`).First()
	got := phoneRoleLabelText(node)
	if strings.Contains(got, "123-45-67") || strings.Contains(got, "8121234567") {
		t.Errorf("phoneRoleLabelText leaked the phone's own digits as role text: %q", got)
	}
}

// TestPhoneRoleLabelText_InlineDeptPrefixSameElement proves the ancestor-
// own-text arm: a role prefix sharing ONE wrapping element with the phone
// anchor ("Отдел аренды. Телефон: <a>…</a>") is read from the wrapping
// element's OWN direct text (excludes the nested anchor's text).
func TestPhoneRoleLabelText_InlineDeptPrefixSameElement(t *testing.T) {
	t.Parallel()
	const html = `<!doctype html><html><body>
<p>Отдел аренды. Телефон: <a href="tel:+78121234567">+7 (812) 123-45-67</a></p>
</body></html>`
	doc := docFromHTML(t, html)
	node := doc.Find(`a[href^="tel:"]`).First()
	got := phoneRoleLabelText(node)
	if !strings.Contains(got, "аренд") {
		t.Errorf("phoneRoleLabelText = %q, want it to contain %q (inline dept prefix)", got, "аренд")
	}
	if strings.Contains(got, "123-45-67") {
		t.Errorf("phoneRoleLabelText leaked the phone's own digits: %q", got)
	}
}

// TestPhoneRoleLabelText_NoContextReturnsEmpty proves the bounded-walk
// prefer-false-negative contract: a phone with no preceding sibling and no
// ancestor own-text within maxPhoneLabelAncestor levels returns "".
func TestPhoneRoleLabelText_NoContextReturnsEmpty(t *testing.T) {
	t.Parallel()
	const html = `<!doctype html><html><body>
<header><a href="tel:+78121234567">+7 (812) 123-45-67</a></header>
</body></html>`
	doc := docFromHTML(t, html)
	node := doc.Find(`a[href^="tel:"]`).First()
	if got := phoneRoleLabelText(node); got != "" {
		t.Errorf("phoneRoleLabelText = %q, want \"\" (no role context anywhere nearby)", got)
	}
}

// p45suShapeHTML is the motivating fixture from the spec: a general tel:
// listed FIRST (header, no role context) plus a leasing-desk number under a
// PRECEDING «…по вопросам аренды торговых мест» heading, printed as a
// label-less [itemprop=telephone] anchor with NO tel: href — the DOM shape
// telCandidates never sees at all, only microdataCandidates.
const p45suShapeHTML = `<!doctype html><html><body>
<header>
  <a href="tel:+78121234567">+7 (812) 123-45-67</a>
</header>
<div class="rent-info">
  <div class="rent-heading">По вопросам аренды торговых мест</div>
  <a itemprop="telephone" href="#">+7 (921) 956-18-40</a>
</div>
</body></html>`

// TestCollectSiteNumbers_P45suShape_LeasingDeskDepartmental is the headline
// D2/D3 integration golden: the leasing number sitting under a PRECEDING
// role heading (no phone token of its own) must classify departmental with
// a RoleLabelRaw carrying the heading text, while the general header tel:
// (no role context) stays general.
func TestCollectSiteNumbers_P45suShape_LeasingDeskDepartmental(t *testing.T) {
	t.Parallel()
	doc := docFromHTML(t, p45suShapeHTML)
	nums := CollectSiteNumbers(doc, false)

	general := findByValueSubstring(nums, "123-45-67")
	if general == nil {
		t.Fatalf("general header tel: candidate missing; got %+v", nums)
	}
	if general.Role != roleGeneral {
		t.Errorf("general candidate Role = %q, want roleGeneral", general.Role)
	}

	leasing := findByValueSubstring(nums, "956-18-40")
	if leasing == nil {
		t.Fatalf("leasing itemprop candidate missing; got %+v", nums)
	}
	if leasing.Role != roleDepartmental {
		t.Errorf("leasing candidate Role = %q, want roleDepartmental", leasing.Role)
	}
	if !strings.Contains(strings.ToLower(leasing.RoleLabelRaw), "аренд") {
		t.Errorf("leasing candidate RoleLabelRaw = %q, want it to contain %q", leasing.RoleLabelRaw, "аренд")
	}
}

// TestCollectSiteNumbers_AllUnlabeledPhonesStayGeneral is the anti-vacuous
// negative control: two bare tel: numbers with zero role context anywhere
// nearby must BOTH stay roleGeneral — proving the new fields never demote a
// candidate merely for lacking a label (the HARD invariant).
func TestCollectSiteNumbers_AllUnlabeledPhonesStayGeneral(t *testing.T) {
	t.Parallel()
	const html = `<!doctype html><html><body>
<header><a href="tel:+78121234567">+7 (812) 123-45-67</a></header>
<footer><a href="tel:+79219561840">+7 (921) 956-18-40</a></footer>
</body></html>`
	doc := docFromHTML(t, html)
	nums := CollectSiteNumbers(doc, false)
	if len(nums) != 2 {
		t.Fatalf("len(nums) = %d, want 2; got %+v", len(nums), nums)
	}
	for i := range nums {
		if nums[i].Role != roleGeneral {
			t.Errorf("nums[%d] = %+v, want Role=roleGeneral (unlabeled must never demote)", i, nums[i])
		}
	}
}

// TestPhoneRoleLabelText_PrecedingSiblingWithOwnPhone_NotBorrowed is the
// BLOCKER (review round 1, #1) regression fixture: a preceding sibling
// block that carries its OWN phone (a DIFFERENT number's label+value block,
// the common "departments as sibling blocks" RU venue layout) must NOT be
// borrowed as the role label for a later, unrelated general number.
// «Бухгалтерия: <a>111…</a>» precedes «Телефон: <a>222…</a>» — the second
// number's own label text ("Телефон:") carries no departmental token, and
// the department-labeled SIBLING BLOCK belongs to the FIRST number, not the
// second.
func TestPhoneRoleLabelText_PrecedingSiblingWithOwnPhone_NotBorrowed(t *testing.T) {
	t.Parallel()
	const html = `<!doctype html><html><body>
<div>Бухгалтерия: <a href="tel:+78121111111">+7 (812) 111-11-11</a></div>
<div>Телефон: <a href="tel:+78122222222">+7 (812) 222-22-22</a></div>
</body></html>`
	doc := docFromHTML(t, html)
	node := doc.Find(`a[href="tel:+78122222222"]`).First()
	if node.Length() == 0 {
		t.Fatalf("fixture setup: second tel: anchor not found")
	}
	got := phoneRoleLabelText(node)
	if strings.Contains(strings.ToLower(got), "бухгалтер") {
		t.Errorf("phoneRoleLabelText = %q, borrowed the PRECEDING sibling's own phone-block label (cross-block contamination)", got)
	}
}

// TestCollectSiteNumbers_DepartmentSiblingBlock_DoesNotContaminateNextGeneralLine
// is the BLOCKER (review round 1, #1) end-to-end integration golden: the
// venue's only public line (222) must NOT be wrongly demoted to
// roleDepartmental merely because a DIFFERENT department's phone block
// («Бухгалтерия: …111…») happens to sit as its preceding DOM sibling. If
// 222 is the venue's sole public line, a downstream card-phone picker that
// skips departmental numbers must not end up with NO phone at all.
func TestCollectSiteNumbers_DepartmentSiblingBlock_DoesNotContaminateNextGeneralLine(t *testing.T) {
	t.Parallel()
	const html = `<!doctype html><html><body>
<div>Бухгалтерия: <a href="tel:+78121111111">+7 (812) 111-11-11</a></div>
<div>Телефон: <a href="tel:+78122222222">+7 (812) 222-22-22</a></div>
</body></html>`
	doc := docFromHTML(t, html)
	nums := CollectSiteNumbers(doc, false)

	accounting := findByValueSubstring(nums, "111-11-11")
	if accounting == nil {
		t.Fatalf("accounting candidate missing; got %+v", nums)
	}
	if accounting.Role != roleDepartmental {
		t.Errorf("accounting candidate (111-11-11) Role = %q, want roleDepartmental (its OWN label)", accounting.Role)
	}

	general := findByValueSubstring(nums, "222-22-22")
	if general == nil {
		t.Fatalf("general candidate missing; got %+v", nums)
	}
	if general.Role != roleGeneral {
		t.Errorf("general candidate (222-22-22) Role = %q, want roleGeneral — must NOT borrow the PRECEDING accounting block's label (cross-block contamination)", general.Role)
	}
}

// TestCollectSiteNumbers_DedupSameNumber_GeneralReadingWinsRoleOverDepartmentalTierWinner
// is the MAJOR (review round 1, #3) dedup regression golden: the SAME
// digits are found TWICE on one page — once via a low-tier general reading
// (body prose, no role context) and once via a HIGHER-tier departmental
// reading (a contacts-region tel: under a preceding department heading).
// DedupeKeepStronger's tier-wins merge legitimately keeps the higher-tier
// reading's Value/Source/Anchored/Trustworthy — but Role must NOT ride
// along blindly: a number that reads GENERAL from ANY reading must stay
// general, since a strong "this is the public line" signal must never be
// silently overridden by a lower-precision reading's departmental misread
// just because that reading happened to win on an unrelated axis (DOM
// region tier).
func TestCollectSiteNumbers_DedupSameNumber_GeneralReadingWinsRoleOverDepartmentalTierWinner(t *testing.T) {
	t.Parallel()
	const html = `<!doctype html><html><body>
<article><p>Иногда до нас можно дозвониться по номеру <a href="tel:+78121234567">+7 (812) 123-45-67</a> в рабочее время.</p></article>
<header>
  <div class="h6">Отдел закупок</div>
  <a href="tel:+78121234567">+7 (812) 123-45-67</a>
</header>
</body></html>`
	doc := docFromHTML(t, html)
	nums := CollectSiteNumbers(doc, false)

	if len(nums) != 1 {
		t.Fatalf("len(nums) = %d, want 1 (same digits deduped); got %+v", len(nums), nums)
	}
	if nums[0].Role != roleGeneral {
		t.Errorf("deduped candidate Role = %q (RoleLabelRaw=%q), want roleGeneral — the tier-winning departmental reading must not override the co-existing general reading", nums[0].Role, nums[0].RoleLabelRaw)
	}
	// Sanity: the tier-winning reading really is the contacts-region one
	// (Anchored=true), proving this isn't vacuously passing because the
	// general reading also happened to win the tier merge.
	if !nums[0].Anchored {
		t.Errorf("deduped candidate Anchored = false, want true (the contacts-region tel: reading should still win the tier/trust merge for the non-Role fields)")
	}
}

// TestPhoneRole_IsDepartmental is the exported-predicate seam (review round
// 1, #5): a downstream consumer (go-wp's card-phone picker, task A2) must
// be able to gate on Role without comparing against an unexported
// PhoneRole constant.
func TestPhoneRole_IsDepartmental(t *testing.T) {
	t.Parallel()
	if roleGeneral.IsDepartmental() {
		t.Errorf("roleGeneral.IsDepartmental() = true, want false")
	}
	if !roleDepartmental.IsDepartmental() {
		t.Errorf("roleDepartmental.IsDepartmental() = false, want true")
	}
}

// TestCollectSiteNumbers_ExistingFixturesStayRoleGeneral_NoGoldenChurn is the
// D1 acceptance gate: every EXISTING golden/fixture-derived SiteNumbers fact
// (none of these fixtures carry any departmentalLabelTokens text anywhere —
// verified by grep before writing this test) must classify roleGeneral —
// proving the additive fields introduce zero classification churn against
// the pre-existing golden set. RoleLabelRaw itself is NOT asserted empty
// here: phoneRoleLabelText's nearest-non-empty-text scan may legitimately
// pick up nearby non-departmental prose (e.g. a business tagline) near a
// candidate that has no dedicated role heading — that text is harmless
// verbatim context for a downstream LLM precisely because it carries no
// departmental token, so it classifies general regardless of its content.
func TestCollectSiteNumbers_ExistingFixturesStayRoleGeneral_NoGoldenChurn(t *testing.T) {
	t.Parallel()
	for _, fixture := range []string{
		"eksimer.html", "lazermed.html", "royal-wed.html",
	} {
		t.Run(fixture, func(t *testing.T) {
			t.Parallel()
			doc := docFromFixture(t, fixture)
			nums := CollectSiteNumbers(doc, false)
			if len(nums) == 0 {
				t.Fatalf("fixture %s: no SiteNumbers found (test setup broken)", fixture)
			}
			for i := range nums {
				if nums[i].Role != roleGeneral {
					t.Errorf("fixture %s: nums[%d] = %+v, want Role=roleGeneral (no golden churn)", fixture, i, nums[i])
				}
			}
		})
	}
}

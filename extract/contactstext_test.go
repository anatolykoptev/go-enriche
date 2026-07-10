package extract

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// docFromHTML parses an inline HTML string into a *goquery.Document, failing
// the test on a parse error. Used by the plain-text-contacts finder tests,
// which craft small fixtures with the exact real-world DOM shapes rather than
// carrying a 300KB golden capture.
func docFromHTML(t *testing.T, html string) *goquery.Document {
	t.Helper()
	doc, err := documentFromHTML(html)
	if err != nil || doc == nil {
		t.Fatalf("parse inline HTML: %v", err)
	}
	return doc
}

// bankrotHeaderHTML reproduces the exact header DOM of bankrot-v-spb.ru/kontakty/
// (live capture 2026-07-07): a «Телефоны» label and its value block are SEPARATE
// sibling subtrees (label in b-header-6__one-line, value in b-header-6__two-line),
// and BOTH numbers are PLAIN TEXT — there is no tel: href and no itemprop on the
// 812 number at all. The real published landline +7 (812) 507-83-55 lives only
// here, so every DOM finder that reads tel:/microdata/JSON-LD misses it — the
// exact anti-fabrication false-negative this finder closes.
const bankrotHeaderHTML = `<!doctype html><html><body>
<div class="b-header-6__contacts-text">
  <div class="b-header-6__one-line">
    <div class="content-editor" data-id="4"><div class="h6">Телефоны</div></div>
  </div>
  <div class="b-header-6__two-line">
    <div class="content-editor" data-id="5"><div class="h5">+7 (812) 507-83-55<br/>+7 (967) 533-44-61</div></div>
  </div>
</div>
</body></html>`

// bankrotBodyHTML reproduces the body-prose contacts DOM of the same page: the
// label «Телефоны» sits in an inline <strong> and the two numbers are plain-text
// nodes of the same <p>, separated by <br/>. A sibling <p> holds the «Реквизиты»
// block (ИНН/ОГРН/КПП/БИК/р.с/к.с) whose digit-runs must NEVER be read as phones.
const bankrotBodyHTML = `<!doctype html><html><body>
<div class="entry-content">
  <p>Консультации проводятся по предварительной записи.<br/>
    <strong>Телефоны</strong>:<br/>
    +7 (812) 507-83-55<br/>
    +7 (967) 533-44-61</p>
  <p><strong>Реквизиты</strong>:&nbsp;БАНК: ПАО «Банк «Санкт-Петербург»<br/>
    ИНН 7825346838<br/>
    КПП 784001001 ОГРН 1037858001181,<br/>
    р/с 40703810319000003849,<br/>
    к/с 30101810900000000790, БИК 044030790</p>
</div>
</body></html>`

// TestCollectSiteNumbers_PlainTextContacts_HeaderSiblingBlock is the headline
// RED case: the published +7 (812) 507-83-55 landline exists ONLY as plain text
// in a header contacts block, in a sibling subtree from its «Телефоны» label, with
// no tel: href and no microdata. It MUST surface in SiteNumbers as a
// contacts-tier, anchored, trustworthy candidate (clean page, no DNI).
func TestCollectSiteNumbers_PlainTextContacts_HeaderSiblingBlock(t *testing.T) {
	t.Parallel()
	doc := docFromHTML(t, bankrotHeaderHTML)
	nums := CollectSiteNumbers(doc, false)

	landline := findByValueSubstring(nums, "507-83-55")
	if landline == nil {
		t.Fatalf("published 812 landline (plain text, label-anchored) missing from SiteNumbers; got %+v", nums)
	}
	if landline.Source != regionContacts {
		t.Errorf("812 landline Source = %q, want %q (contacts-tier plain text)", landline.Source, regionContacts)
	}
	if !landline.Anchored {
		t.Errorf("812 landline Anchored = false, want true")
	}
	if landline.DNI {
		t.Errorf("812 landline DNI = true, want false (no DNI vendor on this page)")
	}
	if !landline.Trustworthy {
		t.Errorf("812 landline Trustworthy = false, want true (anchored plain-text contact, no DNI)")
	}

	// The mobile 967 printed in the same block must also surface.
	if mobile := findByValueSubstring(nums, "533-44-61"); mobile == nil {
		t.Errorf("967 mobile (plain text, same contacts block) missing from SiteNumbers; got %+v", nums)
	}
}

// TestCollectSiteNumbers_PlainTextContacts_BodyProseWithRequisites is the
// anti-vacuous NEGATIVE control: the same two phones printed as body prose under
// an inline «Телефоны» label MUST be extracted, while the neighbouring «Реквизиты»
// digit-runs (ИНН 7825346838, ОГРН 1037858001181, КПП 784001001, р/с/к/с account
// numbers, БИК 044030790) MUST NOT be read as phones. A naive plain-text scan that
// grabbed every digit-run near a contacts block would fabricate a phone from a bank
// account number — this locks that it does not.
func TestCollectSiteNumbers_PlainTextContacts_BodyProseWithRequisites(t *testing.T) {
	t.Parallel()
	doc := docFromHTML(t, bankrotBodyHTML)
	nums := CollectSiteNumbers(doc, false)

	if landline := findByValueSubstring(nums, "507-83-55"); landline == nil {
		t.Fatalf("published 812 landline (body prose, label-anchored) missing from SiteNumbers; got %+v", nums)
	}
	if mobile := findByValueSubstring(nums, "533-44-61"); mobile == nil {
		t.Errorf("967 mobile (body prose) missing from SiteNumbers; got %+v", nums)
	}

	// Anti-fabrication: none of the requisites identifiers may appear as a phone.
	for _, forbidden := range []struct{ label, digits string }{
		{"ИНН", "7825346838"},
		{"ОГРН", "1037858001181"},
		{"КПП", "784001001"},
		{"р/с", "40703810319000003849"},
		{"к/с", "30101810900000000790"},
		{"БИК", "044030790"},
	} {
		for i := range nums {
			if DigitsOnly(nums[i].Value) == forbidden.digits {
				t.Errorf("%s identifier %s was fabricated as a phone: %+v", forbidden.label, forbidden.digits, nums[i])
			}
		}
	}
}

// TestCollectSiteNumbers_PlainTextContacts_SharedScopeRequisitesSkipped is the
// NON-VACUOUS anti-fabrication control for containsIDLabel: unlike the
// body-prose case (where «Реквизиты» sits in a SEPARATE <p>, so phoneValueScope
// returns before the ID guard is ever consulted), here a «Тел.» label, a real
// phone, and an ИНН digit-run share ONE inline block — the harvest node
// therefore carries the ID label, and the fail-closed guard must skip the WHOLE
// block. Both the ИНН and the co-located phone stay out of the trust set
// (prefer-false-negative): this exercises containsIDLabel on the path where it
// is the sole thing standing between the ИНН and a fabricated phone.
func TestCollectSiteNumbers_PlainTextContacts_SharedScopeRequisitesSkipped(t *testing.T) {
	t.Parallel()
	const html = `<!doctype html><html><body>
<div class="contacts"><p>Тел.: +7 (812) 507-83-55 ИНН 7825346838 ОГРН 1037858001181</p></div>
</body></html>`
	doc := docFromHTML(t, html)
	nums := CollectSiteNumbers(doc, false)

	if got := findByValueSubstring(nums, "507-83-55"); got != nil {
		t.Errorf("phone sharing an inline scope with an ИНН must be skipped whole (fail-closed); got %+v", *got)
	}
	for _, bad := range []string{"7825346838", "1037858001181"} {
		for i := range nums {
			if DigitsOnly(nums[i].Value) == bad {
				t.Errorf("identifier %s in a phone-labeled block was fabricated as a phone: %+v", bad, nums[i])
			}
		}
	}
}

// TestCollectSiteNumbers_PlainTextContacts_SvgIconTextNotHarvested proves the
// noise strip (removeNoiseSelectors, via stripNoiseClone) drops inline <svg>
// text before the scan: a phone label whose value block also contains an <svg>
// with a digit-run in a <text> node must NOT read that icon text as a phone.
// Guards the reuse-reviewer MEDIUM-1 leak (the hand-rolled strip dropped svg).
func TestCollectSiteNumbers_PlainTextContacts_SvgIconTextNotHarvested(t *testing.T) {
	t.Parallel()
	const html = `<!doctype html><html><body>
<div class="footer"><div class="h6">Телефон</div>
<div class="val"><svg><text>+7 (495) 111-22-33</text></svg>+7 (812) 507-83-55</div></div>
</body></html>`
	doc := docFromHTML(t, html)
	nums := CollectSiteNumbers(doc, false)

	if got := findByValueSubstring(nums, "111-22-33"); got != nil {
		t.Errorf("<svg> icon text must be stripped before the phone scan; leaked %+v", *got)
	}
	if got := findByValueSubstring(nums, "507-83-55"); got == nil {
		t.Errorf("real plain-text 812 phone should still be harvested; got %+v", nums)
	}
}

// TestCollectSiteNumbers_PlainTextContacts_NoLabelNoExtraction proves the finder
// is label-anchored, not a blanket digit scan: a valid-RU-phone-shaped plain-text
// number with NO phone-context label nearby (here, in an unrelated article
// paragraph) is NOT extracted. Prefer-false-negative: an unanchored number stays
// out of the trust set.
func TestCollectSiteNumbers_PlainTextContacts_NoLabelNoExtraction(t *testing.T) {
	t.Parallel()
	const html = `<!doctype html><html><body>
<article><p>В 2019 году компания привлекла 1 000 000 рублей инвестиций и переехала
в офис по адресу ул. Кузнецовская, где её случайно можно набрать как +7 (812) 507-83-55.</p></article>
</body></html>`
	doc := docFromHTML(t, html)
	nums := CollectSiteNumbers(doc, false)
	if got := findByValueSubstring(nums, "507-83-55"); got != nil {
		t.Errorf("unanchored body-prose number must NOT be extracted (no phone label near it); got %+v", *got)
	}
}

// TestCollectSiteNumbers_PlainTextContacts_DNIStaysUntrustworthy locks the DNI
// contract for the new source: a label-anchored plain-text phone on a page that
// carries pagePoisoned=true (a raw-fetch DNI/call-tracking verdict carried
// forward) is Anchored but NOT Trustworthy — only a DNI-immune social-link phone
// survives the fail-closed gate, exactly as for every other anchored source.
func TestCollectSiteNumbers_PlainTextContacts_DNIStaysUntrustworthy(t *testing.T) {
	t.Parallel()
	doc := docFromHTML(t, bankrotHeaderHTML)
	nums := CollectSiteNumbers(doc, true) // pagePoisoned=true

	landline := findByValueSubstring(nums, "507-83-55")
	if landline == nil {
		t.Fatalf("812 landline missing under pagePoisoned; got %+v", nums)
	}
	if !landline.Anchored {
		t.Errorf("812 landline Anchored = false, want true (region is unchanged by DNI)")
	}
	if !landline.DNI {
		t.Errorf("812 landline DNI = false, want true (pagePoisoned carried forward)")
	}
	if landline.Trustworthy {
		t.Errorf("812 landline Trustworthy = true, want false (DNI active — a rotating text slot is not trustworthy)")
	}
}

// generalAndLeasingPlainTextHTML is the D2/D3 plain-text integration
// fixture: a general «Телефоны:» number with no role context, plus a
// leasing-desk number under a PRECEDING «Аренда мест» heading that itself
// carries no phone token (so it is never itself picked up as a
// rePhoneLabel-anchored label node — only the sibling «Тел.:» block is).
const generalAndLeasingPlainTextHTML = `<!doctype html><html><body>
<div class="contacts">
  <p>Телефоны: +7 (812) 111-22-33</p>
  <div class="dept">
    <div class="h6">Аренда мест</div>
    <p>Тел.: +7 (812) 444-55-66</p>
  </div>
</div>
</body></html>`

// TestCollectSiteNumbers_PlainTextContacts_GeneralAndLeasingRoles is the D2/D3
// plain-text integration golden: contactsTextCandidates must scan the
// NUMBER's neighbourhood (the live phoneValueScope node), not just the
// in-hand «Тел.:» label node, to pick up the preceding «Аренда мест»
// heading — the general number (no role context) must stay roleGeneral
// while the leasing number classifies roleDepartmental.
func TestCollectSiteNumbers_PlainTextContacts_GeneralAndLeasingRoles(t *testing.T) {
	t.Parallel()
	doc := docFromHTML(t, generalAndLeasingPlainTextHTML)
	nums := CollectSiteNumbers(doc, false)

	general := findByValueSubstring(nums, "111-22-33")
	if general == nil {
		t.Fatalf("general plain-text candidate missing; got %+v", nums)
	}
	if general.Role != roleGeneral {
		t.Errorf("general candidate Role = %q, want roleGeneral (RoleLabelRaw=%q)", general.Role, general.RoleLabelRaw)
	}

	leasing := findByValueSubstring(nums, "444-55-66")
	if leasing == nil {
		t.Fatalf("leasing plain-text candidate missing; got %+v", nums)
	}
	if leasing.Role != roleDepartmental {
		t.Errorf("leasing candidate Role = %q, want roleDepartmental", leasing.Role)
	}
	if !strings.Contains(strings.ToLower(leasing.RoleLabelRaw), "аренд") {
		t.Errorf("leasing candidate RoleLabelRaw = %q, want it to contain %q", leasing.RoleLabelRaw, "аренд")
	}
}

// p45SuLeasingContactsHTML reproduces the exact plain-text contacts DOM of
// p45.su's /контакты page (live capture, task fix/phone-role-from-narrowest-
// node): a general LOCAL 7-digit line («…телефону 242-55-38…», no +7/8
// prefix, so rePhone never matches it) sits in the same coarse .content
// block as the only FULL number, the leasing desk's «+7-921-941-10-83», two
// <p>s below a «По вопросам аренды свободных торговых мест…» heading that
// itself carries no phone token of its own (so it is never itself picked up
// as a rePhoneLabel-anchored label — only the local-number <p> and the
// heading <p> are, since «телефонам» also contains the «телефон» stem).
//
// phoneValueScope must climb past both the label's own block (local number,
// no rePhone match) and its immediate next sibling (the heading, also no
// match) to the coarse .content ancestor — whose own PRECEDING sibling in
// the live document is #mobile_header, a <div> wrapping a
// <select class="menu"> whose menu OPTIONs are not themselves flagged nav
// boilerplate (isNavigationBoilerplate inspects only the CANDIDATE node's
// own tag/class/id, and #mobile_header itself carries neither "menu" nor
// "nav" in its id — only its descendant <select> does). Reading a role
// label from the coarse .content scope itself therefore leaks that menu
// text in as a wrongly-general role context — the exact live bug
// narrowestLivePhoneNode (contactstext.go) fixes by first descending to the
// narrowest live phone-bearing node (the leasing <p> itself) before
// phoneRoleLabelText ever walks outward from it.
const p45SuLeasingContactsHTML = `<!doctype html><html><body>
<div id="mobile_header">
  <select class="menu"><option>— Main Menu —</option><option>Главная</option><option>Покупателям</option><option>Контакты</option></select>
</div>
<div id="content"><div class="content">
  <p>Вы можете позвонить по контактному телефону 242-55-38 в любое время.</p>
  <p>По вопросам аренды свободных торговых мест обращайтесь по телефонам:</p>
  <p>+7-921-941-10-83 Леонид Анатольевич</p>
</div></div>
</body></html>`

// TestCollectSiteNumbers_PlainTextContacts_CoarseScopeLeasingNotMenu is the
// p45.su regression (primary): the leasing number +7-921-941-10-83 must
// classify roleDepartmental with a RoleLabelRaw carrying «аренд», NOT
// roleGeneral off the #mobile_header menu text a coarse-scope role-label
// read would otherwise leak in. Before the fix (roleLabel read from the
// coarse `scope` itself, not narrowestLivePhoneNode(scope)) this MUST FAIL:
// phoneRoleLabelText climbs from the coarse .content scope, finds
// #mobile_header as the closest preceding sibling at the #content level,
// and — since #mobile_header itself is not recognized as nav boilerplate —
// reads its menu text as the role label, classifying general instead of
// departmental and never containing «аренд».
func TestCollectSiteNumbers_PlainTextContacts_CoarseScopeLeasingNotMenu(t *testing.T) {
	t.Parallel()
	doc := docFromHTML(t, p45SuLeasingContactsHTML)
	nums := CollectSiteNumbers(doc, false)

	leasing := findByValueSubstring(nums, "941-10-83")
	if leasing == nil {
		t.Fatalf("leasing number +7-921-941-10-83 missing from SiteNumbers; got %+v", nums)
	}
	if !leasing.Role.IsDepartmental() {
		t.Errorf("leasing candidate Role = %q (IsDepartmental=false), want departmental; RoleLabelRaw=%q — coarse-scope climb must not read the #mobile_header menu as the role context", leasing.Role, leasing.RoleLabelRaw)
	}
	if !strings.Contains(strings.ToLower(leasing.RoleLabelRaw), "аренд") {
		t.Errorf("leasing candidate RoleLabelRaw = %q, want it to contain %q (the «аренды» heading), not the #mobile_header menu text", leasing.RoleLabelRaw, "аренд")
	}
	if strings.Contains(leasing.RoleLabelRaw, "Главная") || strings.Contains(leasing.RoleLabelRaw, "Main Menu") {
		t.Errorf("leasing candidate RoleLabelRaw = %q leaked the #mobile_header menu text", leasing.RoleLabelRaw)
	}
}

// TestCollectSiteNumbers_PlainTextContacts_DirectBlockRoleUnchanged is the
// no-regression control: when the phone value sits in the label's OWN
// block (phoneValueScope resolves at depth 0, no climb needed),
// narrowestLivePhoneNode(scope) has no phone-bearing child element to
// descend into and returns scope itself unchanged — so the role-label read
// and the extracted number are identical to pre-fix behaviour. Must stay
// roleGeneral (no departmental context anywhere in this fixture) and the
// number must still be extracted.
func TestCollectSiteNumbers_PlainTextContacts_DirectBlockRoleUnchanged(t *testing.T) {
	t.Parallel()
	const html = `<!doctype html><html><body>
<div class="contacts"><p>Телефон: +7 (812) 234-56-78</p></div>
</body></html>`
	doc := docFromHTML(t, html)
	nums := CollectSiteNumbers(doc, false)

	got := findByValueSubstring(nums, "234-56-78")
	if got == nil {
		t.Fatalf("direct-block phone missing from SiteNumbers; got %+v", nums)
	}
	if got.Role != roleGeneral {
		t.Errorf("direct-block candidate Role = %q, want roleGeneral (RoleLabelRaw=%q)", got.Role, got.RoleLabelRaw)
	}
}

// TestCollectSiteNumbers_PlainTextContacts_SharedValueBlockRoleShared is the
// multi-number shared-block invariant: when a phone-value block holds TWO
// numbers split across text nodes around a <br/> (neither number's own
// child element carries the full match, since they are bare text siblings
// of the <br/>), narrowestLivePhoneNode must find no single qualifying
// child and stop AT the shared block — not silently resolve to only one of
// the two numbers. Both numbers must still be harvested, and both must
// share the SAME RoleLabelRaw (proving one role-label lookup, computed
// once per scope, covers the whole shared block — not a fresh lookup per
// number).
func TestCollectSiteNumbers_PlainTextContacts_SharedValueBlockRoleShared(t *testing.T) {
	t.Parallel()
	const html = `<!doctype html><html><body>
<div class="contacts">
  <p>Работаем ежедневно с 10 до 22</p>
  <p>Телефоны: +7 (812) 111-11-11<br/>+7 (812) 222-22-22</p>
</div>
</body></html>`
	doc := docFromHTML(t, html)
	nums := CollectSiteNumbers(doc, false)

	first := findByValueSubstring(nums, "111-11-11")
	if first == nil {
		t.Fatalf("first shared-block number missing; got %+v", nums)
	}
	second := findByValueSubstring(nums, "222-22-22")
	if second == nil {
		t.Fatalf("second shared-block number missing; got %+v", nums)
	}
	if first.RoleLabelRaw != second.RoleLabelRaw {
		t.Errorf("shared value-block numbers got DIFFERENT RoleLabelRaw: %q vs %q, want identical (one role-label lookup per scope)", first.RoleLabelRaw, second.RoleLabelRaw)
	}
	if first.RoleLabelRaw == "" || !strings.Contains(first.RoleLabelRaw, "ежедневно") {
		t.Errorf("shared value-block RoleLabelRaw = %q, want it to contain %q", first.RoleLabelRaw, "ежедневно")
	}
}

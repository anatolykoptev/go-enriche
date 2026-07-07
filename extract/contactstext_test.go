package extract

import (
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

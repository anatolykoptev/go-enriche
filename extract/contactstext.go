package extract

import (
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// rePhoneLabel matches a phone-context label that introduces a PLAIN-TEXT phone
// number: «телефон(ы)», «тел.»/«тел:», «моб.»/«моб:», «сотовый», «звоните»,
// «viber»/«вайбер», WhatsApp, and the Latin «phone»/«tel.». It is the positive
// anchor the plain-text finder requires — a number is read as a contact ONLY
// when it sits in the DOM neighbourhood of such a label.
//
// The «тел»/«моб» arms are punctuation-anchored («тел.»/«тел:»/«моб.») so a
// common word merely CONTAINING those letters — «строитель», «предваритель­ной»,
// «мобильность» — does NOT match; only the phone-number abbreviation does. The
// full «телефон» arm needs no such anchor: no non-phone Russian word contains it.
var rePhoneLabel = regexp.MustCompile(`(?i)(?:телефон|тел[.:]|моб[.:]|сотов|звони|вайбер|viber|whats\s?app|phone|\btel[.:])`)

// idLabelTokens are identifier/legal-registry labels whose neighbouring digit
// runs (a taxpayer/registry/bank-account number) are NEVER a phone. A value
// scope whose text carries any of these is skipped entirely — a fail-closed
// belt-and-suspenders behind ValidatePhone's Rossvyaz area-code ceiling (which
// already rejects an ИНН/ОГРН/БИК/account number on its own), so a «Реквизиты»
// block that a phone label happens to sit near can never leak a fabricated
// phone. Prefer-false-negative: skipping a whole mixed block is the safe side.
var idLabelTokens = []string{
	"инн", "огрн", "кпп", "бик", "р/с", "к/с", "расчётн", "расчетн",
	"кор.счёт", "кор.счет", "лиценз", "окпо", "октмо", "окато", "iban", "swift",
}

// maxPhoneLabelAncestor bounds how far up from a phone-label node the value
// scope is searched. Small on purpose: the phone value for a label always sits
// in the label's own block, an adjacent sibling block, or a shared small
// contacts wrapper (the label→value sibling-block layout) — never many levels
// away. Keeping the walk local stops a distant, unrelated section (e.g. a
// «Реквизиты» block that shares a far ancestor with the contacts block) from
// ever becoming a phone label's value scope.
const maxPhoneLabelAncestor = 4

// nonTextTags are elements whose text content is code/data, never
// human-readable page copy: a <script> (inline JS, a JSON-LD island, a
// bespoke branch-locator blob) or <style>/<noscript>. goquery's .Text()
// concatenates their content indiscriminately, so the plain-text finder must
// strip them before scanning — otherwise a "phone" JSON key or a digit-run
// buried in a script (a lat/lng/id) would be read as a page-visible phone (the
// branchjson-junk / schema.org JSON-LD false-positive vectors). This finder
// reads ONLY what a human sees on the page.
const nonTextTags = "script, style, noscript"

// contactsTextCandidates finds phone numbers printed as PLAIN TEXT inside a
// genuine phone-labeled contacts region — the class every other finder in this
// package misses because it reads only structured/linked phones (tel: hrefs,
// [itemprop=telephone], og: meta, JSON-LD, branch/schema JSON). A firm whose
// real published landline appears only as text under a «Телефоны» heading —
// with no tel: href and no microdata — is otherwise invisible to SiteNumbers,
// so wp_verify reports that genuinely-published, statically-hosted number as
// "wrong / not found" (the bankrot-v-spb.ru/kontakty/ false-negative this
// finder closes). Emitted at tierContacts (naturalTier), so CollectSiteNumbers
// tags it Source="contacts", Anchored, and — on a clean page — Trustworthy;
// on a DNI/call-tracking page the shared dniTrustworthy gate still marks it
// untrustworthy, exactly as for a contacts-region tel:.
//
// It is a GAP-FILLER: `existing` carries every candidate the structured finders
// already produced, and a plain-text number whose canonical key (8→7 normalized,
// so "8 (812) …" and "+7 (812) …" collapse) is already present is SKIPPED. This
// keeps the finder from (a) re-emitting a differently-formatted duplicate of a
// tel:/microdata number the set already has, and (b) UPGRADING a body/demoted
// structured tier to contacts merely because the same digits also appear as text.
// It only ever ADDS numbers no structured finder found — exactly the false-
// negative it exists to close.
//
// Anti-fabrication is a hard, ordered boundary — a number is read ONLY when:
//  1. it is page-visible text — <script>/<style>/<noscript> content is stripped
//     before any scan (never read a JSON-LD/branch-JSON blob as page copy);
//  2. its DOM neighbourhood carries a phone-context label (rePhoneLabel) — the
//     positive anchor; a bare digit run in article prose, with no phone label
//     near it, is never inspected (prefer-false-negative);
//  3. the value scope is NOT nested inside a named call-tracking widget
//     (callTrackingSelectors) — never launder a rotating tracking number into a
//     contacts-tier candidate;
//  4. the value scope carries no identifier/registry label (idLabelTokens) — a
//     «Реквизиты» ИНН/ОГРН/БИК/account block is skipped, not scanned;
//  5. every surviving match passes the SAME ValidatePhone Rossvyaz
//     numbering-plan ceiling (via makeCandidate) every other finder obeys — an
//     ИНН/account-number digit run that is not a valid RU phone is dropped.
//
// Like branchJSONCandidates / schemaPlaceCandidates, this feeds the SiteNumbers
// trust SET only (CollectSiteNumbers), never collectPhoneCandidates — so the
// single-winner city-guide pick (pickPhoneCandidate / Facts.Phone) stays
// byte-unchanged and every existing golden is unaffected.
func contactsTextCandidates(doc *goquery.Document, existing []phoneCandidate) []phoneCandidate {
	if doc == nil {
		return nil
	}
	// Canonical key set of everything the structured finders already found;
	// also serves as the intra-finder dedup set below.
	have := make(map[string]bool, len(existing))
	for _, c := range existing {
		have[canonicalPhoneKey(c.value)] = true
	}

	var out []phoneCandidate
	// Never treat a code/data element as a label node (gate 1): its text is JS/
	// JSON, not page copy, and a "phone" JSON key would otherwise anchor a scan.
	doc.Find("*").Not(nonTextTags).Each(func(_ int, s *goquery.Selection) {
		// s must be a phone-LABEL node: its OWN direct text (not descendants',
		// so the search lands on the literal label element — mirrors
		// ExtractVisibleHours) carries a phone-context label.
		if !rePhoneLabel.MatchString(ownTextOf(s)) {
			return
		}
		scope := phoneValueScope(s)
		if scope == nil {
			return
		}
		// Gate 3: never read a rotating call-tracking widget's number as a
		// static contact.
		if scope.Closest(callTrackingSelectors).Length() > 0 {
			return
		}
		text := textNoScript(scope)
		// Gate 4: an identifier/registry block near a phone label is skipped
		// whole (fail-closed).
		if containsIDLabel(text) {
			return
		}
		// Gate 5: ValidatePhone (via makeCandidate) is the sole validity arbiter.
		for _, m := range rePhone.FindAllString(text, -1) {
			c, ok := makeCandidate(m, tierContacts)
			if !ok {
				continue
			}
			key := canonicalPhoneKey(c.value)
			if key == "" || have[key] {
				continue // already covered structurally, or already emitted here
			}
			have[key] = true
			out = append(out, c)
		}
	})
	return out
}

// phoneValueScope returns the element whose page-visible text carries the phone
// value for a phone-label node, or nil when none is found within
// maxPhoneLabelAncestor levels. It walks up from the label; at each level it
// prefers the node's own subtree, then the node's immediate next sibling —
// covering both the inline layout («Тел.: +7 …» in one block) and the
// label→value sibling-block layout (a «Телефоны» heading whose numbers live in
// the adjacent value block, the bankrot-v-spb.ru header shape). The first scope
// whose stripped text holds a phone match wins; a distant ancestor is never
// reached (bounded walk). Script/style text is stripped before every match so a
// buried blob never resolves a scope.
func phoneValueScope(label *goquery.Selection) *goquery.Selection {
	node := label
	for depth := 0; depth <= maxPhoneLabelAncestor && node.Length() > 0; depth++ {
		if rePhone.MatchString(textNoScript(node)) {
			return node
		}
		if sib := node.Next(); sib.Length() > 0 && rePhone.MatchString(textNoScript(sib)) {
			return sib
		}
		node = node.Parent()
	}
	return nil
}

// textNoScript returns sel's text with all code/data elements (nonTextTags)
// removed — the page-visible text only. Operates on a detached clone so the
// live document is never mutated.
func textNoScript(sel *goquery.Selection) string {
	c := sel.Clone()
	c.Find(nonTextTags).Remove()
	return c.Text()
}

// canonicalPhoneKey normalizes a phone string to a dedup key: digits only, with
// a leading RU trunk «8» rewritten to the «7» country code so the same number
// written "8 (812) …" and "+7 (812) …" collapses to one key. Used to skip a
// plain-text number a structured finder already produced (which may carry the
// other prefix form) and to dedup within the finder.
func canonicalPhoneKey(value string) string {
	d := DigitsOnly(value)
	if len(d) == 11 && d[0] == '8' {
		return "7" + d[1:]
	}
	return d
}

// containsIDLabel reports whether text carries any identifier/registry label
// (idLabelTokens) — matched case-insensitively as a substring, since the
// fail-closed direction (skip a block that might be requisites) is the safe
// one for anti-fabrication.
func containsIDLabel(text string) bool {
	lower := strings.ToLower(text)
	for _, tok := range idLabelTokens {
		if strings.Contains(lower, tok) {
			return true
		}
	}
	return false
}

package extract

import (
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// rePhoneLabel matches a phone-context label that introduces a PLAIN-TEXT phone
// number: «телефон(ы)», «тел.»/«тел:», «моб.»/«моб:», «сотовый», «звоните»,
// «viber»/«вайбер», WhatsApp, and the Latin «telephone»/«phone»/«tel.». It is
// the positive anchor the plain-text finder requires — a number is read as a
// contact ONLY when it sits in the DOM neighbourhood of such a label.
//
// Every arm is anchored so a common word merely CONTAINING the letters does NOT
// match: «тел»/«моб» need trailing punctuation («тел.»/«тел:»/«моб.»), so
// «строитель»/«предварительной»/«мобильность» never match; the Latin «phone»/
// «telephone» arms are \b-bounded, so iPhone/smartphone/headphone never match.
// The full «телефон» arm needs no anchor — no non-phone Russian word contains it.
var rePhoneLabel = regexp.MustCompile(`(?i)(?:телефон|тел[.:]|моб[.:]|сотов|звони|вайбер|viber|whats\s?app|\btelephone\b|\bphone\b|\btel[.:])`)

// idLabelTokens are identifier/legal-registry labels whose neighbouring digit
// runs (a taxpayer/registry/bank-account number) are NEVER a phone. A harvest
// node whose text carries any of these is skipped WHOLE (fail-closed) — a
// belt-and-suspenders behind ValidatePhone's Rossvyaz area-code ceiling (which
// already rejects an ИНН/ОГРН/БИК/account number on its own), so a «Реквизиты»
// line that a phone label happens to sit near can never leak a fabricated phone.
//
// Matched CASE-SENSITIVELY (see containsIDLabel): the registry abbreviations are
// conventionally uppercase in requisites blocks («ИНН»/«ОГРН»/«КПП»/«БИК»), so
// an exact-case match anchors them without a bare-substring "инн" wrongly firing
// inside инновация/старинный/финны. The slash forms («р/с»/«к/с») and lowercase
// «лиценз»/«расч…счёт» are already distinctive enough to match as-is.
var idLabelTokens = []string{
	"ИНН", "ОГРН", "ОГРНИП", "КПП", "БИК", "ОКПО", "ОКАТО", "ОКТМО", "IBAN", "SWIFT",
	"р/с", "к/с", "р/сч", "к/сч", "расчётный счёт", "расчетный счет",
	"расчётный счет", "лиценз",
}

// maxPhoneLabelAncestor bounds how far up from a phone-label node the value
// scope is searched. Small on purpose: the phone value for a label always sits
// in the label's own block, an adjacent sibling block, or a shared small
// contacts wrapper (the label→value sibling-block layout) — never many levels
// away. Keeping the walk local stops a distant, unrelated section (e.g. a
// «Реквизиты» block that shares a far ancestor with the contacts block) from
// ever becoming a phone label's value scope.
const maxPhoneLabelAncestor = 4

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
//  1. it is page-visible text — the never-a-contact-value noise elements
//     (removeNoiseSelectors: script/style/noscript/template/svg/head) are
//     stripped via stripNoiseClone before any scan, so a JSON-LD island, a
//     bespoke branch-JSON blob, or an inline <svg>/<template> icon's text is
//     never read as page copy;
//  2. its DOM neighbourhood carries a phone-context label (rePhoneLabel) — the
//     positive anchor; a bare digit run in article prose, with no phone label
//     near it, is never inspected (prefer-false-negative);
//  3. the harvest is bounded to the NARROWEST phone-bearing node within the
//     scope (narrowestPhoneNode), so a broad ancestor cannot pull an unrelated
//     partner/aggregator/fax line into the trust set;
//  4. the harvest node is NOT nested inside a named call-tracking widget
//     (callTrackingSelectors) — never launder a rotating tracking number into a
//     contacts-tier candidate;
//  5. the harvest node carries no identifier/registry label (idLabelTokens) — a
//     «Реквизиты» ИНН/ОГРН/БИК/account block is skipped whole, not scanned;
//  6. every surviving match passes the SAME ValidatePhone Rossvyaz
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
	// Gate 1 (label side): never treat a code/data element as a label node —
	// its text is JS/JSON/markup, not page copy, and a "phone" JSON key would
	// otherwise anchor a scan.
	doc.Find("*").Not(removeNoiseSelectors).Each(func(_ int, s *goquery.Selection) {
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
		// Gate 4: never read a rotating call-tracking widget's number as a
		// static contact.
		if scope.Closest(callTrackingSelectors).Length() > 0 {
			return
		}
		// Role-label context is read from the LIVE `scope` node — not the
		// in-hand phone-label node `s` (rePhoneLabel only anchors on a
		// phone-token word like «Тел.», never a role/department word) and
		// not the detached, noise-stripped `harvest` clone computed below
		// (Parent()/PrevAll() cannot walk a cloned, detached node). One
		// role-label lookup covers every phone match this scope yields
		// below — a multi-number value block («+7 (812) …<br/>+7 (967) …»)
		// shares one department context. See phoneRoleLabelText's doc
		// comment (phonerole.go) for why this is a PRECEDING-context scan,
		// not a mirror of phoneValueScope's own FOLLOWING-looking walk.
		roleLabel := phoneRoleLabelText(scope)
		// Gate 1 (value side) + gate 3: strip noise once, then bound the harvest
		// to the narrowest phone-bearing node so a broad ancestor scope does not
		// pull an unrelated far number into the set.
		harvest := narrowestPhoneNode(stripNoiseClone(scope))
		text := harvest.Text()
		// Gate 5: an identifier/registry block is skipped whole (fail-closed).
		if containsIDLabel(text) {
			return
		}
		// Gate 6: ValidatePhone (via makeCandidate) is the sole validity arbiter.
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
			c.roleLabel = roleLabel
			out = append(out, c)
		}
	})
	return out
}

// phoneValueScope returns the LIVE element whose page-visible text carries the
// phone value for a phone-label node, or nil when none is found within
// maxPhoneLabelAncestor levels. It walks up from the label; at each level it
// prefers the node's own subtree, then the node's immediate next sibling —
// covering both the inline layout («Тел.: +7 …» in one block) and the
// label→value sibling-block layout (a «Телефоны» heading whose numbers live in
// the adjacent value block, the bankrot-v-spb.ru header shape). The first scope
// whose noise-stripped text holds a phone match wins; a distant ancestor is
// never reached (bounded walk). Noise is stripped before every match so a
// buried script/svg/template blob never resolves a scope.
func phoneValueScope(label *goquery.Selection) *goquery.Selection {
	node := label
	for depth := 0; depth <= maxPhoneLabelAncestor && node.Length() > 0; depth++ {
		if rePhone.MatchString(stripNoiseClone(node).Text()) {
			return node
		}
		if sib := node.Next(); sib.Length() > 0 && rePhone.MatchString(stripNoiseClone(sib).Text()) {
			return sib
		}
		node = node.Parent()
	}
	return nil
}

// narrowestPhoneNode descends from a (noise-stripped, detached) scope into the
// smallest element that still holds a phone in its text: at each level it steps
// into the first child whose text carries a phone, stopping when no child does
// (the phone sits in this node's own text, or is split across its children —
// e.g. a «+7 (812) …<br/>+7 (967) …» value block, where BOTH co-located numbers
// are then harvested together). Bounding the harvest this way keeps a broad
// depth-3/4 ancestor scope from pulling an unrelated partner/aggregator/fax line
// into the trust set (only the label's own phone block is read). Operates on the
// already-stripped clone, so the live document is never touched.
func narrowestPhoneNode(node *goquery.Selection) *goquery.Selection {
	for {
		var next *goquery.Selection
		node.Children().EachWithBreak(func(_ int, ch *goquery.Selection) bool {
			if rePhone.MatchString(ch.Text()) {
				next = ch
				return false
			}
			return true
		})
		if next == nil {
			return node
		}
		node = next
	}
}

// canonicalPhoneKey normalizes a phone string to a dedup key: digits only, with
// a leading RU trunk «8» rewritten to the «7» country code so the same number
// written "8 (812) …" and "+7 (812) …" collapses to one key. Used to skip a
// plain-text number a structured finder already produced (which may carry the
// other prefix form) and to dedup within the finder.
func canonicalPhoneKey(value string) string {
	d := DigitsOnly(value)
	if len(d) == ruPhoneDigits && d[0] == '8' {
		return "7" + d[1:]
	}
	return d
}

// ruPhoneDigits is the digit count of a full RU phone (country/trunk prefix +
// 10 national digits) — the same 11 ValidatePhone/phoneAreaCode gate on.
const ruPhoneDigits = 11

// containsIDLabel reports whether text carries any identifier/registry label
// (idLabelTokens). Matched CASE-SENSITIVELY: the uppercase registry
// abbreviations anchor without a lowercase substring wrongly firing inside a
// normal word (see idLabelTokens). The fail-closed direction (skip a block that
// might be requisites) is the safe one for anti-fabrication.
func containsIDLabel(text string) bool {
	for _, tok := range idLabelTokens {
		if strings.Contains(text, tok) {
			return true
		}
	}
	return false
}

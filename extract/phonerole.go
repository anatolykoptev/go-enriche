package extract

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// PhoneRole classifies a PhoneNumberFact's nearest human role/label text
// (PhoneNumberFact.RoleLabelRaw, sitenumbers.go) into a BINARY public/
// non-public verdict — see classifyPhoneRole. The zero value is roleGeneral,
// so an unlabeled phone (RoleLabelRaw == "") is ALWAYS general.
type PhoneRole string

// The two PhoneRole values. roleGeneral MUST stay the zero value (""): a
// phoneCandidate a future finder forgets to set roleLabel on (contacts.go/
// contactstext.go) leaves RoleLabelRaw=="" and must fail OPEN to general,
// never to a silently-invented departmental default.
const (
	roleGeneral      PhoneRole = ""
	roleDepartmental PhoneRole = "departmental"
)

// maxRoleLabelRunes caps PhoneNumberFact.RoleLabelRaw (see its doc comment,
// sitenumbers.go). Bounds a pathological long preceding-sibling block (e.g.
// an entire unrelated paragraph picked up by phoneRoleLabelText) from
// ballooning the fact; the value is kept close to verbatim for a downstream
// LLM consumer, just length-bounded.
const maxRoleLabelRunes = 120

// normalizeSpaces collapses internal whitespace runs and trims s. Local
// helper shared by cleanRoleLabelText and classifyPhoneRole below, reusing
// the package's existing reWhitespace regex (goquery.go) rather than a
// second whitespace-collapse implementation.
func normalizeSpaces(s string) string {
	return reWhitespace.ReplaceAllString(strings.TrimSpace(s), " ")
}

// cleanRoleLabelText normalizes whitespace and length-caps a role-label
// candidate string to maxRoleLabelRunes. Applied once by phoneRoleLabelText
// at the point a role text is found.
func cleanRoleLabelText(s string) string {
	s = normalizeSpaces(s)
	r := []rune(s)
	if len(r) <= maxRoleLabelRunes {
		return s
	}
	return string(r[:maxRoleLabelRunes])
}

// phoneRoleLabelText returns the nearest human role/label text found by
// scanning OUTWARD and BACKWARD from a phone-bearing DOM node — the
// department/desk context a phone sits under (e.g. a leasing-desk heading),
// which classifyPhoneRole below classifies into PhoneNumberFact.Role.
//
// Direction is the load-bearing property here: this is a PRECEDING-context
// scanner, NOT a directional mirror of phoneValueScope (contactstext.go),
// which walks a LABEL node's own subtree + its FOLLOWING sibling to find a
// VALUE for a label already known to carry a phone token (rePhoneLabel). A
// role heading instead PRECEDES the phone it introduces and carries no
// phone token of its own — the p45.su leasing-desk shape is a «…по вопросам
// аренды торговых мест» heading sitting BEFORE its tel:/itemprop anchor, so
// contactsTextCandidates' phone-token label gate (rePhoneLabel matching a
// node's own text) never even emits a scope for it. phoneRoleLabelText
// exists precisely to read that preceding, phone-token-free context, which
// phoneValueScope's forward-looking walk structurally cannot reach.
//
// At each of up to maxPhoneLabelAncestor levels (the same bound
// contactstext.go's phone-VALUE scan uses — same locality assumption: a
// phone's context, role or value, sits within a few DOM levels, never
// many) it first checks the current node's PRECEDING siblings (closest
// first, via PrevAll) — the label-heading-before-value-block layout — then,
// from the FIRST ancestor level onward, that ancestor's own DIRECT text
// (ownTextOf) — the inline "Отдел аренды. Телефон: <a>…</a>" layout, where a
// role prefix is a text-node sibling of the phone anchor within one
// wrapping element.
//
// node's OWN text is deliberately never read at depth 0: for an anchor
// (tel:/itemprop=telephone) that own text IS the phone's own display
// digits, not a role label — reading it would leak the phone number back
// into RoleLabelRaw. Climbing to the parent before reading own-text is what
// keeps the phone's digits out of the result (ownTextOf only reads DIRECT
// text-node children, so a parent's own text always excludes a nested
// phone anchor's text).
//
// Returns "" (⇒ roleGeneral, see classifyPhoneRole) when nothing is found
// within the bound — prefer a false negative (a genuinely departmental
// number with unusual markup stays general) over inventing a role from an
// unrelated distant ancestor.
func phoneRoleLabelText(node *goquery.Selection) string {
	n := node
	for depth := 0; depth <= maxPhoneLabelAncestor && n.Length() > 0; depth++ {
		if t := precedingSiblingRoleText(n); t != "" {
			return cleanRoleLabelText(t)
		}
		if depth > 0 {
			if t := ownTextOf(n); t != "" {
				return cleanRoleLabelText(t)
			}
		}
		n = n.Parent()
	}
	return ""
}

// precedingSiblingRoleText returns the noise-stripped, trimmed text of the
// closest non-empty PRECEDING sibling of n (via PrevAll, closest-first), or
// "" when n has no preceding sibling carrying any text (e.g. a leaf <svg>
// icon, which stripNoiseClone removes wholesale).
func precedingSiblingRoleText(n *goquery.Selection) string {
	var found string
	n.PrevAll().EachWithBreak(func(_ int, sib *goquery.Selection) bool {
		t := strings.TrimSpace(stripNoiseClone(sib).Text())
		if t != "" {
			found = t
			return false
		}
		return true
	})
	return found
}

// departmentalLabelTokens are high-precision, case-insensitive substrings
// (matched on the LOWERCASED label — see classifyPhoneRole) whose presence
// in a phone's nearest role-label text marks it departmental/non-public: a
// downstream card MUST NOT auto-apply this number as the venue's general
// public line (аренда/факс/бухгалтерия/… — the motivating p45.su leasing-
// desk case: a general line listed first, plus a mobile tel: under a
// PRECEDING «…по вопросам аренды торговых мест» heading).
//
// Tier A: unambiguous non-public single/compound words — a bare mention
// virtually never appears near a venue's OWN general line.
//
// Tier B: purpose/role FRAMES, deliberately MULTIWORD. A bare stem like
// "аренда" is EXCLUDED (see below) because a rental BUSINESS's own general
// line legitimately carries that stem (e.g. an equipment-rental company's
// main phone would false-demote on a bare-stem match). Only the "по
// вопросам аренды …" / "аренда(ы) мест/помещен…/площад…/торгов…" FRAME — a
// desk/topic label, not the business's own trade — is high-precision
// enough to demote.
//
// Deliberately EXCLUDED (do NOT add — false-demote risk, kept general):
// "отдел продаж" / "продажи" (a sales line is still a legitimate public
// contact), bare "приёмная" (a reception desk routes callers TO the general
// line, not away from it), "диспетчер", "администратор", "ресепшн",
// "справочная", "горячая линия", "единый номер", "call-центр", "запись",
// "бронирование" (all commonly ARE the venue's own general/booking line).
var departmentalLabelTokens = []string{
	// Tier A.
	"факс", "fax", "бухгалтер", "отдел кадров", "кадровая служба",
	"отдел персонала", "юридический отдел", "юротдел", "юрслужба",
	"пресс-служба", "пресс-центр", "для сми", "для прессы",
	"отдел закупок", "закупки", "снабжение", "претензи",
	"оптовый отдел", "оптом", "приёмная директора", "приемная директора",
	// Tier B (multiword frames only — see doc comment above).
	"по вопросам аренды", "вопросам аренды", "аренда мест", "аренды мест",
	"аренда помещен", "аренды помещен", "аренда площад", "аренды площад",
	"аренда торгов", "аренды торгов", "по вопросам сотрудничества",
	"по вопросам поставок", "для поставщиков", "для партнёров",
	"для партнеров", "оптовый прайс",
}

// classifyPhoneRole classifies a phone's nearest role-label text (raw — e.g.
// phoneCandidate.roleLabel / PhoneNumberFact.RoleLabelRaw) into general or
// departmental.
//
// High-precision denylist, default-general: an unlabeled phone (raw=="") is
// ALWAYS general — never demoted merely for lacking context (HARD
// invariant) — and a label with no departmental token match is ALSO
// general. There is NO positive-general override: a departmental match is
// never rescued by a co-occurring general word in the same label — this is
// exactly the «Отдел аренды. Телефон:…» auto-apply hole this function
// exists to close. If "телефон" were treated as a general signal able to
// override an "аренда" hit in the same string, the departmental leasing-
// desk number would still auto-apply as the public line, defeating the
// whole point of this classifier.
//
// Matching is CASE-INSENSITIVE — raw is lowercased before the token scan.
// This INVERTS containsIDLabel/idLabelTokens (contactstext.go), which is
// matched CASE-SENSITIVELY on purpose (the uppercase ИНН/ОГРН/etc. registry
// abbreviations anchor without a lowercase substring firing inside a normal
// word, e.g. "инновация"). A role-label phrase like «Аренда мест» has no
// such uppercase-abbreviation convention to lean on — it is ordinary prose
// that can be capitalized any which way (sentence-initial, a styled
// heading, all-caps CSS, …) — so this function normalizes case instead of
// relying on exact-case anchoring.
func classifyPhoneRole(raw string) PhoneRole {
	if raw == "" {
		return roleGeneral
	}
	low := strings.ToLower(normalizeSpaces(raw))
	for _, tok := range departmentalLabelTokens {
		if strings.Contains(low, tok) {
			return roleDepartmental
		}
	}
	return roleGeneral
}

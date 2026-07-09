package extract

import (
	"strings"
	"unicode"
	"unicode/utf8"

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

// IsDepartmental reports whether r is the non-public departmental role — the
// EXPORTED predicate a downstream consumer (go-wp's card-phone picker, task
// A2) should gate on, instead of comparing against an unexported PhoneRole
// constant or a magic "departmental" string. roleGeneral/roleDepartmental
// stay unexported on purpose: PhoneNumberFact.RoleLabelRaw is the durable
// raw signal a caller may want to inspect verbatim, while the
// CLASSIFICATION itself is meant to be consumed only through this
// compile-checked method, not by matching a literal.
func (r PhoneRole) IsDepartmental() bool {
	return r == roleDepartmental
}

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
// closest QUALIFYING PRECEDING sibling of n (via PrevAll, closest-first), or
// "" when none qualifies within n's sibling list.
//
// A sibling qualifies only when it carries NO phone of its own (rePhone).
// This is the review-round-1 BLOCKER fix (cross-block contamination): the
// common "departments as sibling blocks" RU venue layout —
//
//	<div>Бухгалтерия: <a href="tel:+7...">…</a></div>
//	<div>Телефон: <a href="tel:+7...">…</a></div>
//
// — has the SECOND div's phone climb to its own <div>, find the FIRST div
// as its closest preceding sibling, and (before this fix) read its whole
// text "Бухгалтерия: +7 …" as a role label — wrongly demoting the second,
// unrelated general line. A sibling that itself contains a phone is a
// DIFFERENT number's label+value block, not a role-only heading for n — a
// genuine role heading carries no phone token of its own (see
// phoneRoleLabelText's own doc comment on this exact point). Such a sibling
// is skipped and the scan keeps looking further back for one that
// qualifies, rather than treating the whole ancestor level as unusable —
// an inline same-element ownText prefix (checked separately by
// phoneRoleLabelText) is unaffected by this guard.
func precedingSiblingRoleText(n *goquery.Selection) string {
	var found string
	n.PrevAll().EachWithBreak(func(_ int, sib *goquery.Selection) bool {
		t := strings.TrimSpace(stripNoiseClone(sib).Text())
		if t == "" {
			return true // no text here — keep looking further back
		}
		if rePhone.MatchString(t) {
			return true // a DIFFERENT phone's own block — not a role label for n
		}
		found = t
		return false
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
//
// ALSO deliberately EXCLUDED as of review round 1 (MAJOR-2, real substring
// collisions with common venue types/business words a city guide indexes):
// bare "оптом" (⊂ «оптометрия»/«оптометрист» — optometry clinics are a
// common venue type), bare "снабжение" (⊂ «водоснабжение»/«теплоснабжение»/
// «электроснабжение» — utility-company names), bare "закупки" (⊂
// «госзакупки»). The FRAMED forms that already cover the real departmental
// cases stay: "отдел закупок", "оптовый отдел", "оптовый прайс" — long
// enough that they do not collide with any of the above.
var departmentalLabelTokens = []string{
	// Tier A.
	"бухгалтер", "отдел кадров", "кадровая служба",
	"отдел персонала", "юридический отдел", "юротдел", "юрслужба",
	"пресс-служба", "пресс-центр", "для сми", "для прессы",
	"отдел закупок", "претензи",
	"оптовый отдел", "приёмная директора", "приемная директора",
	// Tier B (multiword frames only — see doc comment above).
	"по вопросам аренды", "вопросам аренды", "аренда мест", "аренды мест",
	"аренда помещен", "аренды помещен", "аренда площад", "аренды площад",
	"аренда торгов", "аренды торгов", "по вопросам сотрудничества",
	"по вопросам поставок", "для поставщиков", "для партнёров",
	"для партнеров", "оптовый прайс",
}

// departmentalBoundaryTokens are short Tier-A tokens that are real
// substrings of an unrelated common word — «факс» ⊂ «факсимиле», "fax" ⊂
// "telefax"/"faxes" — so plain strings.Contains would false-demote a
// facsimile-machine/service mention. Matched via containsWordToken
// (letter-bounded on both sides) instead of departmentalLabelTokens' plain
// Contains.
//
// NOT matched via a regexp `\b` word-boundary anchor: Go's regexp package
// (RE2) defines `\b` as an ASCII word boundary only (`\w` = [0-9A-Za-z_]) —
// it does NOT recognize a Cyrillic letter as a "word" character, so
// `\bфакс\b` never reliably anchors on Cyrillic text (this is why
// rePhoneLabel, contactstext.go, anchors its Cyrillic arms via trailing
// punctuation instead of \b, and reserves \b for its Latin arms only).
// containsWordToken implements the boundary check manually via
// unicode.IsLetter on the runes immediately surrounding each match, which
// works uniformly for both scripts.
var departmentalBoundaryTokens = []string{"факс", "fax"}

// containsWordToken reports whether haystack contains tok as a
// LETTER-BOUNDED occurrence: the rune immediately before and after the
// match (if any) must NOT be a Unicode letter. See departmentalBoundaryTokens'
// doc comment for why this is hand-rolled instead of a regexp \b anchor.
func containsWordToken(haystack, tok string) bool {
	if tok == "" {
		return false
	}
	for {
		i := strings.Index(haystack, tok)
		if i < 0 {
			return false
		}
		beforeOK := i == 0
		if !beforeOK {
			r, _ := utf8.DecodeLastRuneInString(haystack[:i])
			beforeOK = !unicode.IsLetter(r)
		}
		afterOK := i+len(tok) >= len(haystack)
		if !afterOK {
			r, _ := utf8.DecodeRuneInString(haystack[i+len(tok):])
			afterOK = !unicode.IsLetter(r)
		}
		if beforeOK && afterOK {
			return true
		}
		haystack = haystack[i+len(tok):] // keep scanning past this occurrence
	}
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
	for _, tok := range departmentalBoundaryTokens {
		if containsWordToken(low, tok) {
			return roleDepartmental
		}
	}
	return roleGeneral
}

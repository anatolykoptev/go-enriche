package extract

import (
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// maxBranchScriptBytes bounds a single inline <script> element's text before
// any literal-isolation/parse is attempted — mirrors the root package's
// WithMaxContentLen size-bound convention (options.go), scaled for a raw
// script payload rather than extracted prose. A script whose text exceeds
// this is SKIPPED entirely, not truncated: truncating mid-JSON would only
// guarantee a parse failure, so skipping the whole script is the honest
// fail-closed move against an oversized/adversarial payload on the shared
// 4-core host-a box.
const maxBranchScriptBytes = 256 * 1024

// maxBranchCandidates bounds how many phone candidates branchJSONCandidates
// emits for a single page, across every <script> it scans — a crafted page
// with thousands of phone-keyed objects must not balloon
// CollectSiteNumbers' downstream dedupe/sort cost. Logged once when tripped
// so an operator can see a page hit the ceiling.
const maxBranchCandidates = 200

// branchPhoneKeys are the phone-semantic JSON keys branchJSONCandidates
// reads, matched case-insensitively, in this FIXED priority order so
// candidate emission order — and therefore which reading DedupeKeepStronger
// keeps as "first-seen" on a same-tier collision — is deterministic
// regardless of Go's randomized map-iteration order. A national-chain
// branch object routinely carries BOTH "phone" (human-formatted) and
// "phoneLink" (E.164-ish) for the SAME number; CollectSiteNumbers' own
// digit-keyed dedupe collapses the two, so this finder does not need to
// pick a single "best" field itself.
var branchPhoneKeys = []string{"phone", "phoneLink", "telephone", "tel"}

// branchJSONCandidates finds phone numbers embedded in a national-chain
// site's bespoke inline-script branch-locator JSON — e.g. a Bitrix
// `var marker_data = '[{"phone":"+7 (812) 767-36-61", ...}, ...25 branches]';`
// assignment, where the JSON array is serialized as a JS single-quoted
// STRING LITERAL (\u-escaped Cyrillic, \/-escaped slashes). Every other
// finder in this package (tel:/microdata/social-link/og:) reads only the
// visible DOM, so a chain that prints just its Moscow HQ number in markup
// while burying every real branch — including the target city's own —
// inside this kind of script is invisible upstream of here. This is the
// class of bug wp_verify's false "wrong" on mcmedok.ru-style cards traces to
// (see testdata/golden/mcmedok.html).
//
// Anti-fabrication is a HARD three-gate boundary, in order:
//  1. a value must sit under an explicit phone-semantic key (branchPhoneKeys,
//     case-insensitive) — a bare digit-run under "id"/"lat"/"lng"/"timestamp"
//     is never even inspected, regardless of whether it happens to look like
//     a plausible phone number;
//  2. the value is reached via a TYPED json.Unmarshal at ONE flat object
//     level (a top-level array of objects, or a single top-level object) —
//     deliberately NO recursion, so a nested object's numbers are
//     structurally unreachable rather than merely policy-excluded
//     (eliminates the CWE-674 unbounded-recursion class rather than just
//     bounding it);
//  3. every surviving value is gated through the SAME ValidatePhone/
//     makeCandidate Rossvyaz-numbering-plan ceiling every other finder in
//     this package obeys — an invalid-looking "phone" field is dropped, not
//     coerced.
//
// JSON isolation is a small LOCAL string-literal unwrap
// (jsStringLiteralAssignments below) — NOT a bracket/brace scanner: it
// locates a `var X = '...'` / `= "..."` JS string-literal ASSIGNMENT by
// quote-matching (backslash-escape-aware, to find the correct closing
// quote), unescapes only the wrapping quote's own escape (\' inside a
// '...'-wrapped literal, or \" inside a "..."-wrapped one — every other
// backslash sequence, \uXXXX / \/ / \\ / \n / \t / \r, is ALREADY valid JSON
// string-escape syntax and is left for json.Unmarshal to decode natively),
// then json.Unmarshal's the isolated content. This naturally handles a
// <script> that bundles MULTIPLE JS statements in one tag (e.g.
// mcmedok.ru's script carries both `marker_data = '[...]'` AND a trailing
// `var region = {...}` — a BARE object literal, not string-wrapped, so it
// is never even considered): each `= '...'`-wrapped literal is isolated
// independently by its own matching quotes, so one statement's content can
// never bleed into another's the way a whole-script bracket-matching
// heuristic would. This is a deliberately NARROWER target than "any
// bracketed JSON on the page" — it only reads JSON that was serialized
// (server-side, typically json_encode()) INTO a JS string literal, which is
// exactly the real-world branch-locator shape; a bare, hand-written JS
// object/array literal is out of scope (YAGNI absent a real fixture of that
// shape).
//
// Only scripts with no `type` attribute (the HTML default) or an explicit
// text/javascript / application/javascript type are scanned — a
// schema.org <script type="application/ld+json"> or a raw
// application/json data island is Phase-2's domain (structured.Places()),
// never branchJSON's, even on the rare occasion its content would otherwise
// pass every anti-fab gate (see isJSExecutableScript).
//
// branch_json is a non-visible-DOM source class: a wrong number in a hidden
// script blob has no visible-page tell the way a wrong tel: link would.
// Accepted as the same risk class the DOM tel: source already carries; the
// caller's city classification (extract.ClassifyCityMembership) and the
// page-level DNI gate (dniTrustworthy, this file's tier sits below the
// DNI-immune tierSocialLink) still keep a wrong-city or rotating-proxy
// reading from ever becoming authoritative.
func branchJSONCandidates(doc *goquery.Document) []phoneCandidate {
	if doc == nil {
		return nil
	}
	var out []phoneCandidate
	capped := false
	doc.Find("script").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		if !isCandidateScript(s) {
			return true
		}
		out, capped = appendScriptCandidates(out, s.Text())
		return !capped
	})
	if capped {
		slog.Warn("extract: branchJSONCandidates candidate cap tripped", "cap", maxBranchCandidates)
	}
	return out
}

// isCandidateScript reports whether a <script> element is even worth
// scanning: no `src` attribute (an external script has no inline JSON to
// read) and JS-executable per isJSExecutableScript (a schema.org/data-
// island script is Phase-2's domain, not ours).
func isCandidateScript(s *goquery.Selection) bool {
	if _, hasSrc := s.Attr("src"); hasSrc {
		return false
	}
	return isJSExecutableScript(s)
}

// appendScriptCandidates scans one script's text for JS string-literal
// assignments (jsStringLiteralAssignments) and appends every harvested
// candidate to out, stopping as soon as maxBranchCandidates is reached.
// Returns the updated slice and whether the cap was tripped on this call —
// the caller uses that to stop scanning further scripts.
func appendScriptCandidates(out []phoneCandidate, text string) ([]phoneCandidate, bool) {
	if len(text) == 0 || len(text) > maxBranchScriptBytes {
		return out, false
	}
	for _, literal := range jsStringLiteralAssignments(text) {
		if !looksJSONish(literal) {
			continue
		}
		for _, c := range branchLiteralCandidates(literal) {
			out = append(out, c)
			if len(out) >= maxBranchCandidates {
				return out, true
			}
		}
	}
	return out, false
}

// isJSExecutableScript reports whether s is a JS-execution <script> —
// either no type attribute (the HTML default) or an explicit
// text/javascript / application/javascript type — as opposed to a
// schema.org JSON-LD block (application/ld+json), a raw JSON data island
// (application/json), an import map, or any other non-executable script
// payload. Only a JS-execution script can plausibly carry a `var X = '...'`
// string-literal assignment; a JSON-typed script is Phase-2's domain
// (structured.Places()) regardless of whether its content happens to carry
// a clean "telephone" field that would otherwise pass every anti-fab gate.
func isJSExecutableScript(s *goquery.Selection) bool {
	t, ok := s.Attr("type")
	if !ok {
		return true
	}
	t = strings.ToLower(strings.TrimSpace(t))
	return t == "" || t == "text/javascript" || t == "application/javascript"
}

// looksJSONish is a cheap pre-check so branchLiteralCandidates only attempts
// json.Unmarshal on a literal that could plausibly be JSON — the first
// non-space byte is '{' or '['. Not a validity check: a literal that passes
// this can still fail json.Unmarshal (fail-closed miss), and one that fails
// it is skipped without ever attempting a parse.
func looksJSONish(s string) bool {
	s = strings.TrimSpace(s)
	return len(s) > 0 && (s[0] == '{' || s[0] == '[')
}

// jsStringLiteralAssignments scans script text for `= '...'` / `= "..."`
// JS string-literal assignments (an `=` followed by optional whitespace and
// an opening quote) and returns each literal's content, delimiter-
// unescaped only (see unescapeJSStringDelimiter). This is a plain JS
// STRING-LITERAL scan — quote-matching with backslash-escape awareness to
// find the correct closing quote — NOT a JSON/bracket scanner: it has zero
// knowledge of {}/[] structure and never balances braces.
func jsStringLiteralAssignments(text string) []string {
	var out []string
	i := 0
	for i < len(text) {
		eq := strings.IndexByte(text[i:], '=')
		if eq < 0 {
			break
		}
		eq += i
		j := eq + 1
		for j < len(text) && isJSSpace(text[j]) {
			j++
		}
		if j >= len(text) || (text[j] != '\'' && text[j] != '"') {
			i = eq + 1
			continue
		}
		quote := text[j]
		k := j + 1
		for k < len(text) {
			if text[k] == '\\' {
				k += 2
				continue
			}
			if text[k] == quote {
				break
			}
			k++
		}
		if k >= len(text) {
			// Unterminated literal. This is reachable ONLY when the inner loop
			// above scanned to EOF without finding the closing quote — i.e. the
			// entire span from the opening quote to end-of-text is inside this
			// (unterminated) literal. No terminated assignment can therefore exist
			// after it, so this break is equivalent to `return out`: it is NOT a
			// fail-closed miss of a following valid literal (there is none). A
			// well-formed literal always exits the inner loop via `text[k] == quote`
			// with k < len, never here.
			break
		}
		out = append(out, unescapeJSStringDelimiter(text[j+1:k], quote))
		i = k + 1
	}
	return out
}

// isJSSpace reports whether b is JS whitespace between `=` and the string
// literal that follows it.
func isJSSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// unescapeJSStringDelimiter undoes ONLY the escaping a template engine adds
// to protect the wrapping quote character (e.g. \' inside a '...'-wrapped
// literal, so an apostrophe in a real address like "O'Brien" doesn't
// terminate the JS string early). Every other backslash escape (\uXXXX,
// \/, \\, \n, \t, \r, the OTHER quote character, …) is left untouched
// because it is ALSO valid JSON string-escape syntax, which json.Unmarshal
// decodes natively — touching it here would risk double-unescaping.
// Content that turns out not to be JSON at all simply fails json.Unmarshal
// downstream — fail-closed, never fabricated.
func unescapeJSStringDelimiter(s string, quote byte) string {
	esc := "\\" + string(quote)
	if !strings.Contains(s, esc) {
		return s
	}
	return strings.ReplaceAll(s, esc, string(quote))
}

// branchLiteralCandidates parses ONE already-isolated, delimiter-unescaped
// JSON literal and reads phone-keyed fields at a single flat level.
// Returns nil on any parse/shape failure — fail-closed, never a
// fabrication.
func branchLiteralCandidates(literal string) []phoneCandidate {
	var raw any
	if err := json.Unmarshal([]byte(literal), &raw); err != nil {
		return nil
	}
	switch v := raw.(type) {
	case []any:
		var out []phoneCandidate
		for _, el := range v {
			if obj, ok := el.(map[string]any); ok {
				out = append(out, branchObjectCandidates(obj)...)
			}
		}
		return out
	case map[string]any:
		return branchObjectCandidates(v)
	default:
		return nil // a bare string/number/bool literal carries no phone field
	}
}

// branchObjectCandidates reads branchPhoneKeys off ONE flat JSON object
// (case-insensitive key match, string values only — gate #1) and gates each
// surviving value through makeCandidate (ValidatePhone + 8-800 demotion —
// gate #3) at tierBranchJSON. NO recursion into nested objects/arrays —
// gate #2, enforced structurally: branchObjectCandidates never looks at a
// value it did not receive as a direct map[string]any entry.
func branchObjectCandidates(obj map[string]any) []phoneCandidate {
	var out []phoneCandidate
	for _, wantKey := range branchPhoneKeys {
		v, ok := lookupFold(obj, wantKey)
		if !ok {
			continue
		}
		s, ok := v.(string)
		if !ok {
			continue
		}
		if c, ok := makeCandidate(s, tierBranchJSON); ok {
			out = append(out, c)
		}
	}
	return out
}

// lookupFold returns obj's value for the first key matching want
// case-insensitively. Deterministic in practice: a well-formed object has
// at most one key spelling per phone field; a pathological object with two
// differently-cased spellings of the same field is an accepted, harmless
// edge case, since both readings would pass through the identical
// ValidatePhone gate downstream regardless of which one map iteration
// happens to surface first.
func lookupFold(obj map[string]any, want string) (any, bool) {
	for k, v := range obj {
		if strings.EqualFold(k, want) {
			return v, true
		}
	}
	return nil, false
}

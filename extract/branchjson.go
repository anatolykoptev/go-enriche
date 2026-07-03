package extract

import (
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/PuerkitoBio/goquery"
	kitllm "github.com/anatolykoptev/go-kit/llm"
)

// maxBranchScriptBytes bounds a single inline <script> element's text before
// any JSON isolation/parse is attempted — mirrors the root package's
// WithMaxContentLen size-bound convention (options.go), scaled for a raw
// script payload rather than extracted prose. A script whose text exceeds
// this is SKIPPED entirely, not truncated: truncating mid-JSON would only
// guarantee a parse failure, so skipping the whole script is the honest
// fail-closed move against an oversized/adversarial payload on the shared
// 4-core krolik box.
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
// JSON isolation REUSES kitllm.ExtractJSON (go-kit/llm, already a
// go-enriche dependency) — no bespoke bracket scanner. A single inline
// <script> on a real page routinely bundles MULTIPLE JS statements (e.g.
// mcmedok.ru's script carries both the `marker_data = '[...]'` assignment
// AND a trailing `var region = {...}` object in the SAME <script> tag);
// handing ExtractJSON the WHOLE script text lets its earliest-opener/
// latest-closer heuristic over-capture from the first statement's bracket
// all the way through the LAST one, yielding invalid JSON that fails to
// parse. So the script text is first split on ';' — the plain JS statement
// separator, NOT a JSON-aware split — and ExtractJSON runs once per
// statement-shaped segment instead of once per whole script. This adds zero
// bracket-counting/JSON-structure logic of our own: ExtractJSON remains the
// sole JSON-isolation primitive, merely fed a narrower input. On a segment
// ExtractJSON can't cleanly isolate (an exotic shape with a literal ';'
// inside a string value, e.g.), json.Unmarshal fails and that segment
// yields zero candidates — a fail-closed MISS, never a fabrication.
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
		if _, hasSrc := s.Attr("src"); hasSrc {
			return true // an external script has no inline JSON to read
		}
		text := s.Text()
		if len(text) == 0 || len(text) > maxBranchScriptBytes {
			return true
		}
		for _, stmt := range strings.Split(text, ";") {
			if !strings.ContainsAny(stmt, "{[") {
				continue // no bracket at all — not worth an ExtractJSON call
			}
			for _, c := range branchStatementCandidates(stmt) {
				out = append(out, c)
				if len(out) >= maxBranchCandidates {
					capped = true
					return false
				}
			}
		}
		return true
	})
	if capped {
		slog.Warn("extract: branchJSONCandidates candidate cap tripped", "cap", maxBranchCandidates)
	}
	return out
}

// branchStatementCandidates isolates and parses ONE ';'-delimited JS
// statement's JSON literal (via kitllm.ExtractJSON) and reads phone-keyed
// fields at a single flat level. Returns nil on any isolation/parse/shape
// failure — fail-closed, never a fabrication.
func branchStatementCandidates(stmt string) []phoneCandidate {
	extracted := kitllm.ExtractJSON(stmt)
	if extracted == "" {
		return nil
	}
	var raw any
	if err := json.Unmarshal([]byte(extracted), &raw); err != nil {
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

package extract

import (
	"sort"

	"github.com/PuerkitoBio/goquery"
)

// PhoneNumberFact is one candidate phone number found on an official-site
// page — a member of the SET collectPhoneCandidates already builds before
// pickPhoneCandidate collapses it to the single winner Facts.Phone commits to
// (see collectPhoneCandidates / pickPhoneCandidate in contacts.go). A
// valid-but-different site number (e.g. a stable owned tel: that simply isn't
// the top-tier social-link pick — the Lazermed calibration case) is otherwise
// invisible past the resolver and reads as WRONG downstream. Exposing the
// full set closes that gap. pickPhoneCandidate and Facts.Phone are UNCHANGED
// by this file: PhoneNumberFact is a read-only, additive sidecar.
type PhoneNumberFact struct {
	// Value is the candidate's human-facing display value, exactly as the
	// underlying finder produced it (tel: link text, itemprop content, og:
	// meta content, or the normalized wa.me/api.whatsapp.com digits).
	Value string
	// Source names where the candidate was found: "social_link" (wa.me /
	// api.whatsapp.com — DNI-immune), "contacts" (a tel: in the header/
	// footer/address/contacts region), "microdata" ([itemprop=telephone] /
	// og:/business: meta), "body" (a tel: elsewhere on the page), or
	// "demoted" (a tel: nested inside a named call-tracking widget — a
	// dynamic tracking slot). A plain 8-800 toll-free number is NO LONGER
	// forced to "demoted" here: a static, region-anchored 8-800 is a real
	// published contact and keeps its DOM-region Source (e.g. "contacts") and
	// its Trustworthy verdict. The toll-free demotion applies only to the
	// city-guide DISPLAY pick (pickPhoneCandidate), not this trust sidecar.
	Source string
	// Anchored is true when the candidate's tier is contacts-region-or-above
	// (tierContacts / tierSocialLink / tierMicrodata) — i.e. NOT a bare-body
	// tel: and NOT call-tracking-demoted. A regex-fallback / prose-only phone
	// is never a member of this set at all: CollectSiteNumbers reads only the
	// DOM finders collectPhoneCandidates also reads, and never seeds the
	// Layer-1/2 `prior` strings collectPhoneCandidates optionally folds in —
	// so a prose-only candidate is excluded by construction, not by a
	// separate plausibility gate (YAGNI).
	Anchored bool
	// DNI is true when the PAGE (not just this candidate) is considered
	// DNI/call-tracking-active — the OR of doc's own detectDNIVendor()
	// reading AND the caller-supplied pagePoisoned flag (CollectSiteNumbers'
	// Poison-OR: see its doc comment). It is a page-level signal,
	// deliberately the SAME for every candidate a single CollectSiteNumbers
	// call returns — see Trustworthy for why a per-candidate DNI matcher
	// must not be introduced.
	DNI bool
	// Trustworthy is the COMMITTED verdict, computed via dniTrustworthy
	// (contacts.go) — the SAME shared predicate resolvePhoneForCityDNI's own
	// DNI branch calls: when DNI is true, ONLY a social-link candidate
	// (Source == "social_link") is Trustworthy; otherwise every Anchored
	// candidate is. Sharing one predicate means this can never diverge from
	// what resolvePhoneForCityDNI decides for the same page — a second,
	// independent per-candidate DNI heuristic here would risk an unlisted
	// vendor failing OPEN (a rotating proxy marked Trustworthy) where the
	// resolver fails closed.
	Trustworthy bool
	// CityMatch is true when this candidate's area code is local to the
	// project's configured city (Item.City, tagged via
	// ClassifyCityMembership in the enriche resolver's addSiteNumbers — see
	// resolve.go). Additive and NOT computed by CollectSiteNumbers/
	// CollectSiteNumbersHTML themselves (they have no city input); both this
	// and CityForeign default to false (page-city-neutral) for any caller
	// that builds a PhoneNumberFact directly or enriches with no known city.
	CityMatch bool
	// CityForeign is true when this candidate is a confirmed OTHER-city RU
	// geographic landline — i.e. NOT city-local, NOT a mobile number, NOT an
	// 8-800 toll-free number, and NOT unparseable/non-RU (see
	// isRUGeographicLandline in citycode.go). Seed-independent: a landline
	// for a city that was never added to cityAreaCodes still tags
	// CityForeign=true here, so a national-chain site's out-of-town branch
	// number is never mistaken for a neutral/safe candidate. CityMatch and
	// CityForeign are never both true.
	CityForeign bool
	// RoleLabelRaw is the nearest human role/label text found near this
	// candidate's DOM node (phoneRoleLabelText, phonerole.go) — e.g. a
	// leasing-desk heading («по вопросам аренды торговых мест») or an
	// inline department prefix («Отдел кадров. Телефон:»). Noise-stripped,
	// whitespace-normalized, and length-capped to ~120 chars, kept close to
	// verbatim (not further paraphrased) for a downstream LLM consumer. ""
	// when no role/label text was found nearby, when the finder has no DOM
	// node to scan at all (branchJSONCandidates/schemaPlaceCandidates/
	// ogPhoneCandidates read structured JSON/meta, not a labeled DOM
	// region), or when the finder deliberately does not scan one
	// (socialLinkCandidates — see phoneCandidate.roleLabel's doc comment,
	// contacts.go) — in every case Role is roleGeneral (see its doc
	// comment: unlabeled never demotes).
	RoleLabelRaw string
	// Role classifies RoleLabelRaw via classifyPhoneRole (phonerole.go) into
	// a binary general/departmental verdict, so a downstream consumer (e.g.
	// go-wp's card-phone picker, task A2) can skip a departmental number
	// (аренда/факс/бухгалтерия/…) rather than auto-applying it as a card's
	// public line — the p45.su motivating case: a general line listed first
	// plus a mobile tel: under a PRECEDING «…по вопросам аренды торговых
	// мест» heading. Zero value roleGeneral: an unlabeled candidate
	// (RoleLabelRaw=="") is ALWAYS general, never demoted for lacking
	// context.
	Role PhoneRole
}

// Site-number source labels (PhoneNumberFact.Source). The tierContacts label
// reuses the package's existing regionContacts constant (contacts.go) rather
// than a second "contacts" literal — golangci-lint's goconst flags a fresh
// constant that merely duplicates an existing one's value.
const (
	numSourceSocialLink  = "social_link"
	numSourceBranchJSON  = "branch_json"
	numSourceSchemaPlace = "schema_place"
	numSourceMicrodata   = "microdata"
	numSourceBody        = "body"
	numSourceDemoted     = "demoted"
)

// SiteNumberSources is every PhoneNumberFact.Source label this package can
// emit. Exported so a downstream consumer (go-wp's siteNumberSource/
// siteNumberPriority switches, internal/wptools/content/sitenumbers.go) can
// range over it in a fitness test that FAILS ITS BUILD when a label here has
// no matching case there — turning a silent cross-repo drift (an unhandled
// Source silently downgrading to numberSourceUnknown / priority 0 in
// wp_verify's public output) into a loud one instead.
var SiteNumberSources = []string{
	numSourceSocialLink,
	numSourceBranchJSON,
	numSourceSchemaPlace,
	regionContacts,
	numSourceMicrodata,
	numSourceBody,
	numSourceDemoted,
}

// numberSourceForTier maps a phoneCandidate tier to its PhoneNumberFact.Source
// label.
func numberSourceForTier(tier int) string {
	switch tier {
	case tierSocialLink:
		return numSourceSocialLink
	case tierBranchJSON:
		return numSourceBranchJSON
	case tierSchemaPlace:
		return numSourceSchemaPlace
	case tierContacts:
		return regionContacts
	case tierMicrodata:
		return numSourceMicrodata
	case tierBody:
		return numSourceBody
	default:
		return numSourceDemoted
	}
}

// numberIsAnchored reports whether a tier counts as "anchored" for
// PhoneNumberFact purposes — see PhoneNumberFact.Anchored's doc comment.
func numberIsAnchored(tier int) bool {
	switch tier {
	case tierContacts, tierSocialLink, tierBranchJSON, tierSchemaPlace, tierMicrodata:
		return true
	default:
		return false
	}
}

// CollectSiteNumbers returns every distinct, valid phone-number candidate
// found in doc — the UNION collectPhoneCandidates' DOM finders already
// produce (social-link, tel:, microdata, og:/business meta), normalized and
// deduped by digits, each tagged per PhoneNumberFact's doc comments. Unlike
// collectPhoneCandidates it takes NO `prior` strings (the Layer-1/2 JSON-LD/
// regex-fallback seed collectPhoneCandidates optionally folds in) — it is a
// pure DOM read, so a regex-fallback / prose-only phone is never a member of
// the returned set.
//
// pagePoisoned carries the RAW-fetch poison verdict forward across a render —
// the SAME Poison-OR invariant Facts.Phone honors (see enriche_fetch.go's
// "rawFacts.PhonePoisoned && !renderedFacts.PhonePoisoned" and
// enriche_contacts.go's rawPoisoned carry-forward). A DNI/call-tracking
// widget can rewrite or remove itself at runtime, so a clean-looking
// POST-RENDER doc says nothing about whether the RAW page was poisoned. When
// doc is the RENDERED dom and pagePoisoned is true, it is OR'd into doc's own
// detectDNIVendor() reading before dniTrustworthy is evaluated — so a
// raw-poisoned page can never launder a rotating-proxy tel: into
// Trustworthy=true (in the exported, cache-persisted Result.SiteNumbers)
// just because the widget hid itself before the render was captured. Pass
// false when there is no separate raw-fetch stage to carry forward (e.g. a
// standalone doc with no prior fetch).
//
// Ordering is deterministic: highest tier first, then ascending normalized
// digits, so a fixture yields byte-identical output across repeated runs
// regardless of DOM traversal order.
//
// pickPhoneCandidate (the single-winner resolver) and Facts.Phone are
// UNCHANGED by this function — it is an additive, read-only view over the
// same candidate set collectPhoneCandidates already builds.
func CollectSiteNumbers(doc *goquery.Document, pagePoisoned bool) []PhoneNumberFact {
	if doc == nil {
		return nil
	}
	// The SAME candidate union collectPhoneCandidates builds for the
	// single-winner resolver (contacts.go) — no `prior` seed, since a
	// regex-fallback / prose-only phone is never a member of this DOM-only
	// set (see the doc comment above).
	cands := collectPhoneCandidates(doc)
	// branchJSONCandidates and schemaPlaceCandidates are unioned into the
	// SiteNumbers SET ONLY — never into collectPhoneCandidates — so
	// Facts.Phone/pickPhoneCandidate (contacts.go) stay byte-unchanged (see
	// branchjson.go's and schemaplace.go's doc comments).
	cands = append(cands, branchJSONCandidates(doc)...)
	cands = append(cands, schemaPlaceCandidates(doc)...)
	// Plain-text phones printed inside a genuine phone-labeled contacts region
	// (a «Телефоны» block with no tel: href and no microdata) — SiteNumbers-set
	// only, never collectPhoneCandidates, so Facts.Phone stays byte-unchanged.
	cands = append(cands, contactsTextCandidates(doc, cands)...)
	if len(cands) == 0 {
		return nil
	}

	_, domDNI := detectDNIVendor(doc)
	dni := domDNI || pagePoisoned

	type keyed struct {
		fact PhoneNumberFact
		tier int
		key  string
	}
	tagged := make([]keyed, 0, len(cands))
	for _, c := range cands {
		key := DigitsOnly(c.value)
		if key == "" {
			continue
		}
		// The TRUST view (Source / Anchored / Trustworthy) keys off the
		// candidate STATIC DOM region (naturalTier), never its display-rank
		// tier. An 8-800 that makeCandidate demoted to tierDemoted purely on
		// its toll-free prefix is still a real, statically-published contact
		// when it sits in a legit region (naturalTier == tierContacts/
		// tierMicrodata/...), so it stays Anchored + Trustworthy here. A
		// genuinely dynamic call-tracking-widget-nested tel: already carries
		// naturalTier == tierDemoted (telTier isCallTrackingDemoted arm), so it
		// stays untrustworthy — the prefix is not dynamic-insertion evidence,
		// the DOM region is. naturalTier == tier for every non-toll-free
		// candidate, so this is byte-identical for every existing fixture; the
		// city-guide DISPLAY pick (pickPhoneCandidate/Facts.Phone) still reads
		// c.tier and is unchanged.
		anchored := numberIsAnchored(c.naturalTier)
		tagged = append(tagged, keyed{
			fact: PhoneNumberFact{
				Value:        c.value,
				Source:       numberSourceForTier(c.naturalTier),
				Anchored:     anchored,
				DNI:          dni,
				Trustworthy:  dniTrustworthy(c.naturalTier, dni),
				RoleLabelRaw: c.roleLabel,
				Role:         classifyPhoneRole(c.roleLabel),
			},
			tier: c.naturalTier,
			key:  key,
		})
	}

	// Same number seen at another tier (e.g. a header tel: plus repeated
	// itemprop=telephone duplicates for the same number): keep the
	// HIGHER-tier reading — the strongest evidence found for this number
	// wins its classification, mirroring pickPhoneCandidate's own
	// tier-wins tiebreak. DedupeKeepStronger is the shared keyed-dedupe
	// mechanism the enriche resolver's SiteNumbers accumulator (resolve.go)
	// also uses, so both no longer hand-roll the identical map+scan logic.
	out := DedupeKeepStronger(tagged,
		func(k keyed) string { return k.key },
		func(k keyed) int { return k.tier },
	)

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].tier != out[j].tier {
			return out[i].tier > out[j].tier
		}
		return out[i].key < out[j].key
	})

	facts := make([]PhoneNumberFact, len(out))
	for i, k := range out {
		facts[i] = k.fact
	}
	// Role must NOT ride the tier-trust merge blindly (review round 1,
	// MAJOR-3): a lower-tier reading (e.g. contactsText/tierBody) winning
	// the merge on Value/Source/Anchored/Trustworthy must not silently
	// carry a departmental misread over a co-existing higher-signal general
	// reading (e.g. a tierMicrodata itemprop=telephone) for the SAME
	// digits. `tagged` (pre-dedup, every candidate reading) is the OR-reduce
	// input; `facts` (post-dedup, one entry per digits key) is fixed up in
	// place.
	all := make([]PhoneNumberFact, len(tagged))
	for i, k := range tagged {
		all[i] = k.fact
	}
	ReduceRoleGeneralWins(facts, all)
	return facts
}

// ReduceRoleGeneralWins adjusts `out` (an already tier/trust-deduped
// []PhoneNumberFact, one entry per distinct DigitsOnly(Value) key) IN PLACE
// so Role/RoleLabelRaw are decoupled from the tier/trust merge
// DedupeKeepStronger performs on every OTHER field: for each entry, if ANY
// reading in `all` (the FULL pre-dedup set, before DedupeKeepStronger
// collapsed it) sharing its digits key was roleGeneral, the entry's
// Role/RoleLabelRaw are forced to that general reading — even when a
// DIFFERENT, lower-precision reading won the tier/trust merge with a
// departmental label. In other words: a number that reads GENERAL from ANY
// reading stays general; it classifies departmental only when EVERY
// reading for that number does.
//
// Why this must be separate from rankFn: DedupeKeepStronger's tier-wins
// rule is correct and desired for Value/Source/Anchored/Trustworthy — the
// STRONGEST evidence for the number's trust/region should win. But Role
// answers a DIFFERENT question ("is this number ever safe to treat as
// public"), and a strong "this IS the public line" signal (e.g. a clean
// itemprop=telephone reading) must never be silently overridden just
// because a separate, lower-tier reading of the SAME number happened to
// rank higher on the trust axis and carried a departmental-looking nearby
// label.
//
// Shared by CollectSiteNumbers (above) and the enriche resolver's
// addSiteNumbers (resolve.go) — both dedupe PhoneNumberFact-shaped
// candidates by digits and need the SAME OR-toward-general reduction across
// their respective pre-dedup sets, so neither hand-rolls an independent
// copy. First-general-found-in-`all`-order wins the RoleLabelRaw text when
// multiple general readings exist for the same key — deterministic given
// `all`'s own deterministic order (both call sites build it from a fixed
// DOM/accumulation order, never Go's randomized map iteration).
func ReduceRoleGeneralWins(out, all []PhoneNumberFact) {
	generalRaw := make(map[string]string, len(all))
	for _, f := range all {
		if f.Role != roleGeneral {
			continue
		}
		key := DigitsOnly(f.Value)
		if key == "" {
			continue
		}
		if _, ok := generalRaw[key]; !ok {
			generalRaw[key] = f.RoleLabelRaw
		}
	}
	for i := range out {
		key := DigitsOnly(out[i].Value)
		if raw, ok := generalRaw[key]; ok {
			out[i].Role = roleGeneral
			out[i].RoleLabelRaw = raw
		}
	}
}

// DedupeKeepStronger merges items into a deduped slice keyed by keyFn: on a
// duplicate key, the item with the HIGHER rankFn wins; first-seen order is
// preserved (callers apply their own final sort on top). An empty key is
// skipped — it means "could not classify", never a legitimate dedup target.
//
// Shared by CollectSiteNumbers (above) and the enriche resolver's
// addSiteNumbers/siteNumbersSnapshot (resolve.go) — both need "same key,
// keep the strongest reading" and previously hand-rolled the identical
// map+linear-scan logic independently. Exported so the enriche package
// (which already imports this one for PhoneNumberFact) can reuse it instead
// of a second, drift-prone copy.
func DedupeKeepStronger[T any](items []T, keyFn func(T) string, rankFn func(T) int) []T {
	byKey := make(map[string]int, len(items))
	out := make([]T, 0, len(items))
	for _, it := range items {
		key := keyFn(it)
		if key == "" {
			continue
		}
		if i, ok := byKey[key]; ok {
			if rankFn(it) > rankFn(out[i]) {
				out[i] = it
			}
			continue
		}
		byKey[key] = len(out)
		out = append(out, it)
	}
	return out
}

// CollectSiteNumbersHTML parses html and returns CollectSiteNumbers(doc,
// pagePoisoned) — a convenience entry point for callers that hold raw HTML
// rather than an already-parsed document (the enriche resolver's
// SiteNumbers accumulator, which calls this at BOTH the homepage and
// /contacts-subpage mergeSite call sites, passing that page's raw-fetch
// poison verdict — see CollectSiteNumbers' pagePoisoned doc comment).
// Mirrors the documentFromHTML + doc-based-helper split every other
// exported HTML-string entry point in this package already uses (see
// ExtractSiteContacts). Returns nil for empty/unparsable HTML.
func CollectSiteNumbersHTML(html string, pagePoisoned bool) []PhoneNumberFact {
	doc, err := documentFromHTML(html)
	if err != nil || doc == nil {
		return nil
	}
	return CollectSiteNumbers(doc, pagePoisoned)
}

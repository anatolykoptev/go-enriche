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
	// "demoted" (an 8-800, or a tel: nested inside a named call-tracking
	// widget).
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
}

// Site-number source labels (PhoneNumberFact.Source). The tierContacts label
// reuses the package's existing regionContacts constant (contacts.go) rather
// than a second "contacts" literal — golangci-lint's goconst flags a fresh
// constant that merely duplicates an existing one's value.
const (
	numSourceSocialLink = "social_link"
	numSourceMicrodata  = "microdata"
	numSourceBody       = "body"
	numSourceDemoted    = "demoted"
)

// numberSourceForTier maps a phoneCandidate tier to its PhoneNumberFact.Source
// label.
func numberSourceForTier(tier int) string {
	switch tier {
	case tierSocialLink:
		return numSourceSocialLink
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
	case tierContacts, tierSocialLink, tierMicrodata:
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
		anchored := numberIsAnchored(c.tier)
		tagged = append(tagged, keyed{
			fact: PhoneNumberFact{
				Value:       c.value,
				Source:      numberSourceForTier(c.tier),
				Anchored:    anchored,
				DNI:         dni,
				Trustworthy: dniTrustworthy(c.tier, dni),
			},
			tier: c.tier,
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
	return facts
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

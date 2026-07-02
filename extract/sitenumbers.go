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
	// DNI is true when the PAGE (not just this candidate) actively runs a
	// known dynamic-number-insertion / call-tracking vendor
	// (detectDNIVendor). It is a page-level signal, deliberately the SAME
	// for every candidate a single CollectSiteNumbers call returns — see
	// Trustworthy for why a per-candidate DNI matcher must not be
	// introduced.
	DNI bool
	// Trustworthy is the COMMITTED verdict, computed by reusing the
	// EXISTING fail-closed page-level DNI gate verbatim
	// (resolvePhoneForCityDNI's own branch): when DNI is true, ONLY a
	// social-link candidate (Source == "social_link") is Trustworthy;
	// otherwise every Anchored candidate is. This must never diverge from
	// what resolvePhoneForCityDNI would decide for the same page — a
	// second, independent per-candidate DNI heuristic here would risk an
	// unlisted vendor failing OPEN (a rotating proxy marked Trustworthy)
	// where the resolver fails closed.
	Trustworthy bool
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

// numberIsTrustworthy is the COMMITTED Trustworthy verdict — see
// PhoneNumberFact.Trustworthy's doc comment. It is byte-for-byte the same
// branch resolvePhoneForCityDNI takes: DNI active -> social-link only;
// otherwise -> every anchored candidate.
func numberIsTrustworthy(tier int, anchored, dni bool) bool {
	if dni {
		return tier == tierSocialLink
	}
	return anchored
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
// Ordering is deterministic: highest tier first, then ascending normalized
// digits, so a fixture yields byte-identical output across repeated runs
// regardless of DOM traversal order.
//
// pickPhoneCandidate (the single-winner resolver) and Facts.Phone are
// UNCHANGED by this function — it is an additive, read-only view over the
// same candidate set collectPhoneCandidates already builds.
func CollectSiteNumbers(doc *goquery.Document) []PhoneNumberFact {
	if doc == nil {
		return nil
	}
	var cands []phoneCandidate
	cands = append(cands, socialLinkCandidates(doc)...)
	cands = append(cands, telCandidates(doc)...)
	cands = append(cands, microdataCandidates(doc)...)
	cands = append(cands, ogPhoneCandidates(doc)...)
	if len(cands) == 0 {
		return nil
	}

	_, dni := detectDNIVendor(doc)

	type keyed struct {
		fact PhoneNumberFact
		tier int
		key  string
	}
	byKey := make(map[string]int, len(cands)) // digit-key -> index into out
	var out []keyed
	for _, c := range cands {
		key := reDigitsOnly.ReplaceAllString(c.value, "")
		if key == "" {
			continue
		}
		anchored := numberIsAnchored(c.tier)
		fact := PhoneNumberFact{
			Value:       c.value,
			Source:      numberSourceForTier(c.tier),
			Anchored:    anchored,
			DNI:         dni,
			Trustworthy: numberIsTrustworthy(c.tier, anchored, dni),
		}
		if i, ok := byKey[key]; ok {
			// Same number seen at another tier (e.g. a header tel: plus
			// repeated itemprop=telephone duplicates for the same number):
			// keep the HIGHER-tier reading — the strongest evidence found
			// for this number wins its classification, mirroring
			// pickPhoneCandidate's own tier-wins tiebreak.
			if c.tier > out[i].tier {
				out[i] = keyed{fact: fact, tier: c.tier, key: key}
			}
			continue
		}
		byKey[key] = len(out)
		out = append(out, keyed{fact: fact, tier: c.tier, key: key})
	}

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

// CollectSiteNumbersHTML parses html and returns CollectSiteNumbers(doc) — a
// convenience entry point for callers that hold raw HTML rather than an
// already-parsed document (the enriche resolver's SiteNumbers accumulator,
// which calls this at BOTH the homepage and /contacts-subpage mergeSite call
// sites). Mirrors the documentFromHTML + doc-based-helper split every other
// exported HTML-string entry point in this package already uses (see
// ExtractSiteContacts). Returns nil for empty/unparsable HTML.
func CollectSiteNumbersHTML(html string) []PhoneNumberFact {
	doc, err := documentFromHTML(html)
	if err != nil || doc == nil {
		return nil
	}
	return CollectSiteNumbers(doc)
}

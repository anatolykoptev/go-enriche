package extract

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// documentFromHTML parses HTML into a goquery document. Returns (nil, nil) for
// empty input so callers can treat "no contacts" uniformly.
func documentFromHTML(html string) (*goquery.Document, error) {
	if html == "" {
		return nil, nil
	}
	return goquery.NewDocumentFromReader(strings.NewReader(html))
}

// SiteContacts holds contact facts extracted directly from a page's DOM
// (tel:/mailto: hrefs, microdata, og: meta). These are the venue's own,
// human-facing contact links — the highest-signal authoritative source on a
// site, ranked above JSON-LD telephone (which call-tracking widgets often
// inject) and far above raw-HTML regex.
//
// Phone carries a region classification so the caller can prefer a number in
// the header/footer/<address>/contacts region over one inside an embedded
// third-party booking widget/iframe — that widget-injected number is exactly
// the wrong-phone vector this extraction layer exists to defeat.
type SiteContacts struct {
	// Phone is the best site-own phone: a contacts-region tel: href if present,
	// else microdata telephone, else any other valid tel: href. nil if none
	// passed validation.
	Phone *string
	// PhoneRegion records where Phone came from: "contacts" (header/footer/
	// address/contacts-heading), "microdata", "other" (a tel: elsewhere on the
	// page, e.g. body), or "" when Phone is nil.
	PhoneRegion string
	// Email is the first mailto: address found, trimmed of any query string.
	// NOTE: Facts has no Email field yet, so applyContactOverride does not
	// consume this — it is groundwork for the source-priority resolver (P2),
	// extracted here so the single DOM pass covers all contact facts.
	Email *string
}

// Phone-region classifications returned in SiteContacts.PhoneRegion.
const (
	regionContacts  = "contacts"  // header/footer/<address>/contacts block
	regionMicrodata = "microdata" // [itemprop=telephone]
	regionOther     = "other"     // a tel: elsewhere on the page (body/widget)
)

// Phone-candidate tiers, highest first. The local-area-code resolver ranks by
// tier only as a fallback (when no candidate is local to the article's city).
const (
	tierDemoted   = 0 // a tel: inside a named call-tracking widget, or an 8-800
	tierMicrodata = 1 // [itemprop=telephone] / og: / JSON-LD prior phone
	tierBody      = 2 // a human-facing tel: in the page body
	tierContacts  = 3 // a tel: in the header/footer/address/contacts region
)

// tollFreeAreaCode is the 8-800 toll-free / call-tracking range. An 8-800 is
// never a venue's local line for a city guide, so it is demoted in the
// source-order fallback (it may still fill a still-nil phone, last).
const (
	tollFreeLo = 800
	tollFreeHi = 800
)

// contactsRegionSelectors mark DOM subtrees that are the venue's own contact
// area. A tel: inside any of these outranks a tel: elsewhere on the page.
const contactsRegionSelectors = "header, footer, address, .contacts, .contact, #contacts, #contact, .footer, .header"

// callTrackingSelectors mark DOM subtrees injected by named call-tracking
// vendors. A tel: inside one of these is a dynamic tracking number, not the
// venue's own line — the wrong-phone vector this layer defeats.
//
// Deliberately NARROW (specific vendor classes only): generic substrings like
// [class*=widget] / [id*=widget] match standard WordPress (.widget,
// widget-area) and Bitrix (bx-widget) wrappers that legitimately contain the
// real contacts-region tel:, which would re-create the wrong-phone bug. A
// generic widget wrapping the contacts block must NOT demote; only a
// call-tracking node nested INSIDE the contacts region does (see
// isCallTrackingDemoted).
const callTrackingSelectors = "[class*=calltrack], [class*=calltouch], [class*=comagic], [class*=mango], .b24-widget, [class*=callibri], [class*=uiscom]"

// ExtractSiteContacts parses already-fetched HTML for the venue's own contact
// facts via the DOM. It performs ZERO network I/O — it is a deterministic,
// in-process read over the same HTML fetch.ExtractFacts already received.
//
// Phone preference order (highest first):
//  1. tel: href inside a contacts region (header/footer/address/contacts),
//     not inside an embedded widget;
//  2. microdata [itemprop=telephone];
//  3. any other valid tel: href on the page (body), widgets last.
//
// Every returned phone must pass ValidatePhone; an invalid candidate is
// skipped, never returned.
func ExtractSiteContacts(html string) SiteContacts {
	var out SiteContacts
	doc, err := documentFromHTML(html)
	if err != nil || doc == nil {
		return out
	}

	out.Phone, out.PhoneRegion = bestTelPhone(doc)
	if out.Phone == nil {
		if mp := microdataPhone(doc); mp != nil {
			out.Phone = mp
			out.PhoneRegion = regionMicrodata
		}
	}
	out.Email = firstMailto(doc)
	return out
}

// telCandidate is one tel: href with its DOM classification.
type telCandidate struct {
	value    string // human-facing display value (link text or href digits)
	contacts bool   // inside a contacts region (and not call-tracking-demoted)
	demoted  bool   // inside a call-tracking widget nested below the contacts region
}

// bestTelPhone returns the highest-priority valid tel: phone and its region.
// Microdata is intentionally NOT considered here — the caller applies it as a
// strict second tier so a contacts-region tel: always wins over microdata,
// and microdata always wins over a body/widget tel:.
func bestTelPhone(doc *goquery.Document) (*string, string) {
	var cands []telCandidate
	doc.Find(`a[href^="tel:"], a[href^="TEL:"]`).Each(func(_ int, s *goquery.Selection) {
		raw, ok := s.Attr("href")
		if !ok {
			return
		}
		// Prefer the visible link text (formatted, e.g. "+7 (812) 615 70 00");
		// fall back to the href payload when the link has no text.
		display := strings.TrimSpace(s.Text())
		if !ValidatePhone(display) {
			display = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(raw, "tel:"), "TEL:"))
		}
		if !ValidatePhone(display) {
			return
		}
		demoted := isCallTrackingDemoted(s)
		cands = append(cands, telCandidate{
			value: display,
			// A contacts-region link counts as "contacts" only when it is not
			// a call-tracking number nested inside that region.
			contacts: !demoted && s.Closest(contactsRegionSelectors).Length() > 0,
			demoted:  demoted,
		})
	})
	if len(cands) == 0 {
		return nil, ""
	}

	// 1. contacts-region, not call-tracking.
	for i := range cands {
		if cands[i].contacts {
			return &cands[i].value, regionContacts
		}
	}
	// 2. any non-demoted tel: (body or generic-widget-wrapped contacts).
	for i := range cands {
		if !cands[i].demoted {
			return &cands[i].value, regionOther
		}
	}
	// 3. last resort: only call-tracking tel: present. Still validated, but a
	// known tracking number — surfaced as the weak "other" tier so it only
	// ever fills a nil phone, never overrides structured data.
	return &cands[0].value, regionOther
}

// isCallTrackingDemoted reports whether a tel: link should be demoted because
// it sits inside a named call-tracking widget that is NOT merely a generic
// wrapper around the contacts region. The rule: demote only when the nearest
// call-tracking ancestor is nested inside (a descendant of) the nearest
// contacts-region ancestor — i.e. the tracking node is inside the contacts
// block, not the contacts block nested inside a tracking wrapper.
//
// Concretely: a header tel: inside <div id="bx-widget-area"> (Bitrix) or a
// footer tel: inside <aside class="widget"> is NOT demoted (those generic
// classes are excluded from callTrackingSelectors entirely). A tel: inside a
// <div class="comagic-phone"> that itself sits in the footer IS demoted.
func isCallTrackingDemoted(s *goquery.Selection) bool {
	ct := s.Closest(callTrackingSelectors)
	if ct.Length() == 0 {
		return false
	}
	contacts := s.Closest(contactsRegionSelectors)
	if contacts.Length() == 0 {
		// Not in a contacts region at all — a tracking number in the body.
		return true
	}
	// Both ancestors exist. Demote only if the call-tracking node is a
	// descendant of (nested below) the contacts node. If the contacts node is
	// instead nested below the tracking node, the tracking selector matched a
	// generic wrapper and we must NOT demote the real contacts tel:.
	return ct.Closest(contactsRegionSelectors).Length() > 0
}

// microdataPhone reads [itemprop=telephone] content/text.
func microdataPhone(doc *goquery.Document) *string {
	var found *string
	doc.Find(`[itemprop="telephone"], [itemprop=telephone]`).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		v := strings.TrimSpace(s.AttrOr("content", ""))
		if v == "" {
			v = strings.TrimSpace(s.Text())
		}
		if ValidatePhone(v) {
			found = &v
			return false
		}
		return true
	})
	return found
}

// firstMailto returns the first mailto: address (sans ?subject= etc.).
func firstMailto(doc *goquery.Document) *string {
	var found *string
	doc.Find(`a[href^="mailto:"], a[href^="MAILTO:"]`).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		raw, ok := s.Attr("href")
		if !ok {
			return true
		}
		addr := strings.TrimPrefix(strings.TrimPrefix(raw, "mailto:"), "MAILTO:")
		if i := strings.IndexByte(addr, '?'); i >= 0 {
			addr = addr[:i]
		}
		addr = strings.TrimSpace(addr)
		if addr != "" && strings.Contains(addr, "@") {
			found = &addr
			return false
		}
		return true
	})
	return found
}

// --- Phase-1 local-area-code resolver (operator Decision 2, 2026-06-17) ---

// phoneCandidate is one site-own phone with the metadata the resolver needs to
// rank it: its DOM region tier and its area code.
type phoneCandidate struct {
	value    string // human-facing display value
	tier     int    // tierContacts / tierMicrodata / tierBody / tierDemoted
	areaCode int    // 3-digit RU area code, or -1
}

// collectPhoneCandidates returns every valid site-own phone candidate, each
// tagged with a region tier and area code. The candidate set is the union of
// (operator Decision 2 sources): tel: hrefs, microdata itemprop=telephone,
// og:/business:contact_data phone meta, and any phone already resolved by
// Layer 1/2 (JSON-LD telephone / regex, passed in via prior). ZERO network
// I/O — a deterministic read over already-fetched HTML.
func collectPhoneCandidates(doc *goquery.Document, prior ...string) []phoneCandidate {
	var cands []phoneCandidate
	// Layer-1/2 phones (JSON-LD telephone, regex) seeded as structured-tier
	// candidates so the local-area-code rule can still pick them when they are
	// the only city-local number.
	for _, pp := range prior {
		if c, ok := makeCandidate(pp, tierMicrodata); ok {
			cands = append(cands, c)
		}
	}
	cands = append(cands, telCandidates(doc)...)
	cands = append(cands, microdataCandidates(doc)...)
	cands = append(cands, ogPhoneCandidates(doc)...)
	return cands
}

// makeCandidate validates a phone string and builds a phoneCandidate at the
// given natural tier, demoting any 8-800 toll-free number to tierDemoted.
// ok is false when the value is empty or fails ValidatePhone.
func makeCandidate(value string, naturalTier int) (phoneCandidate, bool) {
	value = strings.TrimSpace(value)
	if !ValidatePhone(value) {
		return phoneCandidate{}, false
	}
	ac := phoneAreaCode(value)
	tier := naturalTier
	if isTollFree(ac) {
		tier = tierDemoted
	}
	return phoneCandidate{value: value, tier: tier, areaCode: ac}, true
}

// telCandidates collects tel:/TEL: hrefs, classified by DOM region.
func telCandidates(doc *goquery.Document) []phoneCandidate {
	var out []phoneCandidate
	doc.Find(`a[href^="tel:"], a[href^="TEL:"]`).Each(func(_ int, s *goquery.Selection) {
		raw, ok := s.Attr("href")
		if !ok {
			return
		}
		display := strings.TrimSpace(s.Text())
		if !ValidatePhone(display) {
			display = strings.TrimPrefix(strings.TrimPrefix(raw, "tel:"), "TEL:")
		}
		c, ok := makeCandidate(display, telTier(s))
		if ok {
			out = append(out, c)
		}
	})
	return out
}

// telTier classifies a tel: link's DOM region into a natural tier (before any
// 8-800 demotion, which makeCandidate applies).
func telTier(s *goquery.Selection) int {
	switch {
	case isCallTrackingDemoted(s):
		return tierDemoted
	case s.Closest(contactsRegionSelectors).Length() > 0:
		return tierContacts
	default:
		return tierBody
	}
}

// microdataCandidates collects [itemprop=telephone] values.
func microdataCandidates(doc *goquery.Document) []phoneCandidate {
	var out []phoneCandidate
	doc.Find(`[itemprop="telephone"], [itemprop=telephone]`).Each(func(_ int, s *goquery.Selection) {
		v := strings.TrimSpace(s.AttrOr("content", ""))
		if v == "" {
			v = strings.TrimSpace(s.Text())
		}
		if c, ok := makeCandidate(v, tierMicrodata); ok {
			out = append(out, c)
		}
	})
	return out
}

// ogPhoneCandidates collects og:/business:contact_data phone meta — structured
// but not a human-facing site link, so tierMicrodata.
func ogPhoneCandidates(doc *goquery.Document) []phoneCandidate {
	var out []phoneCandidate
	doc.Find(`meta[property="business:contact_data:phone_number"], meta[property="og:phone_number"], meta[name="og:phone_number"]`).Each(func(_ int, s *goquery.Selection) {
		if c, ok := makeCandidate(s.AttrOr("content", ""), tierMicrodata); ok {
			out = append(out, c)
		}
	})
	return out
}

// resolvePhoneForCity picks the best site-own phone given the article's target
// city. The operator's rule (Decision 2): among ALL candidates, prefer the one
// whose area code is local to the city; only when no candidate matches the
// city's area code does it fall back to source-order (tier) ranking.
//
// Returns (phone, region, true) when a candidate is chosen, or ("", "", false)
// when there is no valid candidate. region is "contacts"/"microdata"/"other"
// so the caller (applyContactOverride) keeps its override-vs-fill semantics.
func resolvePhoneForCity(doc *goquery.Document, city string, prior ...string) (string, string, bool) {
	cands := collectPhoneCandidates(doc, prior...)
	if len(cands) == 0 {
		return "", "", false
	}

	// Local-area-code tiebreaker: if the city is known and any candidate is
	// local, choose the highest-tier local candidate. This is what makes the
	// SPb 812 microdata beat a Moscow 925 tel: href on a multi-city venue.
	if expected := expectedAreaCodes(city); len(expected) > 0 {
		best := -1
		for i := range cands {
			if !areaCodeMatches(cands[i].areaCode, expected) {
				continue
			}
			if best < 0 || cands[i].tier > cands[best].tier {
				best = i
			}
		}
		if best >= 0 {
			return cands[best].value, regionForTier(cands[best].tier), true
		}
	}

	// Fallback: source-order (tier) ranking — contacts-region tel: > body
	// tel: > microdata/og: > demoted (call-tracking / 8-800). The venue's own
	// human-facing tel: outranks structured telephone data; this is the plain
	// tel:-wins behavior used when there is no city signal or no local
	// candidate exists.
	best := 0
	for i := range cands {
		if cands[i].tier > cands[best].tier {
			best = i
		}
	}
	return cands[best].value, regionForTier(cands[best].tier), true
}

// isTollFree reports whether a 3-digit area code is in the 8-800 toll-free
// range — never a venue's local line for a city guide.
func isTollFree(areaCode int) bool {
	return areaCode >= tollFreeLo && areaCode <= tollFreeHi
}

// regionForTier maps a candidate tier back to the region label
// applyContactOverride branches on.
func regionForTier(tier int) string {
	switch tier {
	case tierContacts:
		return regionContacts
	case tierMicrodata:
		return regionMicrodata
	default:
		return regionOther
	}
}

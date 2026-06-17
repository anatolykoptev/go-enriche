package extract

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
)

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
	Email *string
}

// Phone-region classifications returned in SiteContacts.PhoneRegion.
const (
	regionContacts  = "contacts"  // header/footer/<address>/contacts block
	regionMicrodata = "microdata" // [itemprop=telephone]
	regionOther     = "other"     // a tel: elsewhere on the page (body/widget)
)

// contactsRegionSelectors mark DOM subtrees that are the venue's own contact
// area. A tel: inside any of these outranks a tel: elsewhere on the page.
const contactsRegionSelectors = "header, footer, address, .contacts, .contact, #contacts, #contact, .footer, .header"

// widgetAncestorSelectors mark DOM subtrees owned by embedded third-party
// booking/call-tracking widgets. A tel: inside one of these is demoted — it is
// the wrong-phone vector (a call-tracking number injected by mango/calltouch/
// comagic/etc.), not the venue's own line.
const widgetAncestorSelectors = "iframe, .widget, .b24-widget, [class*=calltrack], [class*=calltouch], [class*=comagic], [id*=widget]"

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
	if strings.TrimSpace(html) == "" {
		return out
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
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
	contacts bool   // inside a contacts region
	widget   bool   // inside an embedded third-party widget/iframe
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
		cands = append(cands, telCandidate{
			value:    display,
			contacts: s.Closest(contactsRegionSelectors).Length() > 0,
			widget:   s.Closest(widgetAncestorSelectors).Length() > 0,
		})
	})
	if len(cands) == 0 {
		return nil, ""
	}

	// 1. contacts-region, non-widget.
	for i := range cands {
		if cands[i].contacts && !cands[i].widget {
			return &cands[i].value, regionContacts
		}
	}
	// 2. any non-widget tel: (body).
	for i := range cands {
		if !cands[i].widget {
			return &cands[i].value, regionOther
		}
	}
	// 3. last resort: a widget tel: (still validated). Rare, but better than
	// dropping the only number on a page that is all-widget.
	return &cands[0].value, regionOther
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

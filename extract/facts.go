package extract

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/anatolykoptev/go-enriche/structured"
)

// Facts holds structured data extracted from a page.
type Facts struct {
	PlaceName *string
	PlaceType *string
	Address   *string
	// LegalAddress holds a registered/legal-entity address (юридический адрес)
	// extracted from the same page as a SEPARATE, additive sidecar — it is NEVER
	// the venue's geo address. Many RU /contacts pages print BOTH the legal office
	// (ИНН/ОГРН line, литера/помещение, postal index — the registered seat, often
	// across the city or oblast from the venue) AND the venue's visiting address.
	// Collapsing both into Address inverts precedence: a high-confidence
	// official-site LEGAL address would overwrite the geo-correct (lower-confidence)
	// maps VENUE address, and the card's address-string-driven map link would point
	// at the office, not the venue. Keeping them distinct lets the resolver route
	// the legal one here (rendered as «Реквизиты», never the map slot) and leave the
	// venue slot to the maps/geo address. This is the split-identity-address fix.
	LegalAddress *string
	Phone        *string
	Price        *string
	Website      *string
	Hours        *string
	Email        *string
	EventDate    *string
	Latitude     *float64
	Longitude    *float64

	// PhonePoisoned is true when the official site carries a DNI/call-tracking
	// vendor (Roistat/Calltouch/Comagic/Mango/Callibri/UIS) and has NO DNI-immune
	// phone (a hard-coded social-link number): every tel:/microdata candidate is a
	// rotating proxy, so Phone is deliberately omitted (set nil). This is DISTINCT
	// from "the site had no phone at all" — both leave Phone nil, but only a poison
	// omit must OUTRANK a lower-priority maps/search phone at the resolver. A bare
	// nil Phone says nothing; PhonePoisoned says "refuse, and block lower fills".
	// The whole anti-fab fix turns on keeping these two signals distinct.
	PhonePoisoned bool
}

// ExtractFacts extracts structured facts from HTML using a cascade:
//  1. Schema.org structured data (JSON-LD + Microdata) via structured.Parse
//  2. Pre-compiled regex fallback for address/phone/price
//  3. Official-site contact override (applyContactOverride): a tel: href the
//     venue put on its own page is the highest-signal phone authority and
//     outranks JSON-LD/microdata/regex — see applyContactOverride.
func ExtractFacts(html, pageURL string) Facts {
	return ExtractFactsForCity(html, pageURL, "")
}

// ExtractFactsForCity is ExtractFacts plus the article's target city, which
// drives the local-area-code phone tiebreaker (operator Decision 2,
// 2026-06-17): among all official-site phone candidates (tel: href + microdata)
// the resolver prefers the one whose area code is local to the city, and only
// falls back to source-order ranking when no candidate matches the city's area
// code. An empty city disables the tiebreaker (identical to the old behavior).
func ExtractFactsForCity(html, pageURL, city string) Facts {
	var facts Facts

	if html == "" {
		return facts
	}

	// Layer 1: structured data (JSON-LD + Microdata).
	data, err := structured.Parse(strings.NewReader(html), "text/html", pageURL)
	if err == nil && data != nil {
		applyPlaceFacts(data, &facts)
		applyArticleFacts(data, &facts)
		applyEventFacts(data, &facts)
		applyOrgFacts(data, html, &facts)
	}

	// Layer 2: regex fallback — only fills nil fields. Scoped to
	// boilerplate-stripped HTML (script/style/noscript/... removed — see
	// stripBoilerplate) rather than raw html, so a CSS/script decimal can
	// never surface as a junk "phone" (the Novoclinic bug — see
	// applyRegexFallback / stripBoilerplateHTML).
	applyRegexFallback(stripBoilerplateHTML(html), &facts)

	// Layer 3: official-site contact override — a contacts-region tel: href is
	// authoritative and overrides whatever Layer 1/2 produced; a body tel:
	// only fills a still-nil phone. With a city hint, a candidate local to the
	// city (any region) overrides — that is the local-area-code rule.
	applyContactOverride(html, city, &facts)

	// Layer 3.5: bare <address> element. The regex address path needs an
	// «адрес:» label; a contacts page that renders its address only inside an
	// <address> block (common on contacts subpages) would be missed. Fill-if-nil
	// so a structured/labeled address (Layer 1/2) always wins.
	applyAddressElement(html, &facts)

	// Layer 4: site-own email (first mailto:). A venue's own mailto: link is a
	// reliable contact fact; no call-tracking rotation applies to email, so it
	// fills directly when present (fill-if-nil — never clobber a structured
	// email a future Layer 1 could supply).
	applyEmail(html, &facts)

	// Layer 5: visible Russian opening-hours block («Режим/Часы/Время работы»)
	// when structured data carried no hours. Many RU SMB sites render hours only
	// as visible text, never as openingHoursSpecification — this surfaces them.
	// Fill-if-nil: a structured Hours (Layer 1) always wins.
	if facts.Hours == nil {
		if h := ExtractVisibleHours(html); h != "" {
			facts.Hours = &h
		}
	}

	return facts
}

// applyContactOverride lets the venue's own tel: href win over JSON-LD,
// microdata, and regex. Phone authority order, highest first:
//
//	contacts-region tel: href  >  JSON-LD/microdata telephone (Layer 1)  >
//	body tel: href / microdata-fallback  >  regex (Layer 2)
//
// Rationale: call-tracking widgets routinely inject a JSON-LD/microdata
// telephone (a dynamic 8-800 tracking number) while the human-facing tel:
// link in the header/footer/contacts block is the venue's real line. When a
// contacts-region tel: exists it overrides unconditionally. A tel: found only
// in the page body (region "other") is weaker than structured data, so it
// fills only when Layer 1/2 left Phone nil. The microdata fallback inside
// ExtractSiteContacts covers itemprop=telephone outside a recognized
// Place/Organization scope (structured.Parse already handles in-scope ones).
//
// ValidatePhone gates every candidate inside ExtractSiteContacts, so this
// never lowers phone validity.
func applyContactOverride(html, city string, facts *Facts) {
	doc, err := documentFromHTML(html)
	if err != nil || doc == nil {
		return
	}

	// Seed the resolver with the Layer-1/2 phone (JSON-LD telephone / regex)
	// already on facts, so the local-area-code rule can pick it when it is the
	// only city-local candidate.
	var prior []string
	if facts.Phone != nil {
		prior = append(prior, *facts.Phone)
	}

	// Resolve once. When the city is known, resolvePhoneForCity already
	// applied the local-area-code rule (Decision 2): a candidate local to the
	// city — any region — was chosen and is authoritative for this city, so it
	// overrides any prior phone unconditionally. Otherwise the result is the
	// source-order pick (contacts tel: > body tel: > microdata/og:), which
	// keeps its override-vs-fill semantics below.
	phone, region, ok, dniOmit := resolvePhoneForCityDNI(doc, city, prior...)
	if dniOmit {
		// A DNI/call-tracking vendor is present and no DNI-immune source exists:
		// every injected tel:/microdata candidate is a rotating proxy. Omit the
		// phone entirely — including any Layer-1/2 value, which the vendor can
		// rewrite — so the agent shows «уточняйте» rather than a rotating number.
		// Flag PhonePoisoned so the source-priority resolver treats this as a
		// first-class "refuse" verdict that outranks a maps/search phone, not as a
		// mere absence (which would let the already-merged maps proxy survive).
		facts.Phone = nil
		facts.PhonePoisoned = true
		return
	}
	if !ok {
		return
	}
	if expected := expectedAreaCodes(city); len(expected) > 0 && matchesCity(phone, expected) {
		p := phone
		facts.Phone = &p
		return
	}
	switch region {
	case regionContacts, regionMicrodata:
		// Authoritative: override any prior phone.
		p := phone
		facts.Phone = &p
	default:
		// region "other" (body / widget tel:): fill only if still nil.
		if facts.Phone == nil {
			p := phone
			facts.Phone = &p
		}
	}
}

// applyAddressElement fills Facts.Address from the first valid <address> HTML
// element when the field is still nil. Fill-if-nil — a structured or labeled
// address (Layer 1/2) is more precise and always wins.
func applyAddressElement(html string, facts *Facts) {
	// Route through setAddressFact so a bare <address> legal seat fills
	// LegalAddress, not Address. Skip the parse only when BOTH slots are filled
	// (the venue slot may still be empty while LegalAddress is set, and vice
	// versa — a contacts page can carry both in separate <address> blocks).
	if facts.Address != nil && facts.LegalAddress != nil {
		return
	}
	doc, err := documentFromHTML(html)
	if err != nil || doc == nil {
		return
	}
	firstAddressElements(doc, facts)
}

// applyEmail fills Facts.Email from the first mailto: link on the page when the
// field is still nil. Email is not subject to call-tracking/DNI rotation, so no
// poison gate is needed — the site's own mailto: is authoritative. A still-nil
// Email after this is simply "no email on this page".
func applyEmail(html string, facts *Facts) {
	if facts.Email != nil {
		return
	}
	doc, err := documentFromHTML(html)
	if err != nil || doc == nil {
		return
	}
	if e := firstMailto(doc); e != nil {
		facts.Email = e
	}
}

func applyPlaceFacts(data *structured.Data, facts *Facts) {
	place := data.FirstPlace()
	if place == nil {
		return
	}
	setIfNil(&facts.PlaceName, place.Name)
	setIfNil(&facts.PlaceType, place.Type)
	setAddressIfValid(facts, place.Address)
	setIfValid(&facts.Phone, place.Phone, ValidatePhone)
	setIfNil(&facts.Website, place.Website)
	setIfNil(&facts.Hours, place.Hours)
	setIfValid(&facts.Price, place.Price, ValidatePrice)
}

func applyArticleFacts(data *structured.Data, facts *Facts) {
	article := data.FirstArticle()
	if article == nil {
		return
	}
	setIfNil(&facts.EventDate, article.DatePublished)
}

func applyEventFacts(data *structured.Data, facts *Facts) {
	event := data.FirstEvent()
	if event == nil {
		return
	}
	setIfNil(&facts.PlaceName, event.Name)
	setIfNil(&facts.EventDate, event.StartDate)
	setIfValid(&facts.Price, event.Price, ValidatePrice)
	setAddressIfValid(facts, event.Location)
}

func applyOrgFacts(data *structured.Data, html string, facts *Facts) {
	org := data.FirstOrganization()
	if org == nil {
		return
	}
	setIfNil(&facts.PlaceName, org.Name)
	setIfNil(&facts.Website, org.URL)
	setIfValid(&facts.Phone, org.Phone, ValidatePhone)
	// An Organization itemtype CAN be the registered legal entity, so its address
	// CAN be the legal/registered seat — but only when a corroborating legal signal
	// is present. A bare Organization block whose streetAddress is in fact the
	// venue's visiting address (no separate Place block, no legal identifiers) must
	// NOT have that address demoted to LegalAddress, or the venue loses its map slot
	// (the same false-demote class as the литера-substring bug, via the provenance
	// signal). Route to LegalAddress by PROVENANCE only when at least one corroborant
	// holds; otherwise fall through to the STRING arm so a markerless venue address
	// STAYS venue. This is a routing refinement — it adds no new writer of the
	// Address slot (orgAddressIsLegal picks between the two existing routes).
	if orgAddressIsLegal(org, html, facts) {
		// Legal by corroborated provenance — e.g. drive-igora's seat (streetAddress
		// of an Organization block whose item also carries ИНН), which carries no
		// ИНН/ОГРН in the address string itself yet is unambiguously the legal seat.
		setOrgAddressIfValid(facts, org.Address)
	} else {
		// No corroborant: the Org streetAddress is the only address signal and looks
		// like a venue address — keep it in the venue slot via string classification.
		setAddressIfValid(facts, org.Address)
	}
	setIfNil(&facts.Hours, org.Hours)
}

// orgAddressIsLegal decides whether a schema.org/Organization streetAddress should
// be treated as a legal/registered seat (→ LegalAddress) rather than a venue
// visiting address (→ Address). The Organization itemtype ALONE is not enough —
// many RU SMB sites wrap their single visiting address in an Organization block.
// At least ONE corroborating legal signal is required:
//
//  1. the address STRING itself carries a strong legal marker (isLegalAddress —
//     ИНН/ОГРН/ОГРНИП/КПП/юридический/реквизиты/entity-form), OR
//  2. an ИНН/ОГРН/ОГРНИП/КПП (or taxID/vatID) appears anywhere in the Org item
//     (org.HasLegalID), OR
//  3. the Org item carries a legalName property (org.LegalName), OR
//  4. the page ALSO carries a distinct venue address that DIFFERS from the Org
//     streetAddress — i.e. a Place/LocalBusiness block already filled the venue
//     slot, so the Org address is the OTHER (legal) one.
//
// A page-SCOPE legal-entity marker (an ИНН/ОГРН printed anywhere on the page, e.g.
// in footer requisites) is DELIBERATELY NOT a corroborant. Footer requisites are
// near-universal on RU venue sites, so keying on them re-opened the false-demote:
// a venue page with footer ИНН/ОГРН plus a bare Organization block carrying the
// venue's OWN visiting streetAddress would route that address to LegalAddress and
// empty the map slot — the same class as the литера-substring bug, via a third
// signal. Corroborant #4 already covers the only real live shape (a distinct
// venue address present elsewhere), so the page-scope marker added false-demote
// risk with no recall. A footer ИНН proves a legal entity exists on the page; it
// does NOT prove the Org block's streetAddress is the registered seat.
//
// Absent ALL four, the lone Organization address is routed through the string arm
// so a markerless venue address stays in the map slot. facts is read-only here.
func orgAddressIsLegal(org *structured.Organization, html string, facts *Facts) bool {
	if org.HasLegalID || org.LegalName != nil {
		return true // corroborant #2 (in-item ИНН/ОГРН/taxID/vatID) / #3 (legalName)
	}
	if org.Address != nil && isLegalAddress(*org.Address) {
		return true // corroborant #1 (legal marker in the address string itself)
	}
	// corroborant #4: the page carries a DISTINCT venue address that differs from
	// the Org streetAddress — so the Org address is the OTHER (legal) one. The
	// distinct venue can come from a Place/LocalBusiness block that already filled
	// facts.Address (Layer 1, runs before this), OR from a visible <address> element
	// (Layer 3.5, runs after this — so it is read here directly, read-only). This is
	// the Игора-without-ИНН shape: a display:none Organization legal seat plus a
	// visible <address> venue.
	if org.Address != nil {
		orgAddr := strings.TrimSpace(*org.Address)
		if facts.Address != nil && strings.TrimSpace(*facts.Address) != orgAddr {
			return true
		}
		if venue := firstVenueAddressElement(html); venue != nil &&
			strings.TrimSpace(*venue) != orgAddr {
			return true
		}
	}
	return false
}

// stripBoilerplateHTML parses html, removes boilerplate elements (script/
// style/noscript/nav/ads/... — see stripBoilerplate in goquery.go), and
// re-serializes the result back to HTML. applyRegexFallback below scans this
// instead of the raw page so a CSS/script decimal (e.g. a letter-spacing
// value like "0.06153846153846154em") can no longer read as a digit-shaped
// junk phone/address/price — the root cause of the Novoclinic false phone
// (84615384615, which even ValidatePhone accepted). Falls back to the raw
// html on a parse error, so a malformed page still gets scanned rather than
// silently dropped.
func stripBoilerplateHTML(html string) string {
	doc, err := documentFromHTML(html)
	if err != nil || doc == nil {
		return html
	}
	stripBoilerplate(doc)
	stripped, err := goquery.OuterHtml(doc.Selection)
	if err != nil {
		return html
	}
	return stripped
}

func applyRegexFallback(html string, facts *Facts) {
	if addr := regexAddress(html); addr != nil && ValidateAddress(*addr) {
		setAddressFact(facts, *addr)
	}
	if facts.Phone == nil {
		if phone := regexPhone(html); phone != nil && ValidatePhone(*phone) {
			facts.Phone = phone
		}
	}
	if facts.Price == nil {
		if price := regexPrice(html); price != nil && ValidatePrice(*price) {
			facts.Price = price
		}
	}
}

// ExtractSnippetFacts extracts address/phone/price from plain-text snippets.
// Only fills nil fields in existing facts — never overwrites.
// Validates extracted values to avoid search-title junk (e.g. "адрес и фото").
func ExtractSnippetFacts(text string, facts *Facts) {
	if text == "" || facts == nil {
		return
	}
	if addr := regexSubmatch(reSnippetAddress, text); addr != nil && ValidateAddress(*addr) {
		setAddressFact(facts, *addr)
	}
	if facts.Phone == nil {
		if phone := regexMatch(rePhone, text); phone != nil && ValidatePhone(*phone) {
			facts.Phone = phone
		}
	}
	if facts.Price == nil {
		if price := regexSubmatch(reSnippetPrice, text); price != nil && ValidatePrice(*price) {
			facts.Price = price
		}
	}
}

// setIfNil sets *dst to src if *dst is currently nil and src is non-nil.
func setIfNil(dst **string, src *string) {
	if *dst == nil && src != nil {
		*dst = src
	}
}

// setIfValid sets *dst to src if *dst is nil, src is non-nil, and validate returns true.
func setIfValid(dst **string, src *string, validate func(string) bool) {
	if *dst == nil && src != nil && validate(*src) {
		*dst = src
	}
}

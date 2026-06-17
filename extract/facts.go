package extract

import (
	"strings"

	"github.com/anatolykoptev/go-enriche/structured"
)

// Facts holds structured data extracted from a page.
type Facts struct {
	PlaceName *string
	PlaceType *string
	Address   *string
	Phone     *string
	Price     *string
	Website   *string
	Hours     *string
	EventDate *string
	Latitude  *float64
	Longitude *float64
}

// ExtractFacts extracts structured facts from HTML using a cascade:
//  1. Schema.org structured data (JSON-LD + Microdata) via structured.Parse
//  2. Pre-compiled regex fallback for address/phone/price
//  3. Official-site contact override (applyContactOverride): a tel: href the
//     venue put on its own page is the highest-signal phone authority and
//     outranks JSON-LD/microdata/regex — see applyContactOverride.
func ExtractFacts(html, pageURL string) Facts {
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
		applyOrgFacts(data, &facts)
	}

	// Layer 2: regex fallback — only fills nil fields.
	applyRegexFallback(html, &facts)

	// Layer 3: official-site contact override — a contacts-region tel: href is
	// authoritative and overrides whatever Layer 1/2 produced; a body tel:
	// only fills a still-nil phone.
	applyContactOverride(html, &facts)

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
func applyContactOverride(html string, facts *Facts) {
	contacts := ExtractSiteContacts(html)
	if contacts.Phone == nil {
		return
	}
	switch contacts.PhoneRegion {
	case regionContacts, regionMicrodata:
		// Authoritative: override any prior phone.
		facts.Phone = contacts.Phone
	default:
		// region "other" (body / widget tel:): fill only if still nil.
		if facts.Phone == nil {
			facts.Phone = contacts.Phone
		}
	}
}

func applyPlaceFacts(data *structured.Data, facts *Facts) {
	place := data.FirstPlace()
	if place == nil {
		return
	}
	setIfNil(&facts.PlaceName, place.Name)
	setIfNil(&facts.PlaceType, place.Type)
	setIfValid(&facts.Address, place.Address, ValidateAddress)
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
	setIfValid(&facts.Address, event.Location, ValidateAddress)
}

func applyOrgFacts(data *structured.Data, facts *Facts) {
	org := data.FirstOrganization()
	if org == nil {
		return
	}
	setIfNil(&facts.PlaceName, org.Name)
	setIfNil(&facts.Website, org.URL)
	setIfValid(&facts.Phone, org.Phone, ValidatePhone)
	setIfValid(&facts.Address, org.Address, ValidateAddress)
}

func applyRegexFallback(html string, facts *Facts) {
	if facts.Address == nil {
		if addr := regexAddress(html); addr != nil && ValidateAddress(*addr) {
			facts.Address = addr
		}
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
	if facts.Address == nil {
		if addr := regexSubmatch(reSnippetAddress, text); addr != nil && ValidateAddress(*addr) {
			facts.Address = addr
		}
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

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
}

// ExtractFacts extracts structured facts from HTML using a cascade:
// 1. Schema.org structured data (JSON-LD + Microdata) via structured.Parse
// 2. Pre-compiled regex fallback for address/phone/price
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

	return facts
}

func applyPlaceFacts(data *structured.Data, facts *Facts) {
	place := data.FirstPlace()
	if place == nil {
		return
	}
	setIfNil(&facts.PlaceName, place.Name)
	setIfNil(&facts.PlaceType, place.Type)
	setIfNil(&facts.Address, place.Address)
	setIfNil(&facts.Phone, place.Phone)
	setIfNil(&facts.Website, place.Website)
	setIfNil(&facts.Hours, place.Hours)
	setIfNil(&facts.Price, place.Price)
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
	setIfNil(&facts.Price, event.Price)
	if event.Location != nil {
		setIfNil(&facts.Address, event.Location)
	}
}

func applyOrgFacts(data *structured.Data, facts *Facts) {
	org := data.FirstOrganization()
	if org == nil {
		return
	}
	setIfNil(&facts.PlaceName, org.Name)
	setIfNil(&facts.Website, org.URL)
	setIfNil(&facts.Phone, org.Phone)
	setIfNil(&facts.Address, org.Address)
}

func applyRegexFallback(html string, facts *Facts) {
	if facts.Address == nil {
		facts.Address = regexAddress(html)
	}
	if facts.Phone == nil {
		facts.Phone = regexPhone(html)
	}
	if facts.Price == nil {
		facts.Price = regexPrice(html)
	}
}

// setIfNil sets *dst to src if *dst is currently nil and src is non-nil.
func setIfNil(dst **string, src *string) {
	if *dst == nil && src != nil {
		*dst = src
	}
}

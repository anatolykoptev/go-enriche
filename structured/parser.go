package structured

import (
	"fmt"
	"io"
	"strings"

	"github.com/astappiev/microdata"
)

// Schema.org type names used for lookup.
const (
	typeArticle      = "Article"
	typeNewsArticle  = "NewsArticle"
	typeBlogPosting  = "BlogPosting"
	typeWebPage      = "WebPage"
	typeEvent        = "Event"
	typeOrganization = "Organization"
	typeCorporation  = "Corporation"
	typeGovOrg       = "GovernmentOrganization"
)

// placeTypes lists schema.org Place subtypes to search for.
var placeTypes = []string{
	"LocalBusiness", "Restaurant", "CafeOrCoffeeShop", "BarOrPub",
	"Hotel", "Store", "Place", "TouristAttraction", "Museum",
	"SportsActivityLocation", "EntertainmentBusiness",
}

// Parse extracts JSON-LD and Microdata from HTML.
func Parse(r io.Reader, contentType, pageURL string) (*Data, error) {
	md, err := microdata.ParseHTML(r, contentType, pageURL)
	if err != nil {
		return nil, fmt.Errorf("microdata parse: %w", err)
	}
	return &Data{raw: md}, nil
}

// Raw returns the underlying microdata for advanced use.
func (d *Data) Raw() *microdata.Microdata { return d.raw }

// FirstPlace finds the first Place-like item.
func (d *Data) FirstPlace() *Place {
	for _, t := range placeTypes {
		if item := d.raw.GetFirstOfSchemaType(t); item != nil {
			return itemToPlace(item)
		}
	}
	return nil
}

// FirstArticle finds the first Article-like item.
func (d *Data) FirstArticle() *Article {
	for _, t := range []string{typeArticle, typeNewsArticle, typeBlogPosting, typeWebPage} {
		if item := d.raw.GetFirstOfSchemaType(t); item != nil {
			return itemToArticle(item)
		}
	}
	return nil
}

// FirstEvent finds the first Event item.
func (d *Data) FirstEvent() *Event {
	if item := d.raw.GetFirstOfSchemaType(typeEvent); item != nil {
		return itemToEvent(item)
	}
	return nil
}

// FirstOrganization finds the first Organization item.
func (d *Data) FirstOrganization() *Organization {
	for _, t := range []string{typeOrganization, typeCorporation, typeGovOrg} {
		if item := d.raw.GetFirstOfSchemaType(t); item != nil {
			return itemToOrganization(item)
		}
	}
	return nil
}

func itemToPlace(item *microdata.Item) *Place {
	p := &Place{
		Name:    propString(item, "name"),
		Type:    itemType(item),
		Phone:   propString(item, "telephone"),
		Website: propString(item, "url"),
		Hours:   propString(item, "openingHours"),
	}
	p.Address = extractAddress(item)
	p.Price = extractPrice(item)
	return p
}

func itemToArticle(item *microdata.Item) *Article {
	return &Article{
		Headline:      propString(item, "headline", "name"),
		Author:        extractAuthor(item),
		Description:   propString(item, "description"),
		DatePublished: propString(item, "datePublished"),
		Image:         propString(item, "image"),
	}
}

func itemToEvent(item *microdata.Item) *Event {
	return &Event{
		Name:      propString(item, "name"),
		StartDate: propString(item, "startDate"),
		EndDate:   propString(item, "endDate"),
		Location:  extractEventLocation(item),
		Price:     extractPrice(item),
	}
}

func itemToOrganization(item *microdata.Item) *Organization {
	return &Organization{
		Name:    propString(item, "name"),
		URL:     propString(item, "url"),
		Phone:   propString(item, "telephone"),
		Address: extractAddress(item),
	}
}

// propString returns the first string value for any of the given keys.
func propString(item *microdata.Item, keys ...string) *string {
	for _, key := range keys {
		val, ok := item.GetProperty(key)
		if !ok {
			continue
		}
		var s string
		switch v := val.(type) {
		case string:
			s = strings.TrimSpace(v)
		case fmt.Stringer:
			s = strings.TrimSpace(v.String())
		default:
			s = strings.TrimSpace(fmt.Sprint(v))
		}
		if s != "" {
			return &s
		}
	}
	return nil
}

// itemType returns the first schema.org type stripped of the prefix.
func itemType(item *microdata.Item) *string {
	if len(item.Types) == 0 {
		return nil
	}
	t := item.Types[0]
	t = strings.TrimPrefix(t, "https://schema.org/")
	t = strings.TrimPrefix(t, "http://schema.org/")
	return &t
}

// extractAddress builds an address string from nested PostalAddress or plain string.
func extractAddress(item *microdata.Item) *string {
	nested, ok := item.GetNestedItem("address")
	if ok {
		parts := make([]string, 0, addressPartCount)
		for _, key := range []string{"streetAddress", "addressLocality", "addressRegion", "postalCode"} {
			if s := propString(nested, key); s != nil {
				parts = append(parts, *s)
			}
		}
		if len(parts) > 0 {
			joined := strings.Join(parts, ", ")
			return &joined
		}
	}
	return propString(item, "address")
}

const addressPartCount = 4

// extractPrice gets price from offers or priceRange.
func extractPrice(item *microdata.Item) *string {
	if nested, ok := item.GetNestedItem("offers"); ok {
		if p := propString(nested, "price"); p != nil {
			return p
		}
	}
	return propString(item, "priceRange")
}

// extractAuthor gets author name from nested Person or plain string.
func extractAuthor(item *microdata.Item) *string {
	if nested, ok := item.GetNestedItem("author"); ok {
		return propString(nested, "name")
	}
	return propString(item, "author")
}

// extractEventLocation gets location name from nested Place or plain string.
func extractEventLocation(item *microdata.Item) *string {
	if nested, ok := item.GetNestedItem("location"); ok {
		return propString(nested, "name")
	}
	return propString(item, "location")
}

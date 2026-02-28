package structured

import "github.com/astappiev/microdata"

// Data wraps parsed microdata with typed accessors.
type Data struct {
	raw *microdata.Microdata
}

// Place represents a schema.org Place/LocalBusiness.
type Place struct {
	Name    *string
	Type    *string
	Address *string
	Phone   *string
	Website *string
	Hours   *string
	Price   *string
}

// Article represents a schema.org Article/NewsArticle.
type Article struct {
	Headline      *string
	Author        *string
	Description   *string
	DatePublished *string
	Image         *string
}

// Event represents a schema.org Event.
type Event struct {
	Name      *string
	StartDate *string
	EndDate   *string
	Location  *string
	Price     *string
}

// Organization represents a schema.org Organization.
type Organization struct {
	Name    *string
	URL     *string
	Phone   *string
	Address *string
}

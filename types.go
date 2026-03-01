package enriche

import (
	"time"

	"github.com/anatolykoptev/go-enriche/extract"
	"github.com/anatolykoptev/go-enriche/fetch"
)

// Mode specifies the enrichment mode.
type Mode int

const (
	ModeNews   Mode = iota // News articles
	ModePlaces             // Places and businesses
	ModeEvents             // Events and happenings
)

// Item is the input for enrichment.
type Item struct {
	Name   string // required
	URL    string // optional — if empty, search-only enrichment
	City   string // optional — for places/events
	Mode   Mode
	Source string // origin identifier
	Topic  string // classification tag
}

// Result is the output of enrichment.
type Result struct {
	Name          string
	URL           string
	Status        PageStatus   // page availability status
	Content       string       // extracted article text
	Image         *string      // og:image URL
	PublishedAt   *time.Time   // extracted publication date
	Facts         Facts        // structured data
	SearchContext string       // search engine context
	SearchSources []string     // source URLs from search
	Metadata      *ContentMeta // title/author/language
}

// Facts is re-exported from extract package.
type Facts = extract.Facts

// PageStatus is re-exported from fetch package.
type PageStatus = fetch.PageStatus

// Re-exported PageStatus constants so consumers don't need to import fetch.
const (
	StatusActive      = fetch.StatusActive
	StatusNotFound    = fetch.StatusNotFound
	StatusRedirect    = fetch.StatusRedirect
	StatusUnreachable = fetch.StatusUnreachable
	StatusWebsiteDown = fetch.StatusWebsiteDown
	StatusClosed      = fetch.StatusClosed
)

// ContentMeta holds article metadata extracted by trafilatura.
type ContentMeta struct {
	Title       string
	Author      string
	Description string
	Language    string
	SiteName    string
}

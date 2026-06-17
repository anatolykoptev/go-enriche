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
	Name    string // required
	URL     string // optional — if empty, search-only enrichment
	City    string // optional — for places/events
	Address string // optional — known street address (e.g. "Невский проспект, 28"); used to anchor map lookups
	Mode    Mode
	Source  string // origin identifier
	Topic   string // classification tag

	// Latitude and Longitude are authoritative coordinates provided by the
	// discovery source (e.g. KudaGo). When both are non-nil they take
	// precedence over any coords resolved by the maps checker or geocoder.
	// Must be provided as a pair (both or neither); a lone non-nil Latitude
	// with a nil Longitude is treated as absent.
	Latitude  *float64
	Longitude *float64

	// Seed carries operator-verified field values that MUST win over every
	// enrich-derived source. Empty fields impose no override. This is the
	// override-precedence channel (Phase 3): the content layer re-supplies a
	// previously operator-verified phone/price on every re-enrich so a rotating
	// DNI proxy or a maps card can never silently replace it.
	Seed SeedFacts
}

// SeedFacts holds operator-verified field values injected at the top source
// priority (operator_verified) before any fetch/maps/search runs. A non-empty
// value pins that field; an empty value leaves the field to normal resolution.
type SeedFacts struct {
	PlaceName string
	Address   string
	Phone     string
	Website   string
	Hours     string
	Price     string
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

	// Provenance carries the resolved per-field {source, confidence} for the
	// contact/business facts (Phase 3, ONE_WAY). It rides ALONGSIDE the bare
	// *string Facts (which keep their wire shape) as an additive sidecar so the
	// content layer can persist provenance and protect operator_verified values
	// on re-enrich. Absent fields stay zero-valued.
	Provenance Provenance
}

// FieldProvenance is the resolved provenance of one fact: the winning source
// ("operator_verified" | "official_site" | "aggregator" | "maps" | "search")
// and the coarse confidence bucket ("high" | "medium" | "low"). Zero value
// (empty strings) means the field was absent / had no resolved source.
type FieldProvenance struct {
	Source     string `json:"source,omitempty"`
	Confidence string `json:"confidence,omitempty"`
}

// Provenance is the per-field provenance sidecar for a Result's contact facts.
// Only fields that resolved to a value carry a non-empty FieldProvenance.
type Provenance struct {
	PlaceName FieldProvenance `json:"place_name,omitempty"`
	Address   FieldProvenance `json:"address,omitempty"`
	Phone     FieldProvenance `json:"phone,omitempty"`
	Website   FieldProvenance `json:"website,omitempty"`
	Hours     FieldProvenance `json:"hours,omitempty"`
	Price     FieldProvenance `json:"price,omitempty"`
}

// Facts is re-exported from extract package.
type Facts = extract.Facts

// PageStatus is re-exported from fetch package.
type PageStatus = fetch.PageStatus

// Re-exported PageStatus constants so consumers don't need to import fetch.
const (
	StatusActive          = fetch.StatusActive
	StatusNotFound        = fetch.StatusNotFound
	StatusRedirect        = fetch.StatusRedirect
	StatusUnreachable     = fetch.StatusUnreachable
	StatusWebsiteDown     = fetch.StatusWebsiteDown
	StatusClosed          = fetch.StatusClosed
	StatusTemporaryClosed = fetch.StatusTemporaryClosed
)

// ContentMeta holds article metadata extracted by trafilatura.
type ContentMeta struct {
	Title       string
	Author      string
	Description string
	Language    string
	SiteName    string
}

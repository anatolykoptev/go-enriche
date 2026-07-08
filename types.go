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

	// SkipMapsCheck is a verify-path lever — when true it suppresses the
	// ModePlaces maps closure-check (checkMapsStatus / mapsChecker.Check).
	// Suppressing it drops BOTH the closure status (StatusClosed /
	// StatusTemporaryClosed) AND every field mergeOrg writes at sourceMaps:
	// place name, address, phone, website, hours, and coordinates (resolve.go).
	// A SiteNumbers-membership verifier reads none of those (it consults only
	// the site's own candidate phone set), so a verify caller's VERDICT is
	// unaffected — but the enriched RESULT genuinely differs, which is why
	// SkipMapsCheck is part of the cache identity (cacheKey, enriche.go): a
	// skip=true blob must never be served to a skip=false caller.
	// wp_enrich (ModePlaces) leaves it false — the maps-check still runs there.
	// Default-false is byte-identical for every existing caller.
	SkipMapsCheck bool

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
	Email     string
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

	// SiteNumbers is the additive (Phase P2) SET of every distinct, valid
	// site-own phone number found across the official-site fetch (the
	// homepage plus any discovered /contacts subpage), each tagged
	// Anchored/DNI/Trustworthy by the SAME fail-closed gate that picks
	// Facts.Phone — see extract.PhoneNumberFact. pickPhoneCandidate collapses
	// the candidate set to ONE winner for Facts.Phone; SiteNumbers exposes
	// the rest, so a consumer can recognize a valid-but-different site number
	// instead of reading it as WRONG. Read-only sidecar: it never feeds
	// Facts.Phone. nil when the site fetch found no phone candidate at all.
	SiteNumbers []extract.PhoneNumberFact

	// RenderSkipped is true when the result rests on the RAW fetch ONLY, with NO
	// successful render corroboration — EITHER the escalation render was
	// intentionally SKIPPED because the raw fetch already carried a trustworthy
	// anchored site number (see rawContactsSufficient), OR a render was ATTEMPTED
	// but FAILED / returned a sub-minRenderShellBytes shell and degraded back to
	// raw. Both land on the same single-source-raw data, so the flag marks both.
	// Additive provenance sidecar, NOT yet wired to any reader (like SiteNumbers):
	// it is EMITTED for a go-wp Phase-1.5 Correctable gate (to be wired at go-wp
	// verify.go's classifyContactVerdict/Correctable) that will force a
	// wrong-verdict correction resting on a render-skipped single-source-raw value
	// to be NON-auto-applicable (human-confirm), so a false-negative skip cannot
	// auto-publish a laundered number to a paid card. This code does NOT yet
	// enforce that property. Zero value (false) = a successful render corroborated
	// the page (or none was avoidable).
	RenderSkipped bool
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
	// LegalAddress is the resolved provenance of a registered/legal-entity address
	// (юридический адрес) extracted from the official site. It is an additive
	// sidecar, separate from Address (the venue/geo slot that drives the card's map
	// link). Present only when the site supplied a legal address; consumers render
	// it as «Реквизиты», NEVER as the map slot.
	LegalAddress FieldProvenance `json:"legal_address,omitempty"`
	Phone        FieldProvenance `json:"phone,omitempty"`
	Website      FieldProvenance `json:"website,omitempty"`
	Hours        FieldProvenance `json:"hours,omitempty"`
	Email        FieldProvenance `json:"email,omitempty"`
	Price        FieldProvenance `json:"price,omitempty"`
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

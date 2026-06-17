package enriche

import (
	"github.com/anatolykoptev/go-enriche/extract"
	"github.com/anatolykoptev/go-enriche/fetch"
	"github.com/anatolykoptev/go-enriche/maps"
)

// fieldSource is the in-call provenance of a single extracted fact.
//
// This is INTERNAL to a single Enrich call — it is never written to the
// returned Facts (the public contract stays bare *string), never cached, and
// never crosses the module boundary. Its sole job is to let one authority
// (resolveFacts) decide, per field, which writer wins when the official site
// and the maps/aggregator/search sources disagree. Persisting source +
// confidence onto Facts is a separate ONE_WAY change (Phase 3), deliberately
// out of scope here.
type fieldSource int

// Source priority, highest wins. official_site outranks every aggregator/maps/
// search value: when the venue's own site yields a fact, that fact is
// authoritative and a lower-priority source may never overwrite it. A lower
// source still FILLS a field the site left empty (graceful degradation for the
// many RU SMBs that are VK/2GIS-only).
const (
	sourceNone         fieldSource = iota // absent — any source may fill
	sourceSearch                          // search snippet regex
	sourceMaps                            // 2GIS / Yandex org card
	sourceAggregator                      // (reserved) zoon/restoclub/afisha card
	sourceOfficialSite                    // the venue's own site (tel: href / JSON-LD / regex)
)

func (s fieldSource) String() string {
	switch s {
	case sourceOfficialSite:
		return "official_site"
	case sourceAggregator:
		return "aggregator"
	case sourceMaps:
		return "maps"
	case sourceSearch:
		return "search"
	default:
		return "none"
	}
}

// factProvenance records, for one in-flight Enrich call, which source last
// won each field. Zero value = all sourceNone (every field absent / fillable).
type factProvenance struct {
	placeName fieldSource
	address   fieldSource
	phone     fieldSource
	website   fieldSource
	hours     fieldSource
	price     fieldSource
}

// resolver is the SINGLE authority that merges a value into Facts under
// source-priority. It collapses what used to be two independent writers
// (mergeOrgDataToFacts for maps + the wholesale ExtractFactsForCity assign for
// the site) into one comparator so the official site deterministically wins.
type resolver struct {
	facts *Facts
	prov  *factProvenance
	m     *Metrics
}

// set merges one field value at the given source priority.
//
// Rule (encodes the operator's "on conflict, the official site wins"):
//   - higher-or-equal source than the field's current owner → take the value
//     (equal lets a later same-priority pass refresh, harmless);
//   - strictly lower source → fill ONLY if the field is still absent.
//
// Conflict telemetry fires whenever two DIFFERENT-priority sources offer a
// genuinely DIFFERENT value for the same field — regardless of merge order:
//   - higher source overrides a present, differing lower value (override case);
//   - lower source is REJECTED because a higher source already owns a differing
//     value (rejection case).
//
// This makes enrich_conflict_total{field} order-INDEPENDENT (the resolved value
// already is): site-then-maps and maps-then-site both count the same conflict.
func (r *resolver) set(dst **string, owner *fieldSource, val string, src fieldSource, field string) {
	if val == "" {
		return
	}
	// A real cross-source conflict: a value is present, the new value differs,
	// and the two sources are at different priorities (neither absent).
	if *dst != nil && **dst != val && src != *owner && *owner > sourceNone && src > sourceNone {
		r.m.conflict(field)
	}
	if src >= *owner {
		v := val
		*dst = &v
		*owner = src
		return
	}
	// Strictly lower source: fill the gap only (never overrides; the conflict,
	// if any, was already counted above).
	if *dst == nil {
		v := val
		*dst = &v
		*owner = src
	}
}

// mergeOrg merges maps OrgData under sourceMaps. Coordinates keep the existing
// "fill if absent" semantics (source coords from the item already won upstream
// via seedSourceCoords; maps only fills a still-empty pair).
func (r *resolver) mergeOrg(od *maps.OrgData) {
	r.set(&r.facts.PlaceName, &r.prov.placeName, od.Name, sourceMaps, "place_name")
	r.set(&r.facts.Address, &r.prov.address, od.Address, sourceMaps, "address")
	r.set(&r.facts.Phone, &r.prov.phone, od.Phone, sourceMaps, "phone")
	r.set(&r.facts.Website, &r.prov.website, od.Website, sourceMaps, "website")
	r.set(&r.facts.Hours, &r.prov.hours, od.Hours, sourceMaps, "hours")
	if od.Latitude != 0 && r.facts.Latitude == nil {
		r.facts.Latitude = &od.Latitude
		r.facts.Longitude = &od.Longitude
	}
}

// mergeSite merges the official-site extraction (extract.Facts) under
// sourceOfficialSite. Every present site field wins over any maps/search value;
// PlaceName from the site, if the site labelled it, also wins. Coordinates are
// intentionally NOT taken from the site here — ExtractFactsForCity never reads
// page geo and seedSourceCoords owns the coord authority.
func (r *resolver) mergeSite(sf extract.Facts) {
	deref := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	r.set(&r.facts.PlaceName, &r.prov.placeName, deref(sf.PlaceName), sourceOfficialSite, "place_name")
	r.set(&r.facts.Address, &r.prov.address, deref(sf.Address), sourceOfficialSite, "address")
	r.set(&r.facts.Phone, &r.prov.phone, deref(sf.Phone), sourceOfficialSite, "phone")
	r.set(&r.facts.Website, &r.prov.website, deref(sf.Website), sourceOfficialSite, "website")
	r.set(&r.facts.Hours, &r.prov.hours, deref(sf.Hours), sourceOfficialSite, "hours")
	r.set(&r.facts.Price, &r.prov.price, deref(sf.Price), sourceOfficialSite, "price")
	// PlaceType / EventDate are site-only fields (maps never provides them);
	// take them directly without a comparator.
	if sf.PlaceType != nil && r.facts.PlaceType == nil {
		r.facts.PlaceType = sf.PlaceType
	}
	if sf.EventDate != nil && r.facts.EventDate == nil {
		r.facts.EventDate = sf.EventDate
	}
}

// mergeSearchFacts folds facts already extracted from a search-DISCOVERED page
// (the no-primary-URL fallback) in at sourceSearch — the lowest priority. These
// pages are not the venue's own site, so their facts only fill still-absent
// fields and never override a maps or official-site value. Coordinates keep the
// fill-if-absent behaviour from the former mergeFacts.
func (r *resolver) mergeSearchFacts(sf extract.Facts) {
	deref := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	r.set(&r.facts.PlaceName, &r.prov.placeName, deref(sf.PlaceName), sourceSearch, "place_name")
	r.set(&r.facts.Address, &r.prov.address, deref(sf.Address), sourceSearch, "address")
	r.set(&r.facts.Phone, &r.prov.phone, deref(sf.Phone), sourceSearch, "phone")
	r.set(&r.facts.Website, &r.prov.website, deref(sf.Website), sourceSearch, "website")
	r.set(&r.facts.Hours, &r.prov.hours, deref(sf.Hours), sourceSearch, "hours")
	r.set(&r.facts.Price, &r.prov.price, deref(sf.Price), sourceSearch, "price")
	if sf.PlaceType != nil && r.facts.PlaceType == nil {
		r.facts.PlaceType = sf.PlaceType
	}
	if sf.EventDate != nil && r.facts.EventDate == nil {
		r.facts.EventDate = sf.EventDate
	}
	if sf.Latitude != nil && r.facts.Latitude == nil {
		r.facts.Latitude = sf.Latitude
		r.facts.Longitude = sf.Longitude
	}
}

// mergeSnippet merges search-snippet facts under sourceSearch (lowest). Only
// fills still-absent fields — never overrides site/maps. Uses the existing
// extract.ExtractSnippetFacts gates by snapshotting onto a scratch Facts then
// folding the result in at sourceSearch priority.
func (r *resolver) mergeSnippet(text string) {
	if text == "" {
		return
	}
	var scratch extract.Facts
	extract.ExtractSnippetFacts(text, &scratch)
	deref := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	r.set(&r.facts.Address, &r.prov.address, deref(scratch.Address), sourceSearch, "address")
	r.set(&r.facts.Phone, &r.prov.phone, deref(scratch.Phone), sourceSearch, "phone")
	r.set(&r.facts.Price, &r.prov.price, deref(scratch.Price), sourceSearch, "price")
}

// phoneSource returns the winning phone's source string for telemetry, or ""
// when no phone was resolved.
func (r *resolver) phoneSource() string {
	if r.facts.Phone == nil {
		return ""
	}
	return r.prov.phone.String()
}

// siteHasAnyFact reports whether the official-site extraction yielded at least
// one contact/business fact (used to fire enrich_site_resolved_total only when
// the site actually contributed something, not merely on a successful fetch).
func siteHasAnyFact(f extract.Facts) bool {
	return f.PlaceName != nil || f.PlaceType != nil || f.Address != nil ||
		f.Phone != nil || f.Price != nil || f.Website != nil ||
		f.Hours != nil || f.EventDate != nil
}

// siteRefutesClosed reports whether a reachable, active official-site fetch
// should override a maps-only "closed" status (false-closed class, e.g.
// Карт-Ленд flagged closed by a wrong Yandex card). A live site with its own
// contact facts is stronger evidence of an operating venue than a single maps
// card. Only refutes when the site is StatusActive — an unreachable/down site
// leaves the maps closed-status standing.
func siteRefutesClosed(siteStatus fetch.PageStatus) bool {
	return siteStatus == fetch.StatusActive
}

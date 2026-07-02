package enriche

import (
	"sort"

	"github.com/anatolykoptev/go-enriche/extract"
	"github.com/anatolykoptev/go-enriche/fetch"
	"github.com/anatolykoptev/go-enriche/maps"
)

// fieldSource is the provenance of a single extracted fact.
//
// Phase 2 kept this strictly in-call. Phase 3 (ONE_WAY) promotes the resolved
// per-field {source, confidence} onto the public Result.Provenance so the
// content path can PERSIST it and refuse to overwrite an operator-verified
// value on re-enrich. The value carrier (extract.Facts) stays bare *string;
// provenance rides alongside as a sidecar (additive, backward-compatible).
type fieldSource int

// Source priority, highest wins. operator_verified outranks everything: a value
// the operator hand-verified (e.g. the stable social-link phone shipped to a
// live article) must NEVER be downgraded or replaced by an enrich-derived
// value. Below it, official_site outranks every aggregator/maps/search value.
// A lower source still FILLS a field the higher one left empty (graceful
// degradation for the many RU SMBs that are VK/2GIS-only).
const (
	sourceNone             fieldSource = iota // absent — any source may fill
	sourceSearch                              // search snippet regex
	sourceMaps                                // 2GIS / Yandex org card
	sourceAggregator                          // (reserved) zoon/restoclub/afisha card
	sourceOfficialSite                        // the venue's own site (tel: href / JSON-LD / regex)
	sourcePoisonLock                          // DNI-poison "refuse" verdict — drops + locks the phone (see dropPhone)
	sourceOperatorVerified                    // operator hand-verified — top authority, never overwritten
)

// Source provenance strings — the on-the-wire values persisted in the cache and
// read by the content layer. Defined once so String/parse stay in sync.
const (
	srcStrOperatorVerified = "operator_verified"
	srcStrOfficialSite     = "official_site"
	srcStrAggregator       = "aggregator"
	srcStrMaps             = "maps"
	srcStrSearch           = "search"
	srcStrUnknown          = "unknown"
	srcStrPoisonLocked     = "poison_locked" // DNI omit — phone is nil, but this source still reaches Provenance.Phone (see snapshot)
)

func (s fieldSource) String() string {
	switch s {
	case sourceOperatorVerified:
		return srcStrOperatorVerified
	case sourcePoisonLock:
		// Internal "refuse" verdict for a DNI-poisoned phone. The field is dropped
		// to nil, but the verdict itself is a meaningful signal — snapshot treats
		// sourcePoisonLock as present so Result.Provenance.Phone.Source still
		// reaches consumers (see snapshot() and Facts.PhonePoisoned).
		return srcStrPoisonLocked
	case sourceOfficialSite:
		return srcStrOfficialSite
	case sourceAggregator:
		return srcStrAggregator
	case sourceMaps:
		return srcStrMaps
	case sourceSearch:
		return srcStrSearch
	default:
		return srcStrUnknown
	}
}

// confidence is the coarse, agent-facing trust bucket for a resolved fact.
// Three buckets on purpose — consumers branch on high/medium/low, not a float.
type confidence string

const (
	confHigh confidence = "high"
	confLow  confidence = "low"
	// confMedium ("medium") is reserved for the finer official_site gradation
	// (not-own-domain / regex-only / DNI-rotating) and is introduced in the
	// follow-up sub-phase that actually emits it.
)

// confidenceFor derives the agent-facing confidence from the winning source
// (ADR-006 table). operator_verified and official_site are high-trust; every
// aggregator/maps/search value is low (the agent should omit or mark
// «уточняйте»). This base mapping is the floor; a finer official_site
// gradation (medium for not-own-domain, low for regex-only / DNI-rotating) is a
// follow-up sub-phase (a medium bucket is reserved for it and not yet emitted).
func confidenceFor(src fieldSource) confidence {
	switch src {
	case sourceOperatorVerified, sourceOfficialSite:
		return confHigh
	case sourceAggregator, sourceMaps, sourceSearch:
		return confLow
	default:
		return confLow
	}
}

// fieldProv is the resolved provenance of one field: which source last won and
// the derived confidence bucket.
type fieldProv struct {
	source fieldSource
	conf   confidence
}

// factProvenance records, for one Enrich call, which source won each field and
// at what confidence. Zero value = all sourceNone (every field absent).
type factProvenance struct {
	placeName    fieldProv
	address      fieldProv
	legalAddress fieldProv
	phone        fieldProv
	website      fieldProv
	hours        fieldProv
	email        fieldProv
	price        fieldProv
}

// resolver is the SINGLE authority that merges a value into Facts under
// source-priority. It collapses what used to be two independent writers
// (mergeOrgDataToFacts for maps + the wholesale ExtractFactsForCity assign for
// the site) into one comparator so the official site deterministically wins,
// while operator_verified beats even the site.
type resolver struct {
	facts *Facts
	prov  *factProvenance
	m     *Metrics

	// siteNumbers is the additive (Phase P2) dedup-union of every candidate
	// site-own phone number found across the official-site fetch — the
	// homepage AND any discovered /contacts subpage. Populated via
	// addSiteNumbers at both mergeSite call sites (enriche_fetch.go,
	// enriche_contacts.go); exported read-only via siteNumbersSnapshot. It
	// never feeds Facts.Phone or pickPhoneCandidate — purely a read-only
	// sidecar for a consumer that wants the full SET, not the single winner.
	siteNumbers []extract.PhoneNumberFact
}

// set merges one field value at the given source priority.
//
// Rule (encodes "operator_verified > official_site > aggregator > maps >
// search", and the operator's "on conflict, the official site wins"):
//   - higher-or-equal source than the field's current owner → take the value
//     (equal lets a later same-priority pass refresh, harmless);
//   - strictly lower source → fill ONLY if the field is still absent;
//   - the winning source's confidence is derived and recorded alongside.
//
// Conflict telemetry fires whenever two DIFFERENT-priority sources offer a
// genuinely DIFFERENT value for the same field — order-INDEPENDENTLY.
func (r *resolver) set(dst **string, owner *fieldProv, val string, src fieldSource, field string) {
	if val == "" {
		return
	}
	// A real cross-source conflict: a value is present, the new value differs,
	// and the two sources are at different priorities (neither absent).
	if *dst != nil && **dst != val && src != owner.source && owner.source > sourceNone && src > sourceNone {
		r.m.conflict(field)
	}
	if src >= owner.source {
		v := val
		*dst = &v
		owner.source = src
		owner.conf = confidenceFor(src)
		return
	}
	// Strictly lower source: fill the gap only (never overrides; the conflict,
	// if any, was already counted above). A nil value whose owner is a HIGHER
	// source is a deliberate verdict, not a vacancy — e.g. a DNI-poison drop
	// (dropPhone locks the phone at sourcePoisonLock with a nil value). Such a
	// locked field must NOT be refilled by a lower maps/search source, otherwise
	// the rotating proxy this fix exists to omit would creep back in. Only fill
	// when the field is genuinely unclaimed (owner == sourceNone).
	if *dst == nil && owner.source == sourceNone {
		v := val
		*dst = &v
		owner.source = src
		owner.conf = confidenceFor(src)
	}
}

// setPreferLonger is set() with one refinement for fields where a more-complete
// value is strictly better (address): on an EQUAL-source overwrite it keeps the
// LONGER of the existing and incoming value. This stops a less-precise contacts-
// page address from clobbering a more-precise homepage address at equal
// official_site source ("Невский пр., 28, корп. 2, оф. 5" must survive a bare
// "Невский пр., 28"). A strictly HIGHER source still wins outright (higher trust
// beats length); a strictly lower source still only gap-fills. Used for address
// only — phone/email/hours/place_name keep the plain set() last-writer-wins.
func (r *resolver) setPreferLonger(dst **string, owner *fieldProv, val string, src fieldSource, field string) {
	if val == "" {
		return
	}
	if *dst != nil && **dst != val && src != owner.source && owner.source > sourceNone && src > sourceNone {
		r.m.conflict(field)
	}
	// Equal source, both present: keep the longer (more-complete) value. Length is
	// a cheap proxy for completeness for postal addresses (house/building/office
	// suffixes); ValidateAddress already gated both candidates upstream.
	if src == owner.source && *dst != nil && len([]rune(val)) <= len([]rune(**dst)) {
		return
	}
	if src >= owner.source {
		v := val
		*dst = &v
		owner.source = src
		owner.conf = confidenceFor(src)
		return
	}
	if *dst == nil && owner.source == sourceNone {
		v := val
		*dst = &v
		owner.source = src
		owner.conf = confidenceFor(src)
	}
}

// dropPhone records a DNI-poison "refuse" verdict for the phone: the official
// site carries a call-tracking vendor and no DNI-immune number, so every
// candidate is a rotating proxy. It drops any already-merged lower-priority
// phone (the maps/search card phone, which is itself often the same tracking
// number) and LOCKS the field at sourcePoisonLock so no later maps/search fill
// can resurrect it.
//
// A poison verdict refuses only the rotating proxy it stands in for; it must
// NOT erase a CLEAN phone that a higher-or-equal-trust source already resolved.
// Two such sources are protected:
//   - operator_verified — a hand-verified pin is sacrosanct on any site;
//   - official_site — a clean tel:/JSON-LD phone the HOMEPAGE already resolved
//     (e.g. a venue whose homepage carries a stable +7 (812) … but whose
//     /contacts subpage runs a DNI widget). The contacts page's DNI suppresses
//     only the contacts page's own (rotating) number, never the homepage's
//     stable one — dropping it here was a silent recall regression.
//
// The drive-igora case (homepage IS the DNI site, no clean phone anywhere) is
// unaffected: at the homepage merge the phone is still owned by maps/search/none
// (< official_site), so the drop+lock fires as before.
//
// Sets Facts.PhonePoisoned so the verdict propagates onto the public Result —
// without it, a poison-dropped phone (Phone=nil) is indistinguishable from a
// genuinely absent one, and consumers (e.g. wp_verify_contacts) can't tell
// "DNI/call-tracking, trust as scraped" from "no phone found".
func (r *resolver) dropPhone() {
	if r.prov.phone.source >= sourceOfficialSite {
		// An operator pin or an already-resolved clean official-site phone wins
		// over a (later) DNI verdict — leave it untouched, and leave PhonePoisoned
		// false: this call site did NOT actually drop anything.
		return
	}
	// Count a conflict when a real lower-priority phone is being refused, so the
	// telemetry reflects that the resolver actively overrode a maps/search value.
	if r.facts.Phone != nil {
		r.m.conflict("phone")
	}
	r.facts.Phone = nil
	r.facts.PhonePoisoned = true
	r.prov.phone.source = sourcePoisonLock
	r.prov.phone.conf = confLow
}

// seedOperatorValues injects operator-verified field values at the top
// priority BEFORE any fetch/maps/search runs, so no enrich-derived value can
// overwrite them. Empty values are skipped (no operator override for that
// field). This is the persistence-survival path: the content layer re-supplies
// a previously operator-verified phone/price on every re-enrich.
func (r *resolver) seedOperatorValues(s SeedFacts) {
	r.set(&r.facts.PlaceName, &r.prov.placeName, s.PlaceName, sourceOperatorVerified, "place_name")
	r.set(&r.facts.Address, &r.prov.address, s.Address, sourceOperatorVerified, "address")
	r.set(&r.facts.Phone, &r.prov.phone, s.Phone, sourceOperatorVerified, "phone")
	r.set(&r.facts.Website, &r.prov.website, s.Website, sourceOperatorVerified, "website")
	r.set(&r.facts.Hours, &r.prov.hours, s.Hours, sourceOperatorVerified, "hours")
	r.set(&r.facts.Email, &r.prov.email, s.Email, sourceOperatorVerified, "email")
	r.set(&r.facts.Price, &r.prov.price, s.Price, sourceOperatorVerified, "price")
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
// sourceOfficialSite. Every present site field wins over any maps/search value
// (but never over an operator_verified seed). Coordinates are intentionally
// NOT taken from the site here — ExtractFactsForCity never reads page geo and
// seedSourceCoords owns the coord authority.
func (r *resolver) mergeSite(sf extract.Facts) {
	r.set(&r.facts.PlaceName, &r.prov.placeName, derefStr(sf.PlaceName), sourceOfficialSite, "place_name")
	r.setPreferLonger(&r.facts.Address, &r.prov.address, derefStr(sf.Address), sourceOfficialSite, "address")
	// LegalAddress: a registered/legal-entity address extracted from the site is
	// routed to its OWN slot — it must NEVER occupy the venue Address slot (whose
	// authority the geo-correct maps/site venue address holds). When the site
	// supplies a legal address while a VENUE address already owns the Address slot
	// (typically the maps card's geo address), count an address_legal_vs_venue
	// conflict — this is the exact wrong-map-link signal that previously went
	// silent (the legal address would have overwritten the venue address). The
	// legal address is surfaced via Provenance for rendering as «Реквизиты», never
	// as the map slot.
	if la := derefStr(sf.LegalAddress); la != "" {
		if r.facts.Address != nil && r.prov.address.source > sourceNone && derefStr(r.facts.Address) != la {
			r.m.legalVsVenueAddress()
		}
		r.set(&r.facts.LegalAddress, &r.prov.legalAddress, la, sourceOfficialSite, "legal_address")
	}
	// Phone: a DNI-poison verdict from the site (PhonePoisoned) is a first-class
	// "refuse" that drops + locks the phone (outranking any maps/search value,
	// never an operator seed). Otherwise merge the site phone normally. The two
	// are mutually exclusive — a poisoned site always carries a nil Phone.
	if sf.PhonePoisoned {
		r.dropPhone()
	} else {
		r.set(&r.facts.Phone, &r.prov.phone, derefStr(sf.Phone), sourceOfficialSite, "phone")
	}
	r.set(&r.facts.Website, &r.prov.website, derefStr(sf.Website), sourceOfficialSite, "website")
	r.set(&r.facts.Hours, &r.prov.hours, derefStr(sf.Hours), sourceOfficialSite, "hours")
	r.set(&r.facts.Email, &r.prov.email, derefStr(sf.Email), sourceOfficialSite, "email")
	r.set(&r.facts.Price, &r.prov.price, derefStr(sf.Price), sourceOfficialSite, "price")
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
// fields and never override a maps/official-site/operator value. Coordinates
// keep the fill-if-absent behaviour from the former mergeFacts.
func (r *resolver) mergeSearchFacts(sf extract.Facts) {
	r.set(&r.facts.PlaceName, &r.prov.placeName, derefStr(sf.PlaceName), sourceSearch, "place_name")
	r.set(&r.facts.Address, &r.prov.address, derefStr(sf.Address), sourceSearch, "address")
	r.set(&r.facts.Phone, &r.prov.phone, derefStr(sf.Phone), sourceSearch, "phone")
	r.set(&r.facts.Website, &r.prov.website, derefStr(sf.Website), sourceSearch, "website")
	r.set(&r.facts.Hours, &r.prov.hours, derefStr(sf.Hours), sourceSearch, "hours")
	r.set(&r.facts.Email, &r.prov.email, derefStr(sf.Email), sourceSearch, "email")
	r.set(&r.facts.Price, &r.prov.price, derefStr(sf.Price), sourceSearch, "price")
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
// fills still-absent fields — never overrides site/maps/operator. Uses the
// existing extract.ExtractSnippetFacts gates by snapshotting onto a scratch
// Facts then folding the result in at sourceSearch priority.
func (r *resolver) mergeSnippet(text string) {
	if text == "" {
		return
	}
	var scratch extract.Facts
	extract.ExtractSnippetFacts(text, &scratch)
	r.set(&r.facts.Address, &r.prov.address, derefStr(scratch.Address), sourceSearch, "address")
	r.set(&r.facts.Phone, &r.prov.phone, derefStr(scratch.Phone), sourceSearch, "phone")
	r.set(&r.facts.Price, &r.prov.price, derefStr(scratch.Price), sourceSearch, "price")
}

// phoneSource returns the winning phone's source string for telemetry, or ""
// when no phone was resolved.
func (r *resolver) phoneSource() string {
	if r.facts.Phone == nil {
		return ""
	}
	return r.prov.phone.source.String()
}

// snapshot exports the resolved per-field provenance onto the public,
// persistable Provenance struct. Only fields that resolved to a value carry a
// non-empty source; an absent field stays zero-valued (omitempty on the wire).
//
// Phone is the one exception to "value present": a poison-locked phone is nil
// (dropPhone refused it) but sourcePoisonLock is itself the resolved verdict,
// not an absence — so it is treated as present here too, letting
// Result.Provenance.Phone.Source == "poison_locked" reach consumers alongside
// Facts.PhonePoisoned.
func (r *resolver) snapshot() Provenance {
	conv := func(p fieldProv, present bool) FieldProvenance {
		if !present || p.source == sourceNone {
			return FieldProvenance{}
		}
		return FieldProvenance{Source: p.source.String(), Confidence: string(p.conf)}
	}
	phonePresent := r.facts.Phone != nil || r.prov.phone.source == sourcePoisonLock
	return Provenance{
		PlaceName:    conv(r.prov.placeName, r.facts.PlaceName != nil),
		Address:      conv(r.prov.address, r.facts.Address != nil),
		LegalAddress: conv(r.prov.legalAddress, r.facts.LegalAddress != nil),
		Phone:        conv(r.prov.phone, phonePresent),
		Website:      conv(r.prov.website, r.facts.Website != nil),
		Hours:        conv(r.prov.hours, r.facts.Hours != nil),
		Email:        conv(r.prov.email, r.facts.Email != nil),
		Price:        conv(r.prov.price, r.facts.Price != nil),
	}
}

// addSiteNumbers unions candidate site phone numbers collected from ONE
// official-site page (the homepage or a discovered /contacts subpage) into
// the resolver's accumulated SiteNumbers set, deduping by normalized digits
// across repeated calls — the homepage and its /contacts subpage may print
// the very same stable number. On a duplicate, the more-trustworthy/anchored
// occurrence's classification wins: a weaker reading of the same digits (e.g.
// a call-tracking-demoted homepage slot) must not shadow a Trustworthy
// contacts-page reading of the identical number, and vice versa the strongest
// evidence found for a number must not be lost to whichever page happened to
// merge second.
func (r *resolver) addSiteNumbers(nums []extract.PhoneNumberFact) {
	if len(nums) == 0 {
		return
	}
	// extract.DedupeKeepStronger is the SAME keyed-dedupe-keep-strongest
	// mechanism CollectSiteNumbers (extract/sitenumbers.go) uses — merging
	// the new page's candidates into the existing accumulated set on every
	// call is the incremental form of the same "same key, keep the
	// strongest reading" rule, so the two no longer hand-roll independent
	// copies of it.
	r.siteNumbers = extract.DedupeKeepStronger(
		append(r.siteNumbers, nums...),
		func(n extract.PhoneNumberFact) string { return extract.DigitsOnly(n.Value) },
		siteNumberRank,
	)
}

// siteNumbersSnapshot returns the accumulated SiteNumbers set in a stable,
// deterministic order (highest trust/anchor rank first, then ascending
// digit-key) so a fixture yields byte-identical SiteNumbers across repeated
// runs, independent of which mergeSite call site (homepage / contacts page)
// produced each candidate. Returns nil when nothing was ever collected (the
// zero-value, matching Provenance's own zero-value-stays-empty convention).
func (r *resolver) siteNumbersSnapshot() []extract.PhoneNumberFact {
	if len(r.siteNumbers) == 0 {
		return nil
	}
	out := make([]extract.PhoneNumberFact, len(r.siteNumbers))
	copy(out, r.siteNumbers)
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := siteNumberRank(out[i]), siteNumberRank(out[j])
		if ri != rj {
			return ri > rj
		}
		return extract.DigitsOnly(out[i].Value) < extract.DigitsOnly(out[j].Value)
	})
	return out
}

// siteNumberRank is the dedup tiebreak / sort key for a PhoneNumberFact:
// Trustworthy outranks Anchored outranks neither. Deliberately coarser than
// extract's internal phoneCandidate tier (PhoneNumberFact does not carry it) —
// the resolver only needs "which reading is stronger", not the full ladder.
func siteNumberRank(n extract.PhoneNumberFact) int {
	rank := 0
	if n.Anchored {
		rank++
	}
	if n.Trustworthy {
		rank += 2
	}
	return rank
}

// derefStr returns the pointee or "" for a nil *string.
func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// siteHasAnyFact reports whether the official-site extraction yielded at least
// one contact/business fact (used to fire enrich_site_resolved_total only when
// the site actually contributed something, not merely on a successful fetch).
func siteHasAnyFact(f extract.Facts) bool {
	return f.PlaceName != nil || f.PlaceType != nil || f.Address != nil ||
		f.Phone != nil || f.Price != nil || f.Website != nil ||
		f.Hours != nil || f.Email != nil || f.EventDate != nil
}

// hasContactFacts reports whether the extracted facts carry at least one
// structured CONTACT field — phone, address, or hours. Used to decide whether
// a JS render is worth attempting on a text-rich page: if the raw HTML already
// yielded a contact fact, the contacts are NOT JS-gated and a render adds no
// value; if none are present, JS-injected contacts may be hiding and a render
// can surface them. PhonePoisoned counts as a phone signal (DNI was already
// detected from the raw HTML — a render would only show the rotating proxy, so
// it must NOT re-trigger render for that field).
func hasContactFacts(f extract.Facts) bool {
	return f.Phone != nil || f.PhonePoisoned || f.Address != nil ||
		f.Hours != nil || f.Email != nil
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

package extract

import "regexp"

// reLegalAddressMarker matches STRONG, unambiguous signals that an address string
// is a registered LEGAL/entity address (юридический адрес) rather than the venue's
// visiting (geo) address. Two signal families:
//
//   - explicit legal/entity-identity tokens: a «юридический адрес» label, an
//     ИНН/ОГРН/ОГРНИП/КПП tax-ID, or an ООО/ОАО/ЗАО/АО/ПАО/ИП company-form token
//     bundled into the address line (the registered-seat block on a /contacts
//     page routinely prints these next to the address);
//   - литера / лит. — a cadastral letter token that marks a registered building
//     unit and essentially never appears on a venue's customer-facing visiting
//     address. This is the drive-igora signature: «…дом № 38, литера А…».
//
// reOrgNoise (extract/hours_visible.go) already detects the company-form / tax-ID
// family for trimming hours noise; this pattern is the address-classification
// analogue and additionally covers литера/лит./«юридическ».
//
// Three deliberate scoping decisions, all in the SAFE direction. A false-demote of
// a real VENUE address out of the map slot is the worst outcome — it breaks a
// correct map link — so the classifier errs toward "venue":
//   - RE2 (Go's regexp) defines a word boundary only over ASCII word chars, so a
//     boundary adjacent to a Cyrillic token never matches. The Cyrillic tokens are
//     therefore matched as plain (case-insensitive) substrings; only the
//     ASCII-letter forms (АО, ИП) carry a \b, where it actually binds. «лит.» is
//     matched with its trailing period (not a \b, which would never bind before a
//     Cyrillic letter), which keeps it from firing inside «Литейный» (no period).
//   - помещение / пом. / оф. / офис are deliberately NOT signals: a real venue
//     legitimately occupies an office suite («…корпус 2, офис 5»), so keying on
//     them would false-demote a geo-correct venue address. The drive-igora legal
//     seat already classifies via ИНН + литера, so dropping them costs no recall.
//   - a bare 6-digit postal index is NOT a signal: many real venue addresses
//     print an index, so keying on it would false-demote them.
//
// Conservative by design: a plain street venue address («Невский проспект, 28»,
// even with an office suite or a postal index) matches NONE of these, so it is NOT
// mis-classified as legal — the negative-control invariant that keeps a real venue
// address in the venue slot, winning over maps.
var reLegalAddressMarker = regexp.MustCompile(
	`(?i)(?:юридическ|инн|огрн|огрнип|кпп|ооо|оао|зао|пао|\bао\b|\bип\b|литер|лит\.)`,
)

// isLegalAddress reports whether addr looks like a registered LEGAL/entity
// address (and therefore must NOT occupy the venue Address slot / the card's map
// link). See reLegalAddressMarker for the signal set. Conservative by design: it
// returns false for a plain venue address so the classifier never false-demotes
// a geo-correct venue address (the negative control).
func isLegalAddress(addr string) bool {
	return reLegalAddressMarker.MatchString(addr)
}

// setAddressFact routes a validated address candidate to the correct sidecar
// slot: a legal/entity address fills LegalAddress, a venue address fills Address.
// Each slot is fill-if-nil and independent, so a /contacts page that prints BOTH
// a legal seat and a venue address populates BOTH fields (whichever the cascade
// reaches first per slot). A legal candidate never lands in Address, so it can
// never overwrite the geo-correct venue address downstream. The caller is
// responsible for ValidateAddress gating (kept at the call sites so the existing
// validation order is unchanged).
func setAddressFact(facts *Facts, candidate string) {
	if candidate == "" {
		return
	}
	if isLegalAddress(candidate) {
		if facts.LegalAddress == nil {
			c := candidate
			facts.LegalAddress = &c
		}
		return
	}
	if facts.Address == nil {
		c := candidate
		facts.Address = &c
	}
}

// setAddressIfValid validates an *string address candidate (skipping nil/invalid)
// and routes it through setAddressFact. It mirrors the old setIfValid(...,
// ValidateAddress) call shape used by the structured-data apply paths, but splits
// the result across the venue / legal slots instead of always filling Address.
func setAddressIfValid(facts *Facts, src *string) {
	if src == nil || !ValidateAddress(*src) {
		return
	}
	setAddressFact(facts, *src)
}

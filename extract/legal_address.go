package extract

import "regexp"

// reLegalAddressMarker matches STRONG, explicit signals that an address STRING is
// a registered LEGAL/entity address (юридический адрес) rather than the venue's
// visiting (geo) address. Signal families, all unambiguous:
//
//   - an explicit legal-section label: «юридический адрес» / «реквизиты»;
//   - a tax / registration ID printed in the address line: ИНН / ОГРН / ОГРНИП /
//     КПП — these identify a legal entity, never a place to visit;
//   - a company-form token bundled into the line: ООО / ОАО / ЗАО / ПАО / АО / ИП.
//
// These are the markers a /contacts requisites block prints next to the
// registered seat (e.g. «ООО «Игора Драйв», ИНН 7801321150»). reOrgNoise
// (extract/hours_visible.go) detects the same company-form / tax-ID family for
// trimming hours noise; this pattern is the address-classification analogue.
//
// DELIBERATELY NOT signals (each appears in REAL venue visiting addresses — keying
// on them false-demotes a geo-correct venue out of the map slot, the worst
// outcome, so the classifier errs toward "venue"):
//
//   - литера / лит. — a cadastral letter token. «литера А» is ubiquitous in normal
//     SPb visiting addresses (ул. Маршала Жукова, 28, литера А), and the substring
//     «литер» even matches the STREET NAME «улица Литераторов». Matching it caused
//     the false-demote regression. Игора's legal seat is caught instead by
//     PROVENANCE (it is the streetAddress of a schema.org/Organization block — see
//     applyOrgFacts), not by литера-in-string, so dropping it costs no recall.
//   - помещение / пом. / оф. / офис — a real venue legitimately occupies an office
//     suite («…корпус 2, офис 5»).
//   - a bare postal index — many real venue addresses print one.
//
// Cyrillic-boundary note: RE2 (Go's regexp) defines \b only over ASCII word chars,
// so an ASCII \b adjacent to a Cyrillic token NEVER matches — a prior `\bао\b` /
// `\bип\b` was dead and never classified an ИП/АО legal-prefix address. The
// company-form tokens are therefore bound with an explicit Cyrillic-safe boundary
// (start-of-string or a separator on each side) so «ИП Иванов, ул. Садовая, 5»
// and «…, ООО «Ромашка»» classify while «Литейный» / «Заозёрная» / ordinary words
// embedding the token's letters do not spuriously match.
var reLegalAddressMarker = regexp.MustCompile(
	`(?i)(?:` +
		`юридическ|реквизит|инн|огрн|огрнип|кпп` + // labels + tax / registration IDs
		`|(?:^|[\s,.;«])(?:ооо|оао|зао|пао|ао|ип)(?:[\s,.;»]|$)` + // company forms, Cyrillic-safe boundary
		`)`,
)

// isLegalAddress reports whether an address STRING carries a strong explicit
// legal/entity marker (юридический-адрес/реквизиты label, ИНН/ОГРН/ОГРНИП/КПП tax
// ID, or an ООО/ОАО/ЗАО/ПАО/АО/ИП company form). See reLegalAddressMarker for the
// exact signal set and what is deliberately excluded. This is the STRING-based arm
// of classification, used for Place / <address> / regex-sourced candidates; an
// Organization-sourced address is classified LEGAL by PROVENANCE regardless of its
// string (see setOrgAddressFact). Conservative by design: returns false for a
// plain venue address (incl. one with литера / помещение / a postal index) so the
// classifier never false-demotes a geo-correct venue address (the negative control
// the reviewer flagged).
func isLegalAddress(addr string) bool {
	return reLegalAddressMarker.MatchString(addr)
}

// setAddressFact routes a validated address candidate to the correct sidecar slot
// using STRING-based classification: an address carrying a strong legal marker
// (isLegalAddress) fills LegalAddress, otherwise it fills the venue Address slot.
// Used for candidates whose provenance does NOT itself imply legal vs venue —
// schema.org/Place, a bare <address> element, and the regex fallbacks. (An
// Organization-sourced address bypasses the string test and routes through
// setOrgAddressFact, which is legal by provenance.)
//
// Each slot is fill-if-nil and independent, so a /contacts page that prints BOTH a
// legal seat and a venue address populates BOTH fields. A legal candidate never
// lands in Address, so it can never overwrite the geo-correct venue address
// downstream. The caller is responsible for ValidateAddress gating (kept at the
// call sites so the existing validation order is unchanged).
func setAddressFact(facts *Facts, candidate string) {
	if candidate == "" {
		return
	}
	if isLegalAddress(candidate) {
		fillLegalAddress(facts, candidate)
		return
	}
	fillVenueAddress(facts, candidate)
}

// setOrgAddressFact fills LegalAddress (fill-if-nil) with a schema.org/Organization
// streetAddress that the caller has ALREADY determined to be the registered/legal
// seat. It is the PROVENANCE arm of classification, but it is NOT unconditional:
// applyOrgFacts routes an Organization address here ONLY when orgAddressIsLegal is
// satisfied (a corroborating legal signal — string marker, in-item ИНН/ОГРН via
// org.HasLegalID, legalName, or a distinct venue address present elsewhere). When
// NO corroborant holds, applyOrgFacts instead routes the address through the STRING
// arm (setAddressFact), so a markerless lone Organization address whose
// streetAddress is in fact the venue's visiting address STAYS in the venue slot —
// it is never demoted to LegalAddress merely because the itemtype is Organization
// (the false-demote guard). When a corroborant DOES hold, this catches a legal seat
// like Игора's («…дом № 38, литера А, помещение 91…») that carries no ИНН/ОГРН in
// the address string itself and so is invisible to the string-based isLegalAddress.
func setOrgAddressFact(facts *Facts, candidate string) {
	if candidate == "" {
		return
	}
	fillLegalAddress(facts, candidate)
}

// fillLegalAddress fills the LegalAddress slot fill-if-nil.
func fillLegalAddress(facts *Facts, candidate string) {
	if facts.LegalAddress == nil {
		c := candidate
		facts.LegalAddress = &c
	}
}

// fillVenueAddress fills the venue Address slot fill-if-nil.
func fillVenueAddress(facts *Facts, candidate string) {
	if facts.Address == nil {
		c := candidate
		facts.Address = &c
	}
}

// setAddressIfValid validates an *string address candidate (skipping nil/invalid)
// and routes it through setAddressFact (string-based classification). It mirrors
// the old setIfValid(..., ValidateAddress) call shape used by the schema.org/Place
// and Event apply paths, but splits the result across the venue / legal slots.
func setAddressIfValid(facts *Facts, src *string) {
	if src == nil || !ValidateAddress(*src) {
		return
	}
	setAddressFact(facts, *src)
}

// setOrgAddressIfValid validates an *string Organization address candidate and
// routes it through setOrgAddressFact (provenance-based: always legal). Used by
// applyOrgFacts so a schema.org/Organization seat never occupies the venue slot.
func setOrgAddressIfValid(facts *Facts, src *string) {
	if src == nil || !ValidateAddress(*src) {
		return
	}
	setOrgAddressFact(facts, *src)
}

package extract

import "strings"

// cityAreaCodes maps a normalized city key to the set of telephone area codes
// (the 3-digit operator/area code after the country prefix) that are "local"
// to that city. It powers the local-area-code tiebreaker (operator Decision 2,
// 2026-06-17): among several official-site phone candidates, the one whose area
// code is local to the article's target city wins.
//
// Scope is deliberately minimal for Phase 1 — only the cities the content
// pipeline actually targets. Saint Petersburg (piter.now) = 812. Add an entry
// here (not a new mechanism) when a new city guide ships. The 3-digit-code
// model intentionally does NOT cover Leningrad-oblast 813xx 5-digit prefixes;
// oblast geo-filtering is handled separately upstream (go-wp spbAreaWhitelist).
var cityAreaCodes = map[string][]int{
	"санкт-петербург":  {812},
	"петербург":        {812},
	"спб":              {812},
	"spb":              {812},
	"saint petersburg": {812},
	"st petersburg":    {812},
	"st. petersburg":   {812},
	"москва":           {495, 499},
	"moscow":           {495, 499},
	"мск":              {495, 499},
	"msk":              {495, 499},
}

// expectedAreaCodes returns the local area codes for a city, or nil when the
// city is empty or unknown (in which case the tiebreaker does not fire and the
// caller falls back to source-order resolution). Matching is normalized:
// lower-cased, trimmed, with surrounding "г."/"city" noise removed.
func expectedAreaCodes(city string) []int {
	key := normalizeCityKey(city)
	if key == "" {
		return nil
	}
	if codes, ok := cityAreaCodes[key]; ok {
		return codes
	}
	return nil
}

// ExpectedAreaCodes is the exported form of expectedAreaCodes, for callers
// outside this package (the enriche resolver's SiteNumbers city-membership
// tagging, resolve.go addSiteNumbers) that need the same city→area-codes
// lookup without duplicating the numbering-plan map.
func ExpectedAreaCodes(city string) []int {
	return expectedAreaCodes(city)
}

// normalizeCityKey lowercases, trims, and strips a leading "г." / "г "
// prefix so "г. Санкт-Петербург" matches "санкт-петербург".
func normalizeCityKey(city string) string {
	key := strings.ToLower(strings.TrimSpace(city))
	key = strings.TrimSpace(strings.TrimPrefix(key, "г."))
	key = strings.TrimSpace(strings.TrimPrefix(key, "г "))
	return key
}

// phoneAreaCode extracts the 3-digit area/operator code from a Russian phone
// string (the 3 digits after the leading 7/8 country prefix), or -1 when the
// string is not a well-formed 11-digit RU number. It mirrors ValidatePhone's
// digit parsing so a candidate that ValidatePhone accepts always yields a code.
func phoneAreaCode(phone string) int {
	digits := reDigitsOnly.ReplaceAllString(phone, "")
	if len(digits) != 11 || (digits[0] != '8' && digits[0] != '7') {
		return -1
	}
	code := 0
	for _, c := range digits[1:4] {
		code = code*10 + int(c-'0')
	}
	return code
}

// areaCodeMatches reports whether a precomputed 3-digit area code is local to
// the expected-area-code set.
func areaCodeMatches(code int, expected []int) bool {
	if code < 0 || len(expected) == 0 {
		return false
	}
	for _, want := range expected {
		if code == want {
			return true
		}
	}
	return false
}

// matchesCity reports whether a phone string's area code is local to the given
// expected-area-code set. Convenience wrapper over areaCodeMatches for callers
// that have the string but not the precomputed code.
func matchesCity(phone string, expected []int) bool {
	return areaCodeMatches(phoneAreaCode(phone), expected)
}

// mobileCodeLow/mobileCodeHigh bound the RU mobile-operator area-code range
// (900-999 — every 3-digit code whose first digit is 9). tollFreeCode is the
// standard 8-800 toll-free/call-tracking prefix. Both are excluded from
// "geographic landline" by isRUGeographicLandline: neither identifies a fixed
// city, so neither can be city-matched OR city-foreign — see ClassifyCityMembership.
const (
	mobileCodeLow  = 900
	mobileCodeHigh = 999
	tollFreeCode   = 800
)

// isRUGeographicLandline reports whether phone is a well-formed RU number
// (per phoneAreaCode's 11-digit 7/8-prefixed parse) whose area code
// identifies a fixed geographic location — i.e. NOT a mobile-operator code
// (900-999), NOT the 8-800 toll-free/call-tracking prefix, and NOT an
// unparseable/non-RU string. This is the SEED-INDEPENDENT half of city
// membership: it recognizes "this is some city's landline" WITHOUT the
// city needing an entry in cityAreaCodes, so an un-seeded city's number
// (Новосибирск 383, Ростов 863) still reads as geographic — see
// ClassifyCityMembership's cityForeign, which uses this to exclude a
// wrong-city landline from an authoritative pick even when that city was
// never explicitly seeded.
func isRUGeographicLandline(phone string) bool {
	code := phoneAreaCode(phone)
	if code < 0 {
		return false
	}
	if code >= mobileCodeLow && code <= mobileCodeHigh {
		return false
	}
	if code == tollFreeCode {
		return false
	}
	return true
}

// ClassifyCityMembership tags a single phone candidate against the project's
// city area codes (cityCodes — typically ExpectedAreaCodes(item.City)),
// returning:
//
//   - cityMatch: the number's area code is one of cityCodes — a confirmed
//     LOCAL number for the project's city.
//   - cityForeign: the number is an RU geographic landline (per
//     isRUGeographicLandline) whose area code is NOT one of cityCodes — a
//     confirmed OTHER-city number that must never be treated as this
//     project's authoritative contact, even for a city that was never
//     seeded in cityAreaCodes (seed-independence).
//
// A mobile number, an 8-800 toll-free number, a non-RU/unparseable string,
// or any number when cityCodes is empty (the project's city is unknown/
// unset) classifies as NEITHER — cityMatch=false AND cityForeign=false
// ("neutral"). The empty-cityCodes case is deliberate: a project with no
// configured city (e.g. hully, non-RU) must tag every candidate neutral, so
// its SiteNumbers output stays byte-identical to the pre-city-membership
// behavior.
func ClassifyCityMembership(phone string, cityCodes []int) (cityMatch, cityForeign bool) {
	if len(cityCodes) == 0 {
		return false, false
	}
	cityMatch = matchesCity(phone, cityCodes)
	cityForeign = isRUGeographicLandline(phone) && !cityMatch
	return cityMatch, cityForeign
}

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

// matchesCity reports whether a phone's area code is local to the given
// expected-area-code set.
func matchesCity(phone string, expected []int) bool {
	if len(expected) == 0 {
		return false
	}
	code := phoneAreaCode(phone)
	if code < 0 {
		return false
	}
	for _, want := range expected {
		if code == want {
			return true
		}
	}
	return false
}

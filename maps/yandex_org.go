package maps

import (
	"regexp"
	"strconv"
	"strings"
)

// Regex patterns for Yandex Maps rendered org page JSON blobs.
// Yandex embeds business data as JSON in script tags and page state.
//
// 2026-07-15: Yandex Maps changed its org page JSON structure. The old
// patterns matched the first occurrence of generic field names like "name",
// which on the new pages hits the JSON-LD WebSite schema ("Yandex Maps")
// instead of the business name. The new patterns target the business-data
// block's specific field names (shortTitle, fullAddress, currentWorkingStatus,
// ratingData, categories) and fall back to the old patterns for pages that
// still use the legacy format.
var (
	// Name: prefer shortTitle (business-data block), fall back to chain.name,
	// then the legacy "name" pattern (old pages had the business name as the
	// first "name" field).
	reOrgShortTitle = regexp.MustCompile(`"shortTitle"\s*:\s*"([^"]{2,100})"`)
	reOrgChainName  = regexp.MustCompile(`"chain"\s*:\s*\{[^}]*?"name"\s*:\s*"([^"]{2,100})"`)
	reOrgName       = regexp.MustCompile(`"name"\s*:\s*"([^"]{2,100})"`)

	// Address: prefer fullAddress (top-level string), fall back to legacy
	// address.formatted.
	reOrgFullAddress = regexp.MustCompile(`"fullAddress"\s*:\s*"([^"]+)"`)
	reOrgAddress     = regexp.MustCompile(`"address"\s*:\s*\{[^}]*?"formatted"\s*:\s*"([^"]+)"`)

	// Phone: prefer "number" field (new format), fall back to "formatted" (old).
	reOrgPhoneNew = regexp.MustCompile(`"phones"\s*:\s*\[[^\]]*?"number"\s*:\s*"([^"]+)"`)
	reOrgPhone    = regexp.MustCompile(`"phones"\s*:\s*\[[^\]]*?"formatted"\s*:\s*"([^"]+)"`)

	// Hours: prefer currentWorkingStatus.text (human-readable, new format),
	// fall back to legacy hours.text.
	reOrgWorkingStatus = regexp.MustCompile(`"currentWorkingStatus"\s*:\s*\{[^}]*?"text"\s*:\s*"([^"]+)"`)
	reOrgHours         = regexp.MustCompile(`"hours"\s*:\s*\{[^}]*?"text"\s*:\s*"([^"]+)"`)

	// Rating: prefer ratingData.ratingValue (new), fall back to rating.score (old).
	reOrgRatingNew = regexp.MustCompile(`"ratingData"\s*:\s*\{[^}]*?"ratingValue"\s*:\s*([\d.]+)`)
	reOrgRating    = regexp.MustCompile(`"rating"\s*:\s*\{[^}]*?"score"\s*:\s*([\d.]+)`)

	reOrgCoords = regexp.MustCompile(`"coordinates"\s*:\s*\[([\d.]+)\s*,\s*([\d.]+)\]`)

	// URL: new format is a plain string array ["https://..."], old format was
	// [{"value":"https://..."}]. Try new first, fall back to old.
	reOrgURLNew = regexp.MustCompile(`"urls"\s*:\s*\[\s*"(https?://[^"]+)"`)
	reOrgURL    = regexp.MustCompile(`"urls"\s*:\s*\[[^\]]*?"value"\s*:\s*"(https?://[^"]+)"`)

	// Categories: new format uses "categories" array with "name" fields.
	// Old format used "rubrics" array with "name" fields.
	reOrgCategories = regexp.MustCompile(`"categories"\s*:\s*(\[[^\]]+\])`)
	reOrgRubrics    = regexp.MustCompile(`"rubrics"\s*:\s*(\[[^\]]+\])`)
	reRubricName    = regexp.MustCompile(`"name"\s*:\s*"([^"]+)"`)
)

// parseOrgPage extracts all business data from browser-rendered Yandex Maps HTML.
func parseOrgPage(html []byte) *OrgData {
	od := &OrgData{}

	// Status (reuse existing pattern).
	switch parseOrgStatus(html) {
	case yandexStatusPermanentClosed:
		od.Status = PlacePermanentClosed
	case yandexStatusTemporaryClosed:
		od.Status = PlaceTemporaryClosed
	case yandexStatusOpen:
		od.Status = PlaceOpen
	}

	// Name: shortTitle → chain.name → legacy "name".
	od.Name = extractFirstMatch(html, reOrgShortTitle, reOrgChainName, reOrgName)

	// Address: fullAddress → legacy address.formatted.
	od.Address = extractFirstMatch(html, reOrgFullAddress, reOrgAddress)

	// Phone: "number" field → legacy "formatted".
	od.Phone = extractFirstMatch(html, reOrgPhoneNew, reOrgPhone)

	// Hours: currentWorkingStatus.text → legacy hours.text.
	od.Hours = extractFirstMatch(html, reOrgWorkingStatus, reOrgHours)

	// Website: plain string array → legacy {"value":"..."}.
	od.Website = extractFirstMatch(html, reOrgURLNew, reOrgURL)

	// Rating: ratingData.ratingValue → legacy rating.score.
	if s := extractFirstMatch(html, reOrgRatingNew, reOrgRating); s != "" {
		if v, err := strconv.ParseFloat(s, 64); err == nil {
			od.Rating = v
		}
	}

	od.Latitude, od.Longitude = extractCoords(reOrgCoords, html)
	od.Categories = extractAllRubrics(html)

	return od
}

// extractFirstMatch tries each regex in order and returns the first match.
// Used for new-format → legacy-format fallback chains.
func extractFirstMatch(data []byte, patterns ...*regexp.Regexp) string {
	for _, re := range patterns {
		if s := extractFirst(re, data); s != "" {
			return s
		}
	}
	return ""
}

// extractFirst returns the first capture group match, or "".
func extractFirst(re *regexp.Regexp, data []byte) string {
	sub := re.FindSubmatch(data)
	if len(sub) < 2 { //nolint:mnd
		return ""
	}
	return unescapeJSON(string(sub[1]))
}

// extractCoords returns lat/lon from the first coordinates match.
func extractCoords(re *regexp.Regexp, data []byte) (float64, float64) {
	sub := re.FindSubmatch(data)
	if len(sub) < 3 { //nolint:mnd
		return 0, 0
	}
	lat, err1 := strconv.ParseFloat(string(sub[1]), 64)
	lon, err2 := strconv.ParseFloat(string(sub[2]), 64)
	if err1 != nil || err2 != nil {
		return 0, 0
	}
	return lat, lon
}

// extractAllRubrics extracts all rubric/category names from the page.
// New format uses "categories" array with "name" fields; old format used
// "rubrics" array with "name" fields. Tries categories first, falls back
// to rubrics.
func extractAllRubrics(data []byte) []string {
	// Try new "categories" format first.
	if arrMatch := reOrgCategories.FindSubmatch(data); len(arrMatch) >= 2 {
		if cats := extractRubricNames(arrMatch[1]); len(cats) > 0 {
			return cats
		}
	}
	// Fall back to legacy "rubrics" format.
	arrMatch := reOrgRubrics.FindSubmatch(data)
	if len(arrMatch) < 2 { //nolint:mnd
		return nil
	}
	return extractRubricNames(arrMatch[1])
}

// extractRubricNames finds all "name" values within a JSON array fragment.
func extractRubricNames(jsonArray []byte) []string {
	names := reRubricName.FindAllSubmatch(jsonArray, -1)
	if len(names) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(names))
	var cats []string
	for _, m := range names {
		if len(m) < 2 { //nolint:mnd
			continue
		}
		name := unescapeJSON(string(m[1]))
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		cats = append(cats, name)
	}
	return cats
}

// unescapeJSON handles basic JSON string escapes (\", \\, \/, \n).
func unescapeJSON(s string) string {
	s = strings.ReplaceAll(s, `\"`, `"`)
	s = strings.ReplaceAll(s, `\\`, `\`)
	s = strings.ReplaceAll(s, `\/`, `/`)
	s = strings.ReplaceAll(s, `\n`, "\n")
	return strings.TrimSpace(s)
}

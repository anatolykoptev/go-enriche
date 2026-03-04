package maps

import (
	"regexp"
	"strconv"
	"strings"
)

// Regex patterns for Yandex Maps rendered org page JSON blobs.
// Yandex embeds business data as JSON in script tags and page state.
var (
	reOrgName    = regexp.MustCompile(`"name"\s*:\s*"([^"]{2,100})"`)
	reOrgAddress = regexp.MustCompile(`"address"\s*:\s*\{[^}]*?"formatted"\s*:\s*"([^"]+)"`)
	reOrgPhone   = regexp.MustCompile(`"phones"\s*:\s*\[[^\]]*?"formatted"\s*:\s*"([^"]+)"`)
	reOrgHours   = regexp.MustCompile(`"hours"\s*:\s*\{[^}]*?"text"\s*:\s*"([^"]+)"`)
	reOrgRating  = regexp.MustCompile(`"rating"\s*:\s*\{[^}]*?"score"\s*:\s*([\d.]+)`)
	reOrgCoords  = regexp.MustCompile(`"coordinates"\s*:\s*\[([\d.]+)\s*,\s*([\d.]+)\]`)
	reOrgURL     = regexp.MustCompile(`"urls"\s*:\s*\[[^\]]*?"value"\s*:\s*"(https?://[^"]+)"`)
	reOrgRubrics = regexp.MustCompile(`"rubrics"\s*:\s*(\[[^\]]+\])`)
	reRubricName = regexp.MustCompile(`"name"\s*:\s*"([^"]+)"`)
)

// parseOrgPage extracts all business data from browser-rendered Yandex Maps HTML.
func parseOrgPage(html []byte) *OrgData {
	od := &OrgData{}

	// Status (reuse existing pattern).
	switch parseOrgStatus(html) {
	case "permanent-closed":
		od.Status = PlacePermanentClosed
	case "temporary-closed":
		od.Status = PlaceTemporaryClosed
	case "open":
		od.Status = PlaceOpen
	}

	od.Name = extractFirst(reOrgName, html)
	od.Address = extractFirst(reOrgAddress, html)
	od.Phone = extractFirst(reOrgPhone, html)
	od.Hours = extractFirst(reOrgHours, html)
	od.Website = extractFirst(reOrgURL, html)

	if s := extractFirst(reOrgRating, html); s != "" {
		if v, err := strconv.ParseFloat(s, 64); err == nil {
			od.Rating = v
		}
	}

	od.Latitude, od.Longitude = extractCoords(reOrgCoords, html)
	od.Categories = extractAllRubrics(html)

	return od
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
// First finds the "rubrics" array, then extracts all "name" values within it.
func extractAllRubrics(data []byte) []string {
	arrMatch := reOrgRubrics.FindSubmatch(data)
	if len(arrMatch) < 2 { //nolint:mnd
		return nil
	}
	names := reRubricName.FindAllSubmatch(arrMatch[1], -1)
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

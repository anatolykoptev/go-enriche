package maps

import (
	"regexp"
	"strings"
)

// isYandexMapsOrgURL checks if a URL points to a Yandex Maps org page.
func isYandexMapsOrgURL(u string) bool {
	return strings.Contains(u, "yandex.ru/maps/org") ||
		strings.Contains(u, "yandex.com/maps/org")
}

// statusRe matches "status":"<value>" in embedded JSON on Yandex Maps org pages.
// Yandex Maps uses these status values:
//   - "open"             — currently operating
//   - "permanent-closed" — permanently shut down
//   - "temporary-closed" — temporarily not operating
//   - "closed"           — currently outside working hours
var statusRe = regexp.MustCompile(`"status"\s*:\s*"(permanent-closed|temporary-closed|open)"`)

// parseOrgStatus extracts the business status from Yandex Maps org page HTML.
// Returns "permanent-closed", "temporary-closed", "open", or "" if not found.
func parseOrgStatus(html []byte) string {
	match := statusRe.Find(html)
	if match == nil {
		return ""
	}
	sub := statusRe.FindSubmatch(html)
	if len(sub) < 2 { //nolint:mnd
		return ""
	}
	return string(sub[1])
}

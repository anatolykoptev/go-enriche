package maps

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	stealth "github.com/anatolykoptev/go-stealth"
)

const yandexSearchURL = "https://yandex.ru/search/"

// Closure signal strings to detect in Yandex search result titles/snippets.
var (
	permanentClosedSignals = []string{
		"Закрыто навсегда",
		"закрыто навсегда",
		"Permanently closed",
		"permanently closed",
	}
	temporaryClosedSignals = []string{
		"Временно закрыто",
		"временно закрыто",
		"Temporarily closed",
		"temporarily closed",
	}
)

// buildQuery constructs a Yandex search query targeting Maps org pages.
func buildQuery(name, city string) string {
	q := fmt.Sprintf(`site:yandex.ru/maps/org "%s"`, name)
	if city != "" {
		q += " " + city
	}
	return q
}

// fetchSearchPage fetches a Yandex search results page via go-stealth.
func (y *YandexMaps) fetchSearchPage(query string) ([]byte, error) {
	params := url.Values{}
	params.Set("text", query)
	params.Set("lr", "2") // Saint Petersburg region

	reqURL := yandexSearchURL + "?" + params.Encode()

	headers := stealth.ChromeHeaders()
	headers["referer"] = "https://yandex.ru/"
	headers["accept-language"] = "ru-RU,ru;q=0.9"

	data, _, status, err := y.bc.Do(http.MethodGet, reqURL, headers, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", status)
	}
	return data, nil
}

// parseResults parses Yandex search result HTML looking for Maps org listings
// with closure indicators in their title or snippet.
func parseResults(data []byte, targetName string) *CheckResult {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(data))
	if err != nil {
		return &CheckResult{Status: PlaceUnknown}
	}

	result := &CheckResult{Status: PlaceNotFound}
	nameLower := strings.ToLower(targetName)

	// Scan search result items for yandex.ru/maps/org links.
	doc.Find("li.serp-item, .serp-item").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		href := extractMapsOrgLink(s)
		if href == "" {
			return true // continue
		}

		title := strings.TrimSpace(s.Find("h2, .OrganicTitle-LinkText").Text())
		snippet := strings.TrimSpace(s.Find(".OrganicText, .text-container, .Organic-ContentWrapper").Text())
		combined := title + " " + snippet

		// Only consider results matching our target place name.
		if !strings.Contains(strings.ToLower(combined), nameLower) {
			return true
		}

		result.MapURL = href
		result.RawTitle = title

		if matchesAny(combined, permanentClosedSignals) {
			result.Status = PlacePermanentClosed
			return false // stop
		}
		if matchesAny(combined, temporaryClosedSignals) {
			result.Status = PlaceTemporaryClosed
			return false
		}

		// Found Maps org listing without closure signal → open.
		result.Status = PlaceOpen
		return false
	})

	return result
}

// extractMapsOrgLink finds a yandex.ru/maps/org link in a search result item.
func extractMapsOrgLink(s *goquery.Selection) string {
	var found string
	s.Find("a").EachWithBreak(func(_ int, a *goquery.Selection) bool {
		href, exists := a.Attr("href")
		if !exists {
			return true
		}
		if strings.Contains(href, "yandex.ru/maps/org") || strings.Contains(href, "yandex.com/maps/org") {
			found = href
			return false
		}
		return true
	})
	return found
}

// matchesAny returns true if text contains any of the signals.
func matchesAny(text string, signals []string) bool {
	for _, sig := range signals {
		if strings.Contains(text, sig) {
			return true
		}
	}
	return false
}

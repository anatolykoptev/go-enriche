// Package maps provides place status verification via map services.
//
// TwoGIS checks place existence via 2GIS Catalog API.
// If a place is found, it is considered open (2GIS removes closed places).
// If not found, the caller should fall back to Yandex Maps for status check.
package maps

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
)

const (
	twoGISBaseURL = "https://catalog.api.2gis.com/3.0/items"
	twoGISTimeout = 5 * time.Second
	twoGISDemoKey = "demo"
)

// TwoGISConfig configures the 2GIS checker.
type TwoGISConfig struct {
	APIKey string // default: "demo"
}

// twoGISResponse is the JSON response from 2GIS Catalog API.
type twoGISResponse struct {
	Meta struct {
		Code int `json:"code"`
	} `json:"meta"`
	Result struct {
		Items []twoGISItem `json:"items"`
		Total int          `json:"total"`
	} `json:"result"`
}

type twoGISItem struct {
	Name        string            `json:"name"`
	AddressName string            `json:"address_name"`
	Schedule    *twoGISSchedule   `json:"schedule"`
	Contacts    []twoGISContactGr `json:"contact_groups"`
	Point       *twoGISPoint      `json:"point"`
}

type twoGISSchedule struct {
	Comment string `json:"comment"`
}

type twoGISContactGr struct {
	Contacts []twoGISContact `json:"contacts"`
}

type twoGISContact struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// twoGISPoint holds the geographic coordinates of an item.
type twoGISPoint struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// TwoGISChecker verifies place existence via 2GIS Catalog API.
// Found → open (2GIS removes closed places from index).
// Not found → unknown (needs fallback check via Yandex Maps).
type TwoGISChecker struct {
	apiKey string
	client *http.Client
}

// NewTwoGIS creates a 2GIS checker. Empty apiKey defaults to "demo".
func NewTwoGIS(cfg TwoGISConfig) *TwoGISChecker {
	key := cfg.APIKey
	if key == "" {
		key = twoGISDemoKey
	}
	return &TwoGISChecker{
		apiKey: key,
		client: &http.Client{Timeout: twoGISTimeout},
	}
}

// Check queries 2GIS for a place. Returns PlaceOpen if found,
// PlaceNotFound if not (may be closed — needs fallback).
//
// When address is non-empty, the query is anchored on the known address
// (name + address + city) and the first result is validated: if the result's
// address_name does not share street-name tokens with the query address, a
// PlaceNotFound result is returned so CompositeChecker falls through to Yandex.
// When address is empty, the legacy name+city query is used with no validation.
func (c *TwoGISChecker) Check(ctx context.Context, name, city, address string) (*CheckResult, error) {
	var query string
	if address != "" {
		query = name + " " + address + " " + city
	} else {
		query = name + " " + city
	}

	params := url.Values{
		"q":      {query},
		"type":   {"branch"},
		"key":    {c.apiKey},
		"fields": {"items.schedule,items.contact_groups,items.point"},
	}

	reqURL := twoGISBaseURL + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("2gis: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("2gis: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("2gis: read: %w", err)
	}

	var gr twoGISResponse
	if err := json.Unmarshal(data, &gr); err != nil {
		return nil, fmt.Errorf("2gis: parse: %w", err)
	}

	if gr.Meta.Code != http.StatusOK {
		return nil, fmt.Errorf("2gis: API code %d", gr.Meta.Code)
	}

	if gr.Result.Total == 0 || len(gr.Result.Items) == 0 {
		return &CheckResult{Status: PlaceNotFound}, nil
	}

	item := gr.Result.Items[0]

	// Address-anchored validation: when the caller provided a known address,
	// verify that the result's address_name shares street-name tokens with it.
	// A mismatch (e.g. query "Невский 28", result "улица Чайковского 83/7")
	// means 2GIS returned a fuzzy/nearby place rather than the queried one.
	// Return PlaceNotFound so CompositeChecker falls through to Yandex instead
	// of using the wrong place's coordinates. Never return an error here —
	// that would trip the Resilient circuit breaker.
	if address != "" && !addressTokensOverlap(address, item.AddressName) {
		return &CheckResult{Status: PlaceNotFound}, nil
	}

	return buildTwoGISResult(item), nil
}

// addressNoise is the set of Russian street-type abbreviations and stop-words
// stripped before token overlap comparison. Lowercased.
//
// Matching is purely on street-name tokens — house numbers are treated as
// corroborating but not required (format variance: "28" / "28/7" / "28А").
var addressNoise = map[string]bool{ //nolint:gochecknoglobals
	"улица": true, "ул":  true,
	"проспект": true, "пр":  true, "пр-т": true, "просп": true,
	"переулок": true, "пер": true,
	"набережная": true, "наб": true,
	"шоссе":     true, "ш":   true,
	"бульвар":   true, "б-р": true, "бул": true,
	"площадь": true, "пл": true,
	"дом": true, "д": true,
	"корпус": true, "корп": true, "к": true,
	"строение": true, "стр": true,
	"литера": true, "лит": true,
}

// addressTokenize lowercases s, removes punctuation, splits into words,
// and drops address-type noise words and pure-digit tokens.
// The result is the set of meaningful street-name tokens.
func addressTokenize(s string) map[string]bool {
	s = strings.ToLower(s)
	// Replace punctuation and slashes with spaces.
	s = strings.Map(func(r rune) rune {
		if unicode.IsPunct(r) || r == '/' || r == '\\' {
			return ' '
		}
		return r
	}, s)

	tokens := make(map[string]bool)
	for _, t := range strings.Fields(s) {
		if addressNoise[t] {
			continue
		}
		// Drop pure-digit tokens (house numbers) to avoid false-reject when
		// formatting differs ("28" vs "28/7" vs "28А"). Street names may
		// contain digits but are not purely numeric.
		if isPureDigit(t) {
			continue
		}
		tokens[t] = true
	}
	return tokens
}

// isPureDigit returns true when every rune in s is an ASCII digit.
func isPureDigit(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// addressTokensOverlap returns true when the two address strings share at
// least one meaningful street-name token after noise removal. A single shared
// non-noise word is sufficient — this is deliberately lenient to tolerate
// formatting variance ("Невский пр., 28" vs "Невский проспект, 28").
// Returns false when either side has no meaningful tokens after normalization.
func addressTokensOverlap(queryAddr, resultAddr string) bool {
	qTokens := addressTokenize(queryAddr)
	rTokens := addressTokenize(resultAddr)
	if len(qTokens) == 0 || len(rTokens) == 0 {
		return false
	}
	for t := range qTokens {
		if rTokens[t] {
			return true
		}
	}
	return false
}

// buildTwoGISResult converts a 2GIS item into a CheckResult with OrgData.
func buildTwoGISResult(item twoGISItem) *CheckResult {
	od := &OrgData{
		Status:  PlaceOpen,
		Name:    item.Name,
		Address: item.AddressName,
	}
	applyTwoGISContacts(item.Contacts, od)
	if item.Schedule != nil && item.Schedule.Comment != "" {
		od.Hours = item.Schedule.Comment
	}
	if item.Point != nil {
		od.Latitude = item.Point.Lat
		od.Longitude = item.Point.Lon
	}
	return &CheckResult{
		Status:   PlaceOpen,
		RawTitle: item.Name,
		OrgData:  od,
	}
}

// applyTwoGISContacts extracts phone and website from 2GIS contact groups.
func applyTwoGISContacts(groups []twoGISContactGr, od *OrgData) {
	for _, cg := range groups {
		for _, ct := range cg.Contacts {
			switch ct.Type {
			case "phone":
				if od.Phone == "" {
					od.Phone = ct.Value
				}
			case "website":
				if od.Website == "" {
					od.Website = cleanTwoGISURL(ct.Value)
				}
			}
		}
	}
}

// cleanTwoGISURL extracts the real URL from a 2GIS redirect link.
// Format: http://link.2gis.ru/...?http://real-site.com/
func cleanTwoGISURL(u string) string {
	if idx := strings.Index(u, "?http"); idx >= 0 {
		return u[idx+1:]
	}
	return strings.TrimPrefix(u, "http://link.2gis.ru/")
}

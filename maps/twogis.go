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
	// OnAddressRejected is called when a 2GIS hit is discarded because its
	// address_name does not share distinctive street-name tokens with the
	// query address. Use this to count validation-rejections in prod so they
	// can be distinguished from genuine not-found responses.
	OnAddressRejected func()
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
	apiKey            string
	client            *http.Client
	onAddressRejected func()
}

// NewTwoGIS creates a 2GIS checker. Empty apiKey defaults to "demo".
func NewTwoGIS(cfg TwoGISConfig) *TwoGISChecker {
	key := cfg.APIKey
	if key == "" {
		key = twoGISDemoKey
	}
	return &TwoGISChecker{
		apiKey:            key,
		client:            &http.Client{Timeout: twoGISTimeout},
		onAddressRejected: cfg.OnAddressRejected,
	}
}

// Check queries 2GIS for a place. Returns PlaceOpen if found,
// PlaceNotFound if not (may be closed — needs fallback).
//
// When address is non-empty, the query is anchored on the known address
// (name + address + city) and the first result is validated: if the result's
// address_name does not share distinctive street-name tokens with the query
// address, PlaceNotFound is returned so CompositeChecker falls through to
// Yandex. City, generic adjectives, geo-nouns, and house numbers are excluded
// from eligible tokens — only the actual street name counts. The
// OnAddressRejected callback fires on each such rejection for observability.
// When address is empty, the legacy name+city query is used with no validation.
func (c *TwoGISChecker) Check(ctx context.Context, name, city, address string) (*CheckResult, error) {
	var query string
	if address != "" {
		query = name + " " + address + " " + city
	} else {
		query = name + " " + city
	}

	params := url.Values{
		"q":    {query},
		"type": {"branch"},
		"key":  {c.apiKey},
		// items.address_name is required for address-mismatch validation;
		// listed explicitly so the dependency is self-documenting.
		"fields": {"items.schedule,items.contact_groups,items.point,items.address_name"},
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
	// verify that the result's address_name shares distinctive street-name
	// tokens with it. City, generic adjectives ("большая"/"малая"/etc.),
	// geo-nouns ("реки"), and house numbers are excluded from eligible tokens.
	//
	// A mismatch means 2GIS returned a fuzzy/nearby place rather than the
	// queried one. Return PlaceNotFound so CompositeChecker falls through to
	// Yandex rather than using wrong coordinates. Never return an error here —
	// that would trip the Resilient circuit breaker.
	if address != "" && !addressTokensOverlap(address, item.AddressName, city) {
		if c.onAddressRejected != nil {
			c.onAddressRejected()
		}
		return &CheckResult{Status: PlaceNotFound}, nil
	}

	return buildTwoGISResult(item), nil
}

// addressNoise is the set of Russian street-type abbreviations and stop-words
// stripped before token overlap comparison. Lowercased.
var addressNoise = map[string]bool{ //nolint:gochecknoglobals
	"улица": true, "ул": true,
	"проспект": true, "пр": true, "пр-т": true, "просп": true,
	"переулок": true, "пер": true,
	"набережная": true, "наб": true,
	"шоссе": true, "ш": true,
	"бульвар": true, "б-р": true, "бул": true,
	"площадь": true, "пл": true,
	"дом": true, "д": true,
	"корпус": true, "корп": true, "к": true,
	"строение": true, "стр": true,
	"литера": true, "лит": true,
	// Generic geo-nouns that recur across many distinct addresses.
	"реки": true, "линия": true, "аллея": true,
}

// addressGenericAdjectives is a stop-set of leading adjectives that prefix
// many distinct Russian streets (e.g. "Большая Морская" vs "Большая Конюшенная").
// Stripping them means "Большой проспект П.С." and "Малый проспект П.С." become
// token-identical — a known residual; validation falls through to Yandex rather
// than returning wrong coordinates, which is the safer direction.
var addressGenericAdjectives = map[string]bool{ //nolint:gochecknoglobals
	"большая": true, "большой": true, "большое": true,
	"малая": true, "малой": true, "малое": true, "малый": true,
	"новая": true, "новый": true, "новое": true,
	"старая": true, "старый": true, "старое": true,
	"верхняя": true, "верхний": true, "верхнее": true,
	"нижняя": true, "нижний": true, "нижнее": true,
	"средняя": true, "средний": true, "среднее": true,
}

// citySynonyms maps canonical lowercased city names to their abbreviations
// and alternate spellings, so those alternates are also stripped from tokens.
var citySynonyms = map[string][]string{ //nolint:gochecknoglobals
	"санкт-петербург": {"спб", "питер", "ленинград"},
	"москва":          {"мск"},
}

// buildCityTokens returns a set of lowercased tokens to strip for the given
// city: the city's own words plus known synonyms/abbreviations.
func buildCityTokens(city string) map[string]bool {
	city = strings.ToLower(city)
	city = strings.Map(func(r rune) rune {
		if unicode.IsPunct(r) && r != '-' {
			return ' '
		}
		return r
	}, city)

	result := make(map[string]bool)
	for _, t := range strings.Fields(city) {
		result[t] = true
	}
	// Merge synonyms for known cities.
	rawLower := strings.ToLower(strings.Join(strings.Fields(city), "-"))
	for canonical, syns := range citySynonyms {
		// Check if canonical tokens are all present in result, or the joined
		// form matches (handles "санкт-петербург" which splits to two tokens).
		canonicalParts := strings.Fields(strings.ReplaceAll(canonical, "-", " "))
		allPresent := true
		for _, p := range canonicalParts {
			if !result[p] {
				allPresent = false
				break
			}
		}
		if allPresent || rawLower == canonical {
			for _, s := range syns {
				result[s] = true
			}
		}
	}
	return result
}

// isHouseNumber returns true when s looks like a house number token:
// pure digits ("28") or a token starting with a digit ("28а", "28k", "1-я").
// Different streets share house numbers, so such tokens must not be
// treated as distinctive match evidence.
func isHouseNumber(s string) bool {
	if s == "" {
		return false
	}
	return s[0] >= '0' && s[0] <= '9'
}

// addressDistinctiveTokens lowercases s, removes punctuation, splits into
// words, and drops:
//   - address-type noise words (addressNoise)
//   - generic leading adjectives (addressGenericAdjectives)
//   - city tokens and their known synonyms (buildCityTokens)
//   - house-number tokens (isHouseNumber: leading digit)
//
// The result contains only tokens that are distinctive for the street name.
func addressDistinctiveTokens(s, city string) map[string]bool {
	s = strings.ToLower(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsPunct(r) || r == '/' || r == '\\' {
			return ' '
		}
		return r
	}, s)

	cityToks := buildCityTokens(city)
	tokens := make(map[string]bool)
	for _, t := range strings.Fields(s) {
		if addressNoise[t] {
			continue
		}
		if addressGenericAdjectives[t] {
			continue
		}
		if cityToks[t] {
			continue
		}
		if isHouseNumber(t) {
			continue
		}
		tokens[t] = true
	}
	return tokens
}

// addressTokensOverlap returns true when the two address strings share at
// least one DISTINCTIVE street-name token after stripping noise, generic
// adjectives, city tokens (and synonyms), and house numbers.
//
// Returns false when either side produces no distinctive tokens (e.g. the
// address is city-only or house-only) — falls through to Yandex, the safer
// direction when validation cannot be performed.
func addressTokensOverlap(queryAddr, resultAddr, city string) bool {
	qTokens := addressDistinctiveTokens(queryAddr, city)
	rTokens := addressDistinctiveTokens(resultAddr, city)
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

// addressStreetHint returns a space-joined sorted string of the distinctive
// street-name tokens from addr (city, house numbers, noise, and generic
// adjectives stripped). Used by the Yandex search query to anchor on the
// street name without the noise that over-constrains the query.
// Returns empty string when no distinctive tokens remain.
func addressStreetHint(addr, city string) string {
	tokens := addressDistinctiveTokens(addr, city)
	if len(tokens) == 0 {
		return ""
	}
	// Sort for deterministic query strings.
	parts := make([]string, 0, len(tokens))
	for t := range tokens {
		parts = append(parts, t)
	}
	// Simple insertion sort — token count is small (≤5 typically).
	for i := 1; i < len(parts); i++ {
		for j := i; j > 0 && parts[j] < parts[j-1]; j-- {
			parts[j], parts[j-1] = parts[j-1], parts[j]
		}
	}
	return strings.Join(parts, " ")
}

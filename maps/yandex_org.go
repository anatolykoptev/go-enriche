package maps

import (
	"encoding/json"
	"log/slog"
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
//
// 2026-07-16: A hybrid JSON-extraction parser was added as the primary path.
// It anchors on known field names, extracts the enclosing JSON object, and
// unmarshals it into a typed struct — resilient to field ordering, encoding,
// and minor format changes. The regex patterns remain as a fallback for
// pages where JSON extraction fails (e.g. malformed JS state).
var (
	reOrgShortTitle = regexp.MustCompile(`"shortTitle"\s*:\s*"([^"]{2,100})"`)
	reOrgChainName  = regexp.MustCompile(`"chain"\s*:\s*\{[^}]*?"name"\s*:\s*"([^"]{2,100})"`)
	reOrgName       = regexp.MustCompile(`"name"\s*:\s*"([^"]{2,100})"`)

	reOrgFullAddress = regexp.MustCompile(`"fullAddress"\s*:\s*"([^"]+)"`)
	reOrgAddress     = regexp.MustCompile(`"address"\s*:\s*\{[^}]*?"formatted"\s*:\s*"([^"]+)"`)

	reOrgPhoneNew = regexp.MustCompile(`"phones"\s*:\s*\[[^\]]*?"number"\s*:\s*"([^"]+)"`)
	reOrgPhone    = regexp.MustCompile(`"phones"\s*:\s*\[[^\]]*?"formatted"\s*:\s*"([^"]+)"`)

	reOrgWorkingStatus = regexp.MustCompile(`"currentWorkingStatus"\s*:\s*\{[^}]*?"text"\s*:\s*"([^"]+)"`)
	reOrgHours         = regexp.MustCompile(`"hours"\s*:\s*\{[^}]*?"text"\s*:\s*"([^"]+)"`)

	reOrgRatingNew = regexp.MustCompile(`"ratingData"\s*:\s*\{[^}]*?"ratingValue"\s*:\s*([\d.]+)`)
	reOrgRating    = regexp.MustCompile(`"rating"\s*:\s*\{[^}]*?"score"\s*:\s*([\d.]+)`)

	reOrgCoords = regexp.MustCompile(`"coordinates"\s*:\s*\[([\d.]+)\s*,\s*([\d.]+)\]`)

	reOrgURLNew = regexp.MustCompile(`"urls"\s*:\s*\[\s*"(https?://[^"]+)"`)
	reOrgURL    = regexp.MustCompile(`"urls"\s*:\s*\[[^\]]*?"value"\s*:\s*"(https?://[^"]+)"`)

	reOrgCategories = regexp.MustCompile(`"categories"\s*:\s*(\[[^\]]+\])`)
	reOrgRubrics    = regexp.MustCompile(`"rubrics"\s*:\s*(\[[^\]]+\])`)
	reRubricName    = regexp.MustCompile(`"name"\s*:\s*"([^"]+)"`)
)

// yandexBusinessData is the typed JSON structure of a Yandex Maps org page
// business data block. Fields from both the new (2026-07+) and legacy formats
// are included — json.Unmarshal picks up whichever are present.
type yandexBusinessData struct {
	// New format (2026-07+)
	ShortTitle          string                  `json:"shortTitle"`
	FullAddress         string                  `json:"fullAddress"`
	Chain               yandexChain             `json:"chain"`
	Status              string                  `json:"status"`
	Phones              []yandexPhone           `json:"phones"`
	URLs                []string                `json:"urls"`
	CurrentWorkingStatus yandexWorkingStatus    `json:"currentWorkingStatus"`
	RatingData          yandexRatingData        `json:"ratingData"`
	Coordinates         []float64               `json:"coordinates"`
	Categories          []yandexCategory        `json:"categories"`

	// Legacy format fields (still present on some pages)
	Name    string         `json:"name"`
	Address yandexAddress  `json:"address"`
	Hours   yandexHours    `json:"hours"`
	Rating  yandexRating   `json:"rating"`
	Rubrics []yandexRubric `json:"rubrics"`
}

type yandexChain struct {
	Name string `json:"name"`
}

type yandexPhone struct {
	Number    string `json:"number"`
	Formatted string `json:"formatted"`
}

type yandexWorkingStatus struct {
	Text      string `json:"text"`
	ShortText string `json:"shortText"`
}

type yandexRatingData struct {
	RatingValue float64 `json:"ratingValue"`
}

type yandexCategory struct {
	Name string `json:"name"`
}

type yandexAddress struct {
	Formatted string `json:"formatted"`
}

type yandexHours struct {
	Text string `json:"text"`
}

type yandexRating struct {
	Score float64 `json:"score"`
}

type yandexRubric struct {
	Name string `json:"name"`
}

// anchorFields are the field names used to locate the business data JSON
// block in the rendered HTML. The parser tries each in order — the first
// that yields a valid JSON object wins.
var anchorFields = []string{
	`"shortTitle"`,
	`"currentWorkingStatus"`,
	`"fullAddress"`,
	`"chain"`,
	`"ratingData"`,
	// Legacy format anchors
	`"rubrics"`,
}

// parseOrgPage extracts all business data from browser-rendered Yandex Maps HTML.
// It first tries to extract and unmarshal the business data JSON block (primary
// path, resilient to format changes), then falls back to regex patterns (legacy
// path, handles pages where JSON extraction fails).
func parseOrgPage(html []byte) *OrgData {
	// Primary path: JSON extraction.
	if od := parseOrgPageJSON(html); od != nil {
		return od
	}

	// Fallback: regex extraction (handles legacy pages + malformed JSON).
	return parseOrgPageRegex(html)
}

// parseOrgPageJSON attempts to extract the business data JSON block from
// rendered HTML and unmarshal it into OrgData. Returns nil if extraction fails.
func parseOrgPageJSON(html []byte) *OrgData {
	block, ok := extractBusinessJSON(html)
	if !ok {
		return nil
	}

	var data yandexBusinessData
	if err := json.Unmarshal(block, &data); err != nil {
		slog.Debug("yandex_org: JSON unmarshal failed, falling back to regex",
			slog.String("error", err.Error()),
			slog.Int("block_size", len(block)))
		return nil
	}

	od := &OrgData{}

	// Status (from the business data block, not the whole HTML — more precise).
	switch data.Status {
	case yandexStatusPermanentClosed:
		od.Status = PlacePermanentClosed
	case yandexStatusTemporaryClosed:
		od.Status = PlaceTemporaryClosed
	case yandexStatusOpen:
		od.Status = PlaceOpen
	default:
		// Fall back to the HTML-wide status regex if the block didn't have it.
		switch parseOrgStatus(html) {
		case yandexStatusPermanentClosed:
			od.Status = PlacePermanentClosed
		case yandexStatusTemporaryClosed:
			od.Status = PlaceTemporaryClosed
		case yandexStatusOpen:
			od.Status = PlaceOpen
		}
	}

	// Name: prefer shortTitle, then chain.name, then legacy "name".
	od.Name = firstNonEmpty(data.ShortTitle, data.Chain.Name, data.Name)

	// Address: prefer fullAddress, then legacy address.formatted.
	od.Address = firstNonEmpty(data.FullAddress, data.Address.Formatted)

	// Phone: prefer "number" field, then legacy "formatted".
	for _, p := range data.Phones {
		if p.Number != "" {
			od.Phone = p.Number
			break
		}
		if p.Formatted != "" {
			od.Phone = p.Formatted
		}
	}

	// Hours: prefer currentWorkingStatus.text, then legacy hours.text.
	od.Hours = firstNonEmpty(data.CurrentWorkingStatus.Text, data.Hours.Text)

	// Website: new format is a plain string array, old format was
	// [{"value":"..."}] — the struct handles both via the URLs []string field
	// (new) and the regex fallback (old).
	if len(data.URLs) > 0 {
		od.Website = data.URLs[0]
	}

	// Rating: prefer ratingData.ratingValue, then legacy rating.score.
	if data.RatingData.RatingValue > 0 {
		od.Rating = data.RatingData.RatingValue
	} else if data.Rating.Score > 0 {
		od.Rating = data.Rating.Score
	}

	// Coordinates: [lon, lat] in Yandex Maps JSON (longitude first).
	if len(data.Coordinates) >= 2 { //nolint:mnd
		od.Longitude = data.Coordinates[0]
		od.Latitude = data.Coordinates[1]
	}

	// Categories: prefer "categories" (new), then "rubrics" (legacy).
	if len(data.Categories) > 0 {
		od.Categories = dedupStrings(extractCategoryNames(data.Categories))
	} else if len(data.Rubrics) > 0 {
		od.Categories = dedupStrings(extractRubricNames(data.Rubrics))
	}

	// Validation: log when critical fields are missing.
	missing := checkMissingFields(od)
	if len(missing) > 0 {
		slog.Debug("yandex_org: parsed with missing fields",
			slog.String("name", od.Name),
			slog.String("missing", strings.Join(missing, ", ")))
	}

	return od
}

// extractBusinessJSON locates the business data JSON object in rendered HTML
// by anchoring on known field names and extracting the enclosing balanced
// JSON object. Returns the raw JSON bytes and true on success.
func extractBusinessJSON(html []byte) ([]byte, bool) {
	for _, anchor := range anchorFields {
		idx := strings.Index(string(html), anchor)
		if idx < 0 {
			continue
		}

		// Walk backwards from the anchor to find the opening '{'.
		start := findEnclosingBrace(html, idx)
		if start < 0 {
			continue
		}

		// Walk forwards from start to find the matching '}'.
		end := findMatchingBrace(html, start)
		if end < 0 {
			continue
		}

		block := html[start : end+1]

		// Sanity check: the block must contain at least one business-data
		// field to avoid matching unrelated JSON objects.
		if !looksLikeBusinessData(block) {
			continue
		}

		return block, true
	}
	return nil, false
}

// findEnclosingBrace walks backwards from pos to find the '{' that opens
// the JSON object containing the character at pos. Tracks brace depth.
func findEnclosingBrace(data []byte, pos int) int {
	depth := 0
	for i := pos; i >= 0; i-- {
		switch data[i] {
		case '}':
			depth++
		case '{':
			if depth == 0 {
				return i
			}
			depth--
		}
	}
	return -1
}

// findMatchingBrace walks forwards from start (which must be '{') to find
// the matching '}'. Tracks brace depth, ignoring braces inside strings.
func findMatchingBrace(data []byte, start int) int {
	if start >= len(data) || data[start] != '{' {
		return -1
	}

	depth := 0
	inString := false
	escaped := false

	for i := start; i < len(data); i++ {
		c := data[i]

		if escaped {
			escaped = false
			continue
		}

		if c == '\\' && inString {
			escaped = true
			continue
		}

		if c == '"' {
			inString = !inString
			continue
		}

		if inString {
			continue
		}

		switch c {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// looksLikeBusinessData checks if a JSON block contains at least 2 known
// business-data fields. This prevents matching unrelated JSON objects that
// happen to contain one of the anchor fields.
func looksLikeBusinessData(block []byte) bool {
	knownFields := []string{
		`"shortTitle"`, `"fullAddress"`, `"chain"`, `"phones"`,
		`"urls"`, `"currentWorkingStatus"`, `"ratingData"`, `"categories"`,
		`"coordinates"`, `"status"`, `"rubrics"`, `"address"`,
		`"hours"`, `"rating"`,
	}
	hits := 0
	for _, field := range knownFields {
		if strings.Contains(string(block), field) {
			hits++
		}
	}
	return hits >= 2 //nolint:mnd
}

// parseOrgPageRegex is the legacy regex-based parser, kept as a fallback
// for pages where JSON extraction fails.
func parseOrgPageRegex(html []byte) *OrgData {
	od := &OrgData{}

	switch parseOrgStatus(html) {
	case yandexStatusPermanentClosed:
		od.Status = PlacePermanentClosed
	case yandexStatusTemporaryClosed:
		od.Status = PlaceTemporaryClosed
	case yandexStatusOpen:
		od.Status = PlaceOpen
	}

	od.Name = extractFirstMatch(html, reOrgShortTitle, reOrgChainName, reOrgName)
	od.Address = extractFirstMatch(html, reOrgFullAddress, reOrgAddress)
	od.Phone = extractFirstMatch(html, reOrgPhoneNew, reOrgPhone)
	od.Hours = extractFirstMatch(html, reOrgWorkingStatus, reOrgHours)
	od.Website = extractFirstMatch(html, reOrgURLNew, reOrgURL)

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
// Tries "categories" (new format) first, falls back to "rubrics" (legacy).
func extractAllRubrics(data []byte) []string {
	if arrMatch := reOrgCategories.FindSubmatch(data); len(arrMatch) >= 2 {
		if cats := extractRubricNamesRaw(arrMatch[1]); len(cats) > 0 {
			return cats
		}
	}
	arrMatch := reOrgRubrics.FindSubmatch(data)
	if len(arrMatch) < 2 { //nolint:mnd
		return nil
	}
	return extractRubricNamesRaw(arrMatch[1])
}

// extractRubricNamesRaw finds all "name" values within a JSON array fragment.
func extractRubricNamesRaw(jsonArray []byte) []string {
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

// extractCategoryNames extracts "name" values from typed category structs.
func extractCategoryNames(cats []yandexCategory) []string {
	out := make([]string, 0, len(cats))
	for _, c := range cats {
		if c.Name != "" {
			out = append(out, c.Name)
		}
	}
	return out
}

// extractRubricNames extracts "name" values from typed rubric structs.
func extractRubricNames(rubrics []yandexRubric) []string {
	out := make([]string, 0, len(rubrics))
	for _, r := range rubrics {
		if r.Name != "" {
			out = append(out, r.Name)
		}
	}
	return out
}

// checkMissingFields returns a list of critical field names that are empty
// in the parsed OrgData. Used for debug logging.
func checkMissingFields(od *OrgData) []string {
	var missing []string
	if od.Name == "" {
		missing = append(missing, "name")
	}
	if od.Phone == "" {
		missing = append(missing, "phone")
	}
	if od.Website == "" {
		missing = append(missing, "website")
	}
	if od.Address == "" {
		missing = append(missing, "address")
	}
	if od.Hours == "" {
		missing = append(missing, "hours")
	}
	return missing
}

// firstNonEmpty returns the first non-empty string from the arguments.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// dedupStrings removes duplicate strings while preserving order.
func dedupStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// unescapeJSON handles basic JSON string escapes (\", \\, \/, \n).
func unescapeJSON(s string) string {
	s = strings.ReplaceAll(s, `\"`, `"`)
	s = strings.ReplaceAll(s, `\\`, `\`)
	s = strings.ReplaceAll(s, `\/`, `/`)
	s = strings.ReplaceAll(s, `\n`, "\n")
	return strings.TrimSpace(s)
}

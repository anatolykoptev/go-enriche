package extract

import (
	"regexp"
	"strconv"
	"strings"
)

var reDigitsOnly = regexp.MustCompile(`\D`)

// DigitsOnly strips every non-digit rune from s. Exported so a caller
// outside this package (the enriche resolver's phone dedup key, resolve.go)
// derives the SAME normalization as every phone-tier/dedup key inside this
// package, rather than hand-rolling a second, potentially-drifting digit
// filter — see reDigitsOnly.
func DigitsOnly(s string) string {
	return reDigitsOnly.ReplaceAllString(s, "")
}

const maxPriceLen = 60

var (
	reCSS           = regexp.MustCompile(`[{}]|\w+\s*:\s*\w+\s*;|:\s*\w+\(|margin|padding|display|font-size`)
	reHTMLTag       = regexp.MustCompile(`<[a-zA-Z/]`)
	reJSCode        = regexp.MustCompile(`(?:var |const |let |function |=>|===)`)
	rePriceCurrency = regexp.MustCompile(`(?i)(?:\d|бесплатно|free|₽|руб|\$|€|£)`)
)

// ValidatePrice checks if a price string looks like an actual price
// rather than CSS, HTML, JS code, or unrelated text.
func ValidatePrice(price string) bool {
	price = strings.TrimSpace(price)
	if price == "" {
		return false
	}

	if len([]rune(price)) > maxPriceLen {
		return false
	}

	if !rePriceCurrency.MatchString(price) {
		return false
	}

	if reCSS.MatchString(price) {
		return false
	}

	if reHTMLTag.MatchString(price) {
		return false
	}

	if reJSCode.MatchString(price) {
		return false
	}

	if strings.Contains(price, "://") {
		return false
	}

	return true
}

// mobileCodeLow/mobileCodeHigh bound the RU mobile-operator ("Mobile DEF")
// 3-digit area-code range — Rossvyaz's {900,999} allocation. Named here
// (not just inlined into validCodeRanges below) so validCodeRanges'
// "Mobile DEF" row and isMobileCode share the EXACT same bounds instead of
// two independently hand-typed {900,999} literals that could drift apart.
const (
	mobileCodeLow  = 900
	mobileCodeHigh = 999
)

// validCodeRanges defines valid Russian area/operator code ranges (Rossvyaz).
var validCodeRanges = [][2]int{
	{301, 349},                      // Landline
	{351, 395},                      // Landline
	{401, 499},                      // Landline
	{800, 816},                      // SPb/Leningrad
	{820, 879},                      // Northern regions
	{mobileCodeLow, mobileCodeHigh}, // Mobile DEF
}

// isMobileCode reports whether code falls in the RU mobile-operator range
// (mobileCodeLow-mobileCodeHigh) — exposed as its own predicate for a
// caller (e.g. citycode.go's isRUGeographicLandline) that needs "is this
// SPECIFICALLY mobile", not just "is this any valid RU code"
// (isValidAreaCode). Reuses the SAME bounds validCodeRanges' "Mobile DEF"
// row encodes — one source of truth, not a second hand-typed range.
func isMobileCode(code int) bool {
	return code >= mobileCodeLow && code <= mobileCodeHigh
}

// nonGeoServiceAreaCodes are the RU DEF non-geographic "service" 3-digit
// area codes that sit INSIDE the {800,816} validCodeRanges bracket without
// identifying a fixed city — Rossvyaz's shared-cost/universal-access/
// premium allocation (803, 805-809; 804 shared-cost). The bracket's
// remaining codes — notably 812 (Saint Petersburg) — are real ABC
// geographic codes and are correctly NOT listed here. The plain 8-800
// toll-free code is intentionally NOT repeated here: it already has its
// own named predicate, isTollFree (contacts.go) — see isGeographicAreaCode
// below, which checks both.
var nonGeoServiceAreaCodes = map[int]bool{
	803: true,
	804: true,
	805: true,
	806: true,
	807: true,
	808: true,
	809: true,
}

// isNonGeoServiceCode reports whether code is one of the RU DEF
// non-geographic "service" codes in nonGeoServiceAreaCodes. The single
// source of truth a caller needing the FULL 80x non-geographic exclusion
// (isGeographicAreaCode below; citycode.go's isRUGeographicLandline)
// reuses instead of hand-rolling a second 80x list.
func isNonGeoServiceCode(code int) bool {
	return nonGeoServiceAreaCodes[code]
}

// isGeographicAreaCode reports whether code is a valid RU area code
// (isValidAreaCode) that identifies a FIXED CITY — i.e. neither a
// mobile-operator code (isMobileCode) nor a non-geographic DEF service
// code (the plain toll-free 800 via isTollFree, or the broader 803-809
// service block via isNonGeoServiceCode). The composed predicate
// citycode.go's isRUGeographicLandline reuses instead of re-deriving its
// own exclusion list from scratch.
func isGeographicAreaCode(code int) bool {
	return isValidAreaCode(code) && !isMobileCode(code) && !isTollFree(code) && !isNonGeoServiceCode(code)
}

// ValidatePhone checks if a phone number is a valid Russian number
// by verifying the area/operator code against Rossvyaz ranges.
func ValidatePhone(phone string) bool {
	digits := reDigitsOnly.ReplaceAllString(phone, "")

	if len(digits) != 11 || (digits[0] != '8' && digits[0] != '7') {
		return false
	}

	code, _ := strconv.Atoi(digits[1:4])

	return isValidAreaCode(code)
}

var reStreetWord = regexp.MustCompile(`(?i)(?:` +
	`ул\.|улица|пр\.|просп|проспект|наб\.|набережная|пер\.|переулок|` +
	`ш\.|шоссе|пл\.|площадь|б-р|бульвар|линия|аллея|остров|` +
	`город\b|г\.\s*\w|` +
	`д\.\s*\d|корп\.|стр\.|лит\.|` +
	`\bstreet\b|\bst\b\.?|\bavenue\b|\bave\b\.?|\broad\b|\brd\b\.?|\bdrive\b|\blane\b|\bblvd\b` +
	`)`)

var reMarketingJunk = regexp.MustCompile(`(?i)(?:бронирование|меню|цены|как пройти|режим работы|` +
	`собственник|без комиссии|аренда|официальном сайте|подробнее|интересные факты|фото)`)

const (
	addrMinLen = 8
	addrMaxLen = 120
)

// ValidateAddress checks if a string looks like a real postal address
// rather than a page title, marketing text, CSS, or URL.
func ValidateAddress(addr string) bool {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return false
	}

	runeLen := len([]rune(addr))
	if runeLen < addrMinLen || runeLen > addrMaxLen {
		return false
	}

	if !reStreetWord.MatchString(addr) {
		return false
	}

	if reMarketingJunk.MatchString(addr) {
		return false
	}

	if reCSS.MatchString(addr) {
		return false
	}

	if strings.Contains(addr, "://") {
		return false
	}

	return true
}

func isValidAreaCode(code int) bool {
	for _, r := range validCodeRanges {
		if code >= r[0] && code <= r[1] {
			return true
		}
	}

	return false
}

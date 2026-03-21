package extract

import (
	"regexp"
	"strconv"
	"strings"
)

var reDigitsOnly = regexp.MustCompile(`\D`)

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

// validCodeRanges defines valid Russian area/operator code ranges (Rossvyaz).
var validCodeRanges = [][2]int{
	{301, 349}, // Landline
	{351, 395}, // Landline
	{401, 499}, // Landline
	{800, 816}, // SPb/Leningrad
	{820, 879}, // Northern regions
	{900, 999}, // Mobile DEF
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

func isValidAreaCode(code int) bool {
	for _, r := range validCodeRanges {
		if code >= r[0] && code <= r[1] {
			return true
		}
	}

	return false
}

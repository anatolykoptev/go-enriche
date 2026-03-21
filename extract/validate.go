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

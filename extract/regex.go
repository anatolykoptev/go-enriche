package extract

import (
	"regexp"
	"strings"
)

// Pre-compiled regex patterns for Russian and English fact extraction.
//
// rePrice requires the captured text to START with a price-shaped token —
// an optional "от"/"до"/"from" prefix followed by a digit, a Western
// currency symbol ($, €, £ — conventionally placed before the number), or
// an explicit "бесплатно"/"free" indicator — and caps the capture at ~30
// chars (a bare price rarely exceeds that). This rejects marketing prose
// where "цена"/"стоимость" is part of a sentence (e.g. "Цена: уборки за 30
// минут гарантированно!"), which the old `[^\n<]{2,80}` pattern captured
// verbatim as a garbage price fact (issue #56). The Russian ₽ symbol is
// intentionally NOT a valid start token: it conventionally FOLLOWS the
// number ("1500 ₽"), so a leading ₽ is almost always a schema.org price-
// tier symbol ("₽₽"/"₽₽₽") rather than a real price. A bare currency symbol
// that still slips through is caught downstream by ValidatePrice's digit
// requirement.
var (
	reAddress = regexp.MustCompile(`(?i)(?:адрес|address)[:\s]+([^\n<]{5,100})`)
	rePhone   = regexp.MustCompile(`(?:\+7|8)[\s\-]?\(?\d{3}\)?[\s\-]?\d{3}[\s\-]?\d{2}[\s\-]?\d{2}`)
	rePrice   = regexp.MustCompile(`(?i)(?:цена|стоимость|price)[:\s]+((?:от\s+|до\s+|from\s+)?(?:бесплатно|free|[\d$€£])[^\n<]{0,29})`)
)

// Plain-text-safe variants (no HTML < boundary).
var (
	reSnippetAddress = regexp.MustCompile(`(?i)(?:адрес|address)[:\s]+([^\n]{5,100})`)
	reSnippetPrice   = regexp.MustCompile(`(?i)(?:цена|стоимость|price)[:\s]+((?:от\s+|до\s+|from\s+)?(?:бесплатно|free|[\d$€£])[^\n]{0,29})`)
)


// regexAddress extracts an address from text using regex.
func regexAddress(text string) *string {
	return regexSubmatch(reAddress, text)
}

// regexPhone extracts a Russian phone number from text.
func regexPhone(text string) *string {
	return regexMatch(rePhone, text)
}

// regexPrice extracts a price from text using regex.
func regexPrice(text string) *string {
	return regexSubmatch(rePrice, text)
}

// regexSubmatch returns the first capturing group, or nil.
func regexSubmatch(re *regexp.Regexp, text string) *string {
	m := re.FindStringSubmatch(text)
	if m == nil {
		return nil
	}
	var s string
	if len(m) >= 2 && m[1] != "" {
		s = strings.TrimSpace(m[1])
	} else {
		s = strings.TrimSpace(m[0])
	}
	if s == "" {
		return nil
	}
	return &s
}

// regexMatch returns the full match, or nil.
func regexMatch(re *regexp.Regexp, text string) *string {
	m := re.FindString(text)
	if m == "" {
		return nil
	}
	s := strings.TrimSpace(m)
	if s == "" {
		return nil
	}
	return &s
}

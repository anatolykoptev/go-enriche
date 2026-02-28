package extract

import (
	"regexp"
	"strings"
)

// Pre-compiled regex patterns for Russian and English fact extraction.
var (
	reAddress = regexp.MustCompile(`(?i)(?:адрес|address)[:\s]+([^\n<]{5,100})`)
	rePhone   = regexp.MustCompile(`(?:\+7|8)[\s\-]?\(?\d{3}\)?[\s\-]?\d{3}[\s\-]?\d{2}[\s\-]?\d{2}`)
	rePrice   = regexp.MustCompile(`(?i)(?:цена|стоимость|price)[:\s]+([^\n<]{2,80})`)
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

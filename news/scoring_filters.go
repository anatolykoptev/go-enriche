package news

import (
	"net/url"
	"strings"
	"unicode"
)

// blockedDomains is the hardcoded set of domains that should never appear as news sources.
var blockedDomains = map[string]bool{
	"pinterest.com":  true,
	"pinterest.ru":   true,
	"facebook.com":   true,
	"instagram.com":  true,
	"twitter.com":    true,
	"x.com":          true,
	"vk.com":         true,
	"ok.ru":          true,
	"tiktok.com":     true,
	"youtube.com":    true,
	"reddit.com":     true,
	"telegram.org":   true,
	"t.me":           true,
	"wa.me":          true,
	"apple.com":      true,
	"google.com":     true,
	"yandex.ru":      true,
	"mail.ru":        true,
	"avito.ru":       true,
	"hh.ru":          true,
	"wildberries.ru": true,
	"ozon.ru":        true,
}

// containsWordCI checks whether text contains word as a whole word (UTF-8-aware boundaries).
// Both text and word are expected to already be lowercased for case-insensitive comparison.
func containsWordCI(text, word string) bool {
	if word == "" {
		return false
	}
	idx := 0
	for {
		pos := strings.Index(text[idx:], word)
		if pos < 0 {
			return false
		}
		abs := idx + pos

		// Check left boundary.
		leftOK := abs == 0 || !isWordChar([]rune(text[:abs])[len([]rune(text[:abs]))-1])
		// Check right boundary.
		after := abs + len(word)
		rightOK := after >= len(text) || !isWordCharByte(text[after:])

		if leftOK && rightOK {
			return true
		}
		idx = abs + 1
		if idx >= len(text) {
			return false
		}
	}
}

// isWordChar reports whether r is a letter or digit (word constituent).
func isWordChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// isWordCharByte reports whether the first rune of s is a word character.
func isWordCharByte(s string) bool {
	if s == "" {
		return false
	}
	r := rune(s[0])
	if r >= 0x80 { //nolint:mnd // UTF-8 multi-byte threshold
		// Multi-byte UTF-8 — decode properly.
		for _, decoded := range s {
			return isWordChar(decoded)
		}
	}
	return isWordChar(r)
}

// isBlockedDomain reports whether the URL belongs to a blocked domain.
func isBlockedDomain(rawURL string) bool {
	host := extractHost(rawURL)
	if host == "" {
		return false
	}
	host = strings.ToLower(host)
	if blockedDomains[host] {
		return true
	}
	// Check if host ends with a blocked domain (e.g. subdomain.vk.com).
	for blocked := range blockedDomains {
		if strings.HasSuffix(host, "."+blocked) {
			return true
		}
	}
	return false
}

// isHomepage reports whether the URL path is "/" or empty (i.e. a site root).
func isHomepage(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return u.Path == "" || u.Path == "/"
}

// isSelfReferencing reports whether the source domain matches the project key.
func isSelfReferencing(source, projectKey string) bool {
	if source == "" || projectKey == "" {
		return false
	}
	sourceLower := strings.ToLower(source)
	keyLower := strings.ToLower(projectKey)
	return strings.Contains(sourceLower, keyLower) || strings.Contains(keyLower, sourceLower)
}

// extractHost parses rawURL and returns the hostname without port.
func extractHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

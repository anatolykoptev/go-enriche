package enriche

import (
	"strings"
	"unicode/utf8"
)

// truncateRunes truncates s to at most maxRunes runes, preferring word boundaries.
// Returns s unchanged if maxRunes <= 0 or s is short enough.
func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 || utf8.RuneCountInString(s) <= maxRunes {
		return s
	}

	// Walk forward maxRunes runes to find the byte offset.
	byteOffset := 0
	for range maxRunes {
		_, size := utf8.DecodeRuneInString(s[byteOffset:])
		if size == 0 {
			break
		}
		byteOffset += size
	}

	truncated := s[:byteOffset]

	// Try to break at last space for cleaner output.
	if lastSpace := strings.LastIndexByte(truncated, ' '); lastSpace > len(truncated)/2 {
		truncated = truncated[:lastSpace]
	}

	return truncated
}

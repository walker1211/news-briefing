package fetcher

import (
	"strings"
	"unicode/utf8"
)

// truncateUTF8Bytes limits text by encoded byte length without splitting a rune.
// Invalid source bytes are replaced before truncation so the result is always
// safe to include in an AI prompt.
func truncateUTF8Bytes(text string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	text = strings.ToValidUTF8(text, "\uFFFD")
	if len(text) <= maxBytes {
		return text
	}
	end := maxBytes
	for end > 0 && !utf8.ValidString(text[:end]) {
		end--
	}
	return text[:end]
}

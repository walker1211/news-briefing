package fetcher

import (
	"bytes"
	"unicode/utf8"

	"github.com/mmcdole/gofeed"
)

// parseRSSFeed tolerates literal XML-forbidden C0 controls in otherwise readable
// RSS/Atom feeds. Keep ordinary XML whitespace, text and structural validation.
func parseRSSFeed(parser *gofeed.Parser, body []byte) (*gofeed.Feed, int, error) {
	cleaned, removed := sanitizeRSSXMLControlChars(body)
	feed, err := parser.Parse(bytes.NewReader(cleaned))
	return feed, removed, err
}

func sanitizeRSSXMLControlChars(body []byte) ([]byte, int) {
	removed := 0
	for _, b := range body {
		if isForbiddenXMLControl(b) {
			removed++
		}
	}
	if removed == 0 || !utf8.Valid(body) {
		return body, 0
	}

	// Do not strip bytes from JSON or an unrecognized encoding (notably UTF-16,
	// where NUL bytes are part of ordinary characters). Detection identifies the
	// XML root before parsing affected item descriptions.
	switch gofeed.DetectFeedType(bytes.NewReader(body)) {
	case gofeed.FeedTypeRSS, gofeed.FeedTypeAtom:
	default:
		return body, 0
	}

	cleaned := make([]byte, 0, len(body)-removed)
	for _, b := range body {
		if !isForbiddenXMLControl(b) {
			cleaned = append(cleaned, b)
		}
	}
	return cleaned, removed
}

func isForbiddenXMLControl(b byte) bool {
	return b < 0x20 && b != '\t' && b != '\n' && b != '\r'
}

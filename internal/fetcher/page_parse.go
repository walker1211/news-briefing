package fetcher

import (
	"fmt"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/model"
)

func parsePageSource(src config.Source, html string) (fetchedCandidate, bool, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return fetchedCandidate{}, false, fmt.Errorf("parse html: %w", err)
	}

	title := extractPageTitle(doc)
	summary := extractPageSummary(doc)
	combinedText := strings.Join([]string{title, summary, strings.TrimSpace(doc.Text())}, " ")
	matched := matchedKeywords(combinedText, src.Keywords)

	if !hasReleaseEvidence(combinedText) {
		return fetchedCandidate{}, false, nil
	}

	published, ok := selectPublishedTime(doc, src.TimeHint, hasStrongReleaseEvidence(combinedText))
	if !ok {
		return fetchedCandidate{}, false, nil
	}

	return fetchedCandidate{
		Article: model.Article{
			Title:     title,
			Link:      src.URL,
			Summary:   summary,
			Source:    src.Name,
			Category:  src.Category,
			Published: published,
		},
		MatchedKeywords: matched,
	}, true, nil
}

func extractPageTitle(doc *goquery.Document) string {
	if title := strings.TrimSpace(doc.Find("meta[property='og:title']").AttrOr("content", "")); title != "" {
		return title
	}
	if title := strings.TrimSpace(doc.Find("h1").First().Text()); title != "" {
		return title
	}
	return strings.TrimSpace(doc.Find("title").First().Text())
}

func extractPageSummary(doc *goquery.Document) string {
	if summary := strings.TrimSpace(doc.Find("meta[name='description']").AttrOr("content", "")); summary != "" {
		return summary
	}
	if summary := strings.TrimSpace(doc.Find("meta[property='og:description']").AttrOr("content", "")); summary != "" {
		return summary
	}
	if summary := strings.TrimSpace(doc.Find("article p").First().Text()); summary != "" {
		return summary
	}
	return strings.TrimSpace(doc.Find("p").First().Text())
}

func selectPublishedTime(doc *goquery.Document, timeHint string, allowUpdated bool) (time.Time, bool) {
	if strings.EqualFold(strings.TrimSpace(timeHint), "release published") {
		if t, ok := extractTimeNearLabels(doc, []string{"published", "release published", "released"}); ok {
			return t, true
		}
		if allowUpdated {
			if t, ok := extractTimeNearLabels(doc, []string{"updated", "modified"}); ok {
				return t, true
			}
			if t, ok := extractMetaTime(doc, []string{"article:modified_time", "og:updated_time"}); ok {
				return t, true
			}
		}
		return time.Time{}, false
	}

	if t, ok := extractTimeNearLabels(doc, []string{"published", "publication date", "posted"}); ok {
		return t, true
	}
	if t, ok := extractMetaTime(doc, []string{"article:published_time", "og:published_time"}); ok {
		return t, true
	}
	return time.Time{}, false
}

func extractTimeNearLabels(doc *goquery.Document, labels []string) (time.Time, bool) {
	var result time.Time
	var found bool

	doc.Find("time").EachWithBreak(func(i int, sel *goquery.Selection) bool {
		context := strings.ToLower(strings.Join([]string{
			strings.TrimSpace(sel.Parent().Text()),
			strings.TrimSpace(sel.Prev().Text()),
			strings.TrimSpace(sel.Parent().Prev().Text()),
		}, " "))
		for _, label := range labels {
			if !strings.Contains(context, strings.ToLower(label)) {
				continue
			}
			if parsed, ok := parseHTMLTime(sel.AttrOr("datetime", strings.TrimSpace(sel.Text()))); ok {
				result = parsed
				found = true
				return false
			}
		}
		return true
	})

	return result, found
}

func extractMetaTime(doc *goquery.Document, properties []string) (time.Time, bool) {
	for _, property := range properties {
		if raw := strings.TrimSpace(doc.Find(fmt.Sprintf("meta[property='%s']", property)).AttrOr("content", "")); raw != "" {
			if parsed, ok := parseHTMLTime(raw); ok {
				return parsed, true
			}
		}
	}
	return time.Time{}, false
}

func parseHTMLTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
	}
	for _, layout := range layouts {
		var (
			parsed time.Time
			err    error
		)
		if strings.Contains(layout, "Z07") || layout == time.RFC3339 {
			parsed, err = time.Parse(layout, raw)
		} else {
			parsed, err = time.ParseInLocation(layout, raw, time.UTC)
		}
		if err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

func hasReleaseEvidence(text string) bool {
	return hasAnyPhrase(text, []string{
		"released",
		"announced",
		"announcing",
		"introducing",
		"open-sourced",
		"open sourced",
		"now available",
		"launch",
	})
}

func hasStrongReleaseEvidence(text string) bool {
	return hasAnyPhrase(text, []string{
		"released",
		"announced",
		"announcing",
		"introducing",
		"open-sourced",
		"open sourced",
		"now available",
		"launch",
	})
}

func hasAnyPhrase(text string, phrases []string) bool {
	lower := strings.ToLower(text)
	for _, phrase := range phrases {
		if strings.Contains(lower, strings.ToLower(phrase)) {
			return true
		}
	}
	return false
}

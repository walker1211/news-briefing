package watch

import (
	"crypto/sha1"
	"fmt"
	"strings"
	"unicode"

	"github.com/PuerkitoBio/goquery"
	"github.com/walker1211/news-briefing/internal/model"
)

func parseAnthropicHome(html string, allowlist map[string]struct{}) ([]model.WatchIndexItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("parse anthropic home html: %w", err)
	}

	items := make([]model.WatchIndexItem, 0)
	doc.Find("a[href*='/collections/']").Each(func(i int, sel *goquery.Selection) {
		title := normalizeWatchTitle(sel.Find("[data-testid='collection-name']").First().Text())
		if title == "" {
			title = normalizeWatchTitle(sel.Find("h3").First().Text())
		}
		if title == "" {
			title = normalizeWatchTitle(sel.Contents().Not("span").Text())
		}
		if title == "" {
			title = normalizeWatchTitle(sel.Text())
		}
		if _, ok := allowlist[title]; !ok {
			return
		}
		url := absoluteSupportURL(strings.TrimSpace(sel.AttrOr("href", "")))
		updatedText := ""
		sel.Find("span, p").Each(func(_ int, node *goquery.Selection) {
			if updatedText != "" {
				return
			}
			text := normalizeWatchText(node.Text())
			if strings.Contains(text, "篇文章") {
				updatedText = text
			}
		})
		items = append(items, model.WatchIndexItem{
			Title:       title,
			URL:         url,
			Position:    len(items) + 1,
			UpdatedText: updatedText,
			ItemHash:    hashWatchFields(title, url, updatedText),
		})
	})

	return items, nil
}

func parseAnthropicCategory(category string, url string, html string) (model.WatchIndexSnapshot, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return model.WatchIndexSnapshot{}, fmt.Errorf("parse anthropic category html: %w", err)
	}

	items := make([]model.WatchIndexItem, 0)
	doc.Find("a[href*='/articles/']").Each(func(i int, sel *goquery.Selection) {
		title := normalizeWatchTitle(sel.Text())
		if title == "" {
			return
		}
		articleURL := absoluteSupportURL(strings.TrimSpace(sel.AttrOr("href", "")))
		items = append(items, model.WatchIndexItem{
			Title:    title,
			URL:      articleURL,
			Position: len(items) + 1,
			ItemHash: hashWatchFields(title, articleURL, fmt.Sprintf("%d", len(items)+1)),
		})
	})

	return model.WatchIndexSnapshot{
		Scope:     "category",
		Source:    "Anthropic Claude Support",
		Category:  category,
		URL:       url,
		ItemCount: len(items),
		Items:     items,
		Hash:      hashSnapshotItems(items),
	}, nil
}

func parseAnthropicArticle(html string) (title string, summary string, body string, err error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return "", "", "", fmt.Errorf("parse anthropic article html: %w", err)
	}

	title = normalizeWatchText(doc.Find("article h1").First().Text())
	if title == "" {
		title = normalizeWatchText(doc.Find("title").First().Text())
	}
	summary = normalizeWatchText(doc.Find("meta[name='description']").AttrOr("content", ""))
	if summary == "" {
		summary = normalizeWatchText(doc.Find("article p").First().Text())
	}

	paragraphs := make([]string, 0)
	doc.Find("article p").Each(func(i int, sel *goquery.Selection) {
		text := normalizeWatchText(sel.Text())
		if text == "" {
			return
		}
		paragraphs = append(paragraphs, text)
	})
	body = strings.Join(paragraphs, " ")
	body = normalizeWatchText(body)
	return title, summary, body, nil
}

func normalizeWatchText(value string) string {
	fields := strings.Fields(strings.TrimSpace(value))
	return strings.Join(fields, " ")
}

func normalizeWatchTitle(value string) string {
	text := normalizeWatchText(value)
	if text == "" {
		return ""
	}
	var b strings.Builder
	lastSpace := false
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastSpace = false
			continue
		}
		if unicode.IsSpace(r) && !lastSpace {
			b.WriteRune(' ')
			lastSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

func absoluteSupportURL(href string) string {
	const base = "https://support.claude.com"
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}
	return base + href
}

func hashWatchFields(parts ...string) string {
	h := sha1.New()
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte("\n"))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func hashSnapshotItems(items []model.WatchIndexItem) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, item.ItemHash)
	}
	return hashWatchFields(parts...)
}

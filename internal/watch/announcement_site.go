package watch

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/model"
)

func runAnnouncementSite(ctx context.Context, site config.WatchSite, now time.Time, indexState IndexState, articleState ArticleState, fetchHTML fetchHTMLFunc) ([]model.Article, []model.WatchSeenArticle, []model.WatchEvent, error) {
	type seenPayload struct {
		summary string
		body    string
	}

	homeHTML, err := fetchHTML(ctx, site.HomeURL)
	if err != nil {
		return nil, nil, nil, err
	}
	current, err := parseAnthropicAnnouncementIndex(site.Name, site.HomeURL, homeHTML)
	if err != nil {
		return nil, nil, nil, err
	}
	current.Source = site.Name
	current.Category = site.Name
	current.SnapshotAt = now

	stateKey := watchCategoryStateKey(site.Name, site.Name)
	prevSnapshot, hasPrev := indexState.Categories[stateKey]
	if !hasPrev {
		for _, item := range current.Items {
			articleHTML, err := fetchHTML(ctx, item.URL)
			if err != nil {
				return nil, nil, nil, err
			}
			title, summary, body, err := parseAnnouncementArticleFromURL(item.URL, articleHTML)
			if err != nil {
				return nil, nil, nil, err
			}
			articleState[item.URL] = model.WatchArticleState{
				URL:           item.URL,
				Title:         title,
				SummaryHash:   hashWatchContent(summary),
				BodyHash:      hashWatchContent(body),
				LastCheckedAt: now,
				LastChangedAt: now,
			}
		}
		indexState.Categories[stateKey] = current
		return nil, nil, nil, nil
	}

	prev := &prevSnapshot
	categoryEvents, changedURLs := diffCategorySnapshots(prev, current)
	for i := range categoryEvents {
		categoryEvents[i].Source = site.Name
		categoryEvents[i].DetectedAt = now
		if categoryEvents[i].Reason == "" {
			categoryEvents[i].Reason = defaultWatchReason(categoryEvents[i].EventType, categoryEvents[i].ArticleTitle)
		}
		if containsString(changedURLs, categoryEvents[i].ArticleURL) {
			continue
		}
		applyWatchEventPriority(&categoryEvents[i])
	}

	seenPayloads := make(map[string]seenPayload)
	for _, item := range current.Items {
		if containsString(changedURLs, item.URL) {
			continue
		}
		state, ok := articleState[item.URL]
		articleHTML, err := fetchHTML(ctx, item.URL)
		if err != nil {
			return nil, nil, nil, err
		}
		title, summary, body, err := parseAnnouncementArticleFromURL(item.URL, articleHTML)
		if err != nil {
			return nil, nil, nil, err
		}
		summaryHash := hashWatchContent(summary)
		bodyHash := hashWatchContent(body)
		if !ok {
			articleState[item.URL] = model.WatchArticleState{
				URL:           item.URL,
				Title:         title,
				SummaryHash:   summaryHash,
				BodyHash:      bodyHash,
				LastCheckedAt: now,
				LastChangedAt: now,
			}
			continue
		}
		if state.Title != title || state.SummaryHash != summaryHash || state.BodyHash != bodyHash {
			event := model.WatchEvent{
				EventType:       "content_changed",
				Source:          site.Name,
				Category:        site.Name,
				ArticleURL:      item.URL,
				ArticleTitle:    title,
				DetectedAt:      now,
				BodyFetched:     true,
				ContentChanged:  true,
				Reason:          "正文发生变化",
				MatchedKeywords: matchedWatchKeywords(title+" "+summary+" "+body, site.HighValueKeywords),
			}
			applyWatchEventPriority(&event)
			categoryEvents = append(categoryEvents, event)
			seenPayloads[item.URL] = seenPayload{summary: summary, body: body}
			articleState[item.URL] = model.WatchArticleState{
				URL:           item.URL,
				Title:         title,
				SummaryHash:   summaryHash,
				BodyHash:      bodyHash,
				LastCheckedAt: now,
				LastChangedAt: now,
			}
			continue
		}
		state.LastCheckedAt = now
		articleState[item.URL] = state
	}

	for _, url := range changedURLs {
		matchedIndex := -1
		for i := range categoryEvents {
			if categoryEvents[i].ArticleURL == url {
				matchedIndex = i
				break
			}
		}
		if matchedIndex == -1 || categoryEvents[matchedIndex].EventType == "removed_article" {
			continue
		}

		articleHTML, err := fetchHTML(ctx, url)
		if err != nil {
			return nil, nil, nil, err
		}
		title, summary, body, err := parseAnnouncementArticleFromURL(url, articleHTML)
		if err != nil {
			return nil, nil, nil, err
		}
		articleState[url] = model.WatchArticleState{
			URL:           url,
			Title:         title,
			SummaryHash:   hashWatchContent(summary),
			BodyHash:      hashWatchContent(body),
			LastCheckedAt: now,
			LastChangedAt: now,
		}
		if title != "" {
			categoryEvents[matchedIndex].ArticleTitle = title
		}
		categoryEvents[matchedIndex].BodyFetched = true
		categoryEvents[matchedIndex].MatchedKeywords = matchedWatchKeywords(title+" "+summary+" "+body, site.HighValueKeywords)
		if categoryEvents[matchedIndex].Reason == "" {
			categoryEvents[matchedIndex].Reason = defaultWatchReason(categoryEvents[matchedIndex].EventType, categoryEvents[matchedIndex].ArticleTitle)
		}
		applyWatchEventPriority(&categoryEvents[matchedIndex])
		seenPayloads[url] = seenPayload{summary: summary, body: body}
	}

	indexState.Categories[stateKey] = current
	articles := make([]model.Article, 0)
	seenItems := make([]model.WatchSeenArticle, 0)
	for _, event := range categoryEvents {
		if event.EventType == "removed_article" && event.ArticleURL != "" {
			delete(articleState, event.ArticleURL)
		}
		if !event.IncludeInBriefing {
			continue
		}
		articles = append(articles, watchEventToArticle(site, event))
		payload, ok := seenPayloads[event.ArticleURL]
		if !ok {
			articleHTML, err := fetchHTML(ctx, event.ArticleURL)
			if err != nil {
				return nil, nil, nil, err
			}
			_, summary, body, err := parseAnnouncementArticleFromURL(event.ArticleURL, articleHTML)
			if err != nil {
				return nil, nil, nil, err
			}
			payload = seenPayload{summary: summary, body: body}
		}
		seenItems = append(seenItems, watchEventToSeenArticle(site, event, payload.summary, payload.body))
	}

	return articles, seenItems, categoryEvents, nil
}

func parseAnthropicAnnouncementIndex(source string, url string, html string) (model.WatchIndexSnapshot, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return model.WatchIndexSnapshot{}, fmt.Errorf("parse announcement index html: %w", err)
	}

	var items []model.WatchIndexItem
	if isClaudeReleaseNotesOverviewURL(url) {
		items = parseClaudeReleaseNotesOverview(doc, url)
	} else {
		items = parseAnnouncementLinkIndex(doc, url)
	}

	return model.WatchIndexSnapshot{
		Scope:     "category",
		Source:    source,
		Category:  source,
		URL:       url,
		ItemCount: len(items),
		Items:     items,
		Hash:      hashSnapshotItems(items),
	}, nil
}

func parseAnnouncementLinkIndex(doc *goquery.Document, pageURL string) []model.WatchIndexItem {
	items := make([]model.WatchIndexItem, 0)
	seen := make(map[string]struct{})
	doc.Find("a[href]").Each(func(i int, sel *goquery.Selection) {
		href := strings.TrimSpace(sel.AttrOr("href", ""))
		if href == "" {
			return
		}
		articleURL := absoluteAnnouncementURL(pageURL, href)
		if articleURL == "" {
			return
		}
		if _, ok := seen[articleURL]; ok {
			return
		}

		title := normalizeWatchText(sel.Find("h1, h2, h3").First().Text())
		if title == "" {
			title = normalizeWatchText(sel.Text())
		}
		if title == "" {
			return
		}

		snippet := ""
		sel.Find("p").Each(func(_ int, p *goquery.Selection) {
			if snippet != "" {
				return
			}
			text := normalizeWatchText(p.Text())
			if text != "" {
				snippet = text
			}
		})

		seen[articleURL] = struct{}{}
		items = append(items, model.WatchIndexItem{
			Title:    title,
			URL:      articleURL,
			Position: len(items) + 1,
			Snippet:  snippet,
			ItemHash: hashWatchFields(title, articleURL, snippet),
		})
	})
	return items
}

func parseClaudeReleaseNotesOverview(doc *goquery.Document, pageURL string) []model.WatchIndexItem {
	items := make([]model.WatchIndexItem, 0)
	doc.Find("h3 div[id]").Each(func(i int, heading *goquery.Selection) {
		fragment := strings.TrimSpace(heading.AttrOr("id", ""))
		if fragment == "" {
			return
		}
		entryURL := releaseNotesOverviewURLWithFragment(pageURL, fragment)
		if entryURL == "" {
			return
		}
		content := releaseNotesOverviewContent(heading)
		title := releaseNotesOverviewTitle(content)
		if title == "" {
			return
		}
		snippet := normalizeWatchText(content.Find("li, p").First().Text())
		items = append(items, model.WatchIndexItem{
			Title:    title,
			URL:      entryURL,
			Position: len(items) + 1,
			Snippet:  snippet,
			ItemHash: hashWatchFields(title, entryURL, snippet),
		})
	})
	if len(items) > 0 {
		return items
	}
	return parseClaudeReleaseNotesOverviewLegacy(doc, pageURL)
}

func parseClaudeReleaseNotesOverviewLegacy(doc *goquery.Document, pageURL string) []model.WatchIndexItem {
	items := make([]model.WatchIndexItem, 0)
	seen := make(map[string]struct{})
	doc.Find("a[href]").Each(func(i int, anchor *goquery.Selection) {
		href := strings.TrimSpace(anchor.AttrOr("href", ""))
		entryURL := releaseNotesOverviewEntryURL(pageURL, href)
		if entryURL == "" {
			return
		}
		if _, ok := seen[entryURL]; ok {
			return
		}
		if anchor.ParentsFiltered("nav").Length() > 0 {
			return
		}
		title := normalizeWatchText(anchor.Text())
		if title == "" {
			return
		}
		container := anchor.Parent()
		snippet := normalizeWatchText(container.Find("p, li").First().Text())
		seen[entryURL] = struct{}{}
		items = append(items, model.WatchIndexItem{
			Title:    title,
			URL:      entryURL,
			Position: len(items) + 1,
			Snippet:  snippet,
			ItemHash: hashWatchFields(title, entryURL, snippet),
		})
	})
	return items
}

func isClaudeReleaseNotesOverviewURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return parsed.Host == "platform.claude.com" && parsed.Path == "/docs/en/release-notes/overview"
}

func releaseNotesOverviewEntryURL(pageURL string, href string) string {
	if href == "" {
		return ""
	}
	base, err := url.Parse(pageURL)
	if err != nil {
		return ""
	}
	reference, err := url.Parse(href)
	if err != nil {
		return ""
	}
	resolved := base.ResolveReference(reference)
	if resolved.Host != "platform.claude.com" {
		return ""
	}

	fragment := strings.TrimSpace(resolved.Fragment)
	if fragment == "" {
		slug := strings.TrimPrefix(resolved.Path, "/docs/en/release-notes/")
		if slug != resolved.Path {
			fragment = strings.Trim(slug, "/")
		}
	}
	if fragment == "" {
		fragment = strings.Trim(strings.TrimPrefix(strings.TrimSpace(href), "#"), "/")
	}
	if fragment == "" {
		fragment = strings.Trim(strings.TrimPrefix(strings.TrimSpace(reference.Path), "/"), "/")
	}
	if fragment == "" || strings.Contains(fragment, "/") || fragment == "api" || fragment == "overview" {
		return ""
	}

	resolved.RawQuery = ""
	resolved.Path = "/docs/en/release-notes/overview"
	resolved.Fragment = fragment
	return resolved.String()
}

func parseAnnouncementArticleFromURL(rawURL string, html string) (title string, summary string, body string, err error) {
	if isClaudeReleaseNotesOverviewEntryURL(rawURL) {
		return parseClaudeReleaseNotesOverviewArticle(rawURL, html)
	}
	return parseAnthropicAnnouncementArticle(html)
}

func parseAnthropicAnnouncementArticle(html string) (title string, summary string, body string, err error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return "", "", "", fmt.Errorf("parse announcement article html: %w", err)
	}

	title = normalizeWatchText(doc.Find("article h1").First().Text())
	if title == "" {
		title = normalizeWatchText(doc.Find("h1").First().Text())
	}
	if title == "" {
		title = normalizeWatchText(doc.Find("title").First().Text())
	}

	summary = normalizeWatchText(doc.Find("meta[name='description']").AttrOr("content", ""))
	if summary == "" {
		summary = normalizeWatchText(doc.Find("article p").First().Text())
	}
	if summary == "" {
		summary = normalizeWatchText(doc.Find("p").First().Text())
	}

	paragraphs := make([]string, 0)
	doc.Find("article p").Each(func(i int, sel *goquery.Selection) {
		text := normalizeWatchText(sel.Text())
		if text == "" {
			return
		}
		paragraphs = append(paragraphs, text)
	})
	if len(paragraphs) == 0 {
		doc.Find("p").Each(func(i int, sel *goquery.Selection) {
			text := normalizeWatchText(sel.Text())
			if text == "" {
				return
			}
			paragraphs = append(paragraphs, text)
		})
	}
	body = normalizeWatchText(strings.Join(paragraphs, " "))
	return title, summary, body, nil
}

func isClaudeReleaseNotesOverviewEntryURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return parsed.Host == "platform.claude.com" && parsed.Path == "/docs/en/release-notes/overview" && strings.TrimSpace(parsed.Fragment) != ""
}

func parseClaudeReleaseNotesOverviewArticle(rawURL string, html string) (title string, summary string, body string, err error) {
	fragment := releaseNotesOverviewFragment(rawURL)
	if fragment == "" {
		return "", "", "", fmt.Errorf("missing release notes fragment")
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return "", "", "", fmt.Errorf("parse announcement article html: %w", err)
	}

	heading := doc.Find("#" + fragment).First()
	if heading.Length() != 0 && goquery.NodeName(heading) == "div" && heading.Parent().Is("h3") {
		content := releaseNotesOverviewContent(heading)
		title = releaseNotesOverviewTitle(content)
		if title == "" {
			title = normalizeWatchText(heading.Text())
		}

		parts := make([]string, 0)
		content.Find("li, p").Each(func(i int, sel *goquery.Selection) {
			text := normalizeWatchText(sel.Text())
			if text == "" {
				return
			}
			parts = append(parts, text)
		})
		if len(parts) > 0 {
			summary = parts[0]
			body = normalizeWatchText(strings.Join(parts, " "))
		}
		return title, summary, body, nil
	}

	legacy := doc.Find("#" + fragment).First()
	if legacy.Length() == 0 {
		return "", "", "", fmt.Errorf("release notes fragment %q not found", fragment)
	}
	title = normalizeWatchText(legacy.ChildrenFiltered("a[href]").First().Text())
	if title == "" {
		title = normalizeWatchText(legacy.Find("a[href]").First().Text())
	}
	if title == "" {
		title = normalizeWatchText(legacy.Find("h1, h2, h3").First().Text())
	}
	if title == "" {
		title = normalizeWatchText(legacy.Text())
	}

	parts := make([]string, 0)
	legacy.Find("p, li").Each(func(i int, sel *goquery.Selection) {
		text := normalizeWatchText(sel.Text())
		if text == "" {
			return
		}
		parts = append(parts, text)
	})
	if len(parts) > 0 {
		summary = parts[0]
		body = normalizeWatchText(strings.Join(parts, " "))
	}
	return title, summary, body, nil
}

func releaseNotesOverviewURLWithFragment(pageURL string, fragment string) string {
	if fragment == "" {
		return ""
	}
	base, err := url.Parse(pageURL)
	if err != nil {
		return ""
	}
	base.RawQuery = ""
	base.Fragment = fragment
	return base.String()
}

func releaseNotesOverviewContent(heading *goquery.Selection) *goquery.Selection {
	content := heading.Parent().Next()
	for content.Length() > 0 && goquery.NodeName(content) != "ul" && goquery.NodeName(content) != "p" {
		content = content.Next()
	}
	return content
}

func releaseNotesOverviewTitle(content *goquery.Selection) string {
	if content.Length() == 0 {
		return ""
	}
	firstItem := content.Find("li").First()
	if firstItem.Length() == 0 {
		firstItem = content
	}
	text := normalizeWatchText(firstItem.Text())
	if strings.HasPrefix(text, "We've launched ") {
		if anchor := firstItem.Find("a[href]").First(); anchor.Length() > 0 {
			return "We've launched " + normalizeWatchText(anchor.Text())
		}
	}
	return text
}

func releaseNotesOverviewFragment(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Fragment)
}

func absoluteAnnouncementURL(baseURL string, href string) string {
	if href == "" {
		return ""
	}

	base, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	reference, err := url.Parse(href)
	if err != nil {
		return ""
	}
	resolved := base.ResolveReference(reference)

	switch base.Host {
	case "www.anthropic.com":
		if resolved.Host != "www.anthropic.com" || !strings.HasPrefix(resolved.Path, "/news/") {
			return ""
		}
	case "platform.claude.com":
		if resolved.Host != "platform.claude.com" || !strings.HasPrefix(resolved.Path, "/docs/en/release-notes/") {
			return ""
		}
		slug := strings.TrimPrefix(resolved.Path, "/docs/en/release-notes/")
		if slug == "" || slug == "api" || slug == "overview" || strings.Contains(slug, "/") {
			return ""
		}
	default:
		return ""
	}

	resolved.RawQuery = ""
	resolved.Fragment = ""
	return resolved.String()
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

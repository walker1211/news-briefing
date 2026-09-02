package watch

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/model"
)

var claudeReleaseNotesDateFragmentPattern = regexp.MustCompile(`(?i)^(january|february|march|april|may|june|july|august|september|october|november|december)-[0-9]{1,2}-[0-9]{4}$`)
var announcementPublishedAtPattern = regexp.MustCompile(`(?i)"(?:publishedOn|datePublished)"\s*:\s*"([^"\\]+)"`)
var announcementVisibleDatePattern = regexp.MustCompile(`(?i)(?:^|[^A-Za-z])((?:jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|jun(?:e)?|jul(?:y)?|aug(?:ust)?|sep(?:tember)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?)\s+[0-9]{1,2},\s+[0-9]{4}\b)`)

const staleClaudeReleaseNotesEntryDays = 60

// anthropicAnnouncementContentHashVersion tracks the body-selection contract
// for Anthropic News pages. Increment it when the canonical article scope
// changes so old state can be rehashed without emitting a false event.
const anthropicAnnouncementContentHashVersion = 2

var claudeReleaseNotesMonthByName = map[string]time.Month{
	"january":   time.January,
	"february":  time.February,
	"march":     time.March,
	"april":     time.April,
	"may":       time.May,
	"june":      time.June,
	"july":      time.July,
	"august":    time.August,
	"september": time.September,
	"october":   time.October,
	"november":  time.November,
	"december":  time.December,
}

func runAnnouncementSite(ctx context.Context, site config.WatchSite, now time.Time, indexState IndexState, articleState ArticleState, fetchHTML fetchHTMLFunc, articleConcurrency int, deepVerifyInterval time.Duration, deepVerifyBatchSize int) ([]model.Article, []model.WatchSeenArticle, []model.WatchEvent, error) {
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
	if hasPrev && isClaudeReleaseNotesOverviewURL(site.HomeURL) {
		indexState.Categories[stateKey] = normalizeClaudeReleaseNotesSnapshotURLs(prevSnapshot, site.HomeURL)
		migrateClaudeReleaseNotesArticleState(articleState, site.HomeURL)
	}

	fetchContent := func(ctx context.Context, url string) (watchArticleContent, error) {
		articleHTML, err := fetchHTML(ctx, url)
		if err != nil {
			return watchArticleContent{}, err
		}
		title, summary, body, err := parseAnnouncementArticleFromURL(url, articleHTML)
		if err != nil {
			return watchArticleContent{}, err
		}
		publishedAt := time.Time{}
		contentHashVersion := 0
		if !isClaudeReleaseNotesOverviewEntryURL(url) {
			publishedAt = parseAnnouncementPublishedAt(articleHTML)
			contentHashVersion = anthropicAnnouncementContentHashVersion
		}
		return watchArticleContent{title: title, summary: summary, body: body, publishedAt: publishedAt, contentHashVersion: contentHashVersion}, nil
	}

	return runWatchCategory(ctx, watchCategoryRun{
		site:                site,
		now:                 now,
		stateKey:            stateKey,
		current:             current,
		indexState:          indexState,
		articleState:        articleState,
		fetchContent:        fetchContent,
		articleConcurrency:  articleConcurrency,
		deepVerifyInterval:  deepVerifyInterval,
		deepVerifyBatchSize: deepVerifyBatchSize,
	})
}

func parseAnthropicAnnouncementIndex(source string, url string, html string) (model.WatchIndexSnapshot, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return model.WatchIndexSnapshot{}, fmt.Errorf("parse announcement index html: %w", err)
	}

	var items []model.WatchIndexItem
	if isClaudeReleaseNotesOverviewURL(url) {
		if isClaudeAppUnavailablePage(doc) {
			return model.WatchIndexSnapshot{}, fmt.Errorf("release notes page unavailable: redirected to Claude app unavailable in region page")
		}
		items = parseClaudeReleaseNotesOverview(doc, url)
		if len(items) == 0 {
			return model.WatchIndexSnapshot{}, fmt.Errorf("release notes date anchors not found")
		}
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
	seen := make(map[string]struct{})
	// The docs site has used both an ID on the heading and an ID on a
	// nested div. Keep the date-only contract for both layouts.
	doc.Find("h3[id], h3 div[id]").Each(func(i int, heading *goquery.Selection) {
		fragment := strings.TrimSpace(heading.AttrOr("id", ""))
		if fragment == "" || !isClaudeReleaseNotesDateFragment(fragment) {
			return
		}
		if _, ok := seen[fragment]; ok {
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
		snippet := normalizeWatchText(releaseNotesOverviewParagraphs(content).First().Text())
		seen[fragment] = struct{}{}
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

func isClaudeReleaseNotesDateFragment(fragment string) bool {
	return claudeReleaseNotesDateFragmentPattern.MatchString(strings.TrimSpace(fragment))
}

func isStaleClaudeReleaseNotesOverviewEntry(rawURL string, now time.Time) bool {
	if !isClaudeReleaseNotesOverviewEntryURL(rawURL) || now.IsZero() {
		return false
	}
	entryDate, ok := claudeReleaseNotesDateFromFragment(releaseNotesOverviewFragment(rawURL), now.Location())
	if !ok {
		return false
	}
	nowDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return entryDate.Before(nowDate.AddDate(0, 0, -staleClaudeReleaseNotesEntryDays))
}

func claudeReleaseNotesDateFromFragment(fragment string, loc *time.Location) (time.Time, bool) {
	fragment = strings.ToLower(strings.TrimSpace(fragment))
	if !isClaudeReleaseNotesDateFragment(fragment) {
		return time.Time{}, false
	}
	parts := strings.Split(fragment, "-")
	if len(parts) != 3 {
		return time.Time{}, false
	}
	month, ok := claudeReleaseNotesMonthByName[parts[0]]
	if !ok {
		return time.Time{}, false
	}
	day, err := strconv.Atoi(parts[1])
	if err != nil {
		return time.Time{}, false
	}
	year, err := strconv.Atoi(parts[2])
	if err != nil {
		return time.Time{}, false
	}
	if loc == nil {
		loc = time.UTC
	}
	date := time.Date(year, month, day, 0, 0, 0, 0, loc)
	if date.Year() != year || date.Month() != month || date.Day() != day {
		return time.Time{}, false
	}
	return date, true
}

func normalizeClaudeReleaseNotesSnapshotURLs(snapshot model.WatchIndexSnapshot, pageURL string) model.WatchIndexSnapshot {
	for i := range snapshot.Items {
		normalizedURL := normalizeClaudeReleaseNotesEntryURL(snapshot.Items[i].URL, pageURL)
		if normalizedURL == snapshot.Items[i].URL {
			continue
		}
		snapshot.Items[i].URL = normalizedURL
		snapshot.Items[i].ItemHash = hashWatchFields(snapshot.Items[i].Title, normalizedURL, snapshot.Items[i].Snippet)
	}
	snapshot.Hash = hashSnapshotItems(snapshot.Items)
	return snapshot
}

func migrateClaudeReleaseNotesArticleState(articleState ArticleState, pageURL string) {
	for rawURL, state := range articleState {
		normalizedURL := normalizeClaudeReleaseNotesEntryURL(rawURL, pageURL)
		if normalizedURL == rawURL {
			continue
		}
		if _, exists := articleState[normalizedURL]; !exists {
			state.URL = normalizedURL
			articleState[normalizedURL] = state
		}
		delete(articleState, rawURL)
	}
}

func normalizeClaudeReleaseNotesEntryURL(rawURL string, pageURL string) string {
	if !isClaudeReleaseNotesOverviewEntryURL(rawURL) {
		return rawURL
	}
	fragment := releaseNotesOverviewFragment(rawURL)
	if fragment == "" {
		return rawURL
	}
	normalizedURL := releaseNotesOverviewURLWithFragment(pageURL, fragment)
	if normalizedURL == "" {
		return rawURL
	}
	return normalizedURL
}

func isClaudeReleaseNotesOverviewURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return isClaudeReleaseNotesOverview(parsed)
}

func isClaudeReleaseNotesOverview(parsed *url.URL) bool {
	switch parsed.Host {
	case "platform.claude.com":
		return parsed.Path == "/docs/en/release-notes/overview"
	case "docs.claude.com", "docs.anthropic.com":
		return parsed.Path == "/en/release-notes/overview"
	default:
		return false
	}
}

func isClaudeAppUnavailableHTML(html string) bool {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return false
	}
	return isClaudeAppUnavailablePage(doc)
}

func isClaudeAppUnavailablePage(doc *goquery.Document) bool {
	text := strings.ToLower(normalizeWatchText(doc.Find("title, h1").Text()))
	return strings.Contains(text, "app unavailable in region")
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

	hero := anthropicAnnouncementHeroArticle(doc)
	canonical := anthropicAnnouncementCanonicalArticle(doc, hero)
	title = normalizeWatchText(hero.Find("h1").First().Text())
	if title == "" {
		title = normalizeWatchText(doc.Find("article h1").First().Text())
	}
	if title == "" {
		title = normalizeWatchText(doc.Find("h1").First().Text())
	}
	if title == "" {
		title = normalizeWatchText(doc.Find("title").First().Text())
	}

	summary = normalizeWatchText(doc.Find("meta[name='description']").AttrOr("content", ""))
	if summary == "" {
		summary = normalizeWatchText(canonical.Find("p").First().Text())
	}
	if summary == "" {
		summary = normalizeWatchText(doc.Find("p").First().Text())
	}

	paragraphs := make([]string, 0)
	canonical.Find("p").Each(func(i int, sel *goquery.Selection) {
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

func anthropicAnnouncementHeroArticle(doc *goquery.Document) *goquery.Selection {
	hero := doc.Find("main#main-content > article").First()
	if hero.Length() == 0 {
		hero = doc.Find("article").First()
	}
	return hero
}

func anthropicAnnouncementCanonicalArticle(doc *goquery.Document, hero *goquery.Selection) *goquery.Selection {
	canonical := doc.Find("main#main-content > article > div.page-wrapper > article").First()
	if canonical.Length() == 0 {
		canonical = doc.Find("main#main-content > article article").First()
	}
	if canonical.Length() != 0 {
		return canonical
	}
	if hero != nil && hero.Length() != 0 {
		canonical = hero.Find("article").First()
		if canonical.Length() != 0 {
			return canonical
		}
		return hero
	}
	return doc.Find("article").First()
}

func parseAnnouncementPublishedAt(html string) time.Time {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err == nil {
		selectors := []string{
			`meta[property='article:published_time']`,
			`meta[name='article:published_time']`,
			`time[datetime]`,
		}
		for _, selector := range selectors {
			selection := doc.Find(selector).First()
			value := selection.AttrOr("content", "")
			if value == "" {
				value = selection.AttrOr("datetime", "")
			}
			if publishedAt, ok := parseAnnouncementTime(value); ok {
				return publishedAt
			}
		}
	}

	// Anthropic news pages currently serialize the canonical post as escaped
	// Next.js data. The primary post appears before relatedPosts, so the first
	// publishedOn value is the article's own publication time.
	normalized := strings.ReplaceAll(html, `\"`, `"`)
	match := announcementPublishedAtPattern.FindStringSubmatch(normalized)
	if len(match) == 2 {
		if publishedAt, ok := parseAnnouncementTime(match[1]); ok {
			return publishedAt
		}
	}

	if publishedAt, ok := parseAnnouncementVisiblePublishedAt(doc); ok {
		return publishedAt
	}
	return time.Time{}
}

func parseAnnouncementVisiblePublishedAt(doc *goquery.Document) (time.Time, bool) {
	if doc == nil {
		return time.Time{}, false
	}
	hero := anthropicAnnouncementHeroArticle(doc)
	if hero.Length() == 0 {
		return time.Time{}, false
	}
	heading := hero.Find("h1").First()
	if heading.Length() == 0 {
		return time.Time{}, false
	}

	// The publication date is rendered next to the canonical title in the
	// hero header. Walk only from that h1 to its containing hero article; this
	// prevents dates in body changelogs and related-content cards from winning.
	for scope := heading; scope.Length() > 0; scope = scope.Parent() {
		match := announcementVisibleDatePattern.FindStringSubmatch(scope.Text())
		if len(match) == 2 {
			if publishedAt, ok := parseAnnouncementTime(match[1]); ok {
				return publishedAt, true
			}
		}
		if scope.Is("article") && scope.IsSelection(hero) {
			break
		}
	}
	return time.Time{}, false
}

func parseAnnouncementTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "Jan 2, 2006", "January 2, 2006"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func isClaudeReleaseNotesOverviewEntryURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return isClaudeReleaseNotesOverview(parsed) && strings.TrimSpace(parsed.Fragment) != ""
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
	if heading.Is("h3") || (heading.Is("div") && heading.Closest("h3").Length() != 0) {
		content := releaseNotesOverviewContent(heading)
		title = releaseNotesOverviewTitle(content)
		if title == "" {
			title = normalizeWatchText(heading.Text())
		}

		parts := make([]string, 0)
		releaseNotesOverviewParagraphs(content).Each(func(i int, sel *goquery.Selection) {
			text := normalizeWatchText(sel.Text())
			if text == "" {
				return
			}
			parts = append(parts, text)
		})
		if len(parts) == 0 {
			return "", "", "", fmt.Errorf("release notes entry content not found")
		}
		summary = parts[0]
		body = normalizeWatchText(strings.Join(parts, " "))
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
	if !heading.Is("h3") {
		heading = heading.Closest("h3")
	}
	// Preserve the existing first-content-block contract so a layout-only
	// migration does not change every historical entry's content hash.
	for content := heading.Next(); content.Length() > 0; content = content.Next() {
		if content.Is("h1, h2, h3") {
			break
		}
		if content.Is("ul, p") {
			return content
		}
	}
	return heading.Slice(0, 0)
}

func releaseNotesOverviewParagraphs(content *goquery.Selection) *goquery.Selection {
	if content.Is("p") {
		return content
	}
	return content.Find("li, p")
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

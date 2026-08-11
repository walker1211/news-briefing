package fetcher

import (
	"bytes"
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/mmcdole/gofeed"
	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/imageutil"
	"github.com/walker1211/news-briefing/internal/model"
)

var rssImageSourcePattern = regexp.MustCompile(`(?i)<img[^>]+src=["']([^"']+)["']`)

var fetchFeedWithCurlContext = func(ctx context.Context, url string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "curl",
		"-sS",
		"-L",
		"--max-time", "30",
		"-A", userAgent,
		url,
	)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return output, nil
}

func FetchRSS(source config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
	return FetchRSSContext(context.Background(), source, keywords, since)
}

func (c *Client) FetchRSS(source config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
	return c.FetchRSSContext(context.Background(), source, keywords, since)
}

func FetchRSSContext(ctx context.Context, source config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
	return NewClient(HTTPClient()).FetchRSSContext(ctx, source, keywords, since)
}

func (c *Client) FetchRSSContext(ctx context.Context, source config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
	return c.fetchRSSContextWithOpenGraphOptions(ctx, source, keywords, since, sleepContext, randomRedditDelay)
}

func (c *Client) fetchRSSContextWithOpenGraphOptions(ctx context.Context, source config.Source, keywords []string, since time.Time, sleep sleepFunc, delay delayFunc) (sourceFetchResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	fetchSource := source
	authenticatedURL, err := authenticatedRSSURL(source)
	if err != nil {
		return sourceFetchResult{}, err
	}
	fetchSource.URL = authenticatedURL

	fp := gofeed.NewParser()
	fp.Client = c.httpClient

	fetchStarted := time.Now()
	feed, headers, responseBytes, cacheStatus, err := c.fetchRSSFeed(ctx, fetchSource, source.URL, fp)
	if err != nil {
		if !shouldFallbackToCurl(source, err) {
			return sourceFetchResult{}, err
		}
		fetchCurl := c.fetchCurl
		if fetchCurl == nil {
			fetchCurl = fetchFeedWithCurlContext
		}
		body, curlErr := fetchCurl(ctx, fetchSource.URL)
		if curlErr != nil {
			return sourceFetchResult{}, fmt.Errorf("reddit rss curl fallback failed: %w", curlErr)
		}
		feed, err = fp.Parse(bytes.NewReader(body))
		if err != nil {
			return sourceFetchResult{}, err
		}
		headers = nil
		responseBytes = int64(len(body))
		cacheStatus = "curl"
	}

	result := sourceFetchResult{Source: source, FetchedCount: len(feed.Items), FetchDuration: time.Since(fetchStarted), ResponseBytes: responseBytes, CacheStatus: cacheStatus}
	isRedditRSS := isRedditURL(source.URL)
	if isRedditRSS {
		result.RedditRateLimitWait = redditRateLimitWaitFromHeader(headers)
	}
	redditOpenGraphFallbacks := 0
	now := time.Now()
	items := append([]*gofeed.Item(nil), feed.Items...)
	sort.SliceStable(items, func(i, j int) bool {
		return rssItemPublishedAt(items[i], now).After(rssItemPublishedAt(items[j], now))
	})
	for _, item := range items {
		if item == nil {
			continue
		}
		pub := rssItemPublishedAt(item, now)
		if !since.IsZero() && pub.Before(since) {
			continue
		}
		if source.MaxItems > 0 && len(result.Candidates) >= source.MaxItems {
			break
		}

		summary := item.Description
		if len(summary) > 500 {
			summary = truncateUTF8Bytes(summary, 500)
		}

		imageURL := extractRSSItemImage(item)
		if imageURL == "" {
			if isRedditRSS {
				if redditOpenGraphFallbacks < 3 {
					if redditOpenGraphFallbacks > 0 {
						if err := sleep(ctx, delay()); err != nil {
							return sourceFetchResult{}, err
						}
					}
					redditOpenGraphFallbacks++
					imageURL = c.fetchOpenGraphImage(ctx, item.Link)
				}
			} else {
				imageURL = c.fetchOpenGraphImage(ctx, item.Link)
			}
		}

		result.Candidates = append(result.Candidates, fetchedCandidate{
			Article: model.Article{
				Title:     item.Title,
				Link:      item.Link,
				Summary:   summary,
				ImageURL:  imageURL,
				Source:    source.Name,
				Category:  source.Category,
				Published: pub,
			},
			MatchedKeywords: matchedKeywords(item.Title+" "+item.Description, keywords),
		})
	}

	return result, nil
}

func rssItemPublishedAt(item *gofeed.Item, fallback time.Time) time.Time {
	if item == nil {
		return time.Time{}
	}
	if item.PublishedParsed != nil {
		return *item.PublishedParsed
	}
	if item.UpdatedParsed != nil {
		return *item.UpdatedParsed
	}
	return fallback
}

func authenticatedRSSURL(source config.Source) (string, error) {
	envName := strings.TrimSpace(source.RSSHubAccessKeyEnv)
	if envName == "" {
		return source.URL, nil
	}
	accessKey, ok := os.LookupEnv(envName)
	if !ok || strings.TrimSpace(accessKey) == "" {
		return "", fmt.Errorf("RSSHub access key environment variable %s is not set", envName)
	}
	parsedURL, err := url.Parse(source.URL)
	if err != nil || !parsedURL.IsAbs() || parsedURL.Host == "" {
		return "", fmt.Errorf("parse RSSHub source URL for authentication")
	}
	accessCode := md5.Sum([]byte(parsedURL.Path + accessKey))
	query := parsedURL.Query()
	query.Del("key")
	query.Set("code", fmt.Sprintf("%x", accessCode))
	parsedURL.RawQuery = query.Encode()
	return parsedURL.String(), nil
}

func (c *Client) fetchRSSFeed(ctx context.Context, source config.Source, cacheKeyURL string, fp *gofeed.Parser) (*gofeed.Feed, http.Header, int64, string, error) {
	if !isRedditURL(source.URL) {
		return c.fetchRSSFeedHTTP(ctx, source.URL, cacheKeyURL, fp)
	}
	feed, headers, err := c.fetchRSSFeedWithHeaders(ctx, source.URL, fp)
	return feed, headers, 0, "network", err
}

func (c *Client) fetchRSSFeedWithHeaders(ctx context.Context, feedURL string, fp *gofeed.Parser) (*gofeed.Feed, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", "application/rss+xml, application/xml;q=0.9, */*;q=0.8")
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, resp.Header, fmt.Errorf("http error: %d %s", resp.StatusCode, resp.Status)
	}
	feed, err := fp.Parse(resp.Body)
	return feed, resp.Header, err
}

func redditRateLimitWaitFromHeader(header http.Header) time.Duration {
	if wait := headerSecondsDuration(header.Get("Retry-After")); wait > 0 {
		return wait
	}
	return headerSecondsDuration(header.Get("X-Ratelimit-Reset"))
}

func headerSecondsDuration(raw string) time.Duration {
	seconds, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds * float64(time.Second))
}

func extractRSSItemImage(item *gofeed.Item) string {
	if item == nil {
		return ""
	}
	if item.Image != nil {
		if imageURL := normalizeRSSImageURL(item.Image.URL, item.Link); imageURL != "" {
			return imageURL
		}
	}
	for _, enclosure := range item.Enclosures {
		if enclosure == nil || !strings.HasPrefix(strings.ToLower(strings.TrimSpace(enclosure.Type)), "image/") {
			continue
		}
		if imageURL := normalizeRSSImageURL(enclosure.URL, item.Link); imageURL != "" {
			return imageURL
		}
	}
	for _, html := range []string{item.Content, item.Description} {
		matches := rssImageSourcePattern.FindAllStringSubmatch(html, -1)
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}
			if imageURL := normalizeRSSImageURL(match[1], item.Link); imageURL != "" {
				return imageURL
			}
		}
	}
	return ""
}

func normalizeRSSImageURL(rawURL string, baseURL string) string {
	imageURL := normalizeImageURL(rawURL, baseURL)
	if imageURL == "" || !imageutil.IsUsableRemoteImageURL(imageURL) {
		return ""
	}
	return imageURL
}

func (c *Client) fetchOpenGraphImage(ctx context.Context, articleURL string) string {
	articleURL = strings.TrimSpace(articleURL)
	if articleURL == "" {
		return ""
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, articleURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return ""
	}
	doc, err := goquery.NewDocumentFromReader(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return ""
	}
	for _, selector := range []string{
		`meta[property="og:image"]`,
		`meta[property="og:image:url"]`,
		`meta[name="twitter:image"]`,
		`meta[name="twitter:image:src"]`,
		`link[rel="image_src"]`,
	} {
		if imageURL := openGraphImageFromSelection(doc.Find(selector).First(), articleURL); imageURL != "" {
			return imageURL
		}
	}
	return articleBodyImage(doc, articleURL)
}

func openGraphImageFromSelection(selection *goquery.Selection, baseURL string) string {
	if selection == nil || selection.Length() == 0 {
		return ""
	}
	imageURL, ok := selection.Attr("content")
	if !ok {
		imageURL, _ = selection.Attr("href")
	}
	return normalizeRSSImageURL(imageURL, baseURL)
}

func articleBodyImage(doc *goquery.Document, baseURL string) string {
	for _, selector := range []string{
		`.left_zw img`,
		`article img`,
		`main img`,
		`[role="main"] img`,
		`[itemprop="articleBody"] img`,
		`.article-content img`,
		`.article_body img`,
		`.article-body img`,
		`.articleText img`,
		`.entry-content img`,
		`.post-content img`,
		`.story-body img`,
		`#article img`,
		`#articleContent img`,
		`#artibody img`,
	} {
		if imageURL := firstUsableArticleImage(doc.Find(selector), baseURL); imageURL != "" {
			return imageURL
		}
	}
	return ""
}

func firstUsableArticleImage(selection *goquery.Selection, baseURL string) string {
	var selected string
	selection.EachWithBreak(func(_ int, image *goquery.Selection) bool {
		if isDecorativeArticleImage(image) {
			return true
		}
		if imageURL := normalizeRSSImageURL(imageSource(image), baseURL); imageURL != "" {
			selected = imageURL
			return false
		}
		return true
	})
	return selected
}

func imageSource(image *goquery.Selection) string {
	for _, attr := range []string{"src", "data-src", "data-original", "data-lazy-src", "data-actualsrc", "data-url"} {
		if value := strings.TrimSpace(image.AttrOr(attr, "")); value != "" {
			return value
		}
	}
	return ""
}

func isDecorativeArticleImage(image *goquery.Selection) bool {
	value := strings.ToLower(strings.Join([]string{
		image.AttrOr("src", ""),
		image.AttrOr("data-src", ""),
		image.AttrOr("class", ""),
		image.AttrOr("id", ""),
		image.AttrOr("alt", ""),
	}, " "))
	for _, marker := range []string{
		"logo",
		"icon",
		"avatar",
		"banner",
		"advert",
		"ad-",
		"qrcode",
		"qr-code",
		"wechat",
		"weixin",
		"share",
		"spacer",
		"loading",
	} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return imageDimensionTooSmall(image.AttrOr("width", "")) || imageDimensionTooSmall(image.AttrOr("height", ""))
}

func imageDimensionTooSmall(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	raw = strings.TrimSuffix(raw, "px")
	value, err := strconv.Atoi(raw)
	return err == nil && value > 0 && value < 80
}

func shouldFallbackToCurl(source config.Source, err error) bool {
	if err == nil {
		return false
	}
	if !isRedditURL(source.URL) {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "403")
}

func isRedditURL(rawURL string) bool {
	u, parseErr := url.Parse(rawURL)
	if parseErr != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "reddit.com" || strings.HasSuffix(host, ".reddit.com")
}

func matchedKeywords(text string, keywords []string) []string {
	if len(keywords) == 0 {
		return nil
	}
	lower := strings.ToLower(text)
	var matched []string
	for _, kw := range keywords {
		trimmed := strings.TrimSpace(kw)
		if trimmed == "" {
			continue
		}
		if keywordMatches(lower, trimmed) {
			matched = append(matched, trimmed)
		}
	}
	return matched
}

// MatchKeywords returns configured keywords that occur in text using the same
// boundary rules as source filtering. It is shared with candidate ranking so
// filtering and ranking cannot disagree about short ASCII terms such as AI.
func MatchKeywords(text string, keywords []string) []string {
	return matchedKeywords(text, keywords)
}

func keywordMatches(lowerText string, keyword string) bool {
	lowerKeyword := strings.ToLower(keyword)
	if isASCIIAlnumKeyword(lowerKeyword) {
		return containsASCIIWord(lowerText, lowerKeyword)
	}
	return strings.Contains(lowerText, lowerKeyword)
}

func isASCIIAlnumKeyword(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r > 127 || !isASCIIAlnum(byte(r)) {
			return false
		}
	}
	return true
}

func containsASCIIWord(lowerText string, lowerKeyword string) bool {
	offset := 0
	for {
		index := strings.Index(lowerText[offset:], lowerKeyword)
		if index < 0 {
			return false
		}
		start := offset + index
		end := start + len(lowerKeyword)
		if isASCIIBoundary(lowerText, start-1) && (isASCIIBoundary(lowerText, end) || isSimpleASCIIPluralBoundary(lowerText, lowerKeyword, end)) {
			return true
		}
		offset = end
	}
}

func isSimpleASCIIPluralBoundary(value string, lowerKeyword string, end int) bool {
	return len(lowerKeyword) > 3 && end < len(value) && value[end] == 's' && isASCIIBoundary(value, end+1)
}

func isASCIIBoundary(value string, index int) bool {
	if index < 0 || index >= len(value) {
		return true
	}
	return !isASCIIAlnum(value[index])
}

func isASCIIAlnum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

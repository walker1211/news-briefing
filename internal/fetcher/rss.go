package fetcher

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
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
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	fp := gofeed.NewParser()
	fp.Client = c.httpClient

	feed, err := fp.ParseURLWithContext(source.URL, ctx)
	if err != nil {
		if !shouldFallbackToCurl(source, err) {
			return sourceFetchResult{}, err
		}
		fetchCurl := c.fetchCurl
		if fetchCurl == nil {
			fetchCurl = fetchFeedWithCurlContext
		}
		body, curlErr := fetchCurl(ctx, source.URL)
		if curlErr != nil {
			return sourceFetchResult{}, fmt.Errorf("reddit rss curl fallback failed: %w", curlErr)
		}
		feed, err = fp.Parse(bytes.NewReader(body))
		if err != nil {
			return sourceFetchResult{}, err
		}
	}

	result := sourceFetchResult{Source: source}
	for _, item := range feed.Items {
		pub := time.Now()
		if item.PublishedParsed != nil {
			pub = *item.PublishedParsed
		} else if item.UpdatedParsed != nil {
			pub = *item.UpdatedParsed
		}

		summary := item.Description
		if len(summary) > 500 {
			summary = summary[:500]
		}

		imageURL := extractRSSItemImage(item)
		if imageURL == "" {
			imageURL = c.fetchOpenGraphImage(ctx, item.Link)
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
	return ""
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

func shouldFallbackToCurl(source config.Source, err error) bool {
	if err == nil {
		return false
	}
	u, parseErr := url.Parse(source.URL)
	if parseErr != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	isReddit := host == "reddit.com" || strings.HasSuffix(host, ".reddit.com")
	if !isReddit {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "403")
}

func matchedKeywords(text string, keywords []string) []string {
	if len(keywords) == 0 {
		return nil
	}
	lower := strings.ToLower(text)
	var matched []string
	for _, kw := range keywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			matched = append(matched, kw)
		}
	}
	return matched
}

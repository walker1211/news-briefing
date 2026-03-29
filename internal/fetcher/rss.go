package fetcher

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/model"
)

var fetchFeedWithCurl = func(url string) ([]byte, error) {
	cmd := exec.Command("curl",
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

func FetchRSS(source config.Source, keywords []string, since time.Time) ([]model.Article, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fp := gofeed.NewParser()
	fp.Client = HTTPClient()

	feed, err := fp.ParseURLWithContext(source.URL, ctx)
	if err != nil {
		if !shouldFallbackToCurl(source, err) {
			return nil, err
		}
		body, curlErr := fetchFeedWithCurl(source.URL)
		if curlErr != nil {
			return nil, fmt.Errorf("reddit rss curl fallback failed: %w", curlErr)
		}
		feed, err = fp.Parse(bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
	}

	var articles []model.Article
	for _, item := range feed.Items {
		pub := time.Now()
		if item.PublishedParsed != nil {
			pub = *item.PublishedParsed
		} else if item.UpdatedParsed != nil {
			pub = *item.UpdatedParsed
		}

		if pub.Before(since) {
			continue
		}

		text := strings.ToLower(item.Title + " " + item.Description)
		if !matchKeywords(text, keywords) {
			continue
		}

		summary := item.Description
		if len(summary) > 500 {
			summary = summary[:500]
		}

		articles = append(articles, model.Article{
			Title:     item.Title,
			Link:      item.Link,
			Summary:   summary,
			Source:    source.Name,
			Category:  source.Category,
			Published: pub,
		})
	}

	return articles, nil
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

func matchKeywords(text string, keywords []string) bool {
	if len(keywords) == 0 {
		return true
	}
	lower := strings.ToLower(text)
	for _, kw := range keywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

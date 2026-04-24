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
		body, curlErr := fetchFeedWithCurlContext(ctx, source.URL)
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

		result.Candidates = append(result.Candidates, fetchedCandidate{
			Article: model.Article{
				Title:     item.Title,
				Link:      item.Link,
				Summary:   summary,
				Source:    source.Name,
				Category:  source.Category,
				Published: pub,
			},
			MatchedKeywords: matchedKeywords(item.Title+" "+item.Description, keywords),
		})
	}

	return result, nil
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

func matchKeywords(text string, keywords []string) bool {
	return len(matchedKeywords(text, keywords)) > 0
}

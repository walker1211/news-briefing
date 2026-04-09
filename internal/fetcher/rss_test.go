package fetcher

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestFetchRSSFallsBackToCurlForReddit403(t *testing.T) {
	oldClient := sharedClient
	oldCurl := fetchFeedWithCurl
	t.Cleanup(func() {
		sharedClient = oldClient
		fetchFeedWithCurl = oldCurl
	})

	sharedClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Status:     "403 Forbidden",
			Body:       io.NopCloser(strings.NewReader("forbidden")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})}

	fetchFeedWithCurl = func(url string) ([]byte, error) {
		return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>r/singularity</title>
    <item>
      <title>AI agent breakthrough</title>
      <link>https://example.com/post</link>
      <description>Agent automation update</description>
      <pubDate>Wed, 18 Mar 2026 10:00:00 GMT</pubDate>
    </item>
  </channel>
</rss>`), nil
	}

	result, err := FetchRSS(config.Source{
		Name:     "Reddit Singularity",
		URL:      "https://www.reddit.com/r/singularity/.rss",
		Type:     "rss",
		Category: "AI/科技",
	}, []string{"AI"}, time.Date(2026, 3, 18, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("FetchRSS() error = %v", err)
	}
	if len(result.Candidates) != 1 {
		t.Fatalf("len(result.Candidates) = %d, want 1", len(result.Candidates))
	}
	if result.Candidates[0].Article.Title != "AI agent breakthrough" {
		t.Fatalf("result.Candidates[0].Article.Title = %q", result.Candidates[0].Article.Title)
	}

}

func TestFetchRSSReturnsCurlFallbackError(t *testing.T) {
	oldClient := sharedClient
	oldCurl := fetchFeedWithCurl
	t.Cleanup(func() {
		sharedClient = oldClient
		fetchFeedWithCurl = oldCurl
	})

	sharedClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Status:     "403 Forbidden",
			Body:       io.NopCloser(strings.NewReader("forbidden")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})}

	fetchFeedWithCurl = func(url string) ([]byte, error) {
		return nil, errors.New("curl failed")
	}

	_, err := FetchRSS(config.Source{
		Name:     "Reddit Singularity",
		URL:      "https://www.reddit.com/r/singularity/.rss",
		Type:     "rss",
		Category: "AI/科技",
	}, []string{"AI"}, time.Date(2026, 3, 18, 8, 0, 0, 0, time.UTC))
	if err == nil || !strings.Contains(err.Error(), "curl failed") {
		t.Fatalf("FetchRSS() error = %v, want curl fallback error", err)
	}
}

func TestFetchRSSDoesNotFallbackToCurlForNonReddit403(t *testing.T) {
	oldClient := sharedClient
	oldCurl := fetchFeedWithCurl
	t.Cleanup(func() {
		sharedClient = oldClient
		fetchFeedWithCurl = oldCurl
	})

	sharedClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Status:     "403 Forbidden",
			Body:       io.NopCloser(strings.NewReader("forbidden")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})}

	called := false
	fetchFeedWithCurl = func(url string) ([]byte, error) {
		called = true
		return nil, nil
	}

	_, err := FetchRSS(config.Source{
		Name:     "Example",
		URL:      "https://example.com/feed.xml",
		Type:     "rss",
		Category: "AI/科技",
	}, []string{"AI"}, time.Date(2026, 3, 18, 8, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatalf("FetchRSS() error = nil, want forbidden error")
	}
	if called {
		t.Fatalf("fetchFeedWithCurl() was called for non-reddit source")
	}
}

func TestFetchRSSDoesNotFallbackToCurlForRedditNon403(t *testing.T) {
	oldClient := sharedClient
	oldCurl := fetchFeedWithCurl
	t.Cleanup(func() {
		sharedClient = oldClient
		fetchFeedWithCurl = oldCurl
	})

	sharedClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Status:     "500 Internal Server Error",
			Body:       io.NopCloser(strings.NewReader("boom")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})}

	called := false
	fetchFeedWithCurl = func(url string) ([]byte, error) {
		called = true
		return nil, nil
	}

	_, err := FetchRSS(config.Source{
		Name:     "Reddit Singularity",
		URL:      "https://www.reddit.com/r/singularity/.rss",
		Type:     "rss",
		Category: "AI/科技",
	}, []string{"AI"}, time.Date(2026, 3, 18, 8, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatalf("FetchRSS() error = nil, want server error")
	}
	if called {
		t.Fatalf("fetchFeedWithCurl() was called for reddit non-403 error")
	}
}

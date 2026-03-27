package fetcher

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
)

func loadKeywordsForTest(t *testing.T) []string {
	t.Helper()

	path := os.Getenv("NEWS_TEST_CONFIG_PATH")
	if path == "" {
		path = filepath.Join("..", "..", "configs", "config.example.yaml")
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load(%q) error = %v", path, err)
	}

	return cfg.Keywords
}

func withSharedClient(t *testing.T, client *http.Client) {
	t.Helper()

	oldClient := sharedClient
	sharedClient = client
	t.Cleanup(func() {
		sharedClient = oldClient
	})
}

func TestMatchKeywordsMatchesRSSOpenClawText(t *testing.T) {
	keywords := loadKeywordsForTest(t)
	source := config.Source{
		Name:     "Example RSS",
		URL:      "https://example.com/feed.xml",
		Type:     "rss",
		Category: "AI/科技",
	}
	since := time.Date(2026, 3, 22, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		title       string
		description string
	}{
		{
			name:        "title only",
			title:       "OpenClaw 2.0 ships security fixes",
			description: "Routine maintenance notes for local workflows",
		},
		{
			name:        "description only",
			title:       "Routine maintenance notes for local workflows",
			description: "Stable release notes confirm openclaw rollout completed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			feed := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Example RSS</title>
    <item>
      <title>` + tt.title + `</title>
      <link>https://example.com/post</link>
      <description>` + tt.description + `</description>
      <pubDate>Sun, 23 Mar 2026 10:00:00 GMT</pubDate>
    </item>
  </channel>
</rss>`

			withSharedClient(t, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Body:       io.NopCloser(strings.NewReader(feed)),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			})})

			articles, err := FetchRSS(source, keywords, since)
			if err != nil {
				t.Fatalf("FetchRSS() error = %v", err)
			}
			if len(articles) != 1 {
				t.Fatalf("len(articles) = %d, want 1", len(articles))
			}
		})
	}
}

func TestMatchKeywordsMatchesRedditOpenClawText(t *testing.T) {
	keywords := loadKeywordsForTest(t)
	source := config.Source{
		Name:     "Example Reddit",
		URL:      "https://example.com/reddit.json",
		Type:     "reddit",
		Category: "AI/科技",
	}
	since := time.Date(2026, 3, 22, 0, 0, 0, 0, time.UTC)
	created := time.Date(2026, 3, 23, 10, 0, 0, 0, time.UTC).Unix()

	tests := []struct {
		name     string
		title    string
		selftext string
	}{
		{
			name:     "title only",
			title:    "Show HN: OpenClaw plugin release notes",
			selftext: "Routine maintenance notes for local workflows",
		},
		{
			name:     "selftext only",
			title:    "Routine maintenance notes for local workflows",
			selftext: "Community diary says openclaw upgrade completed after local setup refresh",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listing := `{"data":{"children":[{"data":{"title":"` + tt.title + `","url":"https://example.com/post","permalink":"/r/test/comments/1","score":42,"created_utc":` +
				strconv.FormatInt(created, 10) + `,"selftext":"` + tt.selftext + `"}}]}}`

			withSharedClient(t, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Body:       io.NopCloser(strings.NewReader(listing)),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			})})

			articles, err := FetchReddit(source, keywords, since)
			if err != nil {
				t.Fatalf("FetchReddit() error = %v", err)
			}
			if len(articles) != 1 {
				t.Fatalf("len(articles) = %d, want 1", len(articles))
			}
		})
	}
}

func TestMatchKeywordsMatchesHackerNewsOpenClawTitle(t *testing.T) {
	keywords := loadKeywordsForTest(t)
	source := config.Source{
		Name:     "Hacker News",
		URL:      hnBaseURL,
		Type:     "hackernews",
		Category: "AI/科技",
	}
	since := time.Date(2026, 3, 22, 0, 0, 0, 0, time.UTC)
	itemTime := time.Date(2026, 3, 23, 10, 0, 0, 0, time.UTC).Unix()

	withSharedClient(t, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/v0/topstories.json":
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Body:       io.NopCloser(strings.NewReader(`[12345]`)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		case "/v0/item/12345.json":
			body := `{"id":12345,"title":"OpenClaw 2.0 adds safer local agent flows","url":"https://example.com/hn","score":99,"time":` + strconv.FormatInt(itemTime, 10) + `,"type":"story"}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		default:
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Status:     "404 Not Found",
				Body:       io.NopCloser(strings.NewReader("not found")),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}
	})})

	articles, err := FetchHackerNews(source, keywords, since)
	if err != nil {
		t.Fatalf("FetchHackerNews() error = %v", err)
	}
	if len(articles) != 1 {
		t.Fatalf("len(articles) = %d, want 1", len(articles))
	}
}

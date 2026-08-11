package fetcher

import (
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
	"github.com/mmcdole/gofeed"
	"github.com/walker1211/news-briefing/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func mustRSSHTMLDoc(t *testing.T, html string) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("parse html: %v", err)
	}
	return doc
}

func TestFetchRSSTruncatesSummaryOnUTF8Boundary(t *testing.T) {
	description := strings.Repeat("a", 499) + "中tail"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel><title>Test</title><item>
<title>UTF-8 summary</title><link>https://example.com/article</link>
<description>%s</description>
<enclosure url="https://example.com/image.jpg" type="image/jpeg" />
</item></channel></rss>`, description)
	}))
	defer server.Close()

	result, err := NewClient(server.Client()).FetchRSS(config.Source{
		Name: "Test RSS", URL: server.URL, Type: config.SourceTypeRSS, Category: "AI/科技",
	}, nil, time.Time{})
	if err != nil {
		t.Fatalf("FetchRSS() error = %v", err)
	}
	if len(result.Candidates) != 1 {
		t.Fatalf("len(result.Candidates) = %d, want 1", len(result.Candidates))
	}
	got := result.Candidates[0].Article.Summary
	if want := strings.Repeat("a", 499); got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
	if !utf8.ValidString(got) || len(got) > 500 {
		t.Fatalf("summary is not valid UTF-8 within 500 bytes: valid=%t bytes=%d", utf8.ValidString(got), len(got))
	}
}

func TestFetchRSSSkipsOldItemsAndAppliesNewestItemLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel><title>History</title>
<item><title>old</title><link>https://example.com/old</link><pubDate>Tue, 11 Aug 2026 08:00:00 GMT</pubDate><enclosure url="https://example.com/old.jpg" type="image/jpeg" /></item>
<item><title>newest</title><link>https://example.com/newest</link><pubDate>Tue, 11 Aug 2026 11:00:00 GMT</pubDate><enclosure url="https://example.com/newest.jpg" type="image/jpeg" /></item>
<item><title>third</title><link>https://example.com/third</link><pubDate>Tue, 11 Aug 2026 09:00:00 GMT</pubDate><enclosure url="https://example.com/third.jpg" type="image/jpeg" /></item>
<item><title>second</title><link>https://example.com/second</link><pubDate>Tue, 11 Aug 2026 10:00:00 GMT</pubDate><enclosure url="https://example.com/second.jpg" type="image/jpeg" /></item>
</channel></rss>`)
	}))
	defer server.Close()

	result, err := NewClient(server.Client()).FetchRSS(config.Source{
		Name: "History", URL: server.URL, Type: config.SourceTypeRSS, Category: "AI/科技", MaxItems: 2,
	}, nil, time.Date(2026, 8, 11, 8, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("FetchRSS() error = %v", err)
	}
	if result.FetchedCount != 4 {
		t.Fatalf("FetchedCount = %d, want 4", result.FetchedCount)
	}
	if len(result.Candidates) != 2 || result.Candidates[0].Article.Title != "newest" || result.Candidates[1].Article.Title != "second" {
		t.Fatalf("Candidates = %#v, want newest two in-window items", result.Candidates)
	}
}

func TestFetchRSSAuthenticatesRSSHubRoute(t *testing.T) {
	const accessKey = "test-master-key"
	t.Setenv("RSSHUB_ACCESS_KEY", accessKey)

	var requestedCode string
	var requestedKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requestedCode = req.URL.Query().Get("code")
		requestedKey = req.URL.Query().Get("key")
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = io.WriteString(w, `<?xml version="1.0"?><rss version="2.0"><channel><title>Finance</title></channel></rss>`)
	}))
	defer server.Close()

	client := NewClient(server.Client())
	_, err := client.FetchRSS(config.Source{
		Name:               "First Financial",
		URL:                server.URL + "/yicai/brief",
		Type:               config.SourceTypeRSS,
		Category:           "新闻财经",
		RSSHubAccessKeyEnv: "RSSHUB_ACCESS_KEY",
	}, nil, time.Time{})
	if err != nil {
		t.Fatalf("FetchRSS() error = %v", err)
	}
	wantCode := fmt.Sprintf("%x", md5.Sum([]byte("/yicai/brief"+accessKey)))
	if requestedCode != wantCode {
		t.Fatalf("code = %q, want %q", requestedCode, wantCode)
	}
	if requestedKey != "" {
		t.Fatalf("master key query parameter must be empty")
	}
}

func TestAuthenticatedRSSURLRequiresConfiguredEnvironment(t *testing.T) {
	t.Setenv("MISSING_RSSHUB_ACCESS_KEY", "")
	_, err := authenticatedRSSURL(config.Source{
		URL:                "https://rsshub.example.com/yicai/brief",
		RSSHubAccessKeyEnv: "MISSING_RSSHUB_ACCESS_KEY",
	})
	if err == nil || !strings.Contains(err.Error(), "MISSING_RSSHUB_ACCESS_KEY") {
		t.Fatalf("authenticatedRSSURL() error = %v", err)
	}
}

func TestFetchRSSFallsBackToCurlForReddit403(t *testing.T) {
	client := NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Status:     "403 Forbidden",
			Body:       io.NopCloser(strings.NewReader("forbidden")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})})

	client.fetchCurl = func(ctx context.Context, url string) ([]byte, error) {
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

	result, err := client.FetchRSS(config.Source{
		Name:     "Reddit Singularity",
		URL:      "https://www.reddit.com/r/singularity/.rss",
		Type:     config.SourceTypeRSS,
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

func TestFetchRedditRSSCapturesRateLimitResetHeader(t *testing.T) {
	client := NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body: io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>r/worldnews</title>
  </channel>
</rss>`)),
			Header:  http.Header{"X-Ratelimit-Reset": []string{"53"}},
			Request: req,
		}, nil
	})})

	result, err := client.FetchRSS(config.Source{
		Name:     "Reddit WorldNews",
		URL:      "https://www.reddit.com/r/worldnews/.rss",
		Type:     config.SourceTypeRSS,
		Category: "国际政治",
	}, nil, time.Time{})
	if err != nil {
		t.Fatalf("FetchRSS() error = %v", err)
	}
	if result.RedditRateLimitWait != 53*time.Second {
		t.Fatalf("RedditRateLimitWait = %v, want 53s", result.RedditRateLimitWait)
	}
}

func TestFetchRedditRSSPrefersRetryAfterHeader(t *testing.T) {
	client := NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body: io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>r/worldnews</title>
  </channel>
</rss>`)),
			Header:  http.Header{"Retry-After": []string{"12"}, "X-Ratelimit-Reset": []string{"53"}},
			Request: req,
		}, nil
	})})

	result, err := client.FetchRSS(config.Source{
		Name:     "Reddit WorldNews",
		URL:      "https://www.reddit.com/r/worldnews/.rss",
		Type:     config.SourceTypeRSS,
		Category: "国际政治",
	}, nil, time.Time{})
	if err != nil {
		t.Fatalf("FetchRSS() error = %v", err)
	}
	if result.RedditRateLimitWait != 12*time.Second {
		t.Fatalf("RedditRateLimitWait = %v, want 12s", result.RedditRateLimitWait)
	}
}

func TestFetchRSSExtractsImageFromEnclosure(t *testing.T) {
	client := NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body: io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Example Feed</title>
    <item>
      <title>OpenAI announced product</title>
      <link>https://example.com/openai</link>
      <description>OpenAI announced a product update</description>
      <enclosure url="https://example.com/cover.jpg" type="image/jpeg" length="123" />
      <pubDate>Wed, 18 Mar 2026 10:00:00 GMT</pubDate>
    </item>
  </channel>
</rss>`)),
			Header:  make(http.Header),
			Request: req,
		}, nil
	})})

	result, err := client.FetchRSS(config.Source{
		Name:     "Example",
		URL:      "https://example.com/feed.xml",
		Type:     config.SourceTypeRSS,
		Category: "AI/科技",
	}, []string{"OpenAI"}, time.Date(2026, 3, 18, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("FetchRSS() error = %v", err)
	}
	if len(result.Candidates) != 1 {
		t.Fatalf("len(result.Candidates) = %d, want 1", len(result.Candidates))
	}
	if got := result.Candidates[0].Article.ImageURL; got != "https://example.com/cover.jpg" {
		t.Fatalf("Article.ImageURL = %q, want enclosure image", got)
	}
}

func TestFetchRSSExtractsOpenGraphImageFromArticlePage(t *testing.T) {
	requested := []string{}
	client := NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requested = append(requested, req.URL.String())
		body := `<!doctype html><html><head><meta property="og:image" content="/wp-content/uploads/2026/06/rsz_neo_fromabove.jpg?w=879&amp;ssl=1"></head><body>story</body></html>`
		if req.URL.Path == "/feed.xml" {
			body = `<?xml version="1.0" encoding="UTF-8"?>
	<rss version="2.0">
	  <channel>
	    <title>SpaceNews</title>
	    <item>
	      <title>UK startup NewOrbit raises $18.5 million in Series A round</title>
	      <link>https://spacenews.com/uk-startup-neworbit-raises-18-5-million-in-series-a-round/</link>
	      <description>NewOrbit raises funding</description>
	      <pubDate>Wed, 8 Jun 2026 10:00:00 GMT</pubDate>
	    </item>
	  </channel>
	</rss>`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})})

	result, err := client.FetchRSS(config.Source{
		Name:     "SpaceNews",
		URL:      "https://spacenews.com/feed.xml",
		Type:     config.SourceTypeRSS,
		Category: "AI/科技",
	}, []string{"NewOrbit"}, time.Date(2026, 6, 8, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("FetchRSS() error = %v", err)
	}
	if len(result.Candidates) != 1 {
		t.Fatalf("len(result.Candidates) = %d, want 1", len(result.Candidates))
	}
	want := "https://spacenews.com/wp-content/uploads/2026/06/rsz_neo_fromabove.jpg?w=879&ssl=1"
	if got := result.Candidates[0].Article.ImageURL; got != want {
		t.Fatalf("Article.ImageURL = %q, want og image %q", got, want)
	}
	if strings.Join(requested, "\n") != strings.Join([]string{
		"https://spacenews.com/feed.xml",
		"https://spacenews.com/uk-startup-neworbit-raises-18-5-million-in-series-a-round/",
	}, "\n") {
		t.Fatalf("requested URLs = %#v", requested)
	}
}

func TestFetchRSSExtractsArticleBodyImageFromArticlePage(t *testing.T) {
	client := NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `<!doctype html>
<html>
<body>
  <header><img src="/site-logo.png"></header>
  <div class="left_zw">
    <p>Story text before image.</p>
    <div><img class="thumb-selected" src="//i2.example.com/simg/ypt/2026/260708/story_zsite.jpg" alt=""></div>
  </div>
  <aside><img src="/recommendation.jpg"></aside>
</body>
</html>`
		if req.URL.Path == "/feed.xml" {
			body = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Example Finance</title>
    <item>
      <title>European heat drives cooling economy</title>
      <link>https://example.com/cj/2026/07-08/story.shtml</link>
      <description>Cooling economy grows during heat wave</description>
      <pubDate>Wed, 8 Jul 2026 08:00:00 GMT</pubDate>
    </item>
  </channel>
</rss>`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})})

	result, err := client.FetchRSS(config.Source{
		Name:     "Example Finance",
		URL:      "https://example.com/feed.xml",
		Type:     config.SourceTypeRSS,
		Category: "新闻财经",
	}, []string{"economy"}, time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("FetchRSS() error = %v", err)
	}
	if len(result.Candidates) != 1 {
		t.Fatalf("len(result.Candidates) = %d, want 1", len(result.Candidates))
	}
	want := "https://i2.example.com/simg/ypt/2026/260708/story_zsite.jpg"
	if got := result.Candidates[0].Article.ImageURL; got != want {
		t.Fatalf("Article.ImageURL = %q, want article body image %q", got, want)
	}
}

func TestFetchRSSArticleBodyImageSkipsDecorativeImages(t *testing.T) {
	got := articleBodyImage(mustRSSHTMLDoc(t, `<!doctype html>
<html>
<body>
  <article>
    <img class="logo" src="/logo.png">
    <img width="32" height="32" src="/icon.png">
    <img data-src="/photos/story.jpg">
  </article>
</body>
</html>`), "https://example.com/story")

	if got != "https://example.com/photos/story.jpg" {
		t.Fatalf("articleBodyImage() = %q, want first non-decorative article image", got)
	}
}

func TestFetchRSSSkipsTrackingOpenGraphImage(t *testing.T) {
	client := NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `<!doctype html><html><head><meta property="og:image" content="https://media.npr.org/include/images/tracking/npr-rss-pixel.png?story=nx-s1"></head></html>`
		if req.URL.Path == "/feed.xml" {
			body = `<?xml version="1.0" encoding="UTF-8"?>
	<rss version="2.0">
	  <channel>
	    <title>NPR</title>
	    <item>
	      <title>Story without usable image</title>
	      <link>https://www.npr.org/story</link>
	      <description>Story description</description>
	      <pubDate>Wed, 8 Jun 2026 10:00:00 GMT</pubDate>
	    </item>
	  </channel>
	</rss>`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})})

	result, err := client.FetchRSS(config.Source{
		Name:     "NPR",
		URL:      "https://www.npr.org/feed.xml",
		Type:     config.SourceTypeRSS,
		Category: "国际政治",
	}, []string{"Story"}, time.Date(2026, 6, 8, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("FetchRSS() error = %v", err)
	}
	if len(result.Candidates) != 1 {
		t.Fatalf("len(result.Candidates) = %d, want 1", len(result.Candidates))
	}
	if got := result.Candidates[0].Article.ImageURL; got != "" {
		t.Fatalf("Article.ImageURL = %q, want empty for tracking og image", got)
	}
}

func TestFetchRSSSkipsReadhubGenericOpenGraphImage(t *testing.T) {
	client := NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `<!doctype html><html><head>
<meta property="og:image" content="https://readhub.cn/social-image.webp">
<meta name="twitter:image" content="https://readhub.cn/social-image.webp">
</head><body><main><img alt="logo" width="80" height="15" src="/readhub.png"></main></body></html>`
		if req.URL.Path == "/rss" {
			body = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Readhub</title>
    <item>
      <title>DeepSeek invests in Unitree</title>
      <link>https://readhub.cn/topic/example</link>
      <description>DeepSeek and Unitree update</description>
      <pubDate>Sat, 8 Aug 2026 00:00:00 GMT</pubDate>
    </item>
  </channel>
</rss>`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})})

	result, err := client.FetchRSS(config.Source{
		Name:     "Readhub",
		URL:      "https://readhub.cn/rss",
		Type:     config.SourceTypeRSS,
		Category: "AI/科技",
	}, []string{"DeepSeek"}, time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("FetchRSS() error = %v", err)
	}
	if len(result.Candidates) != 1 {
		t.Fatalf("len(result.Candidates) = %d, want 1", len(result.Candidates))
	}
	if got := result.Candidates[0].Article.ImageURL; got != "" {
		t.Fatalf("Article.ImageURL = %q, want empty for generic Readhub preview", got)
	}
}

func TestExtractRSSItemImageSkipsTrackingPixel(t *testing.T) {
	item := &gofeed.Item{
		Link:        "https://example.com/story",
		Description: `<img src="https://media.npr.org/include/images/tracking/npr-rss-pixel.png?story=nx-s1"><img src="https://example.com/story.jpg">`,
	}

	got := extractRSSItemImage(item)
	if got != "https://example.com/story.jpg" {
		t.Fatalf("extractRSSItemImage() = %q, want content image", got)
	}
}

func TestExtractRSSItemImageSkipsTrackingItemImageForEnclosure(t *testing.T) {
	item := &gofeed.Item{
		Link:  "https://example.com/story",
		Image: &gofeed.Image{URL: "https://media.npr.org/include/images/tracking/npr-rss-pixel.png?story=nx-s1"},
		Enclosures: []*gofeed.Enclosure{{
			URL:  "https://example.com/cover.jpg",
			Type: "image/jpeg",
		}},
	}

	got := extractRSSItemImage(item)
	if got != "https://example.com/cover.jpg" {
		t.Fatalf("extractRSSItemImage() = %q, want enclosure image", got)
	}
}

func TestFetchRSSLeavesImageURLEmptyWhenNoImageExists(t *testing.T) {
	client := NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body: io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Example Feed</title>
    <item>
      <title>OpenAI announced product</title>
      <link>https://example.com/openai</link>
      <description>OpenAI announced a product update</description>
      <pubDate>Wed, 18 Mar 2026 10:00:00 GMT</pubDate>
    </item>
  </channel>
</rss>`)),
			Header:  make(http.Header),
			Request: req,
		}, nil
	})})

	result, err := client.FetchRSS(config.Source{
		Name:     "Example",
		URL:      "https://example.com/feed.xml",
		Type:     config.SourceTypeRSS,
		Category: "AI/科技",
	}, []string{"OpenAI"}, time.Date(2026, 3, 18, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("FetchRSS() error = %v", err)
	}
	if len(result.Candidates) != 1 {
		t.Fatalf("len(result.Candidates) = %d, want 1", len(result.Candidates))
	}
	if got := result.Candidates[0].Article.ImageURL; got != "" {
		t.Fatalf("Article.ImageURL = %q, want empty", got)
	}
}

func TestFetchRedditRSSLimitsOpenGraphFallbackToThreeItems(t *testing.T) {
	requested := []string{}
	client := NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requested = append(requested, req.URL.String())

		body := `<!doctype html><html><head><meta property="og:image" content="/image.jpg"></head></html>`
		if req.URL.Path == "/r/singularity/.rss" {
			body = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>r/singularity</title>
    <item><title>Story 1</title><link>https://www.reddit.com/r/singularity/comments/1/story-1/</link><description>AI story</description></item>
    <item><title>Story 2</title><link>https://www.reddit.com/r/singularity/comments/2/story-2/</link><description>AI story</description></item>
    <item><title>Story 3</title><link>https://www.reddit.com/r/singularity/comments/3/story-3/</link><description>AI story</description></item>
    <item><title>Story 4</title><link>https://www.reddit.com/r/singularity/comments/4/story-4/</link><description>AI story</description></item>
    <item><title>Story 5</title><link>https://www.reddit.com/r/singularity/comments/5/story-5/</link><description>AI story</description></item>
  </channel>
</rss>`
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})})

	sleep := func(context.Context, time.Duration) error {
		return nil
	}
	delay := func() time.Duration {
		return 3 * time.Second
	}

	result, err := client.fetchRSSContextWithOpenGraphOptions(context.Background(), config.Source{
		Name:     "Reddit Singularity",
		URL:      "https://www.reddit.com/r/singularity/.rss",
		Type:     config.SourceTypeRSS,
		Category: "AI/科技",
	}, []string{"AI"}, time.Time{}, sleep, delay)
	if err != nil {
		t.Fatalf("FetchRSSContext() error = %v", err)
	}
	if len(result.Candidates) != 5 {
		t.Fatalf("len(result.Candidates) = %d, want 5", len(result.Candidates))
	}

	wantRequests := strings.Join([]string{
		"https://www.reddit.com/r/singularity/.rss",
		"https://www.reddit.com/r/singularity/comments/1/story-1/",
		"https://www.reddit.com/r/singularity/comments/2/story-2/",
		"https://www.reddit.com/r/singularity/comments/3/story-3/",
	}, "\n")
	if strings.Join(requested, "\n") != wantRequests {
		t.Fatalf("requested URLs = %#v, want only feed plus first 3 OpenGraph fallbacks", requested)
	}

	for i := 0; i < 3; i++ {
		if result.Candidates[i].Article.ImageURL == "" {
			t.Fatalf("candidate %d ImageURL empty, want OpenGraph image", i)
		}
	}
	for i := 3; i < 5; i++ {
		if result.Candidates[i].Article.ImageURL != "" {
			t.Fatalf("candidate %d ImageURL = %q, want empty after fallback limit", i, result.Candidates[i].Article.ImageURL)
		}
	}
}

func TestFetchRedditRSSOpenGraphFallbackSleepsBetweenFallbackRequests(t *testing.T) {
	client := NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `<!doctype html><html><head><meta property="og:image" content="/image.jpg"></head></html>`
		if req.URL.Path == "/r/singularity/.rss" {
			body = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>r/singularity</title>
    <item><title>Story 1</title><link>https://www.reddit.com/r/singularity/comments/1/story-1/</link><description>AI story</description></item>
    <item><title>Story 2</title><link>https://www.reddit.com/r/singularity/comments/2/story-2/</link><description>AI story</description></item>
    <item><title>Story 3</title><link>https://www.reddit.com/r/singularity/comments/3/story-3/</link><description>AI story</description></item>
  </channel>
</rss>`
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})})

	var sleeps []time.Duration
	sleep := func(ctx context.Context, d time.Duration) error {
		sleeps = append(sleeps, d)
		return nil
	}
	delay := func() time.Duration {
		return 2500 * time.Millisecond
	}

	_, err := client.fetchRSSContextWithOpenGraphOptions(context.Background(), config.Source{
		Name: "Reddit Singularity",
		URL:  "https://www.reddit.com/r/singularity/.rss",
		Type: config.SourceTypeRSS,
	}, []string{"AI"}, time.Time{}, sleep, delay)
	if err != nil {
		t.Fatalf("FetchRSSContext() error = %v", err)
	}

	if len(sleeps) != 2 || sleeps[0] != 2500*time.Millisecond || sleeps[1] != 2500*time.Millisecond {
		t.Fatalf("sleeps = %v, want two 2.5s gaps between 3 fallback requests", sleeps)
	}
}

func TestFetchRSSOpenGraphFallbackDoesNotLimitNonRedditFeeds(t *testing.T) {
	requested := []string{}
	client := NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requested = append(requested, req.URL.String())

		body := `<!doctype html><html><head><meta property="og:image" content="/image.jpg"></head></html>`
		if req.URL.Path == "/feed.xml" {
			body = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Example Feed</title>
    <item><title>Story 1</title><link>https://example.com/story-1</link><description>AI story</description></item>
    <item><title>Story 2</title><link>https://example.com/story-2</link><description>AI story</description></item>
    <item><title>Story 3</title><link>https://example.com/story-3</link><description>AI story</description></item>
    <item><title>Story 4</title><link>https://example.com/story-4</link><description>AI story</description></item>
  </channel>
</rss>`
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})})

	var sleeps []time.Duration
	sleep := func(ctx context.Context, d time.Duration) error {
		sleeps = append(sleeps, d)
		return nil
	}
	delay := func() time.Duration {
		return 3 * time.Second
	}

	result, err := client.fetchRSSContextWithOpenGraphOptions(context.Background(), config.Source{
		Name: "Example",
		URL:  "https://example.com/feed.xml",
		Type: config.SourceTypeRSS,
	}, []string{"AI"}, time.Time{}, sleep, delay)
	if err != nil {
		t.Fatalf("FetchRSSContext() error = %v", err)
	}
	if len(result.Candidates) != 4 {
		t.Fatalf("len(result.Candidates) = %d, want 4", len(result.Candidates))
	}
	if len(requested) != 5 {
		t.Fatalf("requested URLs = %#v, want feed plus 4 OpenGraph fallback requests", requested)
	}
	if len(sleeps) != 0 {
		t.Fatalf("sleeps = %v, want no Reddit RSS fallback sleeps for non-Reddit feed", sleeps)
	}
}

func TestFetchRedditRSSOpenGraphFallbackStopsDuringGapWhenCancelled(t *testing.T) {
	client := NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `<!doctype html><html><head><meta property="og:image" content="/image.jpg"></head></html>`
		if req.URL.Path == "/r/singularity/.rss" {
			body = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>r/singularity</title>
    <item><title>Story 1</title><link>https://www.reddit.com/r/singularity/comments/1/story-1/</link><description>AI story</description></item>
    <item><title>Story 2</title><link>https://www.reddit.com/r/singularity/comments/2/story-2/</link><description>AI story</description></item>
  </channel>
</rss>`
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})})

	ctx, cancel := context.WithCancel(context.Background())
	sleep := func(ctx context.Context, d time.Duration) error {
		cancel()
		return ctx.Err()
	}
	delay := func() time.Duration {
		return 3 * time.Second
	}

	_, err := client.fetchRSSContextWithOpenGraphOptions(ctx, config.Source{
		Name: "Reddit Singularity",
		URL:  "https://www.reddit.com/r/singularity/.rss",
		Type: config.SourceTypeRSS,
	}, []string{"AI"}, time.Time{}, sleep, delay)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("FetchRSSContext() error = %v, want context.Canceled", err)
	}
}

func TestFetchRSSReturnsCurlFallbackError(t *testing.T) {
	client := NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Status:     "403 Forbidden",
			Body:       io.NopCloser(strings.NewReader("forbidden")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})})

	client.fetchCurl = func(ctx context.Context, url string) ([]byte, error) {
		return nil, errors.New("curl failed")
	}

	_, err := client.FetchRSS(config.Source{
		Name:     "Reddit Singularity",
		URL:      "https://www.reddit.com/r/singularity/.rss",
		Type:     config.SourceTypeRSS,
		Category: "AI/科技",
	}, []string{"AI"}, time.Date(2026, 3, 18, 8, 0, 0, 0, time.UTC))
	if err == nil || !strings.Contains(err.Error(), "curl failed") {
		t.Fatalf("FetchRSS() error = %v, want curl fallback error", err)
	}
}

func TestFetchRSSDoesNotFallbackToCurlForNonReddit403(t *testing.T) {
	client := NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Status:     "403 Forbidden",
			Body:       io.NopCloser(strings.NewReader("forbidden")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})})

	called := false
	client.fetchCurl = func(ctx context.Context, url string) ([]byte, error) {
		called = true
		return nil, nil
	}

	_, err := client.FetchRSS(config.Source{
		Name:     "Example",
		URL:      "https://example.com/feed.xml",
		Type:     config.SourceTypeRSS,
		Category: "AI/科技",
	}, []string{"AI"}, time.Date(2026, 3, 18, 8, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatalf("FetchRSS() error = nil, want forbidden error")
	}
	if called {
		t.Fatalf("client.fetchCurl() was called for non-reddit source")
	}
}

func TestFetchRSSDoesNotFallbackToCurlForRedditNon403(t *testing.T) {
	client := NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Status:     "500 Internal Server Error",
			Body:       io.NopCloser(strings.NewReader("boom")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})})

	called := false
	client.fetchCurl = func(ctx context.Context, url string) ([]byte, error) {
		called = true
		return nil, nil
	}

	_, err := client.FetchRSS(config.Source{
		Name:     "Reddit Singularity",
		URL:      "https://www.reddit.com/r/singularity/.rss",
		Type:     config.SourceTypeRSS,
		Category: "AI/科技",
	}, []string{"AI"}, time.Date(2026, 3, 18, 8, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatalf("FetchRSS() error = nil, want server error")
	}
	if called {
		t.Fatalf("client.fetchCurl() was called for reddit non-403 error")
	}
}

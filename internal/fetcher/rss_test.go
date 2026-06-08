package fetcher

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mmcdole/gofeed"
	"github.com/walker1211/news-briefing/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
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

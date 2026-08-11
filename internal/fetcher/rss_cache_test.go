package fetcher

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
)

func TestRSSCacheUsesConditionalRequestAndCachedBody(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("If-None-Match") == `"feed-v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Header().Set("ETag", `"feed-v1"`)
		_, _ = w.Write([]byte(`<?xml version="1.0"?><rss version="2.0"><channel><title>Feed</title><link>https://example.com</link><description>test</description><item><title>Cached item</title><link>https://example.com/item</link><pubDate>Tue, 11 Aug 2026 00:00:00 GMT</pubDate><description><![CDATA[<img src="https://example.com/image.jpg">summary]]></description></item></channel></rss>`))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.SetRSSCacheDir(t.TempDir())
	source := config.Source{Name: "cached", Type: config.SourceTypeRSS, URL: server.URL, Category: "AI"}
	first, err := client.FetchRSS(source, nil, time.Time{})
	if err != nil {
		t.Fatalf("first FetchRSS() error = %v", err)
	}
	second, err := client.FetchRSS(source, nil, time.Time{})
	if err != nil {
		t.Fatalf("second FetchRSS() error = %v", err)
	}
	if first.CacheStatus != "updated" || first.ResponseBytes == 0 {
		t.Fatalf("first metrics = cache:%q bytes:%d", first.CacheStatus, first.ResponseBytes)
	}
	if second.CacheStatus != "not_modified" || second.ResponseBytes != 0 {
		t.Fatalf("second metrics = cache:%q bytes:%d", second.CacheStatus, second.ResponseBytes)
	}
	if len(second.Candidates) != 1 || second.Candidates[0].Article.Title != "Cached item" {
		t.Fatalf("second candidates = %#v", second.Candidates)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests = %d, want 2", requests.Load())
	}
}

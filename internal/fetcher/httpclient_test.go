package fetcher

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
)

type httpClientRoundTripFunc func(*http.Request) (*http.Response, error)

func (f httpClientRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestNewHTTPClientAppliesDefaults(t *testing.T) {
	client := NewHTTPClient(config.Proxy{})
	if client == nil {
		t.Fatal("NewHTTPClient() = nil")
	}
	if client.Timeout != 30*time.Second {
		t.Fatalf("NewHTTPClient().Timeout = %v, want 30s", client.Timeout)
	}
	if _, ok := client.Transport.(*uaTransport); !ok {
		t.Fatalf("NewHTTPClient().Transport = %T, want *uaTransport", client.Transport)
	}
}

func TestUATransportSetsUserAgent(t *testing.T) {
	transport := &uaTransport{inner: httpClientRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get("User-Agent"); got != userAgent {
			t.Fatalf("User-Agent = %q, want %q", got, userAgent)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(nil)}, nil
	})}
	req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	_ = resp.Body.Close()
}

func TestNewHTTPClientPrefersHTTPProxyOverSocks5(t *testing.T) {
	client := NewHTTPClient(config.Proxy{HTTP: "http://127.0.0.1:8080", Socks5: "socks5://127.0.0.1:1080"})
	ua, ok := client.Transport.(*uaTransport)
	if !ok {
		t.Fatalf("Transport = %T, want *uaTransport", client.Transport)
	}
	transport, ok := ua.inner.(*http.Transport)
	if !ok {
		t.Fatalf("inner transport = %T, want *http.Transport", ua.inner)
	}
	if transport.Proxy == nil {
		t.Fatal("Proxy = nil, want HTTP proxy")
	}
	reqURL, err := url.Parse("https://example.com")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	req := &http.Request{URL: reqURL}
	proxyURL, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("Proxy() error = %v", err)
	}
	if got := proxyURL.String(); got != "http://127.0.0.1:8080" {
		t.Fatalf("Proxy() = %q, want HTTP proxy", got)
	}
}

func TestHTTPClientUsesDefaultWhenUninitialized(t *testing.T) {
	sharedClientMu.Lock()
	oldClient := sharedClient
	sharedClient = nil
	sharedClientMu.Unlock()
	defer func() {
		sharedClientMu.Lock()
		sharedClient = oldClient
		sharedClientMu.Unlock()
	}()

	client := HTTPClient()
	if client == nil {
		t.Fatal("HTTPClient() = nil")
	}
	sharedClientMu.Lock()
	cachedClient := sharedClient
	sharedClientMu.Unlock()
	if cachedClient != client {
		t.Fatal("HTTPClient() did not cache default shared client")
	}
}

func TestHTTPClientInitializesSharedClientConcurrently(t *testing.T) {
	sharedClientMu.Lock()
	oldClient := sharedClient
	sharedClient = nil
	sharedClientMu.Unlock()
	defer func() {
		sharedClientMu.Lock()
		sharedClient = oldClient
		sharedClientMu.Unlock()
	}()

	const goroutines = 32
	clients := make(chan *http.Client, goroutines)
	var wg sync.WaitGroup
	for range goroutines {
		wg.Go(func() {
			clients <- HTTPClient()
		})
	}
	wg.Wait()
	close(clients)

	var first *http.Client
	for client := range clients {
		if client == nil {
			t.Fatal("HTTPClient() = nil")
		}
		if first == nil {
			first = client
			continue
		}
		if client != first {
			t.Fatal("HTTPClient() returned different shared clients")
		}
	}
}

func TestClientFetchRSSUsesInjectedHTTPClient(t *testing.T) {
	called := false
	client := NewClient(&http.Client{Transport: httpClientRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		if req.URL.String() != "https://example.com/feed.xml" {
			t.Fatalf("request URL = %q", req.URL.String())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body: io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel><item><title>AI update</title><link>https://example.com/a</link><description>AI news</description><pubDate>Wed, 18 Mar 2026 10:00:00 GMT</pubDate></item></channel></rss>`)),
			Header:  make(http.Header),
			Request: req,
		}, nil
	})})

	result, err := client.FetchRSSContext(context.Background(), config.Source{Name: "RSS", URL: "https://example.com/feed.xml"}, []string{"AI"}, time.Time{})
	if err != nil {
		t.Fatalf("FetchRSSContext() error = %v", err)
	}
	if !called {
		t.Fatal("injected client was not called")
	}
	if len(result.Candidates) != 1 || result.Candidates[0].Article.Title != "AI update" {
		t.Fatalf("Candidates = %#v", result.Candidates)
	}
}

func TestClientFetchRedditUsesInjectedHTTPClient(t *testing.T) {
	called := false
	client := NewClient(&http.Client{Transport: httpClientRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		if req.URL.String() != "https://example.com/reddit.json" {
			t.Fatalf("request URL = %q", req.URL.String())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader(`{"data":{"children":[{"data":{"title":"AI post","url":"https://example.com/post","score":12,"created_utc":1773837600,"selftext":"AI discussion"}}]}}`)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})})

	result, err := client.FetchRedditContext(context.Background(), config.Source{Name: "Reddit", URL: "https://example.com/reddit.json"}, []string{"AI"}, time.Time{})
	if err != nil {
		t.Fatalf("FetchRedditContext() error = %v", err)
	}
	if !called {
		t.Fatal("injected client was not called")
	}
	if len(result.Candidates) != 1 || result.Candidates[0].Article.Title != "AI post" {
		t.Fatalf("Candidates = %#v", result.Candidates)
	}
}

func TestClientFetchHackerNewsUsesInjectedHTTPClient(t *testing.T) {
	var urls []string
	client := NewClient(&http.Client{Transport: httpClientRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		urls = append(urls, req.URL.String())
		body := `[101]`
		if strings.HasSuffix(req.URL.Path, "/item/101.json") {
			body = `{"id":101,"title":"AI launch","url":"https://example.com/hn","score":42,"time":1773837600,"type":"story"}`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})})

	result, err := client.FetchHackerNewsContext(context.Background(), config.Source{Name: "HN"}, []string{"AI"}, time.Time{})
	if err != nil {
		t.Fatalf("FetchHackerNewsContext() error = %v", err)
	}
	if len(urls) != 2 {
		t.Fatalf("request URLs = %v, want topstories and item", urls)
	}
	if len(result.Candidates) != 1 || result.Candidates[0].Article.Title != "AI launch" {
		t.Fatalf("Candidates = %#v", result.Candidates)
	}
	summary := result.Candidates[0].Article.Summary
	if !strings.Contains(summary, "HN Score: 42") {
		t.Fatalf("Summary = %q, want HN score", summary)
	}
	if !strings.Contains(summary, "\n简介：") || !strings.Contains(summary, "example.com") || !strings.Contains(summary, "AI launch") {
		t.Fatalf("Summary = %q, want generated HN intro with domain and title on next line", summary)
	}
}

func TestClientFetchPageUsesInjectedHTTPClient(t *testing.T) {
	called := false
	client := NewClient(&http.Client{Transport: httpClientRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		if req.URL.String() != "https://example.com/page" {
			t.Fatalf("request URL = %q", req.URL.String())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader(`<html><head><title>AI tool released</title><meta name="description" content="AI tool announced"><meta property="article:published_time" content="2026-03-18T10:00:00Z"></head><body><p>AI tool now available</p></body></html>`)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})})

	result, err := client.FetchDocsPageContext(context.Background(), config.Source{
		Name:     "Docs",
		URL:      "https://example.com/page",
		Keywords: []string{"AI"},
		TimeHint: "published",
	}, nil, time.Time{})
	if err != nil {
		t.Fatalf("FetchDocsPageContext() error = %v", err)
	}
	if !called {
		t.Fatal("injected client was not called")
	}
	if len(result.Candidates) != 1 || result.Candidates[0].Article.Title != "AI tool released" {
		t.Fatalf("Candidates = %#v", result.Candidates)
	}
}

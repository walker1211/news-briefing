package watch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/model"
)

func TestBrowseboxProxyArgsDefaultHealthURLsFromWatchSites(t *testing.T) {
	cfg := &config.Config{
		Watch: config.WatchConfig{
			Sites: []config.WatchSite{
				{Name: "Support", HomeURL: "https://support.claude.com/zh-CN"},
				{Name: "News", HomeURL: "https://www.anthropic.com/news"},
			},
			ProxyProvider: config.WatchProxyProvider{
				Enabled:          true,
				Type:             config.WatchProxyProviderTypeBrowsebox,
				Command:          "browsebox",
				Group:            "",
				NodesConcurrency: 12,
				DelayTimeoutMS:   7000,
				ProxyPort:        17997,
				ControllerPort:   17998,
			},
		},
	}
	got := browseboxProxyArgs(cfg)
	want := []string{
		"proxy",
		"--select-fastest",
		"--group", "",
		"--proxy-port", "17997",
		"--controller-port", "17998",
		"--nodes-concurrency", "12",
		"--delay-timeout-ms", "7000",
		"--health-url", "https://support.claude.com/zh-CN",
		"--health-url", "https://www.anthropic.com/news",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func TestBrowseboxProxyArgsUseExplicitHealthURLs(t *testing.T) {
	cfg := &config.Config{
		Watch: config.WatchConfig{
			Sites: []config.WatchSite{{Name: "Support", HomeURL: "https://support.claude.com/zh-CN"}},
			ProxyProvider: config.WatchProxyProvider{
				Enabled:          true,
				Type:             config.WatchProxyProviderTypeBrowsebox,
				Command:          "browsebox",
				HealthURLs:       []string{"https://platform.claude.com/docs/en/release-notes/overview"},
				NodesConcurrency: 12,
				DelayTimeoutMS:   7000,
				ProxyPort:        17997,
				ControllerPort:   17998,
			},
		},
	}
	got := browseboxProxyArgs(cfg)
	want := []string{
		"proxy",
		"--select-fastest",
		"--group", "",
		"--proxy-port", "17997",
		"--controller-port", "17998",
		"--nodes-concurrency", "12",
		"--delay-timeout-ms", "7000",
		"--health-url", "https://platform.claude.com/docs/en/release-notes/overview",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func TestRunContextUsesBrowseboxProxyForWatchFetches(t *testing.T) {
	var fetchedURLs []string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchedURLs = append(fetchedURLs, r.URL.String())
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if strings.Contains(r.URL.String(), "/news/a") {
			_, _ = w.Write([]byte(`<html><head><title>Article A</title><meta name="description" content="Article summary." /></head><body><article><h1>Article A</h1><p>Article summary.</p><p>Article body.</p></article></body></html>`))
			return
		}
		_, _ = w.Write([]byte(`<html><body><main><a href="/news/a"><h2>Article A</h2><p>Snippet A</p></a></main></body></html>`))
	}))
	t.Cleanup(proxy.Close)

	oldStart := startBrowseboxProxy
	t.Cleanup(func() { startBrowseboxProxy = oldStart })
	started := false
	closed := false
	startBrowseboxProxy = func(ctx context.Context, cfg *config.Config) (*browseboxProxySession, error) {
		started = true
		done := make(chan error, 1)
		done <- nil
		return &browseboxProxySession{proxyURL: proxy.URL, cancel: func() { closed = true }, done: done}, nil
	}

	fetchHTML := func(ctx context.Context, url string) (string, error) {
		t.Fatalf("runContext used the non-proxy fetcher for %s", url)
		return "", nil
	}
	cfg := &config.Config{
		Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly},
		Fetch:  config.FetchConfig{RetryTimes: 1, Timeout: 2 * time.Second},
		Watch: config.WatchConfig{
			Sites: []config.WatchSite{
				{Name: "Example News", Type: config.WatchTypeAnnouncementPage, HomeURL: "http://example.test/news", BriefingCategory: "AI/科技"},
			},
			ProxyProvider: config.WatchProxyProvider{Enabled: true, Type: config.WatchProxyProviderTypeBrowsebox},
		},
	}
	_, _, err := runContext(context.Background(), cfg, time.Date(2026, 5, 28, 18, 0, 0, 0, time.UTC), fetchHTML)
	if err != nil {
		t.Fatalf("runContext() error = %v", err)
	}
	if !started {
		t.Fatal("browsebox proxy was not started")
	}
	if !closed {
		t.Fatal("browsebox proxy was not closed")
	}
	if len(fetchedURLs) == 0 {
		t.Fatal("proxy did not receive any Watch fetches")
	}
}

package watch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/model"
)

func TestBrowseboxProxyArgsUsesConfiguredMode(t *testing.T) {
	cfg := &config.Config{
		Watch: config.WatchConfig{
			ProxyProvider: config.WatchProxyProvider{
				Mode:             "custom-mode",
				Group:            "",
				NodesConcurrency: 12,
				DelayTimeoutMS:   7000,
				ProxyPort:        17997,
				ControllerPort:   17998,
			},
		},
	}
	got := browseboxProxyArgs(cfg)
	if got[0] != "custom-mode" {
		t.Fatalf("first arg = %q, want configured mode", got[0])
	}
}

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

func TestStartBrowseboxProxyProcessUsesConfiguredStartupTimeout(t *testing.T) {
	dir := t.TempDir()
	commandPath := filepath.Join(dir, "browsebox")
	command := `#!/bin/sh
sleep 0.2
printf 'Proxy: http://127.0.0.1:17997\n'
sleep 5
`
	if err := os.WriteFile(commandPath, []byte(command), 0o755); err != nil {
		t.Fatalf("write fake browsebox: %v", err)
	}
	cfg := &config.Config{Watch: config.WatchConfig{ProxyProvider: config.WatchProxyProvider{
		Command:        commandPath,
		ProxyPort:      17997,
		ControllerPort: 17998,
		StartupTimeout: 50 * time.Millisecond,
	}}}

	session, err := startBrowseboxProxyProcess(context.Background(), cfg)
	if err == nil {
		_ = session.Close()
		t.Fatal("startBrowseboxProxyProcess() error = nil, want startup timeout")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("startBrowseboxProxyProcess() error = %v, want timeout", err)
	}
}

func TestStartBrowseboxProxyProcessRunsCommandFromBinaryDirectory(t *testing.T) {
	dir := t.TempDir()
	otherDir := t.TempDir()
	commandPath := filepath.Join(dir, "browsebox")
	command := "#!/bin/sh\n" +
		"if [ \"$(pwd)\" != " + strconv.Quote(dir) + " ]; then\n" +
		"  printf 'cwd mismatch: %s\\n' \"$(pwd)\" >&2\n" +
		"  exit 8\n" +
		"fi\n" +
		"printf 'Proxy: http://127.0.0.1:17997\\n'\n" +
		"sleep 5\n"
	if err := os.WriteFile(commandPath, []byte(command), 0o755); err != nil {
		t.Fatalf("write fake browsebox: %v", err)
	}
	t.Chdir(otherDir)
	cfg := &config.Config{Watch: config.WatchConfig{ProxyProvider: config.WatchProxyProvider{
		Command:        commandPath,
		ProxyPort:      17997,
		ControllerPort: 17998,
	}}}

	session, err := startBrowseboxProxyProcess(context.Background(), cfg)
	if err != nil {
		t.Fatalf("startBrowseboxProxyProcess() error = %v", err)
	}
	_ = session.Close()
}

func TestStartBrowseboxProxyProcessReturnsWhenChildExitsBeforeReady(t *testing.T) {
	dir := t.TempDir()
	commandPath := filepath.Join(dir, "browsebox")
	command := `#!/bin/sh
printf 'probing nodes\n'
printf 'health check failed\n' >&2
exit 7
`
	if err := os.WriteFile(commandPath, []byte(command), 0o755); err != nil {
		t.Fatalf("write fake browsebox: %v", err)
	}
	cfg := &config.Config{Watch: config.WatchConfig{ProxyProvider: config.WatchProxyProvider{
		Command:        commandPath,
		ProxyPort:      17997,
		ControllerPort: 17998,
	}}}
	errs := make(chan error, 1)
	go func() {
		session, err := startBrowseboxProxyProcess(context.Background(), cfg)
		if session != nil {
			_ = session.Close()
		}
		errs <- err
	}()

	select {
	case err := <-errs:
		if err == nil {
			t.Fatal("startBrowseboxProxyProcess() error = nil, want child exit error")
		}
		for _, want := range []string{"browsebox proxy exited before ready", "stdout: probing nodes", "stderr: health check failed"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("startBrowseboxProxyProcess() error = %v, want %q", err, want)
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("startBrowseboxProxyProcess() did not return after child exited before ready")
	}
}

func TestStartBrowseboxProxyProcessTerminatesChildGracefullyOnContextCancel(t *testing.T) {
	dir := t.TempDir()
	startedPath := filepath.Join(dir, "started")
	markerPath := filepath.Join(dir, "terminated")
	commandPath := filepath.Join(dir, "browsebox")
	command := "#!/bin/sh\nWATCH_BROWSEBOX_PROXY_TEST_HELPER=1 WATCH_BROWSEBOX_PROXY_TEST_STARTED=" + strconv.Quote(startedPath) + " WATCH_BROWSEBOX_PROXY_TEST_MARKER=" + strconv.Quote(markerPath) + " exec " + strconv.Quote(os.Args[0]) + " -test.run=TestBrowseboxProxySignalHelper\n"
	if err := os.WriteFile(commandPath, []byte(command), 0o755); err != nil {
		t.Fatalf("write fake browsebox: %v", err)
	}
	cfg := &config.Config{Watch: config.WatchConfig{ProxyProvider: config.WatchProxyProvider{
		Command:        commandPath,
		ProxyPort:      17997,
		ControllerPort: 17998,
	}}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errs := make(chan error, 1)
	go func() {
		_, err := startBrowseboxProxyProcess(ctx, cfg)
		errs <- err
	}()

	startupTimeout := time.NewTimer(10 * time.Second)
	defer startupTimeout.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(startedPath); err == nil {
			break
		}
		select {
		case err := <-errs:
			t.Fatalf("startBrowseboxProxyProcess() returned before helper started: %v", err)
		case <-startupTimeout.C:
			t.Fatal("fake browsebox did not start")
		case <-ticker.C:
		}
	}
	cancel()

	select {
	case err := <-errs:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("startBrowseboxProxyProcess() error = %v, want context canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("startBrowseboxProxyProcess() did not return after context cancel")
	}
	if _, err := os.Stat(markerPath); err != nil {
		t.Fatalf("fake browsebox was not terminated gracefully: %v", err)
	}
}

func TestBrowseboxProxySignalHelper(t *testing.T) {
	if os.Getenv("WATCH_BROWSEBOX_PROXY_TEST_HELPER") != "1" {
		return
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	if err := os.WriteFile(os.Getenv("WATCH_BROWSEBOX_PROXY_TEST_STARTED"), []byte("started"), 0o644); err != nil {
		os.Exit(2)
	}
	<-signals
	if err := os.WriteFile(os.Getenv("WATCH_BROWSEBOX_PROXY_TEST_MARKER"), []byte("terminated"), 0o644); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func TestRunContextRetriesDirectFetchWhenBrowseboxReturnsClaudeRegionUnavailable(t *testing.T) {
	const releaseNotesURL = "http://docs.claude.com/en/release-notes/overview"
	var callsMu sync.Mutex
	var calls []string
	recordCall := func(call string) {
		callsMu.Lock()
		defer callsMu.Unlock()
		calls = append(calls, call)
	}
	snapshotCalls := func() []string {
		callsMu.Lock()
		defer callsMu.Unlock()
		return append([]string(nil), calls...)
	}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recordCall("browsebox")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><head><title>App unavailable in region | Claude</title></head><body><h1>App unavailable in region</h1></body></html>`))
	}))
	t.Cleanup(proxy.Close)

	oldStart := startBrowseboxProxy
	t.Cleanup(func() { startBrowseboxProxy = oldStart })
	startBrowseboxProxy = func(ctx context.Context, cfg *config.Config) (*browseboxProxySession, error) {
		done := make(chan error, 1)
		done <- nil
		return &browseboxProxySession{proxyURL: proxy.URL, cancel: func() {}, done: done}, nil
	}

	fixture := mustReadAnnouncementFixture(t, "claude_release_notes_home.html")
	fetchHTML := func(ctx context.Context, url string) (string, error) {
		recordCall("direct:" + url)
		if !strings.HasPrefix(url, releaseNotesURL) {
			t.Fatalf("direct fetch URL = %s, want Claude docs release notes URL", url)
		}
		return fixture, nil
	}
	cfg := &config.Config{
		Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly},
		Fetch:  config.FetchConfig{RetryTimes: 1, Timeout: 2 * time.Second},
		Watch: config.WatchConfig{
			Sites: []config.WatchSite{
				{Name: "Claude Platform Release Notes", Type: config.WatchTypeAnnouncementPage, HomeURL: releaseNotesURL, BriefingCategory: "AI/科技"},
			},
			ProxyProvider: config.WatchProxyProvider{Enabled: true, Type: config.WatchProxyProviderTypeBrowsebox},
		},
	}

	_, report, err := runContext(context.Background(), cfg, time.Date(2026, 5, 31, 18, 0, 0, 0, time.UTC), fetchHTML)
	if err != nil {
		t.Fatalf("runContext() error = %v", err)
	}
	if len(report.Events) != 0 {
		t.Fatalf("report.Events = %#v, want none", report.Events)
	}
	gotCalls := snapshotCalls()
	wantCalls := []string{"browsebox", "direct:" + releaseNotesURL}
	if len(gotCalls) < len(wantCalls) || gotCalls[0] != wantCalls[0] || gotCalls[1] != wantCalls[1] {
		t.Fatalf("call order = %#v, want first two calls %#v", gotCalls, wantCalls)
	}
}

func TestRunContextReportsClearErrorWhenBrowseboxAndDirectReleaseNotesUnavailable(t *testing.T) {
	const releaseNotesURL = "http://docs.claude.com/en/release-notes/overview"
	unavailableHTML := `<html><head><title>App unavailable in region | Claude</title></head><body><h1>App unavailable in region</h1></body></html>`
	var callsMu sync.Mutex
	var calls []string
	recordCall := func(call string) {
		callsMu.Lock()
		defer callsMu.Unlock()
		calls = append(calls, call)
	}
	snapshotCalls := func() []string {
		callsMu.Lock()
		defer callsMu.Unlock()
		return append([]string(nil), calls...)
	}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recordCall("browsebox")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(unavailableHTML))
	}))
	t.Cleanup(proxy.Close)

	oldStart := startBrowseboxProxy
	t.Cleanup(func() { startBrowseboxProxy = oldStart })
	startBrowseboxProxy = func(ctx context.Context, cfg *config.Config) (*browseboxProxySession, error) {
		done := make(chan error, 1)
		done <- nil
		return &browseboxProxySession{proxyURL: proxy.URL, cancel: func() {}, done: done}, nil
	}

	fetchHTML := func(ctx context.Context, url string) (string, error) {
		recordCall("direct:" + url)
		return unavailableHTML, nil
	}
	cfg := &config.Config{
		Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly},
		Fetch:  config.FetchConfig{RetryTimes: 1, Timeout: 2 * time.Second},
		Watch: config.WatchConfig{
			Sites: []config.WatchSite{
				{Name: "Claude Platform Release Notes", Type: config.WatchTypeAnnouncementPage, HomeURL: releaseNotesURL, BriefingCategory: "AI/科技"},
			},
			ProxyProvider: config.WatchProxyProvider{Enabled: true, Type: config.WatchProxyProviderTypeBrowsebox},
		},
	}

	_, report, err := runContext(context.Background(), cfg, time.Date(2026, 5, 31, 18, 0, 0, 0, time.UTC), fetchHTML)
	if err != nil {
		t.Fatalf("runContext() error = %v", err)
	}
	gotCalls := snapshotCalls()
	wantCalls := []string{"browsebox", "direct:" + releaseNotesURL}
	if len(gotCalls) < len(wantCalls) || gotCalls[0] != wantCalls[0] || gotCalls[1] != wantCalls[1] {
		t.Fatalf("call order = %#v, want first two calls %#v", gotCalls, wantCalls)
	}
	if len(report.Events) != 1 {
		t.Fatalf("len(report.Events) = %d, want 1; events=%#v", len(report.Events), report.Events)
	}
	want := "抓取失败：release notes page unavailable: browsebox and fallback proxy both redirected to Claude app unavailable in region page"
	if report.Events[0].Reason != want {
		t.Fatalf("report.Events[0].Reason = %q, want %q", report.Events[0].Reason, want)
	}
}

func TestRunContextDoesNotRepeatDirectFallbackWhenReleaseNotesUnavailable(t *testing.T) {
	const releaseNotesURL = "http://docs.claude.com/en/release-notes/overview"
	unavailableHTML := `<html><head><title>App unavailable in region | Claude</title></head><body><h1>App unavailable in region</h1></body></html>`
	var callsMu sync.Mutex
	var calls []string
	var directFetchCount int
	recordCall := func(call string) {
		callsMu.Lock()
		defer callsMu.Unlock()
		calls = append(calls, call)
	}
	snapshotCalls := func() []string {
		callsMu.Lock()
		defer callsMu.Unlock()
		return append([]string(nil), calls...)
	}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recordCall("browsebox")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(unavailableHTML))
	}))
	t.Cleanup(proxy.Close)

	oldStart := startBrowseboxProxy
	t.Cleanup(func() { startBrowseboxProxy = oldStart })
	startBrowseboxProxy = func(ctx context.Context, cfg *config.Config) (*browseboxProxySession, error) {
		done := make(chan error, 1)
		done <- nil
		return &browseboxProxySession{proxyURL: proxy.URL, cancel: func() {}, done: done}, nil
	}

	fetchHTML := func(ctx context.Context, url string) (string, error) {
		recordCall("direct:" + url)
		if !strings.HasPrefix(url, releaseNotesURL) {
			t.Fatalf("direct fetch URL = %s, want Claude docs release notes URL", url)
		}
		directFetchCount++
		return unavailableHTML, nil
	}
	cfg := &config.Config{
		Output: config.OutputCfg{Dir: t.TempDir(), Mode: model.OutputModeOriginalOnly},
		Fetch:  config.FetchConfig{RetryTimes: 3, Timeout: 2 * time.Second},
		Watch: config.WatchConfig{
			Sites: []config.WatchSite{
				{Name: "Claude Platform Release Notes", Type: config.WatchTypeAnnouncementPage, HomeURL: releaseNotesURL, BriefingCategory: "AI/科技"},
			},
			ProxyProvider: config.WatchProxyProvider{Enabled: true, Type: config.WatchProxyProviderTypeBrowsebox},
		},
	}

	_, report, err := runContext(context.Background(), cfg, time.Date(2026, 5, 31, 18, 0, 0, 0, time.UTC), fetchHTML)
	if err != nil {
		t.Fatalf("runContext() error = %v", err)
	}
	if len(report.Events) != 1 {
		t.Fatalf("len(report.Events) = %d, want 1; events=%#v", len(report.Events), report.Events)
	}
	want := "抓取失败：release notes page unavailable: browsebox and fallback proxy both redirected to Claude app unavailable in region page"
	if report.Events[0].Reason != want {
		t.Fatalf("report.Events[0].Reason = %q, want %q", report.Events[0].Reason, want)
	}
	if directFetchCount != 1 {
		t.Fatalf("direct fallback fetch count = %d, want 1", directFetchCount)
	}
	gotCalls := snapshotCalls()
	wantCalls := []string{"browsebox", "direct:" + releaseNotesURL}
	if len(gotCalls) < len(wantCalls) || gotCalls[0] != wantCalls[0] || gotCalls[1] != wantCalls[1] {
		t.Fatalf("call order = %#v, want first two calls %#v", gotCalls, wantCalls)
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

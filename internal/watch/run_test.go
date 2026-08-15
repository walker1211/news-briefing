package watch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/fetcher"
	"github.com/walker1211/news-briefing/internal/model"
)

func TestFetchWatchHTMLIncludesURLOnUnexpectedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	defer server.Close()

	fetcher.InitHTTPClient(config.Proxy{})

	_, err := fetchWatchHTML(context.Background(), server.URL+"/release-notes")
	if err == nil {
		t.Fatal("fetchWatchHTML() error = nil, want unexpected status error")
	}
	if !strings.Contains(err.Error(), server.URL+"/release-notes") {
		t.Fatalf("fetchWatchHTML() error = %q, want URL in error", err)
	}
	if !strings.Contains(err.Error(), "unexpected status 502") {
		t.Fatalf("fetchWatchHTML() error = %q, want status in error", err)
	}
}

func TestRunnerFetchWatchHTMLUsesInjectedHTTPClient(t *testing.T) {
	called := false
	runner := NewRunner(&http.Client{Transport: watchRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		if req.URL.String() != "https://example.com/watch" {
			t.Fatalf("request URL = %q", req.URL.String())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader("<html>ok</html>")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})})

	html, err := runner.fetchWatchHTML(context.Background(), "https://example.com/watch")
	if err != nil {
		t.Fatalf("fetchWatchHTML() error = %v", err)
	}
	if !called {
		t.Fatal("injected client was not called")
	}
	if html != "<html>ok</html>" {
		t.Fatalf("html = %q", html)
	}
}

type watchRoundTripFunc func(*http.Request) (*http.Response, error)

func (f watchRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRunContextReturnsContextErrorWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := RunContext(ctx, &config.Config{}, time.Now())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunContext() error = %v, want context.Canceled", err)
	}
}

func TestDedupeWatchArtifactsRemovesCrossCategoryURLDuplicates(t *testing.T) {
	articles := dedupeWatchArticles([]model.Article{
		{Title: "Article", Link: "https://example.com/article", Source: "Watch A"},
		{Title: "Article", Link: "https://example.com/article", Source: "Watch A"},
	})
	if len(articles) != 1 {
		t.Fatalf("articles = %d, want 1", len(articles))
	}

	seenItems := dedupeWatchSeenItems([]model.WatchSeenArticle{
		{Title: "Article", URL: "https://example.com/article", WatchCategory: "Category A"},
		{Title: "Article", URL: "https://example.com/article", WatchCategory: "Category B"},
	})
	if len(seenItems) != 1 || seenItems[0].WatchCategory != "Category A" {
		t.Fatalf("seen items = %#v, want first unique item", seenItems)
	}

	events := dedupeWatchEvents([]model.WatchEvent{
		{Source: "Watch A", Category: "Category A", EventType: "new_article", ArticleURL: "https://example.com/article"},
		{Source: "Watch A", Category: "Category B", EventType: "new_article", ArticleURL: "https://example.com/article"},
		{Source: "Watch A", Category: "Category B", EventType: "content_changed", ArticleURL: "https://example.com/article"},
	})
	if len(events) != 2 {
		t.Fatalf("events = %#v, want one event per URL and event type", events)
	}
}

func TestRunContextRetriesWatchFetchUsingFetchConfig(t *testing.T) {
	responses := map[string]string{
		"https://support.claude.com/zh-CN":                                  mustReadFixture(t, "anthropic/home.html"),
		"https://support.claude.com/zh-CN/collections/4078535-security":     mustReadFixture(t, "anthropic/category_security.html"),
		"https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证": mustReadFixture(t, "anthropic/article_identity_verification.html"),
	}
	attempts := map[string]int{}
	var attemptsMu sync.Mutex
	fetchHTML := func(ctx context.Context, url string) (string, error) {
		attemptsMu.Lock()
		attempts[url]++
		attempt := attempts[url]
		attemptsMu.Unlock()
		if url == "https://support.claude.com/zh-CN" && attempt < 3 {
			return "", errors.New("EOF")
		}
		return responses[url], nil
	}

	cfg := &config.Config{
		Output: config.OutputCfg{Dir: t.TempDir()},
		Fetch:  config.FetchConfig{RetryTimes: 3, RetryWaitTime: 0},
		Watch: config.WatchConfig{Sites: []config.WatchSite{{
			Name:              "Anthropic Claude Support",
			Type:              config.WatchTypeAnthropicSupport,
			HomeURL:           "https://support.claude.com/zh-CN",
			BriefingCategory:  "AI/科技",
			CategoryAllowlist: []string{"安全保障"},
			HighValueKeywords: []string{"身份验证", "电话验证"},
		}}},
	}

	_, _, err := runContext(context.Background(), cfg, time.Date(2026, 4, 15, 16, 0, 0, 0, time.UTC), fetchHTML)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	attemptsMu.Lock()
	defer attemptsMu.Unlock()
	if attempts["https://support.claude.com/zh-CN"] != 3 {
		t.Fatalf("home attempts = %d, want 3", attempts["https://support.claude.com/zh-CN"])
	}
}

func TestRetryingFetchHTMLUsesFallbackOnlyAfterFinalPrimaryFailure(t *testing.T) {
	primaryAttempts := 0
	fallbackAttempts := 0
	primary := func(context.Context, string) (string, error) {
		primaryAttempts++
		return "", errors.New("temporary")
	}
	fallback := func(context.Context, string) (string, error) {
		fallbackAttempts++
		return "fallback", nil
	}
	fetch := retryingFetchHTML(primary, watchFetchRetrySettings{times: 3, wait: 0, backoffFactor: 1, maxWait: 0}, fallback)
	html, err := fetch(context.Background(), "https://example.com")
	if err != nil {
		t.Fatalf("fetch() error = %v", err)
	}
	if html != "fallback" || primaryAttempts != 3 || fallbackAttempts != 1 {
		t.Fatalf("html=%q primary=%d fallback=%d", html, primaryAttempts, fallbackAttempts)
	}
}

func TestWatchRetryDelayUsesTieredBackoff(t *testing.T) {
	settings := watchFetchRetrySettings{wait: 3 * time.Second, backoffFactor: 3, maxWait: 12 * time.Second}
	if got := watchRetryDelay(settings, 1); got != 3*time.Second {
		t.Fatalf("first retry delay = %v, want 3s", got)
	}
	if got := watchRetryDelay(settings, 2); got != 9*time.Second {
		t.Fatalf("second retry delay = %v, want 9s", got)
	}
}

func TestRunContextTurnsAnthropicSupportFetchFailureIntoSiteError(t *testing.T) {
	attempts := 0
	fetchHTML := func(ctx context.Context, url string) (string, error) {
		attempts++
		return "", errors.New("EOF")
	}

	cfg := &config.Config{
		Output: config.OutputCfg{Dir: t.TempDir()},
		Fetch:  config.FetchConfig{RetryTimes: 3, RetryWaitTime: 0},
		Watch: config.WatchConfig{Sites: []config.WatchSite{{
			Name:             "Anthropic Claude Support",
			Type:             config.WatchTypeAnthropicSupport,
			HomeURL:          "https://support.claude.com/zh-CN",
			BriefingCategory: "AI/科技",
		}}},
	}

	articles, report, err := runContext(context.Background(), cfg, time.Date(2026, 4, 15, 16, 0, 0, 0, time.UTC), fetchHTML)
	if err != nil {
		t.Fatalf("Run() error = %v, want watch to continue when anthropic support fails", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	if len(report.Events) != 1 {
		t.Fatalf("len(report.Events) = %d, want 1; events=%#v", len(report.Events), report.Events)
	}
	if report.Events[0].EventType != "site_error" {
		t.Fatalf("report.Events[0].EventType = %q", report.Events[0].EventType)
	}
	if report.Events[0].Source != "Anthropic Claude Support" {
		t.Fatalf("report.Events[0].Source = %q", report.Events[0].Source)
	}
	if report.Events[0].Reason != "抓取失败：EOF" {
		t.Fatalf("report.Events[0].Reason = %q", report.Events[0].Reason)
	}
	if report.Events[0].IncludeInBriefing {
		t.Fatalf("report.Events[0].IncludeInBriefing = true, want false")
	}
	if len(articles) != 0 {
		t.Fatalf("articles = %#v, want no watch briefing articles", articles)
	}
}

func TestRunContextPropagatesCancellationFromAnnouncementSite(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	fetchHTML := func(ctx context.Context, url string) (string, error) {
		cancel()
		return "", ctx.Err()
	}

	cfg := &config.Config{
		Output: config.OutputCfg{Dir: t.TempDir()},
		Watch: config.WatchConfig{Sites: []config.WatchSite{{
			Name:             "Anthropic News",
			Type:             config.WatchTypeAnnouncementPage,
			HomeURL:          "https://www.anthropic.com/news",
			BriefingCategory: "AI/科技",
		}}},
	}

	_, _, err := runContext(ctx, cfg, time.Now(), fetchHTML)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunContext() error = %v, want context.Canceled", err)
	}
}

func TestRunBootstrapsCategoryBaselineWithoutBriefingArticles(t *testing.T) {
	responses := map[string]string{
		"https://support.claude.com/zh-CN":                                  mustReadFixture(t, "anthropic/home.html"),
		"https://support.claude.com/zh-CN/collections/4078535-security":     mustReadFixture(t, "anthropic/category_security.html"),
		"https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证": mustReadFixture(t, "anthropic/article_identity_verification.html"),
	}
	fetchHTML := func(ctx context.Context, url string) (string, error) { return responses[url], nil }

	cfg := &config.Config{
		Output: config.OutputCfg{Dir: t.TempDir()},
		Watch: config.WatchConfig{Sites: []config.WatchSite{{
			Name:              "Anthropic Claude Support",
			Type:              config.WatchTypeAnthropicSupport,
			HomeURL:           "https://support.claude.com/zh-CN",
			BriefingCategory:  "AI/科技",
			CategoryAllowlist: []string{"安全保障"},
			HighValueKeywords: []string{"身份验证", "电话验证"},
		}}},
	}

	articles, report, err := runContext(context.Background(), cfg, time.Date(2026, 4, 15, 16, 0, 0, 0, time.UTC), fetchHTML)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(report.Events) != 0 {
		t.Fatalf("report.Events = %#v, want bootstrap to stay silent", report.Events)
	}
	if len(articles) != 0 {
		t.Fatalf("articles = %#v, want bootstrap to avoid briefing output", articles)
	}

	indexStore := NewIndexStore(cfg.Output.Dir)
	indexState, err := indexStore.Load()
	if err != nil {
		t.Fatalf("indexStore.Load() error = %v", err)
	}
	snapshot, ok := indexState.Categories["Anthropic Claude Support::安全保障"]
	if !ok || snapshot.ItemCount == 0 {
		t.Fatalf("bootstrap category snapshot missing: %#v", indexState.Categories)
	}
}

func TestRunAnnouncementSiteBootstrapsWithoutBriefingOutput(t *testing.T) {
	responses := map[string]string{
		"https://www.anthropic.com/news":                   mustReadAnnouncementFixture(t, "anthropic_news_home.html"),
		"https://www.anthropic.com/news/claude-opus-4-7":   mustReadAnnouncementFixture(t, "anthropic_news_opus47.html"),
		"https://www.anthropic.com/news/claude-sonnet-4-6": mustReadAnnouncementFixture(t, "anthropic_news_opus47.html"),
	}
	fetchHTML := func(ctx context.Context, url string) (string, error) { return responses[url], nil }

	cfg := &config.Config{
		Output: config.OutputCfg{Dir: t.TempDir()},
		Watch: config.WatchConfig{Sites: []config.WatchSite{{
			Name:             "Anthropic News",
			Type:             config.WatchTypeAnnouncementPage,
			HomeURL:          "https://www.anthropic.com/news",
			BriefingCategory: "AI/科技",
			HighValueKeywords: []string{
				"Anthropic", "Claude", "Opus", "Sonnet",
			},
		}}},
	}

	articles, report, err := runContext(context.Background(), cfg, time.Date(2026, 4, 17, 9, 0, 0, 0, time.UTC), fetchHTML)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(report.Events) != 0 {
		t.Fatalf("report.Events = %#v, want bootstrap to stay silent", report.Events)
	}
	if len(articles) != 0 {
		t.Fatalf("articles = %#v, want bootstrap to avoid briefing output", articles)
	}

	indexStore := NewIndexStore(cfg.Output.Dir)
	indexState, err := indexStore.Load()
	if err != nil {
		t.Fatalf("indexStore.Load() error = %v", err)
	}
	snapshot, ok := indexState.Categories["Anthropic News::Anthropic News"]
	if !ok || snapshot.ItemCount == 0 {
		t.Fatalf("bootstrap announcement snapshot missing: %#v", indexState.Categories)
	}
}

func TestRunAnnouncementSiteIncludesNewArticleInBriefing(t *testing.T) {
	oldHome := `<html><body><main><a href="/news/claude-sonnet-4-6"><h2>Introducing Claude Sonnet 4.6</h2><p>Feb 17, 2026</p></a></main></body></html>`
	responses := map[string]string{
		"https://www.anthropic.com/news":                   mustReadAnnouncementFixture(t, "anthropic_news_home.html"),
		"https://www.anthropic.com/news/claude-opus-4-7":   mustReadAnnouncementFixture(t, "anthropic_news_opus47.html"),
		"https://www.anthropic.com/news/claude-sonnet-4-6": mustReadAnnouncementFixture(t, "anthropic_news_sonnet46.html"),
	}
	fetchHTML := func(ctx context.Context, url string) (string, error) { return responses[url], nil }

	cfg := &config.Config{
		Output: config.OutputCfg{Dir: t.TempDir()},
		Watch: config.WatchConfig{Sites: []config.WatchSite{{
			Name:             "Anthropic News",
			Type:             config.WatchTypeAnnouncementPage,
			HomeURL:          "https://www.anthropic.com/news",
			BriefingCategory: "AI/科技",
			HighValueKeywords: []string{
				"Anthropic", "Claude", "Opus", "Sonnet",
			},
		}}},
	}

	indexStore := NewIndexStore(cfg.Output.Dir)
	articleStore := NewArticleStore(cfg.Output.Dir)
	baseline, err := parseAnthropicAnnouncementIndex("Anthropic News", "https://www.anthropic.com/news", oldHome)
	if err != nil {
		t.Fatalf("parseAnthropicAnnouncementIndex() error = %v", err)
	}
	if err := indexStore.Save(IndexState{Categories: map[string]model.WatchIndexSnapshot{
		"Anthropic News::Anthropic News": baseline,
	}}); err != nil {
		t.Fatalf("indexStore.Save() error = %v", err)
	}
	if err := articleStore.Save(ArticleState{
		"https://www.anthropic.com/news/claude-sonnet-4-6": {
			URL:         "https://www.anthropic.com/news/claude-sonnet-4-6",
			Title:       "Introducing Claude Sonnet 4.6",
			SummaryHash: hashWatchContent("Claude Sonnet 4.6 balances speed and intelligence for everyday tasks."),
			BodyHash:    hashWatchContent("Claude Sonnet 4.6 balances speed and intelligence for everyday tasks. It improves latency and reliability for production use cases."),
		},
	}); err != nil {
		t.Fatalf("articleStore.Save() error = %v", err)
	}

	articles, report, err := runContext(context.Background(), cfg, time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC), fetchHTML)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	found := false
	for _, event := range report.Events {
		if event.EventType == "new_article" && event.ArticleURL == "https://www.anthropic.com/news/claude-opus-4-7" {
			found = true
			if !event.IncludeInBriefing {
				t.Fatalf("event should be included in briefing: %#v", event)
			}
		}
	}
	if !found {
		t.Fatalf("report.Events = %#v", report.Events)
	}
	if len(articles) != 1 {
		t.Fatalf("len(articles) = %d, want 1; articles=%#v", len(articles), articles)
	}
}

func TestRunClaudeReleaseNotesIncludesNewArticleInBriefing(t *testing.T) {
	oldHome := `<html><body><main><h3><div id="february-17-2026"><div>February 17, 2026</div></div></h3><ul><li>We've launched <a href="https://www.anthropic.com/news/claude-sonnet-4-6">Claude Sonnet 4.6</a>, our latest balanced model combining speed and intelligence for everyday tasks. Sonnet 4.6 delivers improved agentic search performance while consuming fewer tokens.</li></ul></main></body></html>`
	responses := map[string]string{
		"https://platform.claude.com/docs/en/release-notes/overview":                  mustReadAnnouncementFixture(t, "claude_release_notes_home.html"),
		"https://platform.claude.com/docs/en/release-notes/overview#april-16-2026":    mustReadAnnouncementFixture(t, "claude_release_notes_home.html"),
		"https://platform.claude.com/docs/en/release-notes/overview#february-17-2026": mustReadAnnouncementFixture(t, "claude_release_notes_home.html"),
		"https://platform.claude.com/docs/en/release-notes/claude-opus-4-7":           mustReadAnnouncementFixture(t, "claude_release_notes_opus47.html"),
		"https://platform.claude.com/docs/en/release-notes/claude-sonnet-4-6":         mustReadAnnouncementFixture(t, "claude_release_notes_sonnet46.html"),
	}
	fetchHTML := func(ctx context.Context, url string) (string, error) { return responses[url], nil }

	cfg := &config.Config{
		Output: config.OutputCfg{Dir: t.TempDir()},
		Watch: config.WatchConfig{Sites: []config.WatchSite{{
			Name:             "Claude Platform Release Notes",
			Type:             config.WatchTypeAnnouncementPage,
			HomeURL:          "https://platform.claude.com/docs/en/release-notes/overview",
			BriefingCategory: "AI/科技",
			HighValueKeywords: []string{
				"Claude", "Opus", "Sonnet", "API", "release",
			},
		}}},
	}

	indexStore := NewIndexStore(cfg.Output.Dir)
	articleStore := NewArticleStore(cfg.Output.Dir)
	baseline, err := parseAnthropicAnnouncementIndex("Claude Platform Release Notes", "https://platform.claude.com/docs/en/release-notes/overview", oldHome)
	if err != nil {
		t.Fatalf("parseAnthropicAnnouncementIndex() error = %v", err)
	}
	if err := indexStore.Save(IndexState{Categories: map[string]model.WatchIndexSnapshot{
		"Claude Platform Release Notes::Claude Platform Release Notes": baseline,
	}}); err != nil {
		t.Fatalf("indexStore.Save() error = %v", err)
	}
	if err := articleStore.Save(ArticleState{
		"https://platform.claude.com/docs/en/release-notes/overview#february-17-2026": {
			URL:         "https://platform.claude.com/docs/en/release-notes/overview#february-17-2026",
			Title:       "We've launched Claude Sonnet 4.6",
			SummaryHash: hashWatchContent("We've launched Claude Sonnet 4.6, our latest balanced model combining speed and intelligence for everyday tasks. Sonnet 4.6 delivers improved agentic search performance while consuming fewer tokens."),
			BodyHash:    hashWatchContent("We've launched Claude Sonnet 4.6, our latest balanced model combining speed and intelligence for everyday tasks. Sonnet 4.6 delivers improved agentic search performance while consuming fewer tokens. API code execution is now free when used with web search or web fetch."),
		},
	}); err != nil {
		t.Fatalf("articleStore.Save() error = %v", err)
	}

	articles, report, err := runContext(context.Background(), cfg, time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC), fetchHTML)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	found := false
	for _, event := range report.Events {
		if event.EventType == "new_article" && event.ArticleURL == "https://platform.claude.com/docs/en/release-notes/overview#april-16-2026" {
			found = true
			if !event.IncludeInBriefing {
				t.Fatalf("event should be included in briefing: %#v", event)
			}
		}
	}
	if !found {
		t.Fatalf("report.Events = %#v", report.Events)
	}
	if len(articles) != 1 {
		t.Fatalf("len(articles) = %d, want 1; articles=%#v", len(articles), articles)
	}
	if articles[0].Source != "Claude Platform Release Notes Watch" {
		t.Fatalf("articles[0].Source = %q", articles[0].Source)
	}
}

func TestRunClaudeReleaseNotesSkipsStaleDateFragmentContentChangesInBriefing(t *testing.T) {
	homeURL := "https://docs.claude.com/en/release-notes/overview"
	entryURL := homeURL + "#november-24-2025"
	currentHome := `<html><body><main>
		<h3><div id="november-24-2025"><div>November 24, 2025</div></div></h3>
		<ul>
			<li>We've launched <a href="https://www.anthropic.com/news/claude-opus-4-5">Claude Opus 4.5</a>, our latest model for complex coding and API workloads.</li>
			<li>API release notes now include additional tool updates.</li>
		</ul>
	</main></body></html>`
	responses := map[string]string{
		homeURL:  currentHome,
		entryURL: currentHome,
	}
	fetchHTML := func(ctx context.Context, url string) (string, error) { return responses[url], nil }

	cfg := &config.Config{
		Output: config.OutputCfg{Dir: t.TempDir()},
		Watch: config.WatchConfig{Sites: []config.WatchSite{{
			Name:             "Claude Platform Release Notes",
			Type:             config.WatchTypeAnnouncementPage,
			HomeURL:          homeURL,
			BriefingCategory: "AI/科技",
			HighValueKeywords: []string{
				"Claude", "Opus", "API", "release",
			},
		}}},
	}

	baseline, err := parseAnthropicAnnouncementIndex("Claude Platform Release Notes", homeURL, currentHome)
	if err != nil {
		t.Fatalf("parseAnthropicAnnouncementIndex() error = %v", err)
	}
	indexStore := NewIndexStore(cfg.Output.Dir)
	articleStore := NewArticleStore(cfg.Output.Dir)
	if err := indexStore.Save(IndexState{Categories: map[string]model.WatchIndexSnapshot{
		"Claude Platform Release Notes::Claude Platform Release Notes": baseline,
	}}); err != nil {
		t.Fatalf("indexStore.Save() error = %v", err)
	}
	if err := articleStore.Save(ArticleState{
		entryURL: {
			URL:         entryURL,
			Title:       "We've launched Claude Opus 4.5",
			SummaryHash: hashWatchContent("old summary"),
			BodyHash:    hashWatchContent("old body"),
		},
	}); err != nil {
		t.Fatalf("articleStore.Save() error = %v", err)
	}

	articles, report, err := runContext(context.Background(), cfg, time.Date(2026, 6, 9, 18, 0, 0, 0, time.UTC), fetchHTML)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	found := false
	for _, event := range report.Events {
		if event.EventType == "content_changed" && event.ArticleURL == entryURL {
			found = true
			if event.IncludeInBriefing {
				t.Fatalf("stale release-note content change should stay out of briefing: %#v", event)
			}
		}
	}
	if !found {
		t.Fatalf("report.Events = %#v", report.Events)
	}
	if len(articles) != 0 {
		t.Fatalf("articles = %#v, want stale release-note content change excluded", articles)
	}
}

func TestRunClaudeReleaseNotesHostMigrationDoesNotReannounceExistingEntries(t *testing.T) {
	oldHome := `<html><body><main><h3><div id="february-17-2026"><div>February 17, 2026</div></div></h3><ul><li>We've launched <a href="https://www.anthropic.com/news/claude-sonnet-4-6">Claude Sonnet 4.6</a>, our latest balanced model combining speed and intelligence for everyday tasks. Sonnet 4.6 delivers improved agentic search performance while consuming fewer tokens.</li></ul></main></body></html>`
	responses := map[string]string{
		"https://docs.claude.com/en/release-notes/overview":                  mustReadAnnouncementFixture(t, "claude_release_notes_home.html"),
		"https://docs.claude.com/en/release-notes/overview#april-16-2026":    mustReadAnnouncementFixture(t, "claude_release_notes_home.html"),
		"https://docs.claude.com/en/release-notes/overview#february-17-2026": mustReadAnnouncementFixture(t, "claude_release_notes_home.html"),
	}
	fetchHTML := func(ctx context.Context, url string) (string, error) { return responses[url], nil }

	cfg := &config.Config{
		Output: config.OutputCfg{Dir: t.TempDir()},
		Watch: config.WatchConfig{Sites: []config.WatchSite{{
			Name:             "Claude Platform Release Notes",
			Type:             config.WatchTypeAnnouncementPage,
			HomeURL:          "https://docs.claude.com/en/release-notes/overview",
			BriefingCategory: "AI/科技",
			HighValueKeywords: []string{
				"Claude", "Opus", "Sonnet", "API", "release",
			},
		}}},
	}

	indexStore := NewIndexStore(cfg.Output.Dir)
	articleStore := NewArticleStore(cfg.Output.Dir)
	baseline, err := parseAnthropicAnnouncementIndex("Claude Platform Release Notes", "https://platform.claude.com/docs/en/release-notes/overview", oldHome)
	if err != nil {
		t.Fatalf("parseAnthropicAnnouncementIndex() error = %v", err)
	}
	if err := indexStore.Save(IndexState{Categories: map[string]model.WatchIndexSnapshot{
		"Claude Platform Release Notes::Claude Platform Release Notes": baseline,
	}}); err != nil {
		t.Fatalf("indexStore.Save() error = %v", err)
	}
	if err := articleStore.Save(ArticleState{
		"https://platform.claude.com/docs/en/release-notes/overview#february-17-2026": {
			URL:         "https://platform.claude.com/docs/en/release-notes/overview#february-17-2026",
			Title:       "We've launched Claude Sonnet 4.6",
			SummaryHash: hashWatchContent("We've launched Claude Sonnet 4.6, our latest balanced model combining speed and intelligence for everyday tasks. Sonnet 4.6 delivers improved agentic search performance while consuming fewer tokens."),
			BodyHash:    hashWatchContent("We've launched Claude Sonnet 4.6, our latest balanced model combining speed and intelligence for everyday tasks. Sonnet 4.6 delivers improved agentic search performance while consuming fewer tokens. API code execution is now free when used with web search or web fetch."),
		},
	}); err != nil {
		t.Fatalf("articleStore.Save() error = %v", err)
	}

	articles, report, err := runContext(context.Background(), cfg, time.Date(2026, 5, 31, 18, 0, 0, 0, time.UTC), fetchHTML)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(articles) != 1 {
		t.Fatalf("len(articles) = %d, want 1; articles=%#v", len(articles), articles)
	}
	if articles[0].Link != "https://docs.claude.com/en/release-notes/overview#april-16-2026" {
		t.Fatalf("articles[0].Link = %q", articles[0].Link)
	}
	for _, event := range report.Events {
		if event.ArticleURL == "https://docs.claude.com/en/release-notes/overview#february-17-2026" && event.EventType == "new_article" {
			t.Fatalf("existing February entry was reannounced after host migration: %#v", event)
		}
		if event.ArticleURL == "https://platform.claude.com/docs/en/release-notes/overview#february-17-2026" && event.EventType == "removed_article" {
			t.Fatalf("legacy February entry was reported removed after host migration: %#v", event)
		}
	}
}

func TestRunSkipsFailedAnnouncementSiteAndKeepsOtherWatchSites(t *testing.T) {
	responses := map[string]string{
		"https://support.claude.com/zh-CN":                                  mustReadFixture(t, "anthropic/home.html"),
		"https://support.claude.com/zh-CN/collections/4078535-security":     mustReadFixture(t, "anthropic/category_security.html"),
		"https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证": mustReadFixture(t, "anthropic/article_identity_verification.html"),
	}
	fetchHTML := func(ctx context.Context, url string) (string, error) {
		if url == "https://platform.claude.com/docs/en/release-notes/overview" {
			return "", fmt.Errorf("EOF")
		}
		return responses[url], nil
	}

	cfg := &config.Config{
		Output: config.OutputCfg{Dir: t.TempDir()},
		Watch: config.WatchConfig{Sites: []config.WatchSite{
			{
				Name:             "Claude Platform Release Notes",
				Type:             config.WatchTypeAnnouncementPage,
				HomeURL:          "https://platform.claude.com/docs/en/release-notes/overview",
				BriefingCategory: "AI/科技",
				HighValueKeywords: []string{
					"Claude", "Opus", "API",
				},
			},
			{
				Name:              "Anthropic Claude Support",
				Type:              config.WatchTypeAnthropicSupport,
				HomeURL:           "https://support.claude.com/zh-CN",
				BriefingCategory:  "AI/科技",
				CategoryAllowlist: []string{"安全保障"},
				HighValueKeywords: []string{"身份验证", "电话验证"},
			},
		}},
	}

	articles, report, err := runContext(context.Background(), cfg, time.Date(2026, 4, 15, 16, 0, 0, 0, time.UTC), fetchHTML)
	if err != nil {
		t.Fatalf("Run() error = %v, want watch to continue when announcement site fails", err)
	}
	if len(report.Events) != 1 {
		t.Fatalf("len(report.Events) = %d, want 1; events=%#v", len(report.Events), report.Events)
	}
	if report.Events[0].EventType != "site_error" {
		t.Fatalf("report.Events[0].EventType = %q", report.Events[0].EventType)
	}
	if report.Events[0].Source != "Claude Platform Release Notes" {
		t.Fatalf("report.Events[0].Source = %q", report.Events[0].Source)
	}
	if report.Events[0].IncludeInBriefing {
		t.Fatalf("report.Events[0].IncludeInBriefing = true, want false")
	}
	if report.Events[0].Reason != "抓取失败：EOF" {
		t.Fatalf("report.Events[0].Reason = %q", report.Events[0].Reason)
	}
	if len(articles) != 0 {
		t.Fatalf("articles = %#v, want no briefing output on bootstrap", articles)
	}

	indexStore := NewIndexStore(cfg.Output.Dir)
	indexState, err := indexStore.Load()
	if err != nil {
		t.Fatalf("indexStore.Load() error = %v", err)
	}
	if _, ok := indexState.Categories["Anthropic Claude Support::安全保障"]; !ok {
		t.Fatalf("support snapshot missing after announcement failure: %#v", indexState.Categories)
	}
	if _, ok := indexState.Categories["Claude Platform Release Notes::Claude Platform Release Notes"]; ok {
		t.Fatalf("release notes snapshot should not be written on fetch failure: %#v", indexState.Categories)
	}
}

func TestRunBackfillsMissingArticleStateForExistingCategoryBaseline(t *testing.T) {
	categoryHTML := mustReadFixture(t, "anthropic/category_security.html")
	snapshot, err := parseAnthropicCategory("安全保障", "https://support.claude.com/zh-CN/collections/4078535-security", categoryHTML)
	if err != nil {
		t.Fatalf("parseAnthropicCategory() error = %v", err)
	}

	responses := map[string]string{
		"https://support.claude.com/zh-CN":                                  mustReadFixture(t, "anthropic/home.html"),
		"https://support.claude.com/zh-CN/collections/4078535-security":     categoryHTML,
		"https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证": mustReadFixture(t, "anthropic/article_identity_verification.html"),
	}
	fetchHTML := func(ctx context.Context, url string) (string, error) { return responses[url], nil }

	cfg := &config.Config{
		Output: config.OutputCfg{Dir: t.TempDir()},
		Watch: config.WatchConfig{Sites: []config.WatchSite{{
			Name:              "Anthropic Claude Support",
			Type:              config.WatchTypeAnthropicSupport,
			HomeURL:           "https://support.claude.com/zh-CN",
			BriefingCategory:  "AI/科技",
			CategoryAllowlist: []string{"安全保障"},
			HighValueKeywords: []string{"身份验证", "电话验证"},
		}}},
	}

	indexStore := NewIndexStore(cfg.Output.Dir)
	articleStore := NewArticleStore(cfg.Output.Dir)
	if err := indexStore.Save(IndexState{Categories: map[string]model.WatchIndexSnapshot{
		"Anthropic Claude Support::安全保障": snapshot,
	}}); err != nil {
		t.Fatalf("indexStore.Save() error = %v", err)
	}

	articles, report, err := runContext(context.Background(), cfg, time.Date(2026, 4, 15, 16, 0, 0, 0, time.UTC), fetchHTML)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(report.Events) != 0 {
		t.Fatalf("report.Events = %#v, want silent article baseline backfill", report.Events)
	}
	if len(articles) != 0 {
		t.Fatalf("articles = %#v, want no briefing output during article baseline backfill", articles)
	}
	state, err := articleStore.Load()
	if err != nil {
		t.Fatalf("articleStore.Load() error = %v", err)
	}
	seeded, ok := state["https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证"]
	if !ok || seeded.BodyHash == "" {
		t.Fatalf("article state not backfilled: %#v", state)
	}

	responses["https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证"] = `<html><head><title>Claude 上的身份验证</title><meta name="description" content="某些使用场景需要提供政府颁发的身份证件与实时自拍。" /></head><body><article><h1>Claude 上的身份验证</h1><p>某些使用场景需要提供政府颁发的身份证件与实时自拍。</p><p>新增了实时自拍与手机号码交叉校验。</p></article></body></html>`

	articles, report, err = runContext(context.Background(), cfg, time.Date(2026, 4, 16, 17, 0, 0, 0, time.UTC), fetchHTML)
	if err != nil {
		t.Fatalf("Run() second error = %v", err)
	}
	found := false
	for _, event := range report.Events {
		if event.EventType == "content_changed" && event.ArticleURL == "https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证" {
			found = true
		}
	}
	if !found {
		t.Fatalf("second report.Events = %#v", report.Events)
	}
	if len(articles) != 1 {
		t.Fatalf("second articles = %#v", articles)
	}
}

func TestRunDetectsContentChangedForExistingArticle(t *testing.T) {
	responses := map[string]string{
		"https://support.claude.com/zh-CN":                                  mustReadFixture(t, "anthropic/home.html"),
		"https://support.claude.com/zh-CN/collections/4078535-security":     mustReadFixture(t, "anthropic/category_security.html"),
		"https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证": `<html><head><title>Claude 上的身份验证</title><meta name="description" content="某些使用场景需要提供政府颁发的身份证件与实时自拍。" /></head><body><article><h1>Claude 上的身份验证</h1><p>某些使用场景需要提供政府颁发的身份证件与实时自拍。</p><p>新增了实时自拍与手机号码交叉校验。</p></article></body></html>`,
	}
	fetchHTML := func(ctx context.Context, url string) (string, error) { return responses[url], nil }

	cfg := &config.Config{
		Output: config.OutputCfg{Dir: t.TempDir()},
		Watch: config.WatchConfig{Sites: []config.WatchSite{{
			Name:              "Anthropic Claude Support",
			Type:              config.WatchTypeAnthropicSupport,
			HomeURL:           "https://support.claude.com/zh-CN",
			BriefingCategory:  "AI/科技",
			CategoryAllowlist: []string{"安全保障"},
			HighValueKeywords: []string{"身份验证", "电话验证"},
		}}},
	}

	indexStore := NewIndexStore(cfg.Output.Dir)
	articleStore := NewArticleStore(cfg.Output.Dir)
	if err := indexStore.Save(IndexState{Categories: map[string]model.WatchIndexSnapshot{
		"Anthropic Claude Support::安全保障": {
			Scope:     "category",
			Source:    "Anthropic Claude Support",
			Category:  "安全保障",
			URL:       "https://support.claude.com/zh-CN/collections/4078535-security",
			ItemCount: 1,
			Items: []model.WatchIndexItem{{
				Title:    "Claude 上的身份验证",
				URL:      "https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证",
				Position: 1,
				ItemHash: "same-item",
			}},
			Hash: "old-snapshot",
		},
	}}); err != nil {
		t.Fatalf("indexStore.Save() error = %v", err)
	}
	if err := articleStore.Save(ArticleState{
		"https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证": {
			URL:         "https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证",
			Title:       "Claude 上的身份验证",
			SummaryHash: "summary-old",
			BodyHash:    "body-old",
		},
	}); err != nil {
		t.Fatalf("articleStore.Save() error = %v", err)
	}

	articles, report, err := runContext(context.Background(), cfg, time.Date(2026, 4, 15, 16, 0, 0, 0, time.UTC), fetchHTML)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	found := false
	for _, event := range report.Events {
		if event.EventType == "content_changed" {
			found = true
		}
	}
	if !found {
		t.Fatalf("report.Events = %#v", report.Events)
	}
	if len(articles) != 1 {
		t.Fatalf("articles = %#v", articles)
	}
}

func TestRunKeepsArticleCountChangedInSidecarOnly(t *testing.T) {
	responses := map[string]string{
		"https://support.claude.com/zh-CN":                                  mustReadFixture(t, "anthropic/home.html"),
		"https://support.claude.com/zh-CN/collections/4078535-security":     mustReadFixture(t, "anthropic/category_security.html"),
		"https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证": mustReadFixture(t, "anthropic/article_identity_verification.html"),
		"https://support.claude.com/zh-CN/articles/14330000-电话验证":           `<html><head><title>电话验证</title><meta name="description" content="电话验证帮助内容。" /></head><body><article><h1>电话验证</h1><p>电话验证帮助内容。</p></article></body></html>`,
	}
	fetchHTML := func(ctx context.Context, url string) (string, error) { return responses[url], nil }

	cfg := &config.Config{
		Output: config.OutputCfg{Dir: t.TempDir()},
		Watch: config.WatchConfig{Sites: []config.WatchSite{{
			Name:              "Anthropic Claude Support",
			Type:              config.WatchTypeAnthropicSupport,
			HomeURL:           "https://support.claude.com/zh-CN",
			BriefingCategory:  "AI/科技",
			CategoryAllowlist: []string{"安全保障"},
			HighValueKeywords: []string{"仅匹配不存在的词"},
		}}},
	}

	indexStore := NewIndexStore(cfg.Output.Dir)
	if err := indexStore.Save(IndexState{Categories: map[string]model.WatchIndexSnapshot{
		"Anthropic Claude Support::安全保障": {
			Scope:     "category",
			Source:    "Anthropic Claude Support",
			Category:  "安全保障",
			URL:       "https://support.claude.com/zh-CN/collections/4078535-security",
			ItemCount: 1,
			Items: []model.WatchIndexItem{{
				Title:    "Claude 上的身份验证",
				URL:      "https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证",
				Position: 1,
				ItemHash: "same-item",
			}},
			Hash: "old-snapshot",
		},
	}}); err != nil {
		t.Fatalf("indexStore.Save() error = %v", err)
	}

	articles, report, err := runContext(context.Background(), cfg, time.Date(2026, 4, 15, 16, 0, 0, 0, time.UTC), fetchHTML)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	found := false
	for _, event := range report.Events {
		if event.EventType == "article_count_changed" {
			found = true
			if event.IncludeInBriefing {
				t.Fatalf("article_count_changed should stay sidecar only: %#v", event)
			}
		}
	}
	if !found {
		t.Fatalf("report.Events = %#v", report.Events)
	}
	if len(articles) != 0 {
		t.Fatalf("articles = %#v, want sidecar only", articles)
	}
}

func TestRunDeletesArticleStateForRemovedArticle(t *testing.T) {
	responses := map[string]string{
		"https://support.claude.com/zh-CN":                              `<html><body><a href="/zh-CN/collections/4078535-security">安全保障 <span>0 articles</span></a></body></html>`,
		"https://support.claude.com/zh-CN/collections/4078535-security": `<html><body><h1>安全保障</h1></body></html>`,
	}
	fetchHTML := func(ctx context.Context, url string) (string, error) { return responses[url], nil }

	cfg := &config.Config{
		Output: config.OutputCfg{Dir: t.TempDir()},
		Watch: config.WatchConfig{Sites: []config.WatchSite{{
			Name:              "Anthropic Claude Support",
			Type:              config.WatchTypeAnthropicSupport,
			HomeURL:           "https://support.claude.com/zh-CN",
			BriefingCategory:  "AI/科技",
			CategoryAllowlist: []string{"安全保障"},
			HighValueKeywords: []string{"身份验证"},
		}}},
	}

	indexStore := NewIndexStore(cfg.Output.Dir)
	articleStore := NewArticleStore(cfg.Output.Dir)
	removedURL := "https://support.claude.com/zh-CN/articles/old-identity-check"
	if err := indexStore.Save(IndexState{Categories: map[string]model.WatchIndexSnapshot{
		"Anthropic Claude Support::安全保障": {
			Scope:     "category",
			Source:    "Anthropic Claude Support",
			Category:  "安全保障",
			URL:       "https://support.claude.com/zh-CN/collections/4078535-security",
			ItemCount: 1,
			Items: []model.WatchIndexItem{{
				Title:    "旧文章",
				URL:      removedURL,
				Position: 1,
				ItemHash: "old-item",
			}},
			Hash: "old-snapshot",
		},
	}}); err != nil {
		t.Fatalf("indexStore.Save() error = %v", err)
	}
	if err := articleStore.Save(ArticleState{
		removedURL: {
			URL:         removedURL,
			Title:       "旧文章",
			SummaryHash: "old-summary",
			BodyHash:    "old-body",
		},
	}); err != nil {
		t.Fatalf("articleStore.Save() error = %v", err)
	}

	_, report, err := runContext(context.Background(), cfg, time.Date(2026, 4, 15, 16, 0, 0, 0, time.UTC), fetchHTML)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	found := false
	for _, event := range report.Events {
		if event.EventType == "removed_article" && event.ArticleURL == removedURL {
			found = true
		}
	}
	if !found {
		t.Fatalf("report.Events = %#v", report.Events)
	}

	state, err := articleStore.Load()
	if err != nil {
		t.Fatalf("articleStore.Load() error = %v", err)
	}
	if _, ok := state[removedURL]; ok {
		t.Fatalf("removed article state still exists: %#v", state[removedURL])
	}
}

func TestRunWritesSeenStateForContentChangedBriefingArticle(t *testing.T) {
	responses := map[string]string{
		"https://support.claude.com/zh-CN":                                  mustReadFixture(t, "anthropic/home.html"),
		"https://support.claude.com/zh-CN/collections/4078535-security":     mustReadFixture(t, "anthropic/category_security.html"),
		"https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证": `<html><head><title>Claude 上的身份验证</title><meta name="description" content="某些使用场景需要提供政府颁发的身份证件与实时自拍。" /></head><body><article><h1>Claude 上的身份验证</h1><p>某些使用场景需要提供政府颁发的身份证件与实时自拍。</p><p>新增了实时自拍与手机号码交叉校验。</p></article></body></html>`,
	}
	fetchHTML := func(ctx context.Context, url string) (string, error) { return responses[url], nil }

	cfg := &config.Config{
		Output: config.OutputCfg{Dir: t.TempDir()},
		Watch: config.WatchConfig{Sites: []config.WatchSite{{
			Name:              "Anthropic Claude Support",
			Type:              config.WatchTypeAnthropicSupport,
			HomeURL:           "https://support.claude.com/zh-CN",
			BriefingCategory:  "AI/科技",
			CategoryAllowlist: []string{"安全保障"},
			HighValueKeywords: []string{"身份验证", "电话验证"},
		}}},
	}

	indexStore := NewIndexStore(cfg.Output.Dir)
	articleStore := NewArticleStore(cfg.Output.Dir)
	seenStore := NewSeenStore(cfg.Output.Dir)
	if err := indexStore.Save(IndexState{Categories: map[string]model.WatchIndexSnapshot{
		"Anthropic Claude Support::安全保障": {
			Scope:     "category",
			Source:    "Anthropic Claude Support",
			Category:  "安全保障",
			URL:       "https://support.claude.com/zh-CN/collections/4078535-security",
			ItemCount: 1,
			Items: []model.WatchIndexItem{{
				Title:    "Claude 上的身份验证",
				URL:      "https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证",
				Position: 1,
				ItemHash: "same-item",
			}},
			Hash: "old-snapshot",
		},
	}}); err != nil {
		t.Fatalf("indexStore.Save() error = %v", err)
	}
	if err := articleStore.Save(ArticleState{
		"https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证": {
			URL:         "https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证",
			Title:       "Claude 上的身份验证",
			SummaryHash: "summary-old",
			BodyHash:    "body-old",
		},
	}); err != nil {
		t.Fatalf("articleStore.Save() error = %v", err)
	}

	articles, _, err := runContext(context.Background(), cfg, time.Date(2026, 4, 15, 16, 0, 0, 0, time.UTC), fetchHTML)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(articles) != 1 {
		t.Fatalf("len(articles) = %d, want 1", len(articles))
	}
	seen, err := seenStore.Load()
	if err != nil {
		t.Fatalf("seenStore.Load() error = %v", err)
	}
	if len(seen.Items) != 1 {
		t.Fatalf("len(seen.Items) = %d, want 1; items=%#v", len(seen.Items), seen.Items)
	}
	item := seen.Items[0]
	if item.URL != "https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证" {
		t.Fatalf("seen item url = %q", item.URL)
	}
	if item.WatchCategory != "安全保障" || item.EventType != "content_changed" {
		t.Fatalf("seen item = %#v", item)
	}
	if item.Body == "" || item.Summary == "" {
		t.Fatalf("seen item body/summary missing: %#v", item)
	}
}

func TestRunBootstrapDoesNotWriteSeenState(t *testing.T) {
	responses := map[string]string{
		"https://support.claude.com/zh-CN":                                  mustReadFixture(t, "anthropic/home.html"),
		"https://support.claude.com/zh-CN/collections/4078535-security":     mustReadFixture(t, "anthropic/category_security.html"),
		"https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证": mustReadFixture(t, "anthropic/article_identity_verification.html"),
	}
	fetchHTML := func(ctx context.Context, url string) (string, error) { return responses[url], nil }

	cfg := &config.Config{
		Output: config.OutputCfg{Dir: t.TempDir()},
		Watch: config.WatchConfig{Sites: []config.WatchSite{{
			Name:              "Anthropic Claude Support",
			Type:              config.WatchTypeAnthropicSupport,
			HomeURL:           "https://support.claude.com/zh-CN",
			BriefingCategory:  "AI/科技",
			CategoryAllowlist: []string{"安全保障"},
			HighValueKeywords: []string{"身份验证", "电话验证"},
		}}},
	}

	_, _, err := runContext(context.Background(), cfg, time.Date(2026, 4, 15, 16, 0, 0, 0, time.UTC), fetchHTML)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	seenStore := NewSeenStore(cfg.Output.Dir)
	seen, err := seenStore.Load()
	if err != nil {
		t.Fatalf("seenStore.Load() error = %v", err)
	}
	if len(seen.Items) != 0 {
		t.Fatalf("len(seen.Items) = %d, want 0", len(seen.Items))
	}
}

func TestRunContentChangedUpdatesSeenState(t *testing.T) {
	responses := map[string]string{
		"https://support.claude.com/zh-CN":                                  mustReadFixture(t, "anthropic/home.html"),
		"https://support.claude.com/zh-CN/collections/4078535-security":     mustReadFixture(t, "anthropic/category_security.html"),
		"https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证": mustReadFixture(t, "anthropic/article_identity_verification.html"),
	}
	fetchHTML := func(ctx context.Context, url string) (string, error) { return responses[url], nil }

	cfg := &config.Config{
		Output: config.OutputCfg{Dir: t.TempDir()},
		Watch: config.WatchConfig{Sites: []config.WatchSite{{
			Name:              "Anthropic Claude Support",
			Type:              config.WatchTypeAnthropicSupport,
			HomeURL:           "https://support.claude.com/zh-CN",
			BriefingCategory:  "AI/科技",
			CategoryAllowlist: []string{"安全保障"},
			HighValueKeywords: []string{"身份验证", "电话验证"},
		}}},
	}

	indexStore := NewIndexStore(cfg.Output.Dir)
	articleStore := NewArticleStore(cfg.Output.Dir)
	seenStore := NewSeenStore(cfg.Output.Dir)
	if err := indexStore.Save(IndexState{Categories: map[string]model.WatchIndexSnapshot{
		"Anthropic Claude Support::安全保障": {
			Scope:     "category",
			Source:    "Anthropic Claude Support",
			Category:  "安全保障",
			URL:       "https://support.claude.com/zh-CN/collections/4078535-security",
			ItemCount: 1,
			Items: []model.WatchIndexItem{{
				Title:    "Claude 上的身份验证",
				URL:      "https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证",
				Position: 1,
				ItemHash: "same-item",
			}},
			Hash: "old-snapshot",
		},
	}}); err != nil {
		t.Fatalf("indexStore.Save() error = %v", err)
	}
	if err := articleStore.Save(ArticleState{
		"https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证": {
			URL:         "https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证",
			Title:       "Claude 上的身份验证",
			SummaryHash: "summary-old",
			BodyHash:    "body-old",
		},
	}); err != nil {
		t.Fatalf("articleStore.Save() error = %v", err)
	}
	if err := seenStore.Save(model.WatchSeenState{Items: []model.WatchSeenArticle{{
		ID:               "https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证",
		URL:              "https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证",
		Title:            "Claude 上的身份验证",
		Source:           "Anthropic Claude Support",
		BriefingCategory: "AI/科技",
		WatchCategory:    "安全保障",
		Summary:          "旧摘要",
		Body:             "旧正文",
		EventType:        "new_article",
		DetectedAt:       time.Date(2026, 4, 15, 15, 0, 0, 0, time.UTC),
	}}}); err != nil {
		t.Fatalf("seenStore.Save() error = %v", err)
	}

	responses["https://support.claude.com/zh-CN/articles/14328960-claude-上的-身份验证"] = `<html><head><title>Claude 上的身份验证</title><meta name="description" content="某些使用场景需要提供政府颁发的身份证件与实时自拍。" /></head><body><article><h1>Claude 上的身份验证</h1><p>某些使用场景需要提供政府颁发的身份证件与实时自拍。</p><p>新增了实时自拍与手机号码交叉校验。</p></article></body></html>`

	_, _, err := runContext(context.Background(), cfg, time.Date(2026, 4, 15, 16, 0, 0, 0, time.UTC), fetchHTML)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	seen, err := seenStore.Load()
	if err != nil {
		t.Fatalf("seenStore.Load() error = %v", err)
	}
	if len(seen.Items) != 2 {
		t.Fatalf("len(seen.Items) = %d, want 2; items=%#v", len(seen.Items), seen.Items)
	}
	item := seen.Items[1]
	if item.EventType != "content_changed" {
		t.Fatalf("item.EventType = %q, want content_changed", item.EventType)
	}
	if item.Body == "旧正文" || item.DetectedAt.IsZero() {
		t.Fatalf("item = %#v", item)
	}
}

func TestRunWatchCategoryDeepVerifiesOldestDueArticlesConcurrently(t *testing.T) {
	now := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	items := []model.WatchIndexItem{
		{Title: "Article 1", URL: "https://example.com/1", ItemHash: "1"},
		{Title: "Article 2", URL: "https://example.com/2", ItemHash: "2"},
		{Title: "Article 3", URL: "https://example.com/3", ItemHash: "3"},
		{Title: "Article 4", URL: "https://example.com/4", ItemHash: "4"},
	}
	articleState := ArticleState{}
	titles := make(map[string]string, len(items))
	for _, item := range items {
		titles[item.URL] = item.Title
		articleState[item.URL] = model.WatchArticleState{
			URL:           item.URL,
			Title:         item.Title,
			SummaryHash:   hashWatchContent("summary"),
			BodyHash:      hashWatchContent("body"),
			LastCheckedAt: now.Add(-48 * time.Hour),
		}
	}
	indexState := IndexState{Categories: map[string]model.WatchIndexSnapshot{
		"source::category": {
			Category:  "category",
			ItemCount: len(items),
			Items:     items,
		},
	}}
	var mu sync.Mutex
	inFlight := 0
	maxInFlight := 0
	calls := 0
	fetchContent := func(ctx context.Context, url string) (watchArticleContent, error) {
		mu.Lock()
		inFlight++
		calls++
		if inFlight > maxInFlight {
			maxInFlight = inFlight
		}
		mu.Unlock()
		time.Sleep(30 * time.Millisecond)
		mu.Lock()
		inFlight--
		mu.Unlock()
		return watchArticleContent{title: titles[url], summary: "summary", body: "body"}, nil
	}

	articles, seenItems, events, err := runWatchCategory(context.Background(), watchCategoryRun{
		site:                config.WatchSite{Name: "source"},
		now:                 now,
		stateKey:            "source::category",
		current:             indexState.Categories["source::category"],
		indexState:          indexState,
		articleState:        articleState,
		fetchContent:        fetchContent,
		articleConcurrency:  2,
		deepVerifyInterval:  24 * time.Hour,
		deepVerifyBatchSize: len(items),
	})
	if err != nil {
		t.Fatalf("runWatchCategory() error = %v", err)
	}
	if len(articles) != 0 || len(seenItems) != 0 || len(events) != 0 {
		t.Fatalf("articles=%#v seenItems=%#v events=%#v, want no changes", articles, seenItems, events)
	}
	if calls != len(items) {
		t.Fatalf("calls = %d, want %d", calls, len(items))
	}
	if maxInFlight != 2 {
		t.Fatalf("maxInFlight = %d, want configured concurrency 2", maxInFlight)
	}
}

func TestRunWatchCategorySkipsFreshArticleBodies(t *testing.T) {
	now := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	item := model.WatchIndexItem{Title: "Article", URL: "https://example.com/1", ItemHash: "1"}
	indexState := IndexState{Categories: map[string]model.WatchIndexSnapshot{
		"source::category": {Category: "category", ItemCount: 1, Items: []model.WatchIndexItem{item}},
	}}
	articleState := ArticleState{item.URL: {
		URL: item.URL, Title: item.Title,
		SummaryHash: hashWatchContent("summary"), BodyHash: hashWatchContent("body"),
		LastCheckedAt: now.Add(-time.Hour),
	}}
	calls := 0
	_, _, events, err := runWatchCategory(context.Background(), watchCategoryRun{
		site: config.WatchSite{Name: "source"}, now: now, stateKey: "source::category",
		current: indexState.Categories["source::category"], indexState: indexState,
		articleState: articleState, articleConcurrency: 2,
		deepVerifyInterval: 24 * time.Hour, deepVerifyBatchSize: 48,
		fetchContent: func(context.Context, string) (watchArticleContent, error) {
			calls++
			return watchArticleContent{}, nil
		},
	})
	if err != nil {
		t.Fatalf("runWatchCategory() error = %v", err)
	}
	if calls != 0 || len(events) != 0 {
		t.Fatalf("calls=%d events=%#v, want fast index-only path", calls, events)
	}
}

func TestRunWatchCategoryBoundsDeepVerificationBatch(t *testing.T) {
	now := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	items := []model.WatchIndexItem{
		{Title: "oldest", URL: "https://example.com/1", Position: 1, ItemHash: "1"},
		{Title: "middle", URL: "https://example.com/2", Position: 2, ItemHash: "2"},
		{Title: "newest", URL: "https://example.com/3", Position: 3, ItemHash: "3"},
	}
	articleState := ArticleState{}
	for i, item := range items {
		articleState[item.URL] = model.WatchArticleState{URL: item.URL, Title: item.Title,
			SummaryHash: hashWatchContent("summary"), BodyHash: hashWatchContent("body"),
			LastCheckedAt: now.Add(time.Duration(i-4) * 24 * time.Hour)}
	}
	indexState := IndexState{Categories: map[string]model.WatchIndexSnapshot{
		"source::category": {Category: "category", ItemCount: len(items), Items: items},
	}}
	fetched := []string{}
	_, _, _, err := runWatchCategory(context.Background(), watchCategoryRun{
		site: config.WatchSite{Name: "source"}, now: now, stateKey: "source::category",
		current: indexState.Categories["source::category"], indexState: indexState,
		articleState: articleState, articleConcurrency: 1,
		deepVerifyInterval: 24 * time.Hour, deepVerifyBatchSize: 2,
		fetchContent: func(_ context.Context, url string) (watchArticleContent, error) {
			fetched = append(fetched, url)
			state := articleState[url]
			return watchArticleContent{title: state.Title, summary: "summary", body: "body"}, nil
		},
	})
	if err != nil {
		t.Fatalf("runWatchCategory() error = %v", err)
	}
	if !slices.Equal(fetched, []string{items[0].URL, items[1].URL}) {
		t.Fatalf("fetched = %#v, want oldest two", fetched)
	}
}

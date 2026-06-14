package watch

import (
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/fetcher"
	"github.com/walker1211/news-briefing/internal/model"
)

type fetchHTMLFunc func(context.Context, string) (string, error)

type Runner struct {
	httpClient *http.Client
}

func NewRunner(httpClient *http.Client) *Runner {
	if httpClient == nil {
		httpClient = fetcher.DefaultHTTPClient()
	}
	return &Runner{httpClient: httpClient}
}

func fetchWatchHTML(ctx context.Context, url string) (string, error) {
	return fetchWatchHTMLWith(ctx, fetcher.HTTPClient(), url)
}

func (r *Runner) fetchWatchHTML(ctx context.Context, url string) (string, error) {
	return fetchWatchHTMLWith(ctx, r.httpClient, url)
}

type watchFetchRetrySettings struct {
	times int
	wait  time.Duration
}

type nonRetryableWatchFetchError struct {
	err error
}

func (e nonRetryableWatchFetchError) Error() string { return e.err.Error() }

func (e nonRetryableWatchFetchError) Unwrap() error { return e.err }

func fetchWatchHTMLWith(ctx context.Context, client *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("fetch watch page %s: unexpected status %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func watchFetchRetrySettingsFromConfig(cfg *config.Config) watchFetchRetrySettings {
	if cfg == nil || cfg.Fetch.RetryTimes < 1 {
		return watchFetchRetrySettings{times: config.DefaultFetchRetryTimes, wait: config.DefaultFetchRetryWaitTime}
	}
	settings := watchFetchRetrySettings{times: cfg.Fetch.RetryTimes, wait: cfg.Fetch.RetryWaitTime}
	if settings.wait < 0 {
		settings.wait = config.DefaultFetchRetryWaitTime
	}
	return settings
}

func retryingFetchHTML(fetchHTML fetchHTMLFunc, settings watchFetchRetrySettings) fetchHTMLFunc {
	return func(ctx context.Context, url string) (string, error) {
		var lastErr error
		for attempt := 1; attempt <= settings.times; attempt++ {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			html, err := fetchHTML(ctx, url)
			if err == nil {
				return html, nil
			}
			var nonRetryable nonRetryableWatchFetchError
			if errors.As(err, &nonRetryable) {
				return "", nonRetryable.err
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return "", ctxErr
			}
			lastErr = err
			if attempt < settings.times {
				if err := sleepWatchRetry(ctx, settings.wait); err != nil {
					return "", err
				}
			}
		}
		return "", lastErr
	}
}

func sleepWatchRetry(ctx context.Context, d time.Duration) error {
	if d == 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func Run(cfg *config.Config, now time.Time) ([]model.Article, *model.WatchReport, error) {
	return RunContext(context.Background(), cfg, now)
}

func (r *Runner) Run(cfg *config.Config, now time.Time) ([]model.Article, *model.WatchReport, error) {
	return r.RunContext(context.Background(), cfg, now)
}

func RunContext(ctx context.Context, cfg *config.Config, now time.Time) ([]model.Article, *model.WatchReport, error) {
	return runContext(ctx, cfg, now, fetchWatchHTML)
}

func (r *Runner) RunContext(ctx context.Context, cfg *config.Config, now time.Time) ([]model.Article, *model.WatchReport, error) {
	return runContext(ctx, cfg, now, r.fetchWatchHTML)
}

func runContext(ctx context.Context, cfg *config.Config, now time.Time, fetchHTML fetchHTMLFunc) ([]model.Article, *model.WatchReport, error) {
	report := &model.WatchReport{GeneratedAt: now, Events: []model.WatchEvent{}}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if cfg == nil || len(cfg.Watch.Sites) == 0 {
		return nil, report, nil
	}
	if watchProxyProviderEnabled(cfg) {
		directFetchHTML := fetchHTML
		session, err := startBrowseboxProxy(ctx, cfg)
		if err != nil {
			return nil, nil, err
		}
		defer func() { _ = session.Close() }()
		client := watchHTTPClientForProxy(cfg, session.proxyURL)
		proxyFetchHTML := func(ctx context.Context, url string) (string, error) {
			return fetchWatchHTMLWith(ctx, client, url)
		}
		fetchHTML = func(ctx context.Context, url string) (string, error) {
			html, err := proxyFetchHTML(ctx, url)
			if err != nil {
				return "", err
			}
			if !shouldRetryWatchProxyWithDirectFetch(url, html) {
				return html, nil
			}
			fallbackHTML, err := directFetchHTML(ctx, url)
			if err != nil {
				return "", fmt.Errorf("release notes page unavailable via browsebox proxy; fallback fetch failed: %w", err)
			}
			if shouldRetryWatchProxyWithDirectFetch(url, fallbackHTML) {
				return "", nonRetryableWatchFetchError{err: fmt.Errorf("release notes page unavailable: browsebox and fallback proxy both redirected to Claude app unavailable in region page")}
			}
			return fallbackHTML, nil
		}
	}
	fetchHTML = retryingFetchHTML(fetchHTML, watchFetchRetrySettingsFromConfig(cfg))

	indexStore := NewIndexStore(cfg.Output.Dir)
	articleStore := NewArticleStore(cfg.Output.Dir)
	indexState, err := indexStore.Load()
	if err != nil {
		return nil, nil, err
	}
	articleState, err := articleStore.Load()
	if err != nil {
		return nil, nil, err
	}

	articleConcurrency := cfg.Watch.ArticleConcurrency
	if articleConcurrency < 1 {
		articleConcurrency = config.DefaultWatchArticleConcurrency
	}
	articles := make([]model.Article, 0)
	seenItems := make([]model.WatchSeenArticle, 0)
	for _, site := range cfg.Watch.Sites {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		siteArticles, siteSeenItems, events, err := runSite(ctx, site, now, indexState, articleState, fetchHTML, articleConcurrency)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, nil, ctxErr
			}
			if site.Type == config.WatchTypeAnnouncementPage || site.Type == config.WatchTypeAnthropicSupport {
				report.Events = append(report.Events, model.WatchEvent{
					EventType:         "site_error",
					Source:            site.Name,
					Category:          site.Name,
					DetectedAt:        now,
					Reason:            fmt.Sprintf("抓取失败：%v", err),
					IncludeInBriefing: false,
				})
				continue
			}
			return nil, nil, err
		}
		report.Events = append(report.Events, events...)
		articles = append(articles, siteArticles...)
		seenItems = append(seenItems, siteSeenItems...)
	}

	if err := indexStore.Save(indexState); err != nil {
		return nil, nil, err
	}
	if err := articleStore.Save(articleState); err != nil {
		return nil, nil, err
	}
	if err := updateSeenState(cfg.Output.Dir, seenItems); err != nil {
		return nil, nil, err
	}
	return articles, report, nil
}

func runSite(ctx context.Context, site config.WatchSite, now time.Time, indexState IndexState, articleState ArticleState, fetchHTML fetchHTMLFunc, articleConcurrency int) ([]model.Article, []model.WatchSeenArticle, []model.WatchEvent, error) {
	switch site.Type {
	case config.WatchTypeAnthropicSupport:
		return runAnthropicSupportSite(ctx, site, now, indexState, articleState, fetchHTML, articleConcurrency)
	case config.WatchTypeAnnouncementPage:
		return runAnnouncementSite(ctx, site, now, indexState, articleState, fetchHTML, articleConcurrency)
	default:
		return nil, nil, nil, nil
	}
}

func runAnthropicSupportSite(ctx context.Context, site config.WatchSite, now time.Time, indexState IndexState, articleState ArticleState, fetchHTML fetchHTMLFunc, articleConcurrency int) ([]model.Article, []model.WatchSeenArticle, []model.WatchEvent, error) {
	homeHTML, err := fetchHTML(ctx, site.HomeURL)
	if err != nil {
		return nil, nil, nil, err
	}
	allowlist := make(map[string]struct{}, len(site.CategoryAllowlist))
	for _, category := range site.CategoryAllowlist {
		allowlist[category] = struct{}{}
	}
	homeItems, err := parseAnthropicHome(homeHTML, allowlist)
	if err != nil {
		return nil, nil, nil, err
	}
	indexState.Homes[site.Name] = model.WatchIndexSnapshot{
		Scope:      "home",
		Source:     site.Name,
		URL:        site.HomeURL,
		SnapshotAt: now,
		ItemCount:  len(homeItems),
		Items:      homeItems,
		Hash:       hashSnapshotItems(homeItems),
	}

	fetchContent := func(ctx context.Context, url string) (watchArticleContent, error) {
		articleHTML, err := fetchHTML(ctx, url)
		if err != nil {
			return watchArticleContent{}, err
		}
		title, summary, body, err := parseAnthropicArticle(articleHTML)
		if err != nil {
			return watchArticleContent{}, err
		}
		return watchArticleContent{title: title, summary: summary, body: body}, nil
	}

	articles := make([]model.Article, 0)
	seenItems := make([]model.WatchSeenArticle, 0)
	events := make([]model.WatchEvent, 0)
	for _, categoryItem := range homeItems {
		categoryHTML, err := fetchHTML(ctx, categoryItem.URL)
		if err != nil {
			return nil, nil, nil, err
		}
		current, err := parseAnthropicCategory(categoryItem.Title, categoryItem.URL, categoryHTML)
		if err != nil {
			return nil, nil, nil, err
		}
		current.Source = site.Name
		current.SnapshotAt = now

		categoryArticles, categorySeenItems, categoryEvents, err := runWatchCategory(ctx, watchCategoryRun{
			site:               site,
			now:                now,
			stateKey:           watchCategoryStateKey(site.Name, current.Category),
			current:            current,
			indexState:         indexState,
			articleState:       articleState,
			fetchContent:       fetchContent,
			articleConcurrency: articleConcurrency,
		})
		if err != nil {
			return nil, nil, nil, err
		}
		articles = append(articles, categoryArticles...)
		seenItems = append(seenItems, categorySeenItems...)
		events = append(events, categoryEvents...)
	}

	return articles, seenItems, events, nil
}

func watchCategoryStateKey(source, category string) string {
	return source + "::" + category
}

func defaultWatchReason(eventType string, title string) string {
	switch eventType {
	case "new_article":
		return fmt.Sprintf("新增文章：%s", title)
	case "removed_article":
		return fmt.Sprintf("文章下线：%s", title)
	case "title_changed":
		return fmt.Sprintf("文章标题变化：%s", title)
	case "article_count_changed":
		return "分类文章总数变化"
	case "content_changed":
		return fmt.Sprintf("正文发生变化：%s", title)
	default:
		return title
	}
}

func applyWatchEventPriority(event *model.WatchEvent) {
	if event.Reason == "" {
		event.Reason = defaultWatchReason(event.EventType, event.ArticleTitle)
	}
	if event.EventType == "article_count_changed" {
		event.IncludeInBriefing = false
		return
	}
	if len(event.MatchedKeywords) == 0 {
		event.IncludeInBriefing = false
		return
	}
	event.IncludeInBriefing = true
	event.Reason = fmt.Sprintf("命中高价值关键词：%s", strings.Join(event.MatchedKeywords, ", "))
}

func applyWatchEventPriorityAt(event *model.WatchEvent, now time.Time) {
	applyWatchEventPriority(event)
	if !event.IncludeInBriefing || !isStaleClaudeReleaseNotesOverviewEntry(event.ArticleURL, now) {
		return
	}
	event.IncludeInBriefing = false
	event.Reason += "；旧日期锚点仅记录在 watch 报告"
}

func matchedWatchKeywords(text string, keywords []string) []string {
	lower := strings.ToLower(text)
	matched := make([]string, 0)
	for _, keyword := range keywords {
		keyword = strings.TrimSpace(keyword)
		if keyword == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(keyword)) {
			matched = append(matched, keyword)
		}
	}
	return slices.Compact(matched)
}

func hashWatchContent(value string) string {
	sum := sha1.Sum([]byte(value))
	return fmt.Sprintf("%x", sum[:])
}

func updateSeenState(outputDir string, items []model.WatchSeenArticle) error {
	store := NewSeenStore(outputDir)
	state, err := store.Load()
	if err != nil {
		return err
	}
	if state.Items == nil {
		state.Items = []model.WatchSeenArticle{}
	}
	for _, item := range items {
		if item.URL == "" {
			continue
		}
		state.Items = append(state.Items, item)
	}
	return store.Save(state)
}

func watchEventToSeenArticle(site config.WatchSite, event model.WatchEvent, summary string, body string) model.WatchSeenArticle {
	return model.WatchSeenArticle{
		ID:               event.ArticleURL,
		URL:              event.ArticleURL,
		Title:            event.ArticleTitle,
		Source:           site.Name,
		BriefingCategory: site.BriefingCategory,
		WatchCategory:    event.Category,
		Summary:          summary,
		Body:             body,
		EventType:        event.EventType,
		DetectedAt:       event.DetectedAt,
	}
}

func watchEventToArticle(site config.WatchSite, event model.WatchEvent) model.Article {
	title := fmt.Sprintf("%s 文档更新：%s", site.Name, event.ArticleTitle)
	summary := event.Reason
	if summary == "" {
		summary = fmt.Sprintf("%s 出现 %s 事件", event.ArticleTitle, event.EventType)
	}
	return model.Article{
		Title:     title,
		Link:      event.ArticleURL,
		Summary:   summary,
		Source:    site.Name + " Watch",
		Category:  site.BriefingCategory,
		Published: event.DetectedAt,
	}
}

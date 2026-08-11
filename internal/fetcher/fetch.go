package fetcher

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/model"
)

const (
	maxRetries     = config.DefaultFetchRetryTimes
	redditDelayMin = 2 * time.Second
	redditDelayMax = 4 * time.Second
)

type fetchRetrySettings struct {
	times int
	wait  time.Duration
}

type sleepFunc func(context.Context, time.Duration) error
type delayFunc func() time.Duration

type redditSourceDelayOptions struct {
	fallback delayFunc
	jitter   delayFunc
	maxWait  time.Duration
}

func randomDuration(min, max time.Duration) time.Duration {
	return min + time.Duration(rand.Int64N(int64(max-min)+1))
}

func randomRedditDelay() time.Duration {
	return randomDuration(redditDelayMin, redditDelayMax)
}

func redditSourceDelayFromConfig(cfg *config.Config) delayFunc {
	min := config.DefaultFetchRedditSourceDelayMin
	max := config.DefaultFetchRedditSourceDelayMax
	if cfg != nil && cfg.Fetch.RedditSourceDelayMin > 0 && cfg.Fetch.RedditSourceDelayMax >= cfg.Fetch.RedditSourceDelayMin {
		min = cfg.Fetch.RedditSourceDelayMin
		max = cfg.Fetch.RedditSourceDelayMax
	}
	return func() time.Duration {
		return randomDuration(min, max)
	}
}

func redditSourceDelayOptionsFromConfig(cfg *config.Config) redditSourceDelayOptions {
	maxWait := config.DefaultFetchRedditRateLimitWaitMax
	if cfg != nil && cfg.Fetch.RedditRateLimitWaitMax > 0 {
		maxWait = cfg.Fetch.RedditRateLimitWaitMax
	}
	return redditSourceDelayOptions{
		fallback: redditSourceDelayFromConfig(cfg),
		jitter:   randomRedditDelay,
		maxWait:  maxWait,
	}
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func defaultFetchRetrySettings() fetchRetrySettings {
	return fetchRetrySettings{times: config.DefaultFetchRetryTimes, wait: config.DefaultFetchRetryWaitTime}
}

func fetchRetrySettingsFromConfig(cfg *config.Config) fetchRetrySettings {
	if cfg == nil || cfg.Fetch.RetryTimes < 1 {
		return defaultFetchRetrySettings()
	}
	settings := fetchRetrySettings{times: cfg.Fetch.RetryTimes, wait: cfg.Fetch.RetryWaitTime}
	if settings.wait < 0 {
		settings.wait = config.DefaultFetchRetryWaitTime
	}
	return settings
}

type fetchedCandidate struct {
	Article         model.Article
	MatchedKeywords []string
}

type sourceFetchResult struct {
	Source              config.Source
	Candidates          []fetchedCandidate
	FetchedCount        int
	RedditRateLimitWait time.Duration
}

type sourceFetchFunc func(context.Context, config.Source, []string, time.Time) (sourceFetchResult, error)
type fetchAllSourcesDetailedFunc func(context.Context, *config.Config, time.Time) ([]sourceFetchResult, []FailedSource, error)

type sourceFetchers struct {
	rss        sourceFetchFunc
	hackernews sourceFetchFunc
	reddit     sourceFetchFunc
	docsPage   sourceFetchFunc
	repoPage   sourceFetchFunc
}

type curlFetchFunc func(context.Context, string) ([]byte, error)

type Client struct {
	httpClient *http.Client
	fetchCurl  curlFetchFunc
}

func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = DefaultHTTPClient()
	}
	return &Client{httpClient: httpClient, fetchCurl: fetchFeedWithCurlContext}
}

func fetchRSSSource(ctx context.Context, src config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
	return FetchRSSContext(ctx, src, keywords, since)
}

func fetchHackerNewsSource(ctx context.Context, src config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
	return FetchHackerNewsContext(ctx, src, keywords, since)
}

func fetchRedditDirect(ctx context.Context, src config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
	return FetchRedditContext(ctx, src, keywords, since)
}

func fetchRedditSource(ctx context.Context, src config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
	return fetchWithRetry(ctx, src, keywords, since)
}

func fetchDocsPageSource(ctx context.Context, src config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
	return FetchDocsPageContext(ctx, src, keywords, since)
}

func fetchRepoPageSource(ctx context.Context, src config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
	return FetchRepoPageContext(ctx, src, keywords, since)
}

func defaultSourceFetchers() sourceFetchers {
	return sourceFetchers{
		rss:        fetchRSSSource,
		hackernews: fetchHackerNewsSource,
		reddit:     fetchRedditDirect,
		docsPage:   fetchDocsPageSource,
		repoPage:   fetchRepoPageSource,
	}
}

func serialSourceFetchers() sourceFetchers {
	fetchers := defaultSourceFetchers()
	fetchers.reddit = fetchRedditSource
	return fetchers
}

func shouldRateLimitAsReddit(src config.Source) bool {
	return src.Type == config.SourceTypeReddit || isRedditURL(src.URL)
}

// isRateLimited 检测是否为 429 限流响应
func isRateLimited(err error) bool {
	return err != nil && strings.Contains(err.Error(), "429")
}

type FailedSource struct {
	Name string
	Err  error
}

type FetchResult struct {
	Articles         []model.Article
	FilteredArticles []model.Article
	Failed           []FailedSource
	SourceStats      model.SourceStatsReport
}

type sourceStatsAccumulator struct {
	entries          map[string]*model.SourceStatsEntry
	sourceTypes      map[string]string
	sourceCategories map[string]string
	sourceURLs       map[string]string
	from             time.Time
	to               time.Time
}

type filterContext struct {
	cfg     *config.Config
	sources map[string]config.Source
}

type includeKeywordRule struct {
	strong  []string
	weak    []string
	minWeak int
}

func newFilterContext(cfg *config.Config) filterContext {
	ctx := filterContext{cfg: cfg, sources: map[string]config.Source{}}
	if cfg == nil {
		return ctx
	}
	for _, source := range cfg.Sources {
		ctx.sources[source.Name] = source
	}
	return ctx
}

func newSourceStatsAccumulator(cfg *config.Config, from, to time.Time) *sourceStatsAccumulator {
	acc := &sourceStatsAccumulator{
		entries:          map[string]*model.SourceStatsEntry{},
		sourceTypes:      map[string]string{},
		sourceCategories: map[string]string{},
		sourceURLs:       map[string]string{},
		from:             from,
		to:               to,
	}
	if cfg == nil {
		return acc
	}
	for _, source := range cfg.Sources {
		acc.sourceTypes[source.Name] = source.Type
		acc.sourceCategories[source.Name] = source.Category
		acc.sourceURLs[source.Name] = source.URL
	}
	return acc
}

func fetchAllSourcesDetailed(ctx context.Context, cfg *config.Config, since time.Time) ([]sourceFetchResult, []FailedSource, error) {
	return fetchAllSourcesDetailedWith(ctx, cfg, since, serialSourceFetchers(), sleepContext)
}

// FetchAll 并发抓取所有新闻源，支持重试。
// 返回文章列表、失败源列表和错误。
func FetchAll(cfg *config.Config, markSeen bool) ([]model.Article, []FailedSource, error) {
	return FetchAllContext(context.Background(), cfg, markSeen)
}

func (c *Client) FetchAll(cfg *config.Config, markSeen bool) ([]model.Article, []FailedSource, error) {
	return c.FetchAllContext(context.Background(), cfg, markSeen)
}

func FetchAllContext(ctx context.Context, cfg *config.Config, markSeen bool) ([]model.Article, []FailedSource, error) {
	result, err := FetchAllDetailedContext(ctx, cfg, markSeen)
	return result.Articles, result.Failed, err
}

func (c *Client) FetchAllContext(ctx context.Context, cfg *config.Config, markSeen bool) ([]model.Article, []FailedSource, error) {
	result, err := c.FetchAllDetailedContext(ctx, cfg, markSeen)
	return result.Articles, result.Failed, err
}

func FetchAllDetailed(cfg *config.Config, markSeen bool) (FetchResult, error) {
	return FetchAllDetailedContext(context.Background(), cfg, markSeen)
}

func (c *Client) FetchAllDetailed(cfg *config.Config, markSeen bool) (FetchResult, error) {
	return c.FetchAllDetailedContext(context.Background(), cfg, markSeen)
}

func FetchAllDetailedContext(ctx context.Context, cfg *config.Config, markSeen bool) (FetchResult, error) {
	now := time.Now()
	since := now.Add(-12 * time.Hour)
	return fetchWindowDetailedContextWithXLookback(ctx, cfg, since, now, markSeen, false, fetchAllSourcesDetailed)
}

func (c *Client) FetchAllDetailedContext(ctx context.Context, cfg *config.Config, markSeen bool) (FetchResult, error) {
	now := time.Now()
	since := now.Add(-12 * time.Hour)
	return fetchWindowDetailedContextWithXLookback(ctx, cfg, since, now, markSeen, false, c.fetchAllSourcesDetailed)
}

func FetchWindow(cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []FailedSource, error) {
	return FetchWindowContext(context.Background(), cfg, from, to, markSeen, ignoreSeen)
}

func (c *Client) FetchWindow(cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []FailedSource, error) {
	return c.FetchWindowContext(context.Background(), cfg, from, to, markSeen, ignoreSeen)
}

func FetchWindowContext(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []FailedSource, error) {
	result, err := FetchWindowDetailedContext(ctx, cfg, from, to, markSeen, ignoreSeen)
	return result.Articles, result.Failed, err
}

func (c *Client) FetchWindowContext(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []FailedSource, error) {
	result, err := c.FetchWindowDetailedContext(ctx, cfg, from, to, markSeen, ignoreSeen)
	return result.Articles, result.Failed, err
}

func FetchWindowDetailed(cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) (FetchResult, error) {
	return FetchWindowDetailedContext(context.Background(), cfg, from, to, markSeen, ignoreSeen)
}

func (c *Client) FetchWindowDetailed(cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) (FetchResult, error) {
	return c.FetchWindowDetailedContext(context.Background(), cfg, from, to, markSeen, ignoreSeen)
}

func FetchWindowDetailedContext(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) (FetchResult, error) {
	return fetchWindowDetailedContext(ctx, cfg, from, to, markSeen, ignoreSeen, fetchAllSourcesDetailed)
}

func (c *Client) FetchWindowDetailedContext(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) (FetchResult, error) {
	return fetchWindowDetailedContext(ctx, cfg, from, to, markSeen, ignoreSeen, c.fetchAllSourcesDetailed)
}

func FetchWindowDetailedWithXVisibleHistoryContext(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool, historyDir string) (FetchResult, error) {
	return fetchWindowDetailedContextWithOptions(ctx, cfg, from, to, markSeen, ignoreSeen, fetchAllSourcesDetailed, false, xVisibleReadOptions{useHistory: true, historyDir: historyDir})
}

func (c *Client) FetchWindowDetailedWithXVisibleHistoryContext(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool, historyDir string) (FetchResult, error) {
	return fetchWindowDetailedContextWithOptions(ctx, cfg, from, to, markSeen, ignoreSeen, c.fetchAllSourcesDetailed, false, xVisibleReadOptions{useHistory: true, historyDir: historyDir})
}

func fetchWindowContext(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool, fetchAll fetchAllSourcesDetailedFunc) ([]model.Article, []FailedSource, error) {
	result, err := fetchWindowDetailedContext(ctx, cfg, from, to, markSeen, ignoreSeen, fetchAll)
	return result.Articles, result.Failed, err
}

func fetchWindowDetailedContext(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool, fetchAll fetchAllSourcesDetailedFunc) (FetchResult, error) {
	return fetchWindowDetailedContextWithOptions(ctx, cfg, from, to, markSeen, ignoreSeen, fetchAll, false)
}

func fetchWindowDetailedContextWithXLookback(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool, fetchAll fetchAllSourcesDetailedFunc) (FetchResult, error) {
	return fetchWindowDetailedContextWithOptions(ctx, cfg, from, to, markSeen, ignoreSeen, fetchAll, true)
}

func (acc *sourceStatsAccumulator) statForArticle(article model.Article, fallback config.Source) *model.SourceStatsEntry {
	if acc == nil {
		return &model.SourceStatsEntry{}
	}
	sourceName := articleSourceName(article, fallback)
	entry := acc.entries[sourceName]
	if entry != nil {
		return entry
	}
	category := strings.TrimSpace(article.Category)
	if category == "" {
		category = strings.TrimSpace(fallback.Category)
	}
	if category == "" {
		category = acc.sourceCategories[sourceName]
	}
	entry = &model.SourceStatsEntry{
		Source:   sourceName,
		Type:     acc.sourceKind(sourceName, fallback),
		Category: category,
	}
	acc.entries[sourceName] = entry
	return entry
}

func (acc *sourceStatsAccumulator) countFetched(result sourceFetchResult) {
	if result.FetchedCount > 0 {
		acc.statForArticle(model.Article{}, result.Source).Fetched += result.FetchedCount
		return
	}
	for _, candidate := range result.Candidates {
		acc.statForArticle(candidate.Article, result.Source).Fetched++
	}
}

func (acc *sourceStatsAccumulator) sourceKind(sourceName string, fallback config.Source) string {
	if strings.HasPrefix(sourceName, "X/") || strings.HasPrefix(sourceName, "X Search/") {
		return "x.com"
	}
	sourceType := strings.TrimSpace(fallback.Type)
	if sourceType == "" {
		sourceType = acc.sourceTypes[sourceName]
	}
	sourceURL := strings.TrimSpace(fallback.URL)
	if sourceURL == "" {
		sourceURL = acc.sourceURLs[sourceName]
	}
	if sourceType == config.SourceTypeRSS && isRedditURL(sourceURL) {
		return "reddit"
	}
	if sourceType != "" {
		return sourceType
	}
	return "unknown"
}

func (acc *sourceStatsAccumulator) build(failed []FailedSource) model.SourceStatsReport {
	report := model.SourceStatsReport{
		SchemaVersion: "source-stats/v1",
		SourceApp:     "news-briefing",
		GeneratedAt:   time.Now(),
		Window:        model.SourceStatsWindow{From: acc.from, To: acc.to},
	}
	for _, entry := range acc.entries {
		report.Sources = append(report.Sources, *entry)
	}
	sort.Slice(report.Sources, func(i, j int) bool {
		if report.Sources[i].Category == report.Sources[j].Category {
			return report.Sources[i].Source < report.Sources[j].Source
		}
		return report.Sources[i].Category < report.Sources[j].Category
	})
	for _, item := range failed {
		if strings.TrimSpace(item.Name) == "" {
			continue
		}
		message := item.SafeErrorMessage()
		report.Failed = append(report.Failed, model.SourceStatsError{Source: item.Name, Error: message})
	}
	report.RecalculateTotals()
	return report
}

func articleSourceName(article model.Article, fallback config.Source) string {
	source := strings.TrimSpace(article.Source)
	if source != "" {
		return source
	}
	source = strings.TrimSpace(fallback.Name)
	if source != "" {
		return source
	}
	return "unknown"
}

func (ctx filterContext) includeKeywords(article model.Article, fallback config.Source) []string {
	rule := ctx.includeKeywordRule(article, fallback)
	out := make([]string, 0, len(rule.strong)+len(rule.weak))
	out = append(out, rule.strong...)
	out = append(out, rule.weak...)
	return out
}

func (ctx filterContext) includeKeywordRule(article model.Article, fallback config.Source) includeKeywordRule {
	if ctx.cfg == nil {
		return includeKeywordRule{}
	}
	sourceName := articleSourceName(article, fallback)
	if sourceFilter, ok := ctx.cfg.Filters.Sources[sourceName]; ok && len(sourceFilter.IncludeKeywords) > 0 {
		return includeKeywordRule{strong: sourceFilter.IncludeKeywords}
	}
	if source, ok := ctx.sources[sourceName]; ok && len(source.Keywords) > 0 {
		return includeKeywordRule{strong: source.Keywords}
	}
	category := filterCategory(article, fallback, ctx.sources[sourceName])
	if categoryFilter, ok := ctx.cfg.Filters.Categories[category]; ok && (len(categoryFilter.IncludeKeywords) > 0 || len(categoryFilter.WeakKeywords) > 0) {
		minWeak := categoryFilter.MinWeakKeywordMatches
		if minWeak == 0 {
			minWeak = 2
		}
		return includeKeywordRule{strong: categoryFilter.IncludeKeywords, weak: categoryFilter.WeakKeywords, minWeak: minWeak}
	}
	return includeKeywordRule{strong: ctx.cfg.Keywords}
}

func (ctx filterContext) excludeKeywords(article model.Article, fallback config.Source) []string {
	if ctx.cfg == nil {
		return nil
	}
	sourceName := articleSourceName(article, fallback)
	category := filterCategory(article, fallback, ctx.sources[sourceName])
	var out []string
	if categoryFilter, ok := ctx.cfg.Filters.Categories[category]; ok {
		out = append(out, categoryFilter.ExcludeKeywords...)
	}
	if sourceFilter, ok := ctx.cfg.Filters.Sources[sourceName]; ok {
		out = append(out, sourceFilter.ExcludeKeywords...)
	}
	return out
}

func (ctx filterContext) maxArticlesForSource(article model.Article) int {
	if ctx.cfg == nil {
		return 0
	}
	sourceName := articleSourceName(article, config.Source{})
	filter, ok := ctx.cfg.Filters.Sources[sourceName]
	if !ok {
		return 0
	}
	return filter.MaxArticles
}

func filterCategory(article model.Article, fallback config.Source, source config.Source) string {
	category := strings.TrimSpace(article.Category)
	if category != "" {
		return category
	}
	category = strings.TrimSpace(source.Category)
	if category != "" {
		return category
	}
	return strings.TrimSpace(fallback.Category)
}

func articleKeywordText(article model.Article) string {
	return article.Title + " " + article.Summary
}

func filterCandidate(article model.Article, fallback config.Source, filters filterContext) (matched []string, excluded []string) {
	text := articleKeywordText(article)
	rule := filters.includeKeywordRule(article, fallback)
	strongMatches := matchedKeywords(text, rule.strong)
	weakMatches := matchedKeywords(text, rule.weak)
	if len(strongMatches) > 0 || (rule.minWeak > 0 && len(weakMatches) >= rule.minWeak) {
		matched = append(strongMatches, weakMatches...)
	}
	excluded = matchedKeywords(text, filters.excludeKeywords(article, fallback))
	return matched, excluded
}

func applySourceLimits(articles []model.Article, filtered []model.Article, filters filterContext, stats *sourceStatsAccumulator) ([]model.Article, []model.Article) {
	if len(articles) == 0 {
		return articles, filtered
	}
	out := make([]model.Article, 0, len(articles))
	counts := make(map[string]int)
	for _, article := range articles {
		sourceName := articleSourceName(article, config.Source{})
		maxArticles := filters.maxArticlesForSource(article)
		if maxArticles > 0 && counts[sourceName] >= maxArticles {
			stat := stats.statForArticle(article, config.Source{})
			stat.Filtered++
			stat.FilteredSourceLimit++
			filtered = append(filtered, article)
			continue
		}
		out = append(out, article)
		counts[sourceName]++
		stats.statForArticle(article, config.Source{}).AcceptedBeforeDedup++
	}
	return out, filtered
}

func fetchWindowDetailedContextWithOptions(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool, fetchAll fetchAllSourcesDetailedFunc, useXLookback bool, xVisibleOptions ...xVisibleReadOptions) (FetchResult, error) {
	if err := ctx.Err(); err != nil {
		return FetchResult{}, err
	}
	results, failed, err := fetchAll(ctx, cfg, from)
	if err != nil {
		return FetchResult{}, err
	}
	xFrom := from
	if useXLookback && cfg.XAccounts.Lookback > 0 {
		xFrom = to.Add(-cfg.XAccounts.Lookback)
	}
	xReadOptions := xVisibleReadOptions{}
	if len(xVisibleOptions) > 0 {
		xReadOptions = xVisibleOptions[0]
	}
	filters := newFilterContext(cfg)
	xResults, xFailed, err := fetchXVisibleNDJSONWithOptions(ctx, cfg.XAccounts, filters.includeKeywords(model.Article{Category: cfg.XAccounts.Category}, config.Source{Name: xVisibleSourceName, Category: cfg.XAccounts.Category}), xFrom, to, xReadOptions)
	if err != nil {
		return FetchResult{}, err
	}
	results = append(results, xResults...)
	failed = append(failed, xFailed...)
	if err := ctx.Err(); err != nil {
		return FetchResult{}, err
	}

	accepted := make([]model.Article, 0)
	filtered := make([]model.Article, 0)
	stats := newSourceStatsAccumulator(cfg, from, to)
	for _, result := range results {
		stats.countFetched(result)
		windowFrom := from
		if result.Source.Name == xVisibleSourceName {
			windowFrom = xFrom
		}
		for _, candidate := range result.Candidates {
			if !articleWithinWindow(candidate.Article, windowFrom, to) {
				continue
			}
			stat := stats.statForArticle(candidate.Article, result.Source)
			stat.InWindow++
			matched, excluded := filterCandidate(candidate.Article, result.Source, filters)
			if len(matched) == 0 && len(candidate.MatchedKeywords) > 0 && len(filters.includeKeywords(candidate.Article, result.Source)) == 0 {
				matched = candidate.MatchedKeywords
			}
			if len(matched) == 0 {
				stat.Filtered++
				stat.FilteredKeywordMiss++
				filtered = append(filtered, candidate.Article)
				continue
			}
			stat.KeywordMatched++
			if len(excluded) > 0 {
				stat.Filtered++
				stat.FilteredExcluded++
				filtered = append(filtered, candidate.Article)
				continue
			}
			accepted = append(accepted, candidate.Article)
		}
	}

	sort.Slice(accepted, func(i, j int) bool {
		return accepted[i].Published.After(accepted[j].Published)
	})
	accepted, filtered = applySourceLimits(accepted, filtered, filters, stats)
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Published.After(filtered[j].Published)
	})

	if err := ctx.Err(); err != nil {
		return FetchResult{}, err
	}

	outcome, err := applyDedupContext(ctx, accepted, markSeen, ignoreSeen, NewSeenStore(cfg.Output.Dir))
	if err != nil {
		return FetchResult{FilteredArticles: filtered, Failed: failed, SourceStats: stats.build(failed)}, err
	}
	for _, article := range outcome.Articles {
		stats.statForArticle(article, config.Source{}).AcceptedAfterDedup++
	}
	return FetchResult{Articles: outcome.Articles, FilteredArticles: filtered, Failed: failed, SourceStats: stats.build(failed)}, nil
}

func MarkArticlesSeen(outputDir string, articles []model.Article) error {
	if len(articles) == 0 {
		return nil
	}
	_, err := Dedup(articles, true, NewSeenStore(outputDir))
	return err
}

func articleWithinWindow(a model.Article, from, to time.Time) bool {
	return !a.Published.Before(from) && a.Published.Before(to)
}

func filterArticlesByWindow(articles []model.Article, from, to time.Time) []model.Article {
	var result []model.Article
	for _, a := range articles {
		if !articleWithinWindow(a, from, to) {
			continue
		}
		result = append(result, a)
	}
	return result
}

func applyDedup(articles []model.Article, markSeen bool, ignoreSeen bool, store SeenStore) (DedupOutcome, error) {
	return applyDedupContext(context.Background(), articles, markSeen, ignoreSeen, store)
}

func applyDedupContext(ctx context.Context, articles []model.Article, markSeen bool, ignoreSeen bool, store SeenStore) (DedupOutcome, error) {
	if err := ctx.Err(); err != nil {
		return DedupOutcome{}, err
	}
	if ignoreSeen {
		return DedupInBatch(articles), nil
	}
	return DedupContext(ctx, articles, markSeen, store)
}

func (c *Client) fetchAllSourcesDetailed(ctx context.Context, cfg *config.Config, since time.Time) ([]sourceFetchResult, []FailedSource, error) {
	return fetchAllSourcesDetailedWith(ctx, cfg, since, c.serialSourceFetchers(sleepContext), sleepContext)
}

func (c *Client) serialSourceFetchers(sleep sleepFunc) sourceFetchers {
	fetchers := c.sourceFetchers()
	fetchers.reddit = func(ctx context.Context, src config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
		return fetchWithRetryUsing(ctx, src, keywords, since, c.sourceFetchers(), sleep)
	}
	return fetchers
}

func (c *Client) sourceFetchers() sourceFetchers {
	return sourceFetchers{
		rss:        c.FetchRSSContext,
		hackernews: c.FetchHackerNewsContext,
		reddit:     c.FetchRedditContext,
		docsPage:   c.FetchDocsPageContext,
		repoPage:   c.FetchRepoPageContext,
	}
}

func fetchAllSourcesDetailedWith(ctx context.Context, cfg *config.Config, since time.Time, fetchers sourceFetchers, sleep sleepFunc) ([]sourceFetchResult, []FailedSource, error) {
	var (
		mu     sync.Mutex
		all    []sourceFetchResult
		failed []FailedSource
		wg     sync.WaitGroup
	)

	var redditSources []config.Source
	var otherSources []config.Source
	filters := newFilterContext(cfg)
	for _, src := range cfg.Sources {
		if shouldRateLimitAsReddit(src) {
			redditSources = append(redditSources, src)
		} else {
			otherSources = append(otherSources, src)
		}
	}

	retrySettings := fetchRetrySettingsFromConfig(cfg)
	for _, src := range otherSources {
		wg.Go(func() {
			if err := ctx.Err(); err != nil {
				mu.Lock()
				failed = append(failed, FailedSource{Name: src.Name, Err: err})
				mu.Unlock()
				return
			}
			result, err := fetchWithRetryUsing(ctx, src, filters.includeKeywords(model.Article{Category: src.Category, Source: src.Name}, src), since, fetchers, sleep, retrySettings)
			mu.Lock()
			if err != nil {
				failed = append(failed, FailedSource{Name: src.Name, Err: err})
			} else {
				all = append(all, result)
			}
			mu.Unlock()
		})
	}

	if len(redditSources) > 0 {
		wg.Go(func() {
			fetchRedditSource := func(ctx context.Context, src config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
				if src.Type == config.SourceTypeReddit {
					return fetchers.reddit(ctx, src, keywords, since)
				}
				return fetchWithRetryUsing(ctx, src, keywords, since, fetchers, sleep, retrySettings)
			}
			fetchRedditSourcesSeriallyWith(ctx, redditSources, nil, since, func(ctx context.Context, src config.Source, _ []string, since time.Time) (sourceFetchResult, error) {
				return fetchRedditSource(ctx, src, filters.includeKeywords(model.Article{Category: src.Category, Source: src.Name}, src), since)
			}, sleep, redditSourceDelayOptionsFromConfig(cfg), func(item FailedSource) {
				mu.Lock()
				failed = append(failed, item)
				mu.Unlock()
			}, func(item sourceFetchResult) {
				mu.Lock()
				all = append(all, item)
				mu.Unlock()
			})
		})
	}

	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return all, failed, nil
}

func fetchRedditSourcesSeriallyWith(ctx context.Context, sources []config.Source, keywords []string, since time.Time, fetchReddit sourceFetchFunc, sleep sleepFunc, delays redditSourceDelayOptions, appendFailed func(FailedSource), appendResult func(sourceFetchResult)) {
	nextDelay := delays.fallback()
	for i, src := range sources {
		if err := ctx.Err(); err != nil {
			appendFailed(FailedSource{Name: src.Name, Err: err})
			return
		}
		if i > 0 {
			if delays.maxWait > 0 && nextDelay > delays.maxWait {
				appendFailed(FailedSource{Name: src.Name, Err: fmt.Errorf("reddit rate limit wait %s exceeds max %s", nextDelay, delays.maxWait)})
				return
			}
			if err := sleep(ctx, nextDelay); err != nil {
				appendFailed(FailedSource{Name: src.Name, Err: err})
				return
			}
		}
		result, err := fetchReddit(ctx, src, keywords, since)
		nextDelay = delays.fallback()
		if err != nil {
			appendFailed(FailedSource{Name: src.Name, Err: err})
			continue
		}
		if result.RedditRateLimitWait > 0 {
			nextDelay = result.RedditRateLimitWait + delays.jitter()
		}
		appendResult(result)
	}
}

func fetchWithRetry(ctx context.Context, src config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
	return fetchWithRetryUsing(ctx, src, keywords, since, defaultSourceFetchers(), sleepContext)
}

func fetchWithRetryUsing(ctx context.Context, src config.Source, keywords []string, since time.Time, fetchers sourceFetchers, sleep sleepFunc, retryOptions ...fetchRetrySettings) (sourceFetchResult, error) {
	settings := defaultFetchRetrySettings()
	if len(retryOptions) > 0 && retryOptions[0].times > 0 {
		settings = retryOptions[0]
	}
	var result sourceFetchResult
	var lastErr error

	for attempt := 1; attempt <= settings.times; attempt++ {
		if err := ctx.Err(); err != nil {
			return sourceFetchResult{}, err
		}
		var err error
		switch src.Type {
		case config.SourceTypeRSS:
			result, err = fetchers.rss(ctx, src, keywords, since)
		case config.SourceTypeHackerNews:
			result, err = fetchers.hackernews(ctx, src, keywords, since)
		case config.SourceTypeReddit:
			result, err = fetchers.reddit(ctx, src, keywords, since)
		case config.SourceTypeDocsPage:
			result, err = fetchers.docsPage(ctx, src, keywords, since)
		case config.SourceTypeRepoPage:
			result, err = fetchers.repoPage(ctx, src, keywords, since)
		default:
			return sourceFetchResult{}, fmt.Errorf("unknown source type for %s: %s", src.Name, src.Type)
		}

		if err == nil {
			return result, nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return sourceFetchResult{}, ctxErr
		}
		lastErr = err
		if isRateLimited(err) {
			break
		}
		if attempt < settings.times {
			if err := sleep(ctx, settings.wait); err != nil {
				return sourceFetchResult{}, err
			}
		}
	}
	return sourceFetchResult{}, lastErr
}

// isTTY 检测 stdout 是否为终端
var (
	ttyOnce   sync.Once
	ttyResult bool
)

func checkTTY() bool {
	ttyOnce.Do(func() {
		fi, _ := os.Stdout.Stat()
		ttyResult = (fi.Mode() & os.ModeCharDevice) != 0
	})
	return ttyResult
}

func PrintFailed(failed []FailedSource) {
	if len(failed) == 0 {
		return
	}
	if checkTTY() {
		fmt.Printf("\n\033[31m--- 以下源获取失败（重试3次均失败）---\033[0m\n")
		for _, f := range failed {
			fmt.Printf("  \033[31m✗ %s: %s\033[0m\n", f.Name, f.SafeErrorMessage())
		}
	} else {
		fmt.Printf("\n--- 以下源获取失败（重试3次均失败）---\n")
		for _, f := range failed {
			fmt.Printf("  ✗ %s: %s\n", f.Name, f.SafeErrorMessage())
		}
	}
	fmt.Println()
}

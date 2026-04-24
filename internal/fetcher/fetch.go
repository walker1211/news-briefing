package fetcher

import (
	"context"
	"fmt"
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
	maxRetries = 3
	retryDelay = 200 * time.Millisecond
)

type sleepFunc func(context.Context, time.Duration) error

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

type fetchedCandidate struct {
	Article         model.Article
	MatchedKeywords []string
}

type sourceFetchResult struct {
	Source     config.Source
	Candidates []fetchedCandidate
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

// isRateLimited 检测是否为 429 限流响应
func isRateLimited(err error) bool {
	return err != nil && strings.Contains(err.Error(), "429")
}

type FailedSource struct {
	Name string
	Err  error
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
	since := time.Now().Add(-12 * time.Hour)
	return fetchWindowContext(ctx, cfg, since, time.Now(), markSeen, false, fetchAllSourcesDetailed)
}

func (c *Client) FetchAllContext(ctx context.Context, cfg *config.Config, markSeen bool) ([]model.Article, []FailedSource, error) {
	since := time.Now().Add(-12 * time.Hour)
	return c.FetchWindowContext(ctx, cfg, since, time.Now(), markSeen, false)
}

func FetchWindow(cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []FailedSource, error) {
	return FetchWindowContext(context.Background(), cfg, from, to, markSeen, ignoreSeen)
}

func (c *Client) FetchWindow(cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []FailedSource, error) {
	return c.FetchWindowContext(context.Background(), cfg, from, to, markSeen, ignoreSeen)
}

func FetchWindowContext(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []FailedSource, error) {
	return fetchWindowContext(ctx, cfg, from, to, markSeen, ignoreSeen, fetchAllSourcesDetailed)
}

func (c *Client) FetchWindowContext(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []FailedSource, error) {
	return fetchWindowContext(ctx, cfg, from, to, markSeen, ignoreSeen, c.fetchAllSourcesDetailed)
}

func fetchWindowContext(ctx context.Context, cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool, fetchAll fetchAllSourcesDetailedFunc) ([]model.Article, []FailedSource, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	results, failed, err := fetchAll(ctx, cfg, from)
	if err != nil {
		return nil, nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	accepted := make([]model.Article, 0)
	for _, result := range results {
		for _, candidate := range result.Candidates {
			if len(candidate.MatchedKeywords) == 0 {
				continue
			}
			if !articleWithinWindow(candidate.Article, from, to) {
				continue
			}
			accepted = append(accepted, candidate.Article)
		}
	}

	sort.Slice(accepted, func(i, j int) bool {
		return accepted[i].Published.After(accepted[j].Published)
	})

	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	outcome, err := applyDedupContext(ctx, accepted, markSeen, ignoreSeen, NewSeenStore(cfg.Output.Dir))
	if err != nil {
		return nil, failed, err
	}
	return outcome.Articles, failed, nil
}

func MarkArticlesSeen(outputDir string, articles []model.Article) error {
	if len(articles) == 0 {
		return nil
	}
	_, err := Dedup(articles, true, NewSeenStore(outputDir))
	return err
}

func articleWithinWindow(a model.Article, from, to time.Time) bool {
	return a.Published.After(from) && !a.Published.After(to)
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
	for _, src := range cfg.Sources {
		if src.Type == config.SourceTypeReddit {
			redditSources = append(redditSources, src)
		} else {
			otherSources = append(otherSources, src)
		}
	}

	for _, src := range otherSources {
		wg.Go(func() {
			if err := ctx.Err(); err != nil {
				mu.Lock()
				failed = append(failed, FailedSource{Name: src.Name, Err: err})
				mu.Unlock()
				return
			}
			result, err := fetchWithRetryUsing(ctx, src, cfg.Keywords, since, fetchers, sleep)
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
			fetchRedditSourcesSeriallyWith(ctx, redditSources, cfg.Keywords, since, fetchers.reddit, sleep, func(item FailedSource) {
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

func fetchRedditSourcesSeriallyWith(ctx context.Context, sources []config.Source, keywords []string, since time.Time, fetchReddit sourceFetchFunc, sleep sleepFunc, appendFailed func(FailedSource), appendResult func(sourceFetchResult)) {
	for i, src := range sources {
		if err := ctx.Err(); err != nil {
			appendFailed(FailedSource{Name: src.Name, Err: err})
			return
		}
		if i > 0 {
			if err := sleep(ctx, 2*time.Second); err != nil {
				appendFailed(FailedSource{Name: src.Name, Err: err})
				return
			}
		}
		result, err := fetchReddit(ctx, src, keywords, since)
		if err != nil {
			appendFailed(FailedSource{Name: src.Name, Err: err})
			continue
		}
		appendResult(result)
	}
}

func fetchWithRetry(ctx context.Context, src config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
	return fetchWithRetryUsing(ctx, src, keywords, since, defaultSourceFetchers(), sleepContext)
}

func fetchWithRetryUsing(ctx context.Context, src config.Source, keywords []string, since time.Time, fetchers sourceFetchers, sleep sleepFunc) (sourceFetchResult, error) {
	var result sourceFetchResult
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
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
		if attempt < maxRetries {
			if err := sleep(ctx, retryDelay); err != nil {
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
			fmt.Printf("  \033[31m✗ %s: %v\033[0m\n", f.Name, f.Err)
		}
	} else {
		fmt.Printf("\n--- 以下源获取失败（重试3次均失败）---\n")
		for _, f := range failed {
			fmt.Printf("  ✗ %s: %v\n", f.Name, f.Err)
		}
	}
	fmt.Println()
}

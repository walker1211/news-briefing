package fetcher

import (
	"fmt"
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

var sleep = time.Sleep

type fetchedCandidate struct {
	Article         model.Article
	MatchedKeywords []string
	Status          model.TraceStatus
}

type sourceFetchResult struct {
	Source     config.Source
	Candidates []fetchedCandidate
}

var fetchRSSSource = func(src config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
	return FetchRSS(src, keywords, since)
}

var fetchHackerNewsSource = func(src config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
	return FetchHackerNews(src, keywords, since)
}

var fetchRedditDirect = func(src config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
	return FetchReddit(src, keywords, since)
}

var fetchRedditSource = func(src config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
	return fetchWithRetry(src, keywords, since)
}

var fetchDocsPageSource = func(src config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
	return FetchDocsPage(src, keywords, since)
}

var fetchRepoPageSource = func(src config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
	return FetchRepoPage(src, keywords, since)
}

// isRateLimited 检测是否为 429 限流响应
func isRateLimited(err error) bool {
	return err != nil && strings.Contains(err.Error(), "429")
}

type FailedSource struct {
	Name string
	Err  error
}

var fetchAllSourcesDetailed = func(cfg *config.Config, since time.Time) ([]sourceFetchResult, []FailedSource, error) {
	var (
		mu     sync.Mutex
		all    []sourceFetchResult
		failed []FailedSource
		wg     sync.WaitGroup
	)

	var redditSources []config.Source
	var otherSources []config.Source
	for _, src := range cfg.Sources {
		if src.Type == "reddit" {
			redditSources = append(redditSources, src)
		} else {
			otherSources = append(otherSources, src)
		}
	}

	for _, src := range otherSources {
		wg.Add(1)
		go func(src config.Source) {
			defer wg.Done()
			result, err := fetchWithRetry(src, cfg.Keywords, since)
			mu.Lock()
			if err != nil {
				failed = append(failed, FailedSource{Name: src.Name, Err: err})
			} else {
				all = append(all, result)
			}
			mu.Unlock()
		}(src)
	}

	if len(redditSources) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fetchRedditSourcesSerially(redditSources, cfg.Keywords, since, func(item FailedSource) {
				mu.Lock()
				failed = append(failed, item)
				mu.Unlock()
			}, func(item sourceFetchResult) {
				mu.Lock()
				all = append(all, item)
				mu.Unlock()
			})
		}()
	}

	wg.Wait()
	return all, failed, nil
}

var fetchAllSources = func(cfg *config.Config, since time.Time) ([]model.Article, []FailedSource, error) {
	results, failed, err := fetchAllSourcesDetailed(cfg, since)
	if err != nil {
		return nil, nil, err
	}
	var all []model.Article
	for _, result := range results {
		for _, candidate := range result.Candidates {
			all = append(all, candidate.Article)
		}
	}
	return all, failed, nil
}

// FetchAll 并发抓取所有新闻源，支持重试。
// 返回文章列表、失败源列表和错误。
func FetchAll(cfg *config.Config, markSeen bool) ([]model.Article, []FailedSource, error) {
	since := time.Now().Add(-12 * time.Hour)
	return FetchWindow(cfg, since, time.Now(), markSeen, false)
}

func FetchAllWithIndex(cfg *config.Config, markSeen bool) ([]model.Article, []FailedSource, model.SourceIndex, error) {
	since := time.Now().Add(-12 * time.Hour)
	return FetchWindowWithIndex(cfg, since, time.Now(), markSeen, false)
}

func FetchWindow(cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []FailedSource, error) {
	articles, failed, _, err := FetchWindowWithIndex(cfg, from, to, markSeen, ignoreSeen)
	return articles, failed, err
}

func FetchWindowWithIndex(cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []FailedSource, model.SourceIndex, error) {
	results, failed, err := fetchAllSourcesDetailed(cfg, from)
	if err != nil {
		return nil, nil, model.SourceIndex{}, err
	}

	index := newSourceIndex(cfg)
	runByName := make(map[string]*model.SourceRun, len(index.SourceRuns))
	for i := range index.SourceRuns {
		runByName[index.SourceRuns[i].Name] = &index.SourceRuns[i]
	}

	type acceptedTrace struct {
		article    model.Article
		traceIndex int
	}

	var accepted []acceptedTrace
	for _, result := range results {
		run := runByName[result.Source.Name]
		if run != nil {
			run.Status = "success"
			run.FetchedCount = len(result.Candidates)
		}
		for _, candidate := range result.Candidates {
			trace := model.ArticleTrace{
				Title:           candidate.Article.Title,
				Link:            candidate.Article.Link,
				Source:          result.Source.Name,
				SourceType:      result.Source.Type,
				Category:        result.Source.Category,
				Published:       candidate.Article.Published,
				MatchedKeywords: append([]string(nil), candidate.MatchedKeywords...),
			}
			index.ArticleTraces = append(index.ArticleTraces, trace)
			traceIndex := len(index.ArticleTraces) - 1
			traceRef := &index.ArticleTraces[traceIndex]

			if candidate.Status == model.TraceStatusMissingAcceptableTime || candidate.Status == model.TraceStatusNonReleasePage {
				traceRef.Status = candidate.Status
				traceRef.RejectionReason = string(candidate.Status)
				continue
			}

			if len(candidate.MatchedKeywords) == 0 {
				traceRef.Status = model.TraceStatusKeywordMiss
				traceRef.RejectionReason = string(model.TraceStatusKeywordMiss)
				if run != nil {
					run.KeywordMissCount++
				}
				continue
			}
			if !articleWithinWindow(candidate.Article, from, to) {
				traceRef.Status = model.TraceStatusOutOfWindow
				traceRef.RejectionReason = string(model.TraceStatusOutOfWindow)
				if run != nil {
					run.WindowMissCount++
				}
				continue
			}
			accepted = append(accepted, acceptedTrace{article: candidate.Article, traceIndex: traceIndex})
		}
	}

	for _, failedSource := range failed {
		if run := runByName[failedSource.Name]; run != nil {
			run.Status = string(model.TraceStatusFetchFailed)
			run.Error = failedSource.Err.Error()
		}
	}

	sort.Slice(accepted, func(i, j int) bool {
		return accepted[i].article.Published.After(accepted[j].article.Published)
	})

	acceptedArticles := make([]model.Article, 0, len(accepted))
	acceptedTraceIndexes := make([]int, 0, len(accepted))
	for _, item := range accepted {
		acceptedArticles = append(acceptedArticles, item.article)
		acceptedTraceIndexes = append(acceptedTraceIndexes, item.traceIndex)
	}

	outcome, err := applyDedup(acceptedArticles, markSeen, ignoreSeen, NewSeenStore(cfg.Output.Dir))
	if err != nil {
		return nil, failed, index, err
	}

	traceIndexesByKey := make(map[string][]int)
	for i, article := range acceptedArticles {
		key := dedupKey(article)
		traceIndexesByKey[key] = append(traceIndexesByKey[key], acceptedTraceIndexes[i])
	}
	for key := range outcome.DuplicateKeys {
		traceIndexes := traceIndexesByKey[key]
		if len(traceIndexes) == 0 {
			continue
		}
		for i := 1; i < len(traceIndexes); i++ {
			trace := &index.ArticleTraces[traceIndexes[i]]
			trace.Status = model.TraceStatusDuplicateInBatch
			trace.RejectionReason = string(model.TraceStatusDuplicateInBatch)
			if run := runByName[trace.Source]; run != nil {
				run.DedupedCount++
			}
		}
	}
	for key := range outcome.SeenBeforeKeys {
		traceIndexes := traceIndexesByKey[key]
		for _, traceIndex := range traceIndexes {
			trace := &index.ArticleTraces[traceIndex]
			trace.Status = model.TraceStatusSeenBefore
			trace.RejectionReason = string(model.TraceStatusSeenBefore)
			if run := runByName[trace.Source]; run != nil {
				run.DedupedCount++
			}
		}
	}
	for _, article := range outcome.Articles {
		traceIndexes := traceIndexesByKey[dedupKey(article)]
		if len(traceIndexes) == 0 {
			continue
		}
		trace := &index.ArticleTraces[traceIndexes[0]]
		trace.Status = model.TraceStatusIncluded
		trace.RejectionReason = ""
		if run := runByName[trace.Source]; run != nil {
			run.IncludedCount++
		}
	}

	return outcome.Articles, failed, index, nil
}

func newSourceIndex(cfg *config.Config) model.SourceIndex {
	index := model.SourceIndex{SourceRuns: make([]model.SourceRun, 0, len(cfg.Sources))}
	for _, src := range cfg.Sources {
		index.SourceRuns = append(index.SourceRuns, model.SourceRun{
			Name:     src.Name,
			Type:     src.Type,
			Category: src.Category,
			Status:   "success",
		})
	}
	return index
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
	if ignoreSeen {
		return DedupInBatch(articles), nil
	}
	return Dedup(articles, markSeen, store)
}

func fetchRedditSourcesSerially(sources []config.Source, keywords []string, since time.Time, appendFailed func(FailedSource), appendResult func(sourceFetchResult)) {
	for i, src := range sources {
		if i > 0 {
			sleep(2 * time.Second)
		}
		result, err := fetchRedditSource(src, keywords, since)
		if err != nil {
			appendFailed(FailedSource{Name: src.Name, Err: err})
			continue
		}
		appendResult(result)
	}
}

func fetchWithRetry(src config.Source, keywords []string, since time.Time) (sourceFetchResult, error) {
	var result sourceFetchResult
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		var err error
		switch src.Type {
		case "rss":
			result, err = fetchRSSSource(src, keywords, since)
		case "hackernews":
			result, err = fetchHackerNewsSource(src, keywords, since)
		case "reddit":
			result, err = fetchRedditDirect(src, keywords, since)
		case "docs_page":
			result, err = fetchDocsPageSource(src, keywords, since)
		case "repo_page":
			result, err = fetchRepoPageSource(src, keywords, since)
		default:
			return sourceFetchResult{}, fmt.Errorf("unknown source type for %s: %s", src.Name, src.Type)
		}

		if err == nil {
			return result, nil
		}
		lastErr = err
		if isRateLimited(err) {
			break
		}
		if attempt < maxRetries {
			sleep(retryDelay)
		}
	}
	return sourceFetchResult{}, lastErr
}

// isTTY 检测 stdout 是否为终端
var ttyChecked bool
var ttyResult bool

func checkTTY() bool {
	if !ttyChecked {
		fi, _ := os.Stdout.Stat()
		ttyResult = (fi.Mode() & os.ModeCharDevice) != 0
		ttyChecked = true
	}
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

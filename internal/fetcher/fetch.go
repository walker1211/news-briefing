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

// FetchAll 并发抓取所有新闻源，支持重试。
// 返回文章列表、失败源列表和错误。
func FetchAll(cfg *config.Config, markSeen bool) ([]model.Article, []FailedSource, error) {
	since := time.Now().Add(-12 * time.Hour)
	return FetchWindow(cfg, since, time.Now(), markSeen, false)
}

func FetchWindow(cfg *config.Config, from, to time.Time, markSeen bool, ignoreSeen bool) ([]model.Article, []FailedSource, error) {
	results, failed, err := fetchAllSourcesDetailed(cfg, from)
	if err != nil {
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

	outcome, err := applyDedup(accepted, markSeen, ignoreSeen, NewSeenStore(cfg.Output.Dir))
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

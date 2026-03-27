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

var fetchRedditSource = func(src config.Source, keywords []string, since time.Time) ([]model.Article, error) {
	return fetchWithRetry(src, keywords, since)
}

// isRateLimited 检测是否为 429 限流响应
func isRateLimited(err error) bool {
	return err != nil && strings.Contains(err.Error(), "429")
}

type FailedSource struct {
	Name string
	Err  error
}

var fetchAllSources = func(cfg *config.Config, since time.Time) ([]model.Article, []FailedSource, error) {
	var (
		mu     sync.Mutex
		all    []model.Article
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
			articles, err := fetchWithRetry(src, cfg.Keywords, since)
			mu.Lock()
			if err != nil {
				failed = append(failed, FailedSource{Name: src.Name, Err: err})
			} else {
				all = append(all, articles...)
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
			}, func(items []model.Article) {
				mu.Lock()
				all = append(all, items...)
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
	all, failed, err := fetchAllSources(cfg, from)
	if err != nil {
		return nil, nil, err
	}

	all = filterArticlesByWindow(all, from, to)
	sort.Slice(all, func(i, j int) bool {
		return all[i].Published.After(all[j].Published)
	})
	all, err = applyDedup(all, markSeen, ignoreSeen, NewSeenStore(cfg.Output.Dir))
	if err != nil {
		return nil, failed, err
	}
	return all, failed, nil
}

func filterArticlesByWindow(articles []model.Article, from, to time.Time) []model.Article {
	var result []model.Article
	for _, a := range articles {
		if !a.Published.After(from) || a.Published.After(to) {
			continue
		}
		result = append(result, a)
	}
	return result
}

func applyDedup(articles []model.Article, markSeen bool, ignoreSeen bool, store SeenStore) ([]model.Article, error) {
	if ignoreSeen {
		return DedupInBatch(articles), nil
	}
	return Dedup(articles, markSeen, store)
}

func fetchRedditSourcesSerially(sources []config.Source, keywords []string, since time.Time, appendFailed func(FailedSource), appendArticles func([]model.Article)) {
	for i, src := range sources {
		if i > 0 {
			sleep(2 * time.Second)
		}
		articles, err := fetchRedditSource(src, keywords, since)
		if err != nil {
			appendFailed(FailedSource{Name: src.Name, Err: err})
			continue
		}
		appendArticles(articles)
	}
}

func fetchWithRetry(src config.Source, keywords []string, since time.Time) ([]model.Article, error) {
	var articles []model.Article
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		var err error
		switch src.Type {
		case "rss":
			articles, err = FetchRSS(src, keywords, since)
		case "hackernews":
			articles, err = FetchHackerNews(src, keywords, since)
		case "reddit":
			articles, err = FetchReddit(src, keywords, since)
		default:
			return nil, fmt.Errorf("unknown source type for %s: %s", src.Name, src.Type)
		}

		if err == nil {
			return articles, nil
		}
		lastErr = err
		if isRateLimited(err) {
			break
		}
		if attempt < maxRetries {
			sleep(retryDelay)
		}
	}
	return nil, lastErr
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

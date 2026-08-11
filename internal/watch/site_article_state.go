package watch

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/model"
)

type watchArticleContent struct {
	title   string
	summary string
	body    string
}

type watchArticleContentFetcher func(context.Context, string) (watchArticleContent, error)

type memoizedWatchArticleContentResult struct {
	ready   chan struct{}
	content watchArticleContent
	err     error
}

// memoizeWatchArticleContentFetcher keeps one in-flight/result entry per URL
// for a single watch run. Anthropic Support currently exposes heavily
// overlapping article lists across collections; sharing the result preserves
// every collection comparison without downloading the same article repeatedly.
func memoizeWatchArticleContentFetcher(fetch watchArticleContentFetcher) watchArticleContentFetcher {
	var mu sync.Mutex
	results := make(map[string]*memoizedWatchArticleContentResult)

	return func(ctx context.Context, url string) (watchArticleContent, error) {
		mu.Lock()
		if existing, ok := results[url]; ok {
			mu.Unlock()
			select {
			case <-ctx.Done():
				return watchArticleContent{}, ctx.Err()
			case <-existing.ready:
				return existing.content, existing.err
			}
		}

		result := &memoizedWatchArticleContentResult{ready: make(chan struct{})}
		results[url] = result
		mu.Unlock()

		result.content, result.err = fetch(ctx, url)
		mu.Lock()
		if result.err != nil {
			// A later collection may retry a transient failure instead of keeping
			// the failure cached for the rest of the run.
			delete(results, url)
		}
		close(result.ready)
		mu.Unlock()
		return result.content, result.err
	}
}

type watchArticleContentResult struct {
	item    model.WatchIndexItem
	content watchArticleContent
}

type watchCategoryRun struct {
	site               config.WatchSite
	now                time.Time
	stateKey           string
	current            model.WatchIndexSnapshot
	indexState         IndexState
	articleState       ArticleState
	fetchContent       watchArticleContentFetcher
	articleConcurrency int
}

func runWatchCategory(ctx context.Context, run watchCategoryRun) ([]model.Article, []model.WatchSeenArticle, []model.WatchEvent, error) {
	prevSnapshot, hasPrev := run.indexState.Categories[run.stateKey]
	if !hasPrev {
		if err := bootstrapWatchCategoryState(ctx, run); err != nil {
			return nil, nil, nil, err
		}
		run.indexState.Categories[run.stateKey] = run.current
		return nil, nil, nil, nil
	}

	categoryEvents, changedURLs := diffWatchCategoryEvents(run.site, run.now, &prevSnapshot, run.current)
	seenPayloads, err := updateCurrentWatchArticleStates(ctx, run, changedURLs, &categoryEvents)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := enrichChangedWatchEvents(ctx, run, changedURLs, categoryEvents, seenPayloads); err != nil {
		return nil, nil, nil, err
	}

	run.indexState.Categories[run.stateKey] = run.current
	articles, seenItems, err := materializeWatchEvents(ctx, run, categoryEvents, seenPayloads)
	if err != nil {
		return nil, nil, nil, err
	}
	return articles, seenItems, categoryEvents, nil
}

func bootstrapWatchCategoryState(ctx context.Context, run watchCategoryRun) error {
	results, err := fetchWatchArticleContents(ctx, run.current.Items, run.fetchContent, run.articleConcurrency)
	if err != nil {
		return err
	}
	for _, result := range results {
		storeWatchArticleState(run.articleState, result.item.URL, result.content, run.now)
	}
	return nil
}

func diffWatchCategoryEvents(site config.WatchSite, now time.Time, prev *model.WatchIndexSnapshot, current model.WatchIndexSnapshot) ([]model.WatchEvent, []string) {
	categoryEvents, changedURLs := diffCategorySnapshots(prev, current)
	for i := range categoryEvents {
		categoryEvents[i].Source = site.Name
		categoryEvents[i].DetectedAt = now
		if categoryEvents[i].Reason == "" {
			categoryEvents[i].Reason = defaultWatchReason(categoryEvents[i].EventType, categoryEvents[i].ArticleTitle)
		}
		if slices.Contains(changedURLs, categoryEvents[i].ArticleURL) {
			continue
		}
		applyWatchEventPriorityAt(&categoryEvents[i], now)
	}
	return categoryEvents, changedURLs
}

func fetchWatchArticleContents(ctx context.Context, items []model.WatchIndexItem, fetchContent watchArticleContentFetcher, concurrency int) ([]watchArticleContentResult, error) {
	results := make([]watchArticleContentResult, len(items))
	if len(items) == 0 {
		return results, nil
	}
	workerCount := concurrency
	if workerCount < 1 {
		workerCount = config.DefaultWatchArticleConcurrency
	}
	if len(items) < workerCount {
		workerCount = len(items)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan int)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	setErr := func(err error) {
		mu.Lock()
		defer mu.Unlock()
		if firstErr == nil {
			firstErr = err
			cancel()
		}
	}
	for range workerCount {
		wg.Go(func() {
			for index := range jobs {
				if err := ctx.Err(); err != nil {
					continue
				}
				content, err := fetchContent(ctx, items[index].URL)
				if err != nil {
					setErr(err)
					continue
				}
				results[index] = watchArticleContentResult{item: items[index], content: content}
			}
		})
	}
sendJobs:
	for index := range items {
		select {
		case <-ctx.Done():
			break sendJobs
		case jobs <- index:
		}
	}
	close(jobs)
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func updateCurrentWatchArticleStates(ctx context.Context, run watchCategoryRun, changedURLs []string, categoryEvents *[]model.WatchEvent) (map[string]watchArticleContent, error) {
	seenPayloads := make(map[string]watchArticleContent)
	changed := make(map[string]struct{}, len(changedURLs))
	for _, url := range changedURLs {
		changed[url] = struct{}{}
	}
	items := make([]model.WatchIndexItem, 0, len(run.current.Items))
	for _, item := range run.current.Items {
		if _, ok := changed[item.URL]; ok {
			continue
		}
		items = append(items, item)
	}
	results, err := fetchWatchArticleContents(ctx, items, run.fetchContent, run.articleConcurrency)
	if err != nil {
		return nil, err
	}
	for _, result := range results {
		item := result.item
		content := result.content
		state, ok := run.articleState[item.URL]
		summaryHash := hashWatchContent(content.summary)
		bodyHash := hashWatchContent(content.body)
		if !ok {
			storeWatchArticleState(run.articleState, item.URL, content, run.now)
			continue
		}
		if state.Title != content.title || state.SummaryHash != summaryHash || state.BodyHash != bodyHash {
			event := model.WatchEvent{
				EventType:       "content_changed",
				Source:          run.site.Name,
				Category:        run.current.Category,
				ArticleURL:      item.URL,
				ArticleTitle:    content.title,
				DetectedAt:      run.now,
				BodyFetched:     true,
				ContentChanged:  true,
				Reason:          "正文发生变化",
				MatchedKeywords: matchedWatchKeywords(content.title+" "+content.summary+" "+content.body, run.site.HighValueKeywords),
			}
			applyWatchEventPriorityAt(&event, run.now)
			*categoryEvents = append(*categoryEvents, event)
			seenPayloads[item.URL] = content
			storeWatchArticleState(run.articleState, item.URL, content, run.now)
			continue
		}
		state.LastCheckedAt = run.now
		run.articleState[item.URL] = state
	}
	return seenPayloads, nil
}

func enrichChangedWatchEvents(ctx context.Context, run watchCategoryRun, changedURLs []string, categoryEvents []model.WatchEvent, seenPayloads map[string]watchArticleContent) error {
	for _, url := range changedURLs {
		matchedIndex := -1
		for i := range categoryEvents {
			if categoryEvents[i].ArticleURL == url {
				matchedIndex = i
				break
			}
		}
		if matchedIndex == -1 || categoryEvents[matchedIndex].EventType == "removed_article" {
			continue
		}

		content, err := run.fetchContent(ctx, url)
		if err != nil {
			return err
		}
		storeWatchArticleState(run.articleState, url, content, run.now)
		if content.title != "" {
			categoryEvents[matchedIndex].ArticleTitle = content.title
		}
		categoryEvents[matchedIndex].BodyFetched = true
		categoryEvents[matchedIndex].MatchedKeywords = matchedWatchKeywords(content.title+" "+content.summary+" "+content.body, run.site.HighValueKeywords)
		if categoryEvents[matchedIndex].Reason == "" {
			categoryEvents[matchedIndex].Reason = defaultWatchReason(categoryEvents[matchedIndex].EventType, categoryEvents[matchedIndex].ArticleTitle)
		}
		applyWatchEventPriorityAt(&categoryEvents[matchedIndex], run.now)
		seenPayloads[url] = content
	}
	return nil
}

func materializeWatchEvents(ctx context.Context, run watchCategoryRun, categoryEvents []model.WatchEvent, seenPayloads map[string]watchArticleContent) ([]model.Article, []model.WatchSeenArticle, error) {
	articles := make([]model.Article, 0)
	seenItems := make([]model.WatchSeenArticle, 0)
	for _, event := range categoryEvents {
		if event.EventType == "removed_article" && event.ArticleURL != "" {
			delete(run.articleState, event.ArticleURL)
		}
		if !event.IncludeInBriefing {
			continue
		}
		articles = append(articles, watchEventToArticle(run.site, event))
		payload, ok := seenPayloads[event.ArticleURL]
		if !ok {
			content, err := run.fetchContent(ctx, event.ArticleURL)
			if err != nil {
				return nil, nil, err
			}
			payload = content
		}
		seenItems = append(seenItems, watchEventToSeenArticle(run.site, event, payload.summary, payload.body))
	}
	return articles, seenItems, nil
}

func storeWatchArticleState(articleState ArticleState, url string, content watchArticleContent, now time.Time) {
	articleState[url] = model.WatchArticleState{
		URL:           url,
		Title:         content.title,
		SummaryHash:   hashWatchContent(content.summary),
		BodyHash:      hashWatchContent(content.body),
		LastCheckedAt: now,
		LastChangedAt: now,
	}
}

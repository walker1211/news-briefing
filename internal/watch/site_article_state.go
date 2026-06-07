package watch

import (
	"context"
	"slices"
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

type watchCategoryRun struct {
	site         config.WatchSite
	now          time.Time
	stateKey     string
	current      model.WatchIndexSnapshot
	indexState   IndexState
	articleState ArticleState
	fetchContent watchArticleContentFetcher
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
	for _, item := range run.current.Items {
		content, err := run.fetchContent(ctx, item.URL)
		if err != nil {
			return err
		}
		storeWatchArticleState(run.articleState, item.URL, content, run.now)
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
		applyWatchEventPriority(&categoryEvents[i])
	}
	return categoryEvents, changedURLs
}

func updateCurrentWatchArticleStates(ctx context.Context, run watchCategoryRun, changedURLs []string, categoryEvents *[]model.WatchEvent) (map[string]watchArticleContent, error) {
	seenPayloads := make(map[string]watchArticleContent)
	for _, item := range run.current.Items {
		if slices.Contains(changedURLs, item.URL) {
			continue
		}
		state, ok := run.articleState[item.URL]
		content, err := run.fetchContent(ctx, item.URL)
		if err != nil {
			return nil, err
		}
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
			applyWatchEventPriority(&event)
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
		applyWatchEventPriority(&categoryEvents[matchedIndex])
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

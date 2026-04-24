package watch

import (
	"context"
	"crypto/sha1"
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

	articles := make([]model.Article, 0)
	seenItems := make([]model.WatchSeenArticle, 0)
	for _, site := range cfg.Watch.Sites {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		siteArticles, siteSeenItems, events, err := runSite(ctx, site, now, indexState, articleState, fetchHTML)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, nil, ctxErr
			}
			if site.Type == "announcement_page" {
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

func runSite(ctx context.Context, site config.WatchSite, now time.Time, indexState IndexState, articleState ArticleState, fetchHTML fetchHTMLFunc) ([]model.Article, []model.WatchSeenArticle, []model.WatchEvent, error) {
	switch site.Type {
	case "anthropic_support":
		return runAnthropicSupportSite(ctx, site, now, indexState, articleState, fetchHTML)
	case "announcement_page":
		return runAnnouncementSite(ctx, site, now, indexState, articleState, fetchHTML)
	default:
		return nil, nil, nil, nil
	}
}

func runAnthropicSupportSite(ctx context.Context, site config.WatchSite, now time.Time, indexState IndexState, articleState ArticleState, fetchHTML fetchHTMLFunc) ([]model.Article, []model.WatchSeenArticle, []model.WatchEvent, error) {
	type seenPayload struct {
		summary string
		body    string
	}

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

		stateKey := watchCategoryStateKey(site.Name, current.Category)
		prevSnapshot, hasPrev := indexState.Categories[stateKey]
		if !hasPrev {
			for _, item := range current.Items {
				articleHTML, err := fetchHTML(ctx, item.URL)
				if err != nil {
					return nil, nil, nil, err
				}
				title, summary, body, err := parseAnthropicArticle(articleHTML)
				if err != nil {
					return nil, nil, nil, err
				}
				articleState[item.URL] = model.WatchArticleState{
					URL:           item.URL,
					Title:         title,
					SummaryHash:   hashWatchContent(summary),
					BodyHash:      hashWatchContent(body),
					LastCheckedAt: now,
					LastChangedAt: now,
				}
			}
			indexState.Categories[stateKey] = current
			continue
		}
		prev := &prevSnapshot
		categoryEvents, changedURLs := diffCategorySnapshots(prev, current)
		for i := range categoryEvents {
			categoryEvents[i].Source = site.Name
			categoryEvents[i].DetectedAt = now
			if slices.Contains(changedURLs, categoryEvents[i].ArticleURL) {
				continue
			}
			applyWatchEventPriority(&categoryEvents[i])
		}

		seenPayloads := make(map[string]seenPayload)
		for _, item := range current.Items {
			if slices.Contains(changedURLs, item.URL) {
				continue
			}
			state, ok := articleState[item.URL]
			articleHTML, err := fetchHTML(ctx, item.URL)
			if err != nil {
				return nil, nil, nil, err
			}
			title, summary, body, err := parseAnthropicArticle(articleHTML)
			if err != nil {
				return nil, nil, nil, err
			}
			summaryHash := hashWatchContent(summary)
			bodyHash := hashWatchContent(body)
			if !ok {
				articleState[item.URL] = model.WatchArticleState{
					URL:           item.URL,
					Title:         title,
					SummaryHash:   summaryHash,
					BodyHash:      bodyHash,
					LastCheckedAt: now,
					LastChangedAt: now,
				}
				continue
			}
			if state.Title != title || state.SummaryHash != summaryHash || state.BodyHash != bodyHash {
				event := model.WatchEvent{
					EventType:       "content_changed",
					Source:          site.Name,
					Category:        current.Category,
					ArticleURL:      item.URL,
					ArticleTitle:    title,
					DetectedAt:      now,
					BodyFetched:     true,
					ContentChanged:  true,
					Reason:          "正文发生变化",
					MatchedKeywords: matchedWatchKeywords(title+" "+summary+" "+body, site.HighValueKeywords),
				}
				applyWatchEventPriority(&event)
				categoryEvents = append(categoryEvents, event)
				seenPayloads[item.URL] = seenPayload{summary: summary, body: body}
				articleState[item.URL] = model.WatchArticleState{
					URL:           item.URL,
					Title:         title,
					SummaryHash:   summaryHash,
					BodyHash:      bodyHash,
					LastCheckedAt: now,
					LastChangedAt: now,
				}
				continue
			}
			state.LastCheckedAt = now
			articleState[item.URL] = state
		}

		for _, url := range changedURLs {
			matchedIndex := -1
			for i := range categoryEvents {
				if categoryEvents[i].ArticleURL == url {
					matchedIndex = i
					break
				}
			}
			if matchedIndex == -1 {
				continue
			}
			if categoryEvents[matchedIndex].EventType == "removed_article" {
				continue
			}

			articleHTML, err := fetchHTML(ctx, url)
			if err != nil {
				return nil, nil, nil, err
			}
			title, summary, body, err := parseAnthropicArticle(articleHTML)
			if err != nil {
				return nil, nil, nil, err
			}
			articleState[url] = model.WatchArticleState{
				URL:           url,
				Title:         title,
				SummaryHash:   hashWatchContent(summary),
				BodyHash:      hashWatchContent(body),
				LastCheckedAt: now,
				LastChangedAt: now,
			}
			if title != "" {
				categoryEvents[matchedIndex].ArticleTitle = title
			}
			categoryEvents[matchedIndex].BodyFetched = true
			categoryEvents[matchedIndex].MatchedKeywords = matchedWatchKeywords(title+" "+summary+" "+body, site.HighValueKeywords)
			if categoryEvents[matchedIndex].Reason == "" {
				categoryEvents[matchedIndex].Reason = defaultWatchReason(categoryEvents[matchedIndex].EventType, categoryEvents[matchedIndex].ArticleTitle)
			}
			applyWatchEventPriority(&categoryEvents[matchedIndex])
			seenPayloads[url] = seenPayload{summary: summary, body: body}
		}

		indexState.Categories[stateKey] = current
		for _, event := range categoryEvents {
			if event.EventType == "removed_article" && event.ArticleURL != "" {
				delete(articleState, event.ArticleURL)
			}
			events = append(events, event)
			if !event.IncludeInBriefing {
				continue
			}
			articles = append(articles, watchEventToArticle(site, event))
			payload, ok := seenPayloads[event.ArticleURL]
			if !ok {
				articleHTML, err := fetchHTML(ctx, event.ArticleURL)
				if err != nil {
					return nil, nil, nil, err
				}
				_, summary, body, err := parseAnthropicArticle(articleHTML)
				if err != nil {
					return nil, nil, nil, err
				}
				payload = seenPayload{summary: summary, body: body}
			}
			seenItems = append(seenItems, watchEventToSeenArticle(site, event, payload.summary, payload.body))
		}
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

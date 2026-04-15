package watch

import "github.com/walker1211/news-briefing/internal/model"

func diffCategorySnapshots(prev *model.WatchIndexSnapshot, curr model.WatchIndexSnapshot) (events []model.WatchEvent, changedURLs []string) {
	prevByURL := make(map[string]model.WatchIndexItem)
	if prev != nil {
		for _, item := range prev.Items {
			prevByURL[item.URL] = item
		}
	}
	currByURL := make(map[string]model.WatchIndexItem)
	for _, item := range curr.Items {
		currByURL[item.URL] = item
	}

	for url, item := range currByURL {
		old, ok := prevByURL[url]
		if !ok {
			events = append(events, model.WatchEvent{EventType: "new_article", Category: curr.Category, ArticleURL: url, ArticleTitle: item.Title})
			changedURLs = append(changedURLs, url)
			continue
		}
		if old.Title != item.Title {
			events = append(events, model.WatchEvent{EventType: "title_changed", Category: curr.Category, ArticleURL: url, ArticleTitle: item.Title})
			changedURLs = append(changedURLs, url)
		}
	}
	for url, item := range prevByURL {
		if _, ok := currByURL[url]; ok {
			continue
		}
		events = append(events, model.WatchEvent{EventType: "removed_article", Category: curr.Category, ArticleURL: url, ArticleTitle: item.Title})
	}
	if prev != nil && prev.ItemCount != curr.ItemCount {
		events = append(events, model.WatchEvent{EventType: "article_count_changed", Category: curr.Category, Reason: "分类文章总数变化"})
	}
	return events, changedURLs
}

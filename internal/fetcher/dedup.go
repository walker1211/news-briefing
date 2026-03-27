package fetcher

import (
	"time"

	"github.com/walker1211/news-briefing/internal/model"
)

type seenEntry struct {
	URL  string    `json:"url"`
	Time time.Time `json:"time"`
}

// Dedup 过滤已读文章。save 为 true 时将新文章标记为已读。
func Dedup(articles []model.Article, save bool, store SeenStore) ([]model.Article, error) {
	seen, err := store.Load()
	if err != nil {
		return nil, err
	}
	seenMap := make(map[string]bool)
	for _, s := range seen {
		seenMap[s.URL] = true
	}

	var result []model.Article
	for _, a := range articles {
		key := dedupKey(a)
		if seenMap[key] {
			continue
		}
		result = append(result, a)
		if save {
			seen = append(seen, seenEntry{URL: key, Time: time.Now()})
		}
		seenMap[key] = true
	}

	if save {
		cutoff := time.Now().AddDate(0, 0, -7)
		var trimmed []seenEntry
		for _, s := range seen {
			if s.Time.After(cutoff) {
				trimmed = append(trimmed, s)
			}
		}
		if err := store.Save(trimmed); err != nil {
			return nil, err
		}
	}

	return result, nil
}

func DedupInBatch(articles []model.Article) []model.Article {
	seen := make(map[string]bool)
	var result []model.Article
	for _, a := range articles {
		key := dedupKey(a)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, a)
	}
	return result
}

func dedupKey(a model.Article) string {
	if a.Link != "" {
		return a.Link
	}
	return a.Title + "|" + a.Source
}

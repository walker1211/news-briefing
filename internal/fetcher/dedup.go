package fetcher

import (
	"time"

	"github.com/walker1211/news-briefing/internal/model"
)

type seenEntry struct {
	URL  string    `json:"url"`
	Time time.Time `json:"time"`
}

type DedupOutcome struct {
	Articles       []model.Article
	DuplicateKeys  map[string]struct{}
	SeenBeforeKeys map[string]struct{}
}

// Dedup 过滤已读文章。save 为 true 时将新文章标记为已读。
func Dedup(articles []model.Article, save bool, store SeenStore) (DedupOutcome, error) {
	seen, err := store.Load()
	if err != nil {
		return DedupOutcome{}, err
	}
	historicalSeenMap := make(map[string]bool)
	for _, s := range seen {
		historicalSeenMap[s.URL] = true
	}
	batchSeenMap := make(map[string]bool)

	outcome := DedupOutcome{
		DuplicateKeys:  make(map[string]struct{}),
		SeenBeforeKeys: make(map[string]struct{}),
	}
	for _, a := range articles {
		key := dedupKey(a)
		if historicalSeenMap[key] {
			outcome.SeenBeforeKeys[key] = struct{}{}
			continue
		}
		if batchSeenMap[key] {
			outcome.DuplicateKeys[key] = struct{}{}
			continue
		}
		outcome.Articles = append(outcome.Articles, a)
		batchSeenMap[key] = true
		if save {
			seen = append(seen, seenEntry{URL: key, Time: time.Now()})
		}
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
			return DedupOutcome{}, err
		}
	}

	return outcome, nil
}

func DedupInBatch(articles []model.Article) DedupOutcome {
	seen := make(map[string]bool)
	outcome := DedupOutcome{
		DuplicateKeys:  make(map[string]struct{}),
		SeenBeforeKeys: make(map[string]struct{}),
	}
	for _, a := range articles {
		key := dedupKey(a)
		if seen[key] {
			outcome.DuplicateKeys[key] = struct{}{}
			continue
		}
		seen[key] = true
		outcome.Articles = append(outcome.Articles, a)
	}
	return outcome
}

func dedupKey(a model.Article) string {
	if a.Link != "" {
		return a.Link
	}
	return a.Title + "|" + a.Source
}

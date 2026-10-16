package fetcher

import (
	"context"
	"net/url"
	"strings"
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
	return DedupContext(context.Background(), articles, save, store)
}

func DedupContext(ctx context.Context, articles []model.Article, save bool, store SeenStore) (DedupOutcome, error) {
	if err := ctx.Err(); err != nil {
		return DedupOutcome{}, err
	}
	seen, err := store.Load()
	if err != nil {
		return DedupOutcome{}, err
	}
	historicalSeenMap := make(map[string]bool)
	for _, s := range seen {
		historicalSeenMap[canonicalArticleURL(s.URL)] = true
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
		if err := ctx.Err(); err != nil {
			return DedupOutcome{}, err
		}
		cutoff := time.Now().AddDate(0, 0, -7)
		var trimmed []seenEntry
		for _, s := range seen {
			if s.Time.After(cutoff) {
				trimmed = append(trimmed, s)
			}
		}
		if err := ctx.Err(); err != nil {
			return DedupOutcome{}, err
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
		return canonicalArticleURL(a.Link)
	}
	return a.Title + "|" + a.Source
}

// canonicalArticleURL only joins the known official article aliases. Other links
// retain their original keys, including their query strings and fragments.
func canonicalArticleURL(link string) string {
	u, err := url.Parse(link)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Port() != "" {
		return link
	}

	var canonicalPath string
	switch strings.ToLower(u.Hostname()) {
	case "deepmind.google":
		if slug, ok := articleSlug(u.Path, "/blog/"); ok {
			canonicalPath = "https://deepmind.google/blog/" + url.PathEscape(slug)
		}
	case "blog.google":
		for _, prefix := range []string{
			"/innovation-and-ai/models-and-research/gemini-models/",
			"/innovation-and-ai/models-and-research/google-deepmind/",
			"/technology/ai/",
		} {
			if slug, ok := articleSlug(u.Path, prefix); ok {
				canonicalPath = "https://deepmind.google/blog/" + url.PathEscape(slug)
				break
			}
		}
	case "developer.nvidia.com":
		if slug, ok := articleSlug(u.Path, "/blog/"); ok {
			canonicalPath = "https://developer.nvidia.com/blog/" + url.PathEscape(slug)
		}
	}
	if canonicalPath == "" {
		return link
	}

	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return link
	}
	for key := range query {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "utm_") || lower == "gclid" || lower == "fbclid" || lower == "dclid" || lower == "msclkid" {
			delete(query, key)
		}
	}
	if encoded := query.Encode(); encoded != "" {
		return canonicalPath + "?" + encoded
	}
	return canonicalPath
}

func articleSlug(path, prefix string) (string, bool) {
	slug, ok := strings.CutPrefix(path, prefix)
	if !ok {
		return "", false
	}
	slug = strings.TrimSuffix(slug, "/")
	return slug, slug != "" && slug != "." && slug != ".." && !strings.Contains(slug, "/")
}

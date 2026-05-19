package fetcher

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/model"
)

const xVisibleSourceName = "X Visible"

type xVisibleArticle struct {
	Kind          string   `json:"kind"`
	SchemaVersion int      `json:"schemaVersion"`
	TargetRaw     string   `json:"targetRaw"`
	TargetType    string   `json:"targetType"`
	TargetURL     string   `json:"targetUrl"`
	SourceURL     string   `json:"sourceUrl"`
	FinalURL      string   `json:"finalUrl"`
	Title         string   `json:"title"`
	ExtractedAt   string   `json:"extractedAt"`
	Text          string   `json:"text"`
	Datetime      string   `json:"datetime"`
	StatusURL     string   `json:"statusUrl"`
	StatusLinks   []string `json:"statusLinks"`
	LinkCount     int      `json:"linkCount"`
	ImageCount    int      `json:"imageCount"`
	VideoCount    int      `json:"videoCount"`
}

func fetchXVisibleNDJSON(ctx context.Context, cfg config.XAccountsConfig, keywords []string, from time.Time, to time.Time) ([]sourceFetchResult, []FailedSource, error) {
	if !cfg.Enabled {
		return nil, nil, nil
	}
	allowedAccounts := xAccountHandleSet(cfg.Accounts)
	seen := map[string]struct{}{}
	var candidates []fetchedCandidate
	var failed []FailedSource
	for _, input := range []struct {
		name string
		path string
	}{
		{name: "X accounts", path: cfg.AccountsPath},
		{name: "X searches", path: cfg.SearchesPath},
	} {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if strings.TrimSpace(input.path) == "" {
			continue
		}
		items, err := readXVisibleNDJSONFile(input.path)
		if err != nil {
			failed = append(failed, FailedSource{Name: input.name, Err: err})
			continue
		}
		for _, item := range items {
			candidate, ok := xVisibleArticleCandidate(item, cfg.Category, keywords, from, to, allowedAccounts)
			if !ok {
				continue
			}
			key := xVisibleDedupKey(item)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			candidates = append(candidates, candidate)
		}
	}
	if len(candidates) == 0 {
		return nil, failed, nil
	}
	return []sourceFetchResult{{Source: config.Source{Name: xVisibleSourceName, Category: cfg.Category}, Candidates: candidates}}, failed, nil
}

func readXVisibleNDJSONFile(path string) ([]xVisibleArticle, error) {
	file, err := os.Open(expandHomePath(path))
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var items []xVisibleArticle
	scanner := bufio.NewScanner(file)
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		var item xVisibleArticle
		if err := json.Unmarshal([]byte(text), &item); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		items = append(items, item)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func xVisibleArticleCandidate(item xVisibleArticle, category string, keywords []string, from time.Time, to time.Time, allowedAccounts map[string]struct{}) (fetchedCandidate, bool) {
	if item.Kind != "x-visible-article" || item.SchemaVersion != 1 || strings.TrimSpace(item.Text) == "" {
		return fetchedCandidate{}, false
	}
	published, err := time.Parse(time.RFC3339, item.Datetime)
	if err != nil || !articleWithinWindow(model.Article{Published: published}, from, to) {
		return fetchedCandidate{}, false
	}
	if item.TargetType == "account" {
		handle := xVisibleHandle(item)
		if _, ok := allowedAccounts[strings.ToLower(handle)]; !ok {
			return fetchedCandidate{}, false
		}
	}
	link := strings.TrimSpace(item.StatusURL)
	if link == "" {
		link = strings.TrimSpace(item.FinalURL)
	}
	if link == "" {
		link = strings.TrimSpace(item.SourceURL)
	}
	return fetchedCandidate{
		Article: model.Article{
			Title:     xVisibleTitle(item),
			Link:      link,
			Summary:   item.Text,
			Source:    xVisibleSource(item),
			Category:  category,
			Published: published,
		},
		MatchedKeywords: matchedKeywords(item.Text, keywords),
	}, true
}

func xAccountHandleSet(accounts []config.XAccountConfig) map[string]struct{} {
	set := make(map[string]struct{}, len(accounts))
	for _, account := range accounts {
		handle := strings.TrimPrefix(strings.TrimSpace(account.Handle), "@")
		if handle != "" {
			set[strings.ToLower(handle)] = struct{}{}
		}
	}
	return set
}

func xVisibleHandle(item xVisibleArticle) string {
	if handle, ok := strings.CutPrefix(item.TargetRaw, "/twitter/user/"); ok {
		return handle
	}
	u, err := url.Parse(item.TargetURL)
	if err != nil {
		return ""
	}
	return strings.Trim(strings.TrimPrefix(u.Path, "/"), "/")
}

func xVisibleSource(item xVisibleArticle) string {
	if item.TargetType == "search" {
		return "X Search/" + strings.TrimSpace(item.TargetRaw)
	}
	return "X/@" + xVisibleHandle(item)
}

func xVisibleTitle(item xVisibleArticle) string {
	trimmed := strings.TrimSpace(item.Text)
	if len([]rune(trimmed)) <= 120 {
		return trimmed
	}
	runes := []rune(trimmed)
	return string(runes[:120])
}

func xVisibleDedupKey(item xVisibleArticle) string {
	if strings.TrimSpace(item.StatusURL) != "" {
		return strings.TrimSpace(item.StatusURL)
	}
	return strings.TrimSpace(item.FinalURL) + "\n" + strings.TrimSpace(item.Text)
}

func expandHomePath(path string) string {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return home + path[1:]
		}
	}
	return path
}

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
	Kind             string   `json:"kind"`
	SchemaVersion    int      `json:"schemaVersion"`
	TargetRaw        string   `json:"targetRaw"`
	TargetType       string   `json:"targetType"`
	TargetURL        string   `json:"targetUrl"`
	SourceURL        string   `json:"sourceUrl"`
	FinalURL         string   `json:"finalUrl"`
	Title            string   `json:"title"`
	ExtractedAt      string   `json:"extractedAt"`
	WindowFrom       string   `json:"windowFrom"`
	WindowTo         string   `json:"windowTo"`
	ScrollStopReason string   `json:"scrollStopReason"`
	Text             string   `json:"text"`
	Datetime         string   `json:"datetime"`
	StatusURL        string   `json:"statusUrl"`
	StatusLinks      []string `json:"statusLinks"`
	LinkCount        int      `json:"linkCount"`
	ImageCount       int      `json:"imageCount"`
	VideoCount       int      `json:"videoCount"`
}

type xVisibleRefreshStatus struct {
	Kind          string                `json:"kind"`
	SchemaVersion int                   `json:"schemaVersion"`
	Job           string                `json:"job"`
	Period        string                `json:"period"`
	Window        xVisibleRefreshWindow `json:"window"`
	Status        string                `json:"status"`
	StartedAt     string                `json:"startedAt"`
	FinishedAt    string                `json:"finishedAt"`
	Error         string                `json:"error"`
	Outputs       map[string]string     `json:"outputs"`
}

type xVisibleRefreshWindow struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func fetchXVisibleNDJSON(ctx context.Context, cfg config.XAccountsConfig, keywords []string, from time.Time, to time.Time) ([]sourceFetchResult, []FailedSource, error) {
	if !cfg.Enabled {
		return nil, nil, nil
	}
	var failed []FailedSource
	refreshFailure, err := waitForXVisibleRefresh(ctx, cfg, from, to)
	if err != nil {
		return nil, nil, err
	}
	if refreshFailure != nil {
		failed = append(failed, *refreshFailure)
	}
	allowedAccounts := xAccountHandleSet(cfg.Accounts)
	seen := map[string]struct{}{}
	coverageWarnings := map[string]struct{}{}
	var candidates []fetchedCandidate
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
			if warning := xVisibleCoverageWarning(item, from, to); warning != nil {
				key := warning.Name + "\n" + warning.Err.Error()
				if _, exists := coverageWarnings[key]; !exists {
					coverageWarnings[key] = struct{}{}
					failed = append(failed, *warning)
				}
			}
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

func waitForXVisibleRefresh(ctx context.Context, cfg config.XAccountsConfig, from, to time.Time) (*FailedSource, error) {
	path := strings.TrimSpace(cfg.RefreshStatusPath)
	if path == "" {
		return nil, nil
	}
	timeout := cfg.RefreshWaitTimeout
	if timeout <= 0 {
		timeout = config.DefaultXRefreshWaitTimeout
	}
	interval := cfg.RefreshWaitInterval
	if interval <= 0 {
		interval = config.DefaultXRefreshWaitInterval
	}
	deadline := time.Now().Add(timeout)

	for {
		status, err := readXVisibleRefreshStatus(path)
		if os.IsNotExist(err) {
			return nil, nil
		}
		if err != nil {
			return &FailedSource{Name: "X refresh status", Err: err}, nil
		}

		switch strings.ToLower(strings.TrimSpace(status.Status)) {
		case "running":
			if !xVisibleRefreshWindowOverlaps(status.Window, from, to) {
				return nil, nil
			}
			if !time.Now().Before(deadline) {
				return &FailedSource{Name: "X refresh status", Err: fmt.Errorf("refresh still running after %s", timeout)}, nil
			}
			wait := interval
			if remaining := time.Until(deadline); remaining < wait {
				wait = remaining
			}
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return nil, ctx.Err()
			case <-timer.C:
			}
		case "failed":
			if !xVisibleRefreshWindowOverlaps(status.Window, from, to) {
				return nil, nil
			}
			message := strings.TrimSpace(status.Error)
			if message == "" {
				message = "refresh failed"
			} else {
				message = "refresh failed: " + message
			}
			return &FailedSource{Name: "X refresh status", Err: fmt.Errorf("%s", message)}, nil
		default:
			return nil, nil
		}
	}
}

func readXVisibleRefreshStatus(path string) (xVisibleRefreshStatus, error) {
	data, err := os.ReadFile(expandHomePath(path))
	if err != nil {
		return xVisibleRefreshStatus{}, err
	}
	var status xVisibleRefreshStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return xVisibleRefreshStatus{}, err
	}
	return status, nil
}

func xVisibleRefreshWindowOverlaps(window xVisibleRefreshWindow, from, to time.Time) bool {
	windowFrom, err := time.Parse(time.RFC3339, strings.TrimSpace(window.From))
	if err != nil {
		return true
	}
	windowTo, err := time.Parse(time.RFC3339, strings.TrimSpace(window.To))
	if err != nil {
		return true
	}
	if !windowTo.After(windowFrom) {
		return true
	}
	return windowFrom.Before(to) && windowTo.After(from)
}

func xVisibleCoverageWarning(item xVisibleArticle, from, to time.Time) *FailedSource {
	if item.Kind != "x-visible-article" || item.SchemaVersion != 1 {
		return nil
	}
	reason := strings.TrimSpace(item.ScrollStopReason)
	if reason == "" || reason == "covered-window-start" {
		return nil
	}
	if strings.TrimSpace(item.WindowFrom) != "" || strings.TrimSpace(item.WindowTo) != "" {
		if !xVisibleRefreshWindowOverlaps(xVisibleRefreshWindow{From: item.WindowFrom, To: item.WindowTo}, from, to) {
			return nil
		}
	}
	target := strings.TrimSpace(item.TargetRaw)
	if item.TargetType == "account" {
		if handle := xVisibleHandle(item); handle != "" {
			target = handle
		}
	}
	if target == "" {
		target = strings.TrimSpace(item.TargetURL)
	}
	if target == "" {
		target = "unknown"
	}
	return &FailedSource{
		Name: "X coverage/" + target,
		Err:  fmt.Errorf("target may not fully cover requested window: %s", reason),
	}
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

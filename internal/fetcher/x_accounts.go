package fetcher

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/model"
)

const xVisibleSourceName = "X Visible"

var xVisibleHistoryPeriodPattern = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z$`)

type xVisibleReadOptions struct {
	useHistory bool
	historyDir string
}

type xVisibleHistoryArchive struct {
	dir    string
	name   string
	status xVisibleRefreshStatus
}

type xVisibleNDJSONInput struct {
	name string
	path string
}

type xVisibleStatusWarningInput struct {
	name   string
	status xVisibleRefreshStatus
}

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
	Kind          string                         `json:"kind"`
	SchemaVersion int                            `json:"schemaVersion"`
	Job           string                         `json:"job"`
	Period        string                         `json:"period"`
	Window        xVisibleRefreshWindow          `json:"window"`
	Status        string                         `json:"status"`
	StartedAt     string                         `json:"startedAt"`
	FinishedAt    string                         `json:"finishedAt"`
	Error         string                         `json:"error"`
	Outputs       map[string]string              `json:"outputs"`
	TargetSummary *xVisibleRefreshTargetSummary  `json:"targetSummary"`
	Targets       []xVisibleRefreshTargetDetails `json:"targets"`
}

type xVisibleRefreshWindow struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type xVisibleRefreshTargetSummary struct {
	TargetCount          int `json:"targetCount"`
	OKCount              int `json:"okCount"`
	ErrorCount           int `json:"errorCount"`
	TimeoutCount         int `json:"timeoutCount"`
	LoginSignalCount     int `json:"loginSignalCount"`
	ChallengeSignalCount int `json:"challengeSignalCount"`
	RetryCount           int `json:"retryCount"`
}

type xVisibleRefreshTargetDetails struct {
	TargetRaw        string          `json:"targetRaw"`
	TargetType       string          `json:"targetType"`
	TargetURL        string          `json:"targetUrl"`
	LoadStopReason   string          `json:"loadStopReason"`
	ScrollStopReason string          `json:"scrollStopReason"`
	LoginSignals     json.RawMessage `json:"loginSignals"`
	ChallengeSignals json.RawMessage `json:"challengeSignals"`
	Attempts         int             `json:"attempts"`
	Error            string          `json:"error"`
	ArticleCount     int             `json:"articleCount"`
}

func fetchXVisibleNDJSON(ctx context.Context, cfg config.XAccountsConfig, keywords []string, from time.Time, to time.Time) ([]sourceFetchResult, []FailedSource, error) {
	return fetchXVisibleNDJSONWithOptions(ctx, cfg, keywords, from, to, xVisibleReadOptions{})
}

func fetchXVisibleNDJSONWithOptions(ctx context.Context, cfg config.XAccountsConfig, keywords []string, from time.Time, to time.Time, options xVisibleReadOptions) ([]sourceFetchResult, []FailedSource, error) {
	if !cfg.Enabled {
		return nil, nil, nil
	}
	if options.useHistory {
		return fetchXVisibleHistoryNDJSON(ctx, cfg, keywords, from, to, options.historyDir)
	}
	var failed []FailedSource
	refreshFailure, err := waitForXVisibleRefresh(ctx, cfg, from, to)
	if err != nil {
		return nil, nil, err
	}
	if refreshFailure != nil {
		failed = append(failed, *refreshFailure)
	} else {
		statusWarnings, err := xVisibleRefreshStatusWarnings(cfg.RefreshStatusPath, from, to)
		if err != nil {
			failed = append(failed, FailedSource{Name: "X refresh status", Err: err})
		} else {
			failed = append(failed, statusWarnings...)
		}
	}
	inputs := []xVisibleNDJSONInput{
		{name: "X accounts", path: cfg.AccountsPath},
		{name: "X searches", path: cfg.SearchesPath},
	}
	return collectXVisibleNDJSON(ctx, cfg, keywords, from, to, inputs, nil, failed)
}

func fetchXVisibleHistoryNDJSON(ctx context.Context, cfg config.XAccountsConfig, keywords []string, from time.Time, to time.Time, historyDir string) ([]sourceFetchResult, []FailedSource, error) {
	archives, failed := xVisibleHistoryArchives(historyDir, from, to)
	var inputs []xVisibleNDJSONInput
	var statuses []xVisibleStatusWarningInput
	for _, archive := range archives {
		statuses = append(statuses, xVisibleStatusWarningInput{name: "X history status/" + archive.name, status: archive.status})
		inputs = append(inputs,
			xVisibleNDJSONInput{name: "X accounts history/" + archive.name, path: filepath.Join(archive.dir, "accounts.ndjson.gz")},
			xVisibleNDJSONInput{name: "X searches history/" + archive.name, path: filepath.Join(archive.dir, "searches.ndjson.gz")},
		)
	}
	return collectXVisibleNDJSON(ctx, cfg, keywords, from, to, inputs, statuses, failed)
}

func collectXVisibleNDJSON(ctx context.Context, cfg config.XAccountsConfig, keywords []string, from time.Time, to time.Time, inputs []xVisibleNDJSONInput, statuses []xVisibleStatusWarningInput, failed []FailedSource) ([]sourceFetchResult, []FailedSource, error) {
	allowedAccounts := xAccountHandleSet(cfg.Accounts)
	seen := map[string]struct{}{}
	coverageWarnings := map[string]struct{}{}
	for _, input := range statuses {
		if !xVisibleRefreshWindowOverlaps(input.status.Window, from, to) {
			continue
		}
		failed = append(failed, xVisibleRefreshStatusWarningsFromStatus(input.status)...)
	}
	limit := cfg.MaxPostsPerTarget
	if limit < 1 {
		limit = config.DefaultXMaxPostsPerTarget
	}
	groupedCandidates := map[string][]fetchedCandidate{}
	var targetOrder []string
	for _, input := range inputs {
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
			target := xVisibleLimitTarget(item)
			if _, exists := groupedCandidates[target]; !exists {
				targetOrder = append(targetOrder, target)
			}
			groupedCandidates[target] = append(groupedCandidates[target], candidate)
		}
	}
	var candidates []fetchedCandidate
	for _, target := range targetOrder {
		candidates = append(candidates, limitXVisibleCandidates(groupedCandidates[target], limit)...)
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

	var reader io.Reader = file
	if strings.HasSuffix(path, ".gz") {
		gzipReader, err := gzip.NewReader(file)
		if err != nil {
			return nil, err
		}
		defer gzipReader.Close()
		reader = gzipReader
	}

	var items []xVisibleArticle
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
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

func xVisibleHistoryArchives(historyDir string, from, to time.Time) ([]xVisibleHistoryArchive, []FailedSource) {
	trimmed := strings.TrimSpace(historyDir)
	if trimmed == "" {
		return nil, []FailedSource{{Name: "X history", Err: fmt.Errorf("x visible history dir is required")}}
	}
	base := expandHomePath(trimmed)
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil, []FailedSource{{Name: "X history", Err: err}}
	}

	var archives []xVisibleHistoryArchive
	var failed []FailedSource
	for _, entry := range entries {
		if !entry.IsDir() || !xVisibleHistoryPeriodPattern.MatchString(entry.Name()) {
			continue
		}
		dir := filepath.Join(base, entry.Name())
		status, err := readXVisibleRefreshStatus(filepath.Join(dir, "status.json"))
		if err != nil {
			failed = append(failed, FailedSource{Name: "X history/" + entry.Name(), Err: err})
			continue
		}
		if strings.ToLower(strings.TrimSpace(status.Status)) != "succeeded" {
			continue
		}
		if !xVisibleRefreshWindowOverlaps(status.Window, from, to) {
			continue
		}
		archives = append(archives, xVisibleHistoryArchive{dir: dir, name: entry.Name(), status: status})
	}

	sort.Slice(archives, func(i, j int) bool {
		left := xVisibleArchiveWindowStart(archives[i].status)
		right := xVisibleArchiveWindowStart(archives[j].status)
		if !left.Equal(right) {
			return left.Before(right)
		}
		return archives[i].name < archives[j].name
	})
	return archives, failed
}

func xVisibleArchiveWindowStart(status xVisibleRefreshStatus) time.Time {
	start, err := time.Parse(time.RFC3339, strings.TrimSpace(status.Window.From))
	if err != nil {
		return time.Time{}
	}
	return start
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

func xVisibleRefreshStatusWarnings(path string, from, to time.Time) ([]FailedSource, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}
	status, err := readXVisibleRefreshStatus(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if strings.ToLower(strings.TrimSpace(status.Status)) != "succeeded" {
		return nil, nil
	}
	if !xVisibleRefreshWindowOverlaps(status.Window, from, to) {
		return nil, nil
	}
	return xVisibleRefreshStatusWarningsFromStatus(status), nil
}

func xVisibleRefreshStatusWarningsFromStatus(status xVisibleRefreshStatus) []FailedSource {
	var failed []FailedSource
	if warning := xVisibleRefreshSummaryWarning(status.TargetSummary, status.Targets); warning != nil {
		failed = append(failed, *warning)
	}
	for _, target := range status.Targets {
		if warning := xVisibleRefreshTargetWarning(target); warning != nil {
			failed = append(failed, *warning)
		}
	}
	return failed
}

func xVisibleRefreshSummaryWarning(summary *xVisibleRefreshTargetSummary, targets []xVisibleRefreshTargetDetails) *FailedSource {
	if summary == nil {
		return nil
	}
	challengeSignalCount := summary.ChallengeSignalCount
	if len(targets) > 0 {
		challengeSignalCount = 0
		for _, target := range targets {
			if xVisibleTargetChallengeProblem(target) {
				challengeSignalCount++
			}
		}
	}
	if summary.ErrorCount == 0 && summary.TimeoutCount == 0 && summary.LoginSignalCount == 0 && challengeSignalCount == 0 {
		return nil
	}
	return &FailedSource{
		Name: "X refresh targets",
		Err: fmt.Errorf(
			"target diagnostics: targets=%d ok=%d errors=%d timeouts=%d loginSignals=%d challengeSignals=%d retries=%d",
			summary.TargetCount,
			summary.OKCount,
			summary.ErrorCount,
			summary.TimeoutCount,
			summary.LoginSignalCount,
			challengeSignalCount,
			summary.RetryCount,
		),
	}
}

func xVisibleRefreshTargetWarning(target xVisibleRefreshTargetDetails) *FailedSource {
	loadStopReason := strings.TrimSpace(target.LoadStopReason)
	messageParts := []string(nil)
	if xVisibleProblemLoadStopReason(loadStopReason) {
		messageParts = append(messageParts, "loadStopReason="+loadStopReason)
	}
	if xVisibleRawSignalPresent(target.LoginSignals) {
		messageParts = append(messageParts, "loginSignals=true")
	}
	if xVisibleTargetChallengeProblem(target) {
		messageParts = append(messageParts, "challengeSignals=true")
	}
	if target.Attempts > 0 {
		messageParts = append(messageParts, fmt.Sprintf("attempts=%d", target.Attempts))
	}
	if target.ArticleCount > 0 {
		messageParts = append(messageParts, fmt.Sprintf("articles=%d", target.ArticleCount))
	}
	if message := strings.TrimSpace(target.Error); message != "" {
		messageParts = append(messageParts, "error="+message)
	}
	if len(messageParts) == 0 || !xVisibleTargetHasProblem(target) {
		return nil
	}
	return &FailedSource{
		Name: "X target/" + xVisibleRefreshTargetLabel(target),
		Err:  fmt.Errorf("target diagnostics: %s", strings.Join(messageParts, ", ")),
	}
}

func xVisibleTargetHasProblem(target xVisibleRefreshTargetDetails) bool {
	return xVisibleProblemLoadStopReason(strings.TrimSpace(target.LoadStopReason)) ||
		xVisibleRawSignalPresent(target.LoginSignals) ||
		xVisibleTargetChallengeProblem(target) ||
		strings.TrimSpace(target.Error) != ""
}

func xVisibleTargetChallengeProblem(target xVisibleRefreshTargetDetails) bool {
	loadStopReason := strings.TrimSpace(target.LoadStopReason)
	if strings.EqualFold(loadStopReason, "challenge-signal") {
		return true
	}
	if !xVisibleRawSignalPresent(target.ChallengeSignals) {
		return false
	}
	return !strings.EqualFold(loadStopReason, "article-ready") || target.ArticleCount == 0
}

func xVisibleProblemLoadStopReason(reason string) bool {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "timeout", "login-signal", "challenge-signal", "error":
		return true
	default:
		return false
	}
}

func xVisibleRawSignalPresent(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return true
	}
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		return strings.TrimSpace(typed) != ""
	case float64:
		return typed != 0
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	default:
		return true
	}
}

func xVisibleRefreshTargetLabel(target xVisibleRefreshTargetDetails) string {
	raw := strings.TrimSpace(target.TargetRaw)
	if target.TargetType == "account" {
		if handle, ok := strings.CutPrefix(raw, "/twitter/user/"); ok && strings.TrimSpace(handle) != "" {
			return strings.TrimSpace(handle)
		}
	}
	if raw != "" {
		return raw
	}
	if targetURL := strings.TrimSpace(target.TargetURL); targetURL != "" {
		return targetURL
	}
	return "unknown"
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
	if item.TargetType == "search" && reason == "max-scrolls" {
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

func xVisibleLimitTarget(item xVisibleArticle) string {
	if item.TargetType == "account" {
		if handle := xVisibleHandle(item); handle != "" {
			return "account:" + strings.ToLower(handle)
		}
	}
	target := strings.TrimSpace(item.TargetRaw)
	if target == "" {
		target = strings.TrimSpace(item.TargetURL)
	}
	if target == "" {
		target = strings.TrimSpace(item.SourceURL)
	}
	if target == "" {
		target = strings.TrimSpace(item.FinalURL)
	}
	if target == "" {
		return "unknown"
	}
	if item.TargetType == "" {
		return strings.ToLower(target)
	}
	return item.TargetType + ":" + strings.ToLower(target)
}

func limitXVisibleCandidates(candidates []fetchedCandidate, limit int) []fetchedCandidate {
	if len(candidates) <= limit {
		return candidates
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Article.Published.After(candidates[j].Article.Published)
	})
	return candidates[:limit]
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

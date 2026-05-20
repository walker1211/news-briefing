package fetcher

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
)

func TestFetchWindowDetailedIncludesXVisibleNDJSON(t *testing.T) {
	dir := t.TempDir()
	accountsPath := filepath.Join(dir, "accounts.ndjson")
	content := `{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"/twitter/user/thsottiaux","targetType":"account","targetUrl":"https://x.com/thsottiaux","sourceUrl":"https://x.com/thsottiaux","finalUrl":"https://x.com/thsottiaux","extractedAt":"2026-05-19T07:08:15.239Z","text":"Tibo @thsottiaux · 5月19日 Codex team fixed GPT-5.5 degradation and will reset usage limits","datetime":"2026-05-19T07:00:00.000Z","statusUrl":"https://x.com/thsottiaux/status/1","statusLinks":["https://x.com/thsottiaux/status/1"],"linkCount":1,"imageCount":0,"videoCount":0}
`
	if err := os.WriteFile(accountsPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write accounts ndjson: %v", err)
	}
	cfg := &config.Config{
		Keywords: []string{"Codex"},
		Output:   config.OutputCfg{Dir: dir},
		XAccounts: config.XAccountsConfig{
			Enabled:      true,
			AccountsPath: accountsPath,
			Category:     "AI/科技",
			Accounts:     []config.XAccountConfig{{Handle: "thsottiaux"}},
		},
	}
	from := time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	fetchAll := func(ctx context.Context, cfg *config.Config, since time.Time) ([]sourceFetchResult, []FailedSource, error) {
		return nil, nil, nil
	}

	result, err := fetchWindowDetailedContext(context.Background(), cfg, from, to, false, true, fetchAll)
	if err != nil {
		t.Fatalf("fetchWindowDetailedContext() error = %v", err)
	}
	if len(result.Articles) != 1 {
		t.Fatalf("len(Articles) = %d, want 1", len(result.Articles))
	}
	if result.Articles[0].Link != "https://x.com/thsottiaux/status/1" {
		t.Fatalf("Article.Link = %q", result.Articles[0].Link)
	}
}

func TestFetchXVisibleNDJSONWaitsForRunningRefresh(t *testing.T) {
	dir := t.TempDir()
	accountsPath := filepath.Join(dir, "accounts.ndjson")
	statusPath := filepath.Join(dir, "status.json")
	if err := os.WriteFile(statusPath, []byte(`{"kind":"x-visible-refresh-status","schemaVersion":1,"job":"x-visible-ai","period":"2026-05-20T08:00:00+08:00","status":"running","startedAt":"2026-05-20T00:00:00.000Z","window":{"from":"2026-05-20T00:00:00Z","to":"2026-05-20T08:00:00Z"},"outputs":{"accounts":"`+accountsPath+`"}}`), 0o644); err != nil {
		t.Fatalf("write status: %v", err)
	}
	go func() {
		time.Sleep(10 * time.Millisecond)
		content := `{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"/twitter/user/OpenAI","targetType":"account","targetUrl":"https://x.com/OpenAI","sourceUrl":"https://x.com/OpenAI","finalUrl":"https://x.com/OpenAI","text":"OpenAI launched Codex refresh","datetime":"2026-05-20T07:59:00.000Z","statusUrl":"https://x.com/OpenAI/status/fresh","statusLinks":["https://x.com/OpenAI/status/fresh"],"linkCount":1,"imageCount":0,"videoCount":0}
`
		_ = os.WriteFile(accountsPath, []byte(content), 0o644)
		_ = os.WriteFile(statusPath, []byte(`{"kind":"x-visible-refresh-status","schemaVersion":1,"job":"x-visible-ai","period":"2026-05-20T08:00:00+08:00","status":"succeeded","startedAt":"2026-05-20T00:00:00.000Z","finishedAt":"2026-05-20T00:00:10.000Z","window":{"from":"2026-05-20T00:00:00Z","to":"2026-05-20T08:00:00Z"},"outputs":{"accounts":"`+accountsPath+`"}}`), 0o644)
	}()
	cfg := config.XAccountsConfig{
		Enabled:             true,
		AccountsPath:        accountsPath,
		RefreshStatusPath:   statusPath,
		RefreshWaitTimeout:  500 * time.Millisecond,
		RefreshWaitInterval: time.Millisecond,
		Category:            "AI/科技",
		Accounts:            []config.XAccountConfig{{Handle: "OpenAI"}},
	}
	from := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 20, 8, 0, 0, 0, time.UTC)

	results, failed, err := fetchXVisibleNDJSON(context.Background(), cfg, []string{"Codex"}, from, to)
	if err != nil {
		t.Fatalf("fetchXVisibleNDJSON() error = %v", err)
	}
	if len(failed) != 0 {
		t.Fatalf("failed = %#v, want none", failed)
	}
	if len(results) != 1 || len(results[0].Candidates) != 1 {
		t.Fatalf("results = %#v, want one fresh X candidate", results)
	}
	if results[0].Candidates[0].Article.Link != "https://x.com/OpenAI/status/fresh" {
		t.Fatalf("Article.Link = %q", results[0].Candidates[0].Article.Link)
	}
}

func TestFetchXVisibleNDJSONSkipsUnrelatedRunningRefresh(t *testing.T) {
	dir := t.TempDir()
	accountsPath := filepath.Join(dir, "accounts.ndjson")
	statusPath := filepath.Join(dir, "status.json")
	content := `{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"/twitter/user/OpenAI","targetType":"account","targetUrl":"https://x.com/OpenAI","sourceUrl":"https://x.com/OpenAI","finalUrl":"https://x.com/OpenAI","text":"OpenAI launched Codex earlier","datetime":"2026-05-19T23:59:00.000Z","statusUrl":"https://x.com/OpenAI/status/earlier","statusLinks":["https://x.com/OpenAI/status/earlier"],"linkCount":1,"imageCount":0,"videoCount":0}
`
	if err := os.WriteFile(accountsPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write accounts ndjson: %v", err)
	}
	if err := os.WriteFile(statusPath, []byte(`{"kind":"x-visible-refresh-status","schemaVersion":1,"job":"x-visible-ai","period":"2026-05-20T10:00:00+08:00","status":"running","startedAt":"2026-05-20T01:00:00.000Z","window":{"from":"2026-05-20T09:00:00+08:00","to":"2026-05-20T10:00:00+08:00"},"outputs":{"accounts":"`+accountsPath+`"}}`), 0o644); err != nil {
		t.Fatalf("write status: %v", err)
	}
	cfg := config.XAccountsConfig{
		Enabled:             true,
		AccountsPath:        accountsPath,
		RefreshStatusPath:   statusPath,
		RefreshWaitTimeout:  time.Hour,
		RefreshWaitInterval: time.Hour,
		Category:            "AI/科技",
		Accounts:            []config.XAccountConfig{{Handle: "OpenAI"}},
	}
	from := time.Date(2026, 5, 19, 8, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	to := time.Date(2026, 5, 20, 8, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	results, failed, err := fetchXVisibleNDJSON(ctx, cfg, []string{"Codex"}, from, to)
	if err != nil {
		t.Fatalf("fetchXVisibleNDJSON() error = %v", err)
	}
	if len(failed) != 0 {
		t.Fatalf("failed = %#v, want none", failed)
	}
	if len(results) != 1 || len(results[0].Candidates) != 1 {
		t.Fatalf("results = %#v, want one existing X candidate", results)
	}
}

func TestFetchXVisibleNDJSONSkipsUnrelatedFailedRefresh(t *testing.T) {
	dir := t.TempDir()
	accountsPath := filepath.Join(dir, "accounts.ndjson")
	statusPath := filepath.Join(dir, "status.json")
	content := `{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"/twitter/user/OpenAI","targetType":"account","targetUrl":"https://x.com/OpenAI","sourceUrl":"https://x.com/OpenAI","finalUrl":"https://x.com/OpenAI","text":"OpenAI launched Codex earlier","datetime":"2026-05-19T23:59:00.000Z","statusUrl":"https://x.com/OpenAI/status/failed-unrelated","statusLinks":["https://x.com/OpenAI/status/failed-unrelated"],"linkCount":1,"imageCount":0,"videoCount":0}
`
	if err := os.WriteFile(accountsPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write accounts ndjson: %v", err)
	}
	if err := os.WriteFile(statusPath, []byte(`{"kind":"x-visible-refresh-status","schemaVersion":1,"job":"x-visible-ai","period":"2026-05-20T10:00:00+08:00","status":"failed","startedAt":"2026-05-20T01:00:00.000Z","finishedAt":"2026-05-20T01:01:00.000Z","window":{"from":"2026-05-20T09:00:00+08:00","to":"2026-05-20T10:00:00+08:00"},"error":"browser not ready","outputs":{"accounts":"`+accountsPath+`"}}`), 0o644); err != nil {
		t.Fatalf("write status: %v", err)
	}
	cfg := config.XAccountsConfig{
		Enabled:           true,
		AccountsPath:      accountsPath,
		RefreshStatusPath: statusPath,
		Category:          "AI/科技",
		Accounts:          []config.XAccountConfig{{Handle: "OpenAI"}},
	}
	from := time.Date(2026, 5, 19, 8, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	to := time.Date(2026, 5, 20, 8, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))

	_, failed, err := fetchXVisibleNDJSON(context.Background(), cfg, []string{"Codex"}, from, to)
	if err != nil {
		t.Fatalf("fetchXVisibleNDJSON() error = %v", err)
	}
	if len(failed) != 0 {
		t.Fatalf("failed = %#v, want none", failed)
	}
}

func TestFetchWindowDetailedUsesExplicitXVisibleWindowWhenLookbackConfigured(t *testing.T) {
	dir := t.TempDir()
	accountsPath := filepath.Join(dir, "accounts.ndjson")
	content := `{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"/twitter/user/OpenAI","targetType":"account","targetUrl":"https://x.com/OpenAI","sourceUrl":"https://x.com/OpenAI","finalUrl":"https://x.com/OpenAI","text":"OpenAI before scheduled window Codex update","datetime":"2026-05-19T22:30:00.000Z","statusUrl":"https://x.com/OpenAI/status/before-window","statusLinks":["https://x.com/OpenAI/status/before-window"],"linkCount":1,"imageCount":0,"videoCount":0}
{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"/twitter/user/OpenAI","targetType":"account","targetUrl":"https://x.com/OpenAI","sourceUrl":"https://x.com/OpenAI","finalUrl":"https://x.com/OpenAI","text":"OpenAI inside scheduled window Codex update","datetime":"2026-05-20T01:00:00.000Z","statusUrl":"https://x.com/OpenAI/status/inside-window","statusLinks":["https://x.com/OpenAI/status/inside-window"],"linkCount":1,"imageCount":0,"videoCount":0}
`
	if err := os.WriteFile(accountsPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write accounts ndjson: %v", err)
	}
	loc := time.FixedZone("UTC+8", 8*60*60)
	cfg := &config.Config{
		Keywords: []string{"Codex"},
		Output:   config.OutputCfg{Dir: dir},
		XAccounts: config.XAccountsConfig{
			Enabled:      true,
			AccountsPath: accountsPath,
			Lookback:     24 * time.Hour,
			Category:     "AI/科技",
			Accounts:     []config.XAccountConfig{{Handle: "OpenAI"}},
		},
	}
	from := time.Date(2026, 5, 20, 7, 0, 0, 0, loc)
	to := time.Date(2026, 5, 20, 18, 0, 0, 0, loc)
	fetchAll := func(ctx context.Context, cfg *config.Config, since time.Time) ([]sourceFetchResult, []FailedSource, error) {
		return nil, nil, nil
	}

	result, err := fetchWindowDetailedContext(context.Background(), cfg, from, to, false, true, fetchAll)
	if err != nil {
		t.Fatalf("fetchWindowDetailedContext() error = %v", err)
	}
	if len(result.Articles) != 1 {
		t.Fatalf("len(Articles) = %d, want 1", len(result.Articles))
	}
	if result.Articles[0].Link != "https://x.com/OpenAI/status/inside-window" {
		t.Fatalf("Article.Link = %q", result.Articles[0].Link)
	}
}

func TestFetchWindowDetailedWithXLookbackUsesConfiguredLookback(t *testing.T) {
	dir := t.TempDir()
	accountsPath := filepath.Join(dir, "accounts.ndjson")
	content := `{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"/twitter/user/OpenAI","targetType":"account","targetUrl":"https://x.com/OpenAI","sourceUrl":"https://x.com/OpenAI","finalUrl":"https://x.com/OpenAI","extractedAt":"2026-05-19T12:08:15.239Z","text":"OpenAI @OpenAI · 5月18日 Codex mobile app update","datetime":"2026-05-18T13:00:00.000Z","statusUrl":"https://x.com/OpenAI/status/24h","statusLinks":["https://x.com/OpenAI/status/24h"],"linkCount":1,"imageCount":0,"videoCount":0}
`
	if err := os.WriteFile(accountsPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write accounts ndjson: %v", err)
	}
	cfg := &config.Config{
		Keywords: []string{"Codex"},
		Output:   config.OutputCfg{Dir: dir},
		XAccounts: config.XAccountsConfig{
			Enabled:      true,
			AccountsPath: accountsPath,
			Lookback:     24 * time.Hour,
			Category:     "AI/科技",
			Accounts:     []config.XAccountConfig{{Handle: "OpenAI"}},
		},
	}
	from := time.Date(2026, 5, 19, 6, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	fetchAll := func(ctx context.Context, cfg *config.Config, since time.Time) ([]sourceFetchResult, []FailedSource, error) {
		return nil, nil, nil
	}

	result, err := fetchWindowDetailedContextWithXLookback(context.Background(), cfg, from, to, false, true, fetchAll)
	if err != nil {
		t.Fatalf("fetchWindowDetailedContextWithXLookback() error = %v", err)
	}
	if len(result.Articles) != 1 {
		t.Fatalf("len(Articles) = %d, want 1", len(result.Articles))
	}
}

func TestFetchXVisibleNDJSONReportsRefreshTargetDiagnostics(t *testing.T) {
	dir := t.TempDir()
	accountsPath := filepath.Join(dir, "accounts.ndjson")
	statusPath := filepath.Join(dir, "status.json")
	content := `{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"/twitter/user/OpenAI","targetType":"account","targetUrl":"https://x.com/OpenAI","sourceUrl":"https://x.com/OpenAI","finalUrl":"https://x.com/OpenAI","text":"OpenAI Codex update","datetime":"2026-05-19T07:00:00.000Z","statusUrl":"https://x.com/OpenAI/status/diagnostics","statusLinks":["https://x.com/OpenAI/status/diagnostics"],"linkCount":1,"imageCount":0,"videoCount":0}
`
	if err := os.WriteFile(accountsPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write accounts ndjson: %v", err)
	}
	status := `{"kind":"x-visible-refresh-status","schemaVersion":1,"job":"x-visible-ai","period":"2026-05-20T00:00:00.000Z","status":"succeeded","startedAt":"2026-05-20T00:00:00.000Z","finishedAt":"2026-05-20T00:01:00.000Z","window":{"from":"2026-05-19T00:00:00.000Z","to":"2026-05-20T00:00:00.000Z"},"targetSummary":{"targetCount":3,"okCount":1,"errorCount":1,"timeoutCount":1,"loginSignalCount":1,"challengeSignalCount":0,"retryCount":2},"targets":[{"targetRaw":"/twitter/user/OpenAI","targetType":"account","loadStopReason":"article-ready","attempts":1,"articleCount":1},{"targetRaw":"/twitter/user/AnthropicAI","targetType":"account","loadStopReason":"login-signal","loginSignals":["Sign in"],"attempts":2,"articleCount":0},{"targetRaw":"Claude Code","targetType":"search","loadStopReason":"timeout","attempts":3,"error":"timeout waiting for article","articleCount":0}]}
`
	if err := os.WriteFile(statusPath, []byte(status), 0o644); err != nil {
		t.Fatalf("write status: %v", err)
	}
	cfg := config.XAccountsConfig{
		Enabled:           true,
		AccountsPath:      accountsPath,
		RefreshStatusPath: statusPath,
		Category:          "AI/科技",
		Accounts:          []config.XAccountConfig{{Handle: "OpenAI"}},
	}
	from := time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)

	results, failed, err := fetchXVisibleNDJSON(context.Background(), cfg, []string{"Codex"}, from, to)
	if err != nil {
		t.Fatalf("fetchXVisibleNDJSON() error = %v", err)
	}
	if len(results) != 1 || len(results[0].Candidates) != 1 {
		t.Fatalf("results = %#v, want one X candidate", results)
	}
	if len(failed) != 3 {
		t.Fatalf("failed = %#v, want summary and two target warnings", failed)
	}
	assertFailedSourceContains(t, failed, "X refresh targets", "errors=1")
	assertFailedSourceContains(t, failed, "X target/AnthropicAI", "loadStopReason=login-signal")
	assertFailedSourceContains(t, failed, "X target/Claude Code", "timeout waiting for article")
}

func assertFailedSourceContains(t *testing.T, failed []FailedSource, name string, text string) {
	t.Helper()
	for _, item := range failed {
		if item.Name == name && strings.Contains(item.Err.Error(), text) {
			return
		}
	}
	t.Fatalf("failed = %#v, want %s containing %q", failed, name, text)
}

func TestFetchXVisibleNDJSONReportsIncompleteCoverageWarning(t *testing.T) {
	dir := t.TempDir()
	accountsPath := filepath.Join(dir, "accounts.ndjson")
	content := `{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"/twitter/user/OpenAI","targetType":"account","targetUrl":"https://x.com/OpenAI","sourceUrl":"https://x.com/OpenAI","finalUrl":"https://x.com/OpenAI","windowFrom":"2026-05-19T00:00:00.000Z","windowTo":"2026-05-20T00:00:00.000Z","scrollStopReason":"max-scrolls","text":"OpenAI Codex update","datetime":"2026-05-19T07:00:00.000Z","statusUrl":"https://x.com/OpenAI/status/incomplete","statusLinks":["https://x.com/OpenAI/status/incomplete"],"linkCount":1,"imageCount":0,"videoCount":0}
`
	if err := os.WriteFile(accountsPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write accounts ndjson: %v", err)
	}
	cfg := config.XAccountsConfig{
		Enabled:      true,
		AccountsPath: accountsPath,
		Category:     "AI/科技",
		Accounts:     []config.XAccountConfig{{Handle: "OpenAI"}},
	}
	from := time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)

	results, failed, err := fetchXVisibleNDJSON(context.Background(), cfg, []string{"Codex"}, from, to)
	if err != nil {
		t.Fatalf("fetchXVisibleNDJSON() error = %v", err)
	}
	if len(results) != 1 || len(results[0].Candidates) != 1 {
		t.Fatalf("results = %#v, want one X candidate", results)
	}
	if len(failed) != 1 {
		t.Fatalf("failed = %#v, want one coverage warning", failed)
	}
	if failed[0].Name != "X coverage/OpenAI" {
		t.Fatalf("FailedSource.Name = %q", failed[0].Name)
	}
	if !strings.Contains(failed[0].Err.Error(), "max-scrolls") {
		t.Fatalf("FailedSource.Err = %v, want max-scrolls", failed[0].Err)
	}
}

func TestFetchXVisibleNDJSONFiltersWindowWhitelistAndDeduplicates(t *testing.T) {
	dir := t.TempDir()
	accountsPath := filepath.Join(dir, "accounts.ndjson")
	content := `{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"/twitter/user/OpenAI","targetType":"account","targetUrl":"https://x.com/OpenAI","sourceUrl":"https://x.com/OpenAI","finalUrl":"https://x.com/OpenAI","extractedAt":"2026-05-19T07:08:15.239Z","text":"OpenAI @OpenAI · 5月19日 Codex in ChatGPT mobile app","datetime":"2026-05-19T07:00:00.000Z","statusUrl":"https://x.com/OpenAI/status/1","statusLinks":["https://x.com/OpenAI/status/1"],"linkCount":1,"imageCount":1,"videoCount":1}
{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"/twitter/user/OpenAI","targetType":"account","targetUrl":"https://x.com/OpenAI","sourceUrl":"https://x.com/OpenAI","finalUrl":"https://x.com/OpenAI","extractedAt":"2026-05-19T07:08:15.239Z","text":"已置顶 OpenAI @OpenAI · 5月19日 Codex in ChatGPT mobile app","datetime":"2026-05-19T07:00:00.000Z","statusUrl":"https://x.com/OpenAI/status/1","statusLinks":["https://x.com/OpenAI/status/1"],"linkCount":1,"imageCount":1,"videoCount":1}
{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"/twitter/user/notlisted","targetType":"account","targetUrl":"https://x.com/notlisted","sourceUrl":"https://x.com/notlisted","finalUrl":"https://x.com/notlisted","extractedAt":"2026-05-19T07:08:15.239Z","text":"notlisted @notlisted · 5月19日 Codex rumor","datetime":"2026-05-19T07:10:00.000Z","statusUrl":"https://x.com/notlisted/status/2","statusLinks":["https://x.com/notlisted/status/2"],"linkCount":1,"imageCount":0,"videoCount":0}
{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"/twitter/user/OpenAI","targetType":"account","targetUrl":"https://x.com/OpenAI","sourceUrl":"https://x.com/OpenAI","finalUrl":"https://x.com/OpenAI","extractedAt":"2026-05-19T07:08:15.239Z","text":"OpenAI @OpenAI · 5月17日 old Codex news","datetime":"2026-05-17T07:00:00.000Z","statusUrl":"https://x.com/OpenAI/status/3","statusLinks":["https://x.com/OpenAI/status/3"],"linkCount":1,"imageCount":0,"videoCount":0}
`
	if err := os.WriteFile(accountsPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write accounts ndjson: %v", err)
	}

	cfg := config.XAccountsConfig{
		Enabled:      true,
		AccountsPath: accountsPath,
		Category:     "AI/科技",
		Accounts:     []config.XAccountConfig{{Handle: "OpenAI"}},
	}
	from := time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)

	results, failed, err := fetchXVisibleNDJSON(context.Background(), cfg, []string{"Codex"}, from, to)
	if err != nil {
		t.Fatalf("fetchXVisibleNDJSON() error = %v", err)
	}
	if len(failed) != 0 {
		t.Fatalf("failed = %#v, want none", failed)
	}
	if len(results) != 1 || len(results[0].Candidates) != 1 {
		t.Fatalf("results = %#v, want one X candidate", results)
	}
	candidate := results[0].Candidates[0]
	if candidate.Article.Link != "https://x.com/OpenAI/status/1" {
		t.Fatalf("Article.Link = %q", candidate.Article.Link)
	}
	if candidate.Article.Source != "X/@OpenAI" {
		t.Fatalf("Article.Source = %q", candidate.Article.Source)
	}
	if candidate.Article.Category != "AI/科技" {
		t.Fatalf("Article.Category = %q", candidate.Article.Category)
	}
	if len(candidate.MatchedKeywords) != 1 || candidate.MatchedKeywords[0] != "Codex" {
		t.Fatalf("MatchedKeywords = %#v, want Codex", candidate.MatchedKeywords)
	}
}

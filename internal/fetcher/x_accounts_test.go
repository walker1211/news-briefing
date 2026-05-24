package fetcher

import (
	"compress/gzip"
	"context"
	"fmt"
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

func TestFetchXVisibleNDJSONIgnoresWeakChallengeSignalAfterArticlesReady(t *testing.T) {
	dir := t.TempDir()
	accountsPath := filepath.Join(dir, "accounts.ndjson")
	statusPath := filepath.Join(dir, "status.json")
	content := `{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"/twitter/user/merettm","targetType":"account","targetUrl":"https://x.com/merettm","sourceUrl":"https://x.com/merettm","finalUrl":"https://x.com/merettm","text":"Jakub Pachocki reposted OpenAI Codex update","datetime":"2026-05-20T19:06:41.000Z","statusUrl":"https://x.com/OpenAI/status/2057176201782075690","statusLinks":["https://x.com/OpenAI/status/2057176201782075690"],"linkCount":1,"imageCount":0,"videoCount":0}
`
	if err := os.WriteFile(accountsPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write accounts ndjson: %v", err)
	}
	status := `{"kind":"x-visible-refresh-status","schemaVersion":1,"job":"x-visible-ai","period":"2026-05-21T00:00:00.000Z","status":"succeeded","startedAt":"2026-05-21T00:00:00.000Z","finishedAt":"2026-05-21T00:01:00.000Z","window":{"from":"2026-05-20T10:00:00.000Z","to":"2026-05-21T00:00:00.000Z"},"targetSummary":{"targetCount":1,"okCount":1,"errorCount":0,"timeoutCount":0,"loginSignalCount":0,"challengeSignalCount":1,"retryCount":0},"targets":[{"targetRaw":"/twitter/user/merettm","targetType":"account","loadStopReason":"article-ready","challengeSignals":true,"attempts":1,"articleCount":1}]}
`
	if err := os.WriteFile(statusPath, []byte(status), 0o644); err != nil {
		t.Fatalf("write status: %v", err)
	}
	cfg := config.XAccountsConfig{
		Enabled:           true,
		AccountsPath:      accountsPath,
		RefreshStatusPath: statusPath,
		Category:          "AI/科技",
		Accounts:          []config.XAccountConfig{{Handle: "merettm"}},
	}
	from := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)

	results, failed, err := fetchXVisibleNDJSON(context.Background(), cfg, []string{"Codex"}, from, to)
	if err != nil {
		t.Fatalf("fetchXVisibleNDJSON() error = %v", err)
	}
	if len(results) != 1 || len(results[0].Candidates) != 1 {
		t.Fatalf("results = %#v, want one X candidate", results)
	}
	if len(failed) != 0 {
		t.Fatalf("failed = %#v, want weak challenge signal ignored", failed)
	}
}

func TestFetchXVisibleNDJSONIgnoresSearchMaxScrollsCoverageWarning(t *testing.T) {
	dir := t.TempDir()
	searchesPath := filepath.Join(dir, "searches.ndjson")
	content := `{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"search:Codex","targetType":"search","targetUrl":"https://x.com/search?q=Codex&src=typed_query&f=live","sourceUrl":"https://x.com/search?q=Codex&src=typed_query&f=live","finalUrl":"https://x.com/search?q=Codex&src=typed_query&f=live","windowFrom":"2026-05-20T10:00:00.000Z","windowTo":"2026-05-21T00:00:00.000Z","scrollStopReason":"max-scrolls","text":"Codex launched and released a major update","datetime":"2026-05-20T23:59:40.000Z","statusUrl":"https://x.com/example/status/2057249933124886592","statusLinks":["https://x.com/example/status/2057249933124886592"],"linkCount":1,"imageCount":0,"videoCount":0}
`
	if err := os.WriteFile(searchesPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write searches ndjson: %v", err)
	}
	cfg := config.XAccountsConfig{
		Enabled:      true,
		SearchesPath: searchesPath,
		Category:     "AI/科技",
	}
	from := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)

	results, failed, err := fetchXVisibleNDJSON(context.Background(), cfg, []string{"Codex"}, from, to)
	if err != nil {
		t.Fatalf("fetchXVisibleNDJSON() error = %v", err)
	}
	if len(results) != 1 || len(results[0].Candidates) != 1 {
		t.Fatalf("results = %#v, want one X search candidate", results)
	}
	if len(failed) != 0 {
		t.Fatalf("failed = %#v, want search max-scrolls coverage ignored", failed)
	}
}

func TestFetchXVisibleNDJSONLimitsPostsPerTargetForAccountsAndSearches(t *testing.T) {
	dir := t.TempDir()
	accountsPath := filepath.Join(dir, "accounts.ndjson")
	searchesPath := filepath.Join(dir, "searches.ndjson")

	var accountLines strings.Builder
	for i := 0; i < 12; i++ {
		accountLines.WriteString(fmt.Sprintf(`{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"/twitter/user/OpenAI","targetType":"account","targetUrl":"https://x.com/OpenAI","sourceUrl":"https://x.com/OpenAI","finalUrl":"https://x.com/OpenAI","text":"OpenAI Codex account update %02d","datetime":"2026-05-20T%02d:00:00.000Z","statusUrl":"https://x.com/OpenAI/status/account-%02d","statusLinks":["https://x.com/OpenAI/status/account-%02d"]}`+"\n", i, i, i, i))
	}
	if err := os.WriteFile(accountsPath, []byte(accountLines.String()), 0o644); err != nil {
		t.Fatalf("write accounts ndjson: %v", err)
	}

	var searchLines strings.Builder
	for i := 0; i < 12; i++ {
		searchLines.WriteString(fmt.Sprintf(`{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"search:Codex","targetType":"search","targetUrl":"https://x.com/search?q=Codex","sourceUrl":"https://x.com/search?q=Codex","finalUrl":"https://x.com/search?q=Codex","text":"Codex search result %02d","datetime":"2026-05-20T%02d:30:00.000Z","statusUrl":"https://x.com/example/status/search-%02d","statusLinks":["https://x.com/example/status/search-%02d"]}`+"\n", i, i, i, i))
	}
	if err := os.WriteFile(searchesPath, []byte(searchLines.String()), 0o644); err != nil {
		t.Fatalf("write searches ndjson: %v", err)
	}

	cfg := config.XAccountsConfig{
		Enabled:           true,
		AccountsPath:      accountsPath,
		SearchesPath:      searchesPath,
		MaxPostsPerTarget: 10,
		Category:          "AI/科技",
		Accounts:          []config.XAccountConfig{{Handle: "OpenAI"}},
	}
	from := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)

	results, failed, err := fetchXVisibleNDJSON(context.Background(), cfg, []string{"Codex"}, from, to)
	if err != nil {
		t.Fatalf("fetchXVisibleNDJSON() error = %v", err)
	}
	if len(failed) != 0 {
		t.Fatalf("failed = %#v, want none", failed)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}

	counts := map[string]int{}
	links := map[string]struct{}{}
	for _, candidate := range results[0].Candidates {
		counts[candidate.Article.Source]++
		links[candidate.Article.Link] = struct{}{}
	}
	if counts["X/@OpenAI"] != 10 {
		t.Fatalf("account candidate count = %d, want 10", counts["X/@OpenAI"])
	}
	if counts["X Search/search:Codex"] != 10 {
		t.Fatalf("search candidate count = %d, want 10", counts["X Search/search:Codex"])
	}
	for _, oldLink := range []string{
		"https://x.com/OpenAI/status/account-00",
		"https://x.com/OpenAI/status/account-01",
		"https://x.com/example/status/search-00",
		"https://x.com/example/status/search-01",
	} {
		if _, ok := links[oldLink]; ok {
			t.Fatalf("oldest link %q survived target limit", oldLink)
		}
	}
	for _, freshLink := range []string{
		"https://x.com/OpenAI/status/account-11",
		"https://x.com/example/status/search-11",
	} {
		if _, ok := links[freshLink]; !ok {
			t.Fatalf("fresh link %q missing after target limit", freshLink)
		}
	}
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

func TestReadXVisibleNDJSONFileReadsGzip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "accounts.ndjson.gz")
	writeGzipFile(t, path, `{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"/twitter/user/OpenAI","targetType":"account","targetUrl":"https://x.com/OpenAI","text":"OpenAI Codex update","datetime":"2026-05-19T07:00:00.000Z","statusUrl":"https://x.com/OpenAI/status/gzip"}
`)

	items, err := readXVisibleNDJSONFile(path)
	if err != nil {
		t.Fatalf("readXVisibleNDJSONFile() error = %v", err)
	}
	if len(items) != 1 || items[0].StatusURL != "https://x.com/OpenAI/status/gzip" {
		t.Fatalf("items = %#v, want gzip-decoded X visible article", items)
	}
}

func TestFetchXVisibleNDJSONWithOptionsReadsMatchingHistoryArchives(t *testing.T) {
	dir := t.TempDir()
	historyDir := filepath.Join(dir, "history")
	writeXVisibleHistoryArchive(t, historyDir, "20260520T000000Z", "2026-05-19T00:00:00.000Z", "2026-05-20T00:00:00.000Z",
		`{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"/twitter/user/OpenAI","targetType":"account","targetUrl":"https://x.com/OpenAI","sourceUrl":"https://x.com/OpenAI","finalUrl":"https://x.com/OpenAI","text":"OpenAI Codex update","datetime":"2026-05-19T07:00:00.000Z","statusUrl":"https://x.com/OpenAI/status/shared","statusLinks":["https://x.com/OpenAI/status/shared"]}
{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"/twitter/user/notlisted","targetType":"account","targetUrl":"https://x.com/notlisted","sourceUrl":"https://x.com/notlisted","finalUrl":"https://x.com/notlisted","text":"notlisted Codex rumor","datetime":"2026-05-19T08:00:00.000Z","statusUrl":"https://x.com/notlisted/status/skip","statusLinks":["https://x.com/notlisted/status/skip"]}
`, "")
	writeXVisibleHistoryArchive(t, historyDir, "20260521T000000Z", "2026-05-20T00:00:00.000Z", "2026-05-21T00:00:00.000Z",
		`{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"/twitter/user/OpenAI","targetType":"account","targetUrl":"https://x.com/OpenAI","sourceUrl":"https://x.com/OpenAI","finalUrl":"https://x.com/OpenAI","text":"Duplicate OpenAI Codex update","datetime":"2026-05-20T01:00:00.000Z","statusUrl":"https://x.com/OpenAI/status/shared","statusLinks":["https://x.com/OpenAI/status/shared"]}
{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"/twitter/user/OpenAI","targetType":"account","targetUrl":"https://x.com/OpenAI","sourceUrl":"https://x.com/OpenAI","finalUrl":"https://x.com/OpenAI","text":"OpenAI Codex second update","datetime":"2026-05-20T02:00:00.000Z","statusUrl":"https://x.com/OpenAI/status/unique","statusLinks":["https://x.com/OpenAI/status/unique"]}
`,
		`{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"search:Codex","targetType":"search","targetUrl":"https://x.com/search?q=Codex&src=typed_query&f=live","sourceUrl":"https://x.com/search?q=Codex&src=typed_query&f=live","finalUrl":"https://x.com/search?q=Codex&src=typed_query&f=live","text":"Search result about Codex","datetime":"2026-05-20T03:00:00.000Z","statusUrl":"https://x.com/example/status/search","statusLinks":["https://x.com/example/status/search"]}
`)
	writeXVisibleHistoryArchive(t, historyDir, "20260522T000000Z", "2026-05-21T00:00:00.000Z", "2026-05-22T00:00:00.000Z",
		`{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"/twitter/user/OpenAI","targetType":"account","targetUrl":"https://x.com/OpenAI","text":"outside Codex update","datetime":"2026-05-21T01:00:00.000Z","statusUrl":"https://x.com/OpenAI/status/outside"}
`, "")
	ignoredDir := filepath.Join(historyDir, "not-a-period")
	if err := os.MkdirAll(ignoredDir, 0o755); err != nil {
		t.Fatalf("mkdir ignored archive: %v", err)
	}

	cfg := config.XAccountsConfig{
		Enabled:  true,
		Category: "AI/科技",
		Accounts: []config.XAccountConfig{{Handle: "OpenAI"}},
	}
	from := time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)

	results, failed, err := fetchXVisibleNDJSONWithOptions(context.Background(), cfg, []string{"Codex"}, from, to, xVisibleReadOptions{useHistory: true, historyDir: historyDir})
	if err != nil {
		t.Fatalf("fetchXVisibleNDJSONWithOptions() error = %v", err)
	}
	if len(failed) != 0 {
		t.Fatalf("failed = %#v, want none", failed)
	}
	if len(results) != 1 || len(results[0].Candidates) != 3 {
		t.Fatalf("results = %#v, want three deduped history candidates", results)
	}
	links := []string{
		results[0].Candidates[0].Article.Link,
		results[0].Candidates[1].Article.Link,
		results[0].Candidates[2].Article.Link,
	}
	want := []string{
		"https://x.com/OpenAI/status/shared",
		"https://x.com/OpenAI/status/unique",
		"https://x.com/example/status/search",
	}
	for i := range want {
		if links[i] != want[i] {
			t.Fatalf("history links = %#v, want %#v", links, want)
		}
	}
}

func writeGzipFile(t *testing.T, path string, content string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create gzip file: %v", err)
	}
	gzipWriter := gzip.NewWriter(file)
	if _, err := gzipWriter.Write([]byte(content)); err != nil {
		t.Fatalf("write gzip content: %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close gzip file: %v", err)
	}
}

func writeXVisibleHistoryArchive(t *testing.T, historyDir string, periodKey string, windowFrom string, windowTo string, accountsContent string, searchesContent string) {
	t.Helper()
	dir := filepath.Join(historyDir, periodKey)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir archive: %v", err)
	}
	status := `{"kind":"x-visible-refresh-status","schemaVersion":1,"job":"x-visible-ai","period":"` + windowTo + `","status":"succeeded","window":{"from":"` + windowFrom + `","to":"` + windowTo + `"}}
`
	if err := os.WriteFile(filepath.Join(dir, "status.json"), []byte(status), 0o644); err != nil {
		t.Fatalf("write archive status: %v", err)
	}
	writeGzipFile(t, filepath.Join(dir, "accounts.ndjson.gz"), accountsContent)
	writeGzipFile(t, filepath.Join(dir, "searches.ndjson.gz"), searchesContent)
}

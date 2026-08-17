package fetcher

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
	"github.com/walker1211/news-briefing/internal/model"
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

func TestFetchWindowXDetailedReadsOnlyXVisibleOutput(t *testing.T) {
	dir := t.TempDir()
	accountsPath := filepath.Join(dir, "accounts.ndjson")
	content := `{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"/twitter/user/OpenAI","targetType":"account","targetUrl":"https://x.com/OpenAI","sourceUrl":"https://x.com/OpenAI","finalUrl":"https://x.com/OpenAI","text":"OpenAI shipped a Codex update","datetime":"2026-08-13T01:00:00.000Z","statusUrl":"https://x.com/OpenAI/status/x-only","statusLinks":["https://x.com/OpenAI/status/x-only"]}
`
	if err := os.WriteFile(accountsPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write accounts ndjson: %v", err)
	}
	cfg := &config.Config{
		Sources:  []config.Source{{Name: "ordinary source must not run", URL: "://invalid", Type: config.SourceTypeRSS}},
		Keywords: []string{"Codex"},
		Output:   config.OutputCfg{Dir: dir},
		XAccounts: config.XAccountsConfig{
			Enabled:      true,
			AccountsPath: accountsPath,
			Category:     "AI/科技",
			Accounts:     []config.XAccountConfig{{Handle: "OpenAI"}},
		},
	}
	from := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	to := from.Add(10 * time.Hour)

	result, err := fetchWindowXDetailedContext(context.Background(), cfg, from, to, false, true)
	if err != nil {
		t.Fatalf("fetchWindowXDetailedContext() error = %v", err)
	}
	if len(result.Articles) != 1 || result.Articles[0].Link != "https://x.com/OpenAI/status/x-only" {
		t.Fatalf("Articles = %#v, want one X-only article", result.Articles)
	}
}

func TestFetchXVisibleNDJSONWaitsForRunningRefresh(t *testing.T) {
	dir := t.TempDir()
	accountsPath := filepath.Join(dir, "accounts.ndjson")
	statusPath := filepath.Join(dir, "status.json")
	from := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	to := time.Now().UTC().Add(2 * time.Second).Truncate(time.Second)
	published := to.Add(-time.Second)
	status := fmt.Sprintf(`{"kind":"x-visible-refresh-status","schemaVersion":1,"job":"x-visible-ai","period":"%s","status":"running","startedAt":"%s","window":{"from":"%s","to":"%s"},"outputs":{"accounts":"%s"}}`, to.Format(time.RFC3339), from.Format(time.RFC3339), from.Format(time.RFC3339), to.Format(time.RFC3339), accountsPath)
	if err := writeTestFileAtomically(statusPath, []byte(status)); err != nil {
		t.Fatalf("write status: %v", err)
	}
	writeDone := make(chan error, 1)
	go func() {
		time.Sleep(10 * time.Millisecond)
		content := fmt.Sprintf(`{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"/twitter/user/OpenAI","targetType":"account","targetUrl":"https://x.com/OpenAI","sourceUrl":"https://x.com/OpenAI","finalUrl":"https://x.com/OpenAI","text":"OpenAI launched Codex refresh","datetime":"%s","statusUrl":"https://x.com/OpenAI/status/fresh","statusLinks":["https://x.com/OpenAI/status/fresh"],"linkCount":1,"imageCount":0,"videoCount":0}
	`, published.Format(time.RFC3339))
		if err := os.WriteFile(accountsPath, []byte(content), 0o644); err != nil {
			writeDone <- fmt.Errorf("write accounts: %w", err)
			return
		}
		succeeded := fmt.Sprintf(`{"kind":"x-visible-refresh-status","schemaVersion":1,"job":"x-visible-ai","period":"%s","status":"succeeded","startedAt":"%s","finishedAt":"%s","window":{"from":"%s","to":"%s"},"outputs":{"accounts":"%s"}}`, to.Format(time.RFC3339), from.Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339), from.Format(time.RFC3339), to.Format(time.RFC3339), accountsPath)
		writeDone <- writeTestFileAtomically(statusPath, []byte(succeeded))
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

	results, failed, err := fetchXVisibleNDJSON(context.Background(), cfg, []string{"Codex"}, from, to)
	if writeErr := <-writeDone; writeErr != nil {
		t.Fatalf("publish refresh fixtures: %v", writeErr)
	}
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

func writeTestFileAtomically(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	closed := false
	defer func() {
		if !closed {
			_ = tmp.Close()
		}
		_ = os.Remove(tmpPath)
	}()
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	closed = true
	return os.Rename(tmpPath, path)
}

func TestFetchXVisibleNDJSONReportsRunningRefreshAfterWindowCutoff(t *testing.T) {
	dir := t.TempDir()
	accountsPath := filepath.Join(dir, "accounts.ndjson")
	statusPath := filepath.Join(dir, "status.json")
	from := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	to := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	status := fmt.Sprintf(`{"kind":"x-visible-refresh-status","schemaVersion":1,"job":"x-visible-ai","period":"%s","status":"running","startedAt":"%s","window":{"from":"%s","to":"%s"},"outputs":{"accounts":"%s"}}`, to.Format(time.RFC3339), from.Format(time.RFC3339), from.Format(time.RFC3339), to.Format(time.RFC3339), accountsPath)
	if err := os.WriteFile(statusPath, []byte(status), 0o644); err != nil {
		t.Fatalf("write status: %v", err)
	}
	if err := os.WriteFile(accountsPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write accounts: %v", err)
	}
	cfg := config.XAccountsConfig{
		Enabled:             true,
		AccountsPath:        accountsPath,
		RefreshStatusPath:   statusPath,
		RefreshWaitTimeout:  time.Millisecond,
		RefreshWaitInterval: time.Hour,
		Category:            "AI/科技",
		Accounts:            []config.XAccountConfig{{Handle: "OpenAI"}},
	}

	_, failed, err := fetchXVisibleNDJSON(context.Background(), cfg, []string{"Codex"}, from, to)
	if err != nil {
		t.Fatalf("fetchXVisibleNDJSON() error = %v", err)
	}
	assertFailedSourceContains(t, failed, "X refresh status", "refresh still running after")
}

func TestXVisibleRefreshRunningRequiresExactActiveWindow(t *testing.T) {
	dir := t.TempDir()
	statusPath := filepath.Join(dir, "status.json")
	from := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)
	cfg := config.XAccountsConfig{Enabled: true, RefreshStatusPath: statusPath}

	tests := []struct {
		name   string
		status string
		want   bool
	}{
		{
			name:   "matching running window",
			status: `{"status":"running","window":{"from":"2026-07-27T00:00:00.000Z","to":"2026-07-27T10:00:00.000Z"}}`,
			want:   true,
		},
		{
			name:   "matching succeeded window",
			status: `{"status":"succeeded","window":{"from":"2026-07-27T00:00:00.000Z","to":"2026-07-27T10:00:00.000Z"}}`,
		},
		{
			name:   "matching failed window",
			status: `{"status":"failed","window":{"from":"2026-07-27T00:00:00.000Z","to":"2026-07-27T10:00:00.000Z"}}`,
		},
		{
			name:   "different running window",
			status: `{"status":"running","window":{"from":"2026-07-26T10:00:00.000Z","to":"2026-07-27T00:00:00.000Z"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := os.WriteFile(statusPath, []byte(tt.status), 0o644); err != nil {
				t.Fatalf("WriteFile(status) error = %v", err)
			}
			got, err := XVisibleRefreshRunning(cfg, from, to)
			if err != nil {
				t.Fatalf("XVisibleRefreshRunning() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("XVisibleRefreshRunning() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReadXVisibleRefreshWindowStateUsesHeartbeatLease(t *testing.T) {
	dir := t.TempDir()
	statusPath := filepath.Join(dir, "status.json")
	from := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)
	now := time.Date(2026, 7, 27, 10, 10, 0, 0, time.UTC)
	cfg := config.XAccountsConfig{
		Enabled:             true,
		RefreshStatusPath:   statusPath,
		HeartbeatStaleAfter: 3 * time.Minute,
	}

	tests := []struct {
		name        string
		status      string
		wantMatches bool
		wantRunning bool
		wantStale   bool
	}{
		{
			name:        "fresh heartbeat",
			status:      `{"status":"running","startedAt":"2026-07-27T10:00:00Z","heartbeatAt":"2026-07-27T10:09:00Z","window":{"from":"2026-07-27T00:00:00Z","to":"2026-07-27T10:00:00Z"}}`,
			wantMatches: true,
			wantRunning: true,
		},
		{
			name:        "stale heartbeat",
			status:      `{"status":"running","startedAt":"2026-07-27T10:00:00Z","heartbeatAt":"2026-07-27T10:06:59Z","window":{"from":"2026-07-27T00:00:00Z","to":"2026-07-27T10:00:00Z"}}`,
			wantMatches: true,
			wantRunning: true,
			wantStale:   true,
		},
		{
			name:        "legacy running status falls back to started at",
			status:      `{"status":"running","startedAt":"2026-07-27T10:08:00Z","window":{"from":"2026-07-27T00:00:00Z","to":"2026-07-27T10:00:00Z"}}`,
			wantMatches: true,
			wantRunning: true,
		},
		{
			name:        "terminal status",
			status:      `{"status":"failed","heartbeatAt":"2026-07-27T10:09:00Z","window":{"from":"2026-07-27T00:00:00Z","to":"2026-07-27T10:00:00Z"}}`,
			wantMatches: true,
		},
		{
			name:   "different window",
			status: `{"status":"running","heartbeatAt":"2026-07-27T10:09:00Z","window":{"from":"2026-07-26T10:00:00Z","to":"2026-07-27T00:00:00Z"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := os.WriteFile(statusPath, []byte(tt.status), 0o644); err != nil {
				t.Fatalf("WriteFile(status) error = %v", err)
			}
			got, err := ReadXVisibleRefreshWindowState(cfg, from, to, now)
			if err != nil {
				t.Fatalf("ReadXVisibleRefreshWindowState() error = %v", err)
			}
			if got.Matches != tt.wantMatches || got.Running != tt.wantRunning || got.Stale != tt.wantStale {
				t.Fatalf("state = %#v, want matches=%v running=%v stale=%v", got, tt.wantMatches, tt.wantRunning, tt.wantStale)
			}
		})
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

func TestFetchXVisibleNDJSONOriginalOnlyDropsReposts(t *testing.T) {
	dir := t.TempDir()
	accountsPath := filepath.Join(dir, "accounts.ndjson")
	content := `{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"/twitter/user/merettm","targetType":"account","targetUrl":"https://x.com/merettm","sourceUrl":"https://x.com/merettm","finalUrl":"https://x.com/merettm","text":"Jakub Pachocki reposted OpenAI Codex update","datetime":"2026-05-20T19:06:41.000Z","statusUrl":"https://x.com/OpenAI/status/repost","statusLinks":["https://x.com/OpenAI/status/repost"],"linkCount":1,"imageCount":0,"videoCount":0}
{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"/twitter/user/merettm","targetType":"account","targetUrl":"https://x.com/merettm","sourceUrl":"https://x.com/merettm","finalUrl":"https://x.com/merettm","text":"Jakub Pachocki shares an original OpenAI Codex update","datetime":"2026-05-20T20:06:41.000Z","statusUrl":"https://x.com/merettm/status/original","statusLinks":["https://x.com/merettm/status/original"],"linkCount":1,"imageCount":0,"videoCount":0}
{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"/twitter/user/merettm","targetType":"account","targetUrl":"https://x.com/merettm","sourceUrl":"https://x.com/merettm","finalUrl":"https://x.com/merettm","text":"Jakub Pachocki 已转帖 OpenAI Codex update","datetime":"2026-05-20T21:06:41.000Z","statusUrl":"https://x.com/OpenAI/status/repost-cn","statusLinks":["https://x.com/OpenAI/status/repost-cn"],"linkCount":1,"imageCount":0,"videoCount":0}
`
	if err := os.WriteFile(accountsPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write accounts ndjson: %v", err)
	}
	cfg := config.XAccountsConfig{
		Enabled:      true,
		AccountsPath: accountsPath,
		Category:     "AI/科技",
		OriginalOnly: true,
		Accounts:     []config.XAccountConfig{{Handle: "merettm"}},
	}
	from := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)

	results, failed, err := fetchXVisibleNDJSON(context.Background(), cfg, []string{"OpenAI", "Codex"}, from, to)
	if err != nil {
		t.Fatalf("fetchXVisibleNDJSON() error = %v", err)
	}
	if len(failed) != 0 {
		t.Fatalf("failed = %#v, want none", failed)
	}
	if len(results) != 1 || len(results[0].Candidates) != 1 {
		t.Fatalf("results = %#v, want one original X candidate", results)
	}
	if got := results[0].Candidates[0].Article.Link; got != "https://x.com/merettm/status/original" {
		t.Fatalf("candidate link = %q, want original status", got)
	}
}

func TestFetchXVisibleNDJSONIgnoresWeakLoginSignalAfterArticlesReady(t *testing.T) {
	dir := t.TempDir()
	searchesPath := filepath.Join(dir, "searches.ndjson")
	statusPath := filepath.Join(dir, "status.json")
	content := `{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"search:Codex limits","targetType":"search","targetUrl":"https://x.com/search?q=Codex%20limits&src=typed_query&f=live","sourceUrl":"https://x.com/search?q=Codex%20limits&src=typed_query&f=live","finalUrl":"https://x.com/search?q=Codex%20limits&src=typed_query&f=live","text":"Codex usage limits discussion","datetime":"2026-05-27T09:00:00.000Z","statusUrl":"https://x.com/example/status/2057249933124886592","statusLinks":["https://x.com/example/status/2057249933124886592"],"linkCount":1,"imageCount":0,"videoCount":0}
`
	if err := os.WriteFile(searchesPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write searches ndjson: %v", err)
	}
	status := `{"kind":"x-visible-refresh-status","schemaVersion":1,"job":"x-visible-ai","period":"2026-05-27T10:00:00.000Z","status":"succeeded","startedAt":"2026-05-27T10:00:18.964Z","finishedAt":"2026-05-27T10:02:32.463Z","window":{"from":"2026-05-27T00:00:00.000Z","to":"2026-05-27T10:00:00.000Z"},"targetSummary":{"targetCount":48,"okCount":48,"errorCount":0,"timeoutCount":0,"loginSignalCount":1,"challengeSignalCount":0,"retryCount":0},"targets":[{"source":"searches","targetRaw":"search:Codex limits","targetType":"search","loadStopReason":"article-ready","loginSignals":true,"attempts":1,"articleCount":30}]}
`
	if err := os.WriteFile(statusPath, []byte(status), 0o644); err != nil {
		t.Fatalf("write status: %v", err)
	}
	cfg := config.XAccountsConfig{
		Enabled:           true,
		SearchesPath:      searchesPath,
		RefreshStatusPath: statusPath,
		Category:          "AI/科技",
	}
	from := time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)

	results, failed, err := fetchXVisibleNDJSON(context.Background(), cfg, []string{"Codex"}, from, to)
	if err != nil {
		t.Fatalf("fetchXVisibleNDJSON() error = %v", err)
	}
	if len(results) != 1 || len(results[0].Candidates) != 1 {
		t.Fatalf("results = %#v, want one X candidate", results)
	}
	if len(failed) != 0 {
		t.Fatalf("failed = %#v, want weak login signal ignored", failed)
	}
}

func TestFetchXVisibleNDJSONIgnoresWeakLoginSignalAfterCoveredEmptyWindow(t *testing.T) {
	dir := t.TempDir()
	accountsPath := filepath.Join(dir, "accounts.ndjson")
	statusPath := filepath.Join(dir, "status.json")
	if err := os.WriteFile(accountsPath, nil, 0o644); err != nil {
		t.Fatalf("write accounts ndjson: %v", err)
	}
	status := `{"kind":"x-visible-refresh-status","schemaVersion":1,"job":"x-visible-ai","period":"2026-07-11T10:00:00.000Z","status":"succeeded","startedAt":"2026-07-11T10:00:13.702Z","finishedAt":"2026-07-11T10:03:13.381Z","window":{"from":"2026-07-11T00:00:00.000Z","to":"2026-07-11T10:00:00.000Z"},"targetSummary":{"targetCount":1,"okCount":1,"errorCount":0,"timeoutCount":0,"loginSignalCount":1,"challengeSignalCount":0,"retryCount":0},"targets":[{"source":"accounts","targetRaw":"/twitter/user/GeminiApp","targetType":"account","targetUrl":"https://x.com/GeminiApp","ok":true,"articleCount":0,"attempts":1,"retried":false,"error":"","loadStopReason":"article-ready","scrollStopReason":"covered-window-start","loginSignals":true,"challengeSignals":false}]}
`
	if err := os.WriteFile(statusPath, []byte(status), 0o644); err != nil {
		t.Fatalf("write status: %v", err)
	}
	cfg := config.XAccountsConfig{
		Enabled:           true,
		AccountsPath:      accountsPath,
		RefreshStatusPath: statusPath,
		Category:          "AI/科技",
		Accounts:          []config.XAccountConfig{{Handle: "GeminiApp"}},
	}
	from := time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 11, 10, 0, 0, 0, time.UTC)

	results, failed, err := fetchXVisibleNDJSON(context.Background(), cfg, []string{"Gemini"}, from, to)
	if err != nil {
		t.Fatalf("fetchXVisibleNDJSON() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %#v, want no current-window X candidate", results)
	}
	if len(failed) != 0 {
		t.Fatalf("failed = %#v, want covered empty-window login signal ignored", failed)
	}
}

func TestXVisibleTargetLoginProblemKeepsActionableSignals(t *testing.T) {
	weakCoveredTarget := xVisibleRefreshTargetDetails{
		OK:               true,
		LoadStopReason:   "article-ready",
		ScrollStopReason: "covered-window-start",
		LoginSignals:     json.RawMessage("true"),
		ArticleCount:     0,
	}
	tests := []struct {
		name   string
		target xVisibleRefreshTargetDetails
		want   bool
	}{
		{name: "covered empty window", target: weakCoveredTarget, want: false},
		{name: "explicit login stop", target: func() xVisibleRefreshTargetDetails {
			target := weakCoveredTarget
			target.LoadStopReason = "login-signal"
			target.ArticleCount = 2
			return target
		}(), want: true},
		{name: "target not ok", target: func() xVisibleRefreshTargetDetails {
			target := weakCoveredTarget
			target.OK = false
			return target
		}(), want: true},
		{name: "target error", target: func() xVisibleRefreshTargetDetails {
			target := weakCoveredTarget
			target.Error = "target failed"
			return target
		}(), want: true},
		{name: "window not covered", target: func() xVisibleRefreshTargetDetails {
			target := weakCoveredTarget
			target.ScrollStopReason = "stable"
			return target
		}(), want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := xVisibleTargetLoginProblem(tt.target); got != tt.want {
				t.Fatalf("xVisibleTargetLoginProblem() = %v, want %v", got, tt.want)
			}
		})
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

func TestFetchXVisibleNDJSONReportsSearchLimitReachedCoverageWarning(t *testing.T) {
	dir := t.TempDir()
	searchesPath := filepath.Join(dir, "searches.ndjson")
	content := `{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"search:Codex","targetType":"search","targetUrl":"https://x.com/search?q=Codex&src=typed_query&f=live","sourceUrl":"https://x.com/search?q=Codex&src=typed_query&f=live","finalUrl":"https://x.com/search?q=Codex&src=typed_query&f=live","windowFrom":"2026-05-20T10:00:00.000Z","windowTo":"2026-05-21T00:00:00.000Z","scrollStopReason":"limit-reached","text":"Codex launched and released a major update","datetime":"2026-05-20T23:59:40.000Z","statusUrl":"https://x.com/example/status/limit-reached","statusLinks":["https://x.com/example/status/limit-reached"],"linkCount":1,"imageCount":0,"videoCount":0}
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
	assertFailedSourceContains(t, failed, "X coverage", "search:Codex=limit-reached")
}

func TestFetchXVisibleNDJSONMergesCoverageWarnings(t *testing.T) {
	dir := t.TempDir()
	accountsPath := filepath.Join(dir, "accounts.ndjson")
	content := `{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"/twitter/user/OpenAI","targetType":"account","targetUrl":"https://x.com/OpenAI","sourceUrl":"https://x.com/OpenAI","finalUrl":"https://x.com/OpenAI","windowFrom":"2026-05-20T10:00:00.000Z","windowTo":"2026-05-21T00:00:00.000Z","scrollStopReason":"limit-reached","text":"OpenAI Codex update","datetime":"2026-05-20T23:59:40.000Z","statusUrl":"https://x.com/OpenAI/status/limit-reached"}
{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"/twitter/user/AnthropicAI","targetType":"account","targetUrl":"https://x.com/AnthropicAI","sourceUrl":"https://x.com/AnthropicAI","finalUrl":"https://x.com/AnthropicAI","windowFrom":"2026-05-20T10:00:00.000Z","windowTo":"2026-05-21T00:00:00.000Z","scrollStopReason":"limit-reached","text":"Anthropic Claude update","datetime":"2026-05-20T23:58:40.000Z","statusUrl":"https://x.com/AnthropicAI/status/limit-reached"}
`
	if err := os.WriteFile(accountsPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write accounts ndjson: %v", err)
	}
	cfg := config.XAccountsConfig{
		Enabled:      true,
		AccountsPath: accountsPath,
		Category:     "AI/科技",
		Accounts: []config.XAccountConfig{
			{Handle: "OpenAI"},
			{Handle: "AnthropicAI"},
		},
	}
	from := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)

	results, failed, err := fetchXVisibleNDJSON(context.Background(), cfg, []string{"Codex", "Claude"}, from, to)
	if err != nil {
		t.Fatalf("fetchXVisibleNDJSON() error = %v", err)
	}
	if len(results) != 1 || len(results[0].Candidates) != 2 {
		t.Fatalf("results = %#v, want two X candidates", results)
	}
	if len(failed) != 1 {
		t.Fatalf("failed = %#v, want one merged coverage warning", failed)
	}
	if failed[0].Name != "X coverage" {
		t.Fatalf("FailedSource.Name = %q, want X coverage", failed[0].Name)
	}
	for _, want := range []string{"2 targets", "AnthropicAI=limit-reached", "OpenAI=limit-reached"} {
		if !strings.Contains(failed[0].Err.Error(), want) {
			t.Fatalf("FailedSource.Err = %q, want %q", failed[0].Err, want)
		}
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

func TestFetchXVisibleNDJSONReportsMissingExpectedHistoryArchive(t *testing.T) {
	dir := t.TempDir()
	accountsPath := filepath.Join(dir, "accounts.ndjson")
	searchesPath := filepath.Join(dir, "searches.ndjson")
	statusPath := filepath.Join(dir, "status.json")
	historyDir := filepath.Join(dir, "history")
	if err := os.MkdirAll(historyDir, 0o755); err != nil {
		t.Fatalf("mkdir history: %v", err)
	}
	if err := os.WriteFile(accountsPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write accounts: %v", err)
	}
	if err := os.WriteFile(searchesPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write searches: %v", err)
	}
	status := `{"kind":"x-visible-refresh-status","schemaVersion":1,"job":"x-visible-ai","period":"2026-05-21T00:00:00.000Z","status":"succeeded","finishedAt":"2026-05-21T00:02:00.000Z","window":{"from":"2026-05-20T00:00:00.000Z","to":"2026-05-21T00:00:00.000Z"}}`
	if err := os.WriteFile(statusPath, []byte(status), 0o644); err != nil {
		t.Fatalf("write status: %v", err)
	}
	cfg := config.XAccountsConfig{
		Enabled:           true,
		AccountsPath:      accountsPath,
		SearchesPath:      searchesPath,
		HistoryDir:        historyDir,
		RefreshStatusPath: statusPath,
		Category:          "AI/科技",
		Accounts:          []config.XAccountConfig{{Handle: "OpenAI"}},
	}
	from := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)

	_, failed, err := fetchXVisibleNDJSON(context.Background(), cfg, []string{"Codex"}, from, to)
	if err != nil {
		t.Fatalf("fetchXVisibleNDJSON() error = %v", err)
	}
	assertFailedSourceContains(t, failed, "X history", "no succeeded X history archive overlaps requested window")
}

func TestFetchXVisibleNDJSONReportsMissingExpectedHistoryArchiveWhenOlderArchiveOverlaps(t *testing.T) {
	dir := t.TempDir()
	accountsPath := filepath.Join(dir, "accounts.ndjson")
	searchesPath := filepath.Join(dir, "searches.ndjson")
	statusPath := filepath.Join(dir, "status.json")
	historyDir := filepath.Join(dir, "history")
	if err := os.WriteFile(accountsPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write accounts: %v", err)
	}
	if err := os.WriteFile(searchesPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write searches: %v", err)
	}
	writeXVisibleHistoryArchive(t, historyDir, "20260520T180000Z", "2026-05-20T12:00:00.000Z", "2026-05-20T18:00:00.000Z", "", "")
	status := `{"kind":"x-visible-refresh-status","schemaVersion":1,"job":"x-visible-ai","period":"2026-05-21T00:00:00.000Z","status":"succeeded","finishedAt":"2026-05-21T00:02:00.000Z","window":{"from":"2026-05-20T00:00:00.000Z","to":"2026-05-21T00:00:00.000Z"}}`
	if err := os.WriteFile(statusPath, []byte(status), 0o644); err != nil {
		t.Fatalf("write status: %v", err)
	}
	cfg := config.XAccountsConfig{
		Enabled:           true,
		AccountsPath:      accountsPath,
		SearchesPath:      searchesPath,
		HistoryDir:        historyDir,
		RefreshStatusPath: statusPath,
		Category:          "AI/科技",
		Accounts:          []config.XAccountConfig{{Handle: "OpenAI"}},
	}
	from := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)

	_, failed, err := fetchXVisibleNDJSON(context.Background(), cfg, []string{"Codex"}, from, to)
	if err != nil {
		t.Fatalf("fetchXVisibleNDJSON() error = %v", err)
	}
	assertFailedSourceContains(t, failed, "X history", "no succeeded X history archive overlaps requested window")
}

func TestFetchXVisibleNDJSONDoesNotRequireHistoryArchiveWhenHistoryDirMissing(t *testing.T) {
	dir := t.TempDir()
	accountsPath := filepath.Join(dir, "accounts.ndjson")
	searchesPath := filepath.Join(dir, "searches.ndjson")
	statusPath := filepath.Join(dir, "status.json")
	if err := os.WriteFile(accountsPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write accounts: %v", err)
	}
	if err := os.WriteFile(searchesPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write searches: %v", err)
	}
	status := `{"kind":"x-visible-refresh-status","schemaVersion":1,"job":"x-visible-ai","period":"2026-05-21T00:00:00.000Z","status":"succeeded","finishedAt":"2026-05-21T00:02:00.000Z","window":{"from":"2026-05-20T00:00:00.000Z","to":"2026-05-21T00:00:00.000Z"}}`
	if err := os.WriteFile(statusPath, []byte(status), 0o644); err != nil {
		t.Fatalf("write status: %v", err)
	}
	cfg := config.XAccountsConfig{
		Enabled:           true,
		AccountsPath:      accountsPath,
		SearchesPath:      searchesPath,
		RefreshStatusPath: statusPath,
		Category:          "AI/科技",
		Accounts:          []config.XAccountConfig{{Handle: "OpenAI"}},
	}
	from := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)

	_, failed, err := fetchXVisibleNDJSON(context.Background(), cfg, []string{"Codex"}, from, to)
	if err != nil {
		t.Fatalf("fetchXVisibleNDJSON() error = %v", err)
	}
	if len(failed) != 0 {
		t.Fatalf("failed = %#v, want none without history_dir", failed)
	}
}

func TestFetchXVisibleHistoryNDJSONReportsNoMatchingArchive(t *testing.T) {
	dir := t.TempDir()
	historyDir := filepath.Join(dir, "history")
	if err := os.MkdirAll(historyDir, 0o755); err != nil {
		t.Fatalf("mkdir history: %v", err)
	}
	cfg := config.XAccountsConfig{
		Enabled:  true,
		Category: "AI/科技",
		Accounts: []config.XAccountConfig{{Handle: "OpenAI"}},
	}
	from := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)

	results, failed, err := fetchXVisibleNDJSONWithOptions(context.Background(), cfg, []string{"Codex"}, from, to, xVisibleReadOptions{useHistory: true, historyDir: historyDir})
	if err != nil {
		t.Fatalf("fetchXVisibleNDJSONWithOptions() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %#v, want none", results)
	}
	assertFailedSourceContains(t, failed, "X history", "no succeeded X history archive overlaps requested window")
}

func TestFetchXVisibleHistoryNDJSONReportsFailedMatchingArchiveStatus(t *testing.T) {
	dir := t.TempDir()
	historyDir := filepath.Join(dir, "history")
	archiveDir := filepath.Join(historyDir, "20260521T000000Z")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		t.Fatalf("mkdir archive: %v", err)
	}
	status := `{"kind":"x-visible-refresh-status","schemaVersion":1,"job":"x-visible-ai","period":"2026-05-21T00:00:00.000Z","status":"failed","error":"browser crashed","window":{"from":"2026-05-20T00:00:00.000Z","to":"2026-05-21T00:00:00.000Z"}}`
	if err := os.WriteFile(filepath.Join(archiveDir, "status.json"), []byte(status), 0o644); err != nil {
		t.Fatalf("write status: %v", err)
	}
	cfg := config.XAccountsConfig{
		Enabled:  true,
		Category: "AI/科技",
		Accounts: []config.XAccountConfig{{Handle: "OpenAI"}},
	}
	from := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)

	_, failed, err := fetchXVisibleNDJSONWithOptions(context.Background(), cfg, []string{"Codex"}, from, to, xVisibleReadOptions{useHistory: true, historyDir: historyDir})
	if err != nil {
		t.Fatalf("fetchXVisibleNDJSONWithOptions() error = %v", err)
	}
	assertFailedSourceContains(t, failed, "X history/20260521T000000Z", "refresh failed: browser crashed")
}

func TestFetchXVisibleNDJSONReportsIncompleteHistoryArchive(t *testing.T) {
	dir := t.TempDir()
	accountsPath := filepath.Join(dir, "accounts.ndjson")
	searchesPath := filepath.Join(dir, "searches.ndjson")
	historyDir := filepath.Join(dir, "history")
	archiveDir := filepath.Join(historyDir, "20260521T000000Z")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		t.Fatalf("mkdir archive: %v", err)
	}
	if err := os.WriteFile(accountsPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write accounts: %v", err)
	}
	if err := os.WriteFile(searchesPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write searches: %v", err)
	}
	status := `{"kind":"x-visible-refresh-status","schemaVersion":1,"job":"x-visible-ai","period":"2026-05-21T00:00:00.000Z","status":"succeeded","finishedAt":"2026-05-21T00:02:00.000Z","window":{"from":"2026-05-20T00:00:00.000Z","to":"2026-05-21T00:00:00.000Z"}}`
	if err := os.WriteFile(filepath.Join(archiveDir, "status.json"), []byte(status), 0o644); err != nil {
		t.Fatalf("write archive status: %v", err)
	}
	cfg := config.XAccountsConfig{
		Enabled:      true,
		AccountsPath: accountsPath,
		SearchesPath: searchesPath,
		HistoryDir:   historyDir,
		Category:     "AI/科技",
		Accounts:     []config.XAccountConfig{{Handle: "OpenAI"}},
	}
	from := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)

	_, failed, err := fetchXVisibleNDJSON(context.Background(), cfg, []string{"Codex"}, from, to)
	if err != nil {
		t.Fatalf("fetchXVisibleNDJSON() error = %v", err)
	}
	assertFailedSourceContains(t, failed, "X history/20260521T000000Z", "missing manifest.json")
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
	if failed[0].Name != "X coverage" {
		t.Fatalf("FailedSource.Name = %q", failed[0].Name)
	}
	if !strings.Contains(failed[0].Err.Error(), "OpenAI=max-scrolls") {
		t.Fatalf("FailedSource.Err = %v, want OpenAI=max-scrolls", failed[0].Err)
	}
}

func TestFetchXVisibleNDJSONIgnoresAccountStableCoverageWarning(t *testing.T) {
	dir := t.TempDir()
	accountsPath := filepath.Join(dir, "accounts.ndjson")
	content := `{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"/twitter/user/OpenAI","targetType":"account","targetUrl":"https://x.com/OpenAI","sourceUrl":"https://x.com/OpenAI","finalUrl":"https://x.com/OpenAI","windowFrom":"2026-05-19T00:00:00.000Z","windowTo":"2026-05-20T00:00:00.000Z","scrollStopReason":"stable","text":"OpenAI Codex update","datetime":"2026-05-19T07:00:00.000Z","statusUrl":"https://x.com/OpenAI/status/stable","statusLinks":["https://x.com/OpenAI/status/stable"],"linkCount":1,"imageCount":0,"videoCount":0}
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
		t.Fatalf("failed = %#v, want no account stable coverage warning", failed)
	}
	if len(results) != 1 || len(results[0].Candidates) != 1 {
		t.Fatalf("results = %#v, want one X candidate", results)
	}
}

func TestFetchXVisibleNDJSONIgnoresCoverageWarningForUntrackedAccount(t *testing.T) {
	dir := t.TempDir()
	accountsPath := filepath.Join(dir, "accounts.ndjson")
	content := `{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"/twitter/user/NASASpaceflight","targetType":"account","targetUrl":"https://x.com/NASASpaceflight","sourceUrl":"https://x.com/NASASpaceflight","finalUrl":"https://x.com/NASASpaceflight","windowFrom":"2026-05-19T00:00:00.000Z","windowTo":"2026-05-20T00:00:00.000Z","scrollStopReason":"limit-reached","text":"NASA rocket update","datetime":"2026-05-19T07:00:00.000Z","statusUrl":"https://x.com/NASASpaceflight/status/limit","statusLinks":["https://x.com/NASASpaceflight/status/limit"],"linkCount":1,"imageCount":0,"videoCount":0}
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

	results, failed, err := fetchXVisibleNDJSON(context.Background(), cfg, []string{"NASA"}, from, to)
	if err != nil {
		t.Fatalf("fetchXVisibleNDJSON() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %#v, want no candidates for untracked account", results)
	}
	if len(failed) != 0 {
		t.Fatalf("failed = %#v, want no coverage warning for untracked account", failed)
	}
}

func TestFetchXVisibleNDJSONIgnoresSearchStableCoverageWarning(t *testing.T) {
	dir := t.TempDir()
	searchesPath := filepath.Join(dir, "searches.ndjson")
	content := `{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"search:Claude Code outage","targetType":"search","targetUrl":"https://x.com/search?q=Claude%20Code%20outage&src=typed_query&f=live","sourceUrl":"https://x.com/search?q=Claude%20Code%20outage&src=typed_query&f=live","finalUrl":"https://x.com/search?q=Claude%20Code%20outage&src=typed_query&f=live","windowFrom":"2026-05-19T00:00:00.000Z","windowTo":"2026-05-20T00:00:00.000Z","scrollStopReason":"stable","text":"Claude Code outage update","datetime":"2026-05-19T07:00:00.000Z","statusUrl":"https://x.com/example/status/stable-search","statusLinks":["https://x.com/example/status/stable-search"],"linkCount":1,"imageCount":0,"videoCount":0}
`
	if err := os.WriteFile(searchesPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write searches ndjson: %v", err)
	}
	cfg := config.XAccountsConfig{
		Enabled:      true,
		SearchesPath: searchesPath,
		Category:     "AI/科技",
		Accounts:     []config.XAccountConfig{{Handle: "OpenAI"}},
	}
	from := time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)

	results, failed, err := fetchXVisibleNDJSON(context.Background(), cfg, []string{"outage"}, from, to)
	if err != nil {
		t.Fatalf("fetchXVisibleNDJSON() error = %v", err)
	}
	if len(results) != 1 || len(results[0].Candidates) != 1 {
		t.Fatalf("results = %#v, want one X candidate", results)
	}
	if len(failed) != 0 {
		t.Fatalf("failed = %#v, want search stable coverage ignored", failed)
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

func TestFindXVisibleArticleByURLReadsAllowedSnapshot(t *testing.T) {
	dir := t.TempDir()
	accountsPath := filepath.Join(dir, "accounts.ndjson")
	content := `{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"/twitter/user/team","targetType":"account","targetUrl":"https://x.com/team","sourceUrl":"https://x.com/team","finalUrl":"https://x.com/team","text":"Codex supports a 1M-token context window","datetime":"2026-08-17T04:12:00Z","statusUrl":"https://x.com/team/status/1","statusLinks":["https://x.com/team/status/1"],"linkCount":1}`
	if err := os.WriteFile(accountsPath, []byte(content+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.XAccountsConfig{
		AccountsPath: accountsPath,
		Category:     "AI/科技",
		OriginalOnly: true,
		Accounts:     []config.XAccountConfig{{Handle: "team"}},
	}
	article, err := FindXVisibleArticleByURL(cfg, "https://x.com/team/status/1?ref=test")
	if err != nil {
		t.Fatalf("FindXVisibleArticleByURL() error = %v", err)
	}
	if article.Link != "https://x.com/team/status/1" || article.Source != "X/@team" || article.SourceRole != model.SourceRoleRadar {
		t.Fatalf("article = %#v", article)
	}
}

func TestFindXVisibleArticleByURLReadsHistoryArchive(t *testing.T) {
	dir := t.TempDir()
	historyDir := filepath.Join(dir, "history")
	writeXVisibleHistoryArchive(t, historyDir, "20260817T000000Z", "2026-08-16T10:00:00Z", "2026-08-17T00:00:00Z",
		`{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"/twitter/user/team","targetType":"account","targetUrl":"https://x.com/team","sourceUrl":"https://x.com/team","finalUrl":"https://x.com/team","text":"Codex supports a 1M-token context window","datetime":"2026-08-16T20:12:00Z","statusUrl":"https://x.com/team/status/history"}
`, "")
	cfg := config.XAccountsConfig{
		AccountsPath: filepath.Join(dir, "missing.ndjson"),
		HistoryDir:   historyDir,
		Category:     "AI/科技",
		OriginalOnly: true,
		Accounts:     []config.XAccountConfig{{Handle: "team"}},
	}
	article, err := FindXVisibleArticleByURL(cfg, "https://x.com/team/status/history")
	if err != nil || article.Link != "https://x.com/team/status/history" {
		t.Fatalf("FindXVisibleArticleByURL() = %#v, %v", article, err)
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
	manifest := `{"kind":"x-visible-history-archive","schemaVersion":1,"period":"` + windowTo + `","periodKey":"` + periodKey + `","window":{"from":"` + windowFrom + `","to":"` + windowTo + `"}}
	`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write archive manifest: %v", err)
	}
	writeGzipFile(t, filepath.Join(dir, "accounts.ndjson.gz"), accountsContent)
	writeGzipFile(t, filepath.Join(dir, "searches.ndjson.gz"), searchesContent)
}

package fetcher

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/walker1211/news-briefing/internal/config"
)

func TestFetchXAlertsContextFiltersKeywordAndStrongSignal(t *testing.T) {
	dir := t.TempDir()
	accountsPath := filepath.Join(dir, "accounts.ndjson")
	searchesPath := filepath.Join(dir, "searches.ndjson")
	accounts := `{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"/twitter/user/OpenAI","targetType":"account","targetUrl":"https://x.com/OpenAI","sourceUrl":"https://x.com/OpenAI","finalUrl":"https://x.com/OpenAI","text":"OpenAI launched Codex mobile today","datetime":"2026-05-19T11:00:00.000Z","statusUrl":"https://x.com/OpenAI/status/accepted-account","statusLinks":["https://x.com/OpenAI/status/accepted-account"],"linkCount":1,"imageCount":0,"videoCount":0}
{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"/twitter/user/OpenAI","targetType":"account","targetUrl":"https://x.com/OpenAI","sourceUrl":"https://x.com/OpenAI","finalUrl":"https://x.com/OpenAI","text":"OpenAI Codex discussion thread for developers","datetime":"2026-05-19T10:00:00.000Z","statusUrl":"https://x.com/OpenAI/status/no-signal","statusLinks":["https://x.com/OpenAI/status/no-signal"],"linkCount":1,"imageCount":0,"videoCount":0}
{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"/twitter/user/OpenAI","targetType":"account","targetUrl":"https://x.com/OpenAI","sourceUrl":"https://x.com/OpenAI","finalUrl":"https://x.com/OpenAI","text":"OpenAI launched a community event","datetime":"2026-05-19T09:00:00.000Z","statusUrl":"https://x.com/OpenAI/status/no-keyword","statusLinks":["https://x.com/OpenAI/status/no-keyword"],"linkCount":1,"imageCount":0,"videoCount":0}
{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"/twitter/user/OpenAI","targetType":"account","targetUrl":"https://x.com/OpenAI","sourceUrl":"https://x.com/OpenAI","finalUrl":"https://x.com/OpenAI","text":"OpenAI released Codex old update","datetime":"2026-05-18T11:59:59.000Z","statusUrl":"https://x.com/OpenAI/status/old","statusLinks":["https://x.com/OpenAI/status/old"],"linkCount":1,"imageCount":0,"videoCount":0}
`
	searches := `{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"search:Codex","targetType":"search","targetUrl":"https://x.com/search?q=Codex","sourceUrl":"https://x.com/search?q=Codex","finalUrl":"https://x.com/search?q=Codex","text":"Someone says Codex launched today","datetime":"2026-05-19T08:00:00.000Z","statusUrl":"https://x.com/user/status/search-one-signal","statusLinks":["https://x.com/user/status/search-one-signal"],"linkCount":1,"imageCount":0,"videoCount":0}
{"kind":"x-visible-article","schemaVersion":1,"targetRaw":"search:Codex","targetType":"search","targetUrl":"https://x.com/search?q=Codex","sourceUrl":"https://x.com/search?q=Codex","finalUrl":"https://x.com/search?q=Codex","text":"Codex launched and released a major update","datetime":"2026-05-19T07:00:00.000Z","statusUrl":"https://x.com/user/status/accepted-search","statusLinks":["https://x.com/user/status/accepted-search"],"linkCount":1,"imageCount":0,"videoCount":0}
`
	if err := os.WriteFile(accountsPath, []byte(accounts), 0o644); err != nil {
		t.Fatalf("write accounts: %v", err)
	}
	if err := os.WriteFile(searchesPath, []byte(searches), 0o644); err != nil {
		t.Fatalf("write searches: %v", err)
	}
	cfg := &config.Config{
		Keywords: []string{"Codex"},
		XAccounts: config.XAccountsConfig{
			Enabled:      true,
			AccountsPath: accountsPath,
			SearchesPath: searchesPath,
			Category:     "AI/科技",
			Accounts:     []config.XAccountConfig{{Handle: "OpenAI"}},
		},
	}
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)

	result, err := FetchXAlertsContext(context.Background(), cfg, now)
	if err != nil {
		t.Fatalf("FetchXAlertsContext() error = %v", err)
	}
	if len(result.Failed) != 0 {
		t.Fatalf("Failed = %#v, want none", result.Failed)
	}
	if len(result.Articles) != 2 {
		t.Fatalf("len(Articles) = %d, want 2: %#v", len(result.Articles), result.Articles)
	}
	if result.Articles[0].Link != "https://x.com/OpenAI/status/accepted-account" {
		t.Fatalf("first Article.Link = %q", result.Articles[0].Link)
	}
	if result.Articles[1].Link != "https://x.com/user/status/accepted-search" {
		t.Fatalf("second Article.Link = %q", result.Articles[1].Link)
	}
}

func TestFetchXAlertsContextPreservesFailedSources(t *testing.T) {
	cfg := &config.Config{
		Keywords: []string{"Codex"},
		XAccounts: config.XAccountsConfig{
			Enabled:      true,
			AccountsPath: filepath.Join(t.TempDir(), "missing.ndjson"),
			Category:     "AI/科技",
			Accounts:     []config.XAccountConfig{{Handle: "OpenAI"}},
		},
	}

	result, err := FetchXAlertsContext(context.Background(), cfg, time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("FetchXAlertsContext() error = %v", err)
	}
	if len(result.Failed) != 1 || result.Failed[0].Name != "X accounts" {
		t.Fatalf("Failed = %#v, want X accounts failure", result.Failed)
	}
}

func TestXAlertSignalScoreCountsEnglishAndChineseSignals(t *testing.T) {
	if xAlertSignalScore("Codex launched and 开源 today") < 2 {
		t.Fatal("xAlertSignalScore() should count English and Chinese signals")
	}
}

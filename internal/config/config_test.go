package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/walker1211/news-briefing/internal/model"
)

func TestLoadAppliesDefaultAIConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
output: {}
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.AI.Command != "codex" {
		t.Fatalf("AI.Command = %q, want %q", cfg.AI.Command, "codex")
	}
	wantArgs := []string{
		"exec",
		"--ignore-user-config",
		"--ephemeral",
		"--skip-git-repo-check",
		"--sandbox", "read-only",
		"--color", "never",
		"--disable", "apps",
		"--disable", "plugins",
		"--disable", "remote_plugin",
	}
	if !reflect.DeepEqual(cfg.AI.Args, wantArgs) {
		t.Fatalf("AI.Args = %v, want %v", cfg.AI.Args, wantArgs)
	}
	if cfg.AI.Models.Default != DefaultAIModel {
		t.Fatalf("AI.Models.Default = %q, want %q", cfg.AI.Models.Default, DefaultAIModel)
	}
	if cfg.AI.Models.Translation != DefaultAITranslationModel {
		t.Fatalf("AI.Models.Translation = %q, want %q", cfg.AI.Models.Translation, DefaultAITranslationModel)
	}
	if cfg.AI.Models.DefaultEffort != DefaultAIEffort {
		t.Fatalf("AI.Models.DefaultEffort = %q, want %q", cfg.AI.Models.DefaultEffort, DefaultAIEffort)
	}
	if cfg.AI.Models.TranslationEffort != DefaultAITranslationEffort {
		t.Fatalf("AI.Models.TranslationEffort = %q, want %q", cfg.AI.Models.TranslationEffort, DefaultAITranslationEffort)
	}
	if cfg.AI.Summary.MaxConcurrency != DefaultAISummaryMaxConcurrency {
		t.Fatalf("AI.Summary.MaxConcurrency = %d, want %d", cfg.AI.Summary.MaxConcurrency, DefaultAISummaryMaxConcurrency)
	}
	if !cfg.AI.ShouldAppendSystemPrompt() {
		t.Fatalf("AI.ShouldAppendSystemPrompt() = false, want true")
	}
	wantRetryDelays := []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 9 * time.Second, 17 * time.Second}
	if !reflect.DeepEqual(cfg.AI.Retry.Delays, wantRetryDelays) {
		t.Fatalf("AI.Retry.Delays = %v, want %v", cfg.AI.Retry.Delays, wantRetryDelays)
	}
}

func TestLoadParsesOutputMaxArticles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
output:
  max_articles:
    "AI/科技": 70
    "国际政治": 30
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := map[string]int{"AI/科技": 70, "国际政治": 30}
	if !reflect.DeepEqual(cfg.Output.MaxArticlesByCategory, want) {
		t.Fatalf("Output.MaxArticlesByCategory = %v, want %v", cfg.Output.MaxArticlesByCategory, want)
	}
}

func TestLoadParsesExactImageFilterRules(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
image_filter:
  blocked_urls:
    - host: media.example.com
      path: /promo/download.jpg
email: {}
schedule: []
output: {}
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := []ImageURLRule{{Host: "media.example.com", Path: "/promo/download.jpg"}}
	if !reflect.DeepEqual(cfg.ImageFilter.BlockedURLs, want) {
		t.Fatalf("ImageFilter.BlockedURLs = %#v, want %#v", cfg.ImageFilter.BlockedURLs, want)
	}
}

func TestLoadRejectsInvalidExactImageFilterRules(t *testing.T) {
	tests := []struct {
		name string
		rule string
	}{
		{name: "scheme", rule: "    - host: https://media.example.com\n      path: /promo.jpg\n"},
		{name: "wildcard", rule: "    - host: '*.example.com'\n      path: /promo.jpg\n"},
		{name: "query", rule: "    - host: media.example.com\n      path: /promo.jpg?campaign=app\n"},
		{name: "duplicate", rule: "    - host: media.example.com\n      path: /promo.jpg\n    - host: MEDIA.EXAMPLE.COM\n      path: /promo.jpg/\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			content := "sources: []\nkeywords: []\nimage_filter:\n  blocked_urls:\n" + tc.rule + "email: {}\nschedule: []\noutput: {}\nproxy: {}\n"
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}
			if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "image_filter.blocked_urls") {
				t.Fatalf("Load() error = %v, want image_filter.blocked_urls validation error", err)
			}
		})
	}
}

func TestLoadParsesAIDurationsAndFallbackLevels(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
output:
  max_articles:
    "AI/科技": 70
    "国际政治": 30
  fallback:
    enabled: true
    levels:
      - name: reduced
        max_articles:
          "AI/科技": 50
          "国际政治": 20
      - name: minimal
        max_articles:
          "AI/科技": 30
          "国际政治": 10
proxy: {}
ai:
  timeout:
    warning_after: 20m
    attempt_timeout: 30m
    total_budget: 75m
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AI.Timeout.WarningAfter != 20*time.Minute {
		t.Fatalf("AI.Timeout.WarningAfter = %v, want 20m", cfg.AI.Timeout.WarningAfter)
	}
	if cfg.AI.Timeout.AttemptTimeout != 30*time.Minute {
		t.Fatalf("AI.Timeout.AttemptTimeout = %v, want 30m", cfg.AI.Timeout.AttemptTimeout)
	}
	if cfg.AI.Timeout.TotalBudget != 75*time.Minute {
		t.Fatalf("AI.Timeout.TotalBudget = %v, want 75m", cfg.AI.Timeout.TotalBudget)
	}
	if !cfg.Output.Fallback.Enabled {
		t.Fatalf("Output.Fallback.Enabled = false, want true")
	}
	if len(cfg.Output.Fallback.Levels) != 2 {
		t.Fatalf("Output.Fallback.Levels length = %d, want 2", len(cfg.Output.Fallback.Levels))
	}
	if cfg.Output.Fallback.Levels[0].Name != "reduced" {
		t.Fatalf("first fallback level name = %q, want reduced", cfg.Output.Fallback.Levels[0].Name)
	}
	wantReduced := map[string]int{"AI/科技": 50, "国际政治": 20}
	if !reflect.DeepEqual(cfg.Output.Fallback.Levels[0].MaxArticlesByCategory, wantReduced) {
		t.Fatalf("first fallback max articles = %v, want %v", cfg.Output.Fallback.Levels[0].MaxArticlesByCategory, wantReduced)
	}
}

func TestLoadParsesFetchRedditSourceDelayRange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
output: {}
proxy: {}
fetch:
  reddit_source_delay_min: 60s
  reddit_source_delay_max: 70s
  reddit_rate_limit_wait_max: 120s
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Fetch.RedditSourceDelayMin != time.Minute {
		t.Fatalf("Fetch.RedditSourceDelayMin = %v, want 1m", cfg.Fetch.RedditSourceDelayMin)
	}
	if cfg.Fetch.RedditSourceDelayMax != 70*time.Second {
		t.Fatalf("Fetch.RedditSourceDelayMax = %v, want 1m10s", cfg.Fetch.RedditSourceDelayMax)
	}
	if cfg.Fetch.RedditRateLimitWaitMax != 2*time.Minute {
		t.Fatalf("Fetch.RedditRateLimitWaitMax = %v, want 2m", cfg.Fetch.RedditRateLimitWaitMax)
	}
}

func TestLoadParsesAIRetryDelays(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
output: {}
proxy: {}
ai:
  retry:
    delays:
      - 2s
      - 11s
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := []time.Duration{2 * time.Second, 11 * time.Second}
	if !reflect.DeepEqual(cfg.AI.Retry.Delays, want) {
		t.Fatalf("AI.Retry.Delays = %v, want %v", cfg.AI.Retry.Delays, want)
	}
}

func TestLoadRejectsInvalidAIRetryDelay(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
output: {}
proxy: {}
ai:
  retry:
    delays:
      - 0s
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "ai.retry.delays[0]") {
		t.Fatalf("Load() error = %v, want ai.retry.delays[0] validation error", err)
	}
}

func TestLoadPreservesConfiguredAIFlags(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
output: {}
proxy: {}
ai:
  command: claude
  args:
    - --model
    - claude-opus-4-6
    - --bare
    - --disable-slash-commands
  append_system_prompt: false
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.AI.Command != "claude" {
		t.Fatalf("AI.Command = %q, want %q", cfg.AI.Command, "claude")
	}
	if !reflect.DeepEqual(cfg.AI.Args, []string{"--model", "claude-opus-4-6", "--bare", "--disable-slash-commands"}) {
		t.Fatalf("AI.Args = %v", cfg.AI.Args)
	}
	if cfg.AI.Models.Default != "claude-opus-4-6" {
		t.Fatalf("AI.Models.Default = %q, want claude-opus-4-6", cfg.AI.Models.Default)
	}
	if cfg.AI.Models.Translation != DefaultAITranslationModel {
		t.Fatalf("AI.Models.Translation = %q, want %q", cfg.AI.Models.Translation, DefaultAITranslationModel)
	}
	if cfg.AI.ShouldAppendSystemPrompt() {
		t.Fatalf("AI.ShouldAppendSystemPrompt() = true, want false")
	}
}

func TestLoadAppliesCommandSpecificDefaultAIArgs(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    []string
	}{
		{name: "Claude", command: "claude", want: []string{"--bare", "--disable-slash-commands"}},
		{name: "Claude Windows path", command: `C:\\tools\\CLAUDE.EXE`, want: []string{"--bare", "--disable-slash-commands"}},
		{name: "custom", command: "my-ai", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			content := fmt.Sprintf(`sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
output: {}
proxy: {}
ai:
  command: %q
`, tt.command)
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}

			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if !reflect.DeepEqual(cfg.AI.Args, tt.want) {
				t.Fatalf("AI.Args = %v, want %v", cfg.AI.Args, tt.want)
			}
		})
	}
}

func TestLoadParsesConfiguredAIModels(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
output: {}
proxy: {}
ai:
  models:
    default: default-model
    translation: translation-model
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AI.Models.Default != "default-model" {
		t.Fatalf("AI.Models.Default = %q, want default-model", cfg.AI.Models.Default)
	}
	if cfg.AI.Models.Translation != "translation-model" {
		t.Fatalf("AI.Models.Translation = %q, want translation-model", cfg.AI.Models.Translation)
	}
}

func TestLoadParsesXAccountsConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
output: {}
proxy: {}
x_accounts:
  enabled: true
  accounts_path: /tmp/rsshub-stack/accounts.ndjson
  searches_path: /tmp/rsshub-stack/searches.ndjson
  history_dir: /tmp/rsshub-stack/history
  lookback: 24h
  max_posts_per_target: 10
  original_only: true
  category: AI/科技
  accounts:
    - handle: OpenAIDevs
    - handle: thsottiaux
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.XAccounts.Enabled {
		t.Fatalf("XAccounts.Enabled = false, want true")
	}
	if cfg.XAccounts.AccountsPath != "/tmp/rsshub-stack/accounts.ndjson" {
		t.Fatalf("XAccounts.AccountsPath = %q, want /tmp/rsshub-stack/accounts.ndjson", cfg.XAccounts.AccountsPath)
	}
	if cfg.XAccounts.SearchesPath != "/tmp/rsshub-stack/searches.ndjson" {
		t.Fatalf("XAccounts.SearchesPath = %q, want /tmp/rsshub-stack/searches.ndjson", cfg.XAccounts.SearchesPath)
	}
	if cfg.XAccounts.HistoryDir != "/tmp/rsshub-stack/history" {
		t.Fatalf("XAccounts.HistoryDir = %q, want /tmp/rsshub-stack/history", cfg.XAccounts.HistoryDir)
	}
	if cfg.XAccounts.Lookback != 24*time.Hour {
		t.Fatalf("XAccounts.Lookback = %v, want 24h", cfg.XAccounts.Lookback)
	}
	if cfg.XAccounts.MaxPostsPerTarget != 10 {
		t.Fatalf("XAccounts.MaxPostsPerTarget = %d, want 10", cfg.XAccounts.MaxPostsPerTarget)
	}
	if !cfg.XAccounts.OriginalOnly {
		t.Fatalf("XAccounts.OriginalOnly = false, want true")
	}
	if cfg.XAccounts.Category != "AI/科技" {
		t.Fatalf("XAccounts.Category = %q, want AI/科技", cfg.XAccounts.Category)
	}
	if got := []string{cfg.XAccounts.Accounts[0].Handle, cfg.XAccounts.Accounts[1].Handle}; !reflect.DeepEqual(got, []string{"OpenAIDevs", "thsottiaux"}) {
		t.Fatalf("XAccounts handles = %v", got)
	}
}

func TestLoadParsesXAccountsRefreshStatusConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
output: {}
proxy: {}
x_accounts:
  enabled: true
  accounts_path: /tmp/rsshub-stack/accounts.ndjson
  searches_path: /tmp/rsshub-stack/searches.ndjson
  refresh_status_path: /tmp/rsshub-stack/status.json
  refresh_wait_timeout: 2m
  refresh_wait_interval: 500ms
  refresh_reconcile_interval: 1m
  refresh_heartbeat_stale_after: 3m
  category: AI/科技
  accounts:
    - handle: OpenAIDevs
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.XAccounts.RefreshStatusPath != "/tmp/rsshub-stack/status.json" {
		t.Fatalf("XAccounts.RefreshStatusPath = %q", cfg.XAccounts.RefreshStatusPath)
	}
	if cfg.XAccounts.RefreshWaitTimeout != 2*time.Minute {
		t.Fatalf("XAccounts.RefreshWaitTimeout = %v, want 2m", cfg.XAccounts.RefreshWaitTimeout)
	}
	if cfg.XAccounts.RefreshWaitInterval != 500*time.Millisecond {
		t.Fatalf("XAccounts.RefreshWaitInterval = %v, want 500ms", cfg.XAccounts.RefreshWaitInterval)
	}
	if cfg.XAccounts.RefreshReconcile != time.Minute {
		t.Fatalf("XAccounts.RefreshReconcile = %v, want 1m", cfg.XAccounts.RefreshReconcile)
	}
	if cfg.XAccounts.HeartbeatStaleAfter != 3*time.Minute {
		t.Fatalf("XAccounts.HeartbeatStaleAfter = %v, want 3m", cfg.XAccounts.HeartbeatStaleAfter)
	}
}

func TestLoadAppliesDefaultOutputMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
output: {}
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Output.Mode != model.OutputModeTranslatedOnly {
		t.Fatalf("Output.Mode = %q, want %q", cfg.Output.Mode, model.OutputModeTranslatedOnly)
	}
}

func TestLoadDefaultsIncludeFilteredArticlesToFalse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
output: {}
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Output.IncludeFilteredArticles {
		t.Fatalf("Output.IncludeFilteredArticles = true, want false")
	}
}

func TestLoadParsesIncludeFilteredArticles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
output:
  include_filtered_articles: true
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.Output.IncludeFilteredArticles {
		t.Fatalf("Output.IncludeFilteredArticles = false, want true")
	}
}

func TestLoadAcceptsValidOutputModes(t *testing.T) {
	modes := []model.OutputMode{
		model.OutputModeOriginalOnly,
		model.OutputModeTranslatedOnly,
		model.OutputModeBilingualTranslatedFirst,
		model.OutputModeBilingualOriginalFirst,
	}

	for _, mode := range modes {
		t.Run(string(mode), func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
output:
  mode: ` + string(mode) + `
proxy: {}
`
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}

			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}

			if cfg.Output.Mode != mode {
				t.Fatalf("Output.Mode = %q, want %q", cfg.Output.Mode, mode)
			}
		})
	}
}

func TestLoadRejectsInvalidOutputMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
output:
  mode: invalid_mode
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "output.mode") {
		t.Fatalf("Load() error = %q, want mention output.mode", err)
	}
	if !strings.Contains(err.Error(), "invalid_mode") {
		t.Fatalf("Load() error = %q, want mention invalid value", err)
	}
}

func TestLoadDefaultsScheduleTimezoneToLocal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
output: {}
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ScheduleLocation == nil {
		t.Fatal("ScheduleLocation = nil")
	}
	if cfg.ScheduleLocation.String() != time.Local.String() {
		t.Fatalf("ScheduleLocation = %q, want %q", cfg.ScheduleLocation.String(), time.Local.String())
	}
}

func TestLoadAppliesConfiguredScheduleTimezone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
schedule_timezone: Asia/Shanghai
output: {}
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ScheduleLocation == nil {
		t.Fatal("ScheduleLocation = nil")
	}
	if got := cfg.ScheduleLocation.String(); got != "Asia/Shanghai" {
		t.Fatalf("ScheduleLocation = %q, want %q", got, "Asia/Shanghai")
	}
}

func TestLoadParsesScheduleDelay(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
schedule_delay: 5m
output: {}
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ScheduleDelay != 5*time.Minute {
		t.Fatalf("ScheduleDelay = %v, want 5m", cfg.ScheduleDelay)
	}
}

func TestLoadAppliesSchedulePrefetchWaitTimeout(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want time.Duration
	}{
		{name: "default", want: DefaultSchedulePrefetchWaitTimeout},
		{name: "configured", raw: "4m", want: 4 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
output: {}
proxy: {}
`
			if tt.raw != "" {
				content += "schedule_prefetch_wait_timeout: " + tt.raw + "\n"
			}
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}

			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.SchedulePrefetchWaitTimeout != tt.want {
				t.Fatalf("SchedulePrefetchWaitTimeout = %v, want %v", cfg.SchedulePrefetchWaitTimeout, tt.want)
			}
		})
	}
}

func TestLoadRejectsInvalidSchedulePrefetchWaitTimeout(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
schedule_prefetch_wait_timeout: 0s
output: {}
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want schedule_prefetch_wait_timeout error")
	}
	if !strings.Contains(err.Error(), "schedule_prefetch_wait_timeout") {
		t.Fatalf("Load() error = %q, want mention schedule_prefetch_wait_timeout", err)
	}
}

func TestLoadRejectsNegativeScheduleDelay(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
schedule_delay: -5m
output: {}
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want schedule_delay error")
	}
	if !strings.Contains(err.Error(), "schedule_delay") {
		t.Fatalf("Load() error = %q, want mention schedule_delay", err)
	}
}

func TestLoadRejectsInvalidScheduleTimezone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
schedule_timezone: Mars/Base
output: {}
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "schedule_timezone") {
		t.Fatalf("Load() error = %q, want mention schedule_timezone", err)
	}
}

func TestLoadTrimsScheduleTimezoneWhitespace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
schedule_timezone: " Asia/Shanghai "
output: {}
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.ScheduleLocation.String(); got != "Asia/Shanghai" {
		t.Fatalf("ScheduleLocation = %q, want %q", got, "Asia/Shanghai")
	}
}

func TestLoadAppliesDefaultFetchConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
output: {}
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Fetch.Timeout != 30*time.Second {
		t.Fatalf("Fetch.Timeout = %v, want %v", cfg.Fetch.Timeout, 30*time.Second)
	}
	if cfg.Fetch.RetryTimes != 3 {
		t.Fatalf("Fetch.RetryTimes = %d, want %d", cfg.Fetch.RetryTimes, 3)
	}
	if cfg.Fetch.RetryWaitTime != 200*time.Millisecond {
		t.Fatalf("Fetch.RetryWaitTime = %v, want %v", cfg.Fetch.RetryWaitTime, 200*time.Millisecond)
	}
}

func TestLoadParsesConfiguredFetchConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
fetch:
  timeout: 45s
  retry_times: 5
  retry_wait_time: 750ms
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
output: {}
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Fetch.Timeout != 45*time.Second {
		t.Fatalf("Fetch.Timeout = %v, want %v", cfg.Fetch.Timeout, 45*time.Second)
	}
	if cfg.Fetch.RetryTimes != 5 {
		t.Fatalf("Fetch.RetryTimes = %d, want %d", cfg.Fetch.RetryTimes, 5)
	}
	if cfg.Fetch.RetryWaitTime != 750*time.Millisecond {
		t.Fatalf("Fetch.RetryWaitTime = %v, want %v", cfg.Fetch.RetryWaitTime, 750*time.Millisecond)
	}
}

func TestLoadRejectsInvalidFetchConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{
			name: "bad timeout",
			config: `sources: []
keywords: []
fetch:
  timeout: nope
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
output: {}
proxy: {}
`,
			wantErr: "fetch.timeout",
		},
		{
			name: "retry times below one",
			config: `sources: []
keywords: []
fetch:
  retry_times: 0
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
output: {}
proxy: {}
`,
			wantErr: "fetch.retry_times",
		},
		{
			name: "negative retry wait",
			config: `sources: []
keywords: []
fetch:
  retry_wait_time: -1ms
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
output: {}
proxy: {}
`,
			wantErr: "fetch.retry_wait_time",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(path, []byte(tt.config), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Load() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadAppliesDefaultEmailDeliveryConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
output: {}
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Email.Timeout != 3*time.Second {
		t.Fatalf("Email.Timeout = %v, want %v", cfg.Email.Timeout, 3*time.Second)
	}
	if cfg.Email.RetryTimes != 3 {
		t.Fatalf("Email.RetryTimes = %d, want %d", cfg.Email.RetryTimes, 3)
	}
	if cfg.Email.RetryWaitTime != 500*time.Millisecond {
		t.Fatalf("Email.RetryWaitTime = %v, want %v", cfg.Email.RetryWaitTime, 500*time.Millisecond)
	}
	if cfg.Email.UseProxy {
		t.Fatalf("Email.UseProxy = true, want false")
	}
}

func TestLoadParsesConfiguredEmailDeliveryConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
  timeout: 4s
  retry_times: 2
  retry_wait_time: 250ms
  use_proxy: true
schedule: []
output: {}
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Email.Timeout != 4*time.Second {
		t.Fatalf("Email.Timeout = %v, want %v", cfg.Email.Timeout, 4*time.Second)
	}
	if cfg.Email.RetryTimes != 2 {
		t.Fatalf("Email.RetryTimes = %d, want %d", cfg.Email.RetryTimes, 2)
	}
	if cfg.Email.RetryWaitTime != 250*time.Millisecond {
		t.Fatalf("Email.RetryWaitTime = %v, want %v", cfg.Email.RetryWaitTime, 250*time.Millisecond)
	}
	if !cfg.Email.UseProxy {
		t.Fatalf("Email.UseProxy = false, want true")
	}
}

func TestLoadRejectsInvalidEmailDeliveryConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{
			name: "bad timeout",
			config: `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
  timeout: nope
schedule: []
output: {}
proxy: {}
`,
			wantErr: "email.timeout",
		},
		{
			name: "retry times below one",
			config: `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
  retry_times: 0
schedule: []
output: {}
proxy: {}
`,
			wantErr: "email.retry_times",
		},
		{
			name: "negative retry wait",
			config: `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
  retry_wait_time: -1ms
schedule: []
output: {}
proxy: {}
`,
			wantErr: "email.retry_wait_time",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(path, []byte(tt.config), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}

			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Load() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadAllowsEmptySourcesScheduleAndEmail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
schedule: []
output: {}
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoadAllowsHackerNewsSourceWithoutURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources:
  - name: Hacker News
    type: hackernews
    category: AI/科技
keywords: []
schedule: []
output: {}
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoadRejectsInvalidSourceConfig(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		wantErr string
	}{
		{name: "missing name", source: "url: https://example.com/feed.xml\ntype: rss\ncategory: AI/科技", wantErr: "sources[0].name"},
		{name: "invalid type", source: "name: Example\nurl: https://example.com/feed.xml\ntype: nope\ncategory: AI/科技", wantErr: "sources[0].type"},
		{name: "invalid url", source: "name: Example\nurl: notaurl\ntype: rss\ncategory: AI/科技", wantErr: "sources[0].url"},
		{name: "missing category", source: "name: Example\nurl: https://example.com/feed.xml\ntype: rss", wantErr: "sources[0].category"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			content := "sources:\n  - " + strings.ReplaceAll(tt.source, "\n", "\n    ") + "\n" +
				"keywords: []\n" +
				"email:\n" +
				"  smtp_host: smtp.example.com\n" +
				"  smtp_port: 465\n" +
				"  from: from@example.com\n" +
				"  to: to@example.com\n" +
				"schedule: []\n" +
				"output: {}\n" +
				"proxy: {}\n"
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Load() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadParsesRSSHubAccessKeyEnvironment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources:
  - name: Protected RSSHub
    url: https://rsshub.example.com/yicai/brief
    type: rss
    category: 新闻财经
    rsshub_access_key_env: RSSHUB_ACCESS_KEY
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
output: {}
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.Sources[0].RSSHubAccessKeyEnv; got != "RSSHUB_ACCESS_KEY" {
		t.Fatalf("Sources[0].RSSHubAccessKeyEnv = %q", got)
	}
}

func TestValidateSourceRejectsInvalidRSSHubAuthentication(t *testing.T) {
	tests := []struct {
		name    string
		source  Source
		wantErr string
	}{
		{
			name:    "invalid environment name",
			source:  Source{Name: "RSSHub", URL: "https://rsshub.example.com/feed", Type: SourceTypeRSS, Category: "新闻财经", RSSHubAccessKeyEnv: "BAD-NAME"},
			wantErr: "environment variable name",
		},
		{
			name:    "remote http",
			source:  Source{Name: "RSSHub", URL: "http://rsshub.example.com/feed", Type: SourceTypeRSS, Category: "新闻财经", RSSHubAccessKeyEnv: "RSSHUB_ACCESS_KEY"},
			wantErr: "must use https",
		},
		{
			name:    "query",
			source:  Source{Name: "RSSHub", URL: "https://rsshub.example.com/feed?code=old", Type: SourceTypeRSS, Category: "新闻财经", RSSHubAccessKeyEnv: "RSSHUB_ACCESS_KEY"},
			wantErr: "must not contain",
		},
		{
			name:    "non rss source",
			source:  Source{Name: "Page", URL: "https://example.com/page", Type: SourceTypeDocsPage, Category: "AI/科技", RSSHubAccessKeyEnv: "RSSHUB_ACCESS_KEY"},
			wantErr: "only rss sources",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSource(0, tt.source)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validateSource() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadRejectsInvalidScheduleExpression(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule:
  - nope
output: {}
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "schedule[0]") {
		t.Fatalf("Load() error = %v, want schedule[0]", err)
	}
}

func TestValidateEmailForSendingRequiresCompleteIdentity(t *testing.T) {
	tests := []struct {
		name    string
		email   Email
		wantErr string
	}{
		{name: "missing smtp host", email: Email{SMTPPort: 465, From: "from@example.com", To: "to@example.com"}, wantErr: "email.smtp_host"},
		{name: "bad port", email: Email{SMTPHost: "smtp.example.com", SMTPPort: 70000, From: "from@example.com", To: "to@example.com"}, wantErr: "email.smtp_port"},
		{name: "bad from", email: Email{SMTPHost: "smtp.example.com", SMTPPort: 465, From: "not-an-address", To: "to@example.com"}, wantErr: "email.from"},
		{name: "bad to", email: Email{SMTPHost: "smtp.example.com", SMTPPort: 465, From: "from@example.com", To: "not-an-address"}, wantErr: "email.to"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEmailForSending(tt.email)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateEmailForSending() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateEmailForSendingAcceptsCompleteIdentity(t *testing.T) {
	err := ValidateEmailForSending(Email{SMTPHost: "smtp.example.com", SMTPPort: 465, From: "from@example.com", To: "to@example.com"})
	if err != nil {
		t.Fatalf("ValidateEmailForSending() error = %v", err)
	}
}

func TestLoadRejectsInvalidEmailIdentityConfig(t *testing.T) {
	tests := []struct {
		name    string
		email   string
		wantErr string
	}{
		{name: "missing smtp host", email: "smtp_port: 465\nfrom: from@example.com\nto: to@example.com", wantErr: "email.smtp_host"},
		{name: "bad port", email: "smtp_host: smtp.example.com\nsmtp_port: 70000\nfrom: from@example.com\nto: to@example.com", wantErr: "email.smtp_port"},
		{name: "bad from", email: "smtp_host: smtp.example.com\nsmtp_port: 465\nfrom: not-an-address\nto: to@example.com", wantErr: "email.from"},
		{name: "bad to", email: "smtp_host: smtp.example.com\nsmtp_port: 465\nfrom: from@example.com\nto: not-an-address", wantErr: "email.to"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			content := "sources: []\n" +
				"keywords: []\n" +
				"email:\n  " + strings.ReplaceAll(tt.email, "\n", "\n  ") + "\n" +
				"schedule: []\n" +
				"output: {}\n" +
				"proxy: {}\n"
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Load() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadAppliesBrowseboxWatchProxyProviderDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
fetch: {}
watch:
  sites:
    - name: Claude Platform Release Notes
      type: announcement_page
      home_url: https://platform.claude.com/docs/en/release-notes/overview
      briefing_category: AI/科技
  proxy_provider:
    enabled: true
    type: browsebox
email: {}
schedule: []
output: {}
proxy: {}
ai: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Watch.ArticleConcurrency != DefaultWatchArticleConcurrency {
		t.Fatalf("ArticleConcurrency = %d, want %d", cfg.Watch.ArticleConcurrency, DefaultWatchArticleConcurrency)
	}
	provider := cfg.Watch.ProxyProvider
	if !provider.Enabled || provider.Type != "browsebox" {
		t.Fatalf("ProxyProvider = %#v", provider)
	}
	if provider.Command != "browsebox" {
		t.Fatalf("Command = %q, want browsebox", provider.Command)
	}
	if provider.Mode != "proxy" {
		t.Fatalf("Mode = %q, want proxy", provider.Mode)
	}
	if provider.NodesConcurrency != 12 {
		t.Fatalf("NodesConcurrency = %d, want 12", provider.NodesConcurrency)
	}
	if provider.DelayTimeoutMS != 7000 {
		t.Fatalf("DelayTimeoutMS = %d, want 7000", provider.DelayTimeoutMS)
	}
	if provider.StartupTimeout != 2*time.Minute {
		t.Fatalf("StartupTimeout = %v, want 2m", provider.StartupTimeout)
	}
	if provider.ProxyPort != 17997 || provider.ControllerPort != 17998 {
		t.Fatalf("ports = %d/%d", provider.ProxyPort, provider.ControllerPort)
	}
	if len(provider.HealthURLs) != 0 {
		t.Fatalf("HealthURLs = %#v, want empty so Watch sites are used at runtime", provider.HealthURLs)
	}
}

func TestLoadExpandsBrowseboxWatchProxyProviderCommandHome(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	t.Setenv("HOME", home)
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
fetch: {}
watch:
  proxy_provider:
    enabled: true
    type: browsebox
    command: ~/Projects/browsebox/browsebox
email: {}
schedule: []
output: {}
proxy: {}
ai: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := filepath.Join(home, "Projects", "browsebox", "browsebox")
	if cfg.Watch.ProxyProvider.Command != want {
		t.Fatalf("Command = %q, want %q", cfg.Watch.ProxyProvider.Command, want)
	}
}

func TestLoadAcceptsBrowseboxWatchProxyProviderHealthURLs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
fetch: {}
watch:
  proxy_provider:
    enabled: true
    type: browsebox
    mode: proxy
    health_urls:
      - https://support.claude.com/zh-CN
      - https://www.anthropic.com/news
email: {}
schedule: []
output: {}
proxy: {}
ai: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Watch.ProxyProvider.Mode != "proxy" {
		t.Fatalf("Mode = %q, want proxy", cfg.Watch.ProxyProvider.Mode)
	}
	want := []string{"https://support.claude.com/zh-CN", "https://www.anthropic.com/news"}
	if !reflect.DeepEqual(cfg.Watch.ProxyProvider.HealthURLs, want) {
		t.Fatalf("HealthURLs = %#v, want %#v", cfg.Watch.ProxyProvider.HealthURLs, want)
	}
}

func TestLoadAppliesConfiguredWatchArticleConcurrency(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
fetch: {}
watch:
  article_concurrency: 2
email: {}
schedule: []
output: {}
proxy: {}
ai: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Watch.ArticleConcurrency != 2 {
		t.Fatalf("ArticleConcurrency = %d, want 2", cfg.Watch.ArticleConcurrency)
	}
}

func TestLoadAppliesWatchDeepVerificationDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
fetch: {}
watch: {}
email: {}
schedule: []
output: {}
proxy: {}
ai: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Watch.DeepVerifyInterval != 24*time.Hour || cfg.Watch.DeepVerifyBatchSize != 48 {
		t.Fatalf("watch deep verification = %s/%d", cfg.Watch.DeepVerifyInterval, cfg.Watch.DeepVerifyBatchSize)
	}
}

func TestLoadAppliesConfiguredWatchDeepVerification(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
fetch: {}
watch:
  deep_verify_interval: 12h
  deep_verify_batch_size: 16
email: {}
schedule: []
output: {}
proxy: {}
ai: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Watch.DeepVerifyInterval != 12*time.Hour || cfg.Watch.DeepVerifyBatchSize != 16 {
		t.Fatalf("watch deep verification = %s/%d", cfg.Watch.DeepVerifyInterval, cfg.Watch.DeepVerifyBatchSize)
	}
}

func TestLoadRejectsInvalidWatchArticleConcurrency(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
fetch: {}
watch:
  article_concurrency: 0
email: {}
schedule: []
output: {}
proxy: {}
ai: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "watch.article_concurrency") {
		t.Fatalf("Load() error = %v, want watch.article_concurrency", err)
	}
}

func TestLoadAcceptsBrowseboxWatchProxyProviderStartupTimeout(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
fetch: {}
watch:
  proxy_provider:
    enabled: true
    type: browsebox
    startup_timeout: 90s
email: {}
schedule: []
output: {}
proxy: {}
ai: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Watch.ProxyProvider.StartupTimeout != 90*time.Second {
		t.Fatalf("StartupTimeout = %v, want 90s", cfg.Watch.ProxyProvider.StartupTimeout)
	}
}

func TestLoadRejectsInvalidWatchProxyProviderConfig(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		wantErr  string
	}{
		{name: "bad type", provider: "enabled: true\ntype: other", wantErr: "watch.proxy_provider.type"},
		{name: "bad mode", provider: "enabled: true\ntype: browsebox\nmode: run", wantErr: "watch.proxy_provider.mode"},
		{name: "blank command", provider: "enabled: true\ntype: browsebox\ncommand: ' '", wantErr: "watch.proxy_provider.command"},
		{name: "bad health url", provider: "enabled: true\ntype: browsebox\nhealth_urls:\n  - not-a-url", wantErr: "watch.proxy_provider.health_urls[0]"},
		{name: "bad concurrency", provider: "enabled: true\ntype: browsebox\nnodes_concurrency: -1", wantErr: "watch.proxy_provider.nodes_concurrency"},
		{name: "bad delay timeout", provider: "enabled: true\ntype: browsebox\ndelay_timeout_ms: 0", wantErr: "watch.proxy_provider.delay_timeout_ms"},
		{name: "bad startup timeout", provider: "enabled: true\ntype: browsebox\nstartup_timeout: 0s", wantErr: "watch.proxy_provider.startup_timeout"},
		{name: "bad proxy port", provider: "enabled: true\ntype: browsebox\nproxy_port: 70000", wantErr: "watch.proxy_provider.proxy_port"},
		{name: "bad controller port", provider: "enabled: true\ntype: browsebox\ncontroller_port: 70000", wantErr: "watch.proxy_provider.controller_port"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			content := "sources: []\n" +
				"keywords: []\n" +
				"fetch: {}\n" +
				"watch:\n  proxy_provider:\n    " + strings.ReplaceAll(tt.provider, "\n", "\n    ") + "\n" +
				"email: {}\n" +
				"schedule: []\n" +
				"output: {}\n" +
				"proxy: {}\n" +
				"ai: {}\n"
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Load() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadRejectsInvalidProxyConfig(t *testing.T) {
	tests := []struct {
		name    string
		proxy   string
		wantErr string
	}{
		{name: "bad http scheme", proxy: "http: ftp://example.com", wantErr: "proxy.http"},
		{name: "bad socks scheme", proxy: "socks5: http://127.0.0.1:1080", wantErr: "proxy.socks5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			content := "sources: []\n" +
				"keywords: []\n" +
				"email:\n" +
				"  smtp_host: smtp.example.com\n" +
				"  smtp_port: 465\n" +
				"  from: from@example.com\n" +
				"  to: to@example.com\n" +
				"schedule: []\n" +
				"output: {}\n" +
				"proxy:\n  " + strings.ReplaceAll(tt.proxy, "\n", "\n  ") + "\n"
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Load() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadRejectsInvalidAIConfig(t *testing.T) {
	tests := []struct {
		name    string
		ai      string
		wantErr string
	}{
		{name: "blank command", ai: "command: \" \"", wantErr: "ai.command"},
		{name: "blank arg", ai: "command: ccs\nargs:\n  - \" \"", wantErr: "ai.args[0]"},
		{name: "blank default model", ai: "models:\n  default: \" \"", wantErr: "ai.models.default"},
		{name: "blank translation model", ai: "models:\n  translation: \" \"", wantErr: "ai.models.translation"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			content := "sources: []\n" +
				"keywords: []\n" +
				"email:\n" +
				"  smtp_host: smtp.example.com\n" +
				"  smtp_port: 465\n" +
				"  from: from@example.com\n" +
				"  to: to@example.com\n" +
				"schedule: []\n" +
				"output: {}\n" +
				"proxy: {}\n" +
				"ai:\n  " + strings.ReplaceAll(tt.ai, "\n", "\n  ") + "\n"
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Load() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadSupportsPageSourcesAndScopedKeywords(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources:
  - name: GLM Docs
    url: https://example.com/glm
    type: docs_page
    category: AI/科技
    keywords:
      - GLM
    page_kind: announcement
    time_hint: published
keywords: []
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
output: {}
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Sources) != 1 {
		t.Fatalf("len(Sources) = %d, want %d", len(cfg.Sources), 1)
	}

	source := cfg.Sources[0]
	if !reflect.DeepEqual(source.Keywords, []string{"GLM"}) {
		t.Fatalf("Source.Keywords = %v, want %v", source.Keywords, []string{"GLM"})
	}
	if source.PageKind != "announcement" {
		t.Fatalf("Source.PageKind = %q, want %q", source.PageKind, "announcement")
	}
	if source.TimeHint != "published" {
		t.Fatalf("Source.TimeHint = %q, want %q", source.TimeHint, "published")
	}
}

func TestLoadSupportsWatchSites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords: []
watch:
  sites:
    - name: Anthropic Claude Support
      type: anthropic_support
      home_url: https://support.claude.com/zh-CN
      briefing_category: AI/科技
      category_allowlist:
        - Claude
        - 安全保障
      high_value_keywords:
        - 身份验证
        - 电话验证
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
schedule: []
output: {}
proxy: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Watch.Sites) != 1 {
		t.Fatalf("len(cfg.Watch.Sites) = %d, want 1", len(cfg.Watch.Sites))
	}
	site := cfg.Watch.Sites[0]
	if site.Type != WatchTypeAnthropicSupport {
		t.Fatalf("site.Type = %q", site.Type)
	}
	if !reflect.DeepEqual(site.CategoryAllowlist, []string{"Claude", "安全保障"}) {
		t.Fatalf("site.CategoryAllowlist = %#v", site.CategoryAllowlist)
	}
	if !reflect.DeepEqual(site.HighValueKeywords, []string{"身份验证", "电话验证"}) {
		t.Fatalf("site.HighValueKeywords = %#v", site.HighValueKeywords)
	}
}

func TestLoadRejectsInvalidWatchSiteConfig(t *testing.T) {
	tests := []struct {
		name    string
		site    string
		wantErr string
	}{
		{name: "missing name", site: "type: anthropic_support\nhome_url: https://support.claude.com/zh-CN\nbriefing_category: AI/科技", wantErr: "watch.sites[0].name"},
		{name: "invalid type", site: "name: Anthropic\ntype: nope\nhome_url: https://support.claude.com/zh-CN\nbriefing_category: AI/科技", wantErr: "watch.sites[0].type"},
		{name: "invalid home url", site: "name: Anthropic\ntype: anthropic_support\nhome_url: notaurl\nbriefing_category: AI/科技", wantErr: "watch.sites[0].home_url"},
		{name: "missing briefing category", site: "name: Anthropic\ntype: anthropic_support\nhome_url: https://support.claude.com/zh-CN", wantErr: "watch.sites[0].briefing_category"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			content := "sources: []\n" +
				"keywords: []\n" +
				"watch:\n" +
				"  sites:\n" +
				"    - " + strings.ReplaceAll(tt.site, "\n", "\n      ") + "\n" +
				"email:\n" +
				"  smtp_host: smtp.example.com\n" +
				"  smtp_port: 465\n" +
				"  from: from@example.com\n" +
				"  to: to@example.com\n" +
				"schedule: []\n" +
				"output: {}\n" +
				"proxy: {}\n"
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Load() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestProjectConfigIncludesAnthropicSupportWatch(t *testing.T) {
	configPaths := map[string]string{
		"example": filepath.Join("..", "..", "configs", "config.example.yaml"),
	}

	for name, configPath := range configPaths {
		t.Run(name, func(t *testing.T) {
			cfg, err := Load(configPath)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			found := false
			for _, site := range cfg.Watch.Sites {
				if site.Name != "Anthropic Claude Support" || site.Type != WatchTypeAnthropicSupport {
					continue
				}
				found = true
				if site.BriefingCategory != "AI/科技" {
					t.Fatalf("site.BriefingCategory = %q", site.BriefingCategory)
				}
				if len(site.CategoryAllowlist) == 0 || len(site.HighValueKeywords) == 0 {
					t.Fatalf("site = %#v", site)
				}
			}
			if !found {
				t.Fatalf("Anthropic Claude Support site not found in %#v", cfg.Watch.Sites)
			}
		})
	}
}

func TestProjectConfigIncludesAnthropicOfficialAnnouncementWatchSites(t *testing.T) {
	configPaths := map[string]string{
		"example": filepath.Join("..", "..", "configs", "config.example.yaml"),
	}
	want := []WatchSite{
		{
			Name:             "Anthropic News",
			Type:             WatchTypeAnnouncementPage,
			HomeURL:          "https://www.anthropic.com/news",
			BriefingCategory: "AI/科技",
		},
		{
			Name:             "Claude Platform Release Notes",
			Type:             WatchTypeAnnouncementPage,
			HomeURL:          "https://docs.claude.com/en/release-notes/overview",
			BriefingCategory: "AI/科技",
		},
	}

	for name, configPath := range configPaths {
		t.Run(name, func(t *testing.T) {
			cfg, err := Load(configPath)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}

			got := make(map[string]WatchSite, len(cfg.Watch.Sites))
			for _, site := range cfg.Watch.Sites {
				key := site.Name + "|" + site.Type + "|" + site.HomeURL + "|" + site.BriefingCategory
				got[key] = site
			}

			for _, site := range want {
				key := site.Name + "|" + site.Type + "|" + site.HomeURL + "|" + site.BriefingCategory
				gotSite, exists := got[key]
				if !exists {
					t.Fatalf("missing Anthropic official watch site: %+v", site)
				}
				if len(gotSite.HighValueKeywords) == 0 {
					t.Fatalf("site.HighValueKeywords for %q = empty", gotSite.Name)
				}
			}
		})
	}
}

func TestProjectConfigIncludesDiscoveryEnhancementAISources(t *testing.T) {
	configPaths := map[string]string{
		"example": filepath.Join("..", "..", "configs", "config.example.yaml"),
	}
	want := []Source{
		{Name: "Bing / Microsoft Search Blog", URL: "https://blogs.bing.com/Home/feed", Type: SourceTypeRSS, Category: "AI/科技"},
	}

	for name, configPath := range configPaths {
		t.Run(name, func(t *testing.T) {
			cfg, err := Load(configPath)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}

			got := make(map[string]Source, len(cfg.Sources))
			for _, source := range cfg.Sources {
				key := source.Name + "|" + source.URL + "|" + source.Type + "|" + source.Category
				if _, exists := got[key]; exists {
					t.Fatalf("duplicate source found: %+v", source)
				}
				got[key] = source
			}
			for _, source := range want {
				key := source.Name + "|" + source.URL + "|" + source.Type + "|" + source.Category
				gotSource, exists := got[key]
				if !exists {
					t.Fatalf("missing discovery enhancement source: %+v", source)
				}
				if len(gotSource.Keywords) != 0 {
					t.Fatalf("Source.Keywords for %q = %v, want empty", gotSource.Name, gotSource.Keywords)
				}
				if gotSource.PageKind != "" {
					t.Fatalf("Source.PageKind for %q = %q, want empty", gotSource.Name, gotSource.PageKind)
				}
				if gotSource.TimeHint != "" {
					t.Fatalf("Source.TimeHint for %q = %q, want empty", gotSource.Name, gotSource.TimeHint)
				}
			}
		})
	}
}

func TestProjectConfigDoesNotIncludeRemovedCognitionRSS(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "configs", "config.example.yaml"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	for _, source := range cfg.Sources {
		if source.Name == "Cognition Blog" || strings.Contains(source.URL, "cognition.ai/rss.xml") {
			t.Fatalf("config.example.yaml includes removed Cognition RSS source: %+v", source)
		}
	}
}

func TestLoadParsesFilters(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `sources: []
keywords:
  - AI
filters:
  categories:
    新闻财经:
      include_keywords:
        - 央行
        - IPO
      weak_keywords:
        - 企业
        - 投资
      min_weak_keyword_matches: 2
      exclude_keywords:
        - 体育
  sources:
    Hacker News:
      max_articles: 12
      priority: 20
      exclude_keywords:
        - Show HN
email: {}
schedule: []
output: {}
proxy: {}
ai: {}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	category := cfg.Filters.Categories["新闻财经"]
	if !reflect.DeepEqual(category.IncludeKeywords, []string{"央行", "IPO"}) {
		t.Fatalf("IncludeKeywords = %#v", category.IncludeKeywords)
	}
	if !reflect.DeepEqual(category.WeakKeywords, []string{"企业", "投资"}) {
		t.Fatalf("WeakKeywords = %#v", category.WeakKeywords)
	}
	if category.MinWeakKeywordMatches != 2 {
		t.Fatalf("MinWeakKeywordMatches = %d, want 2", category.MinWeakKeywordMatches)
	}
	if !reflect.DeepEqual(category.ExcludeKeywords, []string{"体育"}) {
		t.Fatalf("ExcludeKeywords = %#v", category.ExcludeKeywords)
	}
	source := cfg.Filters.Sources["Hacker News"]
	if source.MaxArticles != 12 {
		t.Fatalf("MaxArticles = %d, want 12", source.MaxArticles)
	}
	if source.Priority != 20 {
		t.Fatalf("Priority = %d, want 20", source.Priority)
	}
	if !reflect.DeepEqual(source.ExcludeKeywords, []string{"Show HN"}) {
		t.Fatalf("Source ExcludeKeywords = %#v", source.ExcludeKeywords)
	}
}

func TestLoadRejectsInvalidFilters(t *testing.T) {
	tests := []struct {
		name    string
		filter  string
		wantErr string
	}{
		{
			name:    "empty category keyword",
			filter:  "filters:\n  categories:\n    AI/科技:\n      include_keywords:\n        - ''\n",
			wantErr: "filters.categories",
		},
		{
			name:    "negative source max",
			filter:  "filters:\n  sources:\n    Hacker News:\n      max_articles: -1\n",
			wantErr: "max_articles",
		},
		{
			name:    "negative source priority",
			filter:  "filters:\n  sources:\n    Hacker News:\n      priority: -1\n",
			wantErr: "priority",
		},
		{
			name:    "duplicate source keyword",
			filter:  "filters:\n  sources:\n    Hacker News:\n      exclude_keywords:\n        - Sport\n        - sport\n",
			wantErr: "duplicate keyword",
		},
		{
			name:    "negative weak match minimum",
			filter:  "filters:\n  categories:\n    AI/科技:\n      weak_keywords:\n        - 模型\n      min_weak_keyword_matches: -1\n",
			wantErr: "min_weak_keyword_matches",
		},
		{
			name:    "weak minimum without weak keywords",
			filter:  "filters:\n  categories:\n    AI/科技:\n      include_keywords:\n        - OpenAI\n      min_weak_keyword_matches: 2\n",
			wantErr: "requires weak_keywords",
		},
		{
			name:    "keyword appears in strong and weak lists",
			filter:  "filters:\n  categories:\n    AI/科技:\n      include_keywords:\n        - Model\n      weak_keywords:\n        - model\n",
			wantErr: "also appears in include_keywords",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			content := "sources: []\nkeywords: []\n" + tt.filter + "email: {}\nschedule: []\noutput: {}\nproxy: {}\nai: {}\n"
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Load() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadRejectsInvalidSourceItemLimits(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		wantErr string
	}{
		{
			name:    "negative",
			source:  "  - name: Feed\n    url: https://example.com/feed.xml\n    type: rss\n    category: AI/科技\n    max_items: -1\n",
			wantErr: "max_items",
		},
		{
			name:    "non rss",
			source:  "  - name: HN\n    type: hackernews\n    category: AI/科技\n    max_items: 10\n",
			wantErr: "only rss sources",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			content := "sources:\n" + tc.source + "keywords: []\nemail: {}\nschedule: []\noutput: {}\nproxy: {}\nai: {}\n"
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Load() error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestProjectConfigDoesNotIncludeRemovedAllenAIRSS(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "configs", "config.example.yaml"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	for _, source := range cfg.Sources {
		if source.Name == "AllenAI Blog" || strings.Contains(source.URL, "allenai.org/rss.xml") {
			t.Fatalf("config.example.yaml includes removed AllenAI RSS source: %+v", source)
		}
	}
}

func TestProjectConfigIncludesDiscoveryEnhancementAIKeywords(t *testing.T) {
	configPaths := map[string]string{
		"example": filepath.Join("..", "..", "configs", "config.example.yaml"),
	}
	want := []string{"AllenAI", "Ai2", "GLM", "Qwen", "千问", "HappyHorse"}
	rejected := []string{"BigModel", "z.ai", "ACE Studio", "StepFun", "Paper Review", "BYOK", "Terafab"}

	for name, configPath := range configPaths {
		t.Run(name, func(t *testing.T) {
			cfg, err := Load(configPath)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}

			got := make(map[string]struct{}, len(cfg.Keywords))
			for _, keyword := range cfg.Keywords {
				if _, exists := got[keyword]; exists {
					t.Fatalf("duplicate keyword found: %q", keyword)
				}
				got[keyword] = struct{}{}
			}
			for _, keyword := range want {
				if _, exists := got[keyword]; !exists {
					t.Fatalf("missing discovery enhancement keyword: %q", keyword)
				}
			}
			for _, keyword := range rejected {
				if _, exists := got[keyword]; exists {
					t.Fatalf("unexpected noisy keyword included: %q", keyword)
				}
			}
		})
	}
}
